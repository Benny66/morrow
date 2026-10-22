# 006 — 应用画像同意、采样与会话明细

**Blocked by**: 002、005

**状态**: 已完成（2026-10-22；真实 GUI 同意流/采样/锁屏验收）

> 规格：`docs/specs/2026-10-22-sit-awareness-and-sassy-pet.md` 的 A-1、A-2、A-3、A-4（画像）、E-1、E-2、E-4（应用明细）、E-6（画像状态）、E-10、7.5。
> 本票只做「记录与看见」；类别纠正、日期回看、导出边界与专注会话留给 007/008。

## 目标

用户可以在首次引导或升级卡片里显式同意应用画像；开启后在满足条件的场景内记录前台应用会话，久坐页出现应用名、行为类别、前台时长与会话数。拒绝则留在基础模式，功能不消失。

## 涉及层

- [x] 数据层：`internal/service/sit.go`
  - `app_usage_sessions` 的创建/收尾：应用切换、空闲清零、锁屏、功能关闭、崩溃收尾；`last_seen_at` 心跳最多每分钟一次。
  - 排除 Morrow 自身；前台应用读不到时按「未知」记录并累计诊断计数，不写假应用名。
  - `GetSitDay` 扩展返回应用明细（应用名/标识、类别、前台时长、会话数）与类别分布；类别按 `internal/sit/category.go` 的内置映射派生。
  - `SetSitProfileEnabled(enabled, deleteHistory)`：开启写 `profile_asked=true`；关闭立即停止采样；`deleteHistory` 时清除应用会话、类别纠正与 memory 类发言/派生记忆（保留 nudge/greeting 发言与当天预算，不追溯改 CheckIn）；保留历史时停止新记录。
  - `ClearAppProfile` 供关闭时删除使用；007 的页面「清空应用与久坐数据」另走全量清空。
- [x] 逻辑层：`internal/sit/category.go`
  - 固定类别：coding / design / browsing / communication / media / other（中文展示由前端镜像）。
  - macOS bundle ID 与 Windows 进程名两份内置表；会议应用复用 `pause` 的会议名单归入 communication；浏览器归 browsing；未知归 other。
  - 展示名：常见应用内置中文名；未知显示原始标识（不读窗口标题）。
  - 类别在读取时派生；本票不做用户覆盖（007）。
- [x] 逻辑层：`sit_runtime.go`
  - 5 秒采样器：仅当 `SitEnabled && SitProfileEnabled && 功能处于有效场景/时段` 时读取前台应用；锁屏/空闲≥15/无场景/总开关关闭时停止或降级。
  - 会话切分：应用变化、真实休息、锁屏、功能关闭时收尾；不按每次轮询写库。
  - `Snapshot.profile_enabled` / `profile_supported`；画像状态 `profile_status` 由 `GetSitSettings` 提供（ok / unsupported / read_error / no_data）；应用名绝不进入 `Snapshot` 或宠物气泡。
  - 采样器启动/停止与 `ServiceStartup` / `ServiceShutdown` 对齐；`internal/sit` 不 import Wails。
- [x] UI 层：
  - `Welcome.vue`：现有三步后增加「久坐感知」同意页；说明记录内容、本机、可关闭/清空、默认毒舌；两个动作「开启应用画像」「只使用基础检测」；两者都不阻塞完成；跳过整段引导时 `profile_asked` 保持 false。
  - `Home.vue`：已完成引导的升级用户显示一次性说明卡（开启/基础二选一，告知默认毒舌）；选择后不再出现。
  - `Settings.vue`：久坐区域增加应用画像开关；首次开启走同一说明；关闭时弹出「保留历史 / 同时删除」。
  - `Sit.vue`：今日增加「应用使用」表（应用名、类别、前台时长、会话数）与类别分布；空状态区分「画像未开启」「已开启但今天无数据」「读取失败」。
  - 宠物气泡仍只用类别（本票可暂不引用类别，留给 009）；不显示应用名。
- [x] 测试
  - 采样：应用切换切分、同应用连续只一条、Morrow 排除、未知处理、idle/lock 收尾、心跳上限、崩溃收尾、不按 5 秒写库。
  - 类别：常见应用、会议归 communication、浏览器归 browsing、未知归 other、展示名回落。
  - 同意：引导两个选择、跳过引导、升级卡片、设置关闭保留/删除、profile_asked 幂等、拒绝后基础模式仍工作。
  - 页面：应用表/分布、三种画像空状态；`Snapshot`/气泡无应用名（护栏或类型断言）。
  - 权限/隐私：不需要屏幕录制/摄像头；日志无应用名。
  - `make test` / `make lint`。
- [x] 文档：`docs/map.md` 增加 category/sampler；设置/引导隐私文案；`CONTEXT.md` 类别与画像术语校对。

## 验收标准

- 干净安装引导出现同意页；选「基础」→ 基础模式可用、无应用会话；选「开启」→ `profile_enabled=true`、`profile_asked=true`。
- 升级用户看到一次性卡片；选择后重启不再出现；设置里仍可改。
- 开启画像并使用若干应用后，久坐页显示应用名、类别、前台时长与会话数；同一应用连续使用不产生 5 秒流水。
- 关闭画像立即停止采样；选择删除后应用会话/类别/记忆清空，CheckIn 与连续天数不变。
- 应用名不出现在宠物气泡或 `SitSnapshot`；日志不记录应用名。
- 平台不支持/读取失败有无症状的可见状态，不显示成 0 分钟。
- `make test` / `make lint` 通过；真实 GUI 走查记录。

## 实现记录（2026-10-22）

- 数据/逻辑：`app_usage_sessions` 创建/心跳/收尾（应用切换、锁屏、空闲、功能关闭、崩溃）；`GetSitDay` 返回应用明细与类别分布；`SetSitProfileEnabled` 写 `profile_asked`、关闭立即收尾并按需 `ClearAppProfile`；类别映射在 `internal/sit/category.go`（含常见应用展示名、会议复用 pause 名单）。新增 `RecoverOpenAppUsageSessions`，启动时按最后心跳把遗留开放会话收尾为 `abandoned`。
- 采样：`sit_runtime.go` 5 秒前台应用采样，只在画像开启、场景生效、未锁屏/长空闲时记录；同应用连续只保留一条会话并最多每分钟心跳一次；Morrow 自身、`loginwindow`/屏保等系统界面排除；应用名不进入 `Snapshot`/气泡/日志。
- 前端：Welcome 第 4 步同意；Home 升级卡；Settings 画像开关与保留/删除选择；Sit 页应用表、类别分布与三种画像空状态；`lib/sit.ts` 镜像补齐画像状态。
- **锁屏检测修复（跨票，真实 GUI 发现）**：macOS 26 的 `CGSSessionScreenIsLocked` 是 `CFBoolean`，旧解析只认 `CFNumber`，锁屏被静默当成未锁定。现同时解析两种类型；锁屏 watcher 启动时先同步读一次，避免启动首轮 tick 在 watcher 首次轮询前凭空生成久坐/锁屏休息；Windows watcher 同样补首轮同步读取。
- **文案修复（真实 GUI 发现）**：设置页久坐说明此前硬编码「当前为基础模式」，画像开启时仍显示基础模式；改为按开关动态显示。画像状态 `no_data` 的标签从「今天还没有记录」改为「当前未记录」，避免与已有应用明细并存时自相矛盾。
- GUI（临时 app + 临时 HOME，MCP DOM/绑定断言）：干净安装 4 步引导选「开启」→ `onboarding_completed=true`、`profile_enabled=true`、`profile_asked=true`；前台切到终端/Safari 后 5 秒采样落库（同应用连续 15 秒仅 1 条，期间只有首尾心跳，无 5 秒流水），`/sit` 显示应用名/类别/分钟/会话数与类别分布；Morrow 自身未入库；未知应用 `com.qodercn.app` 归「其他」并显示原始标识。设置关闭选「保留历史」→ 配置关闭、7 条应用会话保留、CheckIn/久坐会话不变；重新开启后关闭选「同时删除」→ 应用会话清零、CheckIn 与久坐会话不变，提示「已关闭并删除应用画像历史」。
- GUI 同意覆盖：升级用户卡片开启后 `asked/enabled=true`，重启不再出现；干净安装跳过引导 → `onboarding_completed=true`、`profile_asked=false`，首页按规格 A-1 出现一次性卡；干净安装选「只使用基础检测」→ `asked=true`、`enabled=false`、无应用会话、无卡片。
- GUI 锁屏：真实锁屏下 `GetPauseState` 为 `paused=true/reason=lock`，快照 `idle`，无新应用会话；启动时遗留的 `loginwindow` 开放会话被崩溃恢复收尾为 `abandoned`（日志 `count=1`）。
- 验证：`make test` / `make lint` 通过。验收后清理临时环境并重新打开 `/Applications/Morrow.app`。
- 未覆盖：真实锁屏/空闲的分钟级长跑与 Windows 实机留 011；本次 GUI 期间屏幕被系统锁屏，无法在解锁态复测「关闭画像后继续切换应用不落库」，该路径由 `setProfileEnabled` 即时收尾与单测覆盖；类别纠正、按日回看、导出边界留 007。
