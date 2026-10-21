# 003 — 手动演示模式核心

**Blocked by**: 001

**状态**: 代码完成，待手工视觉验证（2026-10-21）

## 目标

用户可从菜单栏进入/退出演示模式：提醒停止投递，桌宠与强提醒立即隐藏，菜单栏显示「演示中」；退出后恢复用户原来的宠物显隐。完成后可独立 demo。

## 涉及层

- [ ] 数据层：
  - 新建 `internal/presentation/controller.go`：off / active_manual 状态机，记录 source、since；实现 `pause.Detector`（`Detect` / `Since`），暂停原因为 `ReasonPresentation`。
  - `internal/pause/bundle.go` 或 App 组合处把 presentation detector 加入 `CompositeProvider`；确保 `restartPauseBundle` 不会丢掉活动中的演示状态。
- [ ] 逻辑层：
  - `app.go` 增加 `SetPresentationMode(active bool, source string)` 绑定（本片只使用 `manual`）；进入/退出时调用 `HideAlert`、运行期宠物显隐、`refreshTrayIfChanged`；`GetPauseState` 填充 `presentation_active` / `presentation_source`。
  - 宠物显隐改为「有效可见 = `cfg.PetVisible && !presentationActive`」；演示模式不调用 `setPetVisible`、不写配置、不改贴边位置。
  - 演示状态不落盘；重启后 off。
- [ ] UI 层：
  - `internal/tray/tray.go` 增加 `OnTogglePresentation` 与动态菜单项「进入/退出演示模式」。
  - 菜单栏状态行显示「免打扰：演示中」（经 pause reason）。
  - `frontend/src/views/Alert.vue` 在 presentation active 时不调用 `ShowAlert`；进入时 App 已隐藏。
  - `Settings.vue` 新建「不被打断」区域骨架（说明 + 当前状态；具体控件后续切片加入）。
- [ ] 测试：
  - `internal/presentation/controller_test.go`：进入/退出、幂等、source、since。
  - `internal/pause` 组合优先级测试：presentation 高于 lock / meeting / fullscreen。
  - 引擎使用 fake provider：演示期间周期项不投递、每日/一次性记 missed；退出后不补发。
  - 手工：托盘进入/退出、宠物隐藏/恢复、宠物原为隐藏时仍隐藏、配置未被改写。
  - `make test`、`make lint`。

## 验收标准

- 菜单栏可进入/退出；重复操作幂等；状态行显示「演示中」。
- 演示期间无系统通知、无强提醒、无新待处理；周期项按暂停语义顺延不补发。
- 桌宠/气泡隐藏；退出后恢复用户原显隐；`config.json` 的 `pet_visible` 不变。
- 已有强提醒在进入时立即收起。
- 演示状态不持久化；重启后关闭。
- 不读取屏幕内容/窗口标题，不调用摄像头/麦克风。
- `make test`、`make lint` 通过；覆盖 A-1、A-2、A-3、A-4、A-11、A-12、A-13（部分）。

## 实现记录（2026-10-21）

- 新增 `internal/presentation.Controller`（off / active_manual 状态机，记录 source、since、exitedAt）与 `pause.PresentationDetector`；`pause.NewBundle` 始终装配该信号，`CompositeProvider` 中优先级最高。
- `app.go` 新增 `SetPresentationMode(active, source)` 绑定与 `isPresentationActive`；进入时隐藏强提醒、同步 pause bundle、按运行期隐藏宠物；退出时恢复用户显隐偏好；演示状态不写配置、不持久化。
- 托盘新增「进入/退出演示模式」菜单项与强制刷新；`GetPauseState` 合并 `presentation_active/source/exited_at`；`Alert.vue` 在演示期间不显示；`main.go` 的强提醒兜底也拒绝推窗。
- `Settings.vue` 新增「不被打断」区域骨架（无控件）；`Pet.vue` 补充「演示中」文案。
- 新增测试：`internal/presentation` 状态机、`internal/pause` 检测器与优先级、`app_presentation_test.go` 的 pause 同步/退出时间/非法来源。
- 验证：`make test`、`make lint` 通过。
- 待办：在真实 macOS/Windows GUI 上验证托盘文案、宠物隐藏/恢复、强提醒不出现（本环境未做 GUI 自动化）；休息模式 skip 与退出不补弹由 004 完成。
