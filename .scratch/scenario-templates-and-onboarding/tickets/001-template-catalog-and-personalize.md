# 001 — 模板目录与个性化纯函数（预重构）

**Blocked by**: 无

**状态**: 已完成（2026-10-21）

> 规格：A-1、A-3、C-1～C-4、5.1～5.4、6.1、7.1。

## 目标

建立场景模板的唯一出处与可单测的个性化纯函数：5 个模板、目录加载、查找、按关注目标过滤、按工作时段裁切、每日目标夹取、缺失目标计算。本票不接线、不写库、不改 UI；完成后 002/004 直接消费稳定靶心。

## 涉及层

- [ ] 逻辑层：新建 `internal/scenariotemplate`
  - 类型：`Reminder` / `Template` / `Catalog` / `Options` / `Preview`（按 spec 6.1）。
  - `LoadCatalog()`：5 个模板，`Goals` 为 water / stand / move / rest / toilet；返回防御性副本。
  - `Find(key)`：按键查找。
  - `Personalize(t, opts)`：
    - 无 goals = 全部五类；非空必须恰好 2 个、互不相同、合法。
    - 无工作时段 = 不裁切；只给一个或窗口非法 → validation 错误。
    - 取窗口交集；无交集/交集不足一个触发点丢弃。
    - `DailyTarget` 夹到窗口内最大触发次数（复用 `reminder.FirePoints`，固定日期只取点数）；`interval_min=0` 视为 1 次。
    - 计算 `MissingGoals` 与 `Valid`；缺失目标不返回 error。
  - 模板内容严格按 spec 5.1～5.3；`SortOrder` 按 water→stand→move→rest→toilet。
  - 不读时钟、不写库、不 import service。
- [ ] 数据层：无。
- [ ] UI 层：无。
- [ ] 测试：`internal/scenariotemplate/template_test.go`
  - 目录恰好 5 个（0 个必须失败）；key 唯一；名称/描述/摘要非空。
  - 每条提醒 kind 合法、标题 ≤50 字、正文 ≤200 字、窗口合法、间隔/目标非负；每个模板覆盖五类。
  - 每个模板 × 每个目标二元组 × 09:00–18:00 → `len ≥ 2`、`MissingGoals` 空、`Valid=true`、kind 在目标内。
  - 裁切：10:00–11:00、12:00–13:00 → 窗口落在交集内、目标 ≤ 次数。
  - 缺失目标：03:00–04:00、17:00–17:10 → `Valid=false`、`MissingGoals` 精确。
  - 校验：单边时段、end≤start、1/3 个目标、重复/非法目标 → error。
  - 无目标/无时段 → 原样返回全部提醒。
  - 表内次数/目标与 spec 5.2 一致（可表驱动断言）。
- [ ] 文档：`docs/map.md` 增加 `internal/scenariotemplate/` 行。

## 验收标准

- `go test ./internal/scenariotemplate` 通过；`make test` / `make lint` 不回归（全量在 005）。
- 模板数量、内容、时段、次数、目标与 spec 5.1～5.3 一致。
- 任意两类目标在默认工作时段下都能得到 ≥2 条提醒；窄时段能正确报告缺失目标。
- 应用行为无变化（本票不接线）。

## 实现记录（2026-10-21）

- 新增 `internal/scenariotemplate/template.go`：5 个模板、`LoadCatalog` / `Find` / `Personalize`、目标校验、时段交集、`DailyTarget` 夹取；复用 `reminder.ParseClock` / `FirePoints` / `FormatClock`。
- 新增 `internal/scenariotemplate/template_test.go`：目录不变量、5×10 目标组合、裁切/夹取、缺失目标、输入校验、防御性副本。
- `docs/map.md` 增加 `internal/scenariotemplate/` 导航行。
- 验证：`go test ./internal/scenariotemplate`、`go vet ./internal/scenariotemplate`、`gofmt` 通过。
