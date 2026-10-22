package sit

import (
	"strconv"
	"strings"
	"time"
)

// BubbleTTL 是待展示气泡的默认有效期；未确认展示的气泡过期后不占预算。
const BubbleTTL = time.Minute

// NudgeTitle 是软提醒气泡标题。
const NudgeTitle = "久坐提醒"

// GreetingTitle 是欢迎回来气泡标题。
const GreetingTitle = "回来啦"

// MemoryTitle 是记忆气泡标题（009/010 使用）。
const MemoryTitle = "小满记得"

type textVariant struct {
	ID   string
	Text string
}

// nudgeVariants 是软提醒的三档语气文案；同一模板短期不重复。
var nudgeVariants = map[Tone][]textVariant{
	ToneSassy: {
		{ID: "nudge.sassy.base", Text: "已经连续坐了 {n} 分钟。起来走两步？"},
		{ID: "nudge.sassy.2", Text: "{n} 分钟没挪窝了，椅子快以为你长在上面。起来活动一下？"},
		{ID: "nudge.sassy.3", Text: "坐了 {n} 分钟。姿势再稳，也该让腿出来透口气，起来走两步？"},
	},
	ToneLively: {
		{ID: "nudge.lively.base", Text: "坐了一小会儿啦（{n} 分钟），要不要起来晃晃？"},
		{ID: "nudge.lively.2", Text: "已经 {n} 分钟啦，起来伸个懒腰、走两步？"},
		{ID: "nudge.lively.3", Text: "{n} 分钟打卡！起来活动一下，回来继续。"},
	},
	ToneGentle: {
		{ID: "nudge.gentle.base", Text: "你已经连续用电脑 {n} 分钟了，起来活动一下吧。"},
		{ID: "nudge.gentle.2", Text: "坐得有点久了（{n} 分钟），起来走两步、喝口水吧。"},
		{ID: "nudge.gentle.3", Text: "连续坐了 {n} 分钟，站起来活动一下会更舒服。"},
	},
}

// greetingVariants 是欢迎回来文案；greetingShort 用于不足一分钟的休息。
var greetingVariants = map[Tone][]textVariant{
	ToneSassy: {
		{ID: "greeting.sassy.base", Text: "回来啦，这次休息了 {n} 分钟。还行。"},
		{ID: "greeting.sassy.2", Text: "回来啦，{n} 分钟的休息，算你有点自觉。"},
	},
	ToneLively: {
		{ID: "greeting.lively.base", Text: "回来啦，晃了 {n} 分钟，精神多了吧？"},
		{ID: "greeting.lively.2", Text: "欢迎回来！{n} 分钟的活动，状态回满。"},
	},
	ToneGentle: {
		{ID: "greeting.gentle.base", Text: "回来啦，这次休息了 {n} 分钟。"},
		{ID: "greeting.gentle.2", Text: "欢迎回来，休息了 {n} 分钟，慢慢来。"},
	},
}

var greetingShort = map[Tone]string{
	ToneSassy:  "回来啦。还行。",
	ToneLively: "回来啦，精神点！",
	ToneGentle: "回来啦。",
}

var breakStartTemplates = map[Tone]string{
	ToneSassy:  "去吧，我等你。别磨蹭太久。",
	ToneLively: "去吧去吧，我在这儿等你。",
	ToneGentle: "去吧，我等你。",
}

var muteConfirmTemplates = map[Tone]string{
	ToneSassy:  "行，今天不催你。明天见。",
	ToneLively: "收到，今天先不打扰你～",
	ToneGentle: "好，今天不打扰你。",
}

var toneConfirmTemplates = map[Tone]string{
	ToneSassy:  "行，我按毒舌来。",
	ToneLively: "好嘞，我换个活泼的说法。",
	ToneGentle: "好，我好好说。",
}

// NudgeTemplateID / GreetingTemplateID 用于日志与去重（默认取第一条）。
func NudgeTemplateID(tone Tone) string {
	variants := nudgeVariants[NormalizeTone(string(tone))]
	if len(variants) == 0 {
		return "nudge.sassy.base"
	}
	return variants[0].ID
}

func GreetingTemplateID(tone Tone) string {
	variants := greetingVariants[NormalizeTone(string(tone))]
	if len(variants) == 0 {
		return "greeting.sassy.base"
	}
	return variants[0].ID
}

// GreetingStartTemplateID 返回「去吧，我等你」的模板 ID。
func GreetingStartTemplateID(tone Tone) string {
	return "greeting.start." + string(NormalizeTone(string(tone)))
}

func MuteConfirmTemplateID(tone Tone) string {
	return "say.mute." + string(NormalizeTone(string(tone)))
}

func ToneConfirmTemplateID(tone Tone) string {
	return "say.tone." + string(NormalizeTone(string(tone)))
}

// BreakStartText 是用户点「起来走走」后的即时回执。
func BreakStartText(tone Tone) string {
	return breakStartTemplates[NormalizeTone(string(tone))]
}

func MuteConfirmText(tone Tone) string {
	return muteConfirmTemplates[NormalizeTone(string(tone))]
}

func ToneConfirmText(tone Tone) string {
	return toneConfirmTemplates[NormalizeTone(string(tone))]
}

// SelectNudgeText 选择一条近期未用过的软提醒文案；返回文案与模板 ID。
func SelectNudgeText(continuous time.Duration, tone Tone, recentIDs []string) (string, string) {
	mins := int(continuous.Round(time.Minute) / time.Minute)
	if mins < 1 {
		mins = 1
	}
	tone = NormalizeTone(string(tone))
	variants := nudgeVariants[tone]
	if len(variants) == 0 {
		variants = nudgeVariants[ToneSassy]
	}
	chosen := variants[0]
	for _, variant := range variants {
		if !containsID(recentIDs, variant.ID) {
			chosen = variant
			break
		}
	}
	return strings.ReplaceAll(chosen.Text, "{n}", strconv.Itoa(mins)), chosen.ID
}

// NudgeText 生成一条基础软提醒文案；只使用时长，不出现应用名。
func NudgeText(continuous time.Duration, tone Tone) string {
	text, _ := SelectNudgeText(continuous, tone, nil)
	return text
}

// SelectGreetingText 选择欢迎回来文案；不足一分钟用短句。
func SelectGreetingText(duration time.Duration, tone Tone, recentIDs []string) (string, string) {
	tone = NormalizeTone(string(tone))
	mins := int(duration.Round(time.Minute) / time.Minute)
	if mins < 1 {
		return greetingShort[tone], "greeting." + string(tone) + ".short"
	}
	variants := greetingVariants[tone]
	if len(variants) == 0 {
		variants = greetingVariants[ToneSassy]
	}
	chosen := variants[0]
	for _, variant := range variants {
		if !containsID(recentIDs, variant.ID) {
			chosen = variant
			break
		}
	}
	return strings.ReplaceAll(chosen.Text, "{n}", strconv.Itoa(mins)), chosen.ID
}

// GreetingText 生成欢迎回来文案；duration 为用户自报的休息时长。
func GreetingText(duration time.Duration, tone Tone) string {
	text, _ := SelectGreetingText(duration, tone, nil)
	return text
}

func containsID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

// BubbleTitle 按气泡类型取标题。
func BubbleTitle(kind BubbleKind) string {
	switch kind {
	case BubbleGreeting:
		return GreetingTitle
	case BubbleMemory:
		return MemoryTitle
	default:
		return NudgeTitle
	}
}

// BubbleTitleFor 对「去吧」这类开场问候使用不同标题。
func BubbleTitleFor(kind BubbleKind, templateID string) string {
	if kind == BubbleGreeting && strings.HasPrefix(templateID, "greeting.start") {
		return "去吧"
	}
	return BubbleTitle(kind)
}

// BubbleActions 返回气泡动作集合；前端只按 ID 分发。
func BubbleActions(kind BubbleKind) []ActionID {
	switch kind {
	case BubbleGreeting:
		return []ActionID{ActionDismiss, ActionOpenApp}
	case BubbleMemory:
		return []ActionID{ActionDismiss, ActionOpenApp, ActionMore}
	default:
		return []ActionID{ActionStartBreak, ActionSnooze10, ActionMore}
	}
}
