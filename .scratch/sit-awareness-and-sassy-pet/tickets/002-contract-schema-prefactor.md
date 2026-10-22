# 002 — 预重构：共享契约、CheckIn source、配置与模型

**Blocked by**: 001

**状态**: 已完成（2026-10-22；Windows Win+L 实机待 004）

> 规格：`docs/specs/2026-10-22-sit-awareness-and-sassy-pet.md` 的 2.1、2.2、F-1、F-2、F-3、7.2–7.4、A-7、B-3、B-6、E-10。
> 这是预重构（expand），不是用户可见切片；完成后行为不变。

## 目标

一次性补齐后续切片共用的数据/配置/契约，避免 003–010 反复修改共享类型。完成后 `make test` / `make lint` 全绿，旧功能零回归。

## 涉及层

- [ ] 数据层：`internal/model/checkin.go`
  - `CheckIn` 增加 `Source string`（`json:"source"`，`gorm:"size:16;not null;default:'manual'"`）。
  - 常量：`SourceManual = "manual"`、`SourceActiveBreak = "active_break"`、`SourceLockBreak = "lock_break"`。
  - 现有 `act` 事务抽出一个带 source 的内部入口；`CompleteReminder` / `SkipReminder` / `SnoozeReminder` 仍写 `manual`，行为不变。
  - 旧库 AutoMigrate 增加列；旧行读取为 `manual`。

- [ ] 数据层：新模型（按规格 7.3）
  - `AppUsageSession`：`platform`、`app_id`、`started_at`、`ended_at`、`last_seen_at`、`date_key`、`end_reason`。
  - `SitSession`：`started_at`、`ended_at`、`last_seen_at`、`active_seconds`、`date_key`、`end_reason`、`credited_reminder_id`、`credited_check_in_id`。
  - `SitBreak`：`sit_session_id`、`at`、`kind`、`duration_sec`、`credited_reminder_id`、`credited_check_in_id`。
  - `SitSpeech`：`date_key`、`sit_session_id`、`kind`、`template_id`、`text`、`created_at`、`acked_at`、`expires_at`。
  - `AppCategoryOverride`：`platform`、`app_id`、`category`、唯一键 `(platform, app_id)`。
  - 全部登记进 `model.AllModels()`；`database.AutoMigrate` 自动建表；索引按查询需要（date_key、sit_session_id、platform+app_id）。

- [ ] 数据层：`internal/config/config.go`
  - `Config` 增加 `SitEnabled`（默认 true）、`SitProfileEnabled`（默认 false）、`SitProfileAsked`（默认 false）、`SitNudgeThresholdMinutes`（默认 60，合法 45/60/90）、`SitTone`（默认 `sassy`，合法 `sassy` / `lively` / `gentle`）、`SitMutedDate`（空串）。
  - `SitSettings` DTO：`enabled`、`profile_enabled`、`profile_asked`、`threshold_minutes`、`tone`，以及只读的 `platform_supported`、`profile_status`（ok / unsupported / read_error / no_data）；`LoadSitSettings` / `SaveSitSettings` 做夹取与回落。
  - 旧 config.json 缺失字段由 `Default()` 继承，不写迁移。

- [ ] 逻辑层：`internal/sit/contract.go`
  - 枚举：`State`（idle / tracking / break_wait / muted / unsupported）、`Mood`（energetic / concerned / droopy）、`Tone`（sassy / lively / gentle）、`Category`（coding / design / browsing / communication / media / other）、`BubbleKind`（nudge / memory / greeting）、`ActionID`（start_break / snooze_10 / more / dismiss / open_app）。
  - 类型：`Snapshot`、`Bubble`、`Focus`、`SitActionResult`；字段按规格 7.4，**`Snapshot` 与 `Bubble` 不含任何应用名字段**。
  - 只放契约，不写行为。

- [ ] 逻辑层：`frontend/src/lib/sit.ts`
  - 镜像上述枚举、动作 ID、气泡类型与展示标签；`SitSnapshot` 类型。
  - `internal/guard/parity_test.go` 增加双向校验；改 Go 不同步 TS 必须变红。

- [ ] 逻辑层：`internal/pause` 的锁屏 watcher 与暂停开关解耦
  - `pause.Config` 增加 `LockWatcherEnabled`；`LockEnabled` 仍只控制 `CompositeProvider` 是否包含锁屏信号。
  - `NewBundle` 在 `LockWatcherEnabled` 为 true 时启动 watcher；`Bundle.Lock` 已存在，供久坐运行时读取 `IsLocked()` / `Since()`。
  - `bundleConfigFrom` 设置 `LockWatcherEnabled = PauseLockEnabled || SitEnabled`，保证用户关闭「锁屏暂停」时久坐仍能读到锁屏时长，且不会因此重新启用锁屏暂停。
  - 按 001 结论补齐 Windows Win+L 检测（若需要）；不能补齐时在代码注释与设置文案里明确降级。

- [ ] 逻辑层：文件权限
  - macOS/Linux 上把数据库文件权限收紧到 `0600`（若现有 `appdir`/database 初始化未设置）；Windows 依赖用户目录 ACL，记录说明。
  - 不改导出文件权限之外的行为；导出权限在 007 处理。

- [ ] UI 层：不新增控件；`frontend/src/lib/sit.ts` 只有类型与常量。

- [ ] 测试
  - config：默认值、45/60/90 夹取、非法 tone 回落、Settings 往返。
  - model/service：CheckIn source 迁移、旧行默认 manual、三种 source 的写入；新表 AutoMigrate。
  - pause：`LockWatcherEnabled` 打开但 `LockEnabled=false` 时 watcher 运行、Composite 不产生 ReasonLock；反向组合。
  - parity：新枚举与动作 ID 双向一致。
  - `make gen` / `make lint` 重新生成绑定（如有类型导出）。

## 验收标准

- `CheckIn.Source` 对旧数据默认 `manual`；完成率、连续天数、今日统计不变。
- 新表全部在 `AllModels()` 中；旧库升级后可启动，新表为空。
- `SitSettings` 默认值与夹取正确；旧配置无需迁移。
- 用户关闭「锁屏暂停」时，锁屏 watcher 仍可按 001 的结论为久坐提供时长；暂停行为不受影响。
- `SitSnapshot` / `Bubble` 类型中没有应用名字段；TS 镜像一致。
- 不新增前端依赖；`make test`、`make lint` 通过。
- 覆盖规格 F-1、F-2、F-3、A-7、B-3、B-6 的契约前置部分。

## 实现记录（2026-10-22）

- `model.CheckIn` 增加 `Source`（manual/active_break/lock_break）；`act` 拆出 `actWithSource`，手动路径写 manual，旧库 AutoMigrate 默认 manual（`internal/database/migration_source_test.go` 覆盖）。
- 新增 `AppUsageSession` / `SitSession` / `SitBreak` / `SitSpeech` / `AppCategoryOverride` 并登记 `AllModels()`；唯一索引与迁移测试通过。
- `config` 增加 `SitEnabled` / `SitProfileEnabled` / `SitProfileAsked` / `SitNudgeThresholdMinutes` / `SitTone` / `SitMutedDate` 与 `SitSettings` DTO、夹取、静音当天；测试覆盖默认值/往返/夹取/跨日恢复。
- 新增 `internal/sit/contract.go`（状态/情绪/语气/类别/气泡/动作/结束原因/休息类型/画像状态 + Snapshot/Bubble/Focus/SitActionResult），`frontend/src/lib/sit.ts` 镜像；`parity_test.go` 新增 9 组双向校验。
- `pause.Config` 拆分 `LockWatcherEnabled`；`NewBundle` 由它启动锁屏 watcher，`LockEnabled` 只控制 Composite；`bundleConfigFrom` 设置 `PauseLockEnabled || SitEnabled`。`lock_windows.go` 增加 `OpenInputDesktop` 检测 Win+L；Windows 交叉编译通过，真实 Win+L 验收留给 004。
- 数据库文件在 macOS/Linux 收紧到 0600（`internal/database/database_test.go`）。
- 未新增 App 绑定方法，因此无需重新生成前端绑定；`make test`、`make lint` 全绿。
