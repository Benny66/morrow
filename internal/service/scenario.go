package service

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"morrow/internal/apperr"
	"morrow/internal/model"
)

// 场景字段的字符上限（与 model.Scenario 的列宽一致，越界在写入口拦下）。
const (
	// maxScenarioNameRunes 场景名字符上限。
	maxScenarioNameRunes = 50
	// maxScenarioDescRunes 描述字符上限。描述是 AI 的唯一输入，给足空间。
	maxScenarioDescRunes = 2000
)

// ListScenarios 返回全部场景，最近创建的在前。
func (s *Service) ListScenarios() ([]model.Scenario, error) {
	list := make([]model.Scenario, 0, 4)
	if err := s.db.Order("id DESC").Find(&list).Error; err != nil {
		return nil, apperr.Wrap(fmt.Errorf("查询场景失败: %w", err))
	}
	return list, nil
}

// GetScenario 按 ID 取场景。
func (s *Service) GetScenario(id uint) (*model.Scenario, error) {
	var scenario model.Scenario
	if err := s.db.First(&scenario, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("场景不存在")
		}
		return nil, apperr.Wrap(fmt.Errorf("查询场景失败: %w", err))
	}
	return &scenario, nil
}

// CreateScenario 新建场景，默认不启用（启用是用户的显式动作，见 SetScenarioEnabled）。
func (s *Service) CreateScenario(name, description string) (*model.Scenario, error) {
	cleanName, cleanDesc, invalid := normalizeScenarioFields(name, description)
	if invalid != nil {
		return nil, invalid
	}
	scenario := &model.Scenario{Name: cleanName, Description: cleanDesc}
	if err := s.db.Create(scenario).Error; err != nil {
		return nil, apperr.Wrap(fmt.Errorf("创建场景失败: %w", err))
	}
	return scenario, nil
}

// UpdateScenario 修改场景名称与描述。描述是 AI 的输入，改它不影响已生成的计划
// （计划是快照）：用户想应用新描述时点「重新生成」。
func (s *Service) UpdateScenario(id uint, name, description string) error {
	if _, err := s.GetScenario(id); err != nil {
		return err
	}
	cleanName, cleanDesc, invalid := normalizeScenarioFields(name, description)
	if invalid != nil {
		return invalid
	}
	if err := s.db.Model(&model.Scenario{}).Where("id = ?", id).
		Updates(map[string]any{"name": cleanName, "description": cleanDesc}).Error; err != nil {
		return apperr.Wrap(fmt.Errorf("更新场景失败: %w", err))
	}
	return nil
}

// SetScenarioEnabled 启用/停用场景。
//
// 【单选语义】启用一个场景会先把其他场景停用。放在同一事务里，避免"两个都启用"
// 的中间态被调度引擎读到（引擎每 20 秒读一次库，中间态一旦落库就真的会响）。
func (s *Service) SetScenarioEnabled(id uint, enabled bool) error {
	if _, err := s.GetScenario(id); err != nil {
		return err
	}
	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		if enabled {
			if err := tx.Model(&model.Scenario{}).
				Where("id <> ? AND enabled = ?", id, true).
				Update("enabled", false).Error; err != nil {
				return fmt.Errorf("停用其他场景失败: %w", err)
			}
		}
		if err := tx.Model(&model.Scenario{}).Where("id = ?", id).
			Update("enabled", enabled).Error; err != nil {
			return fmt.Errorf("更新场景状态失败: %w", err)
		}
		return nil
	})
	if txErr != nil {
		return apperr.Wrap(txErr)
	}
	return nil
}

// DeleteScenario 删除场景，并级联清理它名下的提醒、触发记录与打卡记录。
//
// 【为什么三类数据用不同的删除方式】
//   - 场景与提醒项软删：删错时还有挽回余地，打卡记录里引用的 reminder_id 也还能解读；
//   - 触发记录与打卡记录硬删：它们是派生数据（去重与统计），且触发表上有
//     (reminder_id, fire_at) 唯一索引——软删的行仍然占着索引位置，会让将来
//     同 ID 复用直接撞冲突。场景都没了，这些记录也失去归属。
func (s *Service) DeleteScenario(id uint) error {
	if _, err := s.GetScenario(id); err != nil {
		return err
	}
	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		// 先取提醒 ID：后面软删提醒之后就查不到了（gorm 会自动加 deleted_at IS NULL）。
		var reminderIDs []uint
		if err := tx.Model(&model.Reminder{}).Where("scenario_id = ?", id).
			Pluck("id", &reminderIDs).Error; err != nil {
			return fmt.Errorf("查询场景下提醒失败: %w", err)
		}
		if len(reminderIDs) > 0 {
			if err := tx.Unscoped().Where("reminder_id IN ?", reminderIDs).
				Delete(&model.ReminderFire{}).Error; err != nil {
				return fmt.Errorf("删除触发记录失败: %w", err)
			}
		}
		if err := tx.Unscoped().Where("scenario_id = ?", id).
			Delete(&model.CheckIn{}).Error; err != nil {
			return fmt.Errorf("删除打卡记录失败: %w", err)
		}
		if err := tx.Where("scenario_id = ?", id).Delete(&model.Reminder{}).Error; err != nil {
			return fmt.Errorf("删除提醒项失败: %w", err)
		}
		if err := tx.Delete(&model.Scenario{}, id).Error; err != nil {
			return fmt.Errorf("删除场景失败: %w", err)
		}
		return nil
	})
	if txErr != nil {
		return apperr.Wrap(txErr)
	}
	return nil
}

// normalizeScenarioFields 归一化并校验场景的名称与描述。
//
// 返回 *apperr.Error 而不是 error：调用方全是绑定方法的直通路径，
// 用具体类型可以避免"typed nil 装进 error 接口后不再是 nil"那类坑。
func normalizeScenarioFields(name, description string) (string, string, *apperr.Error) {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return "", "", apperr.Validation("场景名称不能为空")
	}
	if utf8.RuneCountInString(cleanName) > maxScenarioNameRunes {
		return "", "", apperr.Validation(fmt.Sprintf("场景名称不能超过 %d 个字", maxScenarioNameRunes))
	}
	cleanDesc := strings.TrimSpace(description)
	if cleanDesc == "" {
		return "", "", apperr.Validation("请描述你的场景，AI 需要它来生成提醒计划")
	}
	if utf8.RuneCountInString(cleanDesc) > maxScenarioDescRunes {
		return "", "", apperr.Validation(fmt.Sprintf("场景描述不能超过 %d 个字", maxScenarioDescRunes))
	}
	return cleanName, cleanDesc, nil
}
