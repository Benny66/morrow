package main

// 桌面宠物的贴边收起 / 悬停展开。
//
// 交互形态（对齐 QQ 宠物那类桌面挂件）：
//   - 把宠物拖到屏幕边缘附近松手 → 窗口移到屏幕边缘外，只留一条 20px 的窄边可见；
//   - 鼠标划过那条窄边 → 窗口还原到拖动前的位置；
//   - 鼠标移开一段时间 → 再次收起。
//
// 【为什么是"真的把窗口移出屏幕"而不是用 CSS 假装藏起来】
// 透明窗口的矩形范围内（240×360）即使像素全透明，依然会吃掉鼠标事件。
// 若只把宠物视觉上挪出去，那块区域就变成一片"看不见但点不到"的死区，比不收起更烦。
// 移出屏幕后只占 20px，代价可以接受。
//
// 【为什么用轮询而不是 WindowDidMove 事件】
// v3 的 events.Common.WindowDidMove 确实存在，但各平台后端是否稳定 emit 无法离线确认；
// 而 Position() 是确定的。500ms 一轮、只在位置变化时才做判断，开销可以忽略。
// 判据是"连续两次采样位置相同"，等价于"拖动已经停下"。

import (
	"context"
	"log/slog"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"morrow/internal/event"
)

const (
	// petDockPeekPx 是收起时露在屏幕内的宽度（逻辑像素）。
	//
	// 与前端 lib/pet.ts 的 PET_DOCK_PEEK_PX 必须一致：后端按它把窗口移出去，
	// 前端按它把宠物平移到那条窄边里。两边不一致的症状是"只露出半张脸"或"露出空白"。
	petDockPeekPx = 20

	// petDockThresholdPx 是"离边缘多近算想贴上"的阈值。
	//
	// 太大会让宠物在屏幕中间就被吸走（用户只是路过那边），太小则几乎对不准。
	// 24px 大约是一次轻微抖动的手误幅度。
	petDockThresholdPx = 24

	// petDockPollInterval 是位置采样间隔。
	petDockPollInterval = 500 * time.Millisecond

	// petDockExpandGrace 是展开后忽略贴边检测的时长。
	//
	// 取 3 秒：足够覆盖"鼠标划过边缘顺手瞄一眼"的时长。用户真想把宠物拖走时，
	// 拖动本身会改变位置，3 秒后新一轮采样自然会重新判断，不会被这个忽略期耽误。
	petDockExpandGrace = 3 * time.Second
)

// petDockState 是贴边状态。
//
// 【side 与 collapsed 必须分开】side 表示"贴在哪个边"，collapsed 表示"当前是不是
// 收着"。只用 side 一个字段的话，展开之后就没法知道"松手时该往哪个边收回去"了。
type petDockState struct {
	// side 贴边方向：left / right / top / bottom；空串表示未贴边。
	side string
	// collapsed 当前是否处于收起状态（窗口在屏幕外）。
	collapsed bool
	// homeX / homeY 收起前的窗口位置，展开时还原到这里。
	homeX int
	homeY int
	// dockX / dockY 收起时的窗口位置，再次收起时直接用。
	dockX int
	dockY int
}

// PetDockPayload 是 pet:dock 事件的载荷。
// 前端镜像见 frontend/src/lib/pet.ts 的 PetDockedPayload（字段名需一致）。
type PetDockPayload struct {
	// Side 贴在哪个边；空串表示已展开。
	//
	// 【为什么要发这个事件】窗口是后端移的，但"宠物该不该平移到那条窄边里"
	// 是纯视觉问题，只有前端能决定。不发事件，用户看到的就是宠物凭空消失。
	Side string `json:"side"`
}

// startPetDockWatcher 启动位置轮询（重复调用安全）。
func (a *App) startPetDockWatcher() {
	a.petDockMu.Lock()
	if a.petDockCancel != nil {
		a.petDockMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.petDockCancel = cancel
	a.petDockMu.Unlock()

	go a.watchPetPosition(ctx)
	slog.Info("宠物贴边监测已启动", "interval", petDockPollInterval)
}

// stopPetDockWatcher 停止位置轮询。
func (a *App) stopPetDockWatcher() {
	a.petDockMu.Lock()
	cancel := a.petDockCancel
	a.petDockCancel = nil
	a.petDockMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// watchPetPosition 采样窗口位置，位置稳定后判断是否贴边。
func (a *App) watchPetPosition(ctx context.Context) {
	defer func() {
		// 长驻 goroutine 的 panic 必须兜住：挂了不会有任何界面提示，只是贴边功能默默失效。
		if r := recover(); r != nil {
			slog.Error("宠物贴边监测异常退出", "panic", r)
		}
	}()

	ticker := time.NewTicker(petDockPollInterval)
	defer ticker.Stop()

	var prevX, prevY int
	var hasPrev, stable bool

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 收起态下窗口在屏幕外，位置是"我们自己设的"，不必再判断。
			if a.petDockCollapsed() {
				continue
			}
			// 刚展开：窗口回到的位置仍贴着边（那正是当初触发吸附的地方），
			// 不跳过这一轮就会立刻被重新吸附——表现为"划过闪一下又藏回去"。
			if time.Now().UnixMilli() < a.petDockIgnoreUntil.Load() {
				continue
			}
			w := a.petWindow()
			if w == nil {
				continue
			}
			x, y := w.Position()
			width, height := w.Size()

			// 展开后一直停在展开位置 → 说明没有新的一次拖动，不要重复吸附。
			// 这条与 ignoreUntil 是两道独立的防线：前者管"停在原地"，后者管"刚展开的瞬间"。
			if a.petDockAtHome(x, y) {
				continue
			}

			if !hasPrev {
				prevX, prevY, hasPrev = x, y, true
				continue
			}
			if x != prevX || y != prevY {
				// 还在动：更新基准，等它停下。
				prevX, prevY, stable = x, y, false
				continue
			}
			if stable {
				continue
			}
			// 连续两次采样位置相同 → 判定拖动结束。
			stable = true
			a.tryDockPet(x, y, width, height)
		}
	}
}

// tryDockPet 判断当前位置是否贴近屏幕边缘，是则收起。
//
// 也负责"用户把宠物从边缘拖走了"这个反向情况：此时清掉贴边状态，
// 否则下次鼠标划过会莫名其妙地把它收回一个早已不相关的边。
func (a *App) tryDockPet(x, y, width, height int) {
	side, dockX, dockY := a.petDockTarget(x, y, width, height)
	if side == "" {
		a.clearPetDock()
		return
	}

	a.petMu.Lock()
	defer a.petMu.Unlock()
	w := a.petWindow()
	if w == nil {
		return
	}

	a.petDockMu.Lock()
	a.petDock = petDockState{
		side:      side,
		collapsed: true,
		homeX:     x,
		homeY:     y,
		dockX:     dockX,
		dockY:     dockY,
	}
	a.petDockMu.Unlock()

	w.SetPosition(dockX, dockY)
	slog.Info("宠物已贴边收起", "side", side, "home_x", x, "home_y", y)
	a.emitPetDock(side)
}

// petDockTarget 计算贴边方向与收起位置；不贴边时返回空串。
//
// 用 WorkArea 而不是 Bounds：后者包含菜单栏与 Dock 所在的区域，
// 按它贴边会让宠物藏到 Dock 底下去。
func (a *App) petDockTarget(x, y, width, height int) (string, int, int) {
	if a.app == nil {
		return "", 0, 0
	}
	for _, screen := range a.app.Screen.GetAll() {
		wa := screen.WorkArea
		// 只用左上角判定归属哪块屏：拖动中的窗口跟着鼠标，
		// 左上角足够明确，也能避免"跨两块屏时被算两次"。
		if x < wa.X || x >= wa.X+wa.Width || y < wa.Y || y >= wa.Y+wa.Height {
			continue
		}
		switch {
		case x-wa.X <= petDockThresholdPx:
			return "left", wa.X - width + petDockPeekPx, y
		case wa.X+wa.Width-(x+width) <= petDockThresholdPx:
			return "right", wa.X + wa.Width - petDockPeekPx, y
		case y-wa.Y <= petDockThresholdPx:
			return "top", x, wa.Y - height + petDockPeekPx
		case wa.Y+wa.Height-(y+height) <= petDockThresholdPx:
			return "bottom", x, wa.Y + wa.Height - petDockPeekPx
		default:
			return "", 0, 0
		}
	}
	return "", 0, 0
}

// clearPetDock 清掉贴边状态（宠物已被拖离边缘）。
func (a *App) clearPetDock() {
	a.petDockMu.Lock()
	had := a.petDock.side != ""
	a.petDock = petDockState{}
	a.petDockMu.Unlock()
	if had {
		a.emitPetDock("")
	}
}

// petDockCollapsed 报告当前是否处于收起态。
func (a *App) petDockCollapsed() bool {
	a.petDockMu.Lock()
	defer a.petDockMu.Unlock()
	return a.petDock.collapsed
}

// petDockAtHome 报告窗口是否正停在"展开位置"（说明用户没有开始新的拖动）。
//
// 只对贴过边的宠物成立：从未贴边的宠物没有 home 可言，这里返回 false 让它照常检测。
func (a *App) petDockAtHome(x, y int) bool {
	a.petDockMu.Lock()
	defer a.petDockMu.Unlock()
	return a.petDock.side != "" &&
		!a.petDock.collapsed &&
		a.petDock.homeX == x &&
		a.petDock.homeY == y
}

// emitPetDock 广播贴边状态。
func (a *App) emitPetDock(side string) {
	if a.app == nil {
		return
	}
	a.app.Event.Emit(event.Name(petDomain, event.ActionDock), PetDockPayload{Side: side})
}

// petDomain 是宠物事件域（与前端 lib/pet.ts 的 PET_DOMAIN 一致）。
const petDomain = "pet"

// restorePetFromDockLocked 把收起的宠物还原到展开位置，返回是否做了还原。
//
// 【调用方必须已持有 petMu】两个调用点（设置页保存、显隐切换）都在 petMu 的
// 临界区里，而这里要改窗口位置；本函数自己去拿 petMu 会死锁。
//
// 【为什么需要它】收起时窗口在屏幕外。用户从设置页把宠物关掉再打开，
// Show() 出来的仍是屏幕外那块——用户看到的是「宠物没了」，而且没有任何入口能找回来。
func (a *App) restorePetFromDockLocked(w application.Window) bool {
	if !a.petDockCollapsed() {
		return false
	}

	a.petDockMu.Lock()
	x, y := a.petDock.homeX, a.petDock.homeY
	side := a.petDock.side
	a.petDock.collapsed = false
	a.petDockMu.Unlock()

	w.SetPosition(x, y)
	a.petDockIgnoreUntil.Store(time.Now().Add(petDockExpandGrace).UnixMilli())
	if side != "" {
		a.emitPetDock("")
	}
	return true
}
