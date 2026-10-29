package service

import "testing"

// TestGrowthStage 覆盖 growthStage 的全部边界（表驱动）。
//
// 用 target=100 构造精确的整数百分比边界（1/33/34/66/67/99/100），
// 再补两条非 100 目标与超额完成的用例，确认整数向下取整没有边界漂移。
func TestGrowthStage(t *testing.T) {
	cases := []struct {
		name   string
		done   int
		target int
		want   int
	}{
		{name: "未设目标", done: 0, target: 0, want: GrowthStageSeedling},
		{name: "目标为负", done: 5, target: -1, want: GrowthStageSeedling},
		{name: "完成为负", done: -1, target: 8, want: GrowthStageSeedling},
		{name: "完成0且目标大于0", done: 0, target: 8, want: GrowthStageSeedling},
		{name: "1%下界", done: 1, target: 100, want: GrowthStageSprout},
		{name: "33%上界", done: 33, target: 100, want: GrowthStageSprout},
		{name: "34%下界", done: 34, target: 100, want: GrowthStageHalf},
		{name: "66%上界", done: 66, target: 100, want: GrowthStageHalf},
		{name: "67%下界", done: 67, target: 100, want: GrowthStageAlmost},
		{name: "99%上界", done: 99, target: 100, want: GrowthStageAlmost},
		{name: "100%达标", done: 100, target: 100, want: GrowthStageFull},
		{name: "超过目标", done: 120, target: 100, want: GrowthStageFull},
		{name: "非100目标33%", done: 3, target: 9, want: GrowthStageSprout},
		{name: "非100目标37%", done: 3, target: 8, want: GrowthStageHalf},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := growthStage(tc.done, tc.target); got != tc.want {
				t.Errorf("growthStage(%d, %d) = %d，期望 %d", tc.done, tc.target, got, tc.want)
			}
		})
	}
}

// TestLongTermStage 覆盖「累计完成天数 → 形态档位」的全部边界。
//
// 边界值必须与 longTermStageThresholds 逐项对齐；调阈值表时这里同步改，
// 否则会出现"阈值改了但没人发现档位不对"。
func TestLongTermStage(t *testing.T) {
	cases := []struct {
		name string
		days int
		want int
	}{
		{name: "负数当作0", days: -3, want: GrowthStageSeedling},
		{name: "从未完成", days: 0, want: GrowthStageSeedling},
		{name: "首个阈值下界", days: 1, want: GrowthStageSprout},
		{name: "第二档上界", days: 6, want: GrowthStageSprout},
		{name: "第二档下界", days: 7, want: GrowthStageHalf},
		{name: "第三档上界", days: 29, want: GrowthStageHalf},
		{name: "第三档下界", days: 30, want: GrowthStageAlmost},
		{name: "第四档上界", days: 99, want: GrowthStageAlmost},
		{name: "第四档下界", days: 100, want: GrowthStageFull},
		{name: "远超上限仍封顶", days: 1000, want: GrowthStageFull},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := longTermStage(tc.days); got != tc.want {
				t.Errorf("longTermStage(%d) = %d，期望 %d", tc.days, got, tc.want)
			}
		})
	}
}

// TestLongTermStageTableMatchesStageCount 断言阈值表长度与档位刻度一致。
//
// 0 档不需要阈值（0 天就是 0 档），故表长必须恰好等于 GrowthStageFull。
//
// 这是一条"感知自己瞎了"的护栏：若有人往表里加第 5 个阈值，longTermStage 会算出 5，
// 然后被封顶逻辑**静默**压回 4——不报错、不崩溃，只是"新增的那一档永远摸不到、
// 或者本该更高的档位永远不出现"。故在这里把表长与刻度直接钉死。
func TestLongTermStageTableMatchesStageCount(t *testing.T) {
	if want := GrowthStageFull; len(longTermStageThresholds) != want {
		t.Fatalf("阈值表有 %d 项，但档位刻度是 0..%d（0 档不需要阈值，应为 %d 项）；"+
			"加减阈值请同步 GrowthStage* 常量与前端 class",
			len(longTermStageThresholds), GrowthStageFull, want)
	}
	// 越界输入也不得超过最高档（表长变了这一条会先被上面的断言拦住）。
	if got := longTermStage(1 << 20); got != GrowthStageFull {
		t.Errorf("超大天数得到 %d，期望封顶在 %d", got, GrowthStageFull)
	}
}

// TestComposePetShape 覆盖形态合成的两条规则：
//  1. 首次调用（seeded=false）用今日档播种一次，兜住升级当天；
//  2. 播种之后今日档不再参与形态，今日高只影响神态。
//
// 必须同时覆盖「今日档高于长期档」——这正是旧实现每天缩小一次的来源。
func TestComposePetShape(t *testing.T) {
	cases := []struct {
		name      string
		floor     int
		longTerm  int
		today     int
		seeded    bool
		wantStage int
		wantFloor int
	}{
		{name: "全新安装首次调用", floor: 0, longTerm: 0, today: 0, seeded: false, wantStage: 0, wantFloor: 0},
		{name: "升级当天今日更高", floor: 0, longTerm: 1, today: 4, seeded: false, wantStage: 4, wantFloor: 4},
		{name: "迁移后次日不缩回", floor: 4, longTerm: 1, today: 0, seeded: true, wantStage: 4, wantFloor: 4},
		{name: "迁移后今日再好也不抬形态", floor: 2, longTerm: 1, today: 4, seeded: true, wantStage: 2, wantFloor: 2},
		{name: "长期超过高水位", floor: 1, longTerm: 2, today: 0, seeded: true, wantStage: 2, wantFloor: 2},
		{name: "长期与高水位相同", floor: 3, longTerm: 3, today: 0, seeded: true, wantStage: 3, wantFloor: 3},
		{name: "配置越界也被夹住", floor: -5, longTerm: 9, today: -1, seeded: true, wantStage: 4, wantFloor: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stage, floor := composePetShape(tc.floor, tc.longTerm, tc.today, tc.seeded)
			if stage != tc.wantStage || floor != tc.wantFloor {
				t.Errorf("composePetShape(%d, %d, %d, %v) = (%d, %d)，期望 (%d, %d)",
					tc.floor, tc.longTerm, tc.today, tc.seeded, stage, floor, tc.wantStage, tc.wantFloor)
			}
		})
	}
}

// TestPetShapeNeverShrinks 模拟「先完美一天、随后归零、再断签」的完整时间线。
//
// 旧实现（Stage=max(长期, 今日)）第一天得 4、第二天掉回 1；
// 新实现把 4 播种成高水位，后续任何跨天都不得低于它。
func TestPetShapeNeverShrinks(t *testing.T) {
	// 每天只给 长期档 / 今日档；第 1 天之后不再传入今日档参与形态。
	days := []struct {
		longTerm int
		today    int
	}{
		{longTerm: 1, today: 4},
		{longTerm: 1, today: 0},
		{longTerm: 2, today: 0},
		{longTerm: 2, today: 4},
		{longTerm: 3, today: 0},
		{longTerm: 4, today: 0},
	}
	floor, seeded := 0, false
	prev := 0
	for i, day := range days {
		stage, nextFloor := composePetShape(floor, day.longTerm, day.today, seeded)
		if stage < prev {
			t.Fatalf("第 %d 天形态 %d 低于前一天 %d——跨天回退了", i+1, stage, prev)
		}
		if nextFloor < floor {
			t.Fatalf("第 %d 天高水位 %d 低于原值 %d——高水位只能增", i+1, nextFloor, floor)
		}
		prev, floor, seeded = stage, nextFloor, true
	}
	if prev != GrowthStageFull {
		t.Errorf("时间线结束时形态 = %d，期望保持最高档 %d", prev, GrowthStageFull)
	}
}
