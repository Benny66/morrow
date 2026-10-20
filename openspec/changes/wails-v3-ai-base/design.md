# wails-v3-ai-base — 设计

> 本文只记**取舍与依据**，不重复 proposal 的结论。
> 所有 v3 API 名均已对着 `wails/v3@v3.0.0-beta.23` 源码核对，标注「待实跑确认」的除外。

## D1 迁到 beta.23，不等稳定版

**决定**：直接迁 `v3.0.0-beta.23`（当前最新 beta）。

**依据**：MCP 是本次迁移的**唯一理由**。它只存在于 v3，v2 永远不会回移。等稳定版意味着无限期搁置 AI 基座这个定位。

**代价与对冲**：beta 的 API 会动。对冲手段不是「小心一点」，而是**把版本钉死 + 让护栏感知漂移**：

- `go.mod` 与 `deps.yaml` 都钉 `v3.0.0-beta.23`，不用 `@latest`。
- 升级时 `go build ./...` 会报错，这是**编译期**的，不会静默。真正的风险是**语义**漂移（编译过、行为变），这类只靠实跑兜。
- 因此在 `tasks.md` 里把「实跑验证」与「编译通过」分列为两道独立关卡，不合并。

## D2 MCP 双服务的职责切分

**决定**：两套 MCP 服务都接，但**只把运行时那套当作基座能力**。

**依据**：两者的对象不同。

| | CLI MCP（`wails3 mcp`） | 运行时 MCP（`-tags mcp`） |
|---|---|---|
| 作用对象 | 项目**源码与生命周期** | **正在运行**的应用实例 |
| 启用方式 | 显式跑 `wails3 mcp` | 构建标签，`WAILS_MCP=1` 时 CLI 自动加 |
| 传输 | stdio（被 host 拉起）/ Streamable HTTP（终端里跑） | Streamable HTTP，默认 `127.0.0.1:9099` |
| 安全 | root 限定项目目录；变更类工具需 session token | 默认只绑 loopback；非 loopback 必须给高熵 token |
| 我们怎么用 | 开发时让 Agent 能跑 build/test/task | **验收时让 Agent 能操作应用** |

**为什么只把运行时那套当基座能力**：CLI 那套替代的是「人敲 make」，收益是顺手；运行时那套替代的是「人肉点界面做验收」，收益是**能力从无到有**。前者任何 Agent 用 shell 也能做，后者不能。

**默认关闭原则（不可动摇）**：`mcp` 标签**绝不出现在默认构建里**。CLI 只在 `WAILS_MCP=1` 显式设置时才加标签，`wails3 build` 裸调用不含服务代码。这条要写进 `AGENTS.md`——它是「无标签即无代码、无端口、无攻击面」这一保证的唯一支点，而**破坏它不会有任何症状**（应用照常跑，只是多开了一个能执行 JS、调 Go 方法的本机端口）。

## D3 托盘：删掉 energye fork，macOS 终于有托盘

**决定**：删除 `internal/tray/start_darwin.go` 的 no-op 与 `start_other.go` 的 `energye/systray` 实现，统一改用 `app.SystemTray`。

**依据**：v2 的 no-op 是被迫的——Wails 自己占着 NSApplication delegate，所有 systray 库也都想当 delegate 来创建 NSStatusBar，冲突无解，项目注释里写得很清楚。v3 的 SystemTray 由框架自己在正确的 delegate 上实现，冲突从根上消失。

**收益**：`internal/tray` 从「两个平台文件 + 一个 fork 依赖 + 一段解释为什么 macOS 不行的注释」缩成一层薄封装。`deps.yaml` 少一个直接依赖。

**风险**：macOS 有托盘之后，「关闭窗口 = 隐藏到托盘」这条在 macOS 上的语义变了——v2 时代 macOS 关闭即退出（无托盘可藏）。这是**有意的行为变更**，需在 `app-lifecycle` 规格里显式更新，而不是让旧注释继续撒谎。

## D4 日志适配器删除

v2：`Options.Logger` 是 `logger.Logger` 接口，项目为此写了 `WailsAdapter` 把 6 个方法桥到 slog，并在注释里论证「不接则前端日志只写 stdout」。

v3：`Options.Logger` 就是 `*slog.Logger`，`Options.LogLevel` 是 `slog.Level`。适配器**整文件删除**。

**注意**：`internal/logging/wails.go` 里那条「不接 logger 则日志静默丢失」的论证**依然成立**——v3 默认 logger 同样是 stdout。所以它的接线条目（`Logger` 字段必填）必须在 `wiring_test.go` 里**保留**，只是解析目标从 `options.App` 换成 `application.Options`。删适配器 ≠ 删护栏。这是本次迁移最容易做错的地方。

## D5 错误协议：`any` → `[]byte`

| | v2 | v3 |
|---|---|---|
| 字段 | `ErrorFormatter func(error) any` | `MarshalError func(error) []byte` |
| 项目实现 | `apperr.Format(err) any` | `apperr.MarshalError(err) []byte` |
| 返回值 | JSON **字符串**（`string(b)`） | JSON **字节**（`[]byte`） |

**为什么要变**：v2 时代项目被迫返回字符串而非对象，因为前端 `new Error(payload)` 会把对象强转成 `"[object Object]"`。v3 的 `MarshalError` 契约是「必须返回合法 JSON 字节，或返回 nil 回退默认处理」——**框架替你做了序列化**，不再经过 `new Error(payload)` 那条会丢结构的路径。

**因此**：`apperr` 的包注释里那段「必须返回字符串，返回对象会变 [object Object]」的论证**需要重写**，不是删掉——它的**结论**（前端要能 `JSON.parse` 出 `{code,message}`）不变，但**理由**变了。留着旧理由就是留一条会误导下一个人的假注释。

## D6 App 不再持有 ctx

v2 的 `App` 有个 `ctx context.Context` 字段，`startup(ctx)` 时存入，之后所有 `runtime.*` 调用都要把它传进去。v3 没有 `runtime` 包——能力长在对象上：

| v2 | v3 |
|---|---|
| `runtime.Show(a.ctx)` | `window.Show()` |
| `runtime.WindowUnminimise(a.ctx)` | `window.UnMinimise()` |
| `runtime.Quit(a.ctx)` | `app.Quit()` |
| `runtime.WindowIsMaximised(a.ctx)` | `window.IsMaximised()` |
| `runtime.WindowGetSize(a.ctx)` | `window.Size()` → `(width, height int)` |
| `runtime.EventsEmit(a.ctx, name, data)` | `app.Event.Emit(name, data)` |

**结果**：`App` 结构体删掉 `ctx` 字段，改为持有 `*application.App`（需要窗口时取 `app.Window.Current()`）。

**收益**：`notifySecondInstance` 里那句 `if a.ctx == nil { warn }` 的防御可以删——`*application.App` 在 `NewApp` 时就注入了，不存在「上下文未就绪」这个状态。少一个不可能发生的分支。

## D7 组合根三拆

v2 一句 `wails.Run` 同时干了「建应用 / 建窗 / 跑循环」。v3 拆成三段，这对基座是**加分**：

```go
app := application.New(application.Options{...})   // 建应用（含 Services / Assets / Logger）
app.Window.NewWithOptions(application.WebviewWindowOptions{...})  // 建窗（几何 / 无边框 / 平台选项）
app.Run()                                          // 跑循环
```

原来的「窗口几何必须在创建期给定，不能运行期设」这条约束（`main.go` 注释里有详细论证：OnStartup 在 goroutine 里跑，紧接着窗口就显示，存在竞态）**在 v3 里更自然了**——`NewWithOptions` 就是创建期，`StartState` 直接写在里面，语义比 v2 更显式。

映射表：

| v2（`options.App` 顶层） | v3 |
|---|---|
| `Title` / `Width` / `Height` | `WebviewWindowOptions.Title/Width/Height` |
| `WindowStartState: options.Normal\|Maximised` | `WebviewWindowOptions.StartState: application.WindowStateNormal\|WindowStateMaximised` |
| `Frameless: bool` | `WebviewWindowOptions.Frameless` |
| `BackgroundColour: *options.RGBA` | `WebviewWindowOptions.BackgroundColour: application.RGBA`（**值类型，非指针**） |
| `AssetServer: &assetserver.Options{Assets: assets}` | `Options.Assets: application.AssetOptions{Handler: application.BundledAssetFileServer(assets)}` |
| `Mac: &mac.Options{TitleBar: mac.TitleBarHiddenInset()}` | `WebviewWindowOptions.Mac.TitleBar`（`mac.TitleBar*` → `application.MacTitleBar*`，**待实跑确认常量名**） |
| `Windows: &windows.Options{Theme: windows.SystemDefault}` | `WebviewWindowOptions.Windows.Theme`（类型 `application.Theme`） |
| `SingleInstanceLock: &options.SingleInstanceLock{UniqueId, OnSecondInstanceLaunch}` | `Options.SingleInstance: &application.SingleInstanceOptions{UniqueID, OnSecondInstanceLaunch}`（**注意 `UniqueId` → `UniqueID`**） |
| `HideWindowOnClose: bool` | 窗口事件拦截（**待实跑确认取消语义**） |
| `OnStartup: app.startup` | `ServiceStartup(ctx, opts) error` |
| `OnShutdown: app.shutdown` | `ServiceShutdown() error` + `Options.OnShutdown func()` |

## D8 前端绑定换形态

| | v2 | v3 |
|---|---|---|
| 目录 | `frontend/wailsjs/go/main/App` | `frontend/bindings/<module>/app.ts` |
| 事件 | `import { EventsOn } from '../wailsjs/runtime/runtime'` | `import { Events } from '@wailsio/runtime'` → `Events.On` |
| 日志 | `import { LogInfo } from '../wailsjs/runtime/runtime'` | `import { LogService } from '../bindings/.../services/log'`（见 D11） |
| 窗口 | `WindowMinimise()` 等自由函数 | `Window.Minimise()`（**单例对象 + 异步**） |
| 生成 | `wails dev/build` 自动 | `wails3 generate bindings`（dev 时自动） |

**改动点比原判断多（此处更正）**：本文早前写过「前端**没有任何文件直接 import `wailsjs`**（已核实）」——**这句是错的**，实测有 5 个文件引用它：

| 文件 | 引用的是什么 |
|---|---|
| `lib/log.ts` | `wailsjs/runtime/runtime` 的 LogXxx |
| `lib/event.ts` | `wailsjs/runtime/runtime` 的 EventsOn |
| `components/TitleBar.vue` | `wailsjs/runtime/runtime` 的 WindowXxx（动态 import） |
| `stores/app.ts` | `wailsjs/go/main/App` 的 GetTheme/SetTheme |
| `views/Settings.vue` | `wailsjs/go/main/App` 的 5 个方法 |

**结论不变**：契约层（`lib/`）仍然把调用收在一处——`stores/` 与 `views/` 只是**改 import 行**，逻辑一行没动；`TitleBar.vue` 略多于改 import（窗口 API 由同步变异步，见 D12）。所以「换框架不动业务代码」这条价值**基本成立，但不是零改动**，别把话说满。

改完仍要在 `docs/map.md` 里把这条写明白。

## D9 护栏同步（最易漏，且漏了无症状）

三个护栏的解析目标会失效。失效方式是 **Fatal（解析到 0 个目标即报错）**，所以不会静默——这是上一轮「护栏感知自己瞎了」那条铁律的兑现。

| 护栏 | 现状 | v3 后 |
|---|---|---|
| `wiring_test.go` | 找 `&options.App{...}` 字面量，断言 `Logger` 字段 | 找 `application.Options{...}` 字面量。**字段断言必须保留**（见 D4） |
| `bindings_test.go` | 校 `frontend/wailsjs/go/main/App.d.ts` | 校 `frontend/bindings/**` 下的服务文件 |
| `layer_test.go` / `parity_test.go` | 与框架无关 | **不动** |

**新增护栏（本次应加的）**：`mcp` 标签不得进入默认构建。这条是 D2 说的「破坏了没有症状」的那类，属于 `internal/guard/wiring_test.go` 的覆盖范围——断言 `scripts/build.sh` 与 CI 工作流里没有裸加 `-tags mcp`。

## D10 不可逆点与回退

**回退成本**：整个迁移在一个独立分支（`feat/wails-v3-ai-base`）上，`main` 未动。回退 = 不合并。

**不可逆的是心态而非代码**：一旦基座带上 MCP，下游会开始依赖「Agent 能验收」这个能力。此后回 v2 等于砍掉一个能力，不只是回滚一个版本。所以**合并前必须实跑验证 MCP 真的能用**，而不是「编译过了」。

## 实跑确认结果（2026-09-20，wails3 v3.0.0-beta.23）

原「待确认清单」6 项**全部已实跑钉死**。方法：`go install wails3@v3.0.0-beta.23` +
`wails3 init` 生成真实项目 + 写 spike 编译（`/tmp/v3probe`）+ 真跑应用并用 MCP 客户端接入。

### ✅ 1. 拦截窗口关闭 = `RegisterHook` + `Hide` + `Cancel`

```go
window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
    window.Hide()
    e.Cancel()
})
```

**关键**：必须用 `RegisterHook`，**不能用 `OnWindowEvent`**。看 `HandleWindowEvent` 实现：

- **hooks 同步顺序执行**，任一 hook `Cancel()` 后**立即 return**，listener 不跑、`dispatchWindowEvent` 不调 → 默认动作（销毁）被阻止。
- **listeners 是 `go func()` 各自异步执行**的，且只是「检查 `IsCancelled()` 后跳过」——它抢不过默认动作。

这是官方 `examples/hide-window` 的写法，已验证。

### ✅ 2. `application.MacTitleBarHiddenInset`

是包级 `var`（值类型，**不是函数调用**），与 v2 的 `mac.TitleBarHiddenInset()` 对应。同级还有 `MacTitleBarDefault` / `MacTitleBarHidden` / `MacTitleBarHiddenInsetUnified`。
写在**窗口**选项里：`WebviewWindowOptions.Mac.TitleBar`（v2 在 `options.App.Mac`）。

### ✅ 3. `application.SystemDefault` / `Dark` / `Light`

`type Theme int`，常量在 **`application` 包级**，不在 `windows` 子包。
→ v2 的 `windows.SystemDefault` 写成 `application.SystemDefault`，放在 `WebviewWindowOptions.Windows.Theme`。

### ✅ 4. `SingleInstanceOptions.UniqueID`（注意大小写）+ `SecondInstanceData`

```go
SingleInstance: &application.SingleInstanceOptions{
    UniqueID: appName + "-single-instance",   // v2 是 UniqueId
    OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
        data.Args        // []string    —— 同 v2
        data.WorkingDir  // string      —— v2 是 WorkingDirectory，【字段名变了】
        data.AdditionalData // map[string]string —— v3 新增
    },
}
```

⚠️ `WorkingDirectory` → `WorkingDir`。本项目 `SecondInstancePayload` 的 JSON tag 是 `working_dir`，**payload 定义不用改**（那是我们自己的契约），但取值的字段名要改。

### ✅ 5. 版本号单一真相换了载体 —— **本次最大的坑**

| | v2 | v3 |
|---|---|---|
| 配置文件 | `wails.json` | **`build/config.yml`**（v3 **没有** wails.json） |
| 版本字段 | `info.productVersion` | `info.version` |
| 格式 | JSON | YAML（值可能带引号：`version: "0.0.1"`） |
| 渲染进 Info.plist | `wails build` **自动** | **必须显式**跑 `wails3 task common:update:build-assets` |

`build/config.yml` 自己的注释就是警告：

> When you update `info` or `fileAssociations`, run `wails3 task common:update:build-assets` to update the assets.
> **Note that this will overwrite any changes you have made to the assets.**

实跑证据：`info.version: "0.0.1"` → 跑 `update:build-assets` 后 `build/darwin/Info.plist` 的
`CFBundleShortVersionString` 与 `CFBundleVersion` 均为 `0.0.1`。

**对本项目的影响**：`scripts/build.sh` 是「版本注入唯一入口」，它现在必须做四件事而不是三件——

1. 从 **`build/config.yml`** 读 `info.version`（正则要改，且要容忍 YAML 引号）
2. **跑 `wails3 task common:update:build-assets`**（v2 没有这一步）
3. ldflags 注入 `internal/appinfo`（不变）
4. 回读 Info.plist 校验（不变）

漏第 2 步的后果：应用内显示新版本、系统「显示简介」显示旧版本——**正是这个脚本当初存在要消灭的那种双源漂移**。

### ✅ 6. 窗口字段名（全部实测编译通过）

```go
application.WebviewWindowOptions{
    Name, Title, Width, Height,
    URL: "/",                    // v2 无此字段（v2 由 AssetServer 决定）
    DevToolsEnabled: bool,       // 字段名确认
    Frameless: bool,             // 同 v2
    Hidden: bool,
    BackgroundColour: application.NewRGB(14, 15, 18),  // 值类型 + 构造器（v2 是 *options.RGBA）
    StartState: application.WindowStateMaximised,      // v2 是 options.Maximised
    MinWidth, MinHeight, MaxWidth, MaxHeight,
    Mac: application.MacWindow{...},
    Windows: application.WindowsWindow{...},
    Linux: application.LinuxWindow{...},
}
```

资源服务：`Assets: application.AssetOptions{Handler: application.AssetFileServerFS(assets)}`
（嵌 `all:frontend/dist` 用 `AssetFileServerFS`；嵌 `assets` 目录的示例用 `BundledAssetFileServer`，两者都在）。

## 实跑中新挖出的 5 个坑（原清单里没有，但会炸）

### ⚠️ P1 `MessageDialog` 语义变了 —— 同步返回没了

| | v2 | v3 |
|---|---|---|
| 调用 | `runtime.MessageDialog(ctx, opts)` | `app.Dialog.Question()` |
| 取结果 | **直接返回**被点按钮的文本 `"Yes"` | **只有 `Show()`，无返回**；结果靠 `Button.OnClick` 回调 |
| 链式 | —— | ⚠️ `AddButton(s)` 返回 **`*Button`**，不能串在链中间 |

本项目 `dialog.Confirm(ctx, title, msg) (bool, error)` 依赖「对话框返回被点按钮文本」。v3 下**必须用 channel 桥接**才能保住同步签名：

```go
dlg := app.Dialog.Question().SetTitle(title).SetMessage(message)  // 先建好
answer := make(chan bool, 1)                                      // 缓冲 1
dlg.AddButton("是").OnClick(func() { answer <- true })             // AddButton 返 *Button，故不能链
dlg.AddButton("否").SetAsCancel().OnClick(func() { answer <- false })
dlg.Show()
return <-answer, nil
```

已编译验证。`Button{Label, IsCancel, IsDefault, Callback}`，`SetAsDefault()` / `SetAsCancel()`。

### ⚠️ P2 macOS 托盘传空图标会 **panic**，不是返回 error

实跑崩溃栈：`index out of range [0] with length 0` @ `systemtray_darwin.go:205`，
在 `(*macosSystemTray).run.func1` 里索引图标字节。补上 1×1 透明 PNG 后启动正常。

**结论**：本项目 `internal/tray` 里那个 `placeholderIcon()` 兜底**不是过度防御，是必需的**——
迁移时必须保留。而且这条该进护栏（「托盘图标不得为空」）。

### ⚠️ P3 前端绑定是 **数字 ID 寻址**，不是名字

生成产物长这样：

```ts
export function Greet(name: string): $CancellablePromise<string> {
    return $Call.ByID(1411160069, name);
}
```

ID 是方法全限定名的哈希（运行时日志也能看到：`Registering bound method: fqn=main.AppService.Confirm id=3802682850`）。
→ **改方法名或改包路径，ID 就变，绑定必须重新生成**，否则运行时才炸。

对护栏的影响：v2 的 `bindings_test.go` 靠「方法名出现在 `App.d.ts`」校验。v3 里函数名仍是
`export function <方法名>`，所以**可以改造成断言 `export function <方法名>(`**，但要额外注意
`Call.ByID` 的存在——「名字对但 ID 过期」是编译期看不出来的。

### ⚠️ P4 MCP 标签的剥离是**可量化**的（好消息）

实跑 `nm` 对比同一个 spike：

| 产物 | 含 `mcp` 的符号数 | 体积 |
|---|---|---|
| 默认构建 | 1（且是巧合：`_runtime.numCPUStartup`，非 MCP） | 15,278,066 B |
| `-tags mcp` | **56**（`wails/v3/pkg/application.(*mcpServer).*`） | 16,498,290 B |

→ 「无标签即无代码」得到硬证据。护栏可用**精确符号**断言：
`nm <产物> | grep "wails/v3/pkg/application.(*mcpServer)"` 必须在默认产物中为 0。

### ✅ P5 `js_eval` / `call_bound_method` 可用 —— 但**要求页面已加载 Wails 运行时**

机制：`eval` 把 `mcp_inject.js` 全文 + 调用拼成脚本交给 `window.ExecJS`，
页面上的 `window.__wailsMCP.run(...)` 执行后 **`fetch()` POST 回 `http://<host>:<port>/eval-result`**。

**初次在 spike 里超时的原因已查明**：spike 的页面是空壳 `<html></html>`，没有
`wails://localhost/wails/runtime.js`，取返回值的那次 `fetch` 无处落地 → 30s 超时。
换成 `wails3 init` 生成的真实前端后**全部正常**。

真实的端到端证据（外部 MCP 客户端 → 运行中的应用）：

| 调用 | 结果 |
|---|---|
| `call_bound_method{name:"main.GreetService.Greet", args:["Wails"]}` | **`Hello Wails!`** |
| `js_eval{js:"return document.body.innerText..."}` | 真实页面文本（含 `v3.0.0-beta.23`、实时时间戳） |
| `dom_query{selector:"h1, button, .logo"}` | `h1.title` 文本 "Wails + Vue"，含真实坐标与尺寸 |
| `app_info` / `windows_list` | 真实窗口几何（1023×767 @ 223,71，name=`main`） |
| `emit_event{name:"app:second-instance"}` | 应用 Go 侧日志收到 `data="map[args:[a] working_dir:/tmp]"` |

**结论**：MCP 的 16 个工具在真实前端下全部可用，「Agent 改完代码后自动验收」这个能力**成立**。
唯一前提是应用有正常的 Wails 前端（任何用 `wails3 init` 或本项目基座搭出来的应用都满足）。

**顺带确认**：页面源是 `wails://localhost`（自定义 scheme），从该源 `fetch` 到 loopback HTTP 端点**不被拦**——不需要担心生产构建下的源限制。

## 顺带确认的其它事实

- `Options.Logger` 确实是 `*slog.Logger`、`LogLevel` 是 `slog.Level`——实跑日志证明接得上（slog 输出落到应用日志）。**`WailsAdapter` 可删，但接线条目要保留在护栏里。**
- `MarshalError func(error) []byte` 签名已编译验证。
- `application.RegisterEvent[T](name)` 是 v3 新增：注册后绑定生成器会产出**强类型事件 API**，可用来强化本项目的事件契约。
- `app.Env.OpenFileManager(path, selectFile bool) error` —— v2 `runtime` 的 reveal 等价物在 `Env` 上。
- 编译告警：`ld: warning: object file ... built for newer 'macOS' version (26.0) than being linked (11.0)`；v3 模板的 Info.plist 声明 `LSMinimumSystemVersion 12.0.0`。**v3 的最低系统要求高于 v2**，需评估对下游用户的影响。
- v3 构建体系是 **Taskfile**（根 `Taskfile.yml` include `build/Taskfile.yml` + 各平台 Taskfile）；`wails3 build` 只是入口。`Makefile` 要包一层而不是替换。
- 前端绑定产物路径：`frontend/bindings/<go module 名>/<service>.ts`——目录名取自 **go.mod 的 module**（模板项目里是 `changeme`），不是应用名。本项目 module 是 `my-notice` 占位符，生成器要处理。
  - ✅ 已实测：生成产物是 `frontend/bindings/my-notice/app.ts`，且 `moduleResolution: "Node"` 能正确解析产物内部的 `.js` 后缀 import（`vue-tsc --listFiles` 确认 5 个 bindings 文件都进了程序）。**tsconfig 不用改。**

---

# 实施阶段追加的结论（2026-09-20，写代码时挖出来的）

前面那 6 项「待确认」是在 spike 里核对的 API 面。真正动手改本项目时，又挖出 4 个
原清单没有的坑——**都不在这 6 项的视野里**，且每一个都会让某条链路静默断掉。

## D11 前端日志通道在 v3 **被整个拿掉了** —— 必须注册官方 log 服务

**这是本次迁移里最容易「白改一处」的地方，也是任务 3.4 的真正内容。**

| | v2 | v3 |
|---|---|---|
| 函数 | `@wailsjs/runtime` 的 `LogDebug/LogInfo/LogWarning/LogError` | **不存在**（`@wailsio/runtime` 的顶层导出里没有 log 模块） |
| 送进哪 | 经 WailsInvoke → `options.App.Logger` | —— |
| `Options.Logger` 管什么 | 应用日志 + 系统消息 | **只管 Wails 系统消息**（字段文档原文：「not application messages」） |
| 替代物 | —— | `pkg/services/log` 的 `LogService`（**一个 Service**，不是 runtime 函数） |

**决定**：注册官方 `logservice.NewWithConfig(&logservice.Config{Logger: logging.Frontend(), LogLevel: logLevel})`。

三点理由：

1. **v3 没有别的路**。前端日志要落进 `app.log`，只有这一个通道。
2. **不注册没有任何症状**——应用照常跑，只是前端日志（绑定调用失败、启动时间线、全局异常上报）全部只剩 `console`，而打包后的应用**没有 DevTools**，等于没记。与 D4 说的「后端 logger 不接则日志静默丢失」是同一类失效，故同样进了护栏。
3. **不自己造**。本可以在 `App` 上加一个 `LogFrontend(level, msg)` 绑定方法——但框架已有官方服务，自己再做一个就是「一件规则两个实现」，与铁律 1 相悖。

**为什么必须 `NewWithConfig` 而不是 `log.New()`**：见下面 P7。这条已写进护栏断言。

**`Logger` 传 `logging.Frontend()` 而非 `slog.Default()`**：v2 的 `WailsAdapter` 给每条前端日志打了 `source=wails` 标记；删掉适配器后这个区分就没了。`logging.Frontend()` 返回 `slog.Default().With("source", "frontend")`，把标记补回来——否则前端日志与 Go 业务日志在文件里完全同形，排查时分不出是谁报的。

## D12 窗口 API 由同步变异步 —— `TitleBar.vue` 不是「只改 import」

`Window.Minimise()` 等返回 Promise，失败是 **reject** 而非同步抛错。故 v2 那套
`try { WindowMinimise() } catch {}` **在 v3 下接不住任何东西**——必须挂 `.catch`。
纯 vite dev 环境下每个未挂 catch 的调用都会变成一条未处理的 rejection。

同一件事在 `lib/log.ts` 上更要紧：那里每条日志都会走一次，故加了一个
`bridgeUnavailable` 标志位，首次 reject 后不再尝试 IPC（否则控制台会被淹没）。

**顺带更正一处**：`lib/event.ts` 里 v2 的
`try { return EventsOn(...) } catch { console.warn('事件订阅失败') }` 是**假的兜底**——
v3 的 `Events.On` 是纯本地注册（往 Map 里塞回调，Go 侧经
`window._wails.dispatchWailsEvent` 推过来），不抛错也不发请求。非 wails 环境下它
**静默失效**，而本模块没有可靠的本地判据能发现。故删掉那个永远不会触发的 catch，
把这件事写进注释——留一个假的兜底只会让人以为它在工作。

## D13 macOS「关闭 = 隐藏到托盘」：**已解决（三平台统一，运行期判据）**

D3 指出 macOS 有托盘后这条语义变了。`tasks.md` 8.2 曾把它列为待决策项，本次**已落地**：
关闭窗口 = 隐藏到托盘，三平台统一；是否隐藏由**运行期**判据「托盘是否真的起来了」决定。

**当时的顾虑与它的答案**：托盘在 `ServiceStartup` 里起、起失败只记日志不阻断启动；若 macOS
上无脑「关闭 = 隐藏」，一旦托盘起失败，应用就变成一个**看不见也点不到的**后台进程。当时
可选答案之一是「按托盘是否真的起来了动态决定」——即本次采用的方案：

- `App` 上用一个 `atomic.Bool`（`trayUp`）记录托盘是否真的起来了，在 `ServiceStartup`
  里 `tray.Start` 成功后才置 true（`app.go`）。
- 关窗钩子（`main.go` 的 `events.Common.WindowClosing`）在**运行期**读该状态：
  - 托盘在 → `window.Hide()` + `e.Cancel()`（拦下默认的销毁动作，窗口保留、进程驻留）；
  - 托盘不在 → **同样 Cancel 拦下销毁**，再显式 `wailsApp.Quit()` 退出——
    绝不残留无窗口进程。（不放行销毁的原因：macOS 的
    `ApplicationShouldTerminateAfterLastWindowClosed` 必须为 false（见下），
    此时「销毁最后一个窗口」并不会让进程退出，放行只会留下无窗口进程。）

**为什么 `ApplicationShouldTerminateAfterLastWindowClosed` 显式钉 `false`，而不是直觉上的
`true`**：true 与「隐藏到托盘」相冲。A/B 实测（2026-09-20，同一 `Window.Close()` 路径）：
true 时关窗钩子正常隐藏（日志出现「窗口已隐藏到托盘」）后**进程随即退出**，R1 被破坏；
false 时进程驻留、Dock 点击可恢复。上游 issue #4389 记录了同源行为
（`webview_window_darwin.m` 为此在 `windowShouldClose:` 加了「窗口隐藏时不触发关闭事件」
的短路），上游官方范例 `examples/hide-window` 也设 false。托盘缺失时的退出**不依赖**该
字段，由关窗钩子显式 `Quit()` 保证。

**证据链（本次报障的直接成因）**：旧 `hide_window_darwin.go` 的 `hideWindowOnClose()` 在
darwin 上返回 `false`（立论「macOS 无托盘常驻」，在 v3 已失效——`internal/tray/tray.go`
文件头写明 v3 起 macOS 有真托盘），于是 `main.go` 不注册 `WindowClosing` 钩子，关闭按钮走
默认动作**销毁窗口**；而 v3 该 macOS 选项默认 `false`，最后一窗关闭后进程**不退出**；此时
`Window.Current()` 因窗口已从注册表 `Remove` 而返回 `nil`，托盘「显示」静默什么都不做；
点 Dock 图标走的 `ApplicationShouldHandleReopen` 事件**全仓无人监听**——四条恢复路径全断。
用户现场日志止于 `Window close() completed`，之后无任何日志、进程却存活，即为佐证。

**复现要点**：关窗后 `windows_list` 不再有窗口、`Window close() completed` 之后再无日志、
进程仍存活。修复后关窗应出现隐藏日志、窗口仍在注册表、进程存活，且可经恢复路径唤回。

---

## ⚠️ P6 `production` 构建下，框架默认 logger 是 `io.Discard`

`pkg/application/logger_prod.go`（`//go:build production`）：

```go
func DefaultLogger(level slog.Leveler) *slog.Logger {
    return slog.New(slog.NewTextHandler(io.Discard, nil))
}
```

**它把日志直接丢掉。** dev 构建（`logger_dev.go`）才是写 stderr 的那个。

意义有两层：

1. **凡是「不配就退回框架默认 logger」的地方，生产产物里都是彻底静默**——不只是"写 stdout 看不到"，是连 stdout 都没有。这让 D4 的接线条目从「应该接」升级为「不接就是零日志」。
2. 由此推出 **D11 的 `log.New()` 是错的**：它走 `application.DefaultLogger`，production 下 = 前端日志进 `io.Discard`。**接线在、日志没了、零报错**。故护栏断言必须是 `NewWithConfig` + `Config.Logger` 非空，而不只是「有没有注册」。

## ⚠️ P7 绑定 ID 是**可复算**的 —— P3 从「靠人记得」升级为「护栏能拦」

P3 说 `Call.ByID(<hash>)` 会让「名字对但 ID 过期」在编译期看不出来。**但那个 hash 是可复算的**：

```go
// wails/v3/pkg/application/bindings.go
fqn := fmt.Sprintf("%s.%s.%s", packagePath, typeName, methodName)  // packagePath 即 reflect PkgPath()
methodID := hash.Fnv(fqn)                                          // wails/v3/internal/hash/fnv.go：FNV-1a 32
```

对 `package main` 里的 `App`，`fqn = "main.App.<方法名>"`。实测复算本项目 11 个方法，
**11/11 命中**生成产物里的 ID。

**于是 `bindings_test.go` 不再只查名字**，而是：解析 `app.go` 的导出方法 → 复算 FNV →
与产物里的 `$Call.ByID(n)` 逐条对账。这同时覆盖两种情况：

- 绑定过期（方法改了名、产物没重新生成）→ 「未找到」+「产物里有已删除的方法」；
- **名字对但 ID 对不上**（产物来自另一次生成的残留）→ 「ID 过期」并打印期望值。

**护栏必须镜像框架的排除表**：`internalServiceMethods = {ServiceName, ServiceStartup, ServiceShutdown, ServeHTTP}`。v3 的 `App` 是 Service，前两个是导出方法但框架不绑给前端——护栏不知道这条就会误报。镜像已写进 `bindings_test.go` 的 `frameworkExcludedAppMethods`。

## ⚠️ P8 `SaveFileDialogStruct` 漏了 `SetTitle`（框架的一处缺口）

`SaveFileDialogOptions` **有** `Title` 字段，Windows 实现也确实消费它
（`dialogs_windows.go` 拿它做对话框标题），但 `SaveFileDialogStruct` **没暴露 `SetTitle`**
——`OpenFileDialogStruct` 有，只有保存对话框漏了。

故 `SelectSaveFile` 只能走 `SetOptions` 整体赋值。**注意它是整体赋值不是增量**：会把
`Message`/`Directory`/`Filename`/`ButtonText`/`Filters`/`Window` 全部按传入值覆盖——
将来往里加字段时必须一并带上，否则被静默清空。已把这条写进 `internal/dialog` 的注释。

（macOS 侧 `title` 本就用不上——`NSSavePanel` 没有标题概念。所以这个坑只在
Windows/Linux 上显形，本机开发不会发现。）

---

## ⚠️ P9 `wails3 build` **不转发 `-ldflags`** —— 版本注入只能走 Taskfile 变量

v2 的注入方式是 `wails build -ldflags "-X ..."`。v3 里这条**没了**：`wails3 build` / `wails3 package`
只把 `EXTRA_TAGS` / `OBFUSCATED` / `GARBLE_ARGS` / `GOOS` / `ARCH` 转发成 Taskfile 变量。

依据是读源码而不是猜（`v3.0.0-beta.23`）：

- `internal/commands/task_wrapper.go`：只把这些字段拼成 `task <name> VAR=value` 的变量传入；
- `internal/commands/flags/package.go`：package 的 flag 集合里根本没有 ldflags。

于是注入链只能改成「经 Taskfile 变量」：

```
scripts/build.sh
  → wails3 build VERSION=x.y.z COMMIT=abc1234
  → 根 Taskfile.yml 的 VERSION_LDFLAGS 拼出 '{{if .VERSION}}-X {{.MODULE}}/internal/appinfo.InjectedVersion={{.VERSION}}{{end}} …'
  → 各平台 Taskfile 的 BUILD_FLAGS 把它拼进 -ldflags
```

**注意 flag 与变量的顺序**：Go 的 flag 解析在遇到第一个非 flag 参数时停止，所以必须
`wails3 build -tags xtra VERSION=… COMMIT=…`，写成 `wails3 build VERSION=… -tags xtra`
会让 `-tags` 被当成位置参数丢掉。

链接器的行为让这条链的失效**没有任何症状**（见护栏 9）：`-X` 指向不存在的符号时静默忽略，
构建成功、无告警，只是应用内版本恒为 `"dev"`。所以 `scripts/build.sh` 打包后会
`grep -aqF "$VERSION"` 回读产物，`smoke.sh` 另断言 `app.log` 里出现 `version=$VERSION`。

## ⚠️ P10 `build` 任务必须认 `OUTPUT` 变量 —— 迁移时真的踩了（护栏 14）

v3 官方模板里每个平台的 `build` 任务都是：

```yaml
cmds:
  - go build {{.BUILD_FLAGS}} -o {{.OUTPUT}}
vars:
  DEFAULT_OUTPUT: '{{.BIN_DIR}}/{{.APP_NAME}}'
  OUTPUT: '{{ .OUTPUT | default .DEFAULT_OUTPUT }}'
```

这不是风格，是 `build:universal` 能工作的**前提**：它给两次 `build` 传不同的
`OUTPUT`（`-amd64` / `-arm64`），产出的两个中间产物再交给 `lipo` 合并。

本仓迁移时照抄平台 Taskfile，却把 `-o` 写死成 `{{.BIN_DIR}}/{{.APP_NAME}}`、顺手丢了 `OUTPUT`。后果分两种：

| 平台 | 后果 | 有症状吗 |
| --- | --- | --- |
| darwin | 两次编译覆盖同一文件，`lipo` 报 `can't open input file` | 有（报错） |
| linux / windows | 没有 universal 调用者 → 任何传 `OUTPUT` 的调用方静默拿到写死路径的产物 | **没有** |

**处置**：三个平台一并改成 `OUTPUT: '{{.OUTPUT | default .DEFAULT_OUTPUT}}'`（只改 darwin 会让
「哪个平台的 build 认 OUTPUT」变成要靠记忆的例外），并由**护栏 14**
（`TestBuildTaskHonorsOutputVar`）覆盖：断言三个平台的 `build` 都用 `-o "{{.OUTPUT}}"`、
都定义了 OUTPUT/DEFAULT_OUTPUT，且 darwin 的 `build:universal` 给两架构传的
OUTPUT 与 `lipo` 的输入路径互相咬合。

## ⚠️ P11 `go build -o` **拒绝覆盖非 Go 产出** → `make build` 不可重复执行

实测（**本机 go1.26.2 / darwin-arm64**），`go build -o <path>` 对已存在文件的处置：

| 既有文件是什么 | 结果 |
| --- | --- |
| 上一次 `go build` 的产物 | 覆盖成功 |
| 同上，但被 `chmod 644` 去掉可执行位 | 覆盖成功（判据不是权限位） |
| 普通文本文件 / 可执行脚本 | **拒绝**：`build output "…" already exists and is not an object file` |
| `lipo` 合并出的 universal 二进制 | **拒绝**（同一条报错） |

> **版本口径**：只实测了 go1.26.2。本仓 `go.mod` 的 go 指令是 `1.25.0`，CI（`build.yml` / `release.yml`）
> 也固定 `go-version: '1.25'`，故**未验证 go1.25 是否同样如此**。不要把这句读成「1.26 才引入的
> 行为变更」——那样会让「CI 绿 = 没事」变成一个假安全结论。`rm -f` 的处置在哪个版本上都无害。

最后一行是本仓的实际故障：darwin 的交付路径走 `package:universal`，`lipo` 把合并结果写到
`build/bin/<APP_NAME>`；此后任何 `make build` / `make smoke` 里的
`go build -o build/bin/<APP_NAME>` 都会撞上它——**先 `make package` 再 `make build` 必炸**。

**这是本次迁移引入的**：v2 的交付路径不产出 lipo 产物，这个组合此前不存在。

**处置**：三个平台的 `build` 任务在编译前加一行 `rm -f "{{.OUTPUT}}"`。删的是任务自己声明的
产物路径，且位于 gitignore 的 `build/bin/` 下，删之无损；换来的是「任意顺序重复执行都成立」。

**为什么没有为之单独加护栏**：这条失效**会出声**（exit 2 + 明确报错），不符合铁律 5 的适用面
（护栏预算只花在「没有任何症状的失效」上，理由同 P2）。验证方式是让构建顺序真的这么跑一遍——
见 tasks.md 末尾的「验证记录」。

## D14 前端镜像类型必须如实反映绑定生成的类型（`Result<T>.list` 的可空性）

修掉 `_example` 模板里 v2 的导入路径之后，生成页面撞上第二层不兼容。绑定生成的实际类型是：

```ts
// frontend/bindings/<模块>/internal/page/models.ts（生成物）
export interface Request { "page": number; "page_size": number }
export interface Result<T> { "list": T[] | null; "total": number; "page": number; "page_size": number }
```

`Request` 与 `lib/page.ts` 的 `PageRequest` 逐字段一致；**只有 `Result<T>.list` 差一处**：
生成物是 `T[] | null`，而镜像写的是 `T[]`。报错因此长成一段很绕的话（先抱怨
`CancellablePromise` 与 `Promise` 的 `then` 不兼容，再层层下钻到 `null`），容易被误读成
「v3 的 `CancellablePromise` 不能当 `Promise` 用」——**不是**：`Settings.vue` 里
`invoke(() => GetAppInfo())` 一直是通的，证明 `CancellablePromise<T>` 可赋给 `Promise<T>`；
那段 `then` 报错只是 `Result<T> ≠ PageResult<T>` 的连带后果。

**决定：镜像改写成 `T[] | null`，而不是在下游加断言或 cast。**

理由分两层：

1. **镜像的职责是如实**。Go 的 `[]T` 可 nil，生成器无从知道运行期一定非 nil，于是保守地标成可空。
   镜像写死 `T[]` 就是替线上类型作一个更强的断言——而这句谎言**不会报错**：它只在 Go 侧真的
   返回 nil 时兑现，症状是下游拿到 null 却以为拿到了数组。
   这与铁律 1「镜像只许复制常量与类型，不许复制逻辑」是同一条：**也不许加固类型**。
2. **null 的归一化本就该只有一处**。`page.NewResult` 保证无数据时是 `[]`（Go 的不变量），
   前端侧对应的一次归一化是 `usePagedList` 里已有的 `res.list ?? []`；它的返回类型
   `UsePagedListReturn<T>.list` 仍是 `Ref<T[]>`。于是「可空」只出现在契约边界，
   下游页面的类型是干净的。

**运行期的非 nil** 仍由 Go 保证（`page.NewResult`），但那是运行期不变量，不该由前端的**类型**来断言——
两者是不同层次的承诺，混同会让人以为「类型对了就等于不会拿到 null」。

**为什么这件事值得单列**：它暴露了「生成物类型 vs 手写镜像类型」这个新的漂移面，而 v3 之前不存在
（v2 的 `wailsjs` 时代前端并不镜像这些结构）。当前的强制手段是 `scripts/verify-gen.sh`
端到端（生成页面必须过 vue-tsc），不是单元护栏——这一类的特征是**会出声**，符合铁律 5 的豁免。

## ⚠️ P12 dev 态页面纯空白：dev server 绑 IPv6、而 wails3 的代理强制 IPv4（护栏 15）

**症状**：`make dev` 打开后窗口一片空白，终端没有任何报错，`wails3 dev` 退出码 0。

**链路**：dev 态的页面内容不取自二进制里的 `frontend/dist`，而是走 wails3 的 dev 代理转发到
Vite dev server。这条链上有**两个**客户端，它们对 `localhost` 的解析方式不同：

| 客户端 | 位置 | 行为 |
| --- | --- | --- |
| preRun 健康检查 | `pkg/application/application_dev.go` | Go 默认 `http.Client`，按地址逐个试，`::1` 通即成功 |
| 代理 transport | `internal/assetserver/build_dev.go` | **硬编码** `dialer.DialContext(ctx, "tcp4", addr)` |

而 Vite 的 `server.host` 默认是 `'localhost'`，Node 在 macOS 上把它解析成 `::1` ——
于是 dev server **只监听 `[::1]:9245`**（`lsof` 实测；`curl -4 --noproxy '*' http://127.0.0.1:9245/`
连不上，`curl -6 'http://[::1]:9245/'` 回 200）。

结果：健康检查走 IPv6 → 成功 → 打印 `Connected to frontend dev server!`；
代理走 IPv4 → `connection refused`（重试 50×50ms ≈ 2.5s）→ 回给 webview 一个 **502** → 空白页。

**为什么难查**（这条失效的特殊之处是**两端都在报成功**）：

- `wails3 dev` 退出码 0，不报任何错；
- 那句 `Connected to frontend dev server!` 是**真的**——它确实连上了，只是走的是另一条路；
- 唯一线索是 `app.log` 里一行 `[ExternalAssetHandler] Proxy error ... dial tcp4 127.0.0.1:9245: connect: connection refused`，
  而它写的是 `localhost`，看不出「协议族」这个维度；
- 故障点（Vite 的绑定地址）与症状（webview 空白）之间隔着一层框架代理。

**这是上游缺口，不是本仓配错**：beta.23 的模板与全部 example
（`internal/templates/**`、`examples/*/frontend/vite.config.*`）**都没有设置 `server.host`**，
即官方脚手架在 macOS 上是同一个空转状态。

**处置**：`frontend/vite.config.ts` 显式 `server.host: '127.0.0.1'`。
不选「绑两个协议族」：那需要 `host: true`，会把 dev server 暴露到局域网；
而代理只走 IPv4，钉死 IPv4 回环正好是这条链需要的最小面。

**为什么值得加护栏**：它的症状（空白页）与原因相隔极远，且**链路两端都在报成功**——
这次是靠人工把 app.log 翻出来才定位的。护栏 15
（`TestViteDevServerBindsIPv4Loopback`）断言该文件里 `server.host` 不是默认值而是
`'127.0.0.1'`；刻意只做字面量断言、不做大括号配对解析（注释里的大括号会把配对带偏，
截断得出的「没有 host」是假阳性，比没有护栏更糟），但锚点 `server: {` 命中 0 处即 `Fatal`。

## D15 定 `frontend/bindings/` 入库策略与 dev 链路的验证入口

两件与 5.x 收尾相关的决定，都不改代码，但都会影响后来者的判断：

1. **`frontend/bindings/` 进 `.gitignore`。** 它是 `wails3 generate bindings` 的产物。
   迁移收尾时它既不入库也不被忽略，于是 5 个生成文件正漏在 `git status` 里，`git add -A` 会提交它们。
   入库的后果是无症状漂移：改了 Go 的签名却忘了重新生成时，`vue-tsc` 会拿旧 `.d.ts` 照过。
   代价是干净检出后必须先跑一次生成（`npm run build` 与 `make lint` 都依赖它存在）——
   该依赖已由 `internal/guard` 的绑定期望护栏显式表达，不靠人记。

2. **dev 链路目前没有可重复的验证入口。** 这次能定位 P12，靠的是手写脚本读 `app.log`。
   `make smoke` 验的是**构建产物**（走 `package` 路径、不设 `FRONTEND_DEVSERVER_URL`），
   它无论如何都覆盖不到 dev 代理这条链——两者是同一份代码的两条不同分支。
   要把它变成一级入口（`make verify-dev`），需要把「起 dev、轮询端口、读 app.log 断言 code=200」
   固化下来，并且必须能在无人值守下退出（现在 `wails3 dev` 是前台常驻进程）。
   先记为待办（tasks.md 8.5），不在本次范围内做。

---

## ⚠️ P13 `WAILS_MCP=1` 在 `wails3 package` 上**静默失效**（只有 build / dev 认它）

**现象**：在 darwin 上用 `WAILS_MCP=1 bash scripts/build.sh` 构建，编译命令仍是
`-tags production`，产物里查不到任何 MCP 常量——而**退出码 0、没有任何提示**。

**根因**（读上游源码定位，非猜测）：`internal/commands/task_wrapper.go` 里

```go
func Build(buildFlags *flags.Build, otherArgs []string) error {
    buildFlags.Tags = mergeTags(buildFlags.Tags, envTags()...)   // ← 这里读 WAILS_MCP
    ...
}
func Package(_ *flags.Package, otherArgs []string) error {
    return wrapTask("package", otherArgs)                       // ← 这里不读
}
```

`envTags()` 只被 `Build()` 调用。`wails3 dev` 则是**另一条**显式路径：`internal/commands/dev.go`
自己也读 `envTags()` 并把结果 `os.Setenv("EXTRA_TAGS", …)` 导出给子构建。

**影响面**：本仓 darwin 的交付路径走 `package`（P10），所以
`WAILS_MCP=1 make build` 在 macOS 上会产出一个**不含 MCP 的产物**，而且不会报错。
`make dev-mcp`（= `WAILS_MCP=1 wails3 dev`）不受影响，那才是本仓受认可的通道。

**处置**：不改行为（本就不需要 MCP 进交付产物），改**注释的边界**——
`Makefile` 的 `dev-mcp` 处写清这条通道只在 `build`/`dev` 上成立；
要造带 MCP 的产物得用 `WAILS_MCP=1 wails3 build`。

**顺带**：这条也修正了护栏 12 注释里「wails3 CLI 见到 WAILS_MCP=1 就追加 mcp 标签」的说法——
它对 `package` 不成立。诊断的代价是 27 秒的一次构建加一次读源码；若只看「退出码 0」
就会得出「MCP 剥离已经生效」的错误结论。

---

## ⚠️ P14 `production` 标签在 v3 由**本仓 Taskfile** 加，不是 CLI（护栏 16）

**归因变了，而且这是一条「约定」而非「保证」**：v2 的 `wails build` 自己加 production
构建标签；v3 的 `wails3 build` 只是 Taskfile 的薄封装，标签由 `BUILD_FLAGS` 在
`DEV!=true` 分支里加。

**上游模板把 `BUILD_FLAGS` 定义了 4 份**（`build/Taskfile.yml` 顶层一份 + 三个平台
Taskfile 的 build 任务内各一份，后者 shadow 前者）。这份重复是**上游自带的**，
刻意保留——与上游模板一致才能让 wails3 升级时的 diff 可读。故处置是「逐份校验」
而非「去重」。

**失效完全没有症状**：标签若丢失，`buildmode_production.go` 不参与编译、
`buildmode_dev.go` 顶上，于是 `debugBuild == true`：

- `DevToolsEnabled` 仍为 true —— 发布版给用户留着一个可改 DOM、可执行 JS 的调试入口；
- 日志级别仍是 Debug —— 噪音会快速吃掉 5MB×3 的轮转窗口，把真正的错误冲走。

两项都不报错、不崩溃，产物照常能跑。**护栏 16**（`TestProductionTagReachesBuildFlags`）
覆盖：标签名从 `buildmode_production.go` 的构建约束读出（不写第二遍），再要求每一份
`BUILD_FLAGS` 的**生产分支**用到它、**开发分支**不用它；`buildmode_production.go` 还必须
把 `debugBuild` 声明为 false。

**一个差点漏掉的假阳性**：最初用 `strings.Contains(prodBranch, "-tags "+tag)` 判断，
而 `"-tags production"` 里**包含**子串 `"-tags prod"`——于是「把标签改名成 `prod`」这种
最危险的漂移反而会被判为通过。改成抓出 `-tags` 的值、按逗号切分后逐项相等比对。
四种回归形态都做了破坏性验证（某平台丢标签 / 改标签名 / `BUILD_FLAGS` 全改名 /
`debugBuild` 改成 true），全部按预期变红。

---

## ⚠️ P15 macOS 最低系统要求是**编译目标下限**，不是可随手调的模板值（护栏 18）

v2 声明 `10.13.0`，v3 是 `12.0.0`。**这不是模板的疏忽，而是框架的下限**：

- 上游用测试把它钉死（`internal/commands/darwin_version_test.go` 断言必须是 12.0.0，
  并专门断言不许出现 10.15）；
- 它同时写在**两个地方**，且两处决定不同的事：

| 位置 | 决定 |
| --- | --- |
| `build/darwin/Taskfile.yml` 的 `CGO_CFLAGS`/`CGO_LDFLAGS`（`-mmacosx-version-min=12.0`）与 `MACOSX_DEPLOYMENT_TARGET: "12.0"` | 二进制**真正能跑**在哪 |
| `build/darwin/Info.plist` 的 `LSMinimumSystemVersion` | 系统**允许**在哪启动 |

**漂移的坏方向完全没有症状**：Info 低于二进制时 macOS 按 Info 放行，在旧系统上启动后
才崩溃——而这只发生在**用户机器**上，构建机与 CI 全绿。

**`Info.plist` 改不动**：它是 `common:update:build-assets` 的渲染产物（由上游的
`Info.plist.tmpl` 生成，`build.sh` 每次构建都会重跑），手改会被覆盖。
真要调下限必须动 Taskfile 的编译标志——只改 plist 是自欺。

**实测三方一致**：产物 `otool -l` 的 `LC_BUILD_VERSION` 是 `minos 12.0`，
`CGO` 标志是 `-mmacosx-version-min=12.0`，plist 是 `12.0.0`。
**护栏 18**（`TestMacOSMinimumVersionIsConsistent`）把这三处的一致性固定下来
（版本号归一到 major.minor，`12.0` 与 `12.0.0` 是写法差异、不是漂移）。
README 的系统要求措辞不做断言——散文断言会因改写误红，留 7.4 一起处理。

## ⚠️ P16 `wails3 mcp` 的 `wails_project_task_list` 在 Wails 模板工程上**恒为空**（不是报错，是空数组）

先分清两个 MCP：**应用内 MCP**（`-tags mcp` 编进产物，`127.0.0.1:9099`，能操作运行中的界面）
与 **项目 MCP**（`wails3 mcp` 单独起的进程，面向工程操作）。这里说的是后者。

`wails_project_task_list` 内部执行 `wails3 task --list --json`。而 go-task 的 `--list`
**只列出带 `desc:` 的任务**（这是它的定义行为：`--list` 的语义就是"列出有描述的任务"）。
Wails 模板体系的 Taskfile 一律用 `summary:` 而不用 `desc:`（本仓 `build/Taskfile.yml`
与 `build/darwin/Taskfile.yml` 沿用上游写法），于是：

| 命令 | 本仓实测结果 |
| --- | --- |
| `wails3 task --list --json` | `{"tasks": []}` ← MCP 工具走的就是这条 |
| `wails3 task --list-all --json` | 50 个任务，每个 `desc: ""` + 真实 `summary` |

**决定：不修。** 本仓的一级入口是 `make <target>`（AGENTS.md 铁律 4），根 `Taskfile.yml`
是实现细节而非对外契约；为了让一个只读工具能有输出而给 50 个任务补 `desc:`，是把
「单一真相」反过来写给工具看。**记录在案的价值**：它返回的是**空数组而不是错误**，
最容易被当成「这个工程没有任务」——下次别再花时间排查这个假故障。

同批实测的其它事实：`wails_project_task_run` / `wails_project_generate_bindings` 均可用
（返回 `jobId`，用 `wails_job_status` 轮询到 `completed`）。`wails3 mcp` 的安全边界与
应用内 MCP **不同**：它要求**全部请求**带 `Authorization: Bearer <token>`（不带即 `401`，
**空响应体**——这一点很坑：`curl -o` 会写出一个空文件，看起来像"服务没响应"，
实际是鉴权失败）；token 由服务启动时生成并打印在启动行；且它是**有状态会话**，
`initialize` 返回的 `Mcp-Session-Id` 需要回传（应用内 MCP 则是无状态、无鉴权）。

## ⚠️ P17 生成绑定与编译用的**标签集不同源**（今天无害，属潜伏坑）

`common:generate:bindings` 用 `-f '{{.BUILD_FLAGS}}'` 复用编译标志。复用本身没问题，
问题在 `.DEV` 的**可见性**：`darwin:build` 把 `DEV` 作为 **task 局部变量**传给
`common:build:frontend`，而 `generate:bindings` 是 `build:frontend` 的 **dep**，
**不接收任何变量**，只能看全局 `.DEV`——而全局 `.DEV` 没有默认值（本仓 `Taskfile.yml`
的 `vars` 只在 `EXTRA_TAGS`/`OBFUSCATED` 上写了「留空是刻意的」）。

实测两次（读构建日志即可复现）：

| 场景 | 绑定生成拿到的标签 | 二进制编译拿到的标签 |
| --- | --- | --- |
| `make dev-mcp` | `-tags production,mcp` | `-tags mcp`（DEV 分支 + EXTRA_TAGS） |
| `make build` | `-tags production` | `-tags production` |

即 **dev 态下生成器看到的是 `production` 分支**（`debugBuild=false`），而二进制是 dev。

**今天无害**：`debugBuild` 不参与任何绑定方法的签名（`buildmode_*.go` 里只有这一个常量），
两端产物一致。**但它是个潜伏坑**：一旦将来有绑定表面被标签门控，生成器与编译器就
「各看各的世界」，症状是前端类型与运行时不一致——正是本仓最不接受的那类静默失效。
留给后续处理（二选一）：给 `generate:bindings` 显式传 `DEV`；或在护栏里断言
「生成标签集 ⊇ 编译标签集」。**本次只记录，不改**——改 Taskfile 变量传递会牵动
三平台四份 Taskfile，属于独立的改动。

## ⚠️ P18 `wails3 dev` 的进程与 watcher 生命周期：**app 退出不带走 watcher，watcher 也不带走 app**

阶段 6 实跑撞了一次「假故障」，代价是半小时排查，值得写下来。

三个叠在一起的机制：

1. `application_dev.go` 的 `postQuit` 会明确打印「The application has terminated, but the
   watcher is still running / To terminate the watcher, press CTRL+C」——
   **关掉窗口 ≠ 结束 dev**。
2. watcher 的监督方向是**单向**的：它监听子进程、发现子任务没了会重跑 dev 任务；
   反过来，父进程（`wails3 dev`）被杀时，**子进程（task runner / app / vite）会变成
   孤儿继续活着并继续占端口**。实测：先停掉后台任务（`wails3 dev` 随之退出），
   4 个子进程全部存活，`9099`/`9245` 仍在监听。
3. **信号会被中介拦下**：当这些进程还挂在**一个仍在运行的后台任务**下时，
   `kill -9` 连杀 6 轮都无效（`kill` 返回 0，进程纹丝不动）；**先停掉那个后台任务**，
   再 `kill -9` 立刻生效。清理顺序是「**先停任务、再杀进程**」，不是反过来。

不干净的现场会导致下一次 `make dev-mcp` 出现**无报错的假故障**：

```
AssetServer Info: middleware=true handler=true devServerURL=http://localhost:9245
<此后再无任何日志：没有 preRun 的 "Waiting for frontend dev server to start..."，
 也没有 "MCP server started">
```

即**日志恰好停在 `application.New()` 返回之后**，看起来像「应用在 `Run()` 之前卡死」。
**实际根因是环境不干净，不是代码缺陷**——这也是当时误判的入口：`preRun` 的第一行日志
（`Waiting for frontend dev server to start...`）缺失，本是「没走到 `Run()`」的铁证，
但它同时也可以由「输出被半死的父进程吞掉」解释。**下次判据用这个**：
`app.log`（应用自己写的文件）与 stdout 是否一致——文件里也只有到 `New()` 为止。

**可靠收尾**：`pgrep -f 'wails3 (dev|mcp|task)|<APP>.dev.app|vite --port 9245'` 循环杀至空，
且必须确认 `lsof -iTCP:9099 -iTCP:9245` 无 LISTEN。这件事正是 8.5「dev 链路没有
可重复验证入口」的具体代价：无人值守的重放必须自带「冷启动前确认无残留 + 退出后确认清干净」。


