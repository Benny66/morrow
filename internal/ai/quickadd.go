package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"morrow/internal/apperr"
	"morrow/internal/model"
	"morrow/internal/reminder"
)

// maxQuickAddDrafts 是单次快速添加最多保留的草稿数。
//
// 一张图/一段长文可能让模型吐出几十条；预览一次展示不完，用户也审不过来。
// 超出部分明确报数，让用户拆分输入，而不是静默丢弃。
const maxQuickAddDrafts = 10

// QuickAddDraft 是快速添加生成的一条草稿。
//
// 一次性草稿用 OnceAt（绝对 Unix 秒）；周期草稿用 IntervalMin/Window/DailyTarget。
// 两者由 ScheduleType 区分，避免"0 值到底是没填还是每天一次"的歧义。
type QuickAddDraft struct {
	Title        string `json:"title"`
	Content      string `json:"content"`
	Kind         string `json:"kind"`
	ScheduleType string `json:"schedule_type"`
	// OnceAt 一次性草稿的绝对触发时刻（Unix 秒）；周期草稿为 0。
	OnceAt int64 `json:"once_at"`
	// IntervalMin 周期草稿的间隔分钟数；0 = 每日单点。
	IntervalMin int `json:"interval_min"`
	// WindowStart / WindowEnd 周期草稿的生效时段。
	WindowStart string `json:"window_start"`
	WindowEnd   string `json:"window_end"`
	// DailyTarget 周期草稿的每日目标次数。
	DailyTarget int `json:"daily_target"`
}

// QuickAddPlan 是快速添加的一次生成结果。
type QuickAddPlan struct {
	Summary string          `json:"summary"`
	Drafts  []QuickAddDraft `json:"drafts"`
	// Omitted 是超过上限被省略的条数；>0 时前端必须提示用户拆分输入。
	Omitted int `json:"omitted"`
}

// quickAddRaw 是模型返回的原始结构：日期与钟点分开，便于容错解析。
type quickAddRaw struct {
	Summary string             `json:"summary"`
	Drafts  []quickAddRawDraft `json:"drafts"`
}

type quickAddRawDraft struct {
	Title        string `json:"title"`
	Content      string `json:"content"`
	Kind         string `json:"kind"`
	ScheduleType string `json:"schedule_type"`
	OnceDate     string `json:"once_date"`
	OnceTime     string `json:"once_time"`
	IntervalMin  *int   `json:"interval_min"`
	WindowStart  string `json:"window_start"`
	WindowEnd    string `json:"window_end"`
	DailyTarget  int    `json:"daily_target"`
}

// ParseQuickAddText 从模型返回文本中抽出快速添加草稿，并归一化/解析时间。
//
// now 由调用方注入：一次性提醒的「今天/明天」与「是否已过去」都依赖它，
// 测试与生产必须用同一个入口，避免测试依赖真实时钟。
func ParseQuickAddText(raw string, now time.Time) (QuickAddPlan, error) {
	text := extractJSONObject(stripCodeFence(raw))
	if text == "" {
		return QuickAddPlan{}, fmt.Errorf("未在返回内容中找到 JSON 对象")
	}
	var parsed quickAddRaw
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return QuickAddPlan{}, fmt.Errorf("JSON 解析失败: %w", err)
	}
	plan := QuickAddPlan{
		Summary: clampRunes(strings.TrimSpace(parsed.Summary), maxSummaryRunes),
		Drafts:  make([]QuickAddDraft, 0, len(parsed.Drafts)),
	}
	if len(parsed.Drafts) > maxQuickAddDrafts {
		plan.Omitted = len(parsed.Drafts) - maxQuickAddDrafts
	}
	for index, rawDraft := range parsed.Drafts {
		if index >= maxQuickAddDrafts {
			break
		}
		draft, err := normalizeQuickAddDraft(rawDraft, now)
		if err != nil {
			return QuickAddPlan{}, fmt.Errorf("第 %d 条：%w", index+1, err)
		}
		plan.Drafts = append(plan.Drafts, draft)
	}
	if len(plan.Drafts) == 0 {
		return QuickAddPlan{}, fmt.Errorf("计划里没有任何可用的提醒项")
	}
	return plan, nil
}

// normalizeQuickAddDraft 归一化单条草稿并解析一次性时间。
func normalizeQuickAddDraft(raw quickAddRawDraft, now time.Time) (QuickAddDraft, error) {
	title := clampRunes(strings.TrimSpace(raw.Title), maxTitleRunes)
	if title == "" {
		title = defaultTitle
	}
	content := clampRunes(strings.TrimSpace(raw.Content), maxContentRunes)
	if content == "" {
		content = title
	}
	kind := model.NormalizeKind(raw.Kind)
	scheduleType := strings.ToLower(strings.TrimSpace(raw.ScheduleType))
	if scheduleType == "" {
		if strings.TrimSpace(raw.OnceTime) != "" {
			scheduleType = "once"
		} else {
			scheduleType = "daily"
		}
	}

	switch scheduleType {
	case "once":
		onceAt, err := resolveOnceAt(raw, now)
		if err != nil {
			return QuickAddDraft{}, err
		}
		return QuickAddDraft{
			Title:        title,
			Content:      content,
			Kind:         kind,
			ScheduleType: "once",
			OnceAt:       onceAt,
		}, nil
	case "daily":
		field := ReminderDraft{
			Title:       title,
			Content:     content,
			Kind:        kind,
			WindowStart: raw.WindowStart,
			WindowEnd:   raw.WindowEnd,
			DailyTarget: raw.DailyTarget,
		}
		if raw.IntervalMin != nil {
			field.IntervalMin = *raw.IntervalMin
			field.intervalMinSet = true
		}
		normalized := normalizeDraft(field)
		return QuickAddDraft{
			Title:        normalized.Title,
			Content:      normalized.Content,
			Kind:         normalized.Kind,
			ScheduleType: "daily",
			IntervalMin:  normalized.IntervalMin,
			WindowStart:  normalized.WindowStart,
			WindowEnd:    normalized.WindowEnd,
			DailyTarget:  normalized.DailyTarget,
		}, nil
	default:
		return QuickAddDraft{}, fmt.Errorf("未知的提醒类型 %q", raw.ScheduleType)
	}
}

// resolveOnceAt 把「日期 + 钟点」解析成绝对 Unix 秒。
//
// 缺日期时按用户确认的规则补：今天该时刻未到 → 今天；已过 → 明天。
// 显式日期已过去则报错——不能把用户明确说的过去时间静默改成未来。
func resolveOnceAt(raw quickAddRawDraft, now time.Time) (int64, error) {
	minutes, ok := reminder.ParseClock(raw.OnceTime)
	if !ok {
		return 0, fmt.Errorf("时间需为 HH:MM（收到 %q）", raw.OnceTime)
	}
	hour, minute := minutes/60, minutes%60
	dateText := strings.TrimSpace(raw.OnceDate)
	if dateText == "" {
		candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		if !candidate.After(now) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return candidate.Unix(), nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", dateText, now.Location())
	if err != nil {
		return 0, fmt.Errorf("日期需为 YYYY-MM-DD（收到 %q）", dateText)
	}
	candidate := time.Date(parsed.Year(), parsed.Month(), parsed.Day(), hour, minute, 0, 0, now.Location())
	if !candidate.After(now) {
		return 0, fmt.Errorf("时间 %s %s 已过去", dateText, raw.OnceTime)
	}
	return candidate.Unix(), nil
}

// quickAddRetryInstruction 是快速添加第二次尝试时追加的格式要求。
const quickAddRetryInstruction = `上一次的回答不是合法 JSON，无法被程序解析。请【只】输出一个 JSON 对象，
不要任何解释文字、不要 Markdown 代码围栏、不要在 JSON 前后加任何字符。
JSON 必须以 { 开头、以 } 结尾，字段名与结构严格如下：
{"summary":"一句话总结","drafts":[{"title":"标题","content":"提醒正文","kind":"枚举值","schedule_type":"once","once_date":"2026-10-21","once_time":"16:00"},{"title":"喝水","content":"每小时喝一杯水","kind":"water","schedule_type":"daily","interval_min":60,"window_start":"09:00","window_end":"18:00","daily_target":8}]}`

// buildQuickAddSystemPrompt 生成快速添加的 system prompt。
//
// 当前日期/时间/时区必须写进提示词：模型没有时钟，缺了它就无法把「明天 4 点」
// 解析成具体日期。kind 枚举仍从 model.AllKinds() 取，保持单一真相。
func buildQuickAddSystemPrompt(now time.Time) string {
	kindList := strings.Join(model.AllKinds(), "|")
	return fmt.Sprintf(`你是一个提醒快速添加解析器。用户会用一句话描述一个或多个事件。
当前本地时间：%s（时区 %s，UTC%s）。
请据此把用户描述翻译成结构化草稿。

输出要求（违反任何一条都会导致解析失败）：
1. 只输出一个 JSON 对象，不要 Markdown 代码围栏，不要在 JSON 前后加任何解释文字。
2. 结构必须是：
   {"summary":"一句话总结","drafts":[{"title":"...","content":"...","kind":"...","schedule_type":"once","once_date":"YYYY-MM-DD","once_time":"HH:MM"}]}
   周期草稿改为：{"title":"...","content":"...","kind":"...","schedule_type":"daily","interval_min":60,"window_start":"09:00","window_end":"18:00","daily_target":8}
3. 含重复词（每天、每隔、每小时、每周…）的长期习惯用 schedule_type="daily"；
   单一时刻/一次性事件用 "once"；无法判断时优先 "once"，并在 summary 说明判断依据。
4. once 草稿必须给出 once_date / once_time，且必须是解析后的具体值，禁止输出「明天」等相对词。
5. 用户没提日期时：今天该时刻还没到就是今天，已经过了就是明天。
   用户明确给出过去的时间时不要编造未来时间；返回空 drafts，并在 summary 说明。
6. daily 草稿必须给出 interval_min / window_start / window_end / daily_target。
   interval_min=0 表示每天只在 window_start 触发一次；window_end 必须晚于 window_start，不支持跨午夜。
7. kind 必须取以下之一：%s。
8. title 不超过 12 个汉字，content 不超过 40 个汉字，语气温和、具体、可执行。
9. 草稿条数控制在 1–10 条；用户描述里的时间、间隔、数量都是硬约束。`,
		now.Format("2006-01-02 15:04"), now.Format("MST"), now.Format("-07:00"), kindList)
}

// GenerateQuickAdd 让模型把一句话 + 可选附件解析成快速添加草稿。
//
// 与 GeneratePlan 相同的容错策略：首次严格 schema，解析失败重试一次并追加
// 更严厉的格式要求；仍失败则把模型原始片段放进 Detail。
func (c *Client) GenerateQuickAdd(ctx context.Context, description string, attachments []Attachment, now time.Time) (QuickAddPlan, error) {
	desc := strings.TrimSpace(description)
	if desc == "" && !hasAttachment(attachments) {
		return QuickAddPlan{}, apperr.Validation("请先写一句提醒内容，或添加一张图片")
	}
	if c.settings.APIKey == "" {
		return QuickAddPlan{}, apperr.Validation("请先在设置页填写 AI API Key")
	}

	baseText := desc
	if baseText == "" {
		baseText = defaultAttachmentPrompt
	}
	systemPrompt := buildQuickAddSystemPrompt(now)
	var lastRaw string
	var lastDetail string
	for attempt := 1; attempt <= 2; attempt++ {
		userText := baseText
		if attempt > 1 {
			userText = baseText + "\n\n" + quickAddRetryInstruction
		}
		raw, err := c.doChat(ctx, []chatMessage{
			textMessage("system", systemPrompt),
			buildUserMessage(userText, attachments),
		}, 2048)
		if err != nil {
			return QuickAddPlan{}, err
		}
		lastRaw = raw
		plan, parseErr := ParseQuickAddText(raw, now)
		if parseErr == nil {
			if strings.TrimSpace(plan.Summary) == "" {
				plan.Summary = fmt.Sprintf("已解析 %d 条提醒", len(plan.Drafts))
			}
			return plan, nil
		}
		lastDetail = fmt.Sprintf("第 %d 次解析失败：%v", attempt, parseErr)
	}
	failure := apperr.New(apperr.CodeInternal, "AI 返回的提醒格式不正确，请调整描述后重试")
	failure.Detail = lastDetail + "；模型原始返回片段：" + snippet([]byte(lastRaw))
	return QuickAddPlan{}, failure
}
