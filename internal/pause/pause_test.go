package pause

import (
	"testing"
	"time"
)

// TestPauseReasonEnum 验证 PauseReason 枚举值的完整性。
//
// 【为什么必须有这个测试】PauseReason 是暂停信号的单一真相，前端/菜单栏/日志都要用它。
// 新增一个 reason 时忘了改枚举，下游就永远收不到这个信号——典型的"没有症状的失效"。
func TestPauseReasonEnum(t *testing.T) {
	reasons := []PauseReason{
		ReasonNone,
		ReasonIdle,
		ReasonLock,
		ReasonMeeting,
		ReasonFullscreen,
		ReasonFocus,
		ReasonCalendar,
	}

	// 每个 reason 必须有唯一的字符串表示（前端镜像依赖这个值）。
	seen := make(map[string]bool)
	for _, r := range reasons {
		s := string(r)
		if seen[s] {
			t.Errorf("PauseReason 枚举值重复: %s", s)
		}
		seen[s] = true
	}

	// ReasonNone 不再复用 Go 的零值 "": 前端镜像与 parity guard 需要非空字面量。
	// 零值 "" 与 ReasonNone 在语义上等价（都表示未暂停），由 State.IsPaused()
	// 同时判两者——见 provider.go 的注释。
	var zero PauseReason
	if zero == ReasonNone {
		t.Error("ReasonNone 不应是空串：空串在 TS 镜像解析器里会被跳过，parity guard 会误报缺失")
	}
	if (State{Reason: zero}).IsPaused() {
		t.Error("State{Reason: \"\"}（零值）应被视为未暂停")
	}
	if (State{Reason: ReasonNone}).IsPaused() {
		t.Error("State{Reason: ReasonNone} 应被视为未暂停")
	}
}

// TestState 验证 State 结构体的字段与语义。
func TestState(t *testing.T) {
	// 未暂停：Reason 为 None，Since 为零值。
	s := State{Reason: ReasonNone}
	if s.IsPaused() {
		t.Error("Reason=None 时 IsPaused() 应返回 false")
	}

	// 空闲暂停：Reason 为 Idle，Since 为 5 分钟前。
	idleSince := time.Now().Add(-5 * time.Minute)
	s = State{Reason: ReasonIdle, Since: idleSince}
	if !s.IsPaused() {
		t.Error("Reason=Idle 时 IsPaused() 应返回 true")
	}
	if s.Since != idleSince {
		t.Errorf("Since 字段未正确保存: got %v, want %v", s.Since, idleSince)
	}
}

// TestIdleDetector 验证空闲检测器的行为（使用 mock 实现）。
func TestIdleDetector(t *testing.T) {
	// Mock：返回固定的空闲时长。
	mock := func() time.Duration { return 6 * time.Minute }
	detector := NewIdleDetector(mock, 5*time.Minute)

	// 空闲 6 分钟 > 阈值 5 分钟 → 应返回 ReasonIdle。
	reason := detector.Detect()
	if reason != ReasonIdle {
		t.Errorf("空闲 6 分钟应返回 ReasonIdle，实际为 %s", reason)
	}

	// Mock：返回 3 分钟（未达阈值）。
	mock = func() time.Duration { return 3 * time.Minute }
	detector = NewIdleDetector(mock, 5*time.Minute)
	reason = detector.Detect()
	if reason != ReasonNone {
		t.Errorf("空闲 3 分钟应返回 ReasonNone，实际为 %s", reason)
	}
}
