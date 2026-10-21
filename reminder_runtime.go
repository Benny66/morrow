package main

import (
	"log/slog"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"morrow/internal/reminder"
)

// 通知交互相关的标识（单一真相：注册分类、发通知、解析响应三处共用）。
const (
	// notificationCategoryID 是「带按钮通知」的分类 ID。
	// 系统要求先注册分类、再按分类发通知，按钮才会出现。
	notificationCategoryID = "reminder-actions"
	// notificationDoneActionID 是「完成」按钮的标识，随通知响应回传。
	notificationDoneActionID = "reminder-done"
	// notificationSnoozeActionID 是「推迟」按钮的标识。
	notificationSnoozeActionID = "reminder-snooze"
)

// reminderNotifier 把引擎的通知请求转成 Wails 系统通知。
//
// 【v3 beta.23 对通知按钮的支持情况（已对着该版本的源码核实）】
//   - notifications.NotificationOptions 本身没有 actions 字段；
//   - 但配套的 RegisterNotificationCategory + SendNotificationWithActions 支持
//     自定义按钮，OnNotificationResponse 会把"用户点的是哪个按钮"回传过来。
//
// 故「完成 / 推迟」是可以直接做在通知上的，本类型就按这条路实现。
//
// 【为什么仍然保留"点通知 → 唤起主窗口 → 首页卡片"这条路径】
//   - 分类注册与按钮要求应用已打包并签名（dev 态 ensureDelegateInitialized 会失败），
//     开发时按钮必然不存在；
//   - 用户也可能点通知本体而不是按钮，或直接把通知划掉；
//   - 通知权限可能被系统关掉。
//
// 三条都指向同一个结论：通知只是"增强通道"，首页的待处理卡片才是可靠的交付方式。
// 两条链路最终落到同一批 service 动作上（见 App.bindNotificationResponses），
// 行为一致，不会出现"从通知点完成"和"从卡片点完成"结果不同的情况。
type reminderNotifier struct {
	// notify 是官方通知服务实例（由组合根注入，可能为 nil）。
	notify *notifications.NotificationService
	// categoryOnce 保证分类只注册一次。
	//
	// 注册失败后不重试（categoryErr 非空即永久走普通通知）：失败原因几乎都是
	// "未打包/未授权"，重试每次只多一次同步 IPC 调用而结果不变；
	// 重启应用是用户可理解、也足够廉价的恢复方式。
	categoryOnce sync.Once
	// categoryErr 记录分类注册的结果（nil 表示可用）。
	categoryErr error
}

// Notify 发送一条系统通知。
//
// 失败一律返回错误交给调用方记日志，不在本类型里吞掉：
// 引擎那边已经明确了"通知失败不影响提醒本身"，但日志里必须留下痕迹，
// 否则用户报"没有通知"时无从判断是权限、打包还是分类注册的问题。
func (n *reminderNotifier) Notify(message reminder.Notification) error {
	if n.notify == nil {
		// 通知服务未注入：不算失败。卡片通道照常工作，用户仍能看到提醒。
		return nil
	}
	options := notifications.NotificationOptions{
		ID:       message.ID,
		Title:    message.Title,
		Subtitle: message.Subtitle,
		Body:     message.Body,
	}
	if !n.categoryReady() {
		return n.notify.SendNotification(options)
	}

	options.CategoryID = notificationCategoryID
	sendErr := n.notify.SendNotificationWithActions(options)
	if sendErr == nil {
		return nil
	}
	// 带按钮失败（分类没注册上、系统限制等）→ 退回普通通知。
	// 宁可少两个按钮，也不能让用户收不到这条提醒。
	slog.Warn("发送带按钮的通知失败，退回普通通知", "err", sendErr)
	return n.notify.SendNotification(options)
}

// categoryReady 确保「完成 / 推迟」分类已注册，返回是否可用。
func (n *reminderNotifier) categoryReady() bool {
	n.categoryOnce.Do(func() {
		n.categoryErr = n.notify.RegisterNotificationCategory(notifications.NotificationCategory{
			ID: notificationCategoryID,
			Actions: []notifications.NotificationAction{
				{ID: notificationDoneActionID, Title: "完成"},
				{ID: notificationSnoozeActionID, Title: "推迟"},
			},
		})
		if n.categoryErr != nil {
			slog.Warn("注册通知动作分类失败，通知将不带按钮", "err", n.categoryErr)
		}
	})
	return n.categoryErr == nil
}

// appEmitter 把引擎事件推给前端。
//
// 【为什么放在 main 包而不是 service 层】*application.App 只在组合根可见；
// service 层一旦 import wails 的 application 包，业务就无法在 go test 里跑了。
// 让引擎面向接口（reminder.Emitter），由组合根提供实现，是最省事的隔离方式。
type appEmitter struct {
	// app 是 wails 应用实例，用于 emit 事件。
	app *application.App
}

// alertShowHook 是「强提醒」的 Go 侧兜底：到点时不只依赖隐藏窗口里的 JS。
//
// 【为什么需要它】整条强提醒链路的触发是「隐藏的 alert 窗口前端收到 reminder:fired
// → 自己判断 → 调 ShowAlert()」，后端从不主动推窗口。而 WKWebView 对 ordered-out
// 的窗口会降优先级，可能节流甚至挂起页面——一旦那一页被挂起，遮罩永远不出现，
// 且不会有任何日志。这正是本仓最忌讳的「没有症状的失效」。
//
// 【边界：它只负责"窗出来"】内容仍由前端渲染（那一页恢复可见时会自己
// syncVisibility 刷新），有没有待处理也仍由前端判断——后端只有数据库与配置，
// 没有"这一屏该不该打断用户"的上下文。故实现里【不查待处理、不调 Focus】。
//
// 【为什么是包级变量而不是 appEmitter 的字段】emitter 在 app.go 里构造
// （ServiceStartup），而 app.go 是「绑定方法层」，本次改动不动它（护栏 3 会复算
// 方法 ID，任何改动都要重新生成绑定）。注入点因此挪到组合根 main.go 建好窗口之后。
// nil 表示不启用兜底；写入发生在 Run() 之前，读取发生在引擎 tick 里，无竞争。
var alertShowHook func()

// EmitFired 广播 reminder:fired，让首页立刻把待处理卡片刷出来。
func (e appEmitter) EmitFired(payload reminder.FiredPayload) {
	if e.app == nil {
		return
	}
	e.app.Event.Emit(reminder.FiredEventName(), payload)
	// 事件照旧广播（前端据此渲染内容），再补一次 Go 侧的"把窗口推出来"。
	// 放在 Emit 之后：让前端的快速路径先跑，兜底只在它没动静时才有实际效果
	// （Show() 对已可见窗口是幂等的，不会造成重复显示）。
	//
	// 安静投递（自然停顿顺延到上限）不推全屏窗：它只应进通知与待处理卡片。
	if alertShowHook != nil && !payload.QuietDelivery {
		alertShowHook()
	}
}

// EmitChanged 广播 reminder:changed，让各页面重新拉取列表。
func (e appEmitter) EmitChanged(payload reminder.ChangedPayload) {
	if e.app == nil {
		return
	}
	e.app.Event.Emit(reminder.ChangedEventName(), payload)
}

// bindNotificationResponses 接管系统通知的交互回调。
//
// 三条路径，同一个出口：
//   - 「完成」按钮 → CompleteReminder；
//   - 「推迟」按钮 → SnoozeReminder（分钟数取设置页的配置）；
//   - 点通知本体（系统默认动作）→ 唤起主窗口，让用户在首页卡片上处理。
//
// 通知 ID 的格式是 reminder-fire-<fireID>（见 reminder.DueFire.NotificationID），
// 反查不出来说明这条通知不是提醒引擎发的（例如设置页的「发送测试通知」），直接忽略。
func (a *App) bindNotificationResponses() {
	if a.notify == nil {
		return
	}
	a.notify.OnNotificationResponse(func(result notifications.NotificationResult) {
		if result.Error != nil {
			slog.Warn("通知交互回调异常", "err", result.Error)
			return
		}
		fireID := reminder.FireIDFromNotificationID(result.Response.ID)
		if fireID == 0 {
			return
		}
		switch result.Response.ActionIdentifier {
		case notificationDoneActionID:
			if err := a.svc.CompleteReminder(fireID); err != nil {
				slog.Warn("从通知点「完成」失败", "fire_id", fireID, "err", err)
				return
			}
			a.emitReminderChanged(reminder.ReasonCheckIn)
		case notificationSnoozeActionID:
			minutes := a.currentReminderSettings().SnoozeMinutes
			if err := a.svc.SnoozeReminder(fireID, minutes); err != nil {
				slog.Warn("从通知点「推迟」失败", "fire_id", fireID, "err", err)
				return
			}
			a.emitReminderChanged(reminder.ReasonCheckIn)
		default:
			// 点通知本体：把窗口拉到前面，用户在首页卡片上处理。
			a.restoreWindow()
		}
	})
}
