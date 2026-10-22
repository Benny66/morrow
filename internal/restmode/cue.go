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
	// WarningCues 是该 kind 的三档预警措辞，下标与 warningCueTier 的返回值对应。
	// 【为什么放进同一张表】倒计时里做什么、到点前一分钟说什么，是同一个 kind 的两面；
	// 分两个 map 就会漂，新增休息类时尤其容易只补一边。
	WarningCues [3]string
}

// cues 是「kind → 引导动作 + 预警措辞」的单一真相。
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
		WarningCues: [3]string{
			"1 分钟后该起身拉伸了", "半分钟后该起身拉伸了", "马上该起身拉伸了",
		},
	},
	model.KindMove: {
		Text:     "离开工位走动，去接杯水或走到窗边再回来；不用赶时间。",
		Duration: 180 * time.Second,
		WarningCues: [3]string{
			"1 分钟后该走动一下了", "半分钟后该走动一下了", "马上该走动一下了",
		},
	},
	model.KindRest: {
		Text:     "望向 6 米外的地方 20 秒，同时慢慢吸气 4 秒、呼气 6 秒；重复到倒计时结束。",
		Duration: 60 * time.Second,
		WarningCues: [3]string{
			"1 分钟后让眼睛休息一下", "半分钟后让眼睛休息一下", "马上让眼睛休息一下",
		},
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

// 预警措辞的两个下界：在 WarningLeadTime 之内再切两刀。
//
// 边界归给更短的一档——剩 30 整秒说"半分钟后"、剩 10 整秒与 0 秒说"马上"，
// 与「最后一档含 0」一起保证"剩余越少、话说得越急"是单调的，不会来回跳。
const (
	warningCueHalf = 30 * time.Second
	warningCueSoon = 10 * time.Second
)

// warningCuesFallback 兜住不在表里的 kind：tickWarnings 只会送 stand/move/rest，
// 但休息类常量若新增了成员而这里没补，兜底句比让气泡空着（或 panic）诚实。
var warningCuesFallback = [3]string{"1 分钟后开始休息", "半分钟后开始休息", "马上开始休息"}

// warningCueFor 返回预警阶段的气泡文案。
//
// 【为什么要按剩余时间分三档，而不是写死一句「1 分钟后」】预警的语义是"下一分钟真的会弹"，
// 但它在 60 秒里每一秒都被读一次：剩 8 秒时还说"1 分钟后"，用户按这句话做的安排就错了。
// 三档（大于半程 / 半程以内 / 最后 10 秒）既不说谎，也不需要把宠物窗口的 4 秒轮询加密成
// 逐秒——那个代价换来的是没人能读准的跳动数字（见 ADR 0012 的备选方案）。
//
// 【为什么整个仓库只有这一处】措辞的单一出处就是这张表 + 这个函数：宠物气泡与菜单栏都取
// Snapshot().CueText，前端只渲染不判档（AGENTS.md 铁律 1）。
// 越界副本由 internal/guard/warning_wording_test.go 拦住。
func warningCueFor(kind string, remaining time.Duration) string {
	cue, ok := cues[kind]
	if !ok {
		cue.WarningCues = warningCuesFallback
	}
	return cue.WarningCues[warningCueTier(remaining)]
}

// warningCueTier 把剩余时间映射到三档下标。
func warningCueTier(remaining time.Duration) int {
	switch {
	case remaining > warningCueHalf:
		return 0
	case remaining > warningCueSoon:
		return 1
	}
	return 2
}
