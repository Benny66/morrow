package sit

import "strings"

// 应用标识（macOS bundle ID / Windows 进程名）→ 行为类别与展示名。
//
// 只收录常见办公/沟通/设计/影音应用；未知一律归 other、展示原始标识。
// 会议应用由调用方用 pause 的会议名单判定后传 isMeeting=true，避免第二份名单。

// morrowBundleID / morrowProcess 是本应用自身标识：采样时排除，不把自己算成工作应用。
const (
	morrowBundleID = "io.github.bennywen.morrow"
	morrowProcess  = "morrow"
)

var codingApps = map[string]struct{}{
	"com.microsoft.vscode": {}, "com.microsoft.vscodeinsiders": {}, "com.apple.dt.xcode": {},
	"com.jetbrains.intellij": {}, "com.jetbrains.goland": {}, "com.jetbrains.pycharm": {},
	"com.jetbrains.webstorm": {}, "com.jetbrains.clion": {}, "com.jetbrains.rider": {},
	"com.sublimetext.4": {}, "com.sublimetext.3": {}, "com.googlecode.iterm2": {},
	"com.apple.terminal": {}, "com.todesktop.230313mzl4w4u92": {}, "dev.warp.warp-stable": {},
	"code": {}, "code-insiders": {}, "cursor": {}, "devenv": {}, "idea64": {}, "goland64": {},
	"pycharm64": {}, "webstorm64": {}, "clion64": {}, "rider64": {}, "sublime_text": {},
	"windowsterminal": {}, "wt": {}, "powershell": {}, "cmd": {},
}

var designApps = map[string]struct{}{
	"com.figma.desktop": {}, "com.adobe.photoshop": {}, "com.adobe.illustrator": {},
	"com.bohemiancoding.sketch3": {}, "com.adobe.xd": {},
	"figma": {}, "photoshop": {}, "illustrator": {}, "sketch": {}, "adobexd": {},
}

var browsingApps = map[string]struct{}{
	"com.google.chrome": {}, "com.apple.safari": {}, "com.microsoft.edgemac": {},
	"org.mozilla.firefox": {}, "com.brave.browser": {}, "com.operasoftware.opera": {},
	"com.vivaldi.vivaldi": {}, "company.thebrowser.browser": {},
	"chrome": {}, "msedge": {}, "firefox": {}, "brave": {}, "opera": {}, "vivaldi": {}, "arc": {},
}

var communicationApps = map[string]struct{}{
	"com.tencent.xinwechat": {}, "com.alibaba.dingtalkmac": {}, "com.bytedance.lark": {},
	"com.bytedance.electron.lark": {}, "com.feishu.lark": {}, "com.tinyspeck.slackmacgap": {},
	"com.hnc.dingtalk": {},
	"wechat":           {}, "weixin": {}, "dingtalk": {}, "feishu": {}, "lark": {}, "slack": {},
	"telegram": {}, "discord": {}, "whatsapp": {},
}

var mediaApps = map[string]struct{}{
	"com.apple.quicktimeplayerx": {}, "com.spotify.client": {}, "com.colliderli.iina": {},
	"org.videolan.vlc": {}, "com.netease.163music": {}, "com.tencent.qqmusic": {},
	"com.apple.music": {}, "com.apple.tv": {},
	"vlc": {}, "potplayermini64": {}, "spotify": {}, "cloudmusic": {}, "qqmusic": {},
	"wmplayer": {}, "mpc-hc64": {},
}

// displayNames 是常见应用的中文/通用展示名；未知应用展示原始标识。
var displayNames = map[string]string{
	"com.microsoft.vscode": "VS Code", "com.microsoft.vscodeinsiders": "VS Code Insiders",
	"com.apple.dt.xcode": "Xcode", "com.jetbrains.intellij": "IntelliJ IDEA",
	"com.jetbrains.goland": "GoLand", "com.jetbrains.pycharm": "PyCharm",
	"com.jetbrains.webstorm": "WebStorm", "com.sublimetext.4": "Sublime Text",
	"com.googlecode.iterm2": "iTerm", "com.apple.terminal": "终端",
	"com.todesktop.230313mzl4w4u92": "Cursor", "dev.warp.warp-stable": "Warp",
	"com.figma.desktop": "Figma", "com.adobe.photoshop": "Photoshop",
	"com.adobe.illustrator": "Illustrator", "com.bohemiancoding.sketch3": "Sketch",
	"com.google.chrome": "Chrome", "com.apple.safari": "Safari",
	"com.microsoft.edgemac": "Edge", "org.mozilla.firefox": "Firefox",
	"com.brave.browser": "Brave", "com.operasoftware.opera": "Opera",
	"com.vivaldi.vivaldi": "Vivaldi", "company.thebrowser.browser": "Arc",
	"com.tencent.xinwechat": "微信", "com.alibaba.dingtalkmac": "钉钉",
	"com.bytedance.lark": "飞书", "com.bytedance.electron.lark": "飞书",
	"com.feishu.lark": "飞书", "com.tinyspeck.slackmacgap": "Slack",
	"us.zoom.xos": "Zoom", "zoom": "Zoom", "com.microsoft.teams": "Teams",
	"com.microsoft.teams2": "Teams", "ms-teams": "Teams",
	"com.apple.facetime": "FaceTime", "com.tencent.meeting": "腾讯会议", "wemeetapp": "腾讯会议",
	"com.spotify.client": "Spotify", "com.colliderli.iina": "IINA",
	"org.videolan.vlc": "VLC", "com.apple.music": "音乐", "com.apple.tv": "Apple TV",
	"code": "VS Code", "cursor": "Cursor", "devenv": "Visual Studio",
	"windowsterminal": "Windows Terminal", "wechat": "微信", "weixin": "微信",
	"dingtalk": "钉钉", "feishu": "飞书", "lark": "飞书", "slack": "Slack",
	"teams": "Teams", "chrome": "Chrome", "msedge": "Edge", "firefox": "Firefox",
	"figma": "Figma", "photoshop": "Photoshop", "vlc": "VLC", "spotify": "Spotify",
}

func normalizedAppID(appID string) string {
	return strings.ToLower(strings.TrimSpace(appID))
}

// IsSelfApp 判断前台应用是否就是 Morrow 自己。
func IsSelfApp(platform, appID string) bool {
	id := normalizedAppID(appID)
	if id == "" {
		return false
	}
	switch platform {
	case "darwin":
		return id == morrowBundleID
	case "windows":
		return id == morrowProcess
	default:
		return false
	}
}

// systemUIAppIDs 是锁屏/登录窗口等系统界面，不是用户的工作应用。
//
// 锁屏 watcher 有最多 1 秒延迟，启动时第一轮采样或锁屏瞬间仍可能读到它们；
// 若不排除，应用明细里会冒出 loginwindow 这类条目，并被归入「其他」。
var systemUIAppIDs = map[string]struct{}{
	"com.apple.loginwindow":        {},
	"com.apple.screensaver.engine": {},
	"lockapp.exe":                  {},
	"logonui.exe":                  {},
}

// IsSystemUIApp 判断前台应用是否是系统登录/锁屏界面。
func IsSystemUIApp(platform, appID string) bool {
	id := normalizedAppID(appID)
	if id == "" {
		return false
	}
	switch platform {
	case "darwin", "windows":
		_, ok := systemUIAppIDs[id]
		return ok
	default:
		return false
	}
}

// CategoryFor 把应用标识映射到行为类别；isMeeting 由 pause 的会议名单判定。
func CategoryFor(platform, appID string, isMeeting bool) Category {
	id := normalizedAppID(appID)
	if id == "" {
		return CategoryOther
	}
	if isMeeting {
		return CategoryCommunication
	}
	switch {
	case inSet(codingApps, id):
		return CategoryCoding
	case inSet(designApps, id):
		return CategoryDesign
	case inSet(browsingApps, id):
		return CategoryBrowsing
	case inSet(communicationApps, id):
		return CategoryCommunication
	case inSet(mediaApps, id):
		return CategoryMedia
	default:
		return CategoryOther
	}
}

// AppDisplayName 返回常见应用展示名；未知回退原始标识。
func AppDisplayName(appID string) string {
	id := normalizedAppID(appID)
	if name, ok := displayNames[id]; ok {
		return name
	}
	return strings.TrimSpace(appID)
}

// categoryLabels 是类别的中文展示名；前端镜像在 frontend/src/lib/sit.ts。
// 宠物记忆文案与久坐页必须使用同一套类别名，迁移时两处一起改。
var categoryLabels = map[Category]string{
	CategoryCoding:        "写代码",
	CategoryDesign:        "做设计",
	CategoryBrowsing:      "浏览",
	CategoryCommunication: "沟通",
	CategoryMedia:         "影音",
	CategoryOther:         "其他",
}

// CategoryLabel 返回类别的中文名；未知值回落原始类别字符串。
func CategoryLabel(c Category) string {
	if label, ok := categoryLabels[c]; ok {
		return label
	}
	return string(c)
}

func inSet(set map[string]struct{}, id string) bool {
	_, ok := set[id]
	return ok
}
