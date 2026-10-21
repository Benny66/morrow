package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"morrow/internal/model"
)

// newMissedTestService 建临时库并锁定一个固定"当前时间"，让「今日」的边界可预期。
func newMissedTestService(t *testing.T, now time.Time) *Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "missed-test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("迁移业务模型失败: %v", err)
	}
	svc := New(db)
	svc.now = func() time.Time { return now }
	return svc
}

// seedDailyReminder 建一个每日单点提醒项，返回其 ID。
func seedDailyReminder(t *testing.T, svc *Service, title string) uint {
	t.Helper()
	// 【必须有描述】CreateScenario 会拒绝空描述（AI 生成计划需要它）；
	// 本测试不生成 AI 计划，但校验走同一条路径。
	scenario, err := svc.CreateScenario("测试场景", "每 45 分钟起身站立 5 分钟")
	if err != nil {
		t.Fatalf("创建场景失败: %v", err)
	}
	item, err := svc.CreateReminder(ReminderInput{
		ScenarioID:  scenario.ID,
		Title:       title,
		Kind:        model.KindStand,
		IntervalMin: 0, // 每日单点
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("创建提醒失败: %v", err)
	}
	return item.ID
}

// TestListMissedTodayReturnsOnlyToday 验证只返回今日 00:00 之后的 missed 记录。
//
// 【为什么要这条】跨入新一天的凌晨，昨日错过的项已经对用户没有可操作性；
// 首页"已错过"分组无限膨胀会让用户根本看不清今天发生了什么。
func TestListMissedTodayReturnsOnlyToday(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	svc := newMissedTestService(t, now)
	remID := seedDailyReminder(t, svc, "上午舒展")

	// 昨日 09:30 的 missed 记录（不应出现在结果里）。
	yesterday := time.Date(2026, 10, 19, 9, 30, 0, 0, time.Local).Unix()
	if _, _, err := svc.ClaimFireWithStatus(remID, yesterday, model.FireMissed); err != nil {
		t.Fatalf("seed 昨日 missed 失败: %v", err)
	}

	// 今日 09:30 的 missed 记录（应出现）。
	today := time.Date(2026, 10, 20, 9, 30, 0, 0, time.Local).Unix()
	if _, _, err := svc.ClaimFireWithStatus(remID, today, model.FireMissed); err != nil {
		t.Fatalf("seed 今日 missed 失败: %v", err)
	}

	list, err := svc.ListMissedToday()
	if err != nil {
		t.Fatalf("ListMissedToday 失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("应只返回今日一条，实际 %d 条：%+v", len(list), list)
	}
	if list[0].FireAt != today {
		t.Errorf("返回的 fire_at 应为今日 09:30，实际 %d", list[0].FireAt)
	}
}

// TestListMissedTodaySkipsFiredAndSilent 验证只包含 missed，不混入 fired / silent。
//
// 【为什么】fired 是待处理卡片；silent 是静默时段与暂停期间的周期项，都
// 不该出现在「已错过」里。
func TestListMissedTodaySkipsFiredAndSilent(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	svc := newMissedTestService(t, now)
	remID := seedDailyReminder(t, svc, "上午舒展")

	base := time.Date(2026, 10, 20, 9, 0, 0, 0, time.Local).Unix()
	// fired：走待处理卡片，不进 missed 分组。
	if _, _, err := svc.ClaimFireWithStatus(remID, base, model.FireFired); err != nil {
		t.Fatal(err)
	}
	// silent：暂停期或静默时段的周期项，不显示为已错过。
	if _, _, err := svc.ClaimFireWithStatus(remID, base+3600, model.FireSilent); err != nil {
		t.Fatal(err)
	}
	// missed：唯一会被返回的。
	if _, _, err := svc.ClaimFireWithStatus(remID, base+7200, model.FireMissed); err != nil {
		t.Fatal(err)
	}

	list, err := svc.ListMissedToday()
	if err != nil {
		t.Fatalf("ListMissedToday 失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("只应返回 1 条 missed，实际 %d 条：%+v", len(list), list)
	}
}

// TestListMissedTodayIncludesOneOffMissed 验证暂停期一次性提醒也出现在结果里。
//
// 【与 US-6 直接对应】一次性提醒在暂停期被 claim 为 FireMissed 之后，必须能在
// 「已错过」分组里被用户看到；否则「保留可见」的承诺就是空的。
func TestListMissedTodayIncludesOneOffMissed(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	svc := newMissedTestService(t, now)

	// CreateReminder 会拒绝「已过去」的 once_at，故先按未来时刻创建，
	// 再把 service 时钟推后、以同一时刻 claim missed——比手写 INSERT 更贴近真实链路。
	onceAt := time.Date(2026, 10, 20, 13, 0, 0, 0, time.Local).Unix()
	oneOff, err := svc.CreateReminder(ReminderInput{
		Title:       "接孩子",
		Kind:        model.KindCustom,
		OnceAt:      onceAt,
		IntervalMin: 0,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("创建一次性提醒失败: %v", err)
	}
	svc.now = func() time.Time { return time.Date(2026, 10, 20, 14, 0, 0, 0, time.Local) }
	if _, _, err := svc.ClaimFireWithStatus(oneOff.ID, onceAt, model.FireMissed); err != nil {
		t.Fatalf("claim missed 失败: %v", err)
	}

	list, err := svc.ListMissedToday()
	if err != nil {
		t.Fatalf("ListMissedToday 失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("应返回一次性提醒 1 条，实际 %d 条", len(list))
	}
	if !list[0].IsOneOff {
		t.Error("一次性提醒应标 IsOneOff=true，让 UI 走「无场景」呈现")
	}
	if list[0].Title != "接孩子" {
		t.Errorf("标题应为「接孩子」，实际 %q", list[0].Title)
	}
}
