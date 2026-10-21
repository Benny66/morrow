# Morrow

本地优先的桌面健康提醒应用。提醒由一只叫「小满」的桌面宠物承载：一滴水，头顶一株芽，芽随今日完成率生长。

## 功能

### 提醒

- **周期性提醒**：每天在指定时段内按间隔重复，例如「09:00–18:00 每小时喝水」；也支持每天只响一次的每日单点。
- **一次性提醒**：指定绝对日期与时刻，只触发一次。独立于场景启停，错过不跨日补发，但会在首页保留可见条目。
- **六种类型**：喝水 / 站立 / 走动 / 如厕 / 护眼放松 / 其他，决定图标、进度量词与休息引导。

### 智能免打扰

到点时若用户不该被打扰，自动顺延、不补发。六路信号可逐项开关：

| 信号 | 触发条件 | 默认 |
|---|---|---|
| 空闲 | 连续 3 / 5 / 10 分钟无键鼠输入 | 开，5 分钟 |
| 锁屏 | 屏幕锁定或系统休眠 | 开 |
| 会议 | Zoom / Teams / Webex / 腾讯会议 / 飞书 / 钉钉 / FaceTime 在前台 | 开 |
| 全屏 | 前台窗口铺满当前显示器 | 关 |
| 专注模式 | macOS 系统专注开启（12+） | 关 |
| 日历忙闲 | macOS 日历当前处于忙碌时段 | 关 |

暂停期间：

- 周期性提醒**顺延不补发**，恢复后按原间隔重新计时；
- 每日单点与一次性提醒记为「已错过」，在首页保留可见、不补弹。

感知只用本机活跃度与前台应用身份：不采集屏幕内容、不读日历事件标题/参与人/备注/地点、不上传。

### 休息模式

站立 / 走动 / 护眼三类提醒在到点前 60 秒给出预警（宠物气泡 + 菜单栏），到点进入倒计时覆盖层，展示与类型匹配的内置引导动作：

| 类型 | 时长 | 引导方向 |
|---|---|---|
| 护眼放松 | 60 秒 | 远眺 + 呼吸 |
| 站立 | 120 秒 | 肩颈拉伸 |
| 走动 | 180 秒 | 起身走动 |

倒计时结束自动记「完成」；提前结束可选「算完成」或「跳过」。喝水 / 如厕 / 其他保持简单打卡。引导内容内置，离线可用，不调用 AI。

### 场景与 AI

- **场景**：一段自然语言描述的处境（如「备孕办公日」），是周期性计划的组织单位；同一时刻至多启用一个。
- **AI 计划**：由场景生成整套周期性提醒，逐条可编辑。
- **快速添加**：在首页输入文字或附加图片/文件，AI 解析为一次性或周期性草稿；先预览、逐条确认，再落库。

### 打卡与进度

- 完成 / 跳过 / 推迟（默认 5 分钟，可调 1–120）。
- 首页：待处理、已错过、今日进度、下一次触发倒计时。
- 计划页：每条提醒的今日进度与连续达标天数。

### 桌面与系统集成

- **桌面宠物**：透明置顶小窗常驻桌面。待处理时进入等待、清空后庆祝、休息时陪做；芽按今日完成率分五档生长；支持缩放、隐藏与导入素材包。
- **菜单栏**：显示今日完成/目标、下一条提醒时间、待处理数量，以及暂停或休息原因；提供「打开主窗」「查看待处理」「退出」。
- **强提醒**：到点在全屏遮罩上展示待处理卡片，覆盖所有显示器；主屏显示完整操作，副屏显示简化卡片。
- **其他**：静默时段（默认 22:00–08:00）、系统通知、开机自启、全局快捷键、明暗主题、数据库导出与恢复。

### 本地优先

SQLite 本地落盘，无账号体系。只有主动使用 AI 功能时才访问你配置的端点；其余能力全部离线可用。

## 技术栈

- 桌面壳：Wails v3（Go + 系统 WebView）
- 前端：Vue 3 + TypeScript + Vite + Element Plus + Pinia
- 数据：gorm + SQLite（纯 Go 驱动，交叉编译无 CGO 依赖）
- AI：OpenAI 兼容的最小客户端，可配置 Base URL / 模型 / Key

## 环境要求

| 依赖 | 版本 | 说明 |
|---|---|---|
| Go | ≥ 1.25 | `go version` 确认 |
| Node | ≥ 20 | `node --version` 确认 |
| Wails CLI | v3，与 `go.mod` 对齐（当前 `v3.0.0-beta.23`） | `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.23` |
| 平台依赖 | — | Windows 需 WebView2；Linux 需 GTK4 + WebKitGTK 6.0 |

## 快速开始

```bash
make dev       # 开发态（含前端热更新）
make build     # 编译当前平台产物
make test      # Go 测试（含架构护栏）
make lint      # gofmt + 护栏 + go vet + ESLint + vue-tsc
```

根包用 `//go:embed` 内嵌 `frontend/dist`，该目录不入库。首次检出先跑一次
`cd frontend && npm install && npm run build`（或直接 `make dev`）；`make test` / `make lint`
会检测缺失并提示。

## 常用命令

| 命令 | 作用 |
|---|---|
| `make dev` | 开发态，前端热更新 |
| `make build` | 编译当前平台产物 |
| `make test` | Go 测试与架构护栏 |
| `make lint` | 静态检查（gofmt / Go 护栏 / go vet / ESLint / vue-tsc） |
| `make smoke` | 冒烟测试（构建 → 启动 → 断言 → 清理） |
| `make package` | 真安装包（`make package os=macos\|windows\|linux`） |
| `make install` | 构建并安装到本机 `/Applications`（macOS） |
| `make icon` | 从 `build/icon/appicon.svg` 重新生成三平台图标 |

## 目录结构

完整导航见 [`docs/map.md`](docs/map.md)。核心目录：

```
main.go                 # 入口：窗口、托盘、单实例、日志、数据库
app.go                  # 前端可调用的绑定方法（业务一律经 service）
alert_window.go         # 强提醒窗口（多显示器）
pet_window.go           # 桌面宠物窗口（透明 + 置顶 + 位置还原）
pet_dock.go             # 宠物贴边与交互
reminder_runtime.go     # 提醒调度运行时接线
internal/reminder/      # 调度引擎（时间计算 / 触发 / 暂停 / 预警 / 补发策略）
internal/pause/         # 智能免打扰：六路信号与优先级聚合
internal/restmode/      # 休息模式状态机与内置引导内容
internal/ai/            # OpenAI 兼容客户端与容错 JSON 解析
internal/service/       # 业务服务（提醒、场景、打卡、成长、导入导出）
internal/model/         # 数据模型 + AllModels() 注册表
internal/database/      # gorm + SQLite 连接与迁移
internal/{config,logging,appdir,appinfo,apperr,event,page,tray,guard,...}
frontend/src/views/     # Home / Plan / Scenarios / Pet / Alert / Settings
frontend/src/lib/       # 事件、错误码、分页、提醒与暂停领域的前端契约镜像
```

## 数据与配置

应用数据目录（数据库、配置、日志同处）：

- macOS：`~/Library/Application Support/Morrow/`
- Windows：`%AppData%\Morrow\`
- Linux：`~/.config/Morrow/`

目录内主要是 `Morrow.db`、`config.json`、`app.log`。设置页会显示实际路径，并提供数据库导出与恢复。

> AI 的 API Key 以明文保存在 `config.json`。不要把数据目录提交进仓库或随手分享；
> 需要备份时优先使用设置页的数据库导出。

## 隐私

- **不采集内容**：不截图、不录屏、不读窗口标题；日历只读「此刻是否忙碌」这一个布尔值。
- **不使用摄像头**：判断「人是否在电脑前」只依赖系统空闲时长与前台应用身份。
- **不上传**：所有数据留在本机；AI 只是可选的一次性调用，只发送你主动输入的场景描述或文本。
- **可关闭、可降级**：六路免打扰信号逐项可关；权限被拒绝时对应信号静默缺席，其余功能照常工作。

## 构建与发布

- 版本号单一真相在 `build/config.yml` 的 `info.version`；`scripts/build.sh` 同时把它渲染进产物与 `-ldflags`，`scripts/set-version.sh` 是写入入口。
- `make package` 产出 macOS `.dmg`（含 Finder 美化布局）或 Windows NSIS 安装器；无 `makensis` 时降级为裸 exe。
- CI：`.github/workflows/build.yml` 跑静态检查与 macOS/Windows 编译；`.github/workflows/release.yml` 由 tag 触发自动发布。
- 版本号必须是数字点分格式（如 `1.0.0`），Windows 的 NSIS 不接受 `v` 前缀或哈希。

### macOS 本地签名

默认构建使用 ad-hoc 签名，其「指定要求」只有代码哈希；每次重新构建哈希都会变，
macOS 会把新二进制当成另一个应用，**屏幕录制 / 专注 / 日历授权会随之失效**。

本地开发前先创建一张固定的自签名证书（一次性）：

```bash
bash scripts/dev-signing-cert.sh
```

之后 `make install` 会自动用它签名，重建不再丢授权。

## 平台支持与已知限制

- 主要交付平台是 macOS / Windows。Linux 可编译，但 WebKitGTK 版本耦合，未纳入 CI。
- 智能免打扰在 macOS 上完整实现；Windows 上专注与日历两路信号静默缺席，不报错。
- 无 headless 模式：冒烟测试只做本地验证，CI 只编译不启动 GUI。
- 托盘启动失败时，关闭窗口会真正退出，避免留下看不见的无窗口进程。
- 窗口只记忆尺寸与最大化，不持久化位置。

## 文档

| 文档 | 内容 |
|---|---|
| [`CONTEXT.md`](CONTEXT.md) | 领域术语表 |
| [`docs/map.md`](docs/map.md) | 代码导航地图 |
| [`docs/adr/`](docs/adr/) | 架构决策记录 |
| [`docs/pet-character.md`](docs/pet-character.md) | 小满的形象、状态与生长规则 |
| [`docs/specs/`](docs/specs/) | 规格说明书 |
| [`AGENTS.md`](AGENTS.md) | 跨端架构铁律 |
