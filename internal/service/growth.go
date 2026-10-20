package service

// 生长档位（0–4）。完成率 → 档位的唯一真相在 growthStage()，前端只据档位切样式，
// 不镜像档位常量、不重算边界——铁律 1：判定逻辑只允许有一处实现。
const (
	// GrowthStageSeedling 小芽苗 / 未设目标时的健康小苗（完成率 0% 或 TargetTotal=0）。
	GrowthStageSeedling = 0
	// GrowthStageSprout 完成率 1%–33%。
	GrowthStageSprout = 1
	// GrowthStageHalf 完成率 34%–66%。
	GrowthStageHalf = 2
	// GrowthStageAlmost 完成率 67%–99%。
	GrowthStageAlmost = 3
	// GrowthStageFull 完成率 100%。
	GrowthStageFull = 4
)

// PetGrowth 是宠物「芽」生长所需的轻量只读视图。
//
// 只回传 date_key 与 stage：前端拿到 stage 直接映射 CSS class，不再接收 done/target，
// 避免「回传两个整数反而诱惑前端自己算档位」——档位是判定逻辑，唯一真相在后端。
type PetGrowth struct {
	// DateKey 统计所属的本地日期（YYYY-MM-DD），供前端日志定位翻篇。
	DateKey string `json:"date_key"`
	// Stage 生长档位 0..4（见 GrowthStage* 常量）。
	Stage int `json:"stage"`
}

// growthStage 把今日完成情况映射为 0..4 的生长档位（整数运算，避免浮点边界误差）。
//
// 边界归属：target<=0 或 done<=0 → 0；pct>=100 → 4；pct>=67 → 3；pct>=34 → 2；
// 其余（1–33）→ 1。done>target（超额完成）时 pct>100，自然归 4。
func growthStage(doneTotal, targetTotal int) int {
	if targetTotal <= 0 || doneTotal <= 0 {
		return GrowthStageSeedling
	}
	pct := doneTotal * 100 / targetTotal
	switch {
	case pct >= 100:
		return GrowthStageFull
	case pct >= 67:
		return GrowthStageAlmost
	case pct >= 34:
		return GrowthStageHalf
	default:
		return GrowthStageSprout
	}
}

// PetGrowth 返回宠物「芽」的生长档位。
//
// 【为什么内部复用 GetTodayStats】DoneTotal/TargetTotal 的唯一真相是 GetTodayStats
// （只统计「当前启用场景里、当前启用提醒」的今日完成/目标）。PetGrowth 直接调它、
// 投影 DateKey/DoneTotal/TargetTotal 再算档位，漂移面为零、改动面最小——任何另写的
// 计数 SQL 都是第二处真相。代价是 5 次本地 SQLite 廉价查询，对 4 秒量级可忽略。
func (s *Service) PetGrowth() (*PetGrowth, error) {
	stats, err := s.GetTodayStats()
	if err != nil {
		return nil, err
	}
	return &PetGrowth{
		DateKey: stats.DateKey,
		Stage:   growthStage(stats.DoneTotal, stats.TargetTotal),
	}, nil
}
