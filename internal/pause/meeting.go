package pause

import (
	"strings"
	"sync"
	"time"
)

// MeetingBundleIDsDarwin 是 macOS 上被视为「会议中」的 bundle ID 名单（单一真相）。
//
// 【为什么这份名单必须集中】产品对外承诺「开会时不打扰」，兑现靠的就是这份 ID；
// 名单散落在检测逻辑或前端就等于没有承诺。任何一条 ID 的加入/删除都应当
// 经过 meeting_test.go 的 7 厂商覆盖测试。
//
// 【为什么用精确 ID 而不是模糊匹配】bundle ID 反向域是唯一标识，误配风险最低。
// 用前缀匹配会把 `com.microsoft.teams.something` 这类非会议进程也算进来。
//
// 【浏览器内会议怎么办】Google Meet / Teams Web / 飞书网页版走 Chrome/Safari，
// 无法通过 bundle ID 识别；靠 ticket 004 的全屏检测兜底。产品文案不承诺
// 「所有会议都能识别」。
var MeetingBundleIDsDarwin = []string{
	// Zoom
	"us.zoom.xos",
	// Microsoft Teams（新客户端是 ms-teams，旧版是 com.microsoft.teams）
	"com.microsoft.teams",
	"com.microsoft.teams2",
	// Cisco Webex
	"Cisco.Webex",
	"com.webex.app-macos",
	// 腾讯会议（Wemeet）
	"com.tencent.meeting",
	"wemeet",
	// 飞书 / Lark（国内叫飞书、海外叫 Lark，bundle 名不同）
	"com.bytedance.lark",
	"com.bytedance.electron.lark",
	"com.feishu.lark",
	// 钉钉
	"com.alibaba.DingTalkMac",
	"com.laiwang.DingTalk",
	// Apple FaceTime（系统视频通话）
	"com.apple.FaceTime",
}

// MeetingProcessNamesWindows 是 Windows 上被视为「会议中」的进程名（小写、不含 .exe）。
//
// 与 Darwin 保持同一厂商覆盖；Windows 上进程名与 bundle ID 语义等价，
// 只是形态不同（不含反向域前缀）。
var MeetingProcessNamesWindows = []string{
	"zoom",
	"teams",      // 旧 Teams
	"ms-teams",   // 新 Teams（WebView2 版本）
	"ciscowebex", // Webex
	"webex",
	"wemeetapp", // 腾讯会议
	"feishu",    // 飞书国内版
	"lark",      // Lark 海外版
	"dingtalk",  // 钉钉
	"idingtalk",
	"facetime", // Windows 无原生 FaceTime，留空占位保持厂商覆盖对称
}

// meetingBundleSetDarwin 是 bundle ID 的快速查表集合（小写）。
//
// 【为什么单独建表】MeetBundleIDsDarwin 是给「覆盖 7 厂商」测试读的名单；
// 匹配路径需要 O(1) 查表，不能每次 Detect 都遍历一次 slice。
// 大小写不敏感：Cisco.Webex 这种 ID 有大写、bundle 大小写不严格统一，
// 匹配前小写化一次最稳。
var meetingBundleSetDarwin = buildSet(MeetingBundleIDsDarwin)

// meetingProcessSetWindows 是 Windows 进程名的查表集合（小写、不含 .exe）。
var meetingProcessSetWindows = buildSet(MeetingProcessNamesWindows)

func buildSet(list []string) map[string]struct{} {
	m := make(map[string]struct{}, len(list))
	for _, s := range list {
		m[strings.ToLower(s)] = struct{}{}
	}
	return m
}

// IsMeetingBundleID 判断给定的 macOS bundle ID 是否在会议名单中。
func IsMeetingBundleID(bundleID string) bool {
	if bundleID == "" {
		return false
	}
	_, ok := meetingBundleSetDarwin[strings.ToLower(bundleID)]
	return ok
}

// IsMeetingProcessName 判断给定的 Windows 进程名是否在会议名单中。
//
// 输入应是小写、不含 .exe 的进程名（platform 层负责规范化）。
func IsMeetingProcessName(name string) bool {
	if name == "" {
		return false
	}
	_, ok := meetingProcessSetWindows[strings.ToLower(name)]
	return ok
}

// MeetingDetector 是会议应用前台检测器。
//
// 平台层通过 getFrontmost 回调返回当前前台应用的标识：
//   - macOS：bundleIdentifier（如 "us.zoom.xos"）
//   - Windows：进程名（小写、不含 .exe，如 "zoom"）
//
// 名单匹配用 IsMeetingBundleID / IsMeetingProcessName 都能通过（两者是不同
// 的查表集合但都是精确匹配）；本 Detector 只按「getFrontmost 的返回值是否
// 命中任一名单」判定，不关心平台怎么取到这个名字。
type MeetingDetector struct {
	// getFrontmost 返回当前前台应用标识；空串表示无前台或读取失败。
	getFrontmost func() string

	mu    sync.Mutex
	inHit bool      // 上一次 Detect 是否命中
	since time.Time // 进入会议状态的时刻
	now   func() time.Time
}

// NewMeetingDetector 构造会议检测器。
func NewMeetingDetector(getFrontmost func() string) *MeetingDetector {
	return &MeetingDetector{
		getFrontmost: getFrontmost,
		now:          time.Now,
	}
}

// Detect 返回当前会议状态对应的暂停原因。
//
// 命中 → ReasonMeeting；否则 → ReasonNone。
//
// 【为什么这里做状态转换而不是让调用方维护】since 记录的是「何时进入会议」，
// 只有本 Detector 自己知道上一次的结果，跨调用记录状态是它职责的一部分。
// 让调用方维护会把这个知识散到每个使用点，改代码时容易漏。
func (d *MeetingDetector) Detect() PauseReason {
	if d.getFrontmost == nil {
		return ReasonNone
	}
	id := d.getFrontmost()
	hit := IsMeetingBundleID(id) || IsMeetingProcessName(id)

	d.mu.Lock()
	defer d.mu.Unlock()
	if hit && !d.inHit {
		d.inHit = true
		d.since = d.now()
	} else if !hit && d.inHit {
		d.inHit = false
		d.since = time.Time{}
	}
	if hit {
		return ReasonMeeting
	}
	return ReasonNone
}

// Since 返回进入会议状态的时刻；未触发时为零值。
func (d *MeetingDetector) Since() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.since
}

// State 让 MeetingDetector 满足 Provider 接口。
func (d *MeetingDetector) State() State {
	if d.Detect() == ReasonNone {
		return State{Reason: ReasonNone}
	}
	return State{Reason: ReasonMeeting, Since: d.Since()}
}
