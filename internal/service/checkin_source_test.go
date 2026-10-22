package service

import (
	"testing"
	"time"

	"morrow/internal/model"
)

// TestCompleteReminderWritesManualSource 保证现有手动打卡路径仍写 manual。
func TestCompleteReminderWritesManualSource(t *testing.T) {
	svc := newReminderTestService(t)
	reminderID := seedScenarioAndReminder(t, svc)
	fireID := seedFired(t, svc, reminderID, time.Now().Add(-time.Minute).Unix())

	if err := svc.CompleteReminder(fireID); err != nil {
		t.Fatalf("完成提醒失败: %v", err)
	}

	var record model.CheckIn
	if err := svc.db.First(&record).Error; err != nil {
		t.Fatalf("查询打卡记录失败: %v", err)
	}
	if record.Source != model.SourceManual {
		t.Errorf("source = %q，期望 %q", record.Source, model.SourceManual)
	}
}

// TestActWithSourceWritesGivenSource 覆盖主动休息/锁屏抵扣走同一事务的入口。
func TestActWithSourceWritesGivenSource(t *testing.T) {
	svc := newReminderTestService(t)
	reminderID := seedScenarioAndReminder(t, svc)
	fireID := seedFired(t, svc, reminderID, time.Now().Add(-time.Minute).Unix())

	if err := svc.actWithSource(fireID, model.ActionDone, 0, model.SourceActiveBreak); err != nil {
		t.Fatalf("带 source 完成失败: %v", err)
	}

	var record model.CheckIn
	if err := svc.db.First(&record).Error; err != nil {
		t.Fatalf("查询打卡记录失败: %v", err)
	}
	if record.Source != model.SourceActiveBreak {
		t.Errorf("source = %q，期望 %q", record.Source, model.SourceActiveBreak)
	}
}

// TestCheckInSourceDefaultsToManual 旧行/零值写入必须落到 manual 默认值。
func TestCheckInSourceDefaultsToManual(t *testing.T) {
	svc := newReminderTestService(t)
	record := &model.CheckIn{
		ReminderID: 1,
		ScenarioID: 1,
		Action:     model.ActionDone,
		ActedAt:    time.Now().Unix(),
		DateKey:    time.Now().Format(model.DateLayout),
	}
	if err := svc.db.Create(record).Error; err != nil {
		t.Fatalf("写入打卡记录失败: %v", err)
	}
	var got model.CheckIn
	if err := svc.db.First(&got, record.ID).Error; err != nil {
		t.Fatalf("查询打卡记录失败: %v", err)
	}
	if got.Source != model.SourceManual {
		t.Errorf("source = %q，期望默认值 %q", got.Source, model.SourceManual)
	}
}
