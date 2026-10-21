# 气泡扩窗接线：0.5 档完整显示

**Blocked by**: 003、004

> 规格：A-1、A-2、A-4、A-5、A-7、A-8、A-12、D-1；5.2 T-01/T-02/T-06/T-07

## 目标

0.5 档触发提醒时气泡完整可见；宠物原位；气泡消失恢复基础尺寸；退出重启与贴边都不留副作用。

## 涉及层

- [ ] 逻辑层（Go）
  - `app.go` 新增两个绑定（绑定方法必须写在 `app.go`，护栏只扫这里）：
    - `SetPetBubbleVisible(visible bool) error`：true 时只加宽到目标宽度、高度维持基础值；false 时恢复基础尺寸与位置。
    - `SetPetBubbleHeight(total int) error`：气泡可见时把窗口高度调到该总高度（含宠物与内边距）；小于基础高度按基础高度处理。
  - 新增 `pet_bubble.go`（或并入 `pet_window.go`）：
    - 以宠物脚底所在屏幕的 `WorkArea` 为工作区；
    - 用 004 的纯函数算扩窗矩形；
    - `SetSize` + `SetPosition` 成对应用；
    - 保存「气泡预算 = total - 当前宠物高度」，供 006 的缩放重算使用；
    - 扩窗不 focus、不改变 `AlwaysOnTop`。
  - `capturePetGeometry` 用 `petBaseRect` 写基础矩形；`petDockAtHome` / `tryDockPet` 用基础矩形比较与存储 home（否则扩窗位置变化会被误判为用户拖动）。
  - `SetPetSettings` 在扩窗中改缩放：本票先做最小安全处理——回基础尺寸，再交给前端重新测量；完整重算留给 006。
- [ ] UI 层（`frontend/src/views/Pet.vue`）
  - 新增 `bubbleRef` 与 `petArtRef`；气泡挂载/内容变化/宽度变化时在 `nextTick` 后测量。
  - 测量绝对需求：`气泡高度 + 间距 + 宠物高度 + 上下内边距`（读 DOM，不复制 CSS 常量）；相同数值不重复上报。
  - 顺序：任一气泡可见 → `SetPetBubbleVisible(true)` → 下一帧测量 → `SetPetBubbleHeight(total)`；气泡卸载/无内容 → `SetPetBubbleVisible(false)`。
  - `ResizeObserver` 监听气泡尺寸变化；宽度被夹窄时触发第二次测量。
  - 扩窗调用失败只记 `logWarn`，不阻断气泡显示。
- [ ] 契约层
  - 运行 `make gen` 更新 `frontend/bindings/`；`internal/guard/bindings_test.go` 复算 ID 通过。
- [ ] 测试
  - 手动 0.5 / 1.0 / 2.0：待处理气泡完整、无裁切；0.5 时窗口约 240 宽，宠物原位。
  - 0.5 点完成最后一条 → 窗口回 120×190；无残留透明区。
  - 宠物贴右缘触发 → 气泡夹取到工作区内，文字正常换行。
  - 气泡可见时退出 → 重启 → 宠物位置不变；`config.json` 的 `pet_x/pet_y` 对应基础矩形。
  - 收起在边缘 → 悬停展开 → 气泡完整且不立即重新吸附。

## 验收标准

- 0.5 档所有气泡内容可见（场景名、标题、正文、按钮、「打开客户端」）。
- 宠物底边坐标在扩窗前后不变（无夹取时）；消失后恢复。
- 退出/重启、贴边展开/收起无位置漂移、无误吸附。
- 生成的绑定与 `app.go` 一致；`make test`、`make lint` 通过。
- 无新增事件域、无新增依赖、无 `console.log`。
