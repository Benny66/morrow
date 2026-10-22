# 003 — 基础久坐时钟、宠物情绪与软提醒

**Blocked by**: 002

**状态**: 已完成（2026-10-22；60/90 长跑用假时钟，真实锁屏转换留 004）

> 规格：`docs/specs/2026-10-22-sit-awareness-and-sassy-pet.md` 的 B-1–B-5、B-7、B-8、B-11–B-13、C-1–C-4、C-10–C-11、D-1–D-3、D-9、D-10、A-2、A-6。
> 这是用户可见的第一颗示踪弹：不做应用画像、不做目标抵扣、不改正式提醒。

## 目标

用户在启用了一个含 stand/move 提醒的场景后，连续用机到阈值时宠物会表现担心/耷拉并冒一条软提醒；用户可以「起来走走」「再坐 10 分钟」「更多」。所有动作只影响久坐状态，不写 CheckIn、不进待处理、不发系统通知。

## 涉及层

- [ ] 数据层：`internal/service/sit.go`
  - 基础 CRUD：创建/收尾 `SitSession`、写入 `SitBreak`（active / lock / idle）、创建/确认/过期 `SitSpeech`。
  - 今日久坐总时长、最长连续、真实休息次数、当前会话；按 `active_seconds` 计算，不按墙上时钟。
  - `GetSitSettings` / `SetSitSettings` 基本字段（master / threshold / tone）；不接应用画像。
  - 无 CheckIn 写入（抵扣在 004）。

- [ ] 逻辑层：`internal/sit`
  - `tracker.go`：全局久坐时钟；切换应用不重置；分级空闲 0–5 继续 / 5–15 暂停不清零 / ≥15 清零；锁屏/休眠按真实休息清零；主动休息进入 `break_wait`，检测输入恢复后记录休息时长并欢迎回来。
  - `policy.go`：60 分钟（可设 45/90）且 15 分钟内无 stand/move 时生成软提醒；忽略后 90 分钟（或首展后 30 分钟，取较晚者）第二次；每会话最多 2 次；每天最多 5 次主动发言；两次主动发言至少 30 分钟；暂停（会议/全屏/系统专注/锁屏）、静默、待处理、休息倒计时、输入中不开口；Ack 后才计预算。应用级专注会话的克制在 008 接入，本票不做。
  - `template.go`：毒舌 / 活泼 / 温和三档基础文案（软提醒、欢迎回来、静音确认、语气确认）；不调 AI；每个软提醒文案必须对应动作。
  - 无应用画像；`Snapshot.profile_enabled=false`；`Snapshot.focus=nil`。
  - 平台不支持 / 空闲读取不可用 → `unsupported`；不冒泡，不影响正式提醒。

- [ ] 逻辑层：`sit_runtime.go` + `app.go`
  - 组合根启动 5 秒 tick；只在「有启用场景 + 至少一条 stand/move + 当前处于其生效时段」时工作；读取现有 `systemActivity` 与 `pause.Bundle.Lock`；不新增权限。
  - 绑定：`GetSitSettings` / `SetSitSettings`、`GetSitSnapshot`、`AckSitBubble`、`TakeActiveBreak`（本票只清零 + 记录休息）、`SnoozeSitNudge`、`MuteSitToday`、`SetSitTone`。
  - 不新增事件域；不写 CheckIn；不广播 `reminder:changed`。
  - 启动/停止与 `ServiceStartup` / `ServiceShutdown` 对齐；`sit_runtime` 不 import Wails 到 `internal/sit`。

- [ ] UI 层：`frontend/src/views/Pet.vue`
  - 在现有 4 秒轮询中加入 `GetSitSnapshot`；情绪 class（`sit-energetic` / `sit-concerned` / `sit-droopy`）只作用于 idle 层，等待/庆祝/休息陪做优先。
  - 新气泡分支放在**待处理之后**（最低优先级）：`陪做 > 预警 > 顺延 > 演示建议 > 暂停 > 待处理 > 久坐/记忆`。
  - 气泡动作：起来走走 / 再坐 10 分钟 / 更多；「更多」在气泡内展开（今天先这样 / 别这样说 / 打开客户端），不使用会被小窗裁切的浮层。
  - 真正渲染气泡时调用 `AckSitBubble(id)`；贴边收起、宠物隐藏、更高优先级气泡占用时不 Ack。
  - 动作调用绑定后刷新快照/待处理；欢迎回来不占主动发言预算。
  - 复用现有气泡扩窗；新增 `frontend/src/lib/sit.ts` 的显示映射与 `Pet.vue` 的 `sitMoodClass`。
  - 不做 `/sit` 页、不做设置 UI（005）。

- [ ] 测试
  - `internal/sit`：切应用不重置、分级空闲、锁屏、主动休息、break_wait、60/90、15 分钟空档、每会话 2 次、5 次/天、30 分钟间隔、Ack 幂等、今天先这样、平台降级。
  - `service`：SitSession/SitBreak/SitSpeech 的收尾、active_seconds、今日聚合、清空基础数据（若本票提供）。
  - Pet.vue：优先级、Ack 调用、三个动作、情绪 class、气泡扩窗、拖拽不冲突；headless/真实 GUI 走查。
  - `make test` / `make lint`。
  - 手工 demo：用临时短阈值或注入假时钟验证 60/90；提交前移除临时调试阈值。

## 验收标准

- 有 stand/move 场景时，连续用机到 60 分钟（15 分钟空档）出现一条软提醒；忽略后 90 分钟出现第二条；此后不再冒。
- 「再坐 10 分钟」延后 10 分钟；「起来走走」清零并进入等待，输入恢复后欢迎回来；情绪从担心/耷拉回到精神。
- 宠物气泡不抢待处理、休息、暂停；不显示应用名；不写 CheckIn、不进待处理、不发通知。
- 每天最多 5 次主动发言、每会话最多 2 次软提醒；未展示的气泡过期不计数，确认幂等。
- 主开关关闭、无场景、无 stand/move、平台不支持时都不冒泡，正式提醒不变。
- 不做应用画像、不抵扣目标、不做 `/sit` 页；这些留给 004–010。
- `make test` / `make lint` 通过；真实 GUI 走查记录。

## 实现记录（2026-10-22）

- `internal/sit/tracker.go`：全局久坐时钟、切应用不重置、分级空闲、锁屏休息、主动休息/break_wait、空闲休息、三态情绪；`policy.go`：60/90、15 分钟空档、2 次/会话、5 次/天、30 分钟间隔、snooze 例外；`template.go`：三档基础文案/动作/标题；纯测试覆盖。
- `service/sit.go`：SitSession 生命周期/心跳、SitBreak、SitSpeech pending/ack/expire、预算查询、closed totals；真实 SQLite 测试。
- `sit_runtime.go` + `app.go` 绑定：5 秒 tick、场景/时段/暂停/静默/待处理/隐藏宠物抑制、Tracker 事件落库、欢迎/「去吧」气泡、Ack、snooze、今天先这样、切换语气；`wails3 generate bindings` 已重新生成，`bindings_test` 通过。
- `Pet.vue`：4 秒轮询久坐快照、最低优先级气泡、Ack、三动作 + 更多、情绪层（芽担心/耷拉）、气泡扩窗/贴边隐藏、减弱动态；`lib/sit.ts` 增加情绪 class 与动作文案。
- `make test` / `make lint` 全绿。
- **真实 GUI 验收（2026-10-22）**：用 MCP/private_mac_apis 构建 + 临时 HOME + 临时 `.app` 启动；宠物窗口 239×379 真实渲染。
  - 注入一条 `sit_speeches` 软提醒后，`.bubble--sit` 显示标题「久坐提醒」、文案、三动作「起来走走 / 再坐 10 分钟 / 更多」；4 秒内 `acked_at` 写入，确认 Ack 链路。
  - 「更多」展开第二行「今天先这样 / 别这样说 / 打开客户端」；点「起来走走」后 `sit_sessions.end_reason=active_break`、`sit_breaks` 写入 active 休息、气泡切到「去吧 / 去吧，我等你。」并 Ack。
  - 「再坐 10 分钟」收起气泡并 Ack；「别这样说」把 `config.sit_tone` 切到 gentle；「今天先这样」写入当天 `sit_muted_date`，快照 `muted_today=true`、mood=energetic。
  - 情绪 CSS 在真实 webview 验证：`pet--sit-concerned` → matrix(0.9925,0.1219,…)，`pet--sit-droopy` → matrix(0.9659,0.2588,…)，与 7°/15° 一致。
  - 验收后退出临时 app、删除临时 HOME/.app，并重新打开 `/Applications/Morrow.app`。
- **仍未覆盖**：60/90 分钟真实长跑（单元测试用假时钟覆盖）；macOS/Windows 真实锁屏转换（001 已标记，004 验收）；B-9 崩溃/重启收尾（留给 005/011）；未提交（等待全部任务与 code-review）。
