package service

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"morrow/internal/ai"
	"morrow/internal/apperr"
	"morrow/internal/model"
)

// GeneratePlanResult 是 AI 生成计划的结果。
//
// 同时返回场景与新提醒项：前端生成成功后要立刻刷新场景卡片的 summary 与计划页，
// 让前端再各查一次会把一次成功变成三次往返，也更容易出现半新半旧的界面。
type GeneratePlanResult struct {
	// Scenario 生成后的场景（Summary 与 GeneratedAt 已更新）。
	Scenario *model.Scenario `json:"scenario"`
	// Reminders 新落库的提醒项（按 AI 返回的顺序）。
	Reminders []model.Reminder `json:"reminders"`
}

// GeneratePlan 调用 AI 生成整套提醒计划，并整体替换该场景下的提醒项。
//
// 【事务语义】"替换"必须原子：先删后插若中途失败，用户会得到一个空计划
// （既没有旧计划也没有新计划）。整体包在事务里，失败时旧计划原样保留——
// 用户至少还有上一次能用的计划，而不是被自己的一次"重新生成"清空。
func (s *Service) GeneratePlan(settings ai.Settings, scenarioID uint) (*GeneratePlanResult, error) {
	scenario, err := s.GetScenario(scenarioID)
	if err != nil {
		return nil, err
	}

	client := ai.NewClient(settings)
	ctx, cancel := context.WithTimeout(context.Background(), ai.RequestTimeout)
	// 取消必须发生在请求结束之后：defer 保证异常路径也会释放 context。
	defer cancel()

	plan, generateErr := client.GeneratePlan(ctx, scenario.Description, nil)
	if generateErr != nil {
		return nil, apperr.Wrap(generateErr)
	}

	now := s.now()
	created := make([]model.Reminder, 0, len(plan.Reminders))
	summary := plan.Summary

	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		// 先记旧提醒 ID，再软删：软删之后正常查询就看不到它们了。
		var oldIDs []uint
		if err := tx.Model(&model.Reminder{}).Where("scenario_id = ?", scenarioID).
			Pluck("id", &oldIDs).Error; err != nil {
			return fmt.Errorf("查询旧计划失败: %w", err)
		}
		if len(oldIDs) > 0 {
			// 旧计划名下未处理的触发记录必须清掉：它们对应的提醒项马上就不存在了，
			// 留在待处理列表里会变成点了就报"提醒项已删除"的死卡片。
			if err := tx.Unscoped().Where("reminder_id IN ?", oldIDs).
				Delete(&model.ReminderFire{}).Error; err != nil {
				return fmt.Errorf("清理旧触发记录失败: %w", err)
			}
		}
		// 提醒项软删：打卡记录里引用的 reminder_id 仍能解读，历史统计不丢。
		if err := tx.Where("scenario_id = ?", scenarioID).
			Delete(&model.Reminder{}).Error; err != nil {
			return fmt.Errorf("删除旧计划失败: %w", err)
		}

		for index, draft := range plan.Reminders {
			// 每条提醒持有自己的场景 ID 副本：共用同一个指针会让序列化结果
			// 依赖循环外的变量生命周期，读起来也更危险。
			scenarioIDCopy := scenarioID
			item := model.Reminder{
				ScenarioID:  &scenarioIDCopy,
				Title:       draft.Title,
				Content:     draft.Content,
				Kind:        draft.Kind,
				IntervalMin: draft.IntervalMin,
				WindowStart: draft.WindowStart,
				WindowEnd:   draft.WindowEnd,
				DailyTarget: draft.DailyTarget,
				Enabled:     true,
				SortOrder:   index,
			}
			if err := tx.Create(&item).Error; err != nil {
				return fmt.Errorf("保存提醒项失败: %w", err)
			}
			created = append(created, item)
		}

		return tx.Model(&model.Scenario{}).Where("id = ?", scenarioID).
			Updates(map[string]any{"summary": summary, "generated_at": now.Unix()}).Error
	})
	if txErr != nil {
		return nil, apperr.Wrap(txErr)
	}

	// 内存里的场景对象跟上落库结果，免得前端拿到的 summary 还是旧值。
	scenario.Summary = summary
	scenario.GeneratedAt = now.Unix()

	return &GeneratePlanResult{Scenario: scenario, Reminders: created}, nil
}

// TestAIConnection 用给定配置试连一次 AI 服务，返回可展示的成功说明。
//
// 只在设置页点「测试连接」时调用：让用户在填写阶段就发现 base_url / 模型名 / Key
// 哪里不对，而不是等到写完场景点「生成」才发现。
func (s *Service) TestAIConnection(settings ai.Settings) (string, error) {
	client := ai.NewClient(settings)
	ctx, cancel := context.WithTimeout(context.Background(), ai.RequestTimeout)
	defer cancel()

	message, err := client.TestConnection(ctx)
	if err != nil {
		return "", apperr.Wrap(err)
	}
	return message, nil
}
