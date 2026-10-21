package main

// 宠物气泡扩窗的几何规则（纯函数，不读屏幕、不看窗口）。
//
// 窗口的锚定方式：底边不动、横向以基础窗口中心对称。这样气泡出现时宠物本体
// 停在原地，只有窗口的上方与两侧变大；气泡消失后按同一规则缩回。
// 工作区夹取、顶部空间不足的处理在后续规则里逐步加入。

// petRect 是逻辑像素的窗口矩形。
//
// 刻意用本地类型而不是 application.Rect：本文件的函数要能在普通 go test 里
// 直接跑，不依赖 Wails 运行时。
type petRect struct {
	X, Y, W, H int
}

// petBubbleWindowWidth 是气泡可见时的目标窗口宽度（逻辑像素）。
//
// 240 与基础尺寸（scale = 1.0 时的 240）同值，但语义不同：基础宽度是宠物画布
// 加内边距，这里是为了让 13px 正文每行约 15 个汉字。Go 侧唯一出处，前端不复制。
const petBubbleWindowWidth = 240

// petBubbleRect 返回气泡可见时窗口应占的矩形。
//
// base 是基础几何（宠物窗口没有气泡时的矩形）；desiredW/H 是【窗口】需要的
// 总宽高（宠物 + 间距 + 内边距 + 气泡），不是气泡自身的尺寸；work 是宠物脚底
// 所在屏幕的工作区。
func petBubbleRect(base petRect, desiredW, desiredH int, work petRect) petRect {
	w, h := desiredW, desiredH
	// 下限：气泡需求再小，也不能把窗口缩到裁掉宠物本体。
	if w < base.W {
		w = base.W
	}
	if h < base.H {
		h = base.H
	}
	// 上限：需求再大也不能超出工作区；超出部分由前端按可用高度降级。
	if w > work.W {
		w = work.W
	}
	if h > work.H {
		h = work.H
	}

	centerX := base.X + base.W/2
	x := centerX - w/2
	// 横向夹取：宠物贴着屏幕边缘时窗口整体向内收，气泡不伸出工作区。
	if x < work.X {
		x = work.X
	}
	if x+w > work.X+work.W {
		x = work.X + work.W - w
	}

	y := base.Y + base.H - h
	// 顶部空间不足：钉住工作区顶边，底边随之下移（宠物临时下移）。
	if y < work.Y {
		y = work.Y
	}
	return petRect{X: x, Y: y, W: w, H: h}
}

// petBaseRect 从当前矩形反算基础矩形（底边不动、横向中心不动）。
//
// 用途：气泡可见时退出应用，持久化的必须是基础几何；贴边吸附与「是否停在
// 展开位置」的判断也按基础矩形比较，否则扩窗造成的位置变化会被误判为用户拖动。
func petBaseRect(cur petRect, baseW, baseH int) petRect {
	x := cur.X + (cur.W-baseW)/2
	y := cur.Y + cur.H - baseH
	return petRect{X: x, Y: y, W: baseW, H: baseH}
}
