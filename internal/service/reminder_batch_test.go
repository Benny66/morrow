package service

import (
	"testing"
	"time"

	"morrow/internal/model"
)

// TestCreateRemindersIsAtomic 断言批量创建全成或全不成。
func TestCreateRemindersIsAtomic(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	_, err := svc.CreateReminders([]ReminderInput{
		{Title: "合法的", OnceAt: now.Add(time.Hour).Unix()},
		{Title: "已过去", OnceAt: now.Add(-time.Second).Unix()},
	})
	if err == nil {
		t.Fatal("批量里有一条非法项时应整体失败")
	}
	var count int64
	if err := svc.db.Model(&model.Reminder{}).Where("once_at > 0").Count(&count).Error; err != nil {
		t.Fatalf("统计提醒失败: %v", err)
	}
	if count != 0 {
		t.Errorf("批量失败后仍写入了 %d 条", count)
	}
}

// TestCreateRemindersMixedBatch 断言一次性与周期草稿能在同一个事务里落库。
func TestCreateRemindersMixedBatch(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	scenario, err := svc.CreateScenario("日常", "每小时喝水")
	if err != nil {
		t.Fatalf("创建场景失败: %v", err)
	}
	items, err := svc.CreateReminders([]ReminderInput{
		{
			ScenarioID:  scenario.ID,
			Title:       "喝水",
			Kind:        model.KindWater,
			IntervalMin: 60,
			WindowStart: "09:00",
			WindowEnd:   "18:00",
			DailyTarget: 8,
			Enabled:     true,
		},
		{Title: "吃苹果", OnceAt: now.Add(time.Hour).Unix()},
	})
	if err != nil {
		t.Fatalf("混合批量创建失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("返回条数 = %d，期望 2", len(items))
	}
	if items[0].ScenarioID == nil || *items[0].ScenarioID != scenario.ID || items[0].OnceAt != 0 {
		t.Errorf("周期项字段不符: %+v", items[0])
	}
	if items[1].ScenarioID != nil || items[1].OnceAt == 0 {
		t.Errorf("一次性项字段不符: %+v", items[1])
	}
}

func TestCreateRemindersAllValid(t *testing.T) {
	now := time.Date(2026, time.March, 2, 15, 0, 0, 0, time.Local)
	svc := newOneOffTestService(t, now)
	items, err := svc.CreateReminders([]ReminderInput{
		{Title: "第一条", OnceAt: now.Add(time.Hour).Unix()},
		{Title: "第二条", OnceAt: now.Add(2 * time.Hour).Unix()},
	})
	if err != nil {
		t.Fatalf("批量创建失败: %v", err)
	}
	if len(items) != 2 || items[0].Title != "第一条" || items[1].Title != "第二条" {
		t.Fatalf("批量返回不符: %+v", items)
	}
	var count int64
	if err := svc.db.Model(&model.Reminder{}).Where("once_at > 0").Count(&count).Error; err != nil {
		t.Fatalf("统计提醒失败: %v", err)
	}
	if count != 2 {
		t.Errorf("写入条数 = %d，期望 2", count)
	}
}
