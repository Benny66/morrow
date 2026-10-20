package service

import (
	"my-notice/internal/apperr"
	"my-notice/internal/model"
	"my-notice/internal/page"
)

// Example 的 CRUD 方法直接挂在 Service 聚合上（数据访问经 s.db）。
// 绑定方法（app.go）不直接碰 db，业务逻辑全在这一层。
//
// 本文件是黄金范例：演示管道契约的正确写法——
//   - 列表方法返回【分页结果】而非裸切片；
//   - 分页参数经 Request.Normalized() 归一化后再查库；
//   - 业务错误经 apperr 构造（前端据 code 分流），系统错误经 apperr.Wrap。

// ListExamples 分页查询列表。
// TODO: 业务逻辑 —— 按需加 keyword 过滤、排序等（过滤条件加在此处，并计入 total）。
func (s *Service) ListExamples(req page.Request) (page.Result[model.Example], error) {
	req = req.Normalized() // 契约：查库前必须归一化页码/页大小

	var (
		list  []model.Example
		total int64
	)
	if err := s.db.Model(&model.Example{}).Count(&total).Error; err != nil {
		return page.Result[model.Example]{}, apperr.Wrap(err)
	}
	if err := s.db.Order("id DESC").
		Offset(req.Offset()).Limit(req.PageSize).
		Find(&list).Error; err != nil {
		return page.Result[model.Example]{}, apperr.Wrap(err)
	}
	return page.NewResult(req, list, total), nil
}

// GetExample 按 ID 查询单个。记录不存在返回 apperr.NotFound。
func (s *Service) GetExample(id uint) (*model.Example, error) {
	var item model.Example
	if err := s.db.First(&item, id).Error; err != nil {
		// 契约：记录不存在统一用 apperr.NotFound（code = not_found）。
		return nil, apperr.NotFound("Example 不存在")
	}
	return &item, nil
}

// CreateExample 新增。
func (s *Service) CreateExample(item *model.Example) error {
	if err := s.db.Create(item).Error; err != nil {
		return apperr.Wrap(err)
	}
	return nil
}

// UpdateExample 更新（全字段覆盖）。
func (s *Service) UpdateExample(item *model.Example) error {
	if err := s.db.Save(item).Error; err != nil {
		return apperr.Wrap(err)
	}
	return nil
}

// DeleteExample 删除（软删除）。目标不存在返回 apperr.NotFound。
func (s *Service) DeleteExample(id uint) error {
	res := s.db.Delete(&model.Example{}, id)
	if res.Error != nil {
		return apperr.Wrap(res.Error)
	}
	if res.RowsAffected == 0 {
		return apperr.NotFound("Example 不存在")
	}
	return nil
}
