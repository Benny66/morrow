# docs/map.md — 代码导航地图

> 目的：告诉 AI「哪类代码在哪个文件」，先读这里再动手，避免 `find` 全库盲扫。

## 顶层

| 路径 | 内容 |
|---|---|
| `main.go` | 应用入口，wails 启动配置（窗口/绑定/assets/错误格式化） |
| `app.go` | App 结构体与绑定方法（前端可直接调用的 Go 方法都在这） |
| `pet_window.go` | 桌面宠物窗口的建窗参数与尺寸换算（透明 + 置顶 + 位置还原） |
| `pet_bubble.go` | 宠物气泡扩窗的窗口接线（工作区选择、扩窗状态、还原、持久化反算） |
| `pet_bubble_geometry.go` | 气泡扩窗的几何纯函数（底边锚定 / 工作区夹取 / 基础矩形反算） |
| `AGENTS.md` | 跨端通用铁律 |
| `CLAUDE.md` | Go 域规范 |
| `frontend/CLAUDE.md` | Vue 域规范 |
| `Makefile` | 统一命令入口 |
| `wails.json` | wails 打包/构建配置 |
| `deps.yaml` | 依赖登记清单（依赖唯一真相，护栏双向校验） |

## Go 侧（`internal/`）

| 路径 | 内容 |
|---|---|
| `internal/model/` | 数据模型 + `AllModels()` 注册表（模型唯一真相） |
| `internal/database/` | gorm + SQLite 连接、模型迁移执行（含 reminders 旧表重建迁移） |
| `internal/service/` | 业务服务层（绑定方法经此访问数据库；`reminder.go` 提醒 CRUD、`quickadd.go` AI 快速添加、`pet.go` 宠物素材包） |
| `internal/appdir/` | **单一真相**：应用数据目录（数据库/配置/日志/崩溃日志同目录） |
| `internal/appinfo/` | 版本/平台/关键路径（版本注入目标见 `scripts/package.sh`） |
| `internal/config/` | `config.json` 读写（主题 + 窗口几何 + 提醒行为 + 桌面宠物） |
| `internal/logging/` | 日志初始化（文件轮转 + 分级）+ Wails logger 适配器 |
| `internal/dialog/` | 原生对话框封装 |
| `internal/reveal/` | 在系统文件管理器中打开目录（跨平台） |
| `internal/tray/` | 系统托盘（macOS 为 no-op，见其包注释） |
| `internal/crash/` | panic 落盘（供业务显式包裹长驻 goroutine） |
| `internal/apperr/` | **契约**：错误协议（code/message + `Format`） |
| `internal/page/` | **契约**：分页协议（`Request` / `Result[T]`） |
| `internal/event/` | **契约**：事件协议（`<domain>:<action>` + payload） |
| `internal/reminder/` | 本地提醒调度引擎（`schedule.go` 纯时间计算 + `engine.go` 调度循环 + 一次性提醒全局扫描 + `contract.go` 接口与事件载荷） |
| `internal/ai/` | OpenAI 兼容的最小客户端（计划生成 + `quickadd.go` 一次性/周期草稿解析；`parse.go` 容错 JSON） |
| `internal/guard/` | 架构护栏（以 go test 形式运行，含契约护栏与**前端镜像一致性**护栏） |

## 前端（`frontend/src/`）

| 路径 | 内容 |
|---|---|
| `main.ts` | 前端入口 |
| `App.vue` | 根组件 |
| `components/` | 通用组件（TitleBar 等） |
| `layouts/` | 布局（AppShell：标题栏 + 侧栏 + 内容区） |
| `stores/` | Pinia 状态 |
| `router/` | 路由（hash 模式） |
| `lib/invoke.ts` | **契约**：绑定调用包装（错误归一化 + 耗时采样，镜像 Go 的错误码） |
| `lib/error-report.ts` | **契约**：异常上报（全局兜底 + Vue 错误钩子） |
| `lib/event.ts` | **契约**：事件协议前端镜像（事件名 / payload / `onEvent` 订阅） |
| `lib/page.ts` | **契约**：分页协议前端镜像（类型 / 页大小常量） |
| `lib/reminder.ts` | **契约**：提醒领域前端镜像（事件名 / 载荷 / kind 枚举与展示元数据） |
| `lib/attachment.ts` | 附件展示常量与文件名/类型判断（真正限额在 Go 侧 `ai_input.go`） |
| `lib/pet.ts` | 宠物渲染的单一真相（图集布局 / 动画行序 / 帧率；与素材约定对齐，无 Go 侧镜像） |
| `lib/app.ts` | **契约**：应用级事件（`onSecondInstance`）与启动参数 |
| `lib/log.ts` | **契约**：前端日志出口 + 全局错误兜底（唯一正确出口） |
| `composables/` | 组合式函数（`useEvent` 自动解绑、`usePagedList` 列表加载） |
| `styles/` | 设计令牌与主题（tokens / theme / element 覆盖） |
| `views/` | 页面（`make gen` 生成的模块页面落在此处；`Home.vue` 含快速添加/即将到来，`Plan.vue` 计划维护，`Scenarios.vue` 场景与 AI 计划，`Pet.vue` 宠物裸页面） |

## 模板与工具

| 路径 | 内容 |
|---|---|
| `_example/` | 模块生成器模板 + 契约用法范例（**非产品页面**） |
| `scripts/set-version.sh` | **版本写入唯一实现**（本地发布与 CI 发布共用） |
| `scripts/build.sh` | **版本注入唯一入口**：读 `build/config.yml` 的 `info.productName` / `info.version`，同时喂给产物与 `-ldflags` |
| `.github/workflows/` | `build.yml`（检查 + 编译矩阵）、`release.yml`（tag 触发自动发布） |
| `scripts/gen.sh` | 模块生成器（`make gen name=<module>`） |
| `scripts/verify-gen.sh` | 生成器端到端验证（`make verify-gen`） |
| `scripts/package.sh` | 打包（dmg / NSIS 安装器） |
| `scripts/release.sh` | 版本号 + 打包 + release 说明 |
| `scripts/smoke.sh` | 冒烟测试（构建 → 启动 → 断言 → 清理） |
| `build/darwin/` | macOS 打包资源（Info.plist、dmg 背景模板） |
| `build/windows/` | Windows 打包资源（manifest、NSIS 脚本） |

## 自动生成（勿手改）

| 路径 | 内容 |
|---|---|
| `frontend/bindings/` | wails 自动生成的 Go↔TS 绑定，由 `wails3 generate bindings -clean=true -ts -i` / `make dev` / `make build` 重新生成 |
| `frontend/dist/` | 前端构建产物 |

## 规约与规格

| 路径 | 内容 |
|---|---|
| `openspec/specs/` | 能力规格基线（唯一真相） |
| `openspec/changes/` | 进行中的变更提案 |
| `docs/specs/` | `to-spec` 产出的规格说明书（供拆任务） |
| `docs/competitive-analysis.md` | 第一轮竞品差异与基础能力补齐的取舍（2026-10） |
| `docs/competitive-analysis-round2.md` | 第二轮竞品差异、用户痛点证据与功能候选（2026-10） |
| `docs/specs/2026-10-20-baseline-parity.md` | 本轮规格：智能免打扰 + 场景化休息模式 + 桌面集成（P0/P1） |
| `docs/specs/2026-10-21-uninterrupted-slice.md` | 「不被打断」切片规格：演示模式 + 自然停顿顺延 |
| `docs/specs/2026-10-21-pet-bubble-expansion-and-refresh.md` | 宠物气泡扩窗 + 打卡刷新 + 休息结算规格 |
| `docs/adr/` | 不可逆决策记录（如一次性提醒独立于场景） |
| `CONTEXT.md` | 领域术语表（一次性 / 每日单点 / 快速添加等） |
| `docs/代码规范.md` | 命名与管道契约的编码约定 |
| `docs/脚手架搭建经验.md` | 基座方法论（从零搭新基座时读） |
