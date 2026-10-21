package pause

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMeetingListCovers7Apps 验证内置名单覆盖 7 个主流会议应用（US-3 明确要求）。
//
// 【为什么用精确 vendor → bundle 映射而不是 substring】substring 太脆弱：
// 「ding」会误匹配任何含 ding 的 ID（比如未来加一个 "com.example.dingdong"）；
// 显式列一家厂商至少一条精确 bundle ID，加错或漏加都能立刻被这条测试抓住。
func TestMeetingListCovers7Apps(t *testing.T) {
	// vendor → 必须存在的至少一条 bundle ID（大小写不敏感匹配）
	required := map[string]string{
		"zoom":     "us.zoom.xos",
		"teams":    "com.microsoft.teams",
		"webex":    "cisco.webex",
		"tencent":  "com.tencent.meeting",
		"lark":     "com.bytedance.lark",
		"dingtalk": "com.alibaba.dingtalkmac",
		"facetime": "com.apple.facetime",
	}
	have := make(map[string]bool, len(MeetingBundleIDsDarwin))
	for _, id := range MeetingBundleIDsDarwin {
		have[strings.ToLower(id)] = true
	}
	for vendor, want := range required {
		if !have[want] {
			t.Errorf("内置会议名单缺少厂商 %s 的 bundle ID %q；实际名单: %v", vendor, want, MeetingBundleIDsDarwin)
		}
	}
}

// TestIsMeetingApp 验证名单匹配逻辑（大小写不敏感、精确匹配）。
func TestIsMeetingApp(t *testing.T) {
	cases := []struct {
		bundleID string
		want     bool
	}{
		{"us.zoom.xos", true},
		{"com.microsoft.teams", true},
		{"com.apple.FaceTime", true},
		{"com.tencent.meeting", true},
		{"com.bytedance.lark", true},
		{"com.alibaba.DingTalkMac", true},
		{"Cisco.Webex", true},
		// 非会议应用应返回 false。
		{"com.google.Chrome", false},
		{"com.apple.Safari", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsMeetingBundleID(tc.bundleID); got != tc.want {
			t.Errorf("IsMeetingBundleID(%q)=%v, want %v", tc.bundleID, got, tc.want)
		}
	}
}

// TestMeetingDetectorTracksFrontmost 验证 MeetingDetector 按当前前台应用变化。
func TestMeetingDetectorTracksFrontmost(t *testing.T) {
	var frontmost string
	d := NewMeetingDetector(func() string { return frontmost })

	frontmost = ""
	if d.Detect() != ReasonNone {
		t.Error("前台为空时应返回 ReasonNone")
	}

	frontmost = "us.zoom.xos"
	if d.Detect() != ReasonMeeting {
		t.Errorf("Zoom 前台时应返回 ReasonMeeting，实际 %s", d.Detect())
	}

	frontmost = "com.apple.Safari"
	if d.Detect() != ReasonNone {
		t.Error("Safari 前台时应返回 ReasonNone")
	}
}

// TestMeetingDetectorSinceTracksTransition 验证暂停开始时间在切换时重置。
func TestMeetingDetectorSinceTracksTransition(t *testing.T) {
	var frontmost string
	base := time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC)
	seq := []time.Time{base, base.Add(2 * time.Second), base.Add(4 * time.Second)}
	i := 0
	d := NewMeetingDetector(func() string { return frontmost })
	d.now = func() time.Time {
		t := seq[i]
		if i < len(seq)-1 {
			i++
		}
		return t
	}

	// 首次进入会议：since 应记第一次 Detect 的时刻。
	frontmost = "us.zoom.xos"
	d.Detect()
	s1 := d.Since()
	if !s1.Equal(base) {
		t.Errorf("首次进入应记 base 时刻，实际 %v", s1)
	}

	// 保持 Zoom：since 不变。
	d.Detect()
	if s2 := d.Since(); !s2.Equal(s1) {
		t.Errorf("持续同一会议时 since 不应变化：%v → %v", s1, s2)
	}

	// 切走：since 归零。
	frontmost = "com.apple.Safari"
	d.Detect()
	if !d.Since().IsZero() {
		t.Errorf("切走后 since 应归零，实际 %v", d.Since())
	}

	// 再次进入：since 是新时刻。
	frontmost = "com.microsoft.teams"
	d.Detect()
	if s3 := d.Since(); s3.Equal(s1) {
		t.Errorf("重新进入应记新的 since（不应沿用旧值）")
	}
}

// TestMeetingPriorityOverIdle 验证优先级：会议 > 空闲。
//
// 【为什么】「在开会」比「键盘没动」更具体；用户在会议中不会乱动键盘是显然的，
// 空闲信号只是巧合。展示更具体的原因让用户理解为什么没被打扰。
func TestMeetingPriorityOverIdle(t *testing.T) {
	meeting := NewMeetingDetector(func() string { return "us.zoom.xos" })
	idle := NewIdleDetector(func() time.Duration { return 10 * time.Minute }, 5*time.Minute)

	provider := NewCompositeProvider(meeting, idle)
	if s := provider.State(); s.Reason != ReasonMeeting {
		t.Errorf("会议应优先于空闲，实际 %s", s.Reason)
	}
}

// TestMeetingDetectorConcurrentAccess 验证并发读写不产生数据竞争。
func TestMeetingDetectorConcurrentAccess(t *testing.T) {
	d := NewMeetingDetector(func() string {
		if time.Now().UnixNano()%2 == 0 {
			return "us.zoom.xos"
		}
		return "com.apple.Safari"
	})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = d.Detect()
		}()
		go func() {
			defer wg.Done()
			_ = d.Since()
			_ = d.State()
		}()
	}
	wg.Wait()
}
