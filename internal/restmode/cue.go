package restmode

import (
	"morrow/internal/model"
	"time"
)

// Cue 是一种休息类提醒对应的内置引导动作。
//
// 【为什么文案与时长绑在一起】两者是同一个产品决定的两面：「肩颈拉伸 2 分钟」
// 里的 2 分钟不是随便配的，是给这个动作留的最小可用时长；分两个 map 会漂。
type Cue struct {
	// Text 一句引导文案（前端直接显示，不解析）。
	Text string
	// Duration 倒计时总时长。
	Duration time.Duration
}

// cues 是「kind → 引导动作」的单一真相。
//
// 只覆盖 stand / move / rest；water / toilet / custom 不进休息模式，
// 见 model.Reminder.Kind 的枚举注释与 spec 决策表。
//
// 【文案为什么写成「先…再…」而不是纯动作名】用户 120 秒里如果只看到「肩颈拉伸」
// 四个字，会不知道往哪几组方向做；一句"缓缓转头 + 耸肩放松"就是最低可用指导。
// 但绝不写成医疗建议（"每天 3 次治疗颈椎病"）——ADR 0003 划的边界。
var cues = map[string]Cue{
	model.KindStand: {
		Text:     "起身站直，缓缓转头 5 秒，再耸肩放松 10 秒；重复几轮到倒计时结束。",
		Duration: 120 * time.Second,
	},
	model.KindMove: {
		Text:     "离开工位走动，去接杯水或走到窗边再回来；不用赶时间。",
		Duration: 180 * time.Second,
	},
	model.KindRest: {
		Text:     "望向 6 米外的地方 20 秒，同时慢慢吸气 4 秒、呼气 6 秒；重复到倒计时结束。",
		Duration: 60 * time.Second,
	},
}

// CueFor 返回 kind 对应的引导动作；不属于休息类时返回零值 Cue。
//
// 【为什么不返回错误】调用点是在提醒 fire 后决定走哪条路；返回零值 + IsRestKind
// 判空是同一件事的两种问法，让引擎里 dispatch 保持一次判断。
func CueFor(kind string) Cue {
	return cues[kind]
}

// IsRestKind 判断 kind 是否属于休息模式覆盖的三类（stand / move / rest）。
func IsRestKind(kind string) bool {
	_, ok := cues[kind]
	return ok
}
