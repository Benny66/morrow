package sit

import (
	"testing"
	"time"
)

func at(min int) time.Time {
	return time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local).Add(time.Duration(min) * time.Minute)
}

func TestDecideNudgeThresholdAndGap(t *testing.T) {
	base := NudgeInput{
		Now:              at(60),
		Continuous:       60 * time.Minute,
		ThresholdMinutes: 60,
		SessionStartedAt: at(0),
		Eligible:         true,
	}
	if d := DecideNudge(base); !d.Nudge {
		t.Fatalf("60 分钟应提醒，得到 %+v", d)
	}
	below := base
	below.Continuous = 59 * time.Minute
	if d := DecideNudge(below); d.Nudge {
		t.Fatal("未到阈值不应提醒")
	}
	closePlan := base
	closePlan.NextStandMoveAt = at(70).Unix()
	if d := DecideNudge(closePlan); d.Nudge {
		t.Fatal("15 分钟内有 stand/move，不应抢在正式提醒前冒泡")
	}
	farPlan := base
	farPlan.NextStandMoveAt = at(80).Unix()
	if d := DecideNudge(farPlan); !d.Nudge {
		t.Fatalf("20 分钟外无正式提醒，应冒泡，得到 %+v", d)
	}
}

func TestDecideNudgeBudgetAndSessionLimit(t *testing.T) {
	base := NudgeInput{
		Now:              at(60),
		Continuous:       60 * time.Minute,
		ThresholdMinutes: 60,
		SessionStartedAt: at(0),
		Eligible:         true,
	}
	noBudget := base
	noBudget.DailyProactive = ProactiveBudget
	if d := DecideNudge(noBudget); d.Nudge {
		t.Fatal("每日预算用尽不应提醒")
	}
	limit := base
	limit.SessionNudges = MaxNudgesPerSession
	if d := DecideNudge(limit); d.Nudge {
		t.Fatal("会话次数用尽不应提醒")
	}
	notEligible := base
	notEligible.Eligible = false
	if d := DecideNudge(notEligible); d.Nudge {
		t.Fatal("Eligible=false 不应提醒")
	}
}

func TestDecideNudgeSecondAndGap(t *testing.T) {
	base := NudgeInput{
		Continuous:       120 * time.Minute,
		ThresholdMinutes: 60,
		SessionStartedAt: at(0),
		SessionNudges:    1,
		FirstNudgeAckAt:  at(60).Unix(),
		LastProactiveAt:  at(60).Unix(),
		Eligible:         true,
	}
	base.Now = at(89)
	if d := DecideNudge(base); d.Nudge {
		t.Fatal("90 分钟前不应出现第二条")
	}
	base.Now = at(90)
	if d := DecideNudge(base); !d.Nudge {
		t.Fatalf("90 分钟应出现第二条，得到 %+v", d)
	}
	late := base
	late.FirstNudgeAckAt = at(75).Unix()
	late.LastProactiveAt = at(75).Unix()
	late.Now = at(95)
	if d := DecideNudge(late); d.Nudge {
		t.Fatal("首条晚到 75 分钟时，第二条应等到 105 分钟")
	}
	late.Now = at(105)
	if d := DecideNudge(late); !d.Nudge {
		t.Fatal("首展后 30 分钟应出现第二条")
	}
}

func TestDecideNudgeSnoozeBypassesProactiveGap(t *testing.T) {
	base := NudgeInput{
		Now:              at(70),
		Continuous:       70 * time.Minute,
		ThresholdMinutes: 60,
		SessionStartedAt: at(0),
		SessionNudges:    1,
		FirstNudgeAckAt:  at(60).Unix(),
		LastProactiveAt:  at(60).Unix(),
		SnoozeUntil:      at(70).Unix(),
		Eligible:         true,
	}
	if d := DecideNudge(base); !d.Nudge || d.Reason != "snooze_due" {
		t.Fatalf("用户点再坐 10 分钟后应在 10 分钟时允许追问，得到 %+v", d)
	}
	base.SessionNudges = MaxNudgesPerSession
	if d := DecideNudge(base); d.Nudge {
		t.Fatal("会话次数用尽后，再坐 10 分钟也不应再提醒")
	}
	base.SessionNudges = 1
	base.Now = at(69)
	if d := DecideNudge(base); d.Nudge {
		t.Fatal("10 分钟未到不应提醒")
	}
}

func TestDecideMemoryBudgetGapAndPlan(t *testing.T) {
	base := MemoryInput{
		Now:             at(60),
		Eligible:        true,
		HasQuote:        true,
		LastProactiveAt: at(30).Unix(),
	}
	if d := DecideMemory(base); !d.Speak {
		t.Fatalf("自然停顿且有事实应冒记忆气泡，得到 %+v", d)
	}
	noFact := base
	noFact.HasQuote = false
	if d := DecideMemory(noFact); d.Speak || d.Reason != "no_fact" {
		t.Errorf("没有事实不应冒泡，得到 %+v", d)
	}
	notEligible := base
	notEligible.Eligible = false
	if d := DecideMemory(notEligible); d.Speak {
		t.Errorf("非自然停顿/专注中不应冒泡，得到 %+v", d)
	}
	exhausted := base
	exhausted.DailyProactive = ProactiveBudget
	if d := DecideMemory(exhausted); d.Speak || d.Reason != "daily_budget" {
		t.Errorf("每日预算用尽不应冒泡，得到 %+v", d)
	}
	gap := base
	gap.LastProactiveAt = at(31).Unix()
	if d := DecideMemory(gap); d.Speak || d.Reason != "proactive_gap" {
		t.Errorf("30 分钟内不应冒泡，得到 %+v", d)
	}
	plan := base
	plan.NextStandMoveAt = at(74).Unix()
	if d := DecideMemory(plan); d.Speak || d.Reason != "plan_too_close" {
		t.Errorf("15 分钟内有正式提醒不应抢跑，得到 %+v", d)
	}
	farPlan := base
	farPlan.NextStandMoveAt = at(80).Unix()
	if d := DecideMemory(farPlan); !d.Speak {
		t.Errorf("20 分钟外的正式提醒不阻止记忆气泡，得到 %+v", d)
	}
}

func TestShouldCreditLock(t *testing.T) {
	cases := []struct {
		name string
		lock time.Duration
		sit  time.Duration
		want bool
	}{
		{"29 分钟锁 10 分钟", 10 * time.Minute, 29 * time.Minute, false},
		{"31 分钟锁 4 分钟", 4 * time.Minute, 31 * time.Minute, false},
		{"31 分钟锁 6 分钟", 6 * time.Minute, 31 * time.Minute, true},
		{"30 分钟锁 5 分钟（边界）", 5 * time.Minute, 30 * time.Minute, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldCreditLock(c.lock, c.sit); got != c.want {
				t.Errorf("ShouldCreditLock(%v,%v) = %v，期望 %v", c.lock, c.sit, got, c.want)
			}
		})
	}
}

func TestDecideNudgeProactiveGap(t *testing.T) {
	base := NudgeInput{
		Now:              at(60),
		Continuous:       60 * time.Minute,
		ThresholdMinutes: 60,
		SessionStartedAt: at(0),
		LastProactiveAt:  at(50).Unix(),
		Eligible:         true,
	}
	if d := DecideNudge(base); d.Nudge {
		t.Fatal("距上次主动发言不足 30 分钟不应提醒")
	}
	base.Now = at(80)
	if d := DecideNudge(base); !d.Nudge {
		t.Fatal("满 30 分钟后应允许提醒")
	}
}
