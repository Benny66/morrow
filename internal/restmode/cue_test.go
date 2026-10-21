package restmode

import (
	"testing"
	"time"
)

// TestCueCoversAllRestKinds 验证三类休息提醒都有内置引导动作。
//
// 【为什么单独测】US-14 承诺「每种类型至少 1 个动作，离线可用」；加/减一类
// 时忘了配 cue 就会返回零值，前端渲染成空气泡。
func TestCueCoversAllRestKinds(t *testing.T) {
	for _, kind := range []string{"stand", "move", "rest"} {
		cue := CueFor(kind)
		if cue.Text == "" {
			t.Errorf("kind=%q 缺少引导动作文案", kind)
		}
		if cue.Duration <= 0 {
			t.Errorf("kind=%q 缺少默认倒计时", kind)
		}
	}
}

// TestCueReturnsZeroForNonRest 验证 water/toilet/custom 不进入休息模式。
//
// CueFor 对这三类返回零值；引擎在 dispatch 时据此判断走原路。
// 【为什么不返回兜底文案】兜底会让 water 类误进休息模式——用户点"喝水"
// 只想打勾，不该被 2 分钟倒计时锁住屏幕。
func TestCueReturnsZeroForNonRest(t *testing.T) {
	for _, kind := range []string{"water", "toilet", "custom", "", "unknown"} {
		cue := CueFor(kind)
		if cue.Text != "" || cue.Duration != 0 {
			t.Errorf("kind=%q 应返回零 cue，实际 %+v", kind, cue)
		}
	}
}

// TestCueDurationsMatchSpec 验证三类默认时长符合 spec 表格（rest=60/stand=120/move=180）。
//
// 【为什么单独测】这三个数字在 spec、CONTEXT、Alert.vue 的 UI 提示里都出现过；
// 一处改动忘了同步会让「120 秒站」变成「60 秒站」，产品语义就飘了。
func TestCueDurationsMatchSpec(t *testing.T) {
	cases := map[string]time.Duration{
		"rest":  60 * time.Second,
		"stand": 120 * time.Second,
		"move":  180 * time.Second,
	}
	for kind, want := range cases {
		if got := CueFor(kind).Duration; got != want {
			t.Errorf("kind=%q 时长应为 %v，实际 %v", kind, want, got)
		}
	}
}

// TestIsRestKind 判断是否属于休息模式三类。
func TestIsRestKind(t *testing.T) {
	for _, k := range []string{"stand", "move", "rest"} {
		if !IsRestKind(k) {
			t.Errorf("%q 应判为休息类", k)
		}
	}
	for _, k := range []string{"water", "toilet", "custom", ""} {
		if IsRestKind(k) {
			t.Errorf("%q 不应判为休息类", k)
		}
	}
}
