# Wails v3 实战：一个桌面提醒客户端，从 v2 升到 v3 的全过程

> 项目地址：**https://github.com/Benny66/morrow**
> 技术栈：Go 1.25 + Vue 3 + SQLite，Wails 从 v2.15.0 升到 v3.0.0-beta.23

---

## 先说这是个什么项目

**Morrow 是一个桌面提醒客户端。**

它干的事很朴素：你在电脑前坐久了，它提醒你喝水、起来走走、看一眼远处。不是又一个手机 App，是装在你 Mac / Windows / Linux 上的一个桌面程序。

它有点特别的地方是：提醒不是靠弹窗，而是靠一只叫「小满」的桌面宠物——一滴水，头顶长了株芽，你每完成一次喝水或起身，芽就长一点。

之所以拿它当例子讲 Wails，是因为它几乎把桌面端的"麻烦需求"踩了个遍：

| 项 | 情况 |
|---|---|
| 形态 | 桌面客户端，单文件安装，没有服务端 |
| 平台 | macOS / Windows / Linux |
| 窗口 | 4 类：主窗、桌面宠物（透明、置顶、浮在所有窗口上）、全屏强提醒、副屏强提醒 |
| 系统能力 | 托盘常驻、原生通知、全局快捷键、录屏时自动隐身、防止重复启动 |
| 数据 | 全部存本地 SQLite，无账号、无云同步 |
| 代码量 | `main.go` 413 行，`app.go` 2682 行，`internal/` 162 个 Go 文件 |

这些需求，Hello World 一个都碰不到，但真做一个能用的桌面软件，你一定会碰到。

---

## 一、Wails 到底是个啥

### 一句话说清

**Wails 就是让 Go 开一个原生窗口，窗口里装个浏览器，然后 Go 的函数可以直接被网页里的 JS 调用。**

你写 Go 写后端，写 Vue / React 写界面，Wails 把两边接起来，最后打成一个可执行文件。

### 和 Electron 的区别

很多人一听"用网页写桌面软件"就想到 Electron。两者最大的不同是**浏览器从哪来**：

| | Electron | Wails | Tauri |
|---|---|---|---|
| 浏览器 | 自己带一个 Chromium | **用系统自带的** | 用系统自带的 |
| 后端语言 | Node.js | **Go** | Rust |
| 安装包大小 | 上百 MB | 十几 MB | 几 MB |
| 内存占用 | 高（Chromium 进程多） | 中 | 低 |
| 前端技术 | 什么都能用 | 什么都能用 | 什么都能用 |

### 优点（我真实感受到的）

**1. 就一个文件，不用装运行时。**
前端打包出来的 `dist` 用 `//go:embed` 塞进 Go 二进制里。发给用户就是一个 `.app` 或者 `.exe`，没有 `node_modules`，没有额外依赖。

**2. 后端就是 Go，不用写中间层。**
想读个文件、连个数据库，直接写 Go。不像 Electron 那样，主进程和渲染进程之间要来回发消息，写一堆 IPC。

**3. 该有的系统能力都有。**
托盘、通知、多窗口、全局快捷键、单实例锁——做桌面软件要用的东西，v3 基本齐了。

**4. 跨平台是真的跨。**
macOS 用系统 WebView，Windows 用 WebView2，Linux 用 WebKitGTK，一套代码三处跑。

### 缺点（也得说清楚）

**1. 浏览器版本不可控。**
用户系统里的 WebView 可能是三年前的版本。你写的 CSS，在你自己机器上好好的，到用户那儿可能就错位了。Electron 没这个问题，因为它自带浏览器。

**2. 有些功能得走"歪门邪道"。**
比如在 macOS 上让窗口真透明，得用苹果没公开的私有 API，还要在编译时加个特殊参数。这类事官方文档一句话带过，你得自己啃源码。

**3. 生态小，遇事只能自己查。**
Electron 报个错，搜索引擎一堆答案。Wails 报个错，可能全网就你一个。

**4. 文档跟不上代码。**
尤其 v3 还在 beta，文档经常落后源码半个版本。**最靠谱的资料来源是源码注释和 GitHub issue。**

一句话总结：

> **Wails 轻、干净、够用，但它不是"开箱即用"的框架，是"给你一副好骨架，血肉自己长"。**

---

## 二、我为什么要升级

这点得说实话，因为动机决定了后面所有取舍。

### 不是因为"版本号更高"

v3 在我升级那会儿还是 beta（`v3.0.0-beta.23`），生产项目上 beta 是有风险的。如果只是"新版本更好看"，我不会动。

### 真正的原因：v3 有个 MCP

MCP 是让 AI 直接操作应用的一套协议。v3 把它内置了，意思是：

**AI 可以直接驱动你正在运行的这个桌面程序**——点按钮、读页面元素、调 Go 函数、等事件发生，全程不用人写一行测试代码。

这个能力对我意味着什么？

以前 AI 帮我改完代码，验证方式是：我人肉点界面、截图、看日志、告诉它"行"或"不行"。改十次，我就得点十次。

现在 AI 改完能自己验收：

```
AI 改代码 → 重新构建 → 启动应用 → 调 Go 方法 → 读页面 → 断言成功
```

**这是"能力从无到有"，不是"效率提高一点"。** 而 MCP 只在 v3 里有，v2 永远不会加回来。

所以升级的理由只有一条：**等稳定版 = 无限期搁置这件事。**

### 顺带解决两个老问题

除了 MCP，v3 还顺手解决了 v2 时代两个憋屈的妥协：

**1. macOS 上终于有系统托盘了。**

v2 时代 macOS 没有托盘，原因很技术：Wails 自己占着 macOS 的 `NSApplication` delegate（一个全局回调入口），而所有第三方的托盘库也要抢这个位置。两个抢一个，结果是编译期符号冲突、运行时崩溃。

项目当初的解决办法是：macOS 上直接不做托盘，另外还 fork 了一个托盘库。

v3 把托盘做进框架了，冲突从根上没了。于是：**一个 no-op 文件、一个 fork 依赖、一段"为什么 macOS 不行"的注释，全删了。**

**2. 日志适配器可以删了。**

v2 的日志接口是自定义的，项目写了个 `WailsAdapter` 手工桥接到标准日志库。v3 直接用 Go 标准的 `*slog.Logger`，桥接层删掉。

### 升级的代价我怎么对冲

因为用的是 beta，我的做法是：

- **版本钉死**：`go.mod` 和依赖清单都写死 `v3.0.0-beta.23`，不用 `@latest`。
- **靠编译兜不住的，靠实跑兜**：API 改名这类问题，`go build` 会直接报错，不会漏。真正危险的是"编译过了但行为变了"——这类只能靠实际跑起来验证。
- **把"编译通过"和"实跑通过"分成两道关卡**，不合并。

这条纪律后面救了我好几次。

---

## 三、v2 和 v3 到底差在哪

差异不少，但真正影响你写代码的是这五处。

### 1. 启动方式：一调到底 → 三步走

**v2：** 一个函数搞定。

```go
wails.Run(&options.App{
    Title:  "Morrow",
    Width:  1024,
    Bind:   []interface{}{app},
})
```

**v3：** 拆成三步。

```go
// 第一步：建应用（服务、资源、日志、错误处理、单实例）
wailsApp := application.New(application.Options{...})

// 第二步：建窗口
window := wailsApp.Window.NewWithOptions(...)

// 第三步：跑起来
wailsApp.Run()
```

**为什么这么改：** 因为窗口不再只有一个。v2 那个写法默认"一个应用一个窗口"，v3 要支持多窗口，就得把"建应用"和"建窗口"分开。

**带来一个坑：** 第三步 `Run()` 一进去就会回调所有服务的启动方法。所以你要在 `Run()` **之前**把应用实例注入给业务代码，否则启动方法里用到了就 nil panic——用户看到的是"双击没反应"。

### 2. 绑定：`Bind` → `Services`

**v2：**

```go
Bind: []interface{}{app}
```

**v3：**

```go
Services: []application.Service{
    application.NewService(app),
}
```

看起来只是换了个名字，其实语义变了：

- 只有**导出的方法**会被暴露给前端，生命周期方法（`ServiceStartup` / `ServiceShutdown`）会被框架自动排除；
- 服务有了生命周期概念，不用再自己找地方初始化。

### 3. `runtime` 包没了，能力长在对象上

这是 v3 最核心的变化。

**v2：** 启动时存一个 `ctx`，之后干什么都把它传进去。

```go
runtime.WindowShow(ctx)
runtime.EventsEmit(ctx, "event", data)
runtime.MessageDialog(ctx, opts)
```

**v3：** 没有 `runtime` 了，能力挂在对象上。

```go
app.Window.GetByName("main").Show()
app.Event.Emit("event", data)
app.Dialog.OpenFile()
```

所以 `App` 结构体里那个 `ctx` 字段，换成了 `*application.App`：

```go
type App struct {
    app *application.App   // v2 这里是 ctx context.Context
    ...
}
```

**好处：** 依赖变显式了，能 mock，能分层。
**代价：** 你得自己想清楚"谁先创建、谁后创建"。

### 4. 日志：从自定义接口 → 直接用标准库

**v2：** 自定义 logger 接口，项目为此写了适配器。

**v3：**

```go
Logger: slog.Default(),
```

适配器删掉，代码变少。

**但这里埋了一个大坑**——后面第五部分会讲，production 构建下这个默认 logger **是往黑洞里写的**。

### 5. 构建：从 CLI 直调 → Taskfile 驱动

**v2：** `wails build` 一条命令，参数直接传。

**v3：** `wails3 build` 只是入口，真正的编译命令在 `build/<平台>/Taskfile.yml` 里。

**好处：** 构建逻辑可读可改。比如给 macOS 加私有 API 参数，改 Taskfile 就行。
**代价：** `-ldflags` 不再转发（版本号注入要靠这个），得自己接一条链。

### 一张表看完

| 项 | v2 怎么写 | v3 怎么写 | 影响 |
|---|---|---|---|
| 启动 | `wails.Run(&options.App{})` | `New` → `NewWithOptions` → `Run` | 得想清楚顺序 |
| 绑定 | `Bind: []interface{}{}` | `Services: []application.Service{}` | 语义变了 |
| 调框架 | `runtime.Xxx(ctx, ...)` | `app.Xxx()` | `App` 不再要 `ctx` |
| 日志 | 自定义接口 + 适配器 | `*slog.Logger` | 适配器删掉 |
| 错误 | `ErrorFormatter func(error) any` | `MarshalError func(error) []byte` | 前端不用改 |
| 托盘 | 第三方库，macOS 不行 | 内置，三平台都行 | 删依赖、删 no-op |
| 构建 | `wails build` | `wails3 build` + Taskfile | 参数不直传了 |
| 配置 | `wails.json` | `build/config.yml` | 文件名和格式都变了 |
| 单实例 | `UniqueId` | `UniqueID` | 大小写变了 |
| 绑定产物 | `wailsjs/go/main/App` | `frontend/bindings/<模块>/` | 路径变了 |

---

## 四、怎么升级

我分了 8 个阶段，每个阶段有明确的"出口"——**过了这个出口才能进下一步**。

关键纪律只有一条：

> **"编译通过"和"实跑通过"是两件事，不要合并。**

因为 API 改名这种问题编译时就会报；但"行为变了"编译时完全看不出来。

### 阶段 0：准备（别急着改代码）

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.23
wails3 version   # 必须是 beta.23，和 go.mod 对齐
```

然后做一件很重要的事：**开个新分支，先写个最小 demo 跑通**。

我用 `wails3 init` 生成了一个空项目，把几个不确定的点都试了一遍：多窗口怎么建、托盘怎么加、关闭窗口怎么拦、单实例怎么配。

**这一步花了两小时，省了后面两天。**

### 阶段 1：换依赖和配置

- `go.mod`：`wails/v2` → `wails/v3`，版本钉死
- 删掉第三方的托盘库
- `wails.json` → `build/config.yml`（注意版本字段从 `productVersion` 改名成 `version`，值是带引号的 YAML）
- 加 `Taskfile.yml`

### 阶段 2：改 Go 代码（到这里先只求编译过）

按这个顺序改，因为依赖是从下往上的：

1. `main.go`：启动改三步
2. `main.go`：`Logger` 换成 `slog.Default()`，`ErrorFormatter` 换 `MarshalError`
3. `app.go`：删 `ctx` 字段，改持 `*application.App`
4. `app.go`：所有 `runtime.*` 换成对象方法
5. `internal/apperr`：`Format` 改 `MarshalError`，返回 `[]byte`
6. `internal/dialog`：改成持有 `app`，调 `app.Dialog.*`
7. `internal/tray`：换成内置托盘，**删掉 macOS 的 no-op 分支**
8. 删掉日志适配器文件

出口：`go build ./...` 通过。

### 阶段 3：改前端

1. 跑 `wails3 generate bindings`，生成 `frontend/bindings/`
2. 删掉旧的 `frontend/wailsjs/` 目录
3. `lib/event.ts`：事件订阅改成 `Events.On`，载荷取 `ev.data`
4. `lib/log.ts`：日志出口改成调 log 服务
5. 所有 import 绑定路径的地方改路径

出口：`vue-tsc --noEmit` + ESLint + `npm run build` 全过。

**注意这一步比想象的大。** 我原以为只改 4 个文件，实际有 5 个文件 import 了旧路径，还有组件里调窗口 API 的地方因为同步变异步也要改。

### 阶段 4：改护栏

如果你的项目有静态检查（我有一堆），这一步不能漏。

因为护栏是靠解析源码做断言的，框架一换，它们解析的目标全变了：

- 原来找 `options.App`，现在要找 `application.Options`
- 原来看 `wailsjs/`，现在要看 `frontend/bindings/`

**漏改的症状是护栏误报**，不是漏报——所以还算安全。但如果护栏写成"找不到就跳过"，那就变成永远绿了。

### 阶段 5：改构建和发布链

1. `Makefile` 切 `wails3`
2. `scripts/build.sh`：**新增一步 `wails3 task common:update:build-assets`**
3. 版本注入改走 Taskfile 变量（`-ldflags` 不转发了）
4. CI 装 wails3 CLI，版本从 `go.mod` 读

第 2 条特别容易漏：v2 时代 `Info.plist` 是自动渲染的，v3 必须显式跑一条命令。**漏了的结果是：应用里显示新版本号，系统里显示旧版本号。**

### 阶段 6-7：接新能力、改文档

到这里应用已经能跑了，剩下的是把 MCP 接上（如果你要用）、更新 README 和架构文档。

### 我的实际节奏

| 阶段 | 干了什么 | 出口 |
|---|---|---|
| 0 | 装 CLI、写 demo、验证 6 个不确定点 | demo 能跑 |
| 1 | 依赖 + 配置骨架 | 目录结构就位 |
| 2 | Go 代码迁移 | `go build` 通过 |
| 3 | 前端迁移 | `vue-tsc` + 构建通过 |
| 4 | 护栏同步 | `make lint` 全绿 + 破坏性验证 |
| 5 | 构建链 | `make package` 出安装包 |
| 6 | MCP 接线 | Agent 能操作运行中的应用 |
| 7 | 文档 | README / 架构图更新 |

**其中阶段 0 和阶段 4 是最容易被跳过、也最值得做的。**

阶段 0 让你在干净环境里踩坑，不污染主分支。阶段 4 保证你那些静态检查在新框架下还真的在检查东西。

---

## 五、升级后踩的坑

下面 10 个，按"害我时间"排序。共同点是：

> **不报错、不崩溃、日志干净。只是某个功能默默没了。**

### 坑 1：production 构建下，日志直接扔进黑洞（最狠）

这条我放在第一个，因为它能让你所有的排查手段全部失效。

v3 的默认 logger，在 `production` 构建标签下是这样的：

```go
// pkg/application/logger_prod.go
func DefaultLogger(level slog.Leveler) *slog.Logger {
    return slog.New(slog.NewTextHandler(io.Discard, nil))
}
```

`io.Discard` 就是"扔掉"。不是"写到 stdout 你看不到"，是**连 stdout 都没有**。

所以：

- 你写的业务日志？如果没显式配 logger，全丢。
- 框架自己的错误日志？全丢。
- 前端日志？如果注册 log 服务时用了 `log.New()`（它走默认 logger），也全丢。

**最阴的是：日志接线看起来是接了的，代码也编译过了，但生产包里的日志是空的。**

我的教训是：日志这件事，护栏不能只检查"有没有注册"，得检查"注册时有没有显式传 logger"。

### 坑 2：关窗拦截，用错了 API 就静默失效

需求：点关闭按钮 → 隐藏到托盘，不退出。

我第一版这么写的：

```go
window.OnWindowEvent(events.Common.WindowClosing, func(e *application.WindowEvent) {
    window.Hide()
    e.Cancel()   // 想拦下"销毁窗口"
})
```

**窗口照常销毁。**

原因：v3 里有 hook 和 listener 两套机制。

| | `RegisterHook` | `OnWindowEvent` |
|---|---|---|
| 怎么跑 | 同步，按顺序 | 每个开个 goroutine 各跑各的 |
| `Cancel()` 有用吗 | 有用，立刻拦下 | 没用，抢不过框架自己的 listener |
| 适合干什么 | 拦截、改行为 | 旁观、听消息 |

框架自己在关窗事件上挂了个"销毁窗口"的 listener，你用 listener 永远抢不过它。

正确写法：

```go
window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
    if trayRunning() {
        window.Hide()
        e.Cancel()
        return
    }
    e.Cancel()
    wailsApp.Quit()   // 托盘没起来，就真退出
})
```

注意那个 `trayRunning()`——**必须是运行期状态，不能写 `runtime.GOOS == "darwin"`。**

因为托盘是在服务启动时创建的，可能失败；而关窗拦截是在 `main()` 里注册的，比服务启动还早。两者差一个阶段，**只有跑起来才知道托盘到底起没起来。**

### 坑 3：前端日志的通道被整个拿掉了

v2 时代，前端调 `runtime.LogInfo()`，消息自动进后端日志文件。免费的。

**v3 把这条通道删了。** 前端运行时里根本没有日志函数了。

替代方案是官方 log 服务，**必须手动注册**：

```go
Services: []application.Service{
    application.NewService(logservice.NewWithConfig(&logservice.Config{
        Logger:   frontendLogger,   // 必须显式传！
        LogLevel: logLevel,
    })),
}
```

不注册的后果：前端所有日志只剩 `console`。而打包后的应用**没有开发者工具**——等于没记。

用户说"点了没反应"，你打开日志文件，干干净净。

### 坑 4：macOS 托盘传空图标会直接 panic

不是返回 error，是 panic。

```go
// 必须有兜底
if len(icon) == 0 {
    icon = placeholderIcon()
}
```

这个坑有个好处：**它崩得很响**，启动就崩，你立刻能发现。所以我没给它加护栏——护栏的预算要留给"崩不了但悄悄坏掉"的问题。

### 坑 5：对话框没有同步返回值了

v2 的确认框是同步的：

```go
if dialog.Confirm("要删除吗？") { ... }
```

v3 没有同步返回，是回调式的。想保住原来的函数签名，只能自己用 channel 桥接：

```go
func Confirm(msg string) bool {
    done := make(chan bool, 1)
    d := app.Dialog.Question()
    d.SetTitle(msg)
    d.AddButton("取消").OnClick(func() { done <- false })
    d.AddButton("确定").OnClick(func() { done <- true })
    d.Show()
    return <-done
}
```

**这类"编译过了但语义变了"的改动，只能靠实跑发现。**

### 坑 6：绑定调用是数字 ID 寻址

v3 生成的绑定，前端调方法是按 **hash ID** 调，不是按方法名：

```ts
Call.ByID(1234567890)
```

方法改名、改包路径，ID 就变了。如果前端没重新生成绑定，**编译期完全看不出来**，运行时才报错。

好消息是这个 hash 是**可复算的**（FNV-1a）。所以我做了件事：护栏里自己复算一遍 ID，和生成产物逐个对账——当时全部命中。

### 坑 7：`wails3 build` 不转发 `-ldflags`

```bash
wails3 build -ldflags "-X main.Version=1.2.3"   # 不生效
```

实测只转发 `EXTRA_TAGS` / `GOOS` / `ARCH` 这几个，`-ldflags` 被吃掉。

版本注入只能走 Taskfile 变量：

```
scripts/build.sh → wails3 build VERSION=x → Taskfile 的 VERSION_LDFLAGS → 各平台 -ldflags
```

**漏了这条链的症状：** 安装包里版本对，应用里版本恒为 `dev`。因为是两个来源，一个对了一个没对。

### 坑 8：拷贝构建脚本时丢了 `OUTPUT` 变量

v3 迁移时照着官方模板抄平台 Taskfile，把输出路径写死成 `BIN_DIR/APP_NAME`，顺手把 `OUTPUT` 变量丢了。

后果分两种：

- macOS 的 universal 构建：两次编译（amd64 / arm64）输出到同一个文件，互相覆盖，`lipo` 合并时报错。
- Linux / Windows：**完全静默**——传了 `OUTPUT`，但产物落在别处，没人发现。

### 坑 9：dev 模式页面一片空白

`wails3 dev` 起来，窗口是白的，控制台没报错。

原因很无聊：前端 dev server 默认绑在 IPv6 的 `localhost`，而 wails3 的代理强制走 IPv4。两边对不上，页面加载失败。

**这类问题的排查成本极高**，因为"白屏"什么信息都不给你。最后是靠 `curl` 分别试 IPv4 和 IPv6 才定位到。

### 坑 10：版本号载体换地方了

v2：`wails.json`。
v3：`build/config.yml`，而且 `Info.plist` **必须显式渲染**：

```bash
wails3 task common:update:build-assets
```

漏了这一步，就会出现"应用内新版本、系统里旧版本"的双份真相。

---

## 六、怎么用好 v3

前面讲的是"升级怎么不踩坑"，这里是"日常怎么写才顺"。

### 1. `main()` 不是启动应用，是装配应用

v3 把能力对象化之后，`main()` 的职责变了：**按顺序把零件装好，然后交给框架跑。**

我把它分成三段：

```go
// 第一段：建应用（服务、资源、日志、错误处理、单实例）
// 第二段：建窗口（主窗、宠物窗、强提醒窗）
// 第三段：注入依赖 + Run
```

关键是第三段的第一句：

```go
appSvc.attach(wailsApp)   // 必须在 Run 之前
wailsApp.Run()
```

因为 `Run()` 一进去就回调服务的启动方法，那时候 `app` 得已经就位。

### 2. 窗口是并列的，别做子窗口

一旦你要做"浮在别的应用上面的东西"，就别想着"在主窗里开个子窗口"。

Morrow 的四个窗口全是平级的：

| 窗口 | 关键设置 | 为什么必须独立 |
|---|---|---|
| 主窗 | 正常窗口，macOS 保留交通灯 | — |
| 宠物窗 | 透明 + 无边框 + 置顶 | 要盖住别的应用，子窗口做不到 |
| 强提醒 | 全屏透明遮罩，启动就隐藏 | 要铺满屏幕压住一切 |
| 副屏强提醒 | 每块屏一个 | 同上 |

多窗口带来的问题是：**同一个前端包，要有不同的界面骨架。**

解法是用 hash 路由区分：

```go
URL: "/#/pet"   // 宠物窗口
```

这条路由在 `App.vue` 里标记为"裸页面"，跳过侧栏和标题栏。一份代码，三种壳。

**两个建窗的坑，都属于"看着能跑"：**

- **坐标有两套。** 建窗参数里的 `X/Y` 是"相对某块屏幕"的，`SetPosition()` 用的是"全局坐标"。混用之后，多屏用户每次启动宠物都被拽回主屏。我干脆不用建窗的 `X/Y`，统一在创建后 `SetPosition`。
- **`Hidden: true` 要写在建窗参数里**，不是创建完再 `Hide()`。后者会先闪一下。

### 3. 透明窗口：三个设置 + 一个编译参数

这是 macOS 上最容易做错的地方。

以为设一个 `BackgroundTypeTransparent` 就行？不行，你会看到宠物周围糊着一块底色。

**要四个条件同时成立：**

```go
application.WebviewWindowOptions{
    Frameless:        true,
    BackgroundType:   application.BackgroundTypeTransparent,
    BackgroundColour: application.RGBA{},   // 零值
    Mac: application.MacWindow{
        DisableShadow: true,
        Backdrop:      application.MacBackdropTransparent,
    },
}
```

第四个在代码外面：

```bash
# 只在 macOS 构建时加
EXTRA_TAGS="private_mac_apis"
```

缺一个的症状：

| 缺什么 | 你会看到 |
|---|---|
| `BackgroundType` | 整个窗口是个不透明方块 |
| `BackgroundColour` 零值 | 边上有一圈黑边 |
| `Mac.Backdrop` | 原生窗口透明了，但网页本身还是白的 |
| `private_mac_apis` | 上面那行配了也不生效，退化成上一条 |

最后一条最坑：**它是编译参数，代码里看不出来，编辑器不报错，`go build` 也过。**

所以有个通用经验：**遇到"配了但不生效"的选项，先怀疑编译参数。**

### 4. 约定好数据长什么样，两边照着来

这是我整个项目里最想推荐的做法。

`internal/` 下有三个包，什么业务都不做，只定义"数据长什么样"：

| 包 | 定义什么 | 前端对应 |
|---|---|---|
| `apperr` | 错误长这样：`{code, message, detail}` | `lib/invoke.ts` |
| `event` | 事件名格式 `<模块>:<动作>` + 载荷 | `lib/event.ts` |
| `page` | 分页请求和返回 | `lib/page.ts` |

**它的价值在框架换代那天才显现。**

举个例子。v2 的错误处理有个著名的坑：Go 返回对象，前端拿到的是 `"[object Object]"`，结构化信息全丢。所以 v2 时代 Go 侧**必须返回字符串**。

v3 改成了 `MarshalError func(error) []byte`，框架自己序列化。

**结果：Go 侧改了一处，前端 `normalizeError` 一行没动。**

因为前端拿到的**还是"一个 message 里装着 JSON 的 Error"**，只是这个 JSON 现在由框架保证格式。**约定没变，实现变了。**

这就是把"约定"单独抽出来的好处：换框架时，改的是接线口，不是所有使用方。

代价是前端要维护一份镜像常量。我的做法是用护栏双向校验——改了 Go 不同步前端，`make test` 就红。

**一条纪律：镜像只许复制"常量和类型"，不许复制"逻辑"。** 同一条规则两个实现，比一个常量两份更糟。

### 5. 把"不报错的问题"变成会红的检查

前面十个坑，一半以上属于同一类：**坏了不吭声。**

这类问题没法靠 code review，也没法靠文档。唯一可靠的办法是：**写成自动检查，让它变红。**

我在 `internal/guard/` 里放了 20 多个检查，全是 `go test` 形式，跑在 `make test` 里。它们用 Go 的 AST 解析自己的源码，检查"该接的线接了没有"：

| 检查什么 | 不检查会怎样 |
|---|---|
| `Options.Logger` / `Assets` / `MarshalError` 都接了 | 框架错误无人可见 |
| log 服务显式注册了，且传了 logger | 前端日志全丢 |
| `-ldflags` 注入的变量真实存在 | 应用内版本恒为 `dev` |
| `mcp` 标签没进默认构建 | 生产包多开一个本机端口 |
| 关窗逻辑是"托盘在→隐藏、托盘不在→退出" | 关窗后回不来 / 留下僵尸进程 |
| macOS 应用菜单挂上了 | 输入框复制粘贴失效 |

**但这里有一条铁律，比检查本身重要：**

> **检查本身要能发现"自己什么都没查到"。**

如果检查是靠正则或 AST 找代码，一旦代码换个写法，它找不到东西了——这时候必须**报错**，不能当作"通过"。

否则检查会从"拦问题"退化成"永远绿的摆设"，还给你一种"很安全"的错觉。

我踩过两个反例：

1. **解析范围必须等于书写范围。** 我的绑定检查只解析 `app.go`。于是"所有绑定方法必须写在 `app.go`"变成了项目铁律——不是框架要求，是护栏只认这个文件。写到别处能编译、能生成，但护栏会误报。
2. **只检查第一个匹配项 = 脆弱。** 我的关窗检查只解析 `main.go` 里第一个关窗钩子，于是"钩子注册顺序"变成了不能改的约定——强提醒窗口的钩子必须排在主窗之后，否则护栏抓错对象。

**检查不是免费的，每条都在给未来加约束。** 但比起"用户投诉时你打开日志发现什么都没有"，这笔买卖划算。

---

## 七、值不值得升

### 该升的情况

- 你在用 v2，且**macOS 需要托盘**——光这一条就够
- 你想要 **AI 能直接操作你的应用**（MCP）——只有 v3 有
- 你需要**多窗口**（悬浮窗、覆盖层、多屏）
- 你不介意花两三天做迁移和验证

### 别升的情况

- 你的应用就是个单窗口工具，v2 跑得好好的
- 你受不了 beta 的 API 变动风险
- 你没有自动化测试/检查，升级后没法验证"行为没变"

### 关于 beta 的实话

我用的是 `v3.0.0-beta.23`。beta 意味着：

- API 还会动
- **文档永远落后源码半个版本**
- 遇到问题，GitHub issue 比文档有用

三个月下来最实用的一条经验：

> **读源码注释，比读文档有用。**

前面那些"为什么不能这么写"的结论，大部分不是文档里抄的，是翻 `webview_window_darwin.m`、看 `HandleWindowEvent` 的实现、读上游 issue #4389 看出来的。

---

## 最后三句

1. **Wails 是"Go + 系统浏览器"，轻、干净，但不是开箱即用。** 框架该给你的可观测性、构建链、错误处理，得自己补。

2. **v3 白给的好处是平台能力（托盘、多窗口、对象化 API），要你付的成本是接线责任（日志、事件、版本注入、应用菜单）。** 好处你会自动享受到，成本不会——**因为它们全都不报错**。

3. **所以真正的经验不是"记住 10 个坑"，是养成一个习惯：凡是"不做也不会报错"的事，都写成会红的检查。** 那条检查可能就 20 行，但它替你记住了三个月后一定会忘的事。

---

*项目开源在 https://github.com/Benny66/morrow ，欢迎来看代码（注释比代码多，那是有意的）。*
