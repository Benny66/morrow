# 气泡扩窗几何纯函数（预重构）

**Blocked by**: 无

> 规格：A-2、A-3、A-10、A-11（几何部分）、5.1 接缝 1

## 目标

把扩窗的几何规则收敛为可单测的纯函数，为 005 提供稳定靶心；本票不接线、不改任何窗口。

## 涉及层

- [ ] 逻辑层（Go，新文件 `pet_bubble_geometry.go`）
  - 本地矩形类型（不依赖 `application`，可在普通测试里跑）。
  - `petBubbleRect(base, desiredW, desiredH, work) rect`：
    - 底边锚定：扩窗后底边 = base 底边；
    - 横向以 base 中心对称；
    - 夹取到 work（先夹高度，再夹宽度）；
    - 顶部不足时 `y = work.Y`，允许底边临时下移。
  - `petBaseRect(cur, baseW, baseH) rect`：从当前（可能已扩大的）矩形反算基础矩形。
  - 常量：气泡目标宽度 240（Go 侧唯一出处）。
- [ ] 测试（`pet_bubble_geometry_test.go`）
  - 0.5 / 1.0 / 2.0 三档基础尺寸；
  - 贴右缘夹取、贴上缘钉住、副屏工作区；
  - 期望宽高小于基础值 → 不缩；
  - 重复调用幂等；
  - `petBaseRect` 反算后再扩窗与直接扩窗一致（往返一致）。
- [ ] 说明
  - 纯函数不读屏幕、不看窗口；工作区矩形由 005 注入。

## 验收标准

- 单测覆盖上述用例，`go test ./...` 通过。
- 函数不 import wails application / config，可直接在普通测试里跑。
- 中文注释写清锚定与夹取规则；全仓只有一个 240 的气泡宽度常量。
