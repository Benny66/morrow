package reminder

import (
	"strconv"
	"strings"

	"morrow/internal/event"
	"morrow/internal/model"
)

// Domain 是事件名的 domain 段（事件契约：<domain>:<action>）。
// 前端镜像见 frontend/src/lib/reminder.ts 的 REMINDER_DOMAIN。
const Domain = "reminder"

// FiredEventName 返回「提醒已送达」的事件名。
//
// 该事件的意义：通知是系统层的东西，界面拿不到它何时出现。引擎发完通知后立刻广播
// 这一条，首页据此把「待处理卡片」刷新出来——这是用户真正能操作提醒的地方
// （通知上的按钮在未打包/未授权时不可用，见 app.go 的说明）。
func FiredEventName() string { return event.Name(Domain, event.ActionFired) }

// ChangedEventName 返回「提醒状态变更」的事件名。
//
// 打卡、推迟、重新生成计划之后广播，让所有已挂载的页面（首页进度、场景页摘要）
// 不必靠轮询就能同步。
func ChangedEventName() string { return event.Name(Domain, event.ActionChanged) }

// 变更原因（单一真相）。前端只用它区分日志链路，不参与业务分支。
const (
	// ReasonCheckIn 用户打卡（完成/推迟/跳过）。
	ReasonCheckIn = "check_in"
	// ReasonPlanChanged 提醒项被增删改，或 AI 重新生成了计划。
	ReasonPlanChanged = "plan_changed"
	// ReasonScenarioChanged 场景被增删、启用或停用。
	ReasonScenarioChanged = "scenario_changed"
)

// FiredPayload 是 reminder:fired 的载荷。
// 前端镜像见 frontend/src/lib/reminder.ts 的 ReminderFiredPayload（字段名需一致）。
type FiredPayload struct {
	// FireID 触发记录 ID，前端点「完成/推迟/跳过」时回传它。
	FireID uint `json:"fire_id"`
	// ReminderID / ScenarioID 便于前端定位是哪条提醒、哪个场景。
	ReminderID uint `json:"reminder_id"`
	ScenarioID uint `json:"scenario_id"`
	// Title / Content / Kind 直接把展示所需的数据带上：前端收到事件后
	// 可以先渲染卡片再异步刷新统计，避免"事件到了但界面还空着"。
	Title   string `json:"title"`
	Content string `json:"content"`
	Kind    string `json:"kind"`
	// IsOneOff 是否为一次性提醒（场景为空、只响这一回）。
	IsOneOff bool `json:"is_one_off"`
	// FireAt 计划触发时间（Unix 秒）。
	FireAt int64 `json:"fire_at"`
}

// ChangedPayload 是 reminder:changed 的载荷。
type ChangedPayload struct {
	// Reason 取值见 Reason* 常量。
	Reason string `json:"reason"`
}

// Settings 是引擎一轮调度所需的行为设置（由组合根从 config.json 注入）。
//
// 【为什么是值而不是指针/方法】引擎在独立 goroutine 里每 20 秒读一次，
// 每次拿到的是不可变快照——没有共享状态，也就没有忘记加锁的机会。
type Settings struct {
	// QuietStart / QuietEnd 静默时段（HH:MM，允许跨午夜）。相等表示不静默。
	QuietStart string
	QuietEnd   string
	// SnoozeMinutes 用户点「推迟」后多少分钟再提醒一次。
	SnoozeMinutes int
	// NotifyEnabled 是否发系统通知。关闭后提醒仍然记录、仍然进待处理卡片——
	// 这是「只想在应用内看进度」的用户要的行为，而不是把功能整个停掉。
	NotifyEnabled bool
}

// Notification 是一次系统通知请求。
type Notification struct {
	// ID 是通知标识，格式见 DueFire.NotificationID()。系统回调只带回这个字符串，
	// 故它必须能反查出是哪一条触发记录——这是通知按钮能落到正确提醒上的前提。
	ID string
	// Title 通知标题；Subtitle 仅 macOS 生效（用来放场景名）；Body 是提醒文案。
	Title    string
	Subtitle string
	Body     string
	// FireID 已解出的触发记录 ID，供注入方直接使用（免去再次解析 ID）。
	FireID uint
}

// Notifier 发送系统通知。
//
// 约定：返回错误表示「通知没送达」，调用方不因此改变提醒本身的状态——
// 首页卡片是可靠的交付通道，通知只是增强。
type Notifier interface {
	Notify(n Notification) error
}

// Emitter 把事件推给前端。
//
// 【为什么是接口而不是直接拿 *application.App】引擎不该知道 wails 的存在：
// 一旦 import application，reminder 包就无法在 go test 里跑。
type Emitter interface {
	EmitFired(p FiredPayload)
	// EmitChanged 广播数据变更（如一次性提醒被标记为已错过），让首页刷新列表。
	EmitChanged(p ChangedPayload)
}

// DueFire 是一条「该送达」的提醒。它由两条链路产生：
//   - 调度网格算出的计划触发点（FirePoints）；
//   - 用户推迟后落库、到点被捞出的补发记录。
//
// 两者在送达阶段行为完全一致，故共用同一个结构。
type DueFire struct {
	FireID       uint
	ReminderID   uint
	ScenarioID   uint
	ScenarioName string
	Title        string
	Content      string
	Kind         string
	// IsOneOff 为 true 时，静默时段也必须送达（用户明确指定了时刻）。
	IsOneOff bool
	FireAt   int64
}

// notificationIDPrefix 是通知标识的前缀（见 DueFire.NotificationID）。
const notificationIDPrefix = "reminder-fire-"

// NotificationID 返回该触发对应的通知标识（"reminder-fire-<fireID>"）。
//
// 为什么把 fireID 编进通知 ID：系统的通知回调只交回 ID 字符串，
// 不带任何自定义 payload 的保证（各平台实现不同）。把主键编进 ID 是最稳的反查方式。
func (f DueFire) NotificationID() string {
	return notificationIDPrefix + strconv.FormatUint(uint64(f.FireID), 10)
}

// DefaultNotificationTitle 是通知标题的最终兜底。
//
// 【为什么必须有】上游 notifications 的 validateNotificationOptions（beta.23）对
// Title == "" 直接返回 error——整条通知发不出去，只在日志里留一条 warn。
// 一条数据异常（场景名与提醒标题同时被清空）不该升级成"用户永远收不到这条提醒"，
// 故这里兜一个常量，让通知仍然能送达。取「提醒」而不是别的词，是为了与
// model.KindCustom 的展示名、AI 解析的 defaultTitle 保持同一个词。
const DefaultNotificationTitle = "提醒"

// NotificationTitle 返回通知标题：有场景名时带上它，让用户一眼知道是哪套计划。
func (f DueFire) NotificationTitle() string {
	name := strings.TrimSpace(f.ScenarioName)
	title := strings.TrimSpace(f.Title)
	switch {
	case name == "" && title == "":
		return DefaultNotificationTitle
	case name == "":
		return title
	case title == "":
		return name
	default:
		return name + " · " + title
	}
}

// FireIDFromNotificationID 从通知标识反查触发记录 ID；无法解析时返回 0。
//
// 返回 0 而不是 error：调用方（通知响应回调）处在一个「不该因异常输入打断」的
// 位置，0 是天然的空值（BaseModel 的主键从 1 开始）。
func FireIDFromNotificationID(id string) uint {
	text := strings.TrimSpace(id)
	if !strings.HasPrefix(text, notificationIDPrefix) {
		return 0
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(text, notificationIDPrefix), 10, 32)
	if err != nil {
		return 0
	}
	return uint(parsed)
}

// Store 是引擎需要的持久化能力（由 service.Service 实现）。
//
// 【为什么是接口】引擎不持有 *gorm.DB，只关心四件事：当前启用什么、有几条提醒、
// 某个时间点归不归我、把结果落成什么状态。把 SQL 细节留在 service 层，
// 调度逻辑才能在没有数据库的情况下被推演。
type Store interface {
	// ActiveScenario 返回当前启用的场景；没有启用任何场景时返回 (nil, nil)。
	ActiveScenario() (*model.Scenario, error)
	// ActiveReminders 返回某场景下启用中的提醒项。
	ActiveReminders(scenarioID uint) ([]model.Reminder, error)
	// DueOneOffs 返回到点且尚未产生触发记录的一次性提醒。
	// 全局查询，不依赖场景启用状态；是否错过由引擎用 OneOffOutcome 判定。
	DueOneOffs(now int64) ([]model.Reminder, error)
	// ClaimFire 尝试占用 (reminderID, fireAt) 这个计划触发点，初始状态 fired。
	// 占用成功返回新记录 ID 与 true；已被占用（含历史记录）返回 0 与 false。
	ClaimFire(reminderID uint, fireAt int64) (uint, bool, error)
	// ClaimFireWithStatus 同 ClaimFire，但允许指定初始状态。
	// 一次性提醒的「错过」必须在写入时就落 missed，避免崩溃窗口把它暴露成待处理。
	ClaimFireWithStatus(reminderID uint, fireAt int64, status string) (uint, bool, error)
	// SettleFire 把触发记录落成终态（fired/missed/silent/done/skip/snooze）。
	// firedAt 为 0 表示保持原值。
	SettleFire(fireID uint, status string, firedAt int64) error
	// DueSnoozeFires 返回到点待补发的推迟记录（status = snooze 且 fire_at <= now）。
	DueSnoozeFires(now int64) ([]DueFire, error)
}
