package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"morrow/internal/model"
)

// 这一组测试补的是「纯函数已经覆盖、但数据库那条路径没人测」的空白：
// growthStage / longTermStage / petShapeStage 都有表驱动，但它们的前提是
// PetGrowth() 真的把 TotalDoneDays 查对了。查错的表现非常隐蔽——
// 芽只是"长得比应该的慢"，不会有任何报错。
//
// 用真实 SQLite（纯 Go 驱动，与生产同一个）而不是 mock：
// 这里被测的正是 SQL 本身（COUNT(DISTINCT) + 软删除过滤 + 不按场景过滤）。

// newGrowthTestDB 建一个独占的内存库并迁移全部模型。
//
// 两次注意事项：
//   - DSN 必须每个测试唯一。`:memory:` 配 `cache=shared` 时同进程的测试会看见彼此的数据。
//   - 必须把连接数压到 1。内存库是"每连接一个库"，连接池开到多个时，
//     迁移建的表可能落在另一条连接看不到的库里，表现为"表不存在"这种莫名报错。
func newGrowthTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:growth_%d?mode=memory&cache=shared", time.Now().UnixNano())
	// Logger 静默：GetTodayStats 在没有启用场景时会主动查一次并吃掉 ErrRecordNotFound，
	// gorm 默认把这类查询打到 stderr，测试输出会被无关噪声淹没。
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("取底层连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("迁移模型失败: %v", err)
	}
	return db
}

// seedCheckIn 插入一条打卡记录。
func seedCheckIn(t *testing.T, db *gorm.DB, day time.Time, action string, scenarioID uint) {
	t.Helper()
	row := model.CheckIn{
		ReminderID:  1,
		ScenarioID:  scenarioID,
		Action:      action,
		Source:      model.SourceManual,
		ScheduledAt: day.Unix(),
		ActedAt:     day.Unix(),
		DateKey:     day.Format(model.DateLayout),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("插入打卡记录失败: %v", err)
	}
}

// TestTotalDoneDaysCountsDistinctDaysGlobally 覆盖累计口径的三条语义。
//
// 这三条缺一条，形态就会算错，而错法是"看起来只是长得慢"：
//  1. 同一天多次完成只算一天（DISTINCT date_key）；
//  2. 不同场景的记录都算（换场景不该让芽缩水）；
//  3. 只看 done，不看 snooze/skip；软删除的记录不算。
func TestTotalDoneDaysCountsDistinctDaysGlobally(t *testing.T) {
	db := newGrowthTestDB(t)
	svc := New(db)

	dayA := time.Date(2026, 10, 20, 9, 0, 0, 0, time.Local)
	dayB := dayA.AddDate(0, 0, 1)
	dayC := dayA.AddDate(0, 0, 2)

	// 同一天、不同场景、多次完成 → 只算 1 天
	seedCheckIn(t, db, dayA, model.ActionDone, 1)
	seedCheckIn(t, db, dayA, model.ActionDone, 2)
	seedCheckIn(t, db, dayA, model.ActionDone, 2)
	// 另一天，只有跳过 → 不算
	seedCheckIn(t, db, dayB, model.ActionSkip, 1)
	seedCheckIn(t, db, dayB, model.ActionSnooze, 1)
	// 第三天，完成 → 算 1 天
	seedCheckIn(t, db, dayC, model.ActionDone, 3)
	// 第四天，完成但已软删除 → 不算
	// （DeletedAt 在嵌入的 BaseModel 里，不能用复合字面量直接赋值，先造再打标记）
	deleted := model.CheckIn{
		ReminderID: 1, ScenarioID: 1, Action: model.ActionDone, Source: model.SourceManual,
		ScheduledAt: dayC.AddDate(0, 0, 1).Unix(),
		ActedAt:     dayC.AddDate(0, 0, 1).Unix(),
		DateKey:     dayC.AddDate(0, 0, 1).Format(model.DateLayout),
	}
	deleted.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true}
	if err := db.Create(&deleted).Error; err != nil {
		t.Fatalf("插入已删除记录失败: %v", err)
	}

	days, err := svc.totalDoneDays()
	if err != nil {
		t.Fatalf("统计累计完成天数失败: %v", err)
	}
	if days != 2 {
		t.Errorf("累计完成天数 = %d，期望 2（同一天多次只算一天、跨场景都算、跳过不算、软删除不算）", days)
	}
}

// TestPetGrowthKeepsShapeAcrossDays 是「芽永不回退」的端到端断言。
//
// 用可注入时钟（Service.now）模拟跨天：第 8 天的今日完成率为 0，
// 但形态必须等于长期档，且不低于前天——这正是用户每天早上看到的那一幕。
func TestPetGrowthKeepsShapeAcrossDays(t *testing.T) {
	db := newGrowthTestDB(t)
	svc := New(db)

	start := time.Date(2026, 10, 20, 9, 0, 0, 0, time.Local)
	// 连续 7 天各完成一次 → 长期档 = longTermStage(7) = 2
	for i := 0; i < 7; i++ {
		seedCheckIn(t, db, start.AddDate(0, 0, i), model.ActionDone, 1)
	}

	// 第 8 天：今天还没做任何事
	svc.now = func() time.Time { return start.AddDate(0, 0, 7) }
	growth, floor, err := svc.PetGrowth(0, false)
	if err != nil {
		t.Fatalf("读取生长档位失败: %v", err)
	}

	if growth.TotalDoneDays != 7 {
		t.Errorf("累计完成天数 = %d，期望 7", growth.TotalDoneDays)
	}
	if want := longTermStage(7); growth.TodayStage != GrowthStageSeedling {
		t.Errorf("今日神态档 = %d，期望 %d（今天还没完成）", growth.TodayStage, GrowthStageSeedling)
	} else if growth.Stage != want {
		t.Errorf("形态档 = %d，期望 %d（应等于长期档，不能因为跨天缩回 0）", growth.Stage, want)
	}
	if floor != growth.Stage {
		t.Errorf("回写高水位 = %d，期望与形态档 %d 一致", floor, growth.Stage)
	}
	if growth.Stage < longTermStage(7) {
		t.Errorf("形态档 %d 低于长期档 %d —— 芽在跨天时缩小了", growth.Stage, longTermStage(7))
	}
	if growth.DateKey != start.AddDate(0, 0, 7).Format(model.DateLayout) {
		t.Errorf("date_key = %q，期望跨天后的日期", growth.DateKey)
	}
	if growth.HasTarget {
		t.Error("没有启用场景时 has_target 应为 false")
	}

	// 播种后的下一次调用（真实链路里由 App 把 floor/seeded 回传）必须稳定。
	again, againFloor, err := svc.PetGrowth(floor, true)
	if err != nil {
		t.Fatalf("播种后再次读取失败: %v", err)
	}
	if again.Stage != growth.Stage || againFloor != floor {
		t.Errorf("播种后形态漂移：%d/%d → %d/%d", growth.Stage, floor, again.Stage, againFloor)
	}
}

// TestPetGrowthUpgradeDayKeepsShapeNextMorning 是「今日档高于长期档」的端到端回归。
//
// 旧实现 Stage=max(长期, 今日) 在这里会得到：第 1 天 4 档，第 2 天掉回 1 档——
// 正是「一夜之间退步」。新实现把第 1 天的 4 播种成高水位，第 2 天仍须保持 4。
func TestPetGrowthUpgradeDayKeepsShapeNextMorning(t *testing.T) {
	db := newGrowthTestDB(t)
	svc := New(db)

	day1 := time.Date(2026, 10, 20, 9, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return day1 }

	scenario, err := svc.CreateScenario("日常", "每天喝一次水")
	if err != nil {
		t.Fatalf("创建场景失败: %v", err)
	}
	if err := svc.SetScenarioEnabled(scenario.ID, true); err != nil {
		t.Fatalf("启用场景失败: %v", err)
	}
	item, err := svc.CreateReminder(ReminderInput{
		ScenarioID:  scenario.ID,
		Title:       "喝水",
		Kind:        model.KindWater,
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		DailyTarget: 1,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("创建提醒失败: %v", err)
	}
	// 第 1 天完成唯一一次目标：今日完成率 100%，但累计只有 1 天。
	if err := db.Create(&model.CheckIn{
		ReminderID:  item.ID,
		ScenarioID:  scenario.ID,
		Action:      model.ActionDone,
		Source:      model.SourceManual,
		ScheduledAt: day1.Unix(),
		ActedAt:     day1.Unix(),
		DateKey:     day1.Format(model.DateLayout),
	}).Error; err != nil {
		t.Fatalf("插入打卡记录失败: %v", err)
	}

	first, floor, err := svc.PetGrowth(0, false)
	if err != nil {
		t.Fatalf("第 1 天读取生长失败: %v", err)
	}
	if first.TodayStage != GrowthStageFull {
		t.Fatalf("第 1 天今日档 = %d，期望 %d", first.TodayStage, GrowthStageFull)
	}
	if !first.HasTarget {
		t.Error("有启用目标时 has_target 应为 true")
	}
	if first.Stage != GrowthStageFull || floor != GrowthStageFull {
		t.Fatalf("第 1 天形态/高水位 = %d/%d，期望 4/4", first.Stage, floor)
	}

	// 第 2 天还没完成：累计仍是 1 天（长期档 1），但高水位必须把形态留在 4。
	svc.now = func() time.Time { return day1.AddDate(0, 0, 1) }
	next, nextFloor, err := svc.PetGrowth(floor, true)
	if err != nil {
		t.Fatalf("第 2 天读取生长失败: %v", err)
	}
	if next.TodayStage != GrowthStageSeedling {
		t.Errorf("第 2 天今日档 = %d，期望 0", next.TodayStage)
	}
	if next.Stage != GrowthStageFull || nextFloor != GrowthStageFull {
		t.Errorf("第 2 天形态/高水位 = %d/%d，期望保持 4/4（不能回退）", next.Stage, nextFloor)
	}
}

// TestPetGrowthOnEmptyDatabase 保证空库/新用户不会报错也不会算出怪值。
//
// 新用户第一次打开宠物窗口就会调它；任何一次 SQL 失败都会让芽停在默认档，
// 而错误只会出现在日志里（宠物窗口没有 DevTools）。
func TestPetGrowthOnEmptyDatabase(t *testing.T) {
	db := newGrowthTestDB(t)
	svc := New(db)

	growth, floor, err := svc.PetGrowth(0, false)
	if err != nil {
		t.Fatalf("空库下读取生长档位失败: %v", err)
	}
	if growth.Stage != GrowthStageSeedling || growth.TodayStage != GrowthStageSeedling {
		t.Errorf("空库应得到 0/0 档，实际形态 %d / 今日 %d", growth.Stage, growth.TodayStage)
	}
	if floor != GrowthStageSeedling {
		t.Errorf("空库高水位 = %d，期望 0", floor)
	}
	if growth.TotalDoneDays != 0 {
		t.Errorf("空库累计完成天数 = %d，期望 0", growth.TotalDoneDays)
	}
	if growth.HasTarget {
		t.Error("空库没有目标，has_target 应为 false")
	}
}
