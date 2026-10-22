package sit

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeTone(t *testing.T) {
	cases := []struct {
		in   string
		want Tone
	}{
		{"sassy", ToneSassy},
		{"lively", ToneLively},
		{"gentle", ToneGentle},
		{"", DefaultTone},
		{"unknown", DefaultTone},
	}
	for _, c := range cases {
		if got := NormalizeTone(c.in); got != c.want {
			t.Errorf("NormalizeTone(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestClampNudgeThreshold(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, DefaultNudgeThresholdMinutes},
		{-10, DefaultNudgeThresholdMinutes},
		{45, 45},
		{47, 45},
		{58, 60},
		{60, 60},
		{89, 90},
		{90, 90},
		{999, 90},
	}
	for _, c := range cases {
		if got := ClampNudgeThreshold(c.in); got != c.want {
			t.Errorf("ClampNudgeThreshold(%d) = %d，期望 %d", c.in, got, c.want)
		}
	}
}

// TestSnapshotHasNoAppIdentity 是隐私契约的编译期/反射护栏：
// Snapshot/Bubble/Focus 不得出现应用名或应用标识字段。
func TestSnapshotHasNoAppIdentity(t *testing.T) {
	for _, v := range []any{Snapshot{}, Bubble{}, Focus{}} {
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			jsonName := field.Tag.Get("json")
			for _, banned := range []string{"app_name", "app_id", "application", "window_title"} {
				if strings.Contains(strings.ToLower(field.Name), banned) ||
					strings.Contains(strings.ToLower(jsonName), banned) {
					t.Errorf("%s.%s 暴露了应用身份字段 %q——宠物快照不允许出现应用名/标识",
						typ.Name(), field.Name, jsonName)
				}
			}
		}
	}
}

// TestEnumValuesNonEmpty 防止枚举零值被误当成合法值。
// 不同枚举之间可以共享字面量（如 state 与 break kind 都有 idle），
// 只要求每个枚举自身不重复。
func TestEnumValuesNonEmpty(t *testing.T) {
	groups := map[string][]string{
		"state": {
			string(StateIdle), string(StateTracking), string(StateBreakWait),
			string(StateMuted), string(StateUnsupported),
		},
		"mood": {
			string(MoodEnergetic), string(MoodConcerned), string(MoodDroopy),
		},
		"tone": {
			string(ToneSassy), string(ToneLively), string(ToneGentle),
		},
		"bubble": {
			string(BubbleNudge), string(BubbleMemory), string(BubbleGreeting),
		},
		"action": {
			string(ActionStartBreak), string(ActionSnooze10), string(ActionMore),
			string(ActionDismiss), string(ActionOpenApp),
		},
		"end_reason": {
			string(EndReasonAppSwitch), string(EndReasonIdle), string(EndReasonLock),
			string(EndReasonFeatureOff), string(EndReasonAbandoned),
		},
		"break_kind": {
			string(BreakKindActive), string(BreakKindLock), string(BreakKindIdle),
		},
		"profile_status": {
			string(ProfileStatusOK), string(ProfileStatusUnsupported),
			string(ProfileStatusReadError), string(ProfileStatusNoData),
		},
	}
	for name, values := range groups {
		seen := map[string]bool{}
		for _, v := range values {
			if v == "" {
				t.Errorf("%s 枚举出现空字符串", name)
			}
			if seen[v] {
				t.Errorf("%s 枚举值重复: %q", name, v)
			}
			seen[v] = true
		}
	}
	categories := map[string]bool{}
	for _, c := range AllCategories() {
		if c == "" {
			t.Error("类别出现空字符串")
		}
		if categories[string(c)] {
			t.Errorf("类别重复: %q", c)
		}
		categories[string(c)] = true
	}
}
