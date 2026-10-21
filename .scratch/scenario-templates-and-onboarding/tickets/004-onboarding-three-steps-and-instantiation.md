# 004 — 三步向导与模板个性化实例化

**Blocked by**: 001、002、003

**状态**: 已完成（2026-10-21）

> 规格：B-4～B-7、B-9、B-10、C-1～C-7（个性化路径）、D-3、D-5、6.2（预览）、6.5、7.2（个性化）、7.4。

## 目标

完整首次引导：工作时段 → 恰好两个关注目标 → 选模板并实时预览 → 自动启用的过滤/裁切计划 → 完成页（条数、下一次计划提醒、查看计划/开始使用）。全程不检查 AI、不调用 AI。

## 涉及层

- [ ] 逻辑层：
  - service `PreviewScenarioTemplate(key, workStart, workEnd, goals)`：`Find` → `Personalize` → 用 `s.now()` + `reminder.NextFireAt` 填充；不写库。
  - app 绑定 `PreviewScenarioTemplate`。
  - `ApplyScenarioTemplate` 个性化路径（002 已实现）：过滤/裁切/夹取、`enable=true`、停用其他场景、Summary 追加「（保留 N 条 · HH:MM–HH:MM）」。
- [ ] UI 层：`frontend/src/views/Welcome.vue`（替换 003 占位）
  - 状态机 `step: 1|2|3|done`；第 1–3 步右上角「跳过」；第 2/3 步可「上一步」。
  - 第 1 步：两个 HH:MM 选择器，默认 09:00–18:00；开始必须早于结束；不合法禁用下一步。
  - 第 2 步：五类目标（water / stand / move / rest / toilet）恰好选 2 个，显示 2/2；不足/超出禁用下一步。
  - 第 3 步：5 个模板卡片；选中后调用预览；展示保留提醒、每类条数、下一次计划提醒；缺失目标明确标出并禁用完成；工作时段/目标变化时重新预览。
  - 完成：先 `ApplyScenarioTemplate(..., enable=true)`；成功后再 `CompleteOnboarding`；若标记失败，保留已创建结果，重试只写标记，不重复创建；进入完成页。
  - 完成页：场景名、保留 N 条、下一次计划提醒；「查看计划」→ `/plan?scenario=<id>`；「开始使用」→ `/`。
  - 错误用现有 `invoke` / `AppError`；不读取 AI 设置。
  - 模板卡片样式尽量复用 002；不在两个页面复制内容。
- [ ] 测试：
  - service：预览不写库、`NextFireAt > 0`、缺失目标 `Valid=false`；个性化创建只保留所选两类、窗口裁切、目标夹取、自动启用、停用其他场景、Summary 后缀。
  - 手工 GUI（spec 7.4）：非法时段、0/1/3 目标、预览随选择变化、窄时段缺失目标、完成页与两个入口、首页已启用场景、跳过、中途关闭回第一步、无 AI Key、stand/move/rest 进入休息模式。
- [ ] 文档：`docs/map.md` 增加 `Welcome.vue`。

## 验收标准

- 无 AI Key、离线，2 分钟内完成三步并得到一套启用中的计划。
- 预览与落库一致：保留的提醒类型/窗口/条数与完成页一致；缺失目标不能完成。
- 完成页信息正确；「查看计划」定位到该场景；「开始使用」回首页看到已启用场景。
- 跳过与中途退出语义正确；完成标记失败可重试且不重复创建。
- 模板副本可编辑；stand/move/rest 触发休息模式。
- `make test` / `make lint` 通过；手工 GUI 验收记录。

## 实现记录（2026-10-21）

- service：`PreviewScenarioTemplate`（不写库，填充 `NextFireAt`）；绑定已加。
- Welcome.vue：三步 + 完成页；默认 09:00–18:00、恰好两个目标、实时 Go 预览、缺失目标禁用完成、自动启用、完成标记失败可重试且不重复建计划。
- lib/template.ts：提醒日程 / 类型标签 / 下一次计划提醒格式化；Scenarios.vue 复用。
- GUI：喝水+放松 + 久坐办公 → 2 条提醒、自动启用、完成页下一次 15:15；计划页显示过滤后的喝水/放松与窗口；17:00–17:30 窄时段 → 走动缺失、完成禁用、喝水裁切为 17:00–17:15、目标夹到 1。
- 未等待真实休息倒计时；rest/stand/move 的休息模式由现有 restmode 测试覆盖。
