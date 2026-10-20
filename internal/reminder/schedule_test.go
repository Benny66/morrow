package reminder

import (
	"strconv"
	"testing"
	"time"

	"morrow/internal/model"
)

// localTime 构造本地时区的时刻，避免测试依赖运行环境的时区设置。
func localTime(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, time.Local)
}

// TestParseClock 覆盖合法/非法写法。
func TestParseClock(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  int
		ok    bool
	}{
		{"标准写法", "09:30", 9*60 + 30, true},
		{"单位数小时（用户手填常见）", "9:05", 9*60 + 5, true},
		{"首尾空格", " 18:00 ", 18 * 60, true},
		{"零点", "00:00", 0, true},
		{"当天最后一分钟", "23:59", 23*60 + 59, true},
		{"缺分钟", "09", 0, false},
		{"小时越界", "24:00", 0, false},
		{"分钟越界", "09:60", 0, false},
		{"负数", "-1:00", 0, false},
		{"非数字", "ab:cd", 0, false},
		{"空串", "", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseClock(c.input)
			if ok != c.ok {
				t.Fatalf("ParseClock(%q) ok = %v，期望 %v", c.input, ok, c.ok)
			}
			if ok && got != c.want {
				t.Errorf("ParseClock(%q) = %d，期望 %d", c.input, got, c.want)
			}
		})
	}
}

// TestFormatClock 校验补零与越界夹取。
func TestFormatClock(t *testing.T) {
	cases := []struct {
		input int
		want  string
	}{
		{0, "00:00"},
		{9*60 + 5, "09:05"},
		{18 * 60, "18:00"},
		{-10, "00:00"},
		{9999, "23:59"},
	}
	for _, c := range cases {
		if got := FormatClock(c.input); got != c.want {
			t.Errorf("FormatClock(%d) = %q，期望 %q", c.input, got, c.want)
		}
	}
}

// TestFirePoints 覆盖调度网格的全部边界：常规、端点计入、一次性、非法时段。
func TestFirePoints(t *testing.T) {
	day := localTime(2026, time.March, 2, 0, 0)

	t.Run("每小时一次且包含恰好在结束点上的那次", func(t *testing.T) {
		points := FirePoints(day, "09:00", "12:00", 60)
		want := []int64{
			localTime(2026, time.March, 2, 9, 0).Unix(),
			localTime(2026, time.March, 2, 10, 0).Unix(),
			localTime(2026, time.March, 2, 11, 0).Unix(),
			localTime(2026, time.March, 2, 12, 0).Unix(),
		}
		assertPoints(t, points, want)
	})

	t.Run("间隔不能整除时不越过结束点：50 分钟间隔停在 17:20", func(t *testing.T) {
		points := FirePoints(day, "09:00", "18:00", 50)
		// 09:00 起每 50 分钟：09:00、09:50、10:40、11:30、12:20、13:10、14:00、
		// 14:50、15:40、16:30、17:20，共 11 个点。
		// 下一个点是 17:20 + 50 分钟 = 18:10，越过 18:00，故不产生——
		// 这正是「不能整除时不含越过结束点的点」要锁定的行为。
		if len(points) != 11 {
			t.Fatalf("点数 = %d，期望 11（09:00..17:20）", len(points))
		}
		if points[0] != localTime(2026, time.March, 2, 9, 0).Unix() {
			t.Errorf("首个点 = %v，期望 09:00", points[0])
		}
		// 末点是 17:20 而不是 18:00：18:00 与 09:00 相差 540 分钟，540 % 50 = 40，
		// 不在网格上；最后一个网格点 17:20 距结束点只剩 40 分钟。
		if points[len(points)-1] != localTime(2026, time.March, 2, 17, 20).Unix() {
			t.Errorf("末个点 = %v，期望 17:20", points[len(points)-1])
		}
	})

	t.Run("间隔正好整除时恰落在结束点上的那次计入：45 分钟间隔到 18:00 共 13 个点", func(t *testing.T) {
		points := FirePoints(day, "09:00", "18:00", 45)
		// 09:00 与 18:00 相差 540 分钟，540 % 45 == 0，18:00 本身就在网格上。
		// FirePoints 的语义是「恰好落在结束点上的点计入」（见 schedule.go 中的说明），
		// 所以是 09:00 + 45×0..12 共 13 个点，末点 18:00，而非 17:30。
		if len(points) != 13 {
			t.Fatalf("点数 = %d，期望 13（09:00..18:00）", len(points))
		}
		if points[0] != localTime(2026, time.March, 2, 9, 0).Unix() {
			t.Errorf("首个点 = %v，期望 09:00", points[0])
		}
		if points[len(points)-1] != localTime(2026, time.March, 2, 18, 0).Unix() {
			t.Errorf("末个点 = %v，期望 18:00（端点计入）", points[len(points)-1])
		}
	})

	t.Run("间隔为 0 视为一次性，只在起点触发", func(t *testing.T) {
		points := FirePoints(day, "09:00", "18:00", 0)
		assertPoints(t, points, []int64{localTime(2026, time.March, 2, 9, 0).Unix()})
	})

	t.Run("负间隔同样按一次性处理", func(t *testing.T) {
		points := FirePoints(day, "09:00", "18:00", -30)
		assertPoints(t, points, []int64{localTime(2026, time.March, 2, 9, 0).Unix()})
	})

	t.Run("结束等于开始是非法时段", func(t *testing.T) {
		if points := FirePoints(day, "09:00", "09:00", 30); len(points) != 0 {
			t.Errorf("期望空结果，实际 %v", points)
		}
	})

	t.Run("结束早于开始是非法时段（不支持跨午夜）", func(t *testing.T) {
		if points := FirePoints(day, "22:00", "06:00", 60); len(points) != 0 {
			t.Errorf("期望空结果，实际 %v", points)
		}
	})

	t.Run("非法写法返回空", func(t *testing.T) {
		if points := FirePoints(day, "25:00", "26:00", 60); len(points) != 0 {
			t.Errorf("期望空结果，实际 %v", points)
		}
	})

	t.Run("相邻两天互不重叠", func(t *testing.T) {
		today := FirePoints(day, "09:00", "10:00", 60)
		tomorrow := FirePoints(day.AddDate(0, 0, 1), "09:00", "10:00", 60)
		if len(today) != 2 || len(tomorrow) != 2 {
			t.Fatalf("today=%v tomorrow=%v，期望各 2 个点", today, tomorrow)
		}
		if today[len(today)-1] >= tomorrow[0] {
			t.Errorf("今天的点 %v 不应晚于明天的点 %v", today[len(today)-1], tomorrow[0])
		}
	})
}

// TestInQuietHours 覆盖默认的跨午夜静默时段与边界。
func TestInQuietHours(t *testing.T) {
	cases := []struct {
		name  string
		at    time.Time
		start string
		end   string
		want  bool
	}{
		{"默认静默：深夜 23:30 属于静默", localTime(2026, time.March, 2, 23, 30), "22:00", "08:00", true},
		{"默认静默：凌晨 03:00 属于静默", localTime(2026, time.March, 2, 3, 0), "22:00", "08:00", true},
		{"默认静默：起点 22:00 含", localTime(2026, time.March, 2, 22, 0), "22:00", "08:00", true},
		{"默认静默：终点 08:00 不含（用户希望 8 点就收到第一条）", localTime(2026, time.March, 2, 8, 0), "22:00", "08:00", false},
		{"默认静默：白天 12:00 不属于静默", localTime(2026, time.March, 2, 12, 0), "22:00", "08:00", false},
		{"不跨午夜：13:00–14:00 内 13:30 属于静默", localTime(2026, time.March, 2, 13, 30), "13:00", "14:00", true},
		{"不跨午夜：14:00 不含", localTime(2026, time.March, 2, 14, 0), "13:00", "14:00", false},
		{"起止相同视为未设置", localTime(2026, time.March, 2, 12, 0), "12:00", "12:00", false},
		{"写法非法视为未设置", localTime(2026, time.March, 2, 12, 0), "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := InQuietHours(c.at, c.start, c.end); got != c.want {
				t.Errorf("InQuietHours(%s, %q, %q) = %v，期望 %v",
					c.at.Format("15:04"), c.start, c.end, got, c.want)
			}
		})
	}
}

// TestNextFireAt 覆盖「今天还有剩余点」与「今天已结束，看明天」。
func TestNextFireAt(t *testing.T) {
	t.Run("时段内返回下一个点", func(t *testing.T) {
		now := localTime(2026, time.March, 2, 10, 30)
		got := NextFireAt(now, "09:00", "18:00", 60)
		want := localTime(2026, time.March, 2, 11, 0).Unix()
		if got != want {
			t.Errorf("NextFireAt = %v，期望 %v", got, want)
		}
	})

	t.Run("最后一个点之后返回明天的第一个点", func(t *testing.T) {
		now := localTime(2026, time.March, 2, 19, 0)
		got := NextFireAt(now, "09:00", "18:00", 60)
		want := localTime(2026, time.March, 3, 9, 0).Unix()
		if got != want {
			t.Errorf("NextFireAt = %v，期望 %v（明天 09:00）", got, want)
		}
	})

	t.Run("一次性提醒已过则看明天", func(t *testing.T) {
		now := localTime(2026, time.March, 2, 10, 0)
		got := NextFireAt(now, "09:00", "18:00", 0)
		want := localTime(2026, time.March, 3, 9, 0).Unix()
		if got != want {
			t.Errorf("NextFireAt = %v，期望 %v", got, want)
		}
	})

	t.Run("非法时段返回 0", func(t *testing.T) {
		now := localTime(2026, time.March, 2, 10, 0)
		if got := NextFireAt(now, "18:00", "09:00", 60); got != 0 {
			t.Errorf("NextFireAt = %v，期望 0", got)
		}
	})
}

// TestValidateWindow 校验时段合法性判定。
func TestValidateWindow(t *testing.T) {
	if !ValidateWindow("09:00", "18:00") {
		t.Error("09:00–18:00 应合法")
	}
	if ValidateWindow("18:00", "09:00") {
		t.Error("18:00–09:00 应非法（不支持跨午夜）")
	}
	if ValidateWindow("09:00", "09:00") {
		t.Error("空时段应非法")
	}
	if ValidateWindow("bad", "18:00") {
		t.Error("写法非法应判为非法")
	}
}

// TestNotificationIDRoundTrip 校验通知 ID 与触发记录 ID 的互查。
func TestNotificationIDRoundTrip(t *testing.T) {
	fire := DueFire{FireID: 42}
	if got := fire.NotificationID(); got != "reminder-fire-42" {
		t.Fatalf("NotificationID = %q，期望 reminder-fire-42", got)
	}
	if got := FireIDFromNotificationID(fire.NotificationID()); got != 42 {
		t.Errorf("反查得到 %d，期望 42", got)
	}
	for _, bad := range []string{"", "other-42", "reminder-fire-", "reminder-fire-abc"} {
		if got := FireIDFromNotificationID(bad); got != 0 {
			t.Errorf("非法 ID %q 应反查出 0，实际 %d", bad, got)
		}
	}
}

// TestNotificationTitle 校验标题拼接。
func TestNotificationTitle(t *testing.T) {
	cases := []struct {
		scenario string
		title    string
		want     string
	}{
		{"备孕办公日", "喝水", "备孕办公日 · 喝水"},
		{"", "喝水", "喝水"},
		{"备孕办公日", "", "备孕办公日"},
		// 两者都空时必须兜底：上游对 Title=="" 直接返回 error，等于整条提醒丢失。
		{"", "", DefaultNotificationTitle},
	}
	for _, c := range cases {
		got := DueFire{ScenarioName: c.scenario, Title: c.title}.NotificationTitle()
		if got != c.want {
			t.Errorf("NotificationTitle(%q,%q) = %q，期望 %q", c.scenario, c.title, got, c.want)
		}
	}
}

// assertPoints 断言点序列相等。
func assertPoints(t *testing.T, got, want []int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("点数 = %d（%v），期望 %d（%v）", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 个点 = %v，期望 %v", i, got[i], want[i])
		}
	}
}

// ---------------------------------------------------------------------------
// 引擎行为测试：用假存储 + 假时钟推演「补发策略 / 静默 / 去重 / 推迟」。
// ---------------------------------------------------------------------------

// fakeStore 是 Store 的内存实现。
type fakeStore struct {
	scenario  *model.Scenario
	reminders []model.Reminder
	// oneOffs 是待触发的一次性提醒（模拟 service.DueOneOffs 的查询结果）。
	oneOffs []model.Reminder
	// claimed 记录已占用的 (reminder_id, fire_at)，模拟唯一索引。
	claimed map[string]bool
	// settled 记录落终态的触发，键是 fireID。
	settled map[uint]string
	// initial 记录 claim 时写入的初始状态（用于断言 missed 不是先 fired 再改）。
	initial map[uint]string
	// snoozes 是"到点待补发"的推迟记录，读出后清空（模拟被处理掉）。
	snoozes []DueFire
	nextID  uint
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		claimed: map[string]bool{},
		settled: map[uint]string{},
		initial: map[uint]string{},
		nextID:  1,
	}
}

func (s *fakeStore) ActiveScenario() (*model.Scenario, error) { return s.scenario, nil }

func (s *fakeStore) ActiveReminders(uint) ([]model.Reminder, error) { return s.reminders, nil }

func (s *fakeStore) DueOneOffs(now int64) ([]model.Reminder, error) {
	due := make([]model.Reminder, 0, len(s.oneOffs))
	for _, item := range s.oneOffs {
		if item.OnceAt > now {
			continue
		}
		if s.claimed[fireKey(item.ID, item.OnceAt)] {
			continue
		}
		due = append(due, item)
	}
	return due, nil
}

func (s *fakeStore) ClaimFire(reminderID uint, fireAt int64) (uint, bool, error) {
	return s.ClaimFireWithStatus(reminderID, fireAt, model.FireFired)
}

func (s *fakeStore) ClaimFireWithStatus(reminderID uint, fireAt int64, status string) (uint, bool, error) {
	key := fireKey(reminderID, fireAt)
	if s.claimed[key] {
		return 0, false, nil
	}
	s.claimed[key] = true
	id := s.nextID
	s.nextID++
	s.initial[id] = status
	return id, true, nil
}

func (s *fakeStore) SettleFire(fireID uint, status string, _ int64) error {
	s.settled[fireID] = status
	return nil
}

func (s *fakeStore) DueSnoozeFires(int64) ([]DueFire, error) {
	due := s.snoozes
	s.snoozes = nil
	return due, nil
}

// fireKey 拼唯一键，模拟数据库上的 (reminder_id, fire_at) 唯一索引。
func fireKey(reminderID uint, fireAt int64) string {
	return strconv.FormatUint(uint64(reminderID), 10) + "-" + strconv.FormatInt(fireAt, 10)
}

// fakeNotifier 记录通知调用。
type fakeNotifier struct{ sent []Notification }

func (n *fakeNotifier) Notify(msg Notification) error {
	n.sent = append(n.sent, msg)
	return nil
}

// fakeEmitter 记录事件广播。
type fakeEmitter struct {
	fired   []FiredPayload
	changed []ChangedPayload
}

func (e *fakeEmitter) EmitFired(p FiredPayload) { e.fired = append(e.fired, p) }

func (e *fakeEmitter) EmitChanged(p ChangedPayload) { e.changed = append(e.changed, p) }

// newTestEngine 组装一个固定时钟的引擎。
func newTestEngine(store *fakeStore, now time.Time, settings Settings) (*Engine, *fakeNotifier, *fakeEmitter) {
	notifier := &fakeNotifier{}
	emitter := &fakeEmitter{}
	engine := NewEngine(Options{
		Store:    store,
		Settings: func() Settings { return settings },
		Notifier: notifier,
		Emitter:  emitter,
		Now:      func() time.Time { return now },
	})
	return engine, notifier, emitter
}

// TestTickOnlyDeliversLatest 断言「过期多个点只补最近 1 条」。
func TestTickOnlyDeliversLatest(t *testing.T) {
	store := newFakeStore()
	store.scenario = &model.Scenario{Name: "备孕办公日"}
	store.scenario.ID = 1
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 7},
		Title:       "喝水",
		Content:     "该喝水了",
		Kind:        model.KindWater,
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
	}}

	// 12:00 时，09:00 / 10:00 / 11:00 / 12:00 四个点都已过期。
	now := localTime(2026, time.March, 2, 12, 0)
	engine, notifier, emitter := newTestEngine(store, now, Settings{NotifyEnabled: true})
	engine.Tick()

	if len(notifier.sent) != 1 {
		t.Fatalf("通知数 = %d，期望 1（只补发最近 1 条）", len(notifier.sent))
	}
	if len(emitter.fired) != 1 {
		t.Fatalf("事件数 = %d，期望 1", len(emitter.fired))
	}
	// 4 个点：1 个 fired，3 个 missed。
	fired, missed := 0, 0
	for _, status := range store.settled {
		switch status {
		case model.FireFired:
			fired++
		case model.FireMissed:
			missed++
		}
	}
	if fired != 1 || missed != 3 {
		t.Errorf("fired=%d missed=%d，期望 fired=1 missed=3", fired, missed)
	}
}

// TestTickIsIdempotent 断言第二轮 tick 不会重复送达。
func TestTickIsIdempotent(t *testing.T) {
	store := newFakeStore()
	store.scenario = &model.Scenario{Name: "备孕办公日"}
	store.scenario.ID = 1
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 7},
		Title:       "起身",
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
	}}

	now := localTime(2026, time.March, 2, 10, 0)
	engine, notifier, _ := newTestEngine(store, now, Settings{NotifyEnabled: true})
	engine.Tick()
	first := len(notifier.sent)
	engine.Tick()
	if len(notifier.sent) != first {
		t.Errorf("第二轮 tick 又发了通知：累计 %d 条，期望维持 %d 条", len(notifier.sent), first)
	}
}

// TestTickRespectsQuietHours 断言静默时段只记录不通知。
func TestTickRespectsQuietHours(t *testing.T) {
	store := newFakeStore()
	store.scenario = &model.Scenario{Name: "备孕办公日"}
	store.scenario.ID = 1
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 9},
		Title:       "喝水",
		IntervalMin: 60,
		WindowStart: "06:00",
		WindowEnd:   "09:00",
	}}

	// 07:00 落在默认静默时段（22:00–08:00）内。
	now := localTime(2026, time.March, 2, 7, 0)
	settings := Settings{QuietStart: "22:00", QuietEnd: "08:00", NotifyEnabled: true}
	engine, notifier, emitter := newTestEngine(store, now, settings)
	engine.Tick()

	if len(notifier.sent) != 0 {
		t.Errorf("静默时段仍发了 %d 条通知", len(notifier.sent))
	}
	if len(emitter.fired) != 0 {
		t.Errorf("静默时段仍广播了 %d 个事件", len(emitter.fired))
	}
	silent := 0
	for _, status := range store.settled {
		if status == model.FireSilent {
			silent++
		}
	}
	if silent != 1 {
		t.Errorf("静默记录数 = %d，期望 1（照常记录）", silent)
	}
}

// TestTickDeliversDueSnooze 断言推迟记录到点后被补发。
func TestTickDeliversDueSnooze(t *testing.T) {
	store := newFakeStore()
	store.scenario = &model.Scenario{Name: "备孕办公日"}
	store.scenario.ID = 1
	store.snoozes = []DueFire{{
		FireID:       99,
		ReminderID:   7,
		ScenarioID:   1,
		ScenarioName: "备孕办公日",
		Title:        "喝水",
		Content:      "该喝水了",
		Kind:         model.KindWater,
		FireAt:       localTime(2026, time.March, 2, 10, 5).Unix(),
	}}

	now := localTime(2026, time.March, 2, 10, 6)
	engine, notifier, emitter := newTestEngine(store, now, Settings{NotifyEnabled: true})
	engine.Tick()

	if len(notifier.sent) != 1 {
		t.Fatalf("通知数 = %d，期望 1", len(notifier.sent))
	}
	if store.settled[99] != model.FireFired {
		t.Errorf("推迟记录状态 = %q，期望 %q", store.settled[99], model.FireFired)
	}
	if len(emitter.fired) != 1 || emitter.fired[0].FireID != 99 {
		t.Errorf("事件载荷不符：%+v", emitter.fired)
	}
}

// TestTickWithoutActiveScenarioDoesNothing 断言没有启用场景时引擎完全安静。
func TestTickWithoutActiveScenarioDoesNothing(t *testing.T) {
	store := newFakeStore()
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 7},
		Title:       "喝水",
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
	}}

	now := localTime(2026, time.March, 2, 12, 0)
	engine, notifier, _ := newTestEngine(store, now, Settings{NotifyEnabled: true})
	engine.Tick()

	if len(notifier.sent) != 0 {
		t.Errorf("无启用场景时不应有任何通知，实际 %d 条", len(notifier.sent))
	}
	if len(store.settled) != 0 {
		t.Errorf("无启用场景时不应写触发记录，实际 %d 条", len(store.settled))
	}
}

// TestTickNotifyDisabledStillRecords 断言关闭通知后提醒仍然落库（首页卡片照常出现）。
func TestTickNotifyDisabledStillRecords(t *testing.T) {
	store := newFakeStore()
	store.scenario = &model.Scenario{Name: "备孕办公日"}
	store.scenario.ID = 1
	store.reminders = []model.Reminder{{
		BaseModel:   model.BaseModel{ID: 7},
		Title:       "喝水",
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
	}}

	now := localTime(2026, time.March, 2, 10, 0)
	engine, notifier, emitter := newTestEngine(store, now, Settings{NotifyEnabled: false})
	engine.Tick()

	if len(notifier.sent) != 0 {
		t.Errorf("通知开关关闭时不应发通知，实际 %d 条", len(notifier.sent))
	}
	if len(emitter.fired) != 1 {
		t.Errorf("事件数 = %d，期望 1（界面仍需刷新）", len(emitter.fired))
	}
	// 10:00 时 09:00 与 10:00 两个点都已过期：只送达最近 1 条，
	// 故恰好一条 fired、一条 missed。
	fired := 0
	for _, status := range store.settled {
		if status == model.FireFired {
			fired++
		}
	}
	if fired != 1 {
		t.Errorf("fired 记录数 = %d，期望 1（关闭通知不影响落库）", fired)
	}
}

// TestStartStop 断言生命周期方法可重入且不 panic。
func TestStartStop(t *testing.T) {
	store := newFakeStore()
	engine, _, _ := newTestEngine(store, localTime(2026, time.March, 2, 12, 0), Settings{})
	engine.Start()
	engine.Start() // 幂等
	engine.Stop()
	engine.Stop() // 幂等
}
