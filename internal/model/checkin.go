package model

// DateLayout 是 date_key 的格式，也是「今天」的唯一定义方式。
//
// 【为什么必须有 date_key 字段而不是每次按时间戳范围查】
// 「今天喝了几杯水」是按用户所在时区的自然日算的。用 Unix 时间戳做范围查询意味着
// 每个统计点都要先算「本地今天零点的时间戳」，而那个值在时区/夏令时变化时极易算错；
// 存成 YYYY-MM-DD 之后，统计退化为一次字符串等值比较，跨天判断也不会出错。
const DateLayout = "2006-01-02"

// 打卡动作（单一真相）。
const (
	// ActionDone 完成。
	ActionDone = "done"
	// ActionSnooze 推迟。
	ActionSnooze = "snooze"
	// ActionSkip 跳过。
	ActionSkip = "skip"
)

// CheckIn 是一次用户动作的记录，是今日统计与连续达标天数的唯一数据源。
//
// 【为什么不直接数触发记录】触发记录只能回答「提醒响过几次」，回答不了
// 「用户做了几次」——完成与跳过都是响过，但语义相反。统计需要的是动作，
// 故单独一张表；它也是将来做趋势图/周报时唯一需要读的表。
type CheckIn struct {
	BaseModel

	// ReminderID 动作所属的提醒项。
	ReminderID uint `gorm:"index;not null" json:"reminder_id"`

	// ScenarioID 冗余的场景 ID：统计按场景聚合时不必再 join 一次提醒表。
	ScenarioID uint `gorm:"index;not null" json:"scenario_id"`

	// Action 取值见上方 Action* 常量。
	Action string `gorm:"size:16;not null" json:"action"`

	// ScheduledAt 这次动作回应的是哪个计划时间点（Unix 秒）。
	ScheduledAt int64 `gorm:"not null;default:0" json:"scheduled_at"`

	// ActedAt 动作发生时间（Unix 秒）。
	ActedAt int64 `gorm:"not null;default:0" json:"acted_at"`

	// DateKey 本地时区的 YYYY-MM-DD（格式见 DateLayout）。
	DateKey string `gorm:"size:10;index;not null" json:"date_key"`
}
