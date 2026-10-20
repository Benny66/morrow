// Package model 是所有数据模型的唯一注册真相。
//
// 新增模型时，只需：
//  1. 在本目录下新建模型文件，定义结构体（建议内嵌 BaseModel）。
//  2. 在下方 AllModels() 切片中登记该结构体指针。
//
// 启动时 migrate 会遍历 AllModels() 调用 AutoMigrate 自动建表。
// 护栏（guardrails change）会对本注册表做双向校验：
// 每个带 BaseModel 的结构体都必须登记，且登记项必须真实存在。
package model

import "gorm.io/gorm"

// BaseModel 所有持久化模型的公共字段。
type BaseModel struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt int64          `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt int64          `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// AllModels 返回所有待迁移的模型。
// 这是模型注册的唯一真相——不要在别处另写一份模型清单。
// 新增模型时在本切片登记；make gen 会通过下方锚点自动注入新模块。
func AllModels() []interface{} {
	return []interface{}{
		// gen:model
	}
}
