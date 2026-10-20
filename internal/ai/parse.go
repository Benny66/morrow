package ai

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"morrow/internal/model"
	"morrow/internal/reminder"
)

// 解析容错用的默认值。
//
// 【为什么给"能立刻用起来"的默认值而不是零值】间隔 0 会被当成一次性提醒、
// 时段空串会让调度器什么都不做——那等于给用户一个「生成了但永远不响」的计划，
// 比明确报错更糟。宁可让它按最保守的常规值跑起来，用户还能在计划页改。
const (
	// defaultIntervalMin 模型漏填 interval_min 时采用的间隔。
	// 显式写 0 是合法的「每日单点」，不会被回落。
	defaultIntervalMin = 60
	// defaultWindowStart / defaultWindowEnd 时段缺失或非法时回落的常规工作时段。
	defaultWindowStart = "09:00"
	// defaultWindowEnd 见 defaultWindowStart。
	defaultWindowEnd = "18:00"
	// defaultTitle 标题缺失时的兜底文案。
	defaultTitle = "提醒"
	// maxIntervalMin 是 AI 输出的间隔上限（12 小时）。超出视为模型误答，回落默认值。
	maxIntervalMin = 720
	// maxTitleRunes 标题字符上限（与 model.Reminder.Title 的列宽一致）。
	maxTitleRunes = 50
	// maxContentRunes 正文字符上限。
	maxContentRunes = 200
	// maxSummaryRunes 总结字符上限。
	maxSummaryRunes = 200
	// maxDailyTarget 每日目标上限，防止模型输出荒谬的目标值。
	maxDailyTarget = 99
	// maxRawSnippet 是错误详情里附带的模型原始返回片段长度上限。
	maxRawSnippet = 600
)

// codeFenceRe 匹配 Markdown 代码围栏（```json ... ``` / ``` ... ```）。
// 用 (?s) 让 . 跨行匹配；贪婪与非贪婪在单围栏场景下等价，这里用非贪婪更稳。
var codeFenceRe = regexp.MustCompile("(?s)```[a-zA-Z]*\\s*(.*?)\\s*```")

// ParsePlanText 从模型返回的文本里抽出计划 JSON 并归一化字段。
//
// 【为什么需要容错解析，而不是直接 json.Unmarshal】
// 模型（尤其是便宜的小模型）几乎总会加一点包装：```json 代码围栏、
// 「好的，这是你的计划：」这样的开场白、结尾的一句补充说明、甚至中英文混排。
// 这些包装在语义上完全无害，却会让严格解析 100% 失败。故按以下顺序逐档放宽，
// 每一档都只做「能确定的还原」，不猜测内容：
//  1. 去掉 ``` 代码围栏；
//  2. 截取首个 '{' 到末个 '}'（容忍前后的废话）；
//  3. 字段级容错：缺字段给默认值、非法时段回落到常规时段、非法 kind 落到 custom。
//
// 仍然解析不出来才算失败——失败信号交给调用方（GeneratePlan）决定是否重试一次。
func ParsePlanText(raw string) (Plan, error) {
	text := extractJSONObject(stripCodeFence(raw))
	if text == "" {
		return Plan{}, fmt.Errorf("未在返回内容中找到 JSON 对象")
	}

	var parsed Plan
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return Plan{}, fmt.Errorf("JSON 解析失败: %w", err)
	}

	plan := normalizePlan(parsed)
	if len(plan.Reminders) == 0 {
		return Plan{}, fmt.Errorf("计划里没有任何提醒项")
	}
	return plan, nil
}

// stripCodeFence 去掉 Markdown 代码围栏；没有围栏时原样返回（去首尾空白）。
func stripCodeFence(s string) string {
	if match := codeFenceRe.FindStringSubmatch(s); match != nil {
		return strings.TrimSpace(match[1])
	}
	return strings.TrimSpace(s)
}

// extractJSONObject 截取首个 '{' 到末个 '}' 之间的内容；不存在时返回空串。
//
// 用「末个 }」而不是括号配对：配对解析要处理字符串里的转义与嵌套，写错一次就是
// 一个静默的错结果；而模型输出里出现两个顶层 JSON 对象的概率极低，
// 且真出现时取外层反而更容易解析成功。
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

// normalizePlan 归一化整份计划：逐条归一化 + 去重。
func normalizePlan(plan Plan) Plan {
	out := Plan{
		Summary:   clampRunes(strings.TrimSpace(plan.Summary), maxSummaryRunes),
		Reminders: make([]ReminderDraft, 0, len(plan.Reminders)),
	}
	seen := make(map[string]bool, len(plan.Reminders))
	for _, draft := range plan.Reminders {
		normalized := normalizeDraft(draft)
		// 同一标题 + 同一时段视为重复项：模型偶尔会把同一条提醒拆成两行输出，
		// 不去重的话首页进度条会出现两个「喝水」在各自计数的怪状。
		key := normalized.Title + "|" + normalized.WindowStart + "|" + normalized.WindowEnd
		if seen[key] {
			continue
		}
		seen[key] = true
		out.Reminders = append(out.Reminders, normalized)
	}
	return out
}

// normalizeDraft 归一化单条提醒项。
func normalizeDraft(draft ReminderDraft) ReminderDraft {
	out := ReminderDraft{
		Title:       clampRunes(strings.TrimSpace(draft.Title), maxTitleRunes),
		Content:     clampRunes(strings.TrimSpace(draft.Content), maxContentRunes),
		Kind:        model.NormalizeKind(draft.Kind),
		IntervalMin: draft.IntervalMin,
		WindowStart: strings.TrimSpace(draft.WindowStart),
		WindowEnd:   strings.TrimSpace(draft.WindowEnd),
		DailyTarget: draft.DailyTarget,
	}

	if out.Title == "" {
		out.Title = defaultTitle
	}
	// 正文为空时用标题兜底：通知正文空着会显示成一条只有标题的空通知。
	if out.Content == "" {
		out.Content = out.Title
	}

	if !reminder.ValidateWindow(out.WindowStart, out.WindowEnd) {
		out.WindowStart, out.WindowEnd = defaultWindowStart, defaultWindowEnd
	}
	// 归一化钟点写法（"9:00" → "09:00"）：前端的时刻选择器只认两位小时。
	if start, ok := reminder.ParseClock(out.WindowStart); ok {
		out.WindowStart = reminder.FormatClock(start)
	}
	if end, ok := reminder.ParseClock(out.WindowEnd); ok {
		out.WindowEnd = reminder.FormatClock(end)
	}

	// 显式 0 是每日单点；只有字段缺失（intervalMinSet=false）或越界才回落默认间隔。
	if !draft.intervalMinSet {
		out.IntervalMin = defaultIntervalMin
	} else if out.IntervalMin < 0 || out.IntervalMin > maxIntervalMin {
		out.IntervalMin = defaultIntervalMin
	}
	if out.DailyTarget < 0 {
		out.DailyTarget = 0
	}
	if out.DailyTarget > maxDailyTarget {
		out.DailyTarget = maxDailyTarget
	}
	return out
}

// clampRunes 按字符数截断（不是字节数，中文按 3 字节算会截出乱码）。
func clampRunes(s string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit])
}

// snippet 把原始返回压成单行片段，供 apperr.Detail 使用。
//
// 压成单行是刻意的：Detail 会进日志，多行内容会破坏行式存储的解析；
// 截断则是为了防止把几 KB 的模型输出灌进错误信息。
func snippet(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	return clampRunes(text, maxRawSnippet)
}
