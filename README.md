# wails-bailer-starter

基于 **Wails v2 (Go + Vue3)** 的桌面客户端脚手架基座，开箱即用、可反复复用。

> **这是脚手架母版，不是可直接运行的项目。** 用它开发新项目，先执行实例化脚本生成干净项目。

- 后端：Go + gorm + SQLite（纯 Go 驱动，跨平台交叉编译无 CGO 依赖）
- 前端：Vue3 + Vite + Element Plus + Vue Router + Pinia
- 设计系统：暗色优先的专业工具风，单主色派生全色阶
- 运行时：单实例锁（含二次启动参数转发）、系统托盘（Windows/Linux/macOS，关闭窗口隐藏到托盘）、窗口几何记忆、
  日志轮转、配置读写、原生对话框、崩溃落盘、数据目录可达、数据库导出
- 可观测：Wails 日志接入 slog（前端日志同样落 `app.log`）、前端全局错误兜底、版本可追溯
- 工程化：架构护栏（AST/ESLint/TS 类型）、依赖登记制、`make gen` 模块生成器、OpenSpec 治理

## 实例化新项目

```bash
bash scripts/init.sh myapp    # 生成干净新项目（替换模块名 + 清空归档历史 + git init）
cd ../myapp
make dev                      # 启动开发态
```

```bash
bash scripts/init.sh myapp com.mycompany   # 第二个参数可选：macOS bundle id 前缀
```

`init.sh` 会把母版的占位符全局替换为你的项目名（go.mod / import / wails.json / index.html / 生成器脚本），
并生成 macOS 的 bundle id 前缀，最后清空母版的归档历史、保留能力基线。

> **bundle id 默认是 `com.example.<项目名>`**——`com.example` 是 IANA 保留的示例域名，
> 明显是占位符。发布前请换成你的公司域名（第二个参数，或直接改 `build/darwin/Info.plist`）。
> 它是 macOS 识别应用的标识，多个产品共用会让登录项、权限授予、文件关联互相串味。

## 环境准备

| 依赖 | 版本 | 说明 |
|---|---|---|
| Go | ≥ 1.25 | `go version` 确认 |
| Node | ≥ 20 | `node --version` 确认 |
| Wails CLI | v2.15+ | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |
| 平台依赖 | — | Windows 需 WebView2；Linux 需 `libgtk-3-dev libwebkit2gtk-4.1-dev` + 构建时带 `-tags webkit2_41`（见下「已知限制」） |

## 快速开始

```bash
make dev       # 启动开发态（含前端热更新）
make build     # 编译当前平台产物
make package   # 打包安装包
```

> **首次 clone 后先跑 `make dev` / `make build` 再跑 `make test`**：根包（`main.go`）用
> `//go:embed` 嵌入 `frontend/dist`，而该目录不入库。`make test` / `make lint` 会检测到
> 缺失并直接告诉你修复命令（而不是抛一句难懂的 Go 编译错误）。

## 命令表

| 命令 | 作用 |
|---|---|
| `make dev` | 启动开发态（wails dev，前端热更新） |
| `make build` | 编译当前平台产物（快速，.app 不封装 dmg） |
| `make package` | 打包真安装包（`make package os=windows\|macos\|linux`） |
| `make test` | 运行 Go 测试（含架构护栏） |
| `make lint` | 静态检查（护栏 + go vet + ESLint） |
| `make smoke` | 冒烟测试（构建 → 启动 → 断言 → 清理） |
| `make gen name=<module>` | 生成新模块（锚点注入 + 幂等） |

## 目录结构

见 [`docs/map.md`](docs/map.md) —— 代码导航地图，AI 干活前先读它。

核心分层：

```
app.go                     # 绑定方法（前端可调用的 Go 方法）
internal/model/            # 数据模型 + AllModels() 注册表（唯一真相）
internal/service/          # 业务服务层（CRUD 逻辑）
internal/database/         # gorm + SQLite 连接、迁移
internal/guard/            # 架构护栏（AST 断言，随 go test 跑）
internal/appdir/           # 应用数据目录（数据库/配置/日志同处的唯一真相）
internal/{config,logging,dialog,tray,crash,reveal}/  # 运行时服务
frontend/src/lib/          # 管道契约前端侧（invoke / event / page）
frontend/src/composables/  # 组合式函数（useEvent / usePagedList）
frontend/src/styles/       # 设计令牌（tokens.css / theme.css）
frontend/src/layouts/      # 应用壳（侧栏 + 标题栏）
frontend/src/views/        # 页面
_example/                  # 黄金范例模板（make gen 的母版）
scripts/gen.sh             # 模块生成器
```

## 生成新模块

```bash
make gen name=asset
```

从 `_example/` 模板生成四段：model + service + 绑定方法 + 前端页面，并自动注入到 `AllModels()`、`app.go`、路由、菜单。生成后填 `// TODO` 处业务逻辑，再 `make dev` 重新生成 bindings。

## 换肤（设计系统）

专业工具风，暗色优先。换肤只需改 `frontend/src/styles/tokens.css` 里**品牌区**的主色，其余颜色全部由主色自动派生，禁硬编码 hex 色值（ESLint 护栏强制）。

logo 在 `build/appicon.png`（前端资源与托盘图标共用）。

> 品牌名/模块名已占位符化为 `my-notice`，通过 `scripts/init.sh` 实例化时全局替换，无需手改。

## 架构护栏

本基座把"约定"编译成"会红的检查"（详见 `AGENTS.md`）：

- **分层越界**：`app.go` 不直接 import gorm/database（经 service）；model 是叶子。
- **模型注册双向校验**：带 `BaseModel` 的结构体必须登记进 `AllModels()`。
- **依赖登记制**：新增依赖必须登记 `deps.yaml`（双向校验）。
- **前端 import 安全**：渲染层禁 import node 能力；禁硬编码色值。
- **前端镜像一致性**：错误码/事件动作/页大小/应用事件四处前端镜像必须与 Go 单一真相
  逐项相等（`internal/guard/parity_test.go`，双向校验）。
- **接线不静默缺失**：`options.App.Logger` 必须接线、版本注入目标必须真实存在
  （`internal/guard/wiring_test.go`）——这类问题漏了没有任何症状。
- **类型检查**：`make lint` 含 `vue-tsc`（开发态 vite 不做类型检查，缺了这步会漂到打包才炸）。

护栏"感知自己瞎了"：解析到 0 个结果会 Fatal，而非静默放行。

## Windows WebView2 部署

Windows 端依赖 WebView2 运行时。大多数 Win10/11 已预装；若未安装，Wails 会在启动时引导安装（在线）。离线环境见 [Microsoft WebView2 部署文档](https://learn.microsoft.com/microsoft-edge/webview2/concepts/distribution)。

## 打包与发布

```bash
make package                          # 打包当前平台真安装包
make package os=windows               # 交叉编译 Windows .exe 安装器（默认 scope=user）
make package os=windows scope=machine # 装到 Program Files（需管理员）
make package os=macos                 # 仅 mac 上可用（Wails 不支持交叉编译 mac）
make package os=linux                 # 仅 linux 上可用（Wails 不支持交叉编译 linux）；未在 CI 验证
```

- **产物位置**：`build/bin/`。

- **macOS 安装**：产出 `.dmg`，内含 `MyApp.app` + `Applications` 软链 + 拖拽引导（背景图/箭头）。
  用户**拖拽 app 到 Applications** 即完成安装。macOS 没有"安装"动作——`.app` 本身就是完整应用。
  - **背景图**：由 `build/darwin/dmg-background.tpl.png`（纯底模板）在打包时**合成**——
    叠上当前应用名与拖拽箭头。要换底色/留白，替换该模板即可；箭头由 `scripts/compose-dmg-bg.sh`
    按图标锚点绘制（与 Finder 图标同一坐标系），**不要**把箭头画进模板，否则会与合成箭头重叠。
  - **美化校验**：打包结束会打印 `✓ 美化已生效` 或 `⚠ 降级产物（无美化）`——后者表示本机
    无 GUI 会话（如 SSH/CI）或 Finder 不可用，dmg 仍可用只是没美化。CI 若要求必须美化成功，
    设 `STRICT_POLISH=1`（降级时按构建失败处理）。

- **Windows 安装**：产出 NSIS `.exe` 安装器（需 `makensis`，见下），**双击走安装向导**即装好。
  - `scope=user`（默认）：装到 `%LOCALAPPDATA%\Programs`，**免管理员**，适合分发给普通员工。
  - `scope=machine`：装到 `Program Files`，需管理员权限，适合 IT 统一部署。
  - 无 `makensis` 时降级为裸 exe（非安装器），脚本会提示安装方式（macOS: `brew install makensis`；Windows: 装 [NSIS](https://nsis.sourceforge.io/)）。

- **版本号单一真相**：`wails.json` 的 `info.productVersion`。wails 用它渲染
  macOS `Info.plist`、Windows exe 版本资源与 NSIS 注册表；`scripts/build.sh` 把同一个值
  经 `-ldflags` 注入应用内（`app.log` 首行 / `GetAppInfo`）。**四处同源**，发布时用
  `bash scripts/release.sh <version>` 更新它。
  - 所有构建都走 `scripts/build.sh`（`make build` 亦然），它是唯一的版本注入点；
    macOS 构建后会回读产物 `Info.plist` 校验版本真的生效。
- **母版防呆**：含占位符时拒绝打包，需先 `scripts/init.sh` 实例化。
- CI：`.github/workflows/build.yml` 在 push/PR 时自动跑
  **静态检查**（`make test` / `make lint`，ubuntu）+ **macOS/Windows 编译**（产物上传为 artifact）
  + **生成器端到端验证**（`make verify-gen`，macos 腿）。
  Linux 的 Go 层覆盖由静态检查腿承担；Linux **产物编译不在矩阵内**（理由见下「已知限制」）。
- **自动发布（推荐）**：打 tag 即自动出 Release——
  ```bash
  git tag v0.1.0 && git push origin v0.1.0
  ```
  `.github/workflows/release.yml` 会先跑静态检查，再由 macOS / Windows 两条腿分别打包
  （dmg / NSIS 安装器），最后创建 Release 并附上产物；变更说明由 GitHub 按提交自动生成。
  - 版本号取自 tag（去掉 `v` 前缀），写进 `wails.json` 的 `info.productVersion` 后
    由既有链路派生到产物与应用内——与本地发布共用 `scripts/set-version.sh`
  - **打 tag 前先把版本号提交进仓库**（`bash scripts/set-version.sh 1.0.0` 并提交），
    否则仓库状态停留在旧版本、本地构建的产物与已发布的版本对不上
  - **母版仓库本身也能打 tag**：检测到占位符就跳过打包，Release 仍会创建（只含源码归档）
- 版本号管理：`scripts/release.sh <version>`（本地发布：写版本 + 三平台打包 + release 说明，
  适合无网/离线场景；产物需自行上传）。

## 已知限制

- **托盘失败时关闭即退出**：三平台（含 macOS）关闭窗口 = 隐藏到托盘，进程驻留；经托盘菜单
  「显示」、双击托盘图标、macOS 点 Dock 图标或再次启动应用（单实例唤起）恢复。若系统托盘
  **未能启动**，关闭按钮则真正关闭并退出应用——不会留下看不见也点不到的无窗口进程。
  （v2 时代 macOS 无托盘、关闭即退出；v3 起 macOS 有真托盘，该限制已消除。）
- **无 headless 模式**：冒烟测试定位为本地验证，CI 只编译不启动 GUI。
- **Linux 产物编译不在 CI 验证范围内**：Wails 在 Linux 上按 **webkit2gtk 版本**做 cgo 链接
  ——默认找 `webkit2gtk-4.0`，而 Ubuntu 24.04+ / Debian 13+ 只提供 4.1，必须带
  `-tags webkit2_41` 才切得过去。这是「取决于 runner 镜像装了什么包」的环境耦合，
  维护成本高于收益，故编译矩阵只保留 macOS / Windows。
  `make package os=linux` 仍可用（脚本已带该标签，但本地需自行确认），**无 CI 背书**；
  Linux 的 Go 层行为（测试/护栏/生成器端到端）仍由 CI 的 ubuntu 静态检查腿覆盖。
- **窗口位置不持久化**：只记住尺寸与最大化。Wails v2 没有创建期位置选项，运行期设置与
  窗口显示存在竞态（会看到跳动），且需屏幕边界夹取，否则窗口可能还原到已拔掉的显示器上
  （详见 `openspec/changes/runtime-pipeline/design.md` D1）。
- **版本号只认数字点分格式（如 `0.1.0`）**：Windows 的 NSIS `VIProductVersion` 不接受
  非数字版本（如 `v1.2.3` 的 `v` 前缀、git 短哈希），`build.sh` / `release.sh` 会提前拒绝。
  追溯性由构建期注入的提交号承担（`GetAppInfo().commit` 与日志）。
- **裸 `go build -tags production` 在 macOS 上链接失败**：Wails 需要 `CGO_LDFLAGS` 注入
  `-framework UniformTypeIdentifiers`，`wails build` 会自动加而裸 `go build` 不会。
  这是 Wails 的既有行为，不是本仓问题；用 `make build` / `make package` 即可。
- **dmg 挂载卷不在 Finder 侧栏**：Finder 默认不显示已挂载的卷，且脚本无法替用户改 Finder 偏好。
  用户误关 dmg 窗口后，再次双击 `.dmg` 即可重新打开挂载卷（不会重复挂载）。
