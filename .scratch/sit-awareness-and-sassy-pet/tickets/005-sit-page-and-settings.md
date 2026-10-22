# 005 — 久坐页与基础设置

**Blocked by**: 003、004

**状态**: 已完成（2026-10-22；真实 GUI 页面/设置验收）

> 规格：`docs/specs/2026-10-22-sit-awareness-and-sassy-pet.md` 的 A-4、A-5、A-6、E-4、E-5（时间数据）、E-6（基础状态）、E-7（久坐数据）。
> 本票只做基础模式；应用画像、日期级应用明细与导出边界留给 006/007。

## 目标

用户能在侧栏「久坐」页看到今天的连续用机、久坐总时长、最长连续、真实休息、抵扣次数与站立/走动目标进度，并看到最近 7 天趋势；设置里可以开关总开关、调整 45/60/90 阈值与三档语气，清空久坐数据。全部数据可解释、可核对。

## 涉及层

- [ ] 数据层：`internal/service/sit.go`
  - `GetSitDay(dateKey)`：指定日期的 `active_seconds` 总时长、最长会话、休息次数（active/lock/idle）、抵扣次数、当前会话；复用 `GetTodayStats` 的 stand/move 目标进度（或返回所需字段，由前端合并）。
  - `GetSitTrend(days)`：最近 N 天（首版 7）按日聚合；时间数据为空时返回空数组而不是伪造 0。
  - `ClearSitData()`：删除 `sit_sessions` / `sit_breaks` / `sit_speeches` 与当前运行态；**不删除 CheckIn、不改连续天数**；幂等。
  - 日期校验：只接受 `YYYY-MM-DD`；未来日期返回空数据。
- [ ] 逻辑层：`app.go` / `sit_runtime.go`
  - 绑定：`GetSitDay`、`GetSitTrend`、`ClearSitData`；`GetSitSettings` / `SetSitSettings` 已有，设置页消费。
  - 页面刷新不需要新事件：挂载、`reminder:changed`、可见期间每 30 秒刷新；当前快照由 `GetSitSnapshot` 取。
  - 清空后同步重置 tracker 的当前会话与待展示气泡。
- [ ] UI 层：
  - `frontend/src/router/index.ts` 新增 `/sit` 路由；`AppShell.vue` 侧栏在「计划」后加「久坐」。
  - `frontend/src/views/Sit.vue`：
    - 今日区：当前连续用机（实时）、今日久坐总时长、最长连续、真实休息次数（主动/锁屏/空闲）、已抵扣 stand/move 次数、站立/走动目标进度（复用首页统计数据）、基础模式说明。
    - 最近 7 天：有标签的 CSS 条形/列表（久坐分钟、最长连续、主动起身），不引入图表库。
    - 空状态：无启用场景 / 场景无 stand/move / 今天无数据 / 平台不支持；每种都有下一步文案。
    - 所有数字必须有中文标签与口径说明；不展示应用名（画像尚未开启）。
    - 清空按钮二次确认；成功后刷新并提示「已清空久坐数据，打卡记录保留」。
  - `frontend/src/views/Settings.vue` 新增「久坐感知」区域：总开关、阈值 45/60/90、语气毒舌/活泼/温和、清空久坐数据、平台能力只读状态、隐私说明；应用画像开关暂不出现（006 加）。
  - 使用现有 tokens/Element Plus；不新增依赖；不硬编码 hex。
- [ ] 测试
  - service：GetSitDay/Trend 的日期边界、空数据、active_seconds 聚合、清空保留 CheckIn、幂等。
  - 设置：总开关关闭后不冒泡；阈值/语气保存后 003 的行为跟随；非法值夹取。
  - UI：空状态、清空确认、7 天条形标签、无障碍/减弱动态；headless/真实 GUI。
  - `make test` / `make lint`。
- [ ] 文档：`docs/map.md` 增加 `/sit` 页面与 `service/sit.go` 导航；设置页说明更新。

## 验收标准

- 侧栏「久坐」页可以打开；今日与 7 天数据与数据库一致；每个数字有标签与口径说明。
- 目标进度复用首页统计，不重复实现一套达标计算。
- 设置总开关能立即停止/恢复软提醒；阈值与语气保存后生效；清空后页面为空但首页完成数与连续天数不变。
- 无场景/无 stand/move/平台不支持/无数据都有明确状态，不显示成「0 分钟」。
- 不出现应用名；不新增事件域；不新增前端依赖。
- `make test` / `make lint` 通过；真实 GUI 走查记录。

## 实现记录（2026-10-22）

- Service：`GetSitDay`（已收尾会话的分钟/最长/主动/锁屏/空闲休息/抵扣）、`GetSitTrend`（含今天最近 N 天）、`ClearSitData`（清空 sit_sessions/sit_breaks/sit_speeches，保留 CheckIn）；真实 SQLite 测试覆盖聚合、非法日期、清空保留打卡。
- 绑定：`GetSitDay` / `GetSitTrend` / `ClearSitData`；`wails3 generate bindings` 已重新生成。`ClearSitData` 经 `sitRuntime.clearData` 重置内存 Tracker（新增 `Tracker.Reset` 与测试）。
- 前端：新 `/sit` 页（当前连续/今日总时长/最长/休息/抵扣/目标进度、最近 7 天 CSS 条形、基础模式说明、空状态、清空按钮）；路由与侧栏「久坐」；设置页新增「久坐感知」区（总开关/阈值/语气/平台能力/打开久坐页/清空/保存），保存保留 profile_* 字段。
- **事件通道修复（跨页）**：实测发现 `lib/event` 用的 npm `@wailsio/runtime.Events` 与 Wails 注入的 `/wails/runtime.js` 是两份实例、两张事件表，Go 的 `reminder:changed` 静默不达；`onEvent` 改为动态 import 注入运行时。修复后 `/sit` 在 `reminder:changed` 时从 20 分钟刷新到 30 分钟，现有 Home/Pet/Plan 的事件订阅一并恢复。
- GUI（2026-10-22）：临时 app + 临时 HOME（预置 onboarding_completed=true）；`/sit` 空状态「还没有站立/走动计划」；创建并启用 stand 计划后显示指标/目标/7 天；插入已收尾会话后页面显示 10 分钟趋势/休息/抵扣；设置区显示总开关/阈值/语气/平台，点击「保存久坐设置」出现「已保存并生效」；验收后清理临时环境并重新打开 `/Applications/Morrow.app`。
- 未覆盖：清空按钮的原生 Confirm 只由 service 测试覆盖（未在 GUI 点确认）；按日应用明细/日期选择/导出边界留 007；应用画像开关留 006；B-9 崩溃收尾留后续。
