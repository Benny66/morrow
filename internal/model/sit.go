package model

// 久坐感知与应用使用画像的持久化模型。
//
// 这里只定义数据形状；状态/类别/语气等取值常量在 internal/sit（领域层），
// 本包保持叶子，不 import 业务包。

// AppUsageSession 是一次前台应用会话。
//
// 只在应用切换、真实休息、锁屏或功能关闭时收尾；不按采样轮询逐条落库。
// 类别不在这里存：它由 internal/sit 的内置映射 + 用户纠正派生。
type AppUsageSession struct {
	BaseModel

	// Platform 平台标识：darwin / windows。
	Platform string `gorm:"size:16;not null;index:idx_app_usage_platform_app" json:"platform"`
	// AppID 前台应用标识：macOS bundle ID / Windows 进程名。
	AppID string `gorm:"size:255;not null;index:idx_app_usage_platform_app" json:"app_id"`
	// StartedAt 会话开始（Unix 秒）。
	StartedAt int64 `gorm:"not null;index" json:"started_at"`
	// EndedAt 会话结束（Unix 秒）；0 表示仍在进行。
	EndedAt int64 `gorm:"not null;default:0" json:"ended_at"`
	// LastSeenAt 最近一次心跳（Unix 秒），用于崩溃后收尾。
	LastSeenAt int64 `gorm:"not null;default:0" json:"last_seen_at"`
	// DateKey 本地日期（YYYY-MM-DD）。
	DateKey string `gorm:"size:10;not null;index" json:"date_key"`
	// EndReason 结束原因：app_switch / idle / lock / feature_off / abandoned。
	EndReason string `gorm:"size:32;not null;default:''" json:"end_reason"`
}

// SitSession 是一段连续用机会话。
//
// active_seconds 是真正累计的久坐秒数，不含 5–15 分钟暂停；今日久坐总时长
// 与最长连续久坐都用它，不能用 ended_at-started_at 替代。
type SitSession struct {
	BaseModel

	StartedAt  int64 `gorm:"not null;index" json:"started_at"`
	EndedAt    int64 `gorm:"not null;default:0" json:"ended_at"`
	LastSeenAt int64 `gorm:"not null;default:0" json:"last_seen_at"`
	// ActiveSeconds 真正累计的久坐秒数。
	ActiveSeconds int    `gorm:"not null;default:0" json:"active_seconds"`
	DateKey       string `gorm:"size:10;not null;index" json:"date_key"`
	// EndReason active_break / lock_break / idle_break / feature_off / abandoned。
	EndReason string `gorm:"size:32;not null;default:''" json:"end_reason"`
	// CreditedReminderID / CreditedCheckInID 本会话已抵扣的提醒与打卡记录；
	// 非零表示本会话的抵扣额度已用完。
	CreditedReminderID uint `gorm:"not null;default:0" json:"credited_reminder_id"`
	CreditedCheckInID  uint `gorm:"not null;default:0" json:"credited_check_in_id"`
}

// SitBreak 是一次真实休息。
//
// 主动休息/锁屏/空闲都会记录；只有主动休息与满足门槛的锁屏会写 CheckIn。
type SitBreak struct {
	BaseModel

	SitSessionID uint   `gorm:"not null;index" json:"sit_session_id"`
	At           int64  `gorm:"not null;index" json:"at"`
	Kind         string `gorm:"size:16;not null" json:"kind"`
	DurationSec  int    `gorm:"not null;default:0" json:"duration_sec"`
	// CreditedReminderID / CreditedCheckInID 仅主动/锁屏抵扣时非零。
	CreditedReminderID uint `gorm:"not null;default:0" json:"credited_reminder_id"`
	CreditedCheckInID  uint `gorm:"not null;default:0" json:"credited_check_in_id"`
}

// SitSpeech 是宠物主动发言/问候的记录，用于每日预算与过期判断。
//
// text 只存本机；导出时可整表排除。预算只统计 acked_at 非空且 kind 为
// nudge/memory 的行；greeting 不占预算。
type SitSpeech struct {
	BaseModel

	DateKey      string `gorm:"size:10;not null;index" json:"date_key"`
	SitSessionID uint   `gorm:"not null;default:0;index" json:"sit_session_id"`
	// Kind nudge / memory / greeting。
	Kind       string `gorm:"size:16;not null" json:"kind"`
	TemplateID string `gorm:"size:64;not null;default:''" json:"template_id"`
	// MemoryID 是这条发言引用的记忆事实 ID（3 天去重）；普通发言为空。
	MemoryID  string `gorm:"size:64;not null;default:''" json:"memory_id"`
	Text      string `gorm:"type:text;not null" json:"text"`
	AckedAt   int64  `gorm:"not null;default:0" json:"acked_at"`
	ExpiresAt int64  `gorm:"not null;default:0" json:"expires_at"`
}

// AppCategoryOverride 是用户对某个应用行为类别的本地纠正。
type AppCategoryOverride struct {
	BaseModel

	Platform string `gorm:"size:16;not null;uniqueIndex:idx_app_category_override" json:"platform"`
	AppID    string `gorm:"size:255;not null;uniqueIndex:idx_app_category_override" json:"app_id"`
	Category string `gorm:"size:16;not null" json:"category"`
}
