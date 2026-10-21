package restmode

import (
	"testing"
	"time"
)

// TestWarningTransitionsToCountdown 验证 Warning → Countdown 的正常路径。
//
// 【产品语义】60 秒预警告诉用户「马上要休息了」，到点后引擎把状态推进到
// Countdown，覆盖层出现。中间不能有旁路：Idle → Countdown 也允许（引擎
// tick 恰好错过 60 秒窗口时），但 Warning 只能进 Countdown 或 Cancel。
func TestWarningTransitionsToCountdown(t *testing.T) {
	m := NewMachine()
	req := RestRequest{FireID: 1, Kind: "stand", Duration: 120 * time.Second, Title: "站起来"}
	if !m.EnterWarning(req) {
		t.Fatal("Idle → Warning 应成功")
	}
	if m.State() != StateWarning {
		t.Errorf("EnterWarning 后应为 Warning，实际 %s", m.State())
	}
	// 到点：从 Warning 进入 Countdown。
	if !m.Enter(req) {
		t.Error("Warning → Countdown 应成功")
	}
	if m.State() != StateCountdown {
		t.Errorf("Enter 后应为 Countdown，实际 %s", m.State())
	}
}

// TestWarningRejectedWhenCountdownActive 验证休息中不接受新的预警。
//
// 【为什么】用户正在做 stand 拉伸，中间收到 move 预警 → 气泡闪一下、菜单栏
// 文案变成 3 分钟后的 move，会打断当前的呼吸节奏；产品明确「一条休息做完
// 才谈下一条」。
func TestWarningRejectedWhenCountdownActive(t *testing.T) {
	m := NewMachine()
	m.Enter(RestRequest{FireID: 1, Kind: "stand", Duration: 120 * time.Second})
	if m.EnterWarning(RestRequest{FireID: 2, Kind: "move", Duration: 180 * time.Second}) {
		t.Error("Countdown 中 EnterWarning 应返回 false")
	}
	if m.State() != StateCountdown {
		t.Errorf("状态应保持 Countdown，实际 %s", m.State())
	}
}

// TestWarningCancelReturnsToIdle 验证预警可以被取消（比如提醒项被删/停用）。
//
// 【为什么需要】引擎扫描下一个点时如果该点已被处理（比如提醒项 Enabled=false），
// 之前发出的预警应该主动撤销，否则宠物会一直挂着「1 分钟后休息」的假信号。
func TestWarningCancelReturnsToIdle(t *testing.T) {
	m := NewMachine()
	m.EnterWarning(RestRequest{FireID: 1, Kind: "rest", Duration: 60 * time.Second})
	if !m.CancelWarning() {
		t.Error("Warning 状态下 CancelWarning 应返回 true")
	}
	if m.State() != StateIdle {
		t.Errorf("Cancel 后应回到 Idle，实际 %s", m.State())
	}
}

// TestCancelWarningIsNoopOutsideWarningState 验证非 Warning 状态下取消是 no-op。
func TestCancelWarningIsNoopOutsideWarningState(t *testing.T) {
	m := NewMachine()
	if m.CancelWarning() {
		t.Error("Idle 状态下 CancelWarning 应返回 false")
	}
	m.Enter(RestRequest{FireID: 1, Kind: "stand", Duration: 120 * time.Second})
	if m.CancelWarning() {
		t.Error("Countdown 状态下 CancelWarning 应返回 false")
	}
	if m.State() != StateCountdown {
		t.Errorf("Countdown 状态不应被 CancelWarning 破坏，实际 %s", m.State())
	}
}

// TestRemainingDuringWarningReflectsPreAlarm 验证 Warning 状态下 Remaining 是
// 「距离 fire 还有多久」而不是休息时长（后者是 Countdown 的语义）。
//
// 【为什么要单独测】两种 Remaining 语义不同，UI 都取 snapshot.RemainingSec；
// 若 Countdown 与 Warning 用同一份计算逻辑，Warning 期间会显示 120 而不是
// 60 → 30 → 0，用户以为还要休息 120 秒。
func TestRemainingDuringWarningReflectsPreAlarm(t *testing.T) {
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	clock := base
	m := NewMachine()
	m.now = func() time.Time { return clock }

	// 预警发起：fire 计划 60 秒后（PlannedFireAt 是引擎写入的真实 fire 时刻）。
	fireAt := base.Add(WarningLeadTime).Unix()
	m.EnterWarning(RestRequest{
		FireID: 1, Kind: "stand", Duration: 120 * time.Second,
		PlannedFireAt: fireAt,
	})
	if r := m.Remaining(); r != WarningLeadTime {
		t.Errorf("刚进入 Warning 时 Remaining 应为 %v，实际 %v", WarningLeadTime, r)
	}

	clock = base.Add(20 * time.Second)
	if r := m.Remaining(); r != 40*time.Second {
		t.Errorf("过 20 秒后 Warning Remaining 应为 40s，实际 %v", r)
	}

	clock = base.Add(61 * time.Second)
	// 预警超期：夹到 0；引擎下一轮 tick 会调 Enter 转 Countdown 或 CancelWarning。
	if r := m.Remaining(); r != 0 {
		t.Errorf("Warning 超期后 Remaining 应夹到 0，实际 %v", r)
	}
}

// TestSnapshotIncludesWarningState 验证 Snapshot 能透出 Warning 阶段的文案。
//
// 【为什么关键】Pet.vue 与菜单栏都通过 Snapshot 拿状态；Snapshot 里若没有
// Warning 分支，前端就永远看不到预警文案。
func TestSnapshotIncludesWarningState(t *testing.T) {
	m := NewMachine()
	m.EnterWarning(RestRequest{
		FireID: 1, Kind: "stand", Duration: 120 * time.Second, Title: "该起身了",
	})
	snap := m.Snapshot()
	if snap.State != StateWarning {
		t.Errorf("Snapshot state 应为 warning，实际 %s", snap.State)
	}
	if snap.Title != "该起身了" {
		t.Errorf("Snapshot title 应透传，实际 %q", snap.Title)
	}
	// Warning 阶段的 cue_text 是预警文案（不是休息引导动作），前端直接渲染。
	if snap.CueText == "" {
		t.Error("Warning 阶段的 CueText 应给出预警文案而不是空串")
	}
}

// TestWarningLeadTimeIsSixtySeconds 验证 60 秒预警时长与 spec 表格一致。
//
// 【为什么单独测】这个值在 spec、Alert 文案、tray 提示里都出现过；一处漂移
// 会让「1 分钟后...」文案与实际倒计时不匹配。
func TestWarningLeadTimeIsSixtySeconds(t *testing.T) {
	if WarningLeadTime != 60*time.Second {
		t.Errorf("WarningLeadTime 应为 60s，实际 %v", WarningLeadTime)
	}
}
