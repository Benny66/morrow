// Package service 是业务服务层，介于绑定方法（app.go）与 model 之间。
//
// 分层纪律（由 guardrails 的 AST 护栏强制）：
//   - app.go 的绑定方法不得直接 import gorm/database，必须经本层的 service。
//   - service 层可以访问数据库（持有 *gorm.DB），model 层是叶子（不 import service/database）。
//
// 后续业务能力（资产等）在各自的 service 文件里实现，并在此注入数据库连接。
package service

import (
	"time"

	"gorm.io/gorm"
)

// Service 聚合所有业务服务，是绑定方法访问业务能力的唯一入口。
// 由启动时初始化并注入 App，App 不再直接持有 *gorm.DB。
type Service struct {
	db *gorm.DB
	// now 是业务层时钟。一次性提醒的「时间已过去」校验与智能补日期需要可注入的
	// 当前时间；生产默认 time.Now，测试可以固定时钟，不依赖运行环境的真实时间。
	now func() time.Time
}

// New 创建服务聚合，注入数据库连接。
func New(db *gorm.DB) *Service {
	return &Service{db: db, now: time.Now}
}

// Close 释放底层数据库连接，供优雅关闭时调用。
func (s *Service) Close() error {
	if s.db == nil {
		return nil
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
