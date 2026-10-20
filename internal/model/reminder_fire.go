package model

// 触发记录的状态机（单一真相）。
//
// 【为什么需要一张「触发记录」表，而不是纯内存调度】
// 桌面应用会被重启、被休眠、系统时间会被用户或 NTP 调整。纯内存的「下次触发时间」
// 在这些场景下只有两种结局：重启后从头再弹一遍（重复轰炸），或休眠期间的时间点
// 无人负责（漏提醒）。把每个「计划触发点」落库、并对 (reminder_id, fire_at) 建唯一
// 索引之后，「这个点处理过没有」就变成数据库能回答的事实——重启多少次都不会重复，
// 唤醒后也能立刻分清哪些是真正错过的。
const (
	// FireFired 已送达，等待用户处理（首页「待处理」卡片就是这一批）。
	FireFired = "fired"
	// FireMissed 已过期但按补发策略丢弃：同一提醒一次只补最近 1 条，
	// 更早的过期点标成本状态，绝不一次弹十几条。
	FireMissed = "missed"
	// FireSilent 落在静默时段：照常记录（统计里能看到），但不打扰用户。
	FireSilent = "silent"
	// FireSnooze 是「待补发」的推迟记录：fire_at = 现在 + N 分钟，到点由引擎补发。
	FireSnooze = "snooze"
	// FireDeferred 该计划触发点已被用户推迟处理，它本身不再触达；
	// 下一次触达由一条新记录承载（见 FireSnooze）。
	//
	// 【必须与 FireSnooze 分开，不能共用一个状态】DueSnoozeFires 的判据是
	// `status = snooze AND fire_at <= now`。原记录的 fire_at 停在过去的计划时间，
	// 若把它也写成 snooze，下一轮 tick（≤20 秒）就会把它捞回来重弹——
	// 用户点完「推迟 5 分钟」二十秒后又收到同一条提醒，推迟形同虚设。
	FireDeferred = "deferred"
	// FireDone 用户点了完成。
	FireDone = "done"
	// FireSkip 用户点了跳过。
	FireSkip = "skip"
)

// ReminderFire 是一条提醒的触发记录。
//
// 这张表同时是三个问题的答案：
//  1. 防重复：唯一索引让「同一次计划触发」只可能被一个人写入（见上方注释）；
//  2. 待处理清单：status = fired 的行即首页卡片的数据源；
//  3. 推迟队列：status = snooze 且 fire_at 已到的行会被引擎补发。
type ReminderFire struct {
	BaseModel

	// ReminderID + FireAt 是幂等键：同一提醒的同一计划时间点只允许一行。
	// 【唯一索引而不是「先查后插」】先查后插在两次 tick、两个进程之间存在窗口，
	// 唯一索引是原子的判据。
	ReminderID uint  `gorm:"uniqueIndex:idx_fire_dedup,priority:1;not null" json:"reminder_id"`
	FireAt     int64 `gorm:"uniqueIndex:idx_fire_dedup,priority:2;not null" json:"fire_at"`

	// Status 取值见上方 Fire* 常量。
	Status string `gorm:"size:16;not null;default:'fired';index" json:"status"`

	// FiredAt 实际送达时间（Unix 秒）。未送达（missed/snooze）时为 0 或原值。
	FiredAt int64 `gorm:"not null;default:0" json:"fired_at"`
}
