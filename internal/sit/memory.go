package sit

import (
	"strconv"
	"strings"
	"time"
)

// DayFact 是 service 汇总的单日类别级事实（分钟）。
//
// 这里只有聚合值，没有应用标识；应用明细留在久坐页，宠物记忆不逐条回放。
type DayFact struct {
	Date             time.Time
	SittingMinutes   int
	LongestMinutes   int
	ActiveBreaks     int
	LockBreaks       int
	IdleBreaks       int
	AfternoonMinutes int
	EveningMinutes   int
	LateNightMinutes int
	CategoryMinutes  map[Category]int
}

// MemoryFacts 是文案选择所需的派生事实。
type MemoryFacts struct {
	TodaySittingMinutes  int
	TodayLongestMinutes  int
	TodayBreaks          int
	TodayActiveBreaks    int
	TodayCategoryMinutes map[Category]int

	RecentSittingMinutes int // 最近 7 天（含今天）
	PriorSittingMinutes  int // 再往前 7 天
	RecentActiveBreaks   int
	PriorActiveBreaks    int
	RecentSkips          int
	RecentSnoozes        int

	AfternoonMinutes int // 最近 7 天
	EveningMinutes   int
	LateNightMinutes int

	WeekdayMinutes [7]int // 最近 28 天，按 time.Weekday 索引
	WeekdayDays    [7]int

	CurrentContinuousMinutes int
	HasProfile               bool
}

// MemoryQuote 是一条可直接搭车展示的记忆句。
type MemoryQuote struct {
	ID         string // 事实 ID，用于 3 天去重
	TemplateID string // 含语气的模板 ID，用于日志
	Text       string
}

// DeriveFacts 把单日聚合折叠成记忆事实；纯函数，时间与日期都由调用方给出。
func DeriveFacts(now time.Time, days []DayFact, skips, snoozes int, hasProfile bool, currentContinuous int) MemoryFacts {
	localNow := now.In(time.Local)
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local)
	weekStart := today.AddDate(0, 0, -6)
	priorStart := today.AddDate(0, 0, -13)
	historyStart := today.AddDate(0, 0, -27)

	out := MemoryFacts{
		RecentSkips:              skips,
		RecentSnoozes:            snoozes,
		CurrentContinuousMinutes: currentContinuous,
		HasProfile:               hasProfile,
	}

	for _, day := range days {
		d := day.Date.In(time.Local)
		d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local)
		if d.After(today) || d.Before(historyStart) {
			continue
		}
		if d.Equal(today) {
			out.TodaySittingMinutes = day.SittingMinutes
			out.TodayLongestMinutes = day.LongestMinutes
			out.TodayActiveBreaks = day.ActiveBreaks
			out.TodayBreaks = day.ActiveBreaks + day.LockBreaks + day.IdleBreaks
			out.TodayCategoryMinutes = day.CategoryMinutes
		}
		if !d.Before(weekStart) {
			out.RecentSittingMinutes += day.SittingMinutes
			out.RecentActiveBreaks += day.ActiveBreaks
			out.AfternoonMinutes += day.AfternoonMinutes
			out.EveningMinutes += day.EveningMinutes
			out.LateNightMinutes += day.LateNightMinutes
		} else if !d.Before(priorStart) {
			out.PriorSittingMinutes += day.SittingMinutes
			out.PriorActiveBreaks += day.ActiveBreaks
		}
		if day.SittingMinutes > 0 {
			weekday := int(d.Weekday())
			out.WeekdayMinutes[weekday] += day.SittingMinutes
			out.WeekdayDays[weekday]++
		}
	}
	return out
}

// quoteSpec 是按事实分组的三档模板；占位符由 SelectQuote 填充。
type quoteSpec struct {
	id   string
	text map[Tone]string
}

var quoteTemplates = []quoteSpec{
	{
		id: "evening",
		text: map[Tone]string{
			ToneSassy:  "晚上已经坐了 {n} 分钟，椅子今天挺黏你。起来走两步？",
			ToneLively: "晚上也坐了 {n} 分钟啦，起来活动一下下？",
			ToneGentle: "晚上已经坐了 {n} 分钟，起来活动一下吧。",
		},
	},
	{
		id: "afternoon",
		text: map[Tone]string{
			ToneSassy:  "下午这 {n} 分钟坐得很稳。稳到椅子都替你着急，起来走两步？",
			ToneLively: "下午已经坐了 {n} 分钟咯，起来伸个懒腰？",
			ToneGentle: "下午已经坐了 {n} 分钟，起来活动一下吧。",
		},
	},
	{
		id: "late_night",
		text: map[Tone]string{
			ToneSassy:  "这个点还坐了 {n} 分钟。夜可以熬，腿得动，起来走两步？",
			ToneLively: "这么晚还坐了 {n} 分钟，起来晃晃再继续？",
			ToneGentle: "这么晚还坐了 {n} 分钟，起来活动一下再继续吧。",
		},
	},
	{
		id: "weekday",
		text: map[Tone]string{
			ToneSassy:  "{weekday}是你的久坐高峰，平均 {n} 分钟。今天别又坐满，起来走两步？",
			ToneLively: "{weekday}平均要坐 {n} 分钟，今天起来晃一晃？",
			ToneGentle: "{weekday}你平均会坐 {n} 分钟，今天记得起来活动。",
		},
	},
	{
		id: "week_up_breaks",
		text: map[Tone]string{
			ToneSassy:  "这周主动起身比上周多了 {n} 次。行，腿还算听话，继续保持。",
			ToneLively: "这周主动起身比上周多了 {n} 次，厉害呀，继续保持！",
			ToneGentle: "这周主动起身比上周多了 {n} 次，做得很好，继续保持。",
		},
	},
	{
		id: "week_up_sitting",
		text: map[Tone]string{
			ToneSassy:  "这周久坐比上周少了 {n} 分钟。不错，椅子总算没白劝。",
			ToneLively: "这周久坐比上周少了 {n} 分钟，进步看得见！",
			ToneGentle: "这周久坐比上周少了 {n} 分钟，保持这个节奏很好。",
		},
	},
	{
		id: "week_down_sitting",
		text: map[Tone]string{
			ToneSassy:  "这周久坐比上周多了 {n} 分钟。椅子很高兴，你的腿未必。起来走两步？",
			ToneLively: "这周久坐比上周多了 {n} 分钟，起来晃晃把数字追回来？",
			ToneGentle: "这周久坐比上周多了 {n} 分钟，接下来多起来活动一下。",
		},
	},
	{
		id: "longest",
		text: map[Tone]string{
			ToneSassy:  "今天最长一口气坐了 {n} 分钟。椅子都记住你了，起来走两步？",
			ToneLively: "今天最长连续坐了 {n} 分钟，起来活动一下下？",
			ToneGentle: "今天最长连续坐了 {n} 分钟，起来活动一下吧。",
		},
	},
	{
		id: "category",
		text: map[Tone]string{
			ToneSassy:  "今天在「{category}」上花了 {n} 分钟。专注是好事，坐穿椅子不是，起来走两步？",
			ToneLively: "今天「{category}」占了 {n} 分钟，起来晃晃换换脑子？",
			ToneGentle: "今天「{category}」已经花了 {n} 分钟，起来活动一下再继续。",
		},
	},
	{
		id: "breaks",
		text: map[Tone]string{
			ToneSassy:  "今天已经休息 {total} 次，主动起身 {n} 次。还行，别让下一次来得太晚。",
			ToneLively: "今天休息了 {total} 次，主动起身 {n} 次，继续保持呀！",
			ToneGentle: "今天已经休息 {total} 次，主动起身 {n} 次，记得继续保持。",
		},
	},
	{
		id: "skips",
		text: map[Tone]string{
			ToneSassy:  "最近你跳过了 {n} 次提醒。提醒可以被跳过，椅子不会自己挪，起来走两步？",
			ToneLively: "最近跳过了 {n} 次提醒，起来动一动把节奏找回来？",
			ToneGentle: "最近跳过了 {n} 次提醒，忙的话也记得短暂起来活动一下。",
		},
	},
}

var weekdayLabels = [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

// WeekdayLabel 返回中文星期名。
func WeekdayLabel(day time.Weekday) string {
	if int(day) < 0 || int(day) >= len(weekdayLabels) {
		return ""
	}
	return weekdayLabels[int(day)]
}

// SelectQuote 选择一条相关且 3 天内未使用过的记忆句；没有值得说的事实返回 nil。
//
// 顺序体现「时段相关性优先」：当前时段 → 星期规律 → 周对比 → 今日最长/类别/休息 → 跳过。
func SelectQuote(now time.Time, facts MemoryFacts, tone Tone, history []string) *MemoryQuote {
	used := make(map[string]bool, len(history))
	for _, id := range history {
		used[id] = true
	}
	localNow := now.In(time.Local)
	type candidate struct {
		id   string
		text map[string]string
	}
	var candidates []candidate
	add := func(id string, vars map[string]string) {
		if used[id] {
			return
		}
		candidates = append(candidates, candidate{id: id, text: vars})
	}

	switch {
	case localNow.Hour() >= 18 && facts.EveningMinutes >= 60:
		add("evening", map[string]string{"n": strconv.Itoa(facts.EveningMinutes)})
	case localNow.Hour() >= 12 && localNow.Hour() < 18 && facts.AfternoonMinutes >= 90:
		add("afternoon", map[string]string{"n": strconv.Itoa(facts.AfternoonMinutes)})
	case localNow.Hour() < 6 && facts.LateNightMinutes >= 30:
		add("late_night", map[string]string{"n": strconv.Itoa(facts.LateNightMinutes)})
	}

	today := int(localNow.Weekday())
	if facts.WeekdayDays[today] >= 2 {
		avg := facts.WeekdayMinutes[today] / facts.WeekdayDays[today]
		if avg >= 45 && facts.TodaySittingMinutes >= 20 {
			add("weekday", map[string]string{"weekday": WeekdayLabel(localNow.Weekday()), "n": strconv.Itoa(avg)})
		}
	}

	if facts.RecentActiveBreaks >= 3 && facts.RecentActiveBreaks >= facts.PriorActiveBreaks+2 {
		add("week_up_breaks", map[string]string{"n": strconv.Itoa(facts.RecentActiveBreaks - facts.PriorActiveBreaks)})
	}
	if facts.PriorSittingMinutes >= 30 {
		if facts.RecentSittingMinutes <= facts.PriorSittingMinutes*8/10 {
			add("week_up_sitting", map[string]string{"n": strconv.Itoa(facts.PriorSittingMinutes - facts.RecentSittingMinutes)})
		} else if facts.RecentSittingMinutes >= facts.PriorSittingMinutes*12/10 {
			add("week_down_sitting", map[string]string{"n": strconv.Itoa(facts.RecentSittingMinutes - facts.PriorSittingMinutes)})
		}
	}
	if facts.TodayLongestMinutes >= 45 {
		add("longest", map[string]string{"n": strconv.Itoa(facts.TodayLongestMinutes)})
	}
	if facts.HasProfile {
		category, minutes := topCategory(facts.TodayCategoryMinutes)
		if minutes >= 20 {
			add("category", map[string]string{"category": CategoryLabel(category), "n": strconv.Itoa(minutes)})
		}
	}
	if facts.TodayActiveBreaks >= 2 || facts.TodayBreaks >= 3 {
		add("breaks", map[string]string{"n": strconv.Itoa(facts.TodayActiveBreaks), "total": strconv.Itoa(facts.TodayBreaks)})
	}
	if count := facts.RecentSkips + facts.RecentSnoozes; count >= 3 {
		add("skips", map[string]string{"n": strconv.Itoa(count)})
	}

	for _, c := range candidates {
		if spec := quoteSpecByID(c.id); spec != nil {
			return &MemoryQuote{
				ID:         c.id,
				TemplateID: "memory." + string(NormalizeTone(string(tone))) + "." + c.id,
				Text:       formatQuote(spec, tone, c.text),
			}
		}
	}
	return nil
}

func quoteSpecByID(id string) *quoteSpec {
	for i := range quoteTemplates {
		if quoteTemplates[i].id == id {
			return &quoteTemplates[i]
		}
	}
	return nil
}

func formatQuote(spec *quoteSpec, tone Tone, vars map[string]string) string {
	text := spec.text[NormalizeTone(string(tone))]
	if text == "" {
		text = spec.text[ToneGentle]
	}
	for key, value := range vars {
		text = strings.ReplaceAll(text, "{"+key+"}", value)
	}
	return text
}

func topCategory(minutes map[Category]int) (Category, int) {
	best := CategoryOther
	bestMinutes := 0
	for _, category := range AllCategories() {
		if minutes[category] > bestMinutes {
			best, bestMinutes = category, minutes[category]
		}
	}
	return best, bestMinutes
}
