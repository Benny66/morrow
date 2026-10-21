# 002 — 场景页模板库与一键创建

**Blocked by**: 001

**状态**: 已完成（2026-10-21）

> 规格：A-1、A-2、A-4、A-5、A-6、C-5～C-7（全量路径）、D-1、D-3、D-5、6.2、6.3、7.2。

## 目标

用户在场景页看到 5 个模板，可展开完整提醒预览，一键创建一套完整模板副本（默认停用、无需 AI Key）。副本与自建场景同权；完成后可独立演示。

## 涉及层

- [ ] 数据层：`internal/service/scenario.go`（或同包新文件）
  - `ListScenarioTemplates() scenariotemplate.Catalog`。
  - `ApplyScenarioTemplate(key, workStart, workEnd, goals, enable)`：
    - `Find` → `Personalize` → `Valid` 必须 true；
    - 事务：若 `enable` 先停用现有启用场景；创建场景（Name / Description / Summary / GeneratedAt / Enabled=enable）；按顺序创建提醒（Enabled=true）；提交；
    - 全量路径：空 options → 全部五类；本票 UI 固定 `enable=false`；
    - `ApplyTemplateResult` 含 scenario / reminders / next_fire_at；
    - 更新 `Scenario.Summary` / `GeneratedAt` 注释（不再限定 AI）。
  - 错误：模板不存在 → `apperr.NotFound`；输入非法/缺失目标 → `apperr.Validation`；DB → `apperr.Wrap`。
- [ ] 逻辑层：`app.go`
  - `ListScenarioTemplates` / `ApplyScenarioTemplate` 绑定直通 service。
  - `ApplyScenarioTemplate` 成功后 `emitReminderChanged(ReasonPlanChanged)`。
  - 绑定重新生成（`make gen` / `make dev` / `make build` 走生成器）。
- [ ] UI 层：`frontend/src/views/Scenarios.vue`
  - 顶部固定「从模板开始」区，位于「新建场景」之前；始终展示（有/无场景都显示）。
  - 卡片：名称、摘要、类型与条数；可展开完整提醒清单（标题/类型/间隔/时段）；使用按钮 loading。
  - 使用模板调 `ApplyScenarioTemplate(key, '', '', [], false)`；成功后刷新列表并提示「已创建，点启用开始提醒」。
  - 空状态文案提示「从上方模板开始」与「手动描述」两条路径。
  - 类型标签复用 `lib/reminder.ts` 的 `kindMeta`；不写死模板内容。
  - 与现有 `useEvent(ReminderEvent.Changed)` 刷新兼容。
- [ ] 测试：
  - service：全量创建（无 AI 配置）→ 场景 + 5 条提醒、默认停用、Summary/GeneratedAt、SortOrder；重复创建 → 两个独立副本；`enable=true` 停用其他场景（为 004 预留）；非法 key/options、缺失目标被拒；`NextFireAt > 0`。
  - 前端 `vue-tsc` / ESLint。
  - 手工 GUI：模板区、展开预览、一键创建、重复创建、默认停用、无 Key；现有新建/AI 路径无回归。
- [ ] 文档：`docs/map.md` 的 service 行补充场景模板；必要时补绑定说明。

## 验收标准

- 场景页顶部稳定展示 5 个模板；卡片信息与展开预览来自绑定，不硬编码。
- 点击使用 → 创建一个普通场景 + 5 条提醒，默认停用；列表刷新并提示。
- 未配置 AI Key 也能完成；场景卡片显示摘要/时间与「重新生成」。
- 重复使用生成两个独立副本；删除副本不影响模板。
- 现有新建场景、AI 生成、计划页编辑无回归。
- `make test` / `make lint` 通过；手工 GUI 审计记录。
- 本票仅消费全量路径；个性化路径的 service 逻辑已实现并被测试，UI 留给 004。

## 实现记录（2026-10-21）

- service：`ListScenarioTemplates` / `ApplyScenarioTemplate`（事务、默认停用、enable 时停用其他场景）；`ApplyTemplateResult` 含下一次计划触发。
- app.go：List / Preview / Apply 三个绑定；Apply 成功后发 `reminder:changed`（reason=plan_changed）。
- Scenarios.vue：顶部固定模板区、展开完整预览、一键创建完整副本、默认停用提示；展示工具抽到 `lib/template.ts`。
- 测试：`internal/service/scenario_template_test.go` 覆盖全量/个性化/重复副本/缺失目标/预览不落库/未知 key/非法入参。
- GUI（Wails MCP + 临时 HOME）：5 张模板卡；展开会议密集日完整 5 条；创建护眼优先完整副本（5 条，默认停用）；启用后久坐办公被停用。DB：2 场景 / 7 提醒。
- 审查后补：场景页「刷新」同时重载场景与模板并清掉旧错误；模板摘要时间统一归一化为 HH:MM。
