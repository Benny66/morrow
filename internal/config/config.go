// Package config 负责应用本地配置（config.json）的读写。
//
// 配置落在应用数据目录（见 internal/appdir，与数据库/日志同目录）。
// 首次启动无配置文件时写入默认值。
package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"morrow/internal/ai"
	"morrow/internal/appdir"
	"morrow/internal/model"
	"morrow/internal/reminder"
	"morrow/internal/sit"
)

// 窗口尺寸的默认值与合法范围（单一真相，config 与护栏共同引用）。
const (
	// DefaultWindowWidth / DefaultWindowHeight 首次启动或配置不可用时的窗口尺寸。
	DefaultWindowWidth  = 1024
	DefaultWindowHeight = 768

	// MinWindowWidth / MinWindowHeight 窗口尺寸下界。
	// 配置是用户可编辑的文件，手改坏了不能把窗口弄成不可用（见 WindowSize）。
	MinWindowWidth  = 640
	MinWindowHeight = 420
)

// 提醒与 AI 的默认值。
//
// 默认端点与模型名不在此另写一份：单一真相在 internal/ai（DefaultBaseURL /
// DefaultModel），config 只负责把它们放进默认配置。
const (
	// DefaultQuietStart / DefaultQuietEnd 默认静默时段（22:00–08:00，跨午夜）。
	// 健康提醒的价值在于"别在睡觉时响"，故默认值取大多数人已经入睡的区间。
	DefaultQuietStart = "22:00"
	// DefaultQuietEnd 见 DefaultQuietStart（终点取开区间：08:00 就允许响）。
	DefaultQuietEnd = "08:00"
	// DefaultSnoozeMinutes 默认推迟分钟数。
	DefaultSnoozeMinutes = 5
	// MinSnoozeMinutes / MaxSnoozeMinutes 推迟分钟数的合法范围。
	MinSnoozeMinutes = 1
	// MaxSnoozeMinutes 见 MinSnoozeMinutes。
	MaxSnoozeMinutes = 120
	// DefaultNaturalPauseMaxSeconds 自然停顿顺延的默认上限（60 秒）；0 表示关闭。
	DefaultNaturalPauseMaxSeconds = 60

	// DefaultPauseIdleMinutes 默认空闲判定阈值（5 分钟）。
	// 健康提醒的价值在于"别在离开时打扰"，5 分钟是"去倒杯水/上个洗手间"的典型时长。
	DefaultPauseIdleMinutes = 5
	// MinPauseIdleMinutes / MaxPauseIdleMinutes 空闲阈值的合法范围。
	MinPauseIdleMinutes = 3
	// MaxPauseIdleMinutes 见 MinPauseIdleMinutes。
	MaxPauseIdleMinutes = 10
)

// 桌面宠物的默认值与合法范围。
//
// 与窗口尺寸同理：配置是用户可手改的文件，取值必须经 PetScaleValue / PetPosition
// 夹取，否则一个手改坏的 config.json 会把宠物缩成看不见的一点、
// 或者把它摆到屏幕之外（那时用户既看不到宠物、也想不到要去改配置）。
const (
	// DefaultPetX / DefaultPetY 首次启动（位置未记录）时宠物窗口的落点。
	// 取左上角稍稍内缩的位置：既避开标题栏，又不至于压在 Dock/任务栏上。
	DefaultPetX = 40
	DefaultPetY = 120
	// DefaultPetScale 默认缩放（1.0 = 精灵图原始尺寸 192×208）。
	DefaultPetScale = 1.0
	// MinPetScale / MaxPetScale 缩放的合法范围。
	MinPetScale = 0.5
	// MaxPetScale 见 MinPetScale。
	MaxPetScale = 2.0
	// DefaultPetPackID 默认宠物包：内置的程序化绘制形象（不依赖任何素材文件）。
	// 该 ID 也是 internal/service 里内置素材包的 ID——两处必须一致，
	// 故 service 侧直接引用本常量，不另写一份字面量。
	DefaultPetPackID = "builtin"
)

// Config 是应用配置的持久化结构。
// 字段带 json tag，新增配置项在此扩展并给默认值。
type Config struct {
	// Theme 界面主题：dark / light。空串（""）表示"未设置"，语义为"跟随系统"。
	Theme string `json:"theme"`

	// OnboardingCompleted 首次引导是否已完成（完成或跳过都置 true）。
	// 旧版 config.json 缺该键时为零值 false；是否真正展示由
	// App.ShouldShowOnboarding 结合「有没有场景」判断：已有场景的升级用户
	// 会被自动补上标记，不会被迫过一遍引导。
	OnboardingCompleted bool `json:"onboarding_completed"`

	// WindowWidth / WindowHeight 上次关闭时的窗口尺寸。
	// 0 表示"未记录"（首次启动），此时取默认值。
	WindowWidth  int `json:"window_width"`
	WindowHeight int `json:"window_height"`

	// WindowMaximised 上次关闭时窗口是否处于最大化。true 时启动直接最大化，
	// 尺寸字段仍保留（还原后回到该尺寸）。
	WindowMaximised bool `json:"window_maximised"`

	// ShortcutToggleWindow 「唤起/隐藏主窗口」的全局快捷键（v3 accelerator 语法，
	// 如 "CmdOrCtrl+Shift+Space"）。空串 = 不注册。
	ShortcutToggleWindow string `json:"shortcut_toggle_window"`

	// ShortcutPresentation 切换演示模式的全局快捷键（v3 accelerator 语法）。
	// 空串 = 不注册。与唤起主窗快捷键冲突时注册失败，设置页如实显示。
	ShortcutPresentation string `json:"shortcut_presentation"`

	// MCPToken 运行时 MCP 服务的 Bearer token。仅 `-tags mcp` 构建且 WAILS_MCP=1
	// 时有意义：非空则所有 MCP 请求（含 loopback）都要求鉴权（见 main.go 的透传）。
	MCPToken string `json:"mcp_token"`

	// AIBaseURL OpenAI 兼容端点（用于一次性生成提醒计划）。空串按 ai.DefaultBaseURL 处理。
	AIBaseURL string `json:"ai_base_url"`

	// AIModel 模型名。空串按 ai.DefaultModel 处理。
	AIModel string `json:"ai_model"`

	// AIAPIKey 接口密钥。
	//
	// 【为什么可以明文存在本地配置文件里】它是一个本机桌面应用调用自家 API Key 的场景，
	// 而钥匙串（Keychain）集成需要 cgo 与签名，代价远大于收益；真正的防线是
	// 「密钥绝不出现在日志、错误详情与前端返回值里」——这三条分别在 ai 包与
	// App 的绑定方法里落实（见 ai.PublicSettings 与 ai.MaskKey）。
	AIAPIKey string `json:"ai_api_key"`

	// QuietStart / QuietEnd 静默时段的起止钟点（HH:MM，允许跨午夜，如 22:00–08:00）。
	// 两者相等表示不启用静默时段。
	QuietStart string `json:"quiet_start"`
	QuietEnd   string `json:"quiet_end"`

	// SnoozeMinutes 用户点「推迟」后再提醒的分钟数。
	SnoozeMinutes int `json:"snooze_minutes"`

	// NotifyEnabled 是否发系统通知。关闭后提醒仍然记录、仍然进待处理卡片——
	// 这是「只想在应用内看进度」的用户要的行为，而不是把提醒功能整个停掉。
	NotifyEnabled bool `json:"notify_enabled"`

	// StrongAlertEnabled 是否在到点时弹【全屏强提醒】（铺满主屏工作区的半透明遮罩）。
	//
	// 【为什么与 NotifyEnabled 分开】两者解决的是不同的问题：系统通知容易被勿扰模式
	// 与通知中心折叠吞掉，而全屏遮罩是"必须处理才能消失"的打断。用户可能想关掉其中
	// 一个而留着另一个（例如"别发通知，但到点就占满屏幕"）。
	// 关闭后提醒仍然记录、仍然进待处理卡片——只是不再占屏。
	StrongAlertEnabled bool `json:"strong_alert_enabled"`

	// PresentationSuggestEnabled 是否在会议/全屏时由宠物建议进入演示模式。
	PresentationSuggestEnabled bool `json:"presentation_suggest_enabled"`

	// NaturalPauseEnabled 是否在用户输入时顺延非一次性提醒。
	NaturalPauseEnabled bool `json:"natural_pause_enabled"`

	// NaturalPauseMaxSeconds 顺延上限秒数：0 / 30 / 60；其他值回落默认 60。
	NaturalPauseMaxSeconds int `json:"natural_pause_max_seconds"`

	// PauseIdleEnabled 是否启用空闲暂停（连续 N 分钟无键鼠输入时自动暂停提醒）。
	// 默认开启：这是「智能免打扰」的核心能力，关掉等于回到"到点就响"的原始模式。
	PauseIdleEnabled bool `json:"pause_idle_enabled"`

	// PauseIdleMinutes 空闲判定的阈值分钟数（默认 5 分钟）。
	// 合法范围：3/5/10 分钟；越界回落默认值。
	PauseIdleMinutes int `json:"pause_idle_minutes"`

	// PauseLockEnabled 是否启用锁屏/休眠暂停（屏幕锁定或系统休眠时自动暂停提醒）。
	// 默认开启：与空闲一样属于「智能免打扰」的核心能力。
	PauseLockEnabled bool `json:"pause_lock_enabled"`

	// PauseMeetingEnabled 是否启用会议应用前台暂停（Zoom/Teams/飞书等在前台时暂停）。
	// 默认开启：这是「智能免打扰」里最贴近用户痛点的信号——开会时被弹窗打扰
	// 是竞品分析里反馈最集中的一条。
	PauseMeetingEnabled bool `json:"pause_meeting_enabled"`

	// PauseFullscreenEnabled 是否启用全屏窗口暂停（前台窗口铺满当前屏幕时暂停）。
	//
	// 与空闲/锁屏/会议不同，这一项默认【关闭】：macOS 上首次开启会弹屏幕录制权限
	// 请求，用户没准备好被问权限时默认开着会被视为「装完就要我问权限」的侵略感。
	// ADR 0005 的「默认关闭、显式开启」同样适用这一项。
	PauseFullscreenEnabled bool `json:"pause_fullscreen_enabled"`

	// ScreenRecordingPermissionAsked 记录是否【已经问过】屏幕录制授权。
	//
	// 【为什么需要这个字段】macOS 上第二次调用 CGRequestScreenCaptureAccess 不再弹原生
	// 授权窗，只会跳到系统设置页；用户拒绝过一次后如果每次开关都触发，体验非常差。
	// 这个字段一旦为 true，UI 层就不再弹说明窗，只提示「请到系统设置里手动开启」。
	ScreenRecordingPermissionAsked bool `json:"screen_recording_permission_asked"`

	// PauseFocusEnabled 是否启用系统专注联动（macOS INFocusStatusCenter）。
	// 默认关闭：P1 敏感权限（ADR 0005）。开启时会走一次授权说明窗。
	PauseFocusEnabled bool `json:"pause_focus_enabled"`

	// FocusAskedBefore 记录是否【已经问过】专注状态授权；拒绝后不再弹。
	FocusAskedBefore bool `json:"focus_asked_before"`

	// PauseCalendarEnabled 是否启用日历忙闲感知（macOS EventKit）。
	// 默认关闭：P1 敏感权限（ADR 0005）。
	PauseCalendarEnabled bool `json:"pause_calendar_enabled"`

	// CalendarAskedBefore 记录是否【已经问过】日历授权；拒绝后不再弹。
	CalendarAskedBefore bool `json:"calendar_asked_before"`

	// SitEnabled 久坐感知总开关。默认开启；只在启用场景包含 stand/move 时生效。
	SitEnabled bool `json:"sit_enabled"`
	// SitProfileEnabled 是否记录应用使用画像。默认关闭，必须由用户显式同意后开启。
	SitProfileEnabled bool `json:"sit_profile_enabled"`
	// SitProfileAsked 是否已经问过应用画像同意（拒绝/接受都置 true，避免反复弹）。
	SitProfileAsked bool `json:"sit_profile_asked"`
	// SitNudgeThresholdMinutes 久坐软提醒阈值：45/60/90，默认 60。
	SitNudgeThresholdMinutes int `json:"sit_nudge_threshold_minutes"`
	// SitTone 宠物语气：sassy / lively / gentle，默认 sassy。
	SitTone string `json:"sit_tone"`
	// SitMutedDate 「今天先这样」的本地日期（YYYY-MM-DD）；空串表示未静音。
	SitMutedDate string `json:"sit_muted_date"`

	// PetVisible 是否显示桌面宠物窗口。关掉只是隐藏窗口，宠物包与其它配置照旧保留。
	PetVisible bool `json:"pet_visible"`
	// PetX / PetY 宠物窗口上次所在的位置（0 表示未记录，此时用 DefaultPetX/Y）。
	PetX int `json:"pet_x"`
	PetY int `json:"pet_y"`
	// PetScale 宠物精灵图的缩放倍数（0.5–2.0，越界回落 DefaultPetScale）。
	PetScale float64 `json:"pet_scale"`
	// PetPackID 选中的宠物包 ID（内置为 DefaultPetPackID）。
	PetPackID string `json:"pet_pack_id"`

	// path 是配置文件路径，不序列化（json:"-"）。
	path string `json:"-"`
}

// Default 返回默认配置。
// 字段默认值为空串/零值，表示"未设置"——由调用方决定 fallback 行为，
// 而非硬编码一个具体默认值（如 Theme 空串 = 跟随系统，窗口尺寸 0 = 未记录）。
//
// 例外是提醒与 AI 的设置：它们有明确的产品默认值（22:00 起静默、推迟 5 分钟、
// 开箱即可用的 AI 端点），给空值只会让用户第一次打开设置页就看到一片空白框。
// 由于 Load 是「先取 Default() 再 Unmarshal」，旧版 config.json 缺失这些键时
// 会自动继承下面的默认值，不需要写迁移代码。
func Default() *Config {
	return &Config{
		Theme:         "",
		AIBaseURL:     ai.DefaultBaseURL,
		AIModel:       ai.DefaultModel,
		QuietStart:    DefaultQuietStart,
		QuietEnd:      DefaultQuietEnd,
		SnoozeMinutes: DefaultSnoozeMinutes,
		NotifyEnabled: true,
		// 强提醒默认开着：本项的立项理由就是「通知太容易被忽略」，
		// 默认关掉等于新用户第一次到点仍然什么都看不见（等于没做这个功能）。
		StrongAlertEnabled: true,
		// 演示建议与自然停顿顺延默认开着：两者都是「不打扰」的温和增强，
		// 关闭后分别退回手动进入演示模式 / 到点即投递。
		PresentationSuggestEnabled: true,
		NaturalPauseEnabled:        true,
		NaturalPauseMaxSeconds:     DefaultNaturalPauseMaxSeconds,
		// 空闲暂停默认开着：这是「智能免打扰」的核心能力，关掉等于回到"到点就响"的原始模式。
		PauseIdleEnabled: true,
		// 空闲阈值默认 5 分钟：去倒杯水/上个洗手间的典型时长。
		PauseIdleMinutes: DefaultPauseIdleMinutes,
		// 锁屏/休眠暂停默认开着：屏幕都关了，弹提醒只是给下一位用户添乱。
		PauseLockEnabled: true,
		// 会议应用暂停默认开着：竞品分析显示「开会时被弹窗打扰」是用户流失首因。
		PauseMeetingEnabled: true,
		// 全屏检测默认【关闭】：见字段注释——避免装完就要用户授权屏幕录制的侵略感。
		// ScreenRecordingPermissionAsked 默认 false：还没问过用户。
		// 久坐感知默认开着，但应用画像默认关闭：基础版只用本机空闲/锁屏，
		// 画像必须显式同意（ADR 0010）。默认毒舌见 ADR 0011。
		SitEnabled:               true,
		SitNudgeThresholdMinutes: sit.DefaultNudgeThresholdMinutes,
		SitTone:                  string(sit.DefaultTone),
		// 宠物默认开着：它是提醒的化身，关掉等于把「到点提醒」的可视化入口藏起来。
		// 位置留 0（= 未记录），由 PetPosition 给出落点。
		PetVisible: true,
		PetScale:   DefaultPetScale,
		PetPackID:  DefaultPetPackID,
	}
}

// WindowSize 返回可直接用于创建窗口的尺寸：非法值（未记录、负数、过小）一律回退默认值。
//
// 配置是用户可写文件，取值必须经此夹取，否则一个手改坏的 config.json
// 会让窗口小到不可用（且用户不知道要去改配置）。
// 上限不夹取：用户把窗口拉到 4K 全屏是正常诉求，屏幕装不下由窗口管理器处理。
func (c *Config) WindowSize() (width, height int) {
	width, height = c.WindowWidth, c.WindowHeight
	if width < MinWindowWidth {
		width = DefaultWindowWidth
	}
	if height < MinWindowHeight {
		height = DefaultWindowHeight
	}
	return width, height
}

// Load 读取配置；文件不存在时写入默认值并返回默认配置。
func Load(appName string) (*Config, error) {
	path, err := appdir.File(appName, "config.json")
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			c := Default()
			c.path = path
			if err := c.Save(); err != nil {
				return nil, fmt.Errorf("写入默认配置失败: %w", err)
			}
			return c, nil
		}
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}

	c := Default()
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	c.path = path
	return c, nil
}

// Save 将当前配置写回文件。
func (c *Config) Save() error {
	if c.path == "" {
		return fmt.Errorf("配置未绑定路径")
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.WriteFile(c.path, data, 0o644); err != nil {
		return fmt.Errorf("写入配置失败: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// AI 连接配置
// ---------------------------------------------------------------------------

// AISettings 返回归一化后的 AI 连接配置。
//
// 【只给后端用】返回值含明文密钥，绝不通过绑定方法下发到前端——前端要的是
// ai.NewPublicSettings 产出的脱敏视图。这条边界由 App 的两个绑定方法分别落实。
func (c *Config) AISettings() ai.Settings {
	return ai.Settings{BaseURL: c.AIBaseURL, Model: c.AIModel, APIKey: c.AIAPIKey}.Normalize()
}

// SaveAISettings 校验并落盘 AI 连接配置。
//
// 【apiKey 为空串表示"保持原值不变"】设置页拿到的是脱敏串，用户不重新输入时前端
// 无法回传明文。若把空串当成"清空密钥"，用户每次改模型名都会顺手把钥匙弄丢，
// 而且下次生成计划才报「未填写 API Key」——一个很隐蔽的失效。
func (c *Config) SaveAISettings(baseURL, model, apiKey string) error {
	endpoint := strings.TrimSpace(baseURL)
	if endpoint == "" {
		return fmt.Errorf("AI 接口地址不能为空")
	}
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		return fmt.Errorf("AI 接口地址必须以 http:// 或 https:// 开头")
	}
	name := strings.TrimSpace(model)
	if name == "" {
		return fmt.Errorf("模型名不能为空")
	}
	c.AIBaseURL = endpoint
	c.AIModel = name
	if key := strings.TrimSpace(apiKey); key != "" {
		c.AIAPIKey = key
	}
	return c.Save()
}

// ---------------------------------------------------------------------------
// 提醒行为设置
// ---------------------------------------------------------------------------

// ReminderSettings 是提醒行为设置的可绑定视图（前端读写用）。
//
// 【为什么单独一个 DTO 而不是直接把 Config 交出去】Config 里还混着窗口几何、
// MCP token、AI 密钥与文件路径；整体下发等于把不该给渲染层的字段一并暴露。
type ReminderSettings struct {
	QuietStart    string `json:"quiet_start"`
	QuietEnd      string `json:"quiet_end"`
	SnoozeMinutes int    `json:"snooze_minutes"`
	NotifyEnabled bool   `json:"notify_enabled"`
	// StrongAlertEnabled 到点是否弹全屏强提醒（见 Config 的同名字段说明）。
	StrongAlertEnabled bool `json:"strong_alert_enabled"`
	// PresentationSuggestEnabled 会议/全屏时是否由宠物建议进入演示模式。
	PresentationSuggestEnabled bool `json:"presentation_suggest_enabled"`
	// NaturalPauseEnabled 是否在用户输入时顺延非一次性提醒。
	NaturalPauseEnabled bool `json:"natural_pause_enabled"`
	// NaturalPauseMaxSeconds 顺延上限秒数：0 / 30 / 60。
	NaturalPauseMaxSeconds int `json:"natural_pause_max_seconds"`
}

// LoadReminderSettings 返回当前提醒行为设置（回显用，已归一化钟点写法）。
func (c *Config) LoadReminderSettings() ReminderSettings {
	start, startOK := reminder.ParseClock(c.QuietStart)
	end, endOK := reminder.ParseClock(c.QuietEnd)
	out := ReminderSettings{
		QuietStart:    c.QuietStart,
		QuietEnd:      c.QuietEnd,
		SnoozeMinutes: c.SnoozeMinutes,
		NotifyEnabled: c.NotifyEnabled,
		// 不回显夹取：强提醒开关是纯布尔，配置里没有"非法取值"这一态。
		StrongAlertEnabled:         c.StrongAlertEnabled,
		PresentationSuggestEnabled: c.PresentationSuggestEnabled,
		NaturalPauseEnabled:        c.NaturalPauseEnabled,
		NaturalPauseMaxSeconds:     clampNaturalPauseMaxSeconds(c.NaturalPauseMaxSeconds),
	}
	// 配置文件是用户可写的，手改坏了不能把非法值回显给前端的选择器。
	if startOK {
		out.QuietStart = reminder.FormatClock(start)
	}
	if endOK {
		out.QuietEnd = reminder.FormatClock(end)
	}
	if out.SnoozeMinutes < MinSnoozeMinutes || out.SnoozeMinutes > MaxSnoozeMinutes {
		out.SnoozeMinutes = DefaultSnoozeMinutes
	}
	return out
}

// SaveReminderSettings 校验并落盘提醒行为设置。
//
// 静默时段允许起点晚于终点（跨午夜是默认用法），也允许两者相同（= 不静默）；
// 唯独不允许写不成钟点的串——那会让调度器永远算不出"现在是不是静默"。
func (c *Config) SaveReminderSettings(in ReminderSettings) error {
	start, startOK := reminder.ParseClock(in.QuietStart)
	if !startOK {
		return fmt.Errorf("静默开始时间格式非法，应为 HH:MM")
	}
	end, endOK := reminder.ParseClock(in.QuietEnd)
	if !endOK {
		return fmt.Errorf("静默结束时间格式非法，应为 HH:MM")
	}
	if in.SnoozeMinutes < MinSnoozeMinutes || in.SnoozeMinutes > MaxSnoozeMinutes {
		return fmt.Errorf("推迟分钟数需在 %d–%d 之间", MinSnoozeMinutes, MaxSnoozeMinutes)
	}
	c.QuietStart = reminder.FormatClock(start)
	c.QuietEnd = reminder.FormatClock(end)
	c.SnoozeMinutes = in.SnoozeMinutes
	c.NotifyEnabled = in.NotifyEnabled
	c.StrongAlertEnabled = in.StrongAlertEnabled
	c.PresentationSuggestEnabled = in.PresentationSuggestEnabled
	c.NaturalPauseEnabled = in.NaturalPauseEnabled
	c.NaturalPauseMaxSeconds = clampNaturalPauseMaxSeconds(in.NaturalPauseMaxSeconds)
	return c.Save()
}

// PresentationShortcut 是演示模式快捷键的可绑定视图。
//
// Accelerator 回显配置值（即使注册失败也保留），Registered 表示当前进程是否
// 真的把它注册到了系统。两者分开，设置页才能显示「已配置但未生效」。
type PresentationShortcut struct {
	Accelerator string `json:"accelerator"`
	Registered  bool   `json:"registered"`
}

// ---------------------------------------------------------------------------
// 智能免打扰
// ---------------------------------------------------------------------------

// PauseSettings 是智能免打扰设置的可绑定视图（设置页读写用）。
//
// 4 个信号独立开关，符合 ADR 0004「逐项可关」的取舍：用户可能对「会议中不弹窗」
// 有需求但对「我发呆 5 分钟你就闭嘴」反感——开关分开才谈得拢。
type PauseSettings struct {
	IdleEnabled       bool `json:"idle_enabled"`
	LockEnabled       bool `json:"lock_enabled"`
	MeetingEnabled    bool `json:"meeting_enabled"`
	FullscreenEnabled bool `json:"fullscreen_enabled"`
	// FocusEnabled / CalendarEnabled 是 P1 的两路敏感信号；默认关闭 + 显式开启 +
	// 拒绝后降级（见 ADR 0005）。
	FocusEnabled    bool `json:"focus_enabled"`
	CalendarEnabled bool `json:"calendar_enabled"`
	// IdleMinutes 空闲阈值：3 / 5 / 10 分钟三档。其他值经 LoadPauseSettings
	// 夹取到最近的合法档（不报错——下拉框给的就是那 3 个，能收到非法值说明
	// 是手改 config.json 或旧版本遗留）。
	IdleMinutes int `json:"idle_minutes"`
	// ScreenRecordingGranted 只读：macOS 屏幕录制权限当前是否已授予。
	// 前端据此决定是否显示「去系统设置开启」按钮；Windows/Linux 恒 true。
	ScreenRecordingGranted bool `json:"screen_recording_granted"`
	// ScreenRecordingAsked 只读：是否曾经问过用户（拒绝过就不再弹说明窗）。
	ScreenRecordingAsked bool `json:"screen_recording_asked"`
	// PlatformNeedsPermission 只读：当前平台是否需要屏幕录制权限。
	PlatformNeedsPermission bool `json:"platform_needs_permission"`
	// 专注：三件套描述授权完整视图。Granted 由 pause.GetFocusStatus 现取；
	// Asked 来自 config；Supported 是平台能力。
	FocusGranted   bool `json:"focus_granted"`
	FocusAsked     bool `json:"focus_asked"`
	FocusSupported bool `json:"focus_supported"`
	// FocusDenied 是「问过、被拒、平台支持」的组合——绑定层用
	// pause.AuthorizationSnapshot.Denied() 算好后填入，前端只读一个布尔
	// 决定要不要显示「已降级」提示，不再自己拼三个字段。
	FocusDenied bool `json:"focus_denied"`
	// CalendarDenied 同 FocusDenied。
	CalendarDenied bool `json:"calendar_denied"`
	// 日历：三件套语义同上。
	CalendarGranted   bool `json:"calendar_granted"`
	CalendarAsked     bool `json:"calendar_asked"`
	CalendarSupported bool `json:"calendar_supported"`
}

// LoadPauseSettings 返回当前智能免打扰设置（回显用，阈值已归一化到合法档位）。
func (c *Config) LoadPauseSettings() PauseSettings {
	return PauseSettings{
		IdleEnabled:             c.PauseIdleEnabled,
		LockEnabled:             c.PauseLockEnabled,
		MeetingEnabled:          c.PauseMeetingEnabled,
		FullscreenEnabled:       c.PauseFullscreenEnabled,
		FocusEnabled:            c.PauseFocusEnabled,
		CalendarEnabled:         c.PauseCalendarEnabled,
		IdleMinutes:             c.PauseIdleMinutesValue(),
		ScreenRecordingGranted:  true, // 由绑定层用 pause.ScreenCaptureGranted 覆盖
		ScreenRecordingAsked:    c.ScreenRecordingPermissionAsked,
		PlatformNeedsPermission: false, // 由绑定层用 pause.RequiresScreenCapture 覆盖
		FocusAsked:              c.FocusAskedBefore,
		CalendarAsked:           c.CalendarAskedBefore,
	}
}

// SavePauseSettings 校验并落盘智能免打扰设置。
//
// 【关于 FullscreenEnabled=true 但权限未授予】这里不做拒绝，也不主动触发系统弹窗——
// 那是绑定方法（SetPauseSettings）的职责，因为它要弹应用内的说明窗再走系统流程。
// 保存本身只是「记住用户意图」；即使没权限，pause 层的 detector 也会自己降级为
// ReasonNone（见 FullscreenDetector.Detect 的注释）。
//
// IdleMinutes 走夹取而非报错：与 PetScaleValue 同理，能收到非档位值说明是手改配置，
// 夹回最近的一档比让设置页弹一个用户看不懂的错误更合适。
func (c *Config) SavePauseSettings(in PauseSettings) error {
	c.PauseIdleEnabled = in.IdleEnabled
	c.PauseLockEnabled = in.LockEnabled
	c.PauseMeetingEnabled = in.MeetingEnabled
	c.PauseFullscreenEnabled = in.FullscreenEnabled
	c.PauseFocusEnabled = in.FocusEnabled
	c.PauseCalendarEnabled = in.CalendarEnabled
	c.PauseIdleMinutes = clampIdleMinutes(in.IdleMinutes)
	return c.Save()
}

// MarkFocusAsked 记录「专注授权已问过」，让 UI 不再重复弹说明窗。
func (c *Config) MarkFocusAsked() error {
	c.FocusAskedBefore = true
	return c.Save()
}

// MarkCalendarAsked 记录「日历授权已问过」。
func (c *Config) MarkCalendarAsked() error {
	c.CalendarAskedBefore = true
	return c.Save()
}

// clampNaturalPauseMaxSeconds 只接受 0 / 30 / 60；其他值回落默认 60。
// 0 是合法值（关闭顺延），不能当成非法值处理。
func clampNaturalPauseMaxSeconds(v int) int {
	switch v {
	case 0, 30, 60:
		return v
	default:
		return DefaultNaturalPauseMaxSeconds
	}
}

// clampIdleMinutes 把任意输入夹到 3/5/10 三档中最接近的一档。
//
// 【为什么允许中间值】下拉框给的就是那 3 个，但用户可能手改 config.json 写个 7；
// 硬拒会毁掉整个设置读取。选最接近的一档：7→5（差 2）比 10（差 3）近，取 5。
func clampIdleMinutes(v int) int {
	options := []int{3, 5, 10}
	best := options[0]
	bestDiff := abs(v - best)
	for _, o := range options[1:] {
		d := abs(v - o)
		// 严格小于才换档：并列时保留前面的档位（3 优先于 5，5 优先于 10），
		// 让「用户手改了一个 4」得到 3 而不是 5——更保守，暂停更容易触发。
		if d < bestDiff {
			best = o
			bestDiff = d
		}
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ---------------------------------------------------------------------------
// 久坐感知
// ---------------------------------------------------------------------------

// SitSettings 是久坐感知设置的可绑定视图。
//
// platform_supported / profile_status / muted_today 是只读字段：前两者由绑定层
// 按平台与实际读取状态填充，muted_today 由 SitMutedDate 与当天比较得出。
type SitSettings struct {
	Enabled           bool              `json:"enabled"`
	ProfileEnabled    bool              `json:"profile_enabled"`
	ProfileAsked      bool              `json:"profile_asked"`
	ThresholdMinutes  int               `json:"threshold_minutes"`
	Tone              sit.Tone          `json:"tone"`
	PlatformSupported bool              `json:"platform_supported"`
	ProfileStatus     sit.ProfileStatus `json:"profile_status"`
	MutedToday        bool              `json:"muted_today"`
}

// LoadSitSettings 返回当前久坐设置（阈值/语气已归一化）。
func (c *Config) LoadSitSettings() SitSettings {
	return SitSettings{
		Enabled:           c.SitEnabled,
		ProfileEnabled:    c.SitProfileEnabled,
		ProfileAsked:      c.SitProfileAsked,
		ThresholdMinutes:  sit.ClampNudgeThreshold(c.SitNudgeThresholdMinutes),
		Tone:              sit.NormalizeTone(c.SitTone),
		PlatformSupported: false, // 由绑定层覆盖
		ProfileStatus:     "",    // 由绑定层覆盖
		MutedToday:        c.SitMutedToday(),
	}
}

// SaveSitSettings 归一化并落盘久坐设置；只读字段忽略。
func (c *Config) SaveSitSettings(in SitSettings) error {
	c.SitEnabled = in.Enabled
	c.SitProfileEnabled = in.ProfileEnabled
	c.SitProfileAsked = in.ProfileAsked
	c.SitNudgeThresholdMinutes = sit.ClampNudgeThreshold(in.ThresholdMinutes)
	c.SitTone = string(sit.NormalizeTone(string(in.Tone)))
	return c.Save()
}

// SitMutedToday 返回今天是否已被「今天先这样」静音。
func (c *Config) SitMutedToday() bool {
	return c.SitMutedDate != "" && c.SitMutedDate == time.Now().Format(model.DateLayout)
}

// MuteSitToday 把今天标记为静音；跨日后 SitMutedToday 自然返回 false。
func (c *Config) MuteSitToday() error {
	c.SitMutedDate = time.Now().Format(model.DateLayout)
	return c.Save()
}

// ---------------------------------------------------------------------------
// 桌面宠物
// ---------------------------------------------------------------------------

// PetSettings 是宠物设置的可绑定视图（设置页读写用）。
//
// 只放「用户能调的三件事」：开关、形象、缩放。位置不进这个 DTO——
// 它是用户拖动窗口的副产物（由 ServiceShutdown 落盘），不是一项可编辑的设置。
type PetSettings struct {
	// PetVisible 是否显示宠物窗口。
	PetVisible bool `json:"pet_visible"`
	// PetScale 精灵图缩放倍数（0.5–2.0）。
	PetScale float64 `json:"pet_scale"`
	// PetPackID 选中的宠物包 ID。
	PetPackID string `json:"pet_pack_id"`
}

// PetScaleValue 返回夹取后的缩放倍数：越界（含 NaN）一律回落默认值。
//
// 为什么要判 NaN：JSON 里的 1e999 会被解析成 +Inf，而 `Inf > MaxPetScale` 为真、
// 能兜住；但 `NaN` 与任何值比较都为 false，不显式判断就会漏过去，
// 让前端拿到一个把画布算成 NaN 像素的缩放值。
func (c *Config) PetScaleValue() float64 {
	if math.IsNaN(c.PetScale) || c.PetScale < MinPetScale || c.PetScale > MaxPetScale {
		return DefaultPetScale
	}
	return c.PetScale
}

// PetPosition 返回可直接用于创建窗口的宠物窗口位置。
//
// 语义：两者都为 0 表示「从未记录过」（首次启动），落回 DefaultPetX/Y；
// 负值（手改坏或被窗口管理器回报的异常值）夹到 0，宁可靠左上角也不要跑到屏幕外。
func (c *Config) PetPosition() (x, y int) {
	if c.PetX == 0 && c.PetY == 0 {
		return DefaultPetX, DefaultPetY
	}
	x, y = c.PetX, c.PetY
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

// PauseIdleMinutesValue 返回夹取后的空闲阈值分钟数：越界一律回落默认值。
//
// 配置是用户可写的，手改坏了不能把非法值交给空闲检测器——过小的阈值会让"发个呆"
// 也触发暂停，过大的阈值会让"离开半小时"仍然被弹窗打扰。
func (c *Config) PauseIdleMinutesValue() int {
	if c.PauseIdleMinutes < MinPauseIdleMinutes || c.PauseIdleMinutes > MaxPauseIdleMinutes {
		return DefaultPauseIdleMinutes
	}
	return c.PauseIdleMinutes
}

// LoadPetSettings 返回当前宠物设置（回显用，取值已归一化）。
func (c *Config) LoadPetSettings() PetSettings {
	packID := strings.TrimSpace(c.PetPackID)
	if packID == "" {
		packID = DefaultPetPackID
	}
	return PetSettings{
		PetVisible: c.PetVisible,
		PetScale:   c.PetScaleValue(),
		PetPackID:  packID,
	}
}

// SavePetSettings 归一化并落盘宠物设置。
//
// 越界的缩放【不报错，回落默认值】：这一项来自滑块，用户很难填错；
// 真正会越界的场景是手改 config.json 或旧版本遗留值，把它们夹回合法区间
// 比让设置页弹一个用户无法理解的错误更合适（与 WindowSize 的取舍一致）。
func (c *Config) SavePetSettings(in PetSettings) error {
	scale := in.PetScale
	if math.IsNaN(scale) || scale < MinPetScale || scale > MaxPetScale {
		scale = DefaultPetScale
	}
	packID := strings.TrimSpace(in.PetPackID)
	if packID == "" {
		packID = DefaultPetPackID
	}
	c.PetVisible = in.PetVisible
	c.PetScale = scale
	c.PetPackID = packID
	return c.Save()
}
