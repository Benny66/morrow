package service

import (
	"context"

	"morrow/internal/ai"
	"morrow/internal/apperr"
)

// GenerateQuickAddDrafts 用一句话 + 可选附件生成快速添加草稿（不落库）。
//
// 附件支持文件路径与粘贴/拖拽图片；一次性与周期草稿都可能出现，保存时由
// CreateReminders 统一校验（周期项必须有场景）。
func (s *Service) GenerateQuickAddDrafts(settings ai.Settings, description string, filePaths []string, inlineImages []InlineImage) (*ai.QuickAddPlan, error) {
	attachments, err := LoadAttachmentsWithInline(filePaths, inlineImages)
	if err != nil {
		return nil, err
	}
	client := ai.NewClient(settings)
	ctx, cancel := context.WithTimeout(context.Background(), ai.RequestTimeout)
	defer cancel()

	plan, err := client.GenerateQuickAdd(ctx, description, attachments, s.now())
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	return &plan, nil
}
