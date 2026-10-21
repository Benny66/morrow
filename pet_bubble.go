package main

// 宠物窗口的「气泡形态」：气泡可见时临时扩窗，消失即还原。
//
// 几何规则在 pet_bubble_geometry.go（纯函数）；本文件负责把它接到真实窗口上：
// 选工作区、读写窗口矩形、维护扩窗状态。
//
// 【加锁约定】所有 expand/restore 函数都要求调用方已持有 petMu（见 app.go 的
// petMu 注释）。它们会读 config、读写窗口，不自己去拿锁——反向获取会死锁。
//
// 【为什么扩窗不写配置】气泡是临时形态：用户退出时可能正挂着一条提醒，若把
// 扩出来的矩形存进 pet_x/pet_y，下次启动宠物就会凭空下移一段。持久化统一用
// 基础几何（petBaseRect 反算）。

import "github.com/wailsapp/wails/v3/pkg/application"

// petWindowRect 返回窗口当前的逻辑矩形。
func petWindowRect(w application.Window) petRect {
	x, y := w.Position()
	width, height := w.Size()
	return petRect{X: x, Y: y, W: width, H: height}
}

// petCurrentBaseLocked 返回当前窗口对应的基础矩形。
//
// 扩窗中调用也成立：底边与横向中心不动，故能从扩出来的矩形反算基础矩形。
func (a *App) petCurrentBaseLocked(w application.Window) petRect {
	baseW, baseH := petWindowSize(a.cfg.PetScaleValue())
	return petBaseRect(petWindowRect(w), baseW, baseH)
}

// applyPetWindowRect 把窗口调整到目标矩形；完全相同时跳过，避免无谓的 resize。
func applyPetWindowRect(w application.Window, r petRect) {
	cur := petWindowRect(w)
	if cur == r {
		return
	}
	w.SetSize(r.W, r.H)
	w.SetPosition(r.X, r.Y)
}

// petWorkArea 返回宠物脚底所在屏幕的工作区。
//
// 用脚底（底边中点）判定归属，而不是窗口左上角：扩窗只向上长，脚底才是用户
// 真正放置宠物的位置；跨屏摆放时也不会因为窗口变大而被算到另一块屏。
// 找不到时回落主屏，再不行用与强提醒相同的兜底尺寸——错位的窗口比不显示好。
func (a *App) petWorkArea(base petRect) petRect {
	fallback := petRect{X: 0, Y: 0, W: alertFallbackWidth, H: alertFallbackHeight}
	if a.app == nil {
		return fallback
	}
	feetX := base.X + base.W/2
	feetY := base.Y + base.H
	for _, screen := range a.app.Screen.GetAll() {
		if screen == nil {
			continue
		}
		wa := screen.WorkArea
		if wa.Width <= 0 || wa.Height <= 0 {
			continue
		}
		if feetX >= wa.X && feetX < wa.X+wa.Width && feetY >= wa.Y && feetY <= wa.Y+wa.Height {
			return petRect{X: wa.X, Y: wa.Y, W: wa.Width, H: wa.Height}
		}
	}
	if primary := a.app.Screen.GetPrimary(); primary != nil {
		wa := primary.WorkArea
		if wa.Width > 0 && wa.Height > 0 {
			return petRect{X: wa.X, Y: wa.Y, W: wa.Width, H: wa.Height}
		}
	}
	return fallback
}

// expandPetBubbleLocked 把窗口切到气泡形态，或更新气泡形态的高度。
//
// total 是前端实测的窗口总高度（宠物 + 间距 + 内边距 + 气泡）；0 表示只加宽、
// 高度维持已经记录的气泡预算。总高小于基础高度时按基础高度处理。
func (a *App) expandPetBubbleLocked(w application.Window, total int) {
	base := a.petCurrentBaseLocked(w)

	budget := a.petBubbleBudget
	if total > 0 {
		budget = total - base.H
		if budget < 0 {
			budget = 0
		}
	}
	if budget < 0 {
		budget = 0
	}

	desiredW := base.W
	if desiredW < petBubbleWindowWidth {
		desiredW = petBubbleWindowWidth
	}
	desiredH := base.H + budget
	rect := petBubbleRect(base, desiredW, desiredH, a.petWorkArea(base))
	applyPetWindowRect(w, rect)

	a.petBubbleActive = true
	a.petBubbleBudget = budget
}

// petBubbleExpanded 报告窗口当前是否处于气泡形态（供贴边监测跳过程序化移动）。
func (a *App) petBubbleExpanded() bool {
	a.petMu.Lock()
	defer a.petMu.Unlock()
	return a.petBubbleActive
}

// restorePetBubbleLocked 把窗口还原到基础矩形并清掉扩窗状态。
func (a *App) restorePetBubbleLocked(w application.Window) {
	if !a.petBubbleActive {
		return
	}
	base := a.petCurrentBaseLocked(w)
	applyPetWindowRect(w, base)
	a.petBubbleActive = false
	a.petBubbleBudget = 0
}

// restorePetBubbleAtLocked 把窗口还原到调用方算好的基础矩形（贴边路径用）。
//
// 与 restorePetBubbleLocked 的差别只在「基础矩形从哪来」：贴边时窗口即将移出屏幕，
// 必须用移动前的矩形反算，不能等窗口移走再读。
func (a *App) restorePetBubbleAtLocked(w application.Window, base petRect) {
	if !a.petBubbleActive {
		return
	}
	applyPetWindowRect(w, base)
	a.petBubbleActive = false
	a.petBubbleBudget = 0
}
