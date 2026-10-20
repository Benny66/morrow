package model

// Scenario 是一个「使用场景」：用户用自然语言描述自己的处境（例如「我在备孕，
// 不能久坐」），AI 据此一次性产出整套提醒计划。
//
// 【为什么同时只能启用一个场景】通知是打断性的稀缺资源。多场景同时启用会让同一个人
// 在一小时里收到「喝水 + 起身 + 走动 + 如厕」四路通知，用户的第一反应是把通知关掉，
// 提醒应用就此失效。故「启用」是单选语义，由 service 层的 SetScenarioEnabled 保证；
// 这里不加数据库约束——SQLite 的部分唯一索引依赖方言，而这条规则只有一处写入口，
// 在入口处用事务表达比在 schema 里表达更易读、也更容易在测试里构造。
type Scenario struct {
	BaseModel

	// Name 场景名（如「备孕办公日」），用于列表展示与通知副标题。
	Name string `gorm:"size:50;not null" json:"name"`

	// Description 用户的自然语言描述，是 AI 生成计划的唯一输入。
	// 存全量原文：用户回头想微调计划时需要看到自己当初怎么说的。
	Description string `gorm:"type:text" json:"description"`

	// Summary 是 AI 对这套计划的一句话总结（生成时写入），供场景页展示。
	Summary string `gorm:"size:500" json:"summary"`

	// Enabled 是否启用。同一时刻至多一行为 true（见类型注释）。
	Enabled bool `gorm:"not null;default:false;index" json:"enabled"`

	// GeneratedAt 最近一次用 AI 生成计划的时间（Unix 秒）。0 表示从未生成过。
	GeneratedAt int64 `gorm:"not null;default:0" json:"generated_at"`
}
