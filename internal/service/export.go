package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"morrow/internal/appdir"
	"morrow/internal/apperr"
)

// exportTempSuffix 导出中转文件的扩展名后缀。
// 中转文件必须与目标文件【同目录】：跨文件系统的 rename 会失败（EXDEV）。
const exportTempSuffix = ".exporting"

// ExportDatabase 把当前数据库导出一份完整可用的副本到 target 路径。
//
// 实现要点（见 openspec/changes/runtime-pipeline/design.md D5）：
//   - 用 SQLite 的 VACUUM INTO 生成一致性快照：不必停写、不必自己处理页锁与 WAL，
//     产出的是一个可直接打开的完整库文件。
//   - VACUUM INTO 要求目标文件【不存在】，但用户通常经系统「保存」对话框选路径，
//     在那里已确认过覆盖。故先导出到同目录的临时文件，成功后原子改名覆盖目标。
//   - 失败路径清理临时文件，不留垃圾。
func (s *Service) ExportDatabase(appName, target string) (err error) {
	if strings.TrimSpace(target) == "" {
		return apperr.Validation("导出路径不能为空")
	}
	cleanTarget := filepath.Clean(target)

	// 拒绝导出到数据目录内：那等于把库往自己身上复制，
	// 且临时文件/导出文件会与数据库、日志混在一处，将来清理时容易误伤。
	dataDir, dirErr := appdir.Dir(appName)
	if dirErr != nil {
		return apperr.Wrap(dirErr)
	}
	if isInside(cleanTarget, dataDir) {
		return apperr.Validation("导出路径不能位于应用数据目录内，请另选位置")
	}

	tmp := cleanTarget + exportTempSuffix
	// 上次失败可能残留，先清掉；VACUUM INTO 要求目标不存在
	if rmErr := os.Remove(tmp); rmErr != nil && !os.IsNotExist(rmErr) {
		return apperr.Wrap(fmt.Errorf("清理临时文件失败: %w", rmErr))
	}
	defer func() {
		// 仅在失败路径清理：成功时 tmp 已被 rename 掉，Remove 返回 IsNotExist 无副作用。
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()

	if execErr := s.db.Exec("VACUUM INTO ?", tmp).Error; execErr != nil {
		return apperr.Wrap(fmt.Errorf("导出数据库失败: %w", execErr))
	}

	// 原子替换（同目录，同文件系统）。
	if renameErr := os.Rename(tmp, cleanTarget); renameErr != nil {
		return apperr.Wrap(fmt.Errorf("写入导出文件失败: %w", renameErr))
	}
	return nil
}

// isInside 判断 path 是否位于 dir 之内（含 dir 自身）。
// 两侧都先 Clean，避免 ../ 或尾随分隔符绕过判断。
func isInside(path, dir string) bool {
	path = filepath.Clean(path)
	dir = filepath.Clean(dir)
	if path == dir {
		return true
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}
