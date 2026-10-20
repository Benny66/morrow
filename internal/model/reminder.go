package model

import "strings"

// 提醒类型枚举（单一真相）。
//
// 【为什么收口成固定枚举而不是自由文本】类型同时决定三件事：仪表盘上的图标、
// 进度的量词（「今天喝了 3 杯」 vs 「起身 4 次」）、以及 AI 输出的可用性。
// 让模型自由发挥「类型」字段，前端就得为一个未知词兜一套文案；固定枚举之后，
// 未知值统一落到 KindCustom，界面永远有可用的呈现。
const (
	// KindWater 喝水。
	KindWater = "water"
	// KindStand 站立（如站立办公）。
	KindStand = "stand"
	// KindMove 走动。
	KindMove = "move"
	// KindToilet 如厕。
	KindToilet = "toilet"
	// KindRest 护眼/放松。
	KindRest = "rest"
	// KindCustom 其他（兜底）。
	KindCustom = "custom"
)

// NormalizeKind 把任意字符串归一化为合法 kind。
//
// 入参来自两处不可信来源：AI 的 JSON 输出、用户手填的表单。两处都可能给出
// 大小写不一、带空格或干脆是空串的值，故归一化只在这一处做。
func NormalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case KindWater:
		return KindWater
	case KindStand:
		return KindStand
	case KindMove:
		return KindMove
	case KindToilet:
		return KindToilet
	case KindRest:
		return KindRest
	default:
		return KindCustom
	}
}

// AllKinds 返回全部合法 kind（顺序即提示词里枚举的顺序）。
//
// 【为什么需要它】AI 的 system prompt 要列出可选值。把那份清单写死在提示词字符串里
// 就是第二处真相——加一个 kind 时忘了改提示词，模型永远不会产出新值，
// 而没有任何报错。故提示词从本函数生成。
func AllKinds() []string {
	return []string{KindWater, KindStand, KindMove, KindToilet, KindRest, KindCustom}
}

// Reminder 是某个场景下的一条提醒项。
//
// 时间字段口径：WindowStart / WindowEnd 是「当天的钟点」字符串（HH:MM，本地时区），
// 不是时间戳；其余时间字段（含 BaseModel 的 CreatedAt/UpdatedAt）一律 Unix 秒。
type Reminder struct {
	BaseModel

	// ScenarioID 所属场景。一次性提醒没有场景，落库为 NULL。
	// 场景删除时其名下周期项被软删（见 service.DeleteScenario）。
	ScenarioID *uint `gorm:"index" json:"scenario_id"`

	// Title 通知标题（如「喝水」），也是首页卡片的标题。
	Title string `gorm:"size:50;not null" json:"title"`

	// Content 通知正文（如「起身倒一杯温水，慢慢喝完」）。
	Content string `gorm:"size:200" json:"content"`

	// Kind 类型，取值见上方 Kind* 常量。
	Kind string `gorm:"size:16;not null;default:'custom'" json:"kind"`

	// IntervalMin 间隔分钟数。0 表示「每日单点」——每天只在 WindowStart 触发一次。
	//
	// 【刻意不写 default 标签】GORM 对带 default 的零值字段会走「省略该列、用库默认」，
	// 导致显式 0 被存成 60。0 在这里是有意义的值，必须原样写入。
	IntervalMin int `gorm:"not null" json:"interval_min"`

	// OnceAt 一次性提醒的绝对触发时刻（Unix 秒）。0 表示周期性提醒。
	//
	// 【为什么是绝对时间而不是日期+钟点】用户说「明天 4 点」表达的是一个具体瞬间；
	// 存成绝对时间后，触发判定不需要再拼「今天」，也不会因为跨时区/夏令时被重排。
	// 一次性项没有场景（ScenarioID 为 NULL），IntervalMin / DailyTarget 恒为 0，
	// WindowStart / WindowEnd 不参与调度与展示。
	OnceAt int64 `gorm:"index;not null;default:0" json:"once_at"`

	// WindowStart / WindowEnd 生效时段的起止钟点（HH:MM）。要求 End > Start，
	// 不支持跨午夜：健康提醒的时段语义是「上班这段时间」，
	// 允许跨午夜只会让「今天」这个概念在统计里失去意义。
	WindowStart string `gorm:"size:5;not null;default:'09:00'" json:"window_start"`
	WindowEnd   string `gorm:"size:5;not null;default:'18:00'" json:"window_end"`

	// DailyTarget 每日目标次数（如喝水 8 杯）。0 表示不设目标，只做纯提醒。
	DailyTarget int `gorm:"not null;default:0" json:"daily_target"`

	// Enabled 是否参与调度。关掉单条周期提醒不需要停用整个场景。
	// 一次性提醒不使用该字段做启停：它只有编辑与删除。
	//
	// 同样不写 default：false 是合法值，带 default 会让显式关闭被库默认覆盖。
	Enabled bool `gorm:"not null" json:"enabled"`

	// SortOrder 展示顺序（AI 生成时按返回顺序赋值，用户可后续调整）。
	SortOrder int `gorm:"not null;default:0" json:"sort_order"`
}
