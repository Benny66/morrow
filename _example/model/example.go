package model

// Example 是黄金范例模型。
// TODO: 业务逻辑 —— 按需修改字段名/类型/tag，或直接删除重写。
type Example struct {
	BaseModel
	// TODO: 业务字段，例如：
	// Name string `gorm:"size:100;not null" json:"name"`
}
