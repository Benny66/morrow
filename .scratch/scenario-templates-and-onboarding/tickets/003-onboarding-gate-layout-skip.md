# 003 — 引导启动判定、布局与跳过

**Blocked by**: 无

**状态**: 已完成（2026-10-21）

> 规格：B-1、B-2、B-3、B-8（门禁部分）、D-2、D-4、6.3、6.4、7.2 配置测试。

## 目标

干净安装且无场景时，主窗口进入 `/welcome` 占位向导："保留标题栏、无侧栏"，可跳过并持久化完成标记；已有场景的升级用户自动完成；宠物/强提醒窗口不被重定向。完成后可独立验证引导门禁与跳过。

## 涉及层

- [ ] 配置：`internal/config/config.go`
  - `OnboardingCompleted bool json:"onboarding_completed"`，默认 false；旧配置缺键自然 false。
- [ ] 逻辑层：`app.go`
  - `ShouldShowOnboarding() (bool, error)`：flag true → false；否则查场景；有场景 → 写 flag + Save（失败只记日志），返回 false；无场景 → true。
  - `CompleteOnboarding() error`：写 flag + Save；失败 `apperr.Wrap`。
  - 绑定重新生成。
- [ ] UI 层：
  - `router/index.ts`：`/welcome` 路由，`meta: { title: '首次设置', wizard: true }`；`RouteMeta` 增 `wizard?: boolean`。
  - `App.vue`：`bare` 分支不变；新增 wizard 分支（TitleBar + 居中内容，无侧栏）；其余走 AppShell。
  - `main.ts`：`router.isReady()` 后、`mount` 前；`meta.bare` 为 true 跳过；否则调 `ShouldShowOnboarding`；true 且不在 `/welcome` → `replace('/welcome')`；false 且在 `/welcome` → `replace('/')`；绑定失败 `logError` 后放行首页。
  - 新建占位 `frontend/src/views/Welcome.vue`：标题/说明 + 「跳过」按钮；跳过调 `CompleteOnboarding` 成功后 `router.replace('/')`；失败展示 `AppError` 并停留。占位页不包含三步业务。
- [ ] 测试：
  - config：默认 false、旧配置缺键、读写往返。
  - `ShouldShowOnboarding`：无场景 → true；有场景 + flag false → false 且 flag 落盘；flag true → false。
  - 手工 GUI：干净数据目录 → `/welcome`；无侧栏、标题栏可拖动/关闭；跳过 → 首页、重启不再出现；已有场景 → 直接首页；宠物/强提醒窗口不跳转；绑定失败放行。
- [ ] 文档：`docs/map.md` 路由/视图行在 004 完成后补；本票在实现记录中说明占位页。

## 验收标准

- 干净安装进入 `/welcome`，布局符合「保留标题栏、无侧栏」；宠物/强提醒窗口不受影响。
- 跳过写完成标记并进首页；重启不再出现；无场景也不会再进引导。
- 已有场景的升级用户自动补标记并进首页；标记写入失败只记日志，不把用户关进引导。
- 启动绑定失败时不阻塞应用，日志留痕。
- 本票不包含三步业务、模板预览或实例化。
- `make test` / `make lint` 通过；手工 GUI 记录。

## 实现记录（2026-10-21）

- config：`onboarding_completed` 默认 false；旧配置缺键自然 false。
- app.go：`ShouldShowOnboarding`（已有场景自动补标记并放行，落盘失败只记日志）、`CompleteOnboarding`。
- router / App.vue / main.ts：`/welcome` + wizard 布局（保留标题栏、无侧栏）；挂载前门禁，`meta.bare` 路由跳过；绑定失败放行首页。
- 占位 `Welcome.vue` 由 004 替换为完整三步。
- GUI：clean install 进入 `/welcome`；跳过 → 首页 + 标记 true；重启不再出现；已有场景 + 标记 false → 首页且自动补 true。
