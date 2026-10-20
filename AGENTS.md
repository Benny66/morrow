# AGENTS.md — 跨端通用铁律

本文件是 AI 与人在本仓库工作的最高约束。它只写**跨端通用**的铁律，
域内规范见根目录 `CLAUDE.md`（Go）与 `frontend/CLAUDE.md`（Vue）。

> 标记约定：带 `⚙️` 的规则由机器强制（护栏/lint/测试），违反会在
> `make test` / `make lint` 阶段变红；不带 ⚙️ 的为软约束，靠 code review 守护。

## 技术栈与结构

- Wails v2（Go 后端 + Vue3 前端），锁定版本见 `go.mod` 与 `frontend/package.json`。
- Go 侧根目录为 `main.go`/`app.go`，业务分层在 `internal/` 内；Vue 侧在 `frontend/`。
- 代码导航见 `docs/map.md`——先读它再动手，禁止 `find` 全库盲扫。

## 铁律

1. **单一真相（Single Source of Truth）** ⚙️
   一条规则只允许有一个出处，其余消费者共同引用，禁止复制粘贴。
   - 模型清单唯一真相在 `internal/model/model.go` 的 `AllModels()`。
   - 依赖清单唯一真相在 `deps.yaml`。
   - 应用数据目录（数据库/配置/日志/崩溃日志）唯一真相在 `internal/appdir`。
   - 管道契约常量（错误码 / 事件动作 / 页大小上下界）的前端镜像由
     `internal/guard/parity_test.go` 双向校验：改了 Go 不同步前端即红。
     **镜像只许复制常量与类型，不许复制逻辑**——一件规则两个实现比一个常量两份更糟。

2. **依赖登记制** ⚙️（由 `internal/guard/deps_test.go` 双向校验）
   新增依赖不能只 `go get` / `npm install`，必须登记并附理由。
   护栏做双向校验：清单里每个直接依赖必须登记，登记项必须真实存在。

3. **干净性** ⚙️
   运行时产物不得入库：`*.db`、`node_modules/`、`frontend/dist/`、`build/bin/`、
   `*.exe`、日志文件等（见 `.gitignore`）。提交前 `make test` 与 `make lint` 必须通过。

4. **验证入口统一** ⚙️
   只记 `make <target>`，不记零散脚本路径。`make dev/build/test/lint/smoke/package/gen`。
   `make lint` = gofmt + Go 护栏 + `go vet` + ESLint + `vue-tsc` 类型检查，
   是提交前唯一需要的静态验证入口。

5. **护栏必须"感知自己瞎了"** ⚙️（由 `internal/guard/` 各护栏落实）
   任何用正则/AST 解析源码做断言的护栏，解析到 0 个结果必须报错，
   而非当作"通过"静默放行。附一句"写法可能已变更，请同步更新护栏解析规则"。

   推论：**没有任何症状的失效必须有护栏**。接线漏了、注入路径写错、镜像漂移——
   这类问题不报错、不崩溃，只是某条链路默默断掉（如不接 `options.App.Logger`
   则前端日志全部丢失）。凡属此类，都要编译成会红的检查
   （见 `internal/guard/wiring_test.go`）。

6. **分层纪律** ⚙️（由 `internal/guard/layer_test.go` 强制）
   `app.go` 绑定方法层不得 import gorm/database（经 service 层）；model 层是叶子；
   每个带 BaseModel 的结构体必须登记进 `AllModels()`。

7. **前端 import 安全与禁硬编码** ⚙️（由 `frontend/eslint.config.js` 强制）
   渲染层禁 import node 能力（走 bindings）；禁硬编码 hex 色值（引 tokens）。

8. **中文注释**
   项目编码语言为中文，注释使用中文。提交前移除 `console.log` 与调试代码。

## 目录命名

- Go 包目录：小写、单数、简短（`model`、`database`、`service`）。
- Vue 页面目录：kebab-case（`views/system/user/`）；组件文件 PascalCase（`UserList.vue`）。
- Pinia 状态目录统一用 `stores/`，禁止 `store/` 单复数混用。
