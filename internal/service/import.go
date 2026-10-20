package service

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"my-notice/internal/appdir"
	"my-notice/internal/apperr"
	"my-notice/internal/database"
)

// preImportSuffix 导入前的现场备份后缀。备份【保留】不删：它是恢复动作本身的
// 兜底（下次导入覆盖），用户手改坏数据时还能从这里捞回上一次的状态。
const preImportSuffix = ".pre-import"

// ImportDatabase 用 source 指定的数据库文件替换当前数据库，替换失败自动回滚。
//
// 流程是 ExportDatabase（VACUUM INTO 导出）的镜像操作：
//  1. 校验：源文件非空且存在；位于数据目录内一律拒绝——数据目录里躺着现役库，
//     把「数据目录内某个文件」当源，等于把库往自己身上复制（与导出同规则）。
//  2. 关闭当前连接（WAL/shm 的句柄随连接释放）。
//  3. 现库改名 *.pre-import（同目录原子操作，失败即中止、不碰数据）。
//  4. 复制源文件到位。
//  5. database.Init 重开并迁移。失败则回滚：删掉复制进来的文件、把备份改名回来、
//     重开旧库，返回原始错误——应用回到导入前的可用状态。
//
// 已知限制：恢复后前端已加载的状态仍是旧的（绑定调用拿到的是新库，页面状态不是），
// 前端在成功后提示用户重启应用（见 Settings.vue）。
func (s *Service) ImportDatabase(appName, source string) (err error) {
	if strings.TrimSpace(source) == "" {
		return apperr.Validation("恢复路径不能为空")
	}
	cleanSource := filepath.Clean(source)
	if st, statErr := os.Stat(cleanSource); statErr != nil || st.IsDir() {
		return apperr.Validation("恢复源文件不存在或不可读")
	}

	dataDir, dirErr := appdir.Dir(appName)
	if dirErr != nil {
		return apperr.Wrap(dirErr)
	}
	if isInside(cleanSource, dataDir) {
		return apperr.Validation("恢复源不能位于应用数据目录内，请选择导出的备份文件")
	}

	dbPath, err := appdir.File(appName, appName+".db")
	if err != nil {
		return apperr.Wrap(err)
	}

	// 关闭当前连接：SQLite 文件被复制/改名时不能有活句柄，
	// 且后续重开得到的连接要整体替换 s.db。
	if closeErr := s.Close(); closeErr != nil {
		return apperr.Wrap(fmt.Errorf("关闭当前数据库失败: %w", closeErr))
	}

	backupPath := dbPath + preImportSuffix
	// 现库改名留作回滚点。上次导入的旧备份直接被覆盖（Backup() 的语义）。
	if renameErr := os.Rename(dbPath, backupPath); renameErr != nil {
		// 改名失败说明现库还动不了（如文件被外部占用）——连接已关但数据未动，
		// 重开旧库即可恢复运行态。
		s.reopenOrDie(appName)
		return apperr.Wrap(fmt.Errorf("备份当前数据库失败: %w", renameErr))
	}

	rollback := func() {
		_ = os.Remove(dbPath) // 复制进来的坏文件
		_ = os.Rename(backupPath, dbPath)
		s.reopenOrDie(appName)
	}

	// 残留的 WAL/shm 属于旧库，改名只动了主文件，复制前一并清掉。
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(dbPath + suffix) // 不存在是常态，忽略
	}

	if copyErr := copyFile(cleanSource, dbPath); copyErr != nil {
		rollback()
		return apperr.Wrap(fmt.Errorf("写入恢复文件失败: %w", copyErr))
	}

	newDB, openErr := database.Init(appName)
	if openErr != nil {
		rollback()
		// 返回【重开失败的原始错误】而不是笼统的"恢复失败"：
		// 常见原因是源文件不是 SQLite 库，用户需要看到具体原因。
		return apperr.Wrap(fmt.Errorf("恢复的数据库无法打开: %w", openErr))
	}

	s.db = newDB
	return nil
}

// reopenOrDie 重新打开数据库替换 s.db，供回滚路径使用。
// 回滚已经是最坏情况：这里再失败只能记日志并放弃——应用将处于无库状态，
// 但绝不能 panic，让上层（绑定调用）收到错误、优雅关闭链路照常工作。
func (s *Service) reopenOrDie(appName string) {
	db, openErr := database.Init(appName)
	if openErr != nil {
		slog.Error("回滚后重开数据库失败，应用处于无库状态", "err", openErr)
		return
	}
	s.db = db
}

// copyFile 覆盖式复制，目标权限 0600（数据库文件含业务数据，不放权给组/其他）。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
