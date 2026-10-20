package database

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

// captureSlog 把默认 logger 临时换成写入内存的 handler，返回缓冲与还原函数。
func captureSlog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return buf, func() { slog.SetDefault(old) }
}

// trace 用给定的耗时 / SQL / 错误触发一次 Trace 回调。
func trace(elapsed time.Duration, sql string, err error) {
	slogLogger{}.Trace(context.Background(),
		time.Now().Add(-elapsed),
		func() (string, int64) { return sql, 1 },
		err,
	)
}

// TestTraceIsSilentForFastQuery 正常查询 MUST 不记日志——慢查询日志的价值在于稀少，
// 每次查询都记会把它淹没。
func TestTraceIsSilentForFastQuery(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	trace(1*time.Millisecond, "SELECT * FROM examples WHERE id = 1", nil)

	if buf.Len() != 0 {
		t.Errorf("正常查询不应产生日志，实际输出: %q", buf.String())
	}
}

// TestTraceLogsSlowQuery 超过阈值 MUST 记一条 warn，且带上耗时与语句。
func TestTraceLogsSlowQuery(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	trace(slowQueryThreshold+10*time.Millisecond, "SELECT * FROM examples", nil)
	out := buf.String()

	if !strings.Contains(out, "level=WARN") {
		t.Errorf("慢查询应为 WARN 级，实际: %q", out)
	}
	if !strings.Contains(out, "慢查询") {
		t.Errorf("缺少可检索的中文消息，实际: %q", out)
	}
	if !strings.Contains(out, "SELECT * FROM examples") {
		t.Errorf("慢查询日志应包含语句，实际: %q", out)
	}
}

// TestTraceIgnoresRecordNotFound 记录不存在是业务正常路径（service 据此返回
// not_found），MUST NOT 记错误——否则日志会被它淹没。
func TestTraceIgnoresRecordNotFound(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	trace(1*time.Millisecond, "SELECT * FROM examples WHERE id = 42", gorm.ErrRecordNotFound)

	if buf.Len() != 0 {
		t.Errorf("记录不存在不应记日志，实际输出: %q", buf.String())
	}
}

// TestTraceLogsRealError 真实错误 MUST 记 error 级，且保留原始错误信息。
func TestTraceLogsRealError(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	trace(1*time.Millisecond, "INSERT INTO examples", errors.New("disk I/O error"))
	out := buf.String()

	if !strings.Contains(out, "level=ERROR") {
		t.Errorf("SQL 失败应为 ERROR 级，实际: %q", out)
	}
	if !strings.Contains(out, "disk I/O error") {
		t.Errorf("应保留原始错误，实际: %q", out)
	}
}

// TestTruncateSQLFlattensAndLimits 日志是一行一条，超长语句必须压平并截断，
// 否则一条 SQL 会把日志文件撑爆、也破坏逐行解析。
func TestTruncateSQLFlattensAndLimits(t *testing.T) {
	multiline := "SELECT *\nFROM examples\nWHERE id IN (1, 2, 3)"
	if got := truncateSQL(multiline); strings.Contains(got, "\n") {
		t.Errorf("换行未被压平: %q", got)
	}

	long := "SELECT '" + strings.Repeat("啊", sqlLogMaxLen*2) + "'"
	got := truncateSQL(long)
	if !strings.HasSuffix(got, "…(已截断)") {
		t.Errorf("超长语句未被标记截断: %q", got[:40])
	}
	// 按字符截断：截断后仍应是合法 UTF-8（不能把多字节字符切一半）
	if !strings.Contains(got, "啊") {
		t.Error("截断结果损坏了多字节字符")
	}
	if len([]rune(got)) > sqlLogMaxLen+8 {
		t.Errorf("截断后仍过长: %d 字符", len([]rune(got)))
	}
}

// TestLogModeKeepsSameImplementation 级别过滤交给 slog 统一处理，
// LogMode 不改行为（避免两处各配一套级别）。
func TestLogModeKeepsSameImplementation(t *testing.T) {
	logger := slogLogger{}.LogMode(0)
	if _, ok := logger.(slogLogger); !ok {
		t.Errorf("LogMode 应返回同一实现，实际: %T", logger)
	}
}
