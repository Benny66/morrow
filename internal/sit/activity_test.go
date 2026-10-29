package sit

import (
	"testing"
	"time"
)

// TestActivityFor 覆盖三个档位的边界。
//
// 边界点刻意取「刚好不到 / 刚好等于」，因为这两处最容易在改成 <= 或 < 时静默偏移：
// 偏移一秒不会报错，只会让「停下来想了 20 秒」的人被当成不在场。
func TestActivityFor(t *testing.T) {
	cases := []struct {
		name string
		idle time.Duration
		want ActivityLevel
	}{
		{"零空闲", 0, ActivityBusy},
		{"忙的边界内", ActivityBusyMax - time.Millisecond, ActivityBusy},
		{"忙的边界上", ActivityBusyMax, ActivityRecent},
		{"刚动过", 90 * time.Second, ActivityRecent},
		{"在读代码", ActivityRecentMax - time.Millisecond, ActivityRecent},
		{"离开的边界上", ActivityRecentMax, ActivityAway},
		{"离开很久", 20 * time.Minute, ActivityAway},
		{"时钟异常（负值）", -time.Second, ActivityBusy},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ActivityFor(c.idle); got != c.want {
				t.Fatalf("ActivityFor(%v) = %q，期望 %q", c.idle, got, c.want)
			}
		})
	}
}

// TestActivityThresholdsOrdered 守住阈值之间的序关系与设计前提。
//
// 【为什么要断言 ActivityRecentMax > FocusSwitchGrace】design.md 第 3 节把
// 「3 分钟略宽于 2 分钟的切走宽限」当作选值理由。若哪天 FocusSwitchGrace 被调大，
// 这条理由就不成立了——而症状只是「读代码时宠物姿态开始闪」，没有任何报错。
func TestActivityThresholdsOrdered(t *testing.T) {
	if !(ActivityBusyMax < ActivityRecentMax) {
		t.Fatalf("阈值顺序错误：ActivityBusyMax(%v) 必须小于 ActivityRecentMax(%v)",
			ActivityBusyMax, ActivityRecentMax)
	}
	if ActivityRecentMax <= FocusSwitchGrace {
		t.Fatalf("ActivityRecentMax(%v) 必须严格大于 FocusSwitchGrace(%v)："+
			"否则停在专注应用上读两分钟就会被判为离开，宠物姿态会持续闪烁",
			ActivityRecentMax, FocusSwitchGrace)
	}
	if ActivityRecentMax >= ResetIdle {
		t.Fatalf("ActivityRecentMax(%v) 必须小于 ResetIdle(%v)："+
			"越过它之后采样本身已经停止，再谈档位没有意义",
			ActivityRecentMax, ResetIdle)
	}
}

// TestNewActivity 覆盖「采样没成功」与「成功」两种形状。
func TestNewActivity(t *testing.T) {
	if got := NewActivity("", time.Second); got != nil {
		t.Fatalf("category 为空时必须返回 nil（表示没有活动），实际得到 %+v", got)
	}

	got := NewActivity(CategoryCoding, 0)
	if got == nil {
		t.Fatal("category 非空时必须返回活动信息，实际得到 nil")
	}
	if got.Category != CategoryCoding {
		t.Fatalf("类别 = %q，期望 %q", got.Category, CategoryCoding)
	}
	if got.Level != ActivityBusy {
		t.Fatalf("档位 = %q，期望 %q", got.Level, ActivityBusy)
	}

	// away 仍然要返回非 nil：人不在但类别明确，由前端据档位决定不演姿态。
	// 若这里返回 nil，前端就分不清「没有画像」与「人不在」了。
	if away := NewActivity(CategoryMedia, time.Hour); away == nil || away.Level != ActivityAway {
		t.Fatalf("离开时仍应返回活动信息且档位为 away，实际得到 %+v", away)
	}
}
