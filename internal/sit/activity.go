package sit

import "time"

// 活动档位：回答「你是真的在干活，还是只是没关窗口」。
//
// 【为什么由空闲时长直接算，而不是在 Tracker 里加状态】
// pause.GetIdleDuration 读的是一个【由任意输入重置】的单调时钟，
// 所以「idle < 3 分钟」本身就等价于「最近 3 分钟里有过输入」——
// 不需要滑动窗口、不需要跨 tick 累积、不需要持久化。
// 放进 Tracker 会给同一件事造出第二个状态机（铁律 1）。
//
// 本文件只有纯函数：输入是时长与类别，输出是档位。没有任何平台调用。
const (
	// ActivityBusyMax 以内视为正在连续输入。
	//
	// 取值 20s 的依据：运行层每 5 秒采样一次，20s 意味着最近 4 个采样点里
	// 至少命中一次输入。再抬高会把「停下来想 30 秒」误判成不在场。
	ActivityBusyMax = 20 * time.Second

	// ActivityRecentMax 以内视为「刚动过，可能在看/在想」。
	//
	// 取值 3 分钟刻意比 FocusSwitchGrace（2 分钟，切走宽限）略宽：
	// 看代码两分钟不动手是常态，必须仍算「在场」，否则宠物姿态会持续闪烁。
	ActivityRecentMax = 3 * time.Minute
)

// ActivityLevel 是活动档位。
type ActivityLevel string

const (
	// ActivityBusy 表示正在连续输入。
	ActivityBusy ActivityLevel = "busy"
	// ActivityRecent 表示刚有输入、但此刻手是停的。
	ActivityRecent ActivityLevel = "recent"
	// ActivityAway 表示已离开键盘前。
	ActivityAway ActivityLevel = "away"
)

// ActivityFor 把系统空闲时长映射为活动档位。
//
// 负值按 busy 处理：那是时钟异常，此时「当作在忙」比「当作离开」安全——
// 后者会让宠物在用户眼前假装没看见他。
func ActivityFor(idle time.Duration) ActivityLevel {
	switch {
	case idle < ActivityBusyMax:
		return ActivityBusy
	case idle < ActivityRecentMax:
		return ActivityRecent
	default:
		return ActivityAway
	}
}

// Activity 是「用户此刻在做什么」的抽象。
//
// 【契约红线】只含行为类别与档位，不含应用名、应用标识或窗口标题——
// 与 Snapshot 的红线一致（见 contract.go）。
type Activity struct {
	Category Category      `json:"category"`
	Level    ActivityLevel `json:"level"`
}

// NewActivity 组装对外暴露的活动信息。
//
// category 为空表示采样本身没有成功（画像关闭、非工作时段、锁屏、长空闲、
// 前台是本应用或系统界面）——此时返回 nil，前端据此清空一切镜像姿态。
// 返回 nil 而不是一个零值结构，是为了让「没有活动」与「活动是 other」
// 在 JSON 里可区分：前者是 omitempty 的空缺，后者是显式的 other。
func NewActivity(category Category, idle time.Duration) *Activity {
	if category == "" {
		return nil
	}
	return &Activity{Category: category, Level: ActivityFor(idle)}
}
