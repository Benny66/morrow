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
