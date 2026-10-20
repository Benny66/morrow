// Package reminder 实现本地提醒调度引擎。
//
// 职责边界（刻意划得很窄）：
//   - 决定「什么时候该响」——纯时间计算，全部集中在 schedule.go，可脱离数据库与
//     系统时钟单测；
//   - 决定「响了之后怎么记」——通过 Store 接口持久化，去重靠数据库唯一索引；
//   - 决定「响了之后通知谁」——通过 Notifier / Emitter 接口交给组合根注入。
//
// 本包【不】import application 包，也不持有 *gorm.DB：它要能在 go test 里
// 用假时钟、假存储跑完整轮调度。
//
// 时间口径：所有对外的时间戳都是 Unix 秒（与 model.BaseModel 的 autoCreateTime 一致）；
// 时段（window_start/window_end）是本地时区的「当天钟点」字符串。
package reminder

import (
	"strconv"
	"strings"
	"time"
)

// minutesPerDay 一天的分钟数，用于钟点换算。
const minutesPerDay = 24 * 60

// OneOffGrace 是一次性提醒允许的调度迟到窗口。
//
// 【为什么是 60 秒】引擎心跳 20 秒，正常 tick 抖动最多几十秒；超过 1 分钟仍未送达，
// 按用户确认的语义视为「已错过」，不再补发。注意这与推迟队列无关：推迟是用户主动要求，
// 仍按推迟记录补发。
const OneOffGrace = time.Minute

// OneOffState 是一次性提醒在某个时刻的处置结果。
type OneOffState int

const (
	// OneOffNotDue 还没到点。
	OneOffNotDue OneOffState = iota
	// OneOffDue 到点且在容差内，应送达。
	OneOffDue
	// OneOffMissed 已超过容差，应记为错过，不送达。
	OneOffMissed
)

// OneOffOutcome 判断一次性提醒在 nowUnix 时该不该送达。
//
// 边界：onceAt 当秒记为 due；onceAt+grace 当秒仍为 due；超过一秒即 missed。
// grace <= 0 时只允许正好到点那一秒。
func OneOffOutcome(onceAt, nowUnix int64, grace time.Duration) OneOffState {
	if onceAt <= 0 {
		return OneOffNotDue
	}
	if nowUnix < onceAt {
		return OneOffNotDue
	}
	graceSeconds := int64(grace / time.Second)
	if graceSeconds < 0 {
		graceSeconds = 0
	}
	if nowUnix <= onceAt+graceSeconds {
		return OneOffDue
	}
	return OneOffMissed
}

// ParseClock 把 "HH:MM" 解析为当天的分钟数（0..1439）。
//
// 宽容度：允许 "9:00"（单位数小时）与首尾空格——用户手填表单时这两种写法很常见，
// 拒绝它们只会换来一句「格式不对」而没有更多信息。超出 00:00–23:59 的值一律拒绝，
// 因为「25:00」这类输入几乎一定是笔误，静默夹取会让用户以为设置生效了。
func ParseClock(s string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, false
	}
	hour, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, false
	}
	minute, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, false
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

// FormatClock 把当天分钟数格式化为 "HH:MM"（越界值夹取到合法区间）。
func FormatClock(minutes int) string {
	if minutes < 0 {
		minutes = 0
	}
	if minutes >= minutesPerDay {
		minutes = minutesPerDay - 1
	}
	hour := minutes / 60
	minute := minutes % 60
	return twoDigits(hour) + ":" + twoDigits(minute)
}

// twoDigits 补零到两位。
func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// startOfDay 返回 t 所在自然日的本地零点。
//
// 用 time.Date(y, m, d, 0, 0, 0, 0, loc) 而不是 Truncate(24h)：
// Truncate 按 UTC 纪元对齐，在有夏令时的时区里会偏一小时。
func startOfDay(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}

// FirePoints 返回 day 这一天内、落在生效时段上的全部计划触发时间（Unix 秒，升序）。
//
// 语义（这三条就是「提醒什么时候该响」的全部规则）：
//   - intervalMin <= 0：一次性提醒，只在 windowStart 触发一次；
//   - intervalMin > 0：从 windowStart 起每隔 intervalMin 分钟一个点，
//     恰好落在 windowEnd 上的点【计入】（「每天 9:00–18:00 每小时一次」应当包含 18:00）；
//   - 时段非法（解析失败、或 end <= start）一律返回空：不支持跨午夜，
//     时段语义是「白天这段时间」；非法时返回空比猜一个时段更安全——
//     调度器什么都不做，用户会立刻发现「没响」并去检查设置。
func FirePoints(day time.Time, windowStart, windowEnd string, intervalMin int) []int64 {
	start, startOK := ParseClock(windowStart)
	end, endOK := ParseClock(windowEnd)
	if !startOK || !endOK || end <= start {
		return nil
	}

	base := startOfDay(day)
	if intervalMin <= 0 {
		return []int64{base.Add(time.Duration(start) * time.Minute).Unix()}
	}

	// 容量按最坏情况给一个上界（每 15 分钟一个点也才 100 个左右），
	// 避免小间隔时反复扩容；不做硬上限——间隔的合法性由写入口校验。
	points := make([]int64, 0, (end-start)/intervalMin+1)
	for minute := start; minute <= end; minute += intervalMin {
		points = append(points, base.Add(time.Duration(minute)*time.Minute).Unix())
	}
	return points
}

// InQuietHours 判断 t 是否落在静默时段内。
//
// 区间语义是「左闭右开」：start == end 视为未设置静默时段（返回 false），
// start > end 表示跨午夜（默认的 22:00–08:00 正是这种）。
// 终点取开区间是有意的：静默到 08:00 的用户期望 08:00 就想收到第一条提醒。
func InQuietHours(t time.Time, quietStart, quietEnd string) bool {
	start, startOK := ParseClock(quietStart)
	end, endOK := ParseClock(quietEnd)
	if !startOK || !endOK || start == end {
		return false
	}
	current := t.Hour()*60 + t.Minute()
	if start < end {
		return current >= start && current < end
	}
	return current >= start || current < end
}

// NextFireAt 返回 now 之后的下一个计划触发时间（Unix 秒）；彻底没有时返回 0。
//
// 今天已无剩余触发点时看明天——否则晚上 19 点之后仪表盘的倒计时会永远显示
// 「已结束」，而用户其实只是等明天的第一条。
func NextFireAt(now time.Time, windowStart, windowEnd string, intervalMin int) int64 {
	nowUnix := now.Unix()
	for _, point := range FirePoints(now, windowStart, windowEnd, intervalMin) {
		if point > nowUnix {
			return point
		}
	}
	if points := FirePoints(now.AddDate(0, 0, 1), windowStart, windowEnd, intervalMin); len(points) > 0 {
		return points[0]
	}
	return 0
}

// ValidateWindow 校验生效时段的写法与先后关系，返回是否合法。
//
// 单独导出而不是并进 FirePoints：写入口（service 保存提醒/解析 AI 输出）需要
// 明确的「非法」信号，而调度器只需要「算不出点就不算」。
func ValidateWindow(windowStart, windowEnd string) bool {
	start, startOK := ParseClock(windowStart)
	end, endOK := ParseClock(windowEnd)
	if !startOK || !endOK {
		return false
	}
	return end > start
}
