# 005 — 全量验收与文档收口

**Blocked by**: 001、002、003、004

**状态**: 已完成（2026-10-21）

> 规格：7.4、7.5、8、9；全部验收标准。

## 目标

跑完 spec 的完整验收集，修复小缺口；确认无 AI Key 路径、AI 路径、一次性/快速添加、场景删除级联、升级用户、宠物/强提醒窗口均无回归；文档与 ticket 状态收口。发现大缺口时新开 ticket，不在本票里掩盖。

## 涉及层

- [ ] 测试：
  - `make test`、`make lint`、`make smoke`。
  - spec 7.4 全部手工 GUI 条目；记录证据（窗口/页面/结果）。
  - 回归：AI 生成/重新生成、一次性提醒、快速添加、首页统计、计划页编辑、场景删除级联。
  - 无 AI Key：模板库 + 三步引导完整可用。
  - 升级用户：已有场景 + flag false → 自动完成、不弹引导。
  - 宠物/强提醒窗口：首启不被重定向。
- [ ] 文档：
  - `README.md` 增加场景模板与首次引导的用户可见说明（按当前功能重写，不写迁移/旧名）。
  - `docs/map.md` 补 `internal/scenariotemplate`、`Welcome.vue`、service 模板能力。
  - spec 状态从「待实现」改为完成日期。
  - 本目录 `index.md` 状态表更新；必要时补审计记录。
- [ ] 修复：
  - 仅修验收发现的小缺口；大缺口新开 ticket 并记录。

## 验收标准

- `make test`、`make lint`、`make smoke` 全绿。
- spec 7.4 手工 GUI 条目全部通过，证据记录在 ticket。
- 7.5 回归全部通过；AI 路径不回归。
- 文档与 ticket 状态收口；spec 状态更新。
- 任何未完成项都有明确的新 ticket 或风险说明，无静默放行。

## 验收记录（2026-10-21）

- `make test`、`make lint`、`make smoke` 全绿；smoke 使用隔离 HOME 并固定 GOPATH/GOMODCACHE。
- GUI 矩阵（Wails MCP + 临时 HOME）：clean install → 三步 → 自动启用 → 完成页；场景页模板库/展开预览/完整副本/默认停用/单选启用；跳过与重启；已有场景自动补齐；窄时段缺失目标。
- 回归：AI 路径与场景页新建表单未改；一次性/快速添加/计划编辑代码未动；场景删除级联未改。
- 文档：README 增加场景模板与首次引导；map 增加 `internal/scenariotemplate`、`lib/template.ts`、`Welcome.vue`；spec 状态与验收记录已更新。
- 验收使用 MCP 构建；默认产物已恢复，原 `/Applications/Morrow.app` 已重新打开。
- 未做：真实等待休息倒计时（restmode 测试覆盖）；Windows 手工验收（CI 仅 macOS）。
- 审查后补：`formatNextFireAt(0)` 返回「—」；`make test` / `make lint` / 前端 `npm run build` 复跑通过。
