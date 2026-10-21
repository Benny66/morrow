// Package scenariotemplate 是场景模板的单一真相。
//
// 模板是确定性内容：名称、描述、摘要与一组提醒项，不调用 AI。
// 选用模板时由 service 复制成普通场景；本包只负责目录、查找与个性化纯函数。
package scenariotemplate

import (
	"fmt"
	"strings"
	"time"

	"morrow/internal/apperr"
	"morrow/internal/model"
	"morrow/internal/reminder"
)

// Reminder 是模板里的一条提醒定义（落库前的草稿形态）。
type Reminder struct {
	Title       string `json:"title"`
	Content     string `json:"content"`
	Kind        string `json:"kind"`
	IntervalMin int    `json:"interval_min"`
	WindowStart string `json:"window_start"`
	WindowEnd   string `json:"window_end"`
	DailyTarget int    `json:"daily_target"`
}

// Template 是一个只读场景模板。
type Template struct {
	Key         string     `json:"key"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Summary     string     `json:"summary"`
	Reminders   []Reminder `json:"reminders"`
}

// Catalog 是模板目录与可选关注目标的唯一出口。
type Catalog struct {
	Goals     []string   `json:"goals"`
	Templates []Template `json:"templates"`
}

// Options 是个性化选项：为空表示按模板原样输出。
type Options struct {
	WorkStart string
	WorkEnd   string
	Goals     []string
}

// Preview 是个性化结果。
type Preview struct {
	Reminders    []Reminder `json:"reminders"`
	MissingGoals []string   `json:"missing_goals"`
	Valid        bool       `json:"valid"`
	// NextFireAt 由 service 注入；纯函数不读时钟，故这里恒为 0。
	NextFireAt int64 `json:"next_fire_at"`
}

// GoalKinds 返回首次引导可选的五类关注目标（顺序即展示顺序）。
//
// 与 model.AllKinds 的区别：这里刻意不包含 KindCustom——
// 「其他」不是一个可承诺的健康目标。
func GoalKinds() []string {
	return []string{
		model.KindWater,
		model.KindStand,
		model.KindMove,
		model.KindRest,
		model.KindToilet,
	}
}

// LoadCatalog 返回完整目录。每次调用都构造新切片，调用方修改不会污染目录。
func LoadCatalog() Catalog {
	return Catalog{Goals: GoalKinds(), Templates: catalogTemplates()}
}

// Find 按 key 查找模板；返回的模板同样是独立副本。
func Find(key string) (Template, bool) {
	key = strings.TrimSpace(key)
	for _, tpl := range catalogTemplates() {
		if tpl.Key == key {
			return tpl, true
		}
	}
	return Template{}, false
}

// Personalize 按关注目标过滤、按工作时段裁切，并把每日目标夹到窗口内可达次数。
//
// 【为什么不因缺失目标返回 error】首次引导要能展示「你选的护眼在这个时段没有
// 提醒」，让用户回去改；错误只用于入参本身非法。
func Personalize(tpl Template, opts Options) (Preview, error) {
	goals, err := normalizeGoals(opts.Goals)
	if err != nil {
		return Preview{}, err
	}
	workStart, workEnd, clip, err := normalizeWorkHours(opts.WorkStart, opts.WorkEnd)
	if err != nil {
		return Preview{}, err
	}

	selected := make(map[string]bool, len(goals))
	for _, goal := range goals {
		selected[goal] = true
	}

	preview := Preview{
		Reminders:    make([]Reminder, 0, len(tpl.Reminders)),
		MissingGoals: make([]string, 0, 2),
	}
	perGoal := make(map[string]int, len(goals))

	for _, item := range tpl.Reminders {
		if len(selected) > 0 && !selected[item.Kind] {
			continue
		}
		current := item
		if clip {
			start, end, ok := intersectClock(current.WindowStart, current.WindowEnd, workStart, workEnd)
			if !ok {
				continue
			}
			current.WindowStart, current.WindowEnd = start, end
		}

		fires := maxFiresPerDay(current.WindowStart, current.WindowEnd, current.IntervalMin)
		if fires == 0 {
			continue
		}
		if current.DailyTarget > fires {
			current.DailyTarget = fires
		}
		preview.Reminders = append(preview.Reminders, current)
		perGoal[current.Kind]++
	}

	for _, goal := range goals {
		if perGoal[goal] == 0 {
			preview.MissingGoals = append(preview.MissingGoals, goal)
		}
	}
	preview.Valid = len(preview.Reminders) > 0 && len(preview.MissingGoals) == 0
	return preview, nil
}

// normalizeGoals 校验并归一化关注目标：空 = 全部；非空必须恰好两个且互不相同。
func normalizeGoals(goals []string) ([]string, error) {
	if len(goals) == 0 {
		return nil, nil
	}
	if len(goals) != 2 {
		return nil, apperr.Validation("请恰好选择两个关注目标")
	}
	seen := make(map[string]bool, 2)
	out := make([]string, 0, 2)
	for _, raw := range goals {
		kind := strings.ToLower(strings.TrimSpace(raw))
		if !isGoalKind(kind) {
			return nil, apperr.Validation(fmt.Sprintf("关注目标非法: %s", raw))
		}
		if seen[kind] {
			return nil, apperr.Validation("关注目标不能重复")
		}
		seen[kind] = true
		out = append(out, kind)
	}
	return out, nil
}

// normalizeWorkHours 校验工作时段：两端要么都空（不裁切），要么都合法。
func normalizeWorkHours(start, end string) (string, string, bool, error) {
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	if start == "" && end == "" {
		return "", "", false, nil
	}
	if start == "" || end == "" {
		return "", "", false, apperr.Validation("工作时段需要同时提供开始与结束时间")
	}
	startMin, ok := reminder.ParseClock(start)
	if !ok {
		return "", "", false, apperr.Validation("工作时段格式非法，应为 HH:MM")
	}
	endMin, ok := reminder.ParseClock(end)
	if !ok {
		return "", "", false, apperr.Validation("工作时段格式非法，应为 HH:MM")
	}
	if endMin <= startMin {
		return "", "", false, apperr.Validation("工作时段结束时间必须晚于开始时间")
	}
	return reminder.FormatClock(startMin), reminder.FormatClock(endMin), true, nil
}

// intersectClock 返回两个时段的交集；交集为空或不足一分钟时 ok=false。
func intersectClock(start, end, workStart, workEnd string) (string, string, bool) {
	startMin, ok1 := reminder.ParseClock(start)
	endMin, ok2 := reminder.ParseClock(end)
	workStartMin, ok3 := reminder.ParseClock(workStart)
	workEndMin, ok4 := reminder.ParseClock(workEnd)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return "", "", false
	}
	if workStartMin > startMin {
		startMin = workStartMin
	}
	if workEndMin < endMin {
		endMin = workEndMin
	}
	if endMin <= startMin {
		return "", "", false
	}
	return reminder.FormatClock(startMin), reminder.FormatClock(endMin), true
}

// maxFiresPerDay 复用调度器的触发点算法，不另写间隔公式。
//
// 触发点数量与具体日期无关，故传固定日期只取点数。
func maxFiresPerDay(start, end string, interval int) int {
	day := time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local)
	return len(reminder.FirePoints(day, start, end, interval))
}

func isGoalKind(kind string) bool {
	for _, goal := range GoalKinds() {
		if kind == goal {
			return true
		}
	}
	return false
}

// kindCopy 是五类提醒的通用文案；模板只改节奏，不复制文案。
type kindCopy struct {
	Title   string
	Content string
}

var kindCopies = map[string]kindCopy{
	model.KindWater:  {Title: "喝水", Content: "起身倒一杯温水，慢慢喝完。"},
	model.KindStand:  {Title: "站起来", Content: "起身站直，活动肩颈，喝口水再坐下。"},
	model.KindMove:   {Title: "走动一下", Content: "离开工位走几步，去窗边看看远处。"},
	model.KindRest:   {Title: "让眼睛歇一会儿", Content: "看向远处，慢慢眨眼，放松眼睛。"},
	model.KindToilet: {Title: "去趟洗手间", Content: "起身走动，顺便补一杯水。"},
}

func newItem(kind string, intervalMin int, start, end string, target int) Reminder {
	copy := kindCopies[kind]
	return Reminder{
		Title:       copy.Title,
		Content:     copy.Content,
		Kind:        kind,
		IntervalMin: intervalMin,
		WindowStart: start,
		WindowEnd:   end,
		DailyTarget: target,
	}
}

// catalogTemplates 是五个模板的唯一出处（内容见 spec 5.1–5.3）。
func catalogTemplates() []Template {
	return []Template{
		{
			Key:         "sedentary-office",
			Name:        "久坐办公",
			Description: "我每天长时间坐在电脑前办公，经常一坐就是两三个小时。希望提醒我按时喝水、站起来活动、走动一下、让眼睛休息，也别忘了去洗手间。",
			Summary:     "标准坐班节奏：喝水、起身、走动、护眼与如厕均衡覆盖。",
			Reminders: []Reminder{
				newItem(model.KindWater, 120, "09:15", "17:15", 4),
				newItem(model.KindStand, 180, "09:30", "17:30", 2),
				newItem(model.KindMove, 240, "10:00", "16:00", 2),
				newItem(model.KindRest, 150, "10:45", "16:45", 2),
				newItem(model.KindToilet, 240, "11:00", "17:00", 0),
			},
		},
		{
			Key:         "meeting-heavy",
			Name:        "会议密集日",
			Description: "我一天里会议很多，经常连续几个小时离不开屏幕。希望提醒少而稳，不要频繁打断，但仍能保证喝水、起身、护眼和如厕。",
			Summary:     "低频稳妥：减少打断，适合会议多的一天。",
			Reminders: []Reminder{
				newItem(model.KindWater, 150, "10:00", "17:00", 2),
				newItem(model.KindStand, 240, "10:30", "16:30", 2),
				newItem(model.KindMove, 240, "11:15", "15:15", 2),
				newItem(model.KindRest, 180, "10:45", "16:45", 2),
				newItem(model.KindToilet, 300, "09:45", "15:45", 0),
			},
		},
		{
			Key:         "exam-sprint",
			Name:        "备考冲刺",
			Description: "我最近在备考或冲刺项目，常常连续专注很久。希望提醒我喝水、起身、走动和护眼，节奏不要太碎，但喝水和护眼要稳定。",
			Summary:     "长专注节奏：喝水与护眼稳定，减少碎片化打断。",
			Reminders: []Reminder{
				newItem(model.KindWater, 120, "09:15", "17:15", 4),
				newItem(model.KindStand, 240, "09:30", "17:30", 2),
				newItem(model.KindMove, 300, "10:15", "16:15", 2),
				newItem(model.KindRest, 120, "09:00", "17:00", 4),
				newItem(model.KindToilet, 300, "11:30", "16:30", 0),
			},
		},
		{
			Key:         "work-from-home",
			Name:        "居家办公",
			Description: "我在家办公，通勤和走动都变少了。希望提醒更频繁一些，督促我喝水、站起来、在屋里走动、让眼睛休息。",
			Summary:     "补水与走动加强：弥补居家办公消失的通勤步数。",
			Reminders: []Reminder{
				newItem(model.KindWater, 90, "09:00", "18:00", 6),
				newItem(model.KindStand, 180, "09:45", "17:45", 2),
				newItem(model.KindMove, 180, "09:30", "15:30", 2),
				newItem(model.KindRest, 180, "10:15", "17:15", 2),
				newItem(model.KindToilet, 300, "11:45", "16:45", 0),
			},
		},
		{
			Key:         "eye-care-first",
			Name:        "护眼优先",
			Description: "我每天用眼强度很大，最想优先保护眼睛。希望用眼提醒更密一些，同时保留喝水、起身、走动和如厕的提醒。",
			Summary:     "护眼优先：更密的远眺提醒，其余保持标准节奏。",
			Reminders: []Reminder{
				newItem(model.KindWater, 150, "09:15", "17:15", 3),
				newItem(model.KindStand, 240, "09:45", "17:45", 2),
				newItem(model.KindMove, 300, "10:15", "16:15", 2),
				newItem(model.KindRest, 90, "09:00", "17:30", 5),
				newItem(model.KindToilet, 300, "11:15", "17:15", 0),
			},
		},
	}
}
