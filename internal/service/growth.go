package service

import (
	"fmt"

	"morrow/internal/apperr"
	"morrow/internal/model"
)

// 生长档位（0–4）。完成率 → 档位的唯一真相在 growthStage()，前端只据档位切样式，
// 不镜像档位常量、不重算边界——铁律 1：判定逻辑只允许有一处实现。
//
// 同一组档位被两条轨道复用：长期轨（longTermStage，累计完成天数）决定芽的**形态**，
// 今日轨（growthStage，今日完成率）决定**神态**。两者共用 0..4 的刻度，
// 但语义不同，不要互相代入。
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
// 【两个档位，不是一个】形态看长期、神态看今日：
//   - Stage 是**最终形态**档：max(持久化高水位, 长期档)，单调不减；
//   - TodayStage 是**今日**档，只用于神态（高光 / 是否轻垂）。
//
// 之前的版本只有一个 Stage 且等于今日档，导致 00:00 一过芽就缩回去——
// 用户看到的是"我一夜之间退步了"，与「陪伴」的定位相反。
type PetGrowth struct {
	// DateKey 统计所属的本地日期（YYYY-MM-DD），供前端日志定位翻篇。
	DateKey string `json:"date_key"`
	// Stage 最终形态档位 0..4（= max(持久化高水位, 长期档)）。
	Stage int `json:"stage"`
	// TodayStage 今日档位 0..4（只驱动神态，不驱动尺寸）。
	TodayStage int `json:"today_stage"`
	// TotalDoneDays 累计完成天数（有完成记录的不同本地日期数），形态的长期依据。
	TotalDoneDays int `json:"total_done_days"`
	// HasTarget 今日是否有启用中的目标。false 时前端不应把「0%」显示成蔫，
	// 那是"没安排"而不是"没做到"。
	HasTarget bool `json:"has_target"`
}

// longTermStageThresholds 是「累计完成天数 → 形态档位」的阈值表（唯一真相）。
//
// 语义：days >= 阈值 即达到该档；表长必须等于 GrowthStageFull+1，
// 否则 max 档永远摸不到（由 growth 测试里的护栏断言）。调阈值只改这一行。
var longTermStageThresholds = []int{1, 7, 30, 100}

// longTermStage 把累计完成天数映射为 0..4 的形态档位。
//
// 【为什么用「累计完成天数」而不是连续天数 streak】单调性由查询本身保证：
// 累计只会增加，断签不会让芽缩回去。用 streak 则需要跨提醒聚合与连续性判断，
// 还要另存"历史最好成绩"；累计天数一句 COUNT(DISTINCT date_key) 就够。
// （composePetShape 里也有一份高水位，但它只用于版本升级迁移，不是 streak 成绩。）
func longTermStage(days int) int {
	stage := 0
	for _, threshold := range longTermStageThresholds {
		if days < threshold {
			break
		}
		stage++
	}
	if stage > GrowthStageFull {
		stage = GrowthStageFull
	}
	return stage
}

// composePetShape 计算本次要显示的形态档，以及需要持久化的高水位。
//
// 【为什么不是简单的 max(长期档, 今日档)】今日档每天归零，而累计 1–99 天的用户
// 长期档只有 1–3；若把今日档并进形态，一个 100% 的今天会得到 4 档、次日又掉回 1 档，
// "一夜之间退步"会原样复现。故形态只由「长期档」与「持久化高水位」取大：
//
//   - seeded=false 表示这是旧版本配置的首次运行（升级）：用旧版本会显示的值
//     （今日档）与长期档取大，作为高水位播种一次，避免升级当天突然缩小；
//     seeded=true 之后今日档不再参与形态，只走 PetGrowth.TodayStage 驱动神态。
//     全新安装由 config.Default 直接置 seeded=true、高水位 0，不参考今日档。
//
// 高水位只增不减，形态因此单调；持久化由调用方（App 绑定层）负责，service 不碰配置。
func composePetShape(floor, longTerm, today int, seeded bool) (stage, nextFloor int) {
	floor = clampStage(floor)
	longTerm = clampStage(longTerm)
	today = clampStage(today)
	if !seeded && today > floor {
		floor = today
	}
	if longTerm > floor {
		floor = longTerm
	}
	return floor, floor
}

// clampStage 把档位夹进 0..GrowthStageFull；配置是用户可手改的文件，不能信。
func clampStage(stage int) int {
	if stage < GrowthStageSeedling {
		return GrowthStageSeedling
	}
	if stage > GrowthStageFull {
		return GrowthStageFull
	}
	return stage
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

// PetGrowth 返回宠物「芽」的生长档位（形态 + 今日神态，供宠物窗口轮询）。
//
// shapeFloor / seeded 由调用方从配置读取：floor 是已持久化的形态高水位；
// seeded=false 表示旧版本配置尚未播种（升级后的首次运行），此时把今日档并入
// 高水位一次；全新安装由 config.Default 直接置 seeded=true、高水位 0。
// 返回值第二个 int 是本次需要回写的高水位——调用方负责持久化。
//
// 【为什么内部复用 GetTodayStats】DoneTotal/TargetTotal 的唯一真相是 GetTodayStats
// （只统计「当前启用场景里、当前启用提醒」的今日完成/目标）。PetGrowth 直接调它、
// 投影出今日档位，漂移面为零、改动面最小——任何另写的计数 SQL 都是第二处真相。
//
// 【累计完成天数为什么不按场景过滤】今天的统计必须跟随启用场景（换场景就该换目标），
// 但**形态不能跟着换**：用户切到另一个场景，芽不该瞬间缩一级。故累计口径是全局的，
// 只按 action 过滤。代价是「累计」包含已删除场景的记录——这是刻意的，
// 删除场景同样不该抹掉已经长出来的芽。
func (s *Service) PetGrowth(shapeFloor int, seeded bool) (*PetGrowth, int, error) {
	stats, err := s.GetTodayStats()
	if err != nil {
		return nil, 0, err
	}
	days, err := s.totalDoneDays()
	if err != nil {
		return nil, 0, err
	}
	todayStage := growthStage(stats.DoneTotal, stats.TargetTotal)
	stage, nextFloor := composePetShape(shapeFloor, longTermStage(days), todayStage, seeded)
	return &PetGrowth{
		DateKey:       stats.DateKey,
		Stage:         stage,
		TodayStage:    todayStage,
		TotalDoneDays: days,
		HasTarget:     stats.TargetTotal > 0,
	}, nextFloor, nil
}

// totalDoneDays 返回累计完成天数：有完成动作的不同本地日期数。
//
// 用 COUNT(DISTINCT date_key) 而不是把行数搬到 Go 里数：日期去重是数据库的长项，
// 且 date_key 上本来就有索引。软删除由 Model(&CheckIn{}) 自动加 deleted_at 条件。
func (s *Service) totalDoneDays() (int, error) {
	var row struct {
		Days int64
	}
	if err := s.db.Model(&model.CheckIn{}).
		Select("COUNT(DISTINCT date_key) AS days").
		Where("action = ?", model.ActionDone).
		Scan(&row).Error; err != nil {
		return 0, apperr.Wrap(fmt.Errorf("统计累计完成天数失败: %w", err))
	}
	return int(row.Days), nil
}
