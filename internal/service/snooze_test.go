package service

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"morrow/internal/apperr"
	"morrow/internal/model"
)

// newReminderTestService 建一个临时库（迁移全部业务模型）并返回服务实例。
//
// 【为什么用临时文件而不是 ":memory:"】SQLite 的内存库是「每条连接一个库」，
// gorm 的连接池一旦开出第二条连接就会看到一个空库，测试会随机失败。
func newReminderTestService(t *testing.T) *Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "reminder-test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("迁移业务模型失败: %v", err)
	}
	return New(db)
}

// seedScenarioAndReminder 建一个场景与一条提醒，返回提醒项 ID。
func seedScenarioAndReminder(t *testing.T, svc *Service) uint {
	t.Helper()
	scenario, err := svc.CreateScenario("备孕办公日", "每 45 分钟起身站立 5 分钟；每小时喝一杯水")
	if err != nil {
		t.Fatalf("创建场景失败: %v", err)
	}
	item, err := svc.CreateReminder(ReminderInput{
		ScenarioID:  scenario.ID,
		Title:       "喝水",
		Content:     "起身倒一杯温水，慢慢喝完",
		Kind:        model.KindWater,
		IntervalMin: 60,
		WindowStart: "09:00",
		WindowEnd:   "18:00",
		DailyTarget: 8,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("创建提醒失败: %v", err)
	}
	return item.ID
}

// seedFired 占用一个已到点的计划触发，得到一条「待处理」（fired）记录。
func seedFired(t *testing.T, svc *Service, reminderID uint, fireAt int64) uint {
	t.Helper()
	fireID, claimed, err := svc.ClaimFire(reminderID, fireAt)
	if err != nil {
		t.Fatalf("占用触发点失败: %v", err)
	}
	if !claimed {
		t.Fatalf("触发点 %d 已被占用，测试数据不干净", fireAt)
	}
	return fireID
}

// TestSnoozeMarksOriginalDeferred 是「点推迟后 20 秒又弹」这个 bug 的回归测试。
//
// 原实现把被推迟的【原记录】也写成 status=snooze，而它的 fire_at 停在过去的计划时间，
// 于是 DueSnoozeFires（status=snooze AND fire_at<=now）在下一轮 tick 把它捞回来重弹。
// 修复后原记录落 FireDeferred（终态、不参与补发查询），只有新记录是 snooze。
func TestSnoozeMarksOriginalDeferred(t *testing.T) {
	svc := newReminderTestService(t)
	reminderID := seedScenarioAndReminder(t, svc)
	fireID := seedFired(t, svc, reminderID, time.Now().Add(-time.Minute).Unix())

	if err := svc.SnoozeReminder(fireID, 5); err != nil {
		t.Fatalf("推迟失败: %v", err)
	}

	var original model.ReminderFire
	if err := svc.db.First(&original, fireID).Error; err != nil {
		t.Fatalf("查询原记录失败: %v", err)
	}
	if original.Status != model.FireDeferred {
		t.Fatalf("原记录状态 = %q，期望 %q（写成 snooze 会让它在下一轮 tick 被捞回来重弹）",
			original.Status, model.FireDeferred)
	}

	// 推迟还没到期：此刻不该有任何待补发记录。
	due, err := svc.DueSnoozeFires(time.Now().Unix())
	if err != nil {
		t.Fatalf("查询待补发记录失败: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("推迟尚未到期就有 %d 条待补发记录——原记录被当成了待补发", len(due))
	}

	// 把时间拨到推迟之后：应当只有新记录那一条，且不是原记录。
	later, err := svc.DueSnoozeFires(time.Now().Add(10 * time.Minute).Unix())
	if err != nil {
		t.Fatalf("查询待补发记录失败: %v", err)
	}
	if len(later) != 1 {
		t.Fatalf("到点后待补发记录 = %d 条，期望 1 条", len(later))
	}
	if later[0].FireID == fireID {
		t.Error("待补发的是原记录——这正是「推迟后立刻重弹」的成因")
	}
	if later[0].ReminderID != reminderID {
		t.Errorf("待补发记录的 reminder_id = %d，期望 %d", later[0].ReminderID, reminderID)
	}
}

// TestSecondSnoozeSupersedesPending 断言同一条提醒同时只存在一个待补发的推迟。
func TestSecondSnoozeSupersedesPending(t *testing.T) {
	svc := newReminderTestService(t)
	reminderID := seedScenarioAndReminder(t, svc)

	now := time.Now()
	first := seedFired(t, svc, reminderID, now.Add(-2*time.Minute).Unix())
	if err := svc.SnoozeReminder(first, 5); err != nil {
		t.Fatalf("第一次推迟失败: %v", err)
	}
	second := seedFired(t, svc, reminderID, now.Add(-time.Minute).Unix())
	if err := svc.SnoozeReminder(second, 30); err != nil {
		t.Fatalf("第二次推迟失败: %v", err)
	}

	var pending int64
	if err := svc.db.Model(&model.ReminderFire{}).
		Where("reminder_id = ? AND status = ?", reminderID, model.FireSnooze).
		Count(&pending).Error; err != nil {
		t.Fatalf("统计待补发记录失败: %v", err)
	}
	if pending != 1 {
		t.Errorf("待补发记录 = %d 条，期望 1 条（旧的那条应被作废）", pending)
	}

	due, err := svc.DueSnoozeFires(now.Add(60 * time.Minute).Unix())
	if err != nil {
		t.Fatalf("查询待补发记录失败: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("到点后待补发记录 = %d 条，期望 1 条", len(due))
	}
	if due[0].FireID == first || due[0].FireID == second {
		t.Error("待补发的仍是被推迟过的记录——新的待补发记录没有覆盖旧的")
	}
}

// TestActRejectsAlreadyHandledFire 断言重复打卡被拒，且不会多记 CheckIn 污染统计。
func TestActRejectsAlreadyHandledFire(t *testing.T) {
	svc := newReminderTestService(t)
	reminderID := seedScenarioAndReminder(t, svc)
	fireID := seedFired(t, svc, reminderID, time.Now().Add(-time.Minute).Unix())

	if err := svc.CompleteReminder(fireID); err != nil {
		t.Fatalf("首次完成失败: %v", err)
	}
	repeatErr := svc.CompleteReminder(fireID)
	if repeatErr == nil {
		t.Fatal("对已完成的记录再次打卡应返回错误")
	}
	var appErr *apperr.Error
	if !errors.As(repeatErr, &appErr) || appErr.Code != apperr.CodeValidation {
		t.Fatalf("重复打卡的错误 = %v，期望 validation", repeatErr)
	}

	var count int64
	if err := svc.db.Model(&model.CheckIn{}).Count(&count).Error; err != nil {
		t.Fatalf("统计打卡记录失败: %v", err)
	}
	if count != 1 {
		t.Errorf("打卡记录 = %d 条，期望 1 条（重复打卡污染了今日统计）", count)
	}
}

// TestActRejectsDeferredFire 断言被推迟过的原记录不能再被打卡。
//
// 它与上一条同属「终态守卫」：deferred 是终态，若允许再打一次卡，
// 用户从一张过期的界面上点「完成」也能虚增一次完成数。
func TestActRejectsDeferredFire(t *testing.T) {
	svc := newReminderTestService(t)
	reminderID := seedScenarioAndReminder(t, svc)
	fireID := seedFired(t, svc, reminderID, time.Now().Add(-time.Minute).Unix())

	if err := svc.SnoozeReminder(fireID, 5); err != nil {
		t.Fatalf("推迟失败: %v", err)
	}
	if err := svc.CompleteReminder(fireID); err == nil {
		t.Fatal("对已推迟的记录再点完成应返回错误")
	}
}
