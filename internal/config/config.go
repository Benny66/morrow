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

	"morrow/internal/ai"
	"morrow/internal/appdir"
	"morrow/internal/reminder"
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
		StrongAlertEnabled: c.StrongAlertEnabled,
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
