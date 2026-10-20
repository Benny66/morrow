# Morrow

本地优先的桌面提醒与习惯应用。提醒由一只叫「小满」的桌面宠物承载：一滴水，头顶一株芽，芽随今日完成率生长。

> Morrow 是产品名（古英语「明日 / 次日」），小满是内置宠物角色名。两者的命名取舍见 [`docs/adr/0002-product-name-morrow.md`](docs/adr/0002-product-name-morrow.md)。

## 它是什么

- **周期性提醒**：每天在指定时段内按间隔重复，例如「09:00–18:00 每小时喝水」；也支持每天只响一次的每日单点提醒。
- **一次性提醒**：指定绝对日期与时刻，只触发一次。独立于场景启停，错过不跨日补发，但会在首页保留可见条目。
- **场景与 AI 计划**：用一段自然语言描述处境（如「备孕办公日」），由 AI 生成整套周期性提醒；同一时刻至多启用一个场景，也可以完全手写计划。
- **快速添加**：在首页输入文字或附加图片/文件，AI 解析成一次性或周期性草稿；先预览、逐条确认，再落库。
- **打卡与进度**：完成、跳过、推迟（Snooze）；首页展示待处理、今日进度与下一个触发时间，计划页维护连续天数。
- **桌面宠物**：透明置顶小窗常驻桌面。有待处理时进入等待状态，清空后庆祝；芽按今日完成率分五档生长；支持缩放、隐藏与导入素材包。
- **本地优先**：SQLite 本地落盘，不依赖账号体系；只有配置了 AI 端点并主动使用 AI 功能时才联网。

## 技术栈

- 桌面壳：Wails v3（Go + 系统 WebView）、单实例锁、系统托盘、强提醒窗口
- 前端：Vue 3 + TypeScript + Vite + Element Plus + Pinia
- 数据：gorm + SQLite（纯 Go 驱动，交叉编译无 CGO 依赖）
- AI：OpenAI 兼容的最小客户端，可配置 Base URL / 模型 / Key

## 环境准备

| 依赖 | 版本 | 说明 |
|---|---|---|
| Go | ≥ 1.25 | `go version` 确认 |
| Node | ≥ 20 | `node --version` 确认 |
| Wails CLI | v3，与 `go.mod` 对齐（当前 `v3.0.0-beta.23`） | `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.23` |
| 平台依赖 | — | Windows 需 WebView2；Linux 需 GTK4 + WebKitGTK 6.0（未纳入 CI，见「已知限制」） |

## 快速开始

```bash
make dev       # 开发态（含前端热更新）
make build     # 编译当前平台产物
make test      # Go 测试（含架构护栏）
make lint      # gofmt + 护栏 + go vet + ESLint + vue-tsc
```

> 根包用 `//go:embed` 内嵌 `frontend/dist`，该目录不入库。首次 checkout 先跑一次
> `cd frontend && npm install && npm run build`（或 `make dev`），`make test` / `make lint`
> 会检测缺失并提示。

## 命令表

| 命令 | 作用 |
|---|---|
| `make dev` | 开发态，前端热更新 |
| `make dev-mcp` | 开发态并开放本机 MCP 服务（允许 Agent 操作运行中的应用） |
| `make build` | 编译当前平台产物（快速，不封装 dmg） |
| `make test` | Go 测试与架构护栏 |
| `make lint` | 静态检查（gofmt / Go 护栏 / go vet / ESLint / vue-tsc） |
| `make smoke` | 冒烟测试（构建 → 启动 → 断言 → 清理） |
| `make package` | 真安装包（`make package os=windows\|macos\|linux`） |
| `make install` | 构建并安装到本机 `/Applications`（macOS） |
| `make gen name=<module>` | 生成新模块（model + service + 绑定 + 页面） |
| `make icon` | 从 `build/icon/appicon.svg` 重新生成三平台图标 |
| `make verify-gen` | 生成器端到端验证 |

## 目录结构

完整导航见 [`docs/map.md`](docs/map.md)。运行时核心：

```
main.go                 # 入口：窗口、托盘、单实例、日志、数据库
app.go                  # 前端可调用的绑定方法（业务一律经 service）
pet_window.go           # 桌面宠物窗口（透明 + 置顶 + 位置还原）
pet_dock.go             # 宠物贴边与交互
alert_window.go         # 强提醒窗口
reminder_runtime.go     # 提醒调度运行时接线
internal/reminder/      # 调度引擎（时间计算 / 触发 / 一次性扫描）
internal/ai/            # OpenAI 兼容客户端与容错 JSON 解析
internal/service/       # 业务服务（提醒、场景、打卡、成长、宠物素材、导入导出）
internal/model/         # 数据模型 + AllModels() 注册表
internal/database/      # gorm + SQLite 连接与迁移
internal/{config,logging,appdir,appinfo,apperr,event,page,guard,...}  # 契约与运行时
frontend/src/views/     # Home / Plan / Scenarios / Pet / Alert / Settings
frontend/src/lib/       # 事件、错误码、分页、提醒领域的前端契约镜像
```

## 数据与配置

应用数据目录（数据库、配置、日志、崩溃日志同处）：

- macOS：`~/Library/Application Support/Morrow/`
- Windows：`%AppData%\Morrow\`
- Linux：`~/.config/Morrow/`

目录内主要是 `Morrow.db`、`config.json`、`app.log`。设置页会显示实际路径，并提供数据库导出。

> AI 的 API Key 以明文保存在 `config.json`。不要把数据目录提交进仓库或随手分享；
> 需要备份时优先使用设置页的数据库导出。

### 从旧名字迁移

旧版数据目录叫 `my-notice`。改名后不会自动迁移，手动复制并改名数据库即可（保留旧目录作备份）：

```bash
OLD="$HOME/Library/Application Support/my-notice"
NEW="$HOME/Library/Application Support/Morrow"
cp -a "$OLD" "$NEW"
mv "$NEW/my-notice.db" "$NEW/Morrow.db"
```

Windows 对应 `%AppData%\my-notice` → `%AppData%\Morrow`，同样把 `my-notice.db` 改名为 `Morrow.db`。

## 构建与发布

- 版本号单一真相在 `build/config.yml` 的 `info.version`；`scripts/build.sh` 同时把它渲染进产物与 `-ldflags`，`scripts/set-version.sh` 是写入入口。
- `make package` 产出 macOS `.dmg`（含 Finder 美化布局）或 Windows NSIS 安装器；无 `makensis` 时降级为裸 exe。
- CI：`.github/workflows/build.yml` 跑静态检查与 macOS/Windows 编译；`.github/workflows/release.yml` 由 tag 触发自动发布。
- 版本号必须是数字点分格式（如 `1.0.0`），Windows 的 NSIS 不接受 `v` 前缀或哈希。

## 已知限制

- 主要交付平台是 macOS / Windows；Linux 的产物编译不在 CI 矩阵内（WebKitGTK 版本耦合）。
- 无 headless 模式：冒烟测试只做本地验证，CI 只编译不启动 GUI。
- 托盘启动失败时，关闭窗口会真正退出，避免留下看不见的无窗口进程。
- 窗口只记忆尺寸与最大化，不持久化位置（Wails 创建期限制）。

## 文档

| 文档 | 内容 |
|---|---|
| [`CONTEXT.md`](CONTEXT.md) | 领域术语表（提醒类型、场景、草稿等） |
| [`docs/map.md`](docs/map.md) | 代码导航地图 |
| [`docs/adr/`](docs/adr/) | 不可逆决策记录 |
| [`docs/pet-character.md`](docs/pet-character.md) | 小满的形象、状态与生长规则 |
| [`docs/specs/`](docs/specs/) | 规格说明书 |
| [`openspec/specs/`](openspec/specs/) | 能力规格基线 |
| [`AGENTS.md`](AGENTS.md) / `CLAUDE.md` | 架构铁律与域内规范 |
