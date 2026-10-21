package main

import "testing"

// TestPetBubbleRectAnchorsBottom 锁定扩窗的第一条规则：底边不动、横向以宠物中心对称。
//
// 数值来自 0.5 档的真实尺寸：基础窗口 120×190，气泡需要 240×276。
// 基础窗口底边在 y=790，扩窗后仍应是 790；中心 x=1060 左右各扩 60。
func TestPetBubbleRectAnchorsBottom(t *testing.T) {
	base := petRect{X: 1000, Y: 600, W: 120, H: 190}
	work := petRect{X: 0, Y: 0, W: 1440, H: 900}

	got := petBubbleRect(base, 240, 276, work)
	want := petRect{X: 940, Y: 514, W: 240, H: 276}
	if got != want {
		t.Fatalf("petBubbleRect = %+v, want %+v", got, want)
	}
}

// TestPetBubbleRectClampsRightEdge 锁定横向夹取：宠物贴着屏幕右缘时，
// 扩到 240 宽会越界，窗口应整体向内收，而不是把气泡伸出工作区。
func TestPetBubbleRectClampsRightEdge(t *testing.T) {
	base := petRect{X: 1300, Y: 600, W: 120, H: 190}
	work := petRect{X: 0, Y: 0, W: 1440, H: 900}

	got := petBubbleRect(base, 240, 276, work)
	want := petRect{X: 1200, Y: 514, W: 240, H: 276}
	if got != want {
		t.Fatalf("petBubbleRect = %+v, want %+v", got, want)
	}
}

// TestPetBubbleRectPinsToWorkAreaTop 锁定顶部空间不足时的行为：
// 窗口钉住工作区顶边，底边随之下移（宠物临时下移），而不是把气泡顶出屏幕。
func TestPetBubbleRectPinsToWorkAreaTop(t *testing.T) {
	base := petRect{X: 1000, Y: 50, W: 120, H: 190}
	work := petRect{X: 0, Y: 0, W: 1440, H: 900}

	got := petBubbleRect(base, 240, 276, work)
	want := petRect{X: 940, Y: 0, W: 240, H: 276}
	if got != want {
		t.Fatalf("petBubbleRect = %+v, want %+v", got, want)
	}
}

// TestPetBubbleRectNeverShrinksBelowBase 锁定下限：气泡需求小于基础尺寸时，
// 窗口保持基础矩形，不能把宠物裁掉。
func TestPetBubbleRectNeverShrinksBelowBase(t *testing.T) {
	base := petRect{X: 1000, Y: 600, W: 120, H: 190}
	work := petRect{X: 0, Y: 0, W: 1440, H: 900}

	got := petBubbleRect(base, 100, 100, work)
	if got != base {
		t.Fatalf("petBubbleRect = %+v, want base %+v", got, base)
	}
}

// TestPetBubbleRectClampsHeightToWorkArea 锁定高度上限：
// 气泡再长，窗口也不能高出工作区；超出的部分由前端按可用高度降级。
func TestPetBubbleRectClampsHeightToWorkArea(t *testing.T) {
	base := petRect{X: 1000, Y: 600, W: 120, H: 190}
	work := petRect{X: 0, Y: 0, W: 1440, H: 900}

	got := petBubbleRect(base, 240, 1000, work)
	want := petRect{X: 940, Y: 0, W: 240, H: 900}
	if got != want {
		t.Fatalf("petBubbleRect = %+v, want %+v", got, want)
	}
}

// TestPetBubbleRectClampsWidthToWorkArea 锁定宽度上限：需求宽度超过工作区时按工作区夹取。
func TestPetBubbleRectClampsWidthToWorkArea(t *testing.T) {
	base := petRect{X: 1000, Y: 600, W: 120, H: 190}
	work := petRect{X: 0, Y: 0, W: 1440, H: 900}

	got := petBubbleRect(base, 2000, 276, work)
	want := petRect{X: 0, Y: 514, W: 1440, H: 276}
	if got != want {
		t.Fatalf("petBubbleRect = %+v, want %+v", got, want)
	}
}

// TestPetBubbleRectRespectsSecondaryScreenOrigin 锁定副屏：工作区原点不为 0 时，
// 夹取必须用这块屏的边界，而不是主屏的 (0,0)。
func TestPetBubbleRectRespectsSecondaryScreenOrigin(t *testing.T) {
	base := petRect{X: 1530, Y: 600, W: 120, H: 190}
	work := petRect{X: 1512, Y: 0, W: 1440, H: 900}

	got := petBubbleRect(base, 240, 276, work)
	want := petRect{X: 1512, Y: 514, W: 240, H: 276}
	if got != want {
		t.Fatalf("petBubbleRect = %+v, want %+v", got, want)
	}
}

// TestPetBaseRectReversesExpansion 锁定基础矩形反算：从扩窗后的矩形
// 能还原出扩窗前的底边锚定矩形（退出持久化与贴边比较都依赖它）。
func TestPetBaseRectReversesExpansion(t *testing.T) {
	cur := petRect{X: 940, Y: 514, W: 240, H: 276}

	got := petBaseRect(cur, 120, 190)
	want := petRect{X: 1000, Y: 600, W: 120, H: 190}
	if got != want {
		t.Fatalf("petBaseRect = %+v, want %+v", got, want)
	}
}

// TestPetBubbleRectIsIdempotent 锁定重复调用：前端每次可见性变化都会重报一次，
// 以扩窗后的矩形再算一遍必须原地不动，否则会每帧继续长高。
func TestPetBubbleRectIsIdempotent(t *testing.T) {
	base := petRect{X: 1000, Y: 600, W: 120, H: 190}
	work := petRect{X: 0, Y: 0, W: 1440, H: 900}

	first := petBubbleRect(base, 240, 276, work)
	again := petBubbleRect(first, 240, 276, work)
	if again != first {
		t.Fatalf("第二次扩窗 = %+v, want %+v", again, first)
	}
}
