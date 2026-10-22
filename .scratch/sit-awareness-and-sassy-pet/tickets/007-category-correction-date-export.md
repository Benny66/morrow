# 007 — 类别纠正、按日查看与导出/清空边界

**Blocked by**: 006

**状态**: 已完成（2026-10-22；真实 GUI 类别纠正/按日/导出/清空验收）

> 规格：`docs/specs/2026-10-22-sit-awareness-and-sassy-pet.md` 的 E-3、E-5、E-6、E-7、E-8、E-9、E-10、7.5。
> 本票把「记录与看见」补成「可核对、可纠正、可带走、可删除」。

## 目标

用户可以在久坐页纠正某个应用的行为类别，历史与之后的视图立即跟随；可以按日期回看任意一天的应用明细与趋势；导出数据库时可以选择是否包含应用画像；清空应用与久坐数据时保留打卡记录与连续天数。

## 涉及层

- [x] 数据层：`internal/service/sit.go`
  - `SetAppCategory(platform, appID, category)`：写入/更新 `app_category_overrides`；非法类别拒绝；未知应用也可纠正；纠正只存本机。
  - 类别读取优先 override，其次内置映射；纠正对历史与未来视图、趋势、宠物记忆同时生效，不重写原始 `app_usage_sessions`。
  - `GetSitDay(date)` / `GetSitTrend(7)` 支持任意过去日期与类别聚合；未来日期返回空。
  - `ClearSitProfile`：删除应用会话、类别纠正、久坐会话/休息、宠物发言与派生记忆；保留 CheckIn；重置 tracker 当前会话/待展示气泡；幂等。
  - `ExportDatabase` 增加 `includeSitProfile bool`（或新增带选项的方法）：默认包含；取消时导出副本不包含 `app_usage_sessions` / `app_category_overrides` / `sit_sessions` / `sit_breaks` / `sit_speeches`，`check_ins` 与其 source 保留。
  - 导出实现：先 `VACUUM INTO` 临时库，再在副本上删除 sit/profile 表，成功后原子改名；覆盖现有导出路径的测试。
  - 数据库文件与导出副本在 macOS/Linux 使用 `0600`；Windows 依赖用户目录 ACL 并记录说明（002 已收紧数据库，本票覆盖导出副本）。
- [x] 逻辑层：`app.go`
  - 绑定：`SetAppCategory`、`GetSitDay(date)`、`GetSitTrend(days)`、`ClearSitProfile`、带 includeProfile 的导出。
  - 清空/导出/纠正后不需要新事件；页面主动刷新或 `reminder:changed`。
- [x] UI 层：`Sit.vue` / `Settings.vue`
  - 应用明细每行增加类别选择器；保存后原地更新并提示「已应用，历史视图同步」。
  - 日期选择器（原生 `input[type=date]` 或 Element Plus）切换今日/任意历史日；未来日期禁用。
  - 7 天趋势增加类别分布/主动起身；有文字标签；继续使用 CSS 条形，不引入图表库。
  - 设置/久坐页的「清空应用与久坐数据」二次确认，文案明确「CheckIn 与连续天数保留」。
  - 导出对话框增加「包含应用使用明细（应用名与时长）」复选框，默认勾选并提示分享风险。
  - 画像状态：ok / unsupported / read_error / no_data 的可见文案与下一步。
- [x] 测试
  - override：纠正后历史类别分布变化、原始行不变、趋势/记忆跟随、非法类别拒绝。
  - 日期：按日查询与 7 天聚合一致、未来日期空、跨月/跨年。
  - 导出：包含/不包含、VACUUM 副本可打开、恢复后可迁移、CheckIn 保留、文件权限。
  - 清空：应用+久坐数据为空、CheckIn/连续天数不变、幂等、当前快照重置。
  - 页面：日期切换、类别选择、导出复选框、清空确认；headless/真实 GUI。
  - `make test` / `make lint`。
- [x] 文档：导出/清空/纠正的隐私说明；`docs/map.md` 的导出路径说明。

## 验收标准

- 改一个应用的类别后，历史分布、7 天趋势与之后的宠物上下文都使用新类别；原始应用会话不被改写。
- 可按任意过去日期查看应用明细；未来日期不可选或返回空。
- 导出时勾选包含 → 副本含应用明细；取消 → 副本不含 profile 表但可正常恢复；CheckIn 保留。
- 清空后久坐页为空、宠物不再引用旧记忆；首页完成数与连续天数不变；操作幂等。
- 未知应用可纠正；画像读取异常有可见状态，不显示成 0 分钟。
- 不新增依赖；`make test` / `make lint` 通过；真实 GUI 走查记录。

## 实现记录（2026-10-22）

- 类别纠正：`SetAppCategory` 以 `platform+app_id` upsert `app_category_overrides`；非法类别/空标识返回 validation；`GetSitDay` 先查纠正再回落内置映射，类别分布、7 天趋势与后续记忆共用同一派生路径；原始 `app_usage_sessions` 不写类别、不改写。
- 按日：`AppUsage` 增加 `platform`（供纠正定位）；`GetSitDay` 显式拒绝未来日期返回空聚合；`GetSitTrend` 任意 7 天区间跨月/跨年测试通过。久坐页新增日期选择器与可点击趋势柱；选中日显示久坐/最长/休息/抵扣 + 应用明细/类别分布。
- 清空：`ClearSitData` 升级为 `ClearSitProfile`（应用会话/纠正 + 久坐会话/休息/发言），保留 CheckIn；运行时同时重置 tracker、当前会话/休息/抵扣、应用采样会话与画像状态，幂等。设置页与久坐页统一为「清空应用与久坐数据…」，提示「画像开启时会继续记录新数据」。
- 导出：`ExportDatabase(appName,target,includeSitProfile)`；取消画像时在 `VACUUM INTO` 副本上 `DROP` 五张画像/久坐表并再次 `VACUUM`（避免敏感数据残留在空闲页），副本 POSIX `0600`；CheckIn 保留，副本可重新迁移。前端设置页新增默认勾选的「包含应用使用明细」，并提示分享前取消。
- 画像关闭后保留历史：久坐页不再整块隐藏应用卡；关闭画像但保留历史时仍可按日查看旧明细并显示「应用画像已关闭，以下是保留的历史记录」；未开启且无数据时显示明确空状态与「去设置开启」入口。
- GUI（临时 app + 临时 HOME，MCP DOM/绑定断言）：预置今日/昨日应用与久坐数据后，`/sit` 默认显示今日 Chrome/终端/Figma 与类别分布；点趋势柱切到 10-21，显示 Chrome 45 分/Slack 20 分；把 Chrome 改为「影音」后历史分布、当前日与 `GetSitTrend` 全部变为 media，数据库只有 1 行纠正、原始 Chrome 3 条会话跨两日不变；日期选择器选 10-20 成功，10-23 之后不可选且 `GetSitDay` 返回空。
- GUI 导出/清空：设置页导出复选框默认勾选；经绑定导出 include=true 副本含 9 条应用会话/1 条纠正/3 条 CheckIn，include=false 副本无 `app_usage_*`/`sit_*` 表但保留 3 条 CheckIn，两份文件权限均为 `600`。连续两次 `ClearSitProfile` 后应用/纠正/久坐/休息/发言均为 0、CheckIn 仍为 3，页面回到空态（画像开启时下一轮采样会立即开始新会话，提示文案已说明）。
- 验证：`make test` / `make lint` 全绿；Windows amd64 交叉编译通过。验收后清理临时环境并重新打开 `/Applications/Morrow.app`。
- 未覆盖：清空/导出前的原生确认框未在 GUI 内自动点击（清空经绑定验证，导出经绑定验证）；类别纠正对宠物记忆的影响需等 009 的记忆实现后回归。
