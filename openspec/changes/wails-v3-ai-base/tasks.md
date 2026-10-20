# wails-v3-ai-base — 实施任务

> 分阶段，每阶段有**独立可验的出口**。不合并「编译过」与「实跑过」两道关卡。
>
> 进度：阶段 1–5 完成（`go build` / `make lint` / `make test` 全绿）。护栏共 22 项，
> 每一项新增或改动时都做过破坏性验证（改坏 → 确认变红 → 还原），逐条见各阶段条目。
> 阶段 5 的出口已在**实例化副本**上端到端实跑通过，见文末「验证记录」。阶段 6–7 未开始。
> 实施中新挖出的坑记在 design.md 的「实施阶段追加的结论」。

## 0. 准备

- [x] 0.1 切分支 `feat/wails-v3-ai-base`（`main` 不动，回退 = 不合并）
- [x] 0.2 装 CLI：`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.23` → `wails3 version` = `v3.0.0-beta.23`
- [x] 0.3 拉源码核对 API：`wails/v3@v3.0.0-beta.23` 已进模块缓存，逐字段核对
- [x] 0.4 **实跑确认 6 项**（结论见 `design.md` 的「实跑确认结果」）
      —— `wails3 init` 生成真实项目 + 写 spike 编译通过 + 真跑应用并用 MCP 客户端接入
- [x] 0.5 spike 编译验证：`/tmp/v3probe` 用上全部 6 项结论，`go build` 通过（产物 15MB）
- [x] 0.6 真跑验证：MCP 服务启动、握手成功、列出 16 个工具、`emit_event` 闭环到达 Go 侧
- [x] 0.7 **P5 已确认**：真实前端下 `call_bound_method` 返回 `Hello Wails!`、`js_eval` 读到真实 DOM、
      `dom_query` 返回真实坐标。此前 spike 超时是空壳页面缺 Wails 运行时所致
- [x] 0.8 **CLI MCP 已确认**：`wails3 mcp --stdio` 起来后 root 限定项目目录、随机 token 走 stderr、
      暴露 10 个工具（build / dev_start / doctor / generate_bindings / init / inspect / task_list / task_run / job_status / job_stop）

## 0b. 实跑挖出的坑（原计划没有，必须处理）

- [x] 0b.1 **P1 对话框语义**：`MessageDialog` v3 无同步返回，`Confirm` 改用 channel 桥接 + 非链式 `AddButton`
      （已落进 `internal/dialog/dialog.go`；额外发现 `SaveFileDialogStruct` 漏了 `SetTitle`，见 design.md P8）
- [x] 0b.2 **P2 托盘空图标**：macOS 传空图标**panic**（非 error）→ `placeholderIcon()` 兜底保留（`internal/tray/tray.go`）
      **护栏刻意不加**：这条失效的表现是【启动即 panic】——有症状、且够响。护栏的意义在于拦「没有任何症状」
      的失效（见铁律 5）；给一个会崩的事情再加一层断言，是拿护栏预算换心安。
- [x] 0b.3 **P3 绑定数字寻址**：`Call.ByID(<hash>)`，改方法名/包路径即失效
      ——**比原计划做得更狠**：ID 是 FNV-1a 32 可复算的（实测 11/11 命中），故护栏不只提示「重新生成」，
      而是复算 ID 逐条对账。见 design.md P7 与 `bindings_test.go`
- [x] 0b.4 **P4 MCP 剥离可量化**：做成**两层**，因为「查编译单元」与「查产物」的分辨率不同
      ——**护栏 17**（`TestMCPNotCompiledIntoDefaultBuild`，`make test` 里跑，0.09s）用**真实的
      生产标签**去问 Go「wails 的 application 包有哪些编译单元」：默认下必须只有 `mcp_disabled.go`、
      不得出现任何 `mcp_*_enabled.go`；**并带正控**——加 `mcp` 标签必须能看到 `mcp_enabled.go`，
      否则「没看到」可能只是名字查错了，即一个永远为真的假承诺。
      **产物层**放在 `scripts/smoke.sh` 第 6 步（那里已付了构建成本）：`strings` 扫产物，
      断言 5 个 `WAILS_MCP_*` 常量命中数为 0。
      **为什么不用 `go tool nm` 查符号**：生产构建带 `-ldflags="-w -s"`，Go 符号表已被剥离
      （实测 `go tool nm <产物> | grep -c 'main\.main'` = 0）；且 nm 里的 `_memcpy` 含 "mcp"、
      strings 里的 `xdmcp` / `asf-rmcp` / 内嵌 base64 图片数据也都含 "mcp"——松散子串匹配会
      得到一堆假阳性，故必须用完整的 `WAILS_MCP_*` 名字。见 design.md P13
- [x] 0b.5 **v3 最低系统要求**：查清了，**不是可随手调的模板默认值，而是框架的编译目标下限**，
      且写在两处：`build/darwin/Taskfile.yml` 的 `CGO_CFLAGS`/`CGO_LDFLAGS`
      （`-mmacosx-version-min=12.0`）与 `MACOSX_DEPLOYMENT_TARGET: "12.0"`，
      以及 `build/darwin/Info.plist` 的 `LSMinimumSystemVersion`。上游自己就用测试钉死了
      12.0.0，还专门断言不许出现 10.15（`internal/commands/darwin_version_test.go`）。
      **实测三方一致**：产物 `otool -l` 的 `LC_BUILD_VERSION minos 12.0`。
      **Info.plist 改不动**——它是 `common:update:build-assets` 的渲染产物，手改会被覆盖；
      真要调下限必须动 Taskfile 的 CGO 标志。新增**护栏 18** 断言两处一致
      （漂移的坏方向是 Info 低于二进制：macOS 按 Info 放行、在用户旧系统上崩溃，而 CI 全绿）。
      README 的系统要求说明留给 7.4（散文措辞不做断言，会因改写误红）


## 1. 依赖与配置骨架

- [x] 1.1 `go.mod`：`wails/v2 v2.15.0` → `wails/v3 v3.0.0-beta.23`（钉死，不用 `@latest`）；删 `energye/systray`
- [x] 1.2 `deps.yaml`：同步两条改动并改理由（wails 理由补「MCP 能力」；删 systray 条目）
- [x] 1.3 **`wails.json` → `build/config.yml`**（v3 无 wails.json）
      版本字段 `info.productVersion` → `info.version`，且是 YAML 带引号
- [x] 1.4 新增 `Taskfile.yml`（根）+ `build/Taskfile.yml`（wails3 构建体系入口）
- [x] 1.5 `frontend/package.json`：加 `@wailsio/runtime`；确认 tsconfig 接受绑定产物的 `.js` 扩展名 import
      ——**已实测**：`moduleResolution: "Node"` 够用，`vue-tsc --listFiles` 确认 5 个 bindings 文件都进了程序，tsconfig 不用改

## 2. Go 骨架迁移（编译可验）

- [x] 2.1 `main.go`：`wails.Run` → `application.New` + `Window.NewWithOptions` + `Run` 三段
      （保留 bootTimer 三段打点，护栏 `TestStartupTimelineStagesExist` 要认）
- [x] 2.2 `main.go`：`Logger: logging.WailsAdapter{}` → `Logger: <*slog.Logger>`；`LogLevel` → `slog.Level`
      **保留接线**——v3 默认 logger 同样只写 stdout（D4）
- [x] 2.3 `main.go`：`ErrorFormatter` → `MarshalError`；`SingleInstanceLock` → `SingleInstance`（`UniqueId`→`UniqueID`）
- [x] 2.4 `main.go`：几何/无边框/背景色/平台选项迁进 `WebviewWindowOptions`；`HideWindowOnClose` 换成窗口事件拦截
- [x] 2.5 `app.go`：删 `ctx` 字段，改持 `*application.App`；`startup`→`ServiceStartup`、`shutdown`→`ServiceShutdown`
- [x] 2.6 `app.go`：6 处 `runtime.*` 改对象方法（对照 design.md D6 表）
- [x] 2.7 `app.go`：删 `notifySecondInstance` 里「ctx 未就绪」的防御分支（v3 下不可能发生）
- [x] 2.8 `internal/apperr`：`Format(error) any` → `MarshalError(error) []byte`；**重写包注释的理由段**（结论不变、理由变了）
- [x] 2.9 `internal/logging/wails.go`：整文件删除（含其测试）
- [x] 2.10 `internal/dialog`：改持有 `*application.App`，`runtime.*Dialog` → `app.Dialog.*`；
      **`Confirm` 必须用 channel 桥接**（v3 对话框无同步返回，见 design.md P1）
- [x] 2.11 `internal/tray/*`：删 `start_darwin.go` 的 no-op 与 `start_other.go` 的 energye 实现，改 `app.SystemTray`；
      **`placeholderIcon()` 兜底必须保留**——macOS 传空图标会 panic（见 design.md P2）
- [x] 2.12 **出口**：`go build ./...` 通过

## 3. 前端契约层

> 原以为「只动 4 个 lib 文件」。实测改动点更多（5 个文件 import 了 wailsjs），
> 但契约层收口的结论仍成立：`stores/` 与 `views/` 只改 import 行。见 design.md D8 的更正。

- [x] 3.1 生成绑定：`wails3 generate bindings` → 确认 `frontend/bindings/` 产出（2 服务 / 18 方法）
- [x] 3.2 `lib/invoke.ts`：**代码零改动**（它是泛型包装，不 import 绑定）；只按 D5 重写注释里的理由段
- [x] 3.3 `lib/event.ts`：`wailsjs/runtime` → `@wailsio/runtime` 的 `Events`；
      载荷从「摊平参数」改成 `ev.data`（WailsEvent 对象）；删掉永远不会触发的假 catch（见 design.md D12）
- [x] 3.4 `lib/log.ts`：日志出口改 **`pkg/services/log` 服务**（v3 没有 runtime 日志函数了，见 design.md D11）
- [x] 3.5 `lib/app.ts`：**代码零改动**（它走 `onEvent`，实际改动落在 3.3）
- [x] 3.6 删 `frontend/wailsjs/`（v3 不再生成该目录）
- [x] 3.7 **出口**：`vue-tsc --noEmit` + ESLint + `npm run build` 全过
- [x] 3.8 额外：`stores/app.ts` / `views/Settings.vue` 改绑定 import 路径；`components/TitleBar.vue` 改窗口 API（同步→异步）
- [x] 3.9 额外：`eslint.config.js` 的 ignores 加 `bindings/**`（生成物不入 lint）

## 4. 护栏同步（漏了会 Fatal，不会静默）

- [x] 4.1 `wiring_test.go`：解析目标 `options.App` → `application.Options`；字段断言 `Logger` **保留**，并加 `MarshalError`
- [x] 4.2 `wiring_test.go`：新增「`mcp` 标签不得进默认构建」断言（查 Makefile / Taskfile / scripts / CI）
- [x] 4.3 `bindings_test.go`：绑定产物路径 → `frontend/bindings/<module>/app.ts`；
      并升级为 **FNV 复算 ID 对账**（见 design.md P7）
- [x] 4.4 `layer_test.go` / `parity_test.go`：**不动**（与框架无关，实测确认）
- [x] 4.5 破坏性验证：7 项全跑过，确认护栏**会红**——
      删 Logger / 把 `NewWithConfig` 换成 `New()` / 加 `-tags mcp` / 方法改名 / 篡改产物 ID /
      改 config.yml 不渲染 / 把 bundle id 换成模板默认值
- [x] 4.6 **出口**：`make lint` 全绿，且 4.5 的破坏性验证确实变红
- [x] 4.7 额外：`internal/logging` 加 `Frontend()`（补回 v2 `source=wails` 的可区分性）
- [x] 4.8 额外：`Makefile` 切 wails3 CLI，并加独立的 `dev-mcp` target（`WAILS_MCP=1` —— MCP 唯一受认可的启用通道，护栏会核对它存在）

## 5. 构建与交付链

- [x] 5.1 `Makefile`：`WAILS := $(GOPATH)/bin/wails3`；`dev` 切 wails3（`dev-mcp` 见 4.8）
- [x] 5.2 `scripts/build.sh`：CLI 切 wails3；**新增「跑 `wails3 task common:update:build-assets`」这一步**
      （v2 是自动渲染，v3 必须显式；漏了就回到「应用内新版本、系统旧版本」的双源漂移）
      版本读取改自 `build/config.yml` 的 `info.version`（YAML，容忍引号）
      ——**实测确认 `wails3 build` 不转发 `-ldflags`**（只转发 `EXTRA_TAGS`/`OBFUSCATED`/`GARBLE_ARGS`/
      `GOOS`/`ARCH`，见 v3 的 `internal/commands/task_wrapper.go` 与 `flags/package.go`）。
      故版本只能走 Taskfile 变量：`scripts/build.sh` → `wails3 build VERSION=x COMMIT=y`
      → 根 `Taskfile.yml` 的 `VERSION_LDFLAGS` → 各平台 `BUILD_FLAGS`。
      另：darwin 上交付物是 `.app`（v2 的 `-o` 裸二进制已不存在），故 darwin 的交付动作是 `package` 而非 `build`。
      配置读取（版本 / productName / plist 回读）抽进 `scripts/lib/wails3-common.sh`——
      否则 BUILD/PACKAGE/RELEASE/SMOKE 四个脚本各解析一遍 `config.yml`，同一规则四份实现。
- [x] 5.3 `scripts/package.sh` / `release.sh` / `smoke.sh`：切 wails3
      （三条路径一律经 `build.sh` 走，版本注入只剩一个入口；dmg 仍由 package.sh 的
      hdiutil + osascript 封装，理由见 `build/darwin/Taskfile.yml` 头部注释）
- [x] 5.4 `scripts/gen.sh` / `verify-gen.sh`：模块生成器锚点适配新架构（`gen:import` / `gen:bind` 锚点要仍然有效）
      ——**注意：这两条出口是本次才真正跑通的**。5.4 此前只改了 verify-gen.sh 的替换逻辑，
      `gen.sh` 本身自 09-16 起就没动过，而它生成的 Vue 模板仍写着 v2 的绑定导入
      `'../../../wailsjs/go/main/App'`（v2 目录已被删除）→ `wails3 build` 的 vue-tsc 阶段必报 TS2307。
      之所以没人发现：verify-gen.sh 第 3 步把 `wails3 build` 的输出丢进了 `/dev/null`，
      把一次明确的 TS2307 压成一句「生成物或绑定生成有问题」，排查方向被误导到 bindings 生成。
      两处都已修（模板导入路径改 v3；verify-gen.sh 失败时打印原始输出末 40 行）。详见 5.10
- [x] 5.5 `scripts/init.sh` / `set-version.sh`：版本身份与占位符路径同步
      （`build/config.yml` 与 `frontend/bindings/my-notice/` 都带占位符/模块名，
      替换清单与残留断言都已覆盖；`set-version.sh` 用 awk 只改 `info:` 段内的 `version:`，
      并断言恰好替换 1 处——退回 sed 会误伤顶层 `version: '3'`）
      **决定**：`frontend/bindings/` 进 `.gitignore`。它是 `common:generate:bindings` 的产物
      （每次 `build:frontend` 都重跑），入库会造出无症状漂移：改了 Go 绑定签名却忘了重新生成，
      vue-tsc 会拿【旧】的 `.d.ts` 校验前端——类型检查照过，运行期才炸。
- [x] 5.6 `.github/workflows/build.yml` / `release.yml`：CI 装 wails3 CLI
      （版本从 `go.mod` 读 `go list -m -f '{{.Version}}' ...`，不写死也不 `@latest`；
      release.yml 的产物收集改用 `bc_read_info productName`，不再 `node -p require('./wails.json')`）
- [x] 5.7 **出口**：`make build` 产出可运行 `.app`；`make package` 出 dmg
      ——**已在【实例化副本】上端到端实跑通过**（在母版上跑会被占位符防呆拦下，这是刻意的：
      母版不是可发布项目。验证方式见本文件末尾的「验证记录」）
- [x] 5.8 **实施中新挖出的坑**：`build` 任务必须认 `OUTPUT` 变量
      （v3 迁移照抄平台 Taskfile 时把 `-o` 写死成 `BIN_DIR/APP_NAME`、顺手丢了 `OUTPUT`。
      后果：`build:universal` 给两次 `build` 传的 `-amd64`/`-arm64` 被忽略，两次编译覆盖同一个文件，
      `lipo` 报 `can't open input file`；linux/windows 没有 universal 调用者，则是**完全静默**的
      「传了 OUTPUT 却落在别处」。已按上游模板改成 `OUTPUT: '{{.OUTPUT | default .DEFAULT_OUTPUT}}'`，
      三个平台一并改齐，并由**护栏 14** 覆盖——见 design.md P10）
- [x] 5.9 **实施中新挖出的坑**：`go build -o` 拒绝覆盖「不是它自己产出」的既有文件
      （报 `build output "…" already exists and is not an object file`；**本机 go1.26.2 实测**，
      go1.25 未验证——CI 固定 1.25，故别把它读成「1.26 才引入」）。darwin 的交付路径走
      `package:universal`，`lipo` 把合并结果写到 `build/bin/<APP_NAME>`——它正好不被认作 Go 产出，
      于是**先 `make package` 再 `make build` 必炸**，`make smoke` 同样（它内部先构建）。
      三个平台的 `build` 任务已加 `rm -f "{{.OUTPUT}}"`。见 design.md P11
      （**未加护栏**：这条失效会出声，不符合铁律 5 的适用面；验证方式是让构建顺序真的这么跑）
- [x] 5.10 **实施中新挖出的坑**：生成的前端页面与绑定类型对不上（同一处漏改的第二层）
      模板导入路径修好后，露出真正的不兼容：`lib/page.ts` 的 `PageResult<T>.list` 写死为 `T[]`，
      而绑定生成的 `Result<T>.list` 是 `T[] | null`（Go 的 `[]T` 可 nil，生成器无从知道运行期保证）
      → TS2322 在生成页面上必报。**处置**：把镜像改成如实的 `T[] | null`，null 归一化只做一次
      （`usePagedList` 本就有的 `res.list ?? []`），其返回类型仍是干净的 `T[]`。
      见 design.md D14。**规则**：镜像只许复制类型，不许替线上类型作更强的断言——
      写死成 `T[]` 是在类型层面撒谎，且这条谎言不会报错
- [x] 5.11 **实施中新挖出的坑**：dev 态页面纯空白——dev server 绑 IPv6、代理强制 IPv4
      （Vite 的 `server.host` 默认 `'localhost'`，在 macOS 上只绑 `[::1]`；而 wails3 的 dev 代理
      在 `internal/assetserver/build_dev.go` 里硬编码 `dialer.DialContext(ctx, "tcp4", addr)`
      → 代理必然 `connection refused` → 回 webview 502 → 空白页。
      **两端都在报成功**：`wails3 dev` 退出码 0，终端还打印
      `Connected to frontend dev server!`（健康检查走 Go 默认 client，会先试 `::1` 且成功）。
      唯一线索是 `app.log` 里一行 502，而它写的是 `localhost`，看不出协议族。
      上游模板与全部 example 都不设 `server.host`，是 beta.23 + Vite 7 的缺口。
      **处置**：`frontend/vite.config.ts` 显式 `server.host: '127.0.0.1'`；由**护栏 15** 覆盖——
      见 design.md P12。`frontend/bindings/` 入库策略与 dev 链路的验证入口见 design.md D15

## 6. AI 基座接线（本次的真正目的）

- [x] 6.1 `make dev-mcp` 目标：`WAILS_MCP=1 wails3 dev`（**独立 target，不污染默认 `dev`**）
- [x] 6.2 实跑：应用日志出现 `MCP server started ... url=http://127.0.0.1:9099/mcp`
      —— 2026-09-20 通过，证据见「阶段 6」。**踩坑（重要）**：首次实跑「失败」，
      根因不是代码——上一轮 `make dev` 结束后 `wails3 dev` 的 watcher 仍然存活
      （app 退出不带走 watcher，它自己会打印「watcher is still running」），
      残留链路占着 9245/9099，且在被中途清掉的环境里重启会出现
      「日志停在 `AssetServer Info`，`MCP server started` 永不出现、`preRun` 的
      `Waiting for frontend dev server...` 一行都没有」这种**无报错的假故障**。
      结论：冷启动前必须先确认无残留（与 8.5 同一问题的运维面）。
- [x] 6.3 接 MCP 客户端：`claude mcp add --transport http wails-app-dev http://127.0.0.1:9099/mcp`
      —— 已注册（**local scope**，写入 `~/.claude.json` 的本项目节；移除：
      `claude mcp remove wails-app-dev`）。协议层另用原生 JSON-RPC 客户端独立验过
      （initialize / tools/list / tools/call 全通），不把验收绑死在某个宿主上。
- [x] 6.4 **端到端验收**：让 Agent 做一次真实操作——「点开设置页，切主题，验证事件触发」
      —— 已按**真实鼠标点击 + 真实二次启动**跑通，四重校验 + 事件载荷断言，见「阶段 6」。
      注意与原始措辞的偏差：**切主题并不发事件**（`SetTheme` 落盘即可，事件只有
      `app:second-instance` 一条）。故拆成两段断言：主题走「DOM/绑定/磁盘」三重校验，
      事件另走真实的二次启动路径。
- [x] 6.5 确认默认构建无 MCP：`make build` 后 `lsof -i :9099` 无监听、二进制中无 MCP 符号
      —— 已由 0b.4 两层落地（护栏 17 机制层 + `smoke.sh` 第 6 步产物层，后者带
      「判据来源」与「扫描方法」双重正控），阶段 6 另做了一次直测，见「阶段 6」。
- [x] 6.6 CLI MCP 试用：`wails3 mcp` 接 host，验证 `task_run` / `generate_bindings` 可用
      —— 已实测通过（含 `wails_job_status` 轮询与 token 强制）。**发现**：
      `wails_project_task_list` 在任何 Wails 模板工程上恒返回空列表——它内部调
      `wails3 task --list --json`，而 go-task 的 `--list` 只列有 `desc:` 的任务，
      模板体系全用 `summary:`（需 `--list-all` 才全量）。见 design P16。

## 7. 文档与规格

- [ ] 7.1 `docs/map.md`：更新 Go/前端文件表；补 MCP 启用方式；写明「换框架不动业务代码」这条契约层价值
- [ ] 7.2 `AGENTS.md`：新增铁律「`mcp` 标签不得进默认构建」；更新 Wails 版本与验证入口
- [ ] 7.3 `CLAUDE.md` + `frontend/CLAUDE.md`：更新域内规范（Service 形态、绑定路径、日志服务）
- [ ] 7.4 `README.md`：环境准备（wails3 CLI）、MCP 用法、v3 差异、**macOS 12.0+ 最低要求**（见 0b.5）
- [ ] 7.5 openspec 规格：把 proposal 里标 Modified 的 9 个能力逐条落进 `openspec/specs/`
- [ ] 7.6 归档本变更到 `openspec/changes/archive/2026-09-20-wails-v3-ai-base/`

## 8. 未决 / 需决策

- [ ] 8.1 v3 的 `App` 是否要暴露 `DevToolsEnabled`（开发态开、生产态关）——需定一条与 `debugBuild` 一致的策略
      （**注**：已按 `DevToolsEnabled: debugBuild` 落地，策略与 `buildmode_*.go` 同源；若要改，改 `main.go` 一处）
- [x] 8.2 **macOS 有了托盘后，「关闭窗口 = 隐藏」是否也用于 macOS**（行为变更，需产品决策）
      ——**已落地**：三平台统一「关闭 = 隐藏到托盘」，是否隐藏由**运行期**判据
      「托盘是否真的起来了」（`App.trayRunning`）决定；托盘缺失时关闭真正退出、不留无窗口进程。
      结论与依据见 design.md D13，实跑记录见文末「验证记录 · 阶段 8」。
- [ ] 8.3 是否把「Agent 驱动验收」固化成 `make verify-ui` 之类的一级入口
      （**注**：阶段 6 已把这条流程完整跑通一遍，可直接拿来做雏形——
      起 `dev-mcp` → 轮询 `9099` → 拿 16 个工具做「真实鼠标 + 断言 DOM/绑定/磁盘/事件」→
      停任务 → 杀进程 → 确认端口释放。见「验证记录 · 阶段 6」。
      真要做，**先解决 8.5 的退出问题**：入口不能依赖"人记得 pkill"）
- [ ] 8.4 **是否把阶段 5 的出口做成可重复的 `make verify-package`**（与 `verify-gen` 对称）
      ——现在的困境：`verify-gen` 覆盖了生成器，但**打包路径没有任何可重复的验证入口**，
      本次的 P10/P11 两个坑正是藏在这个空白里（都必须真跑一次「先 package 再 build」才会露头）。
      目前的做法是「按 tasks.md 的验证记录手工重放」——即依赖人记得。
      **未直接动手的原因**：sandbox 实例化逻辑已经在 `init.sh` 与 `verify-gen.sh` 里存在两份
      （verify-gen 自称「init.sh 的忠实预演」），再加一份就是第三份。要加就先把它抽进
      `scripts/lib/`，那是比「补一个脚本」更大的改动，值得单独决策。
- [ ] 8.5 **是否把 dev 链路做成可重复的验证入口**（`make verify-dev`）
      ——`make smoke` 验的是**构建产物**（走 `package` 路径、不设 `FRONTEND_DEVSERVER_URL`），
      它无论如何都覆盖不到 dev 代理这条分支；P12 正是藏在这个空白里，本次是靠手写脚本读
      `app.log` 才定位的（见「验证记录」）。
      要固化，前提是能把「起 dev → 轮询端口 → 读 app.log 断言 `code=200` → 干净退出」做成
      无人值守可退出的流程——现在 `wails3 dev` 是前台常驻进程，退出依赖信号。
      与 8.4 是同一类（把「靠人记得重放」变成入口），可一并决策
      ——**阶段 6 补充了两个必须写进该入口的前提（design P18）**：
      (1) **冷启动前**要断言无残留（`9099`/`9245` 无 LISTEN，且无 `wails3 dev` 残留），
      否则会得到「日志停在 `AssetServer Info`」这种无报错的假故障；
      (2) **退出时**要先停掉承载 `wails3 dev` 的任务/进程组再杀子进程——
      `wails3 dev` 被杀不会带走 task runner / app / vite，它们会成孤儿继续占端口；
      而挂在"仍在运行的后台任务"下的进程，`kill -9` 是**无效的**（返回 0 但不生效）。
      也就是说这个入口的难点不在"启动"，而在**两端各有一道进程卫生检查**

## 验证记录

阶段出口的验证都在**实例化副本**上做，不在母版上做——母版带 `my-notice` 占位符，
任何打包路径都会被 `package.sh` 的防呆拦下（`错误：检测到占位符残留`），这是刻意的：
母版不是可发布项目，也不是可运行项目。

实例化的重放方式（两步都要，缺一不可）：

1. `git clone` 母版到临时目录 → 把**工作区**覆盖上去（带上未提交的改动）；
2. `rsync` **必须加 `--delete`**。不带的话，工作区里已删除的文件会被 `git clone` 出来的
   HEAD 版本残留——曾因此让副本里多出 v2 遗留的 `internal/tray/start_darwin.go`，
   与新的 `tray.go` 撞 `Start` 重名，把一次真实打包验证带偏成假故障。

### 阶段 5（构建与交付链）——2026-09-20

命令按**修复前会失败的顺序**执行（`make package` 先跑，以产出 `lipo` 的 universal 产物）：

| 命令 | 结果 | 说明 |
| --- | --- | --- |
| `make package os=macos` | exit 0 | 产出 universal `.app` + 打磨后的 `.dmg` |
| `make build` | exit 0 | 修复前必炸（design.md P11） |
| `make smoke` | exit 0 | 修复前必炸（同一个 P11）。断言：进程存活 pid、config.json 与 db 落盘、`app.log` 里有 `version=1.0.0` |
| `make build`（第二次） | exit 0 | 幂等 |
| `make test` | exit 0 | 护栏共 23 项，全绿 |
| `make lint` | exit 0 | gofmt + 护栏 + `go vet` + ESLint + `vue-tsc` |
| `make verify-gen` | exit 0 | 6/6 步；修复前必炸（5.4 / 5.10） |
| `make dev` | — | 见下方「阶段 5 补充：dev 链路」；修复前必炸（P12） |

产物侧的独立核对（不信脚本的自述，直接查产物）：

- `lipo -archs build/bin/<APP>.app/Contents/MacOS/<APP>` → `x86_64 arm64`
- `PlistBuddy -c 'Print :CFBundleShortVersionString'`（`.app` 内与 dmg 内各一次）→ `1.0.0`
- 产物内 `grep -aF 1.0.0` 命中 → 证明 `-ldflags` 注入链真的走到了二进制（design.md P9）
- `hdiutil verify` → `checksum ... is VALID`；挂载后含 `Applications` 软链 + `<APP>.app`
- dmg 内 `.app` 的 `CFBundleIdentifier` → `com.sandbox.<项目名>`，即 init.sh 的 bundle-prefix 链（5.5）

护栏侧的破坏性验证（**护栏 14**，本次新增）：三种回归形态都会红——
① darwin 把 `-o` 写死且丢掉 `OUTPUT`；② linux 同样；③ 把 `build` 任务改名（断言护栏
「感知自己瞎了」：报「找不到任务」而不是静默放行）。还原后全绿，且三份 Taskfile 与工作区逐字节一致。

**环境限制**：dmg 的 Finder 布局美化（图标锚点对齐）需要 GUI 会话，在无头 shell 下
`osascript` 驱动 Finder 会失败，`package.sh` 会打印
`提示：Finder 布局设置失败，dmg 仍可用（无美化）` 并**以 exit 0 继续**——
这是设计好的降级路径，不是失败。要验证美化效果需在真实桌面会话下重跑。

**已知未覆盖**：`make package os=windows` / `os=linux` 未在本机验证（需要对应平台：
Windows 的 NSIS 与 cgo=0 的产物、Linux 的 WebKitGTK）。这两条路径的正确性目前只有
静态保证（护栏 12/13/14 + `build.sh` 的 GOOS/ARCH 变量传递），留待 CI 覆盖。
另：CI 的 Go 固定 1.25，本机是 1.26.2，design.md P11 那条行为**未在 1.25 上验证**。

### 阶段 5 补充：dev 链路 —— 2026-09-20

`make dev` 修好 `server.host`（design.md P12）前后的对照证据。判据不看脚本自述，
直接读 `app.log` 与 `lsof`：

| 观测点 | 修复前 | 修复后 |
| --- | --- | --- |
| `lsof -iTCP:9245 -sTCP:LISTEN` | `TCP [::1]:9245`（IPv6 回环） | `TCP 127.0.0.1:9245`（IPv4 回环） |
| `app.log` 的 `/` 请求 | `code=502`（**唯一**一条 Asset Request） | `code=200`，且随后 20+ 条 `code=200` |
| `app.log` 的代理错误 | 每次 `[ExternalAssetHandler] Proxy error ... connection refused` | 一条都没有 |
| 页面 | 纯空白 | 依赖图全部加载（`/src/main.ts`、`/bindings/**`、`/src/layouts/AppShell.vue`、`/src/views/Home.vue`） |

复现要点（这条链没有一级入口，8.5 记了）：

- 冷启动到 dev server 就绪约 **30s**（其中 `wails3 generate bindings` 单项实测 14.9s），
  观测窗口给 24s 会误判成「dev server 起不来」——**先给足 60s 再下结论**。
- 轮询端口必须区分协议族：`lsof` 会显示 `[::1]` 或 `127.0.0.1`，只看「有没有监听」会把
  故障当成正常。
- `curl -4 http://127.0.0.1:9245/` 在本机 shell 下会返回 **502**，那是 shell 的
  `HTTP_PROXY=http://127.0.0.1:58626` 应答的假象，**不是** 9245 上真有东西在听；
  要拿真值必须 `curl -4 --noproxy '*'`（那样才会得到连接失败）。

`wails3 dev` 是前台常驻进程，本次验证靠后台起 + 定时取样 + `pkill` 收尾，不是可重复的入口。

### 阶段 6（AI 基座接线）——2026-09-20

**6.2 实跑 `make dev-mcp`**：冷启动前先确认无残留（见 design P18），`app.log` 里依次出现——

```
level=INFO msg="Waiting for frontend dev server to start..." url=http://localhost:9245
level=INFO msg="Connected to frontend dev server!"
level=INFO msg="MCP server started. Connect MCP clients using the streamable HTTP transport." url=http://127.0.0.1:9099/mcp
```

`lsof -iTCP:9099 -sTCP:LISTEN` → 应用进程；`curl http://127.0.0.1:9099/` →
`{"endpoint":"http://127.0.0.1:9099/mcp","service":"wails-mcp","tools":16}`。
**同一流程跑了两遍，结论一致**（第二遍 pid 不同、行为相同）。

**6.4 端到端验收**（真实鼠标 + 真实事件，不是调 JS 假装点）：

协议客户端用原生 JSON-RPC（`POST /mcp`，`initialize` → `tools/list` → `tools/call`；应用内 MCP 无鉴权、无状态）。
16 个工具：`app_info`、`windows_list`、`window_control`、`js_eval`、`dom_html`、`dom_query`、
`mouse_move`、`mouse_click`、`mouse_drag`、`mouse_scroll`、`keyboard_type`、`keyboard_press`、
`call_bound_method`、`emit_event`、`wait_for_event`、`screenshot_dom`。

「点开设置页，切主题」——全部走真实鼠标（`mouse_click` 带 CSS 选择器或视口坐标）：

| 步骤 | 断言 | 实测 |
| --- | --- | --- |
| 点「仪表盘」 | `location.hash` | `#/`，`.page__title` = 仪表盘 |
| 点「设置」 | `location.hash` + `app.log` | `#/settings`，标题「设置」，Go 侧出现 `Binding call complete: method=main.App.GetAppInfo`（`Settings.vue` 的 `onMounted` 真的跑了） |
| 坐标点「暗色」 | DOM 属性 | `documentElement[data-theme]` = `dark` |
| | active 类归属 | `.setting__btn--active` 从「亮色」移到「暗色」 |
| | Go 侧回读 | `call_bound_method main.App.GetTheme` → `dark` |
| | 磁盘落盘 | `config.json` 的 `"theme"` 由 `light` 变 `dark` |

**「验证事件触发」的偏差说明**：切主题**不发事件**（`SetTheme` 落盘即完成），全仓只有
`app:second-instance` 一条真实事件（`app.go:247`）。故事件单独验，且用**真实路径**而不是
`emit_event` 造一条：

| 步骤 | 断言 | 实测 |
| --- | --- | --- |
| 页面内 `window.wails.Events.On('app:second-instance', …)` 挂监听 | — | 挂上（运行时全局确实叫 `window.wails`，`Events` 在其下） |
| 直接跑第二个实例（带 `--second-instance-probe hello`） | 单实例锁 | 第二实例走到 `AssetServer Info` 后退出，**没抢 9099** |
| | 主实例日志 | `检测到二次启动，唤起已有窗口 args="[<exe 路径> --second-instance-probe hello]"` |
| | 前端收到的载荷 | `{name:'app:second-instance', data:{args:[…真实参数…], working_dir:'/tmp'}}` |

即 `第二进程 → 单实例锁 → OnSecondInstanceLaunch → notifySecondInstance → event.Name → 事件总线 → 前端 ev.data` 整条闭合，
且**验证了 `lib/event.ts` 里记的 v3 载荷形状**（回调收到 `WailsEvent` 对象、载荷在 `ev.data`，不是 v2 的摊平参数）。

收尾：把测试中改过的主题用真实点击改回 `light`，`config.json` 确认复原。

**6.5 默认构建无 MCP**（在 `make build` 产物上直测，与 smoke 第 6 步互为独立证据）：

| 检查 | 结果 |
| --- | --- |
| 正控：`strings` 能否读出注入版本号 | `1.0.0` 命中 25 行（扫描方法本身有效） |
| `WAILS_MCP_HOST/TOKEN/PORT/TIMEOUT/HIDE_CURSOR` 计数 | 全 `0` |
| 松散匹配 `mcp` 的假阳性 | `xdmcp`×2、`asf-rmcp`、`Xmemcpy` —— 现场复现了 smoke 注释里写的"不能用子串" |
| 实跑产物进程存活 | ✓，启动行 `version=1.0.0`、**日志级别 Info**（无 DEBUG 泄漏） |
| `lsof -iTCP:9099` | 无监听 |

**6.3 接 MCP 客户端**：`claude mcp add --transport http wails-app-dev http://127.0.0.1:9099/mcp`
（local scope，写入 `~/.claude.json` 的本项目节）。服务在跑时 `claude mcp list` →
`wails-app-dev: http://127.0.0.1:9099/mcp (HTTP) - ✔ Connected`；
服务没跑时是 `✘ Failed to connect`（预期）。移除：`claude mcp remove wails-app-dev`。

**6.6 `wails3 mcp`**（项目 MCP，与上面的应用内 MCP 是两个东西）：

| 检查 | 结果 |
| --- | --- |
| 启动 | `wails3 mcp -http -port 9310` → 打印 endpoint / root / **bearer-token** |
| 无 token | `HTTP 401`，**空响应体**（`curl -o` 会写出空文件，看着像"没响应"，其实是鉴权失败） |
| 带 token | `200`，且返回 `Mcp-Session-Id`（有状态会话，后续要回传） |
| 工具数 | 10（`wails_project_task_run` / `_generate_bindings` / `_task_list` / `_inspect` / `_doctor` / `_build` / `_dev_start` / `_init` / `wails_job_status` / `wails_job_stop`） |
| `wails_project_inspect` | 通过（326 文件、`isWailsProject: true`） |
| `wails_project_task_list` | **空数组**（go-task `--list` 只列 `desc:`，模板用 `summary:`）→ 见 design P16，决定不修 |
| `task_run`（`common:generate:icons`） | `jobId` → `wails_job_status` → `completed`，输出 `is up to date` |
| `generate_bindings` | `completed`，`Processed: 314 Packages, 2 Services, 18 Methods`；**`frontend/bindings` 零 git 改动**（生成幂等） |
| 缺 token 调用变更类工具 | schema 层拒绝：`required: missing properties: ["token"]` |

### 阶段 8（托盘关闭行为，8.2）——2026-09-20

**8.2 落地**：三平台统一「关闭 = 隐藏到托盘」，运行期判据 `App.trayRunning`（`atomic.Bool`，
托盘真起来才隐藏）；托盘缺失时关窗钩子显式 `Quit()`，不留无窗口进程。恢复窗口抽成单一实现
`App.restoreWindow`——托盘菜单「显示」、托盘双击、macOS 点 Dock 图标（新增
`ApplicationShouldHandleReopen` 监听）、再次启动应用（单实例唤起）四条路径共用；
`TitleBar.vue` 的自绘关闭按钮同步改为 `Window.Close()`（进 Go 侧统一决策，不再直接 Hide 绕过）。

| 验证 | 结果 |
|---|---|
| 修复前后对照（用户现场） | 前：`Window close() completed` 之后无任何日志、进程存活、恢复路径全断；后：出现 `窗口已隐藏到托盘 window=main` |
| 关窗 → 隐藏 → 恢复 | `17:34:40 窗口已隐藏到托盘`；`Dock 图标被点击，恢复窗口` + `窗口已恢复显示`（17:35 / 17:37 / 17:41 多次，含用户实机 Dock 点击） |
| `ApplicationShouldTerminateAfterLastWindowClosed` A/B | true：关窗钩子**隐藏成功后进程随即退出**（`窗口已隐藏到托盘` 后 1ms 即 `Shutting down service`，R1 破坏）；false：进程驻留、Dock 可恢复 → **钉 false**（与上游 #4389、官方范例 hide-window 一致）。⚠️ 第一轮实验被测试手法污染（见下），当晚已用干净手法复测，结论不变 |
| 托盘缺失兜底（注入 `tray.Start` 失败实跑） | 关窗日志 `托盘未运行，关闭窗口即退出应用` → `Shutting down service: main.App` → `窗口几何已记录` → `config.json` mtime 更新 → 进程退出、MCP 端口释放、无残留。**完整优雅关闭**（R3 达标） |
| R5 功能回归（MCP 驱动实测，提交版代码） | 几何持久化：config 1018×762 → 重启窗口 1017×761（±1px 边框取整）；主题：`SetTheme dark` → `GetTheme`/config 三方一致（验毕恢复 light）；关窗 → `窗口已隐藏到托盘` 且进程驻留；二次启动（带 `--regression-probe hello-mcp`）exit=0 未抢锁，主实例日志 `检测到二次启动，唤起已有窗口 args=[...]` 并恢复窗口 |
| 护栏红/绿实证 | 回退修复（stash 代码、保留护栏）→ `TestWindowCloseHidesToTray` / `TestCloseWithoutTrayExitsCleanly` 双双 FAIL 且报错指向准确；恢复修复 → ok |
| `make test` / `make lint` | 全绿（guard 1.6s；eslint + vue-tsc 通过） |

**未尽事项**：

- 托盘菜单「显示」的**点击本身**未自动化实测（macOS 辅助功能授权不可用，error -25211），
  按「四路径共用 `restoreWindow` + 可自动化路径端到端实测」口径验收；
- QA 独立回归因账号用量限额（429，次日 10:31 重置）未完成，R5 回归已由主理人用 MCP
  驱动实测补跑（见上表），独立复核仍待补。

**实验手法教训（第一轮证据为何被污染，2026-09-20 复查确认）**：

- `cmd1 && cmd2 &` 的 `$!` 是**子 shell 的 pid**，不是应用 pid——用它做存活判断全错；
- 用前台 `&` 起常驻进程，**Bash 工具调用结束时子进程会被清理**——「config 未落盘、
  日志截断」的假象即源于此，实为测试手法杀掉了进程，与代码无关；
- mcp.py 的 `MCP_URL` **必须显式设置**：默认 9099，机器上同时跑着用户实例时，
  实验请求会静默发给**错误的目标**；
- 正确姿势：`run_in_background` 起服务 + 显式 `MCP_URL` + `pgrep -f 二进制名` 取 pid。


