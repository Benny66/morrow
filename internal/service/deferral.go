package service

// DeferralState 是自然停顿顺延的可绑定视图（宠物气泡用）。
type DeferralState struct {
	// Active 是否有提醒正在顺延。
	Active bool `json:"active"`
	// Since 最早一轮顺延的开始时间（Unix 秒；0 表示无顺延）。
	Since int64 `json:"since"`
	// Supported 当前平台是否支持读取系统空闲时长。
	Supported bool `json:"supported"`
}
