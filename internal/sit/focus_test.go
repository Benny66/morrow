package sit

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func focusAt(tr *Tracker, at time.Time, appID string, category Category) {
	tr.ObserveFocus(FocusInput{Now: at, Enabled: true, AppID: appID, Category: category})
}

func TestFocusRequiresTenMinutesSameApp(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	tr := NewTracker()
	observe(tr, start, 0, true)

	focusAt(tr, start, "com.apple.dt.Xcode", CategoryCoding)
	if got := tr.Focus(); got != nil {
		t.Fatalf("刚开始不应算专注，得到 %+v", got)
	}
	focusAt(tr, start.Add(9*time.Minute), "com.apple.dt.Xcode", CategoryCoding)
	if got := tr.Focus(); got != nil {
		t.Fatalf("9 分钟不应算专注，得到 %+v", got)
	}
	focusAt(tr, start.Add(10*time.Minute), "com.apple.dt.Xcode", CategoryCoding)
	got := tr.Focus()
	if got == nil || got.Category != CategoryCoding || got.Minutes != 10 {
		t.Fatalf("10 分钟应为 coding 专注 10 分钟，得到 %+v", got)
	}
	focusAt(tr, start.Add(40*time.Minute), "com.apple.dt.Xcode", CategoryCoding)
	if got = tr.Focus(); got == nil || got.Minutes != 40 {
		t.Fatalf("40 分钟专注分钟数 = %+v，期望 40", got)
	}
	if snap := tr.Snapshot(); snap.ContinuousMinutes != 0 {
		t.Errorf("专注不应推进久坐时钟，ContinuousMinutes = %d", snap.ContinuousMinutes)
	}
}

func TestFocusShortSwitchGraceAndGraceExpiry(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	tr := NewTracker()
	observe(tr, start, 0, true)
	focusAt(tr, start, "com.apple.dt.Xcode", CategoryCoding)
	focusAt(tr, start.Add(10*time.Minute), "com.apple.dt.Xcode", CategoryCoding)

	// 切到浏览器 90 秒：不打断，离开时间不计入专注分钟。
	focusAt(tr, start.Add(10*time.Minute), "com.google.Chrome", CategoryBrowsing)
	focusAt(tr, start.Add(11*time.Minute+30*time.Second), "com.apple.dt.Xcode", CategoryCoding)
	if got := tr.Focus(); got == nil || got.Minutes != 10 {
		t.Fatalf("90 秒切走后应继续专注且仍为 10 分钟，得到 %+v", got)
	}
	focusAt(tr, start.Add(13*time.Minute), "com.apple.dt.Xcode", CategoryCoding)
	if got := tr.Focus(); got == nil || got.Minutes != 11 {
		t.Fatalf("回到原应用后应继续累计，得到 %+v", got)
	}

	// 切到另一类别并停留 3 分钟：超过 120 秒宽限，原专注结束。
	focusAt(tr, start.Add(13*time.Minute), "com.google.Chrome", CategoryBrowsing)
	focusAt(tr, start.Add(16*time.Minute), "com.google.Chrome", CategoryBrowsing)
	if got := tr.Focus(); got != nil {
		t.Fatalf("切走 3 分钟后原专注应结束，得到 %+v", got)
	}
}

func TestFocusEndsOnBreakProfileOffAndCorrection(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	tr := NewTracker()
	observe(tr, start, 0, true)
	focusAt(tr, start, "com.apple.dt.Xcode", CategoryCoding)
	focusAt(tr, start.Add(10*time.Minute), "com.apple.dt.Xcode", CategoryCoding)

	// 用户纠正类别后，同一应用的专注展示跟随。
	focusAt(tr, start.Add(11*time.Minute), "com.apple.dt.Xcode", CategoryDesign)
	if got := tr.Focus(); got == nil || got.Category != CategoryDesign {
		t.Fatalf("纠正后专注类别 = %+v，期望 design", got)
	}

	// 主动休息是真实离开：专注立即结束。
	res := tr.StartBreak(start.Add(12 * time.Minute))
	if res.Snapshot.Focus != nil {
		t.Fatalf("主动休息后快照仍带专注: %+v", res.Snapshot.Focus)
	}
	tr.ObserveFocus(FocusInput{Now: start.Add(13 * time.Minute), Enabled: true, AppID: "com.apple.dt.Xcode", Category: CategoryDesign})
	if got := tr.Focus(); got != nil {
		t.Fatalf("休息中不应恢复专注，得到 %+v", got)
	}

	// 画像关闭：清空专注。
	observe(tr, start.Add(20*time.Minute), 0, true)
	focusAt(tr, start.Add(20*time.Minute), "com.apple.dt.Xcode", CategoryCoding)
	focusAt(tr, start.Add(31*time.Minute), "com.apple.dt.Xcode", CategoryCoding)
	if tr.Focus() == nil {
		t.Fatal("重新 tracking 后应重新累计专注")
	}
	tr.ObserveFocus(FocusInput{Now: start.Add(32 * time.Minute), Enabled: false})
	if got := tr.Focus(); got != nil {
		t.Fatalf("画像关闭后专注应为 nil，得到 %+v", got)
	}
}

func TestFocusSnapshotCarriesNoAppIdentity(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	tr := NewTracker()
	observe(tr, start, 0, true)
	focusAt(tr, start, "com.example.secret-app", CategoryCommunication)
	focusAt(tr, start.Add(11*time.Minute), "com.example.secret-app", CategoryCommunication)

	focus := tr.Focus()
	if focus == nil {
		t.Fatal("应处于专注态")
	}
	raw, err := json.Marshal(focus)
	if err != nil {
		t.Fatalf("序列化专注失败: %v", err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "com.example") ||
		strings.Contains(strings.ToLower(string(raw)), "secret-app") {
		t.Errorf("专注快照泄露了应用标识: %s", raw)
	}
	if !strings.Contains(string(raw), `"category":"communication"`) {
		t.Errorf("专注快照缺少类别: %s", raw)
	}
}

func TestSuppressesBubbleDuringFocus(t *testing.T) {
	focus := &Focus{Category: CategoryCoding, Minutes: 20}
	if !SuppressesBubble(focus, BubbleNudge) || !SuppressesBubble(focus, BubbleMemory) {
		t.Error("专注期间应抑制软提醒与记忆气泡")
	}
	if SuppressesBubble(focus, BubbleGreeting) {
		t.Error("问候不应被专注抑制")
	}
	if SuppressesBubble(nil, BubbleNudge) {
		t.Error("无专注时不应抑制")
	}
}

func TestDecideNudgeSuppressedDuringFocus(t *testing.T) {
	base := NudgeInput{
		Now:              at(70),
		Continuous:       70 * time.Minute,
		ThresholdMinutes: 60,
		SessionStartedAt: at(0),
		Eligible:         true,
		FocusActive:      true,
	}
	if d := DecideNudge(base); d.Nudge || d.Reason != "focus" {
		t.Fatalf("专注期间应抑制软提醒，得到 %+v", d)
	}
	base.FocusActive = false
	if d := DecideNudge(base); !d.Nudge {
		t.Fatalf("切出专注后满足条件应提醒，得到 %+v", d)
	}
}
