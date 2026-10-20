package database

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// slowQueryThreshold 慢查询阈值。
//
// 刻意不用 gorm 的默认值 200ms：那是给远程数据库定的量级，对本地嵌入式 SQLite
// 等于永不触发——索引命中的查询在微秒级，全表扫一万行也就几毫秒。
// 50ms 高于正常查询两个数量级，又低于「明显有问题」的查询，不会产生噪音。
const slowQueryThreshold = 50 * time.Millisecond

// sqlLogMaxLen 单条 SQL 写入日志的最大长度（长 IN 子句会撑爆日志行）。
// 按【字符】截断而非字节，避免把多字节字符切成乱码。
const sqlLogMaxLen = 500

// slogLogger 把 gorm 的日志接到 slog，使慢查询与 SQL 错误进入 app.log。
//
// 为何需要它：gorm 默认 logger 写 os.Stdout —— 打包后的 GUI 应用 stdout 无人可见，
// 于是慢查询与 SQL 错误全部石沉大海。这与「Wails logger 未接线导致前端日志黑洞」
// 是同一类问题（日志写到了无人可见的地方），故同样由护栏强制
// （见 internal/guard/wiring_test.go 的 TestGormLoggerIsWired）。
type slogLogger struct{}

// LogMode 返回自身：级别过滤交给 slog 的 handler 统一处理，避免两处各配一套级别。
func (slogLogger) LogMode(logger.LogLevel) logger.Interface { return slogLogger{} }

// Info/Warn/Error 是 gorm 的通用出口（多为框架自身提示），实际有价值的
// SQL 信息都在 Trace 里，这里保持空实现以免同一件事被记两遍。
func (slogLogger) Info(context.Context, string, ...interface{})  {}
func (slogLogger) Warn(context.Context, string, ...interface{})  {}
func (slogLogger) Error(context.Context, string, ...interface{}) {}

// Trace 在每次 SQL 执行后回调，是唯一记录 SQL 的地方。
func (slogLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)

	// 记录不存在是业务有意表达的结果（service 层据此返回 apperr.NotFound），
	// 属正常路径。把它当错误记会让日志充满噪音，反而埋掉真正的问题。
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return
	}

	sql, rows := fc()
	if err != nil {
		slog.Error("SQL 执行失败",
			"ms", elapsed.Milliseconds(), "rows", rows, "sql", truncateSQL(sql), "err", err)
		return
	}
	if elapsed >= slowQueryThreshold {
		slog.Warn("慢查询",
			"ms", elapsed.Milliseconds(), "rows", rows, "sql", truncateSQL(sql))
	}
}

// truncateSQL 压平换行并按字符截断，保证一条日志一行、不会被超长语句撑爆。
func truncateSQL(sql string) string {
	sql = strings.Join(strings.Fields(sql), " ")
	r := []rune(sql)
	if len(r) <= sqlLogMaxLen {
		return sql
	}
	return string(r[:sqlLogMaxLen]) + "…(已截断)"
}
