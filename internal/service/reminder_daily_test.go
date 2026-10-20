package service

import (
	"testing"
	"time"

	"morrow/internal/model"
)

// TestDailySinglePointRemainsEnabledAfterCompletion 守住「旧 once 收编」后的语义：
// interval=0 是每日单点，完成一次只结束当天的触发，不会把提醒永久停用。
func TestDailySinglePointRemainsEnabledAfterCompletion(t *testing.T) {
	svc := newReminderTestService(t)
	scenario, err := svc.CreateScenario("日常", "每天固定时刻提醒一次")
	if err != nil {
		t.Fatalf("创建场景失败: %v", err)
	}
	item, err := svc.CreateReminder(ReminderInput{
		ScenarioID:  scenario.ID,
		Title:       "吃药",
		Content:     "每天同一时刻一次",
		Kind:        model.KindCustom,
		IntervalMin: 0,
		WindowStart: "16:00",
		WindowEnd:   "18:00",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("创建每日单点失败: %v", err)
	}
	if !item.Enabled {
		t.Fatal("新建的每日单点应为启用状态")
	}

	fireID := seedFired(t, svc, item.ID, time.Now().Add(-time.Minute).Unix())
	if err := svc.CompleteReminder(fireID); err != nil {
		t.Fatalf("完成提醒失败: %v", err)
	}

	var stored model.Reminder
	if err := svc.db.First(&stored, item.ID).Error; err != nil {
		t.Fatalf("查询提醒失败: %v", err)
	}
	if !stored.Enabled {
		t.Error("每日单点在完成后被停用了；它应当次日继续触发")
	}
	if stored.IntervalMin != 0 {
		t.Errorf("interval_min = %d，期望保持 0", stored.IntervalMin)
	}
}
