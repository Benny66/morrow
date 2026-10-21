# 场景模板 + 首次引导规格（2026-10-21）

> 目标用户：中文办公人群（见 `CONTEXT.md`）。
> 依据：`docs/competitive-analysis-round2.md` 候选 C、`CONTEXT.md`、ADR 0008。
> 范围：场景模板目录 + 首次引导（三步）+ 模板个性化（工作时段 / 关注目标）。
> 范围外：见第 8 节。
> 状态：已实现并验收（2026-10-21）；验收记录见
> `.scratch/scenario-templates-and-onboarding/tickets/005-full-acceptance-and-docs.md`。
> 术语：场景模板 / 首次引导 / 关注目标 / 场景 / 提醒计划，定义见 `CONTEXT.md`。

## 1. 问题陈述

**新用户的第一道门槛不是功能，是配置。**

- 现有唯一的计划路径是「场景描述 → AI 生成」；AI 需要 OpenAI 兼容 Key，默认端点是 DeepSeek。没有 Key 的用户在场景页只能保存一个空场景。
- 用户要先理解「场景 / 提醒项 / 间隔 / 生效时段」才能手动建计划；首次启动没有任何引导。
- 第二轮竞品分析把「零配置起步」列为 P1：用户应在 2 分钟内得到一套能用的计划，之后再决定是否接 AI。
- 模板如果也走 AI，就与「无 Key 可用」直接冲突；模板必须是确定性内置内容。

**同时，模板不能是黑盒。** 用户需要看到模板包含什么、选中后保留了什么、下一次提醒是什么时候；否则「一键模板」只是把配置问题藏起来。

## 2. 方案概述

### 2.1 场景模板目录（Go 内置只读定义）

- 5 个模板：久坐办公 / 会议密集日 / 备考冲刺 / 居家办公 / 护眼优先。
- 每个模板 = 名称 + 描述 + 摘要 + 一组确定性提醒项（类型 / 标题 / 正文 / 间隔 / 时段 / 每日目标）。
- 不调用 AI，不需要 Key，离线可用。
- 模板定义是唯一真相，位于 Go 侧；前端只渲染绑定返回的数据，不复制模板内容。
- 选用模板 = 复制成一个普通场景 + 提醒项副本；模板本身不可编辑/删除，副本与自建场景同权。模板更新不回溯已有副本（ADR 0008）。

### 2.2 模板库入口

- 场景页顶部固定「从模板开始」区，5 张卡片；卡片显示名称、摘要、包含类型与条数，可展开完整提醒预览。
- 「使用模板」按模板原样实例化（全部五类、模板自带时段），默认停用；允许重复创建。
- 现有「新建场景」表单保留在下方，AI 路径不回归。

### 2.3 首次引导（三步）

- 仅在干净安装（`onboarding_completed=false` 且没有任何场景）时出现；已有场景的升级用户自动写完成标记。
- 独立全屏向导页 `/welcome`：保留标题栏、隐藏侧栏；右上角可跳过。
- 第 1 步：工作时段（默认 09:00–18:00，仅用于本次裁切，不写全局配置）。
- 第 2 步：关注目标（五类中恰好选两个）。
- 第 3 步：选模板，实时预览过滤/裁切后保留的提醒；缺失目标时禁止完成。
- 完成后自动启用场景，进入完成页：场景名、保留条数、下一次计划提醒、「查看计划」/「开始使用」。
- 跳过：写完成标记，去首页，不创建场景。
- 整个流程不检查 AI 配置、不调用 AI；无 Key 也能完成。

### 2.4 接缝（深模块）

- **最高接缝**：`scenariotemplate.Personalize`（纯函数：过滤、裁切、目标夹取、缺失目标）与 `Service.ApplyScenarioTemplate`（事务落库 + 启用）。
- `PreviewScenarioTemplate` 复用同一个 `Personalize`，保证「预览所见」与「落库所得」一致；前端不复制任何过滤/裁切逻辑。
- 模板目录、目标集合、下一次触发时间都在 Go 侧计算；前端只消费绑定结果。
- 复用现有 `reminder.FirePoints` / `NextFireAt` / `ValidateWindow`，不新建时间计算。

## 3. 用户故事

### A. 场景模板目录

**A-1 内置目录**
应用内置 5 个场景模板；模板列表、名称、摘要、提醒项均来自 Go 侧绑定，前端不写死任何模板内容。无网络、无 AI Key 也能读取和使用。

**A-2 只读模板**
模板本身不可编辑、不可删除；应用升级更新模板内容时，已有副本保持原样，只有此后新建的副本使用新内容。

**A-3 内容覆盖**
每个模板至少覆盖五类关注目标（喝水 / 站立 / 走动 / 放松 / 如厕）；任意两类目标组合都能过滤出至少 2 条提醒。该约束由 Go 测试护栏强制。

**A-4 场景页展示**
场景页顶部固定展示模板区（即使已有场景、即使场景列表为空）；卡片显示名称、一句话摘要、包含的类型与条数；可展开查看完整提醒清单（标题、类型、间隔、时段）。

**A-5 一键使用**
点「使用模板」按模板原样创建：全部五类提醒、模板自带时段、不裁切、不过滤；场景默认停用；允许重复创建；创建成功后刷新列表并提示「已创建，点启用开始提醒」。

**A-6 副本同权**
模板创建的场景是普通场景：可在计划页编辑/停用/删除提醒、可启用/停用场景、可在配置了 AI 时点「重新生成」。删除副本不影响模板；模板可再次选用。副本不记录模板来源，不显示「模板」徽标。

### B. 首次引导

**B-1 触发**
启动时若 `onboarding_completed=false` 且数据库没有任何场景，主窗口进入 `/welcome`；宠物窗口与强提醒窗口不重定向。若已有场景，自动写完成标记并留在首页。绑定失败时记录日志并放行到首页（不阻塞启动）。

**B-2 完成标记持久化**
`onboarding_completed` 存在 `config.json`；完成或跳过都写 `true`。升级用户的旧配置没有该键时视为 `false`，由 B-1 的场景检查自动补齐。

**B-3 布局**
`/welcome` 是独立全屏向导：保留标题栏（窗口可拖动/关闭），隐藏侧栏与常规页面区域；用户不能通过侧栏离开；第 1–3 步右上角始终有「跳过」。

**B-4 第 1 步：工作时段**
两个 HH:MM 时间选择器，默认 09:00–18:00；开始必须早于结束（不支持跨午夜）；不合法时「下一步」禁用并显示原因。

**B-5 第 2 步：关注目标**
从五类目标中恰好选择两个；显示「已选 2/2」；不足或超过时不能进入下一步；可返回上一步修改工作时段。

**B-6 第 3 步：选模板**
展示 5 个模板；选中一个后调用 Go 预览，展示按当前工作时段与目标过滤/裁切后的提醒清单、每类条数与下一次计划提醒。若某个已选目标在时段内没有提醒，明确标出缺失目标并禁止完成；用户可返回修改目标或时段。预览与最终落库使用同一套 Go 逻辑。

**B-7 完成**
点「开始使用」：

- 在同一个数据库事务里创建场景、提醒项，并停用其他场景、启用新场景；
- 场景摘要体现保留条数与工作时段，生成时间记为当前时间；
- 写 `onboarding_completed=true`；
- 进入完成页，展示场景名、保留 N 条提醒、下一次计划提醒，并提供「查看计划」（`/plan?scenario=<id>`）与「开始使用」（首页）。

如果模板应用失败：停留在向导、展示可读错误、不写完成标记、不留下半套数据。如果模板已创建但完成标记写入失败：保留已创建结果，允许只重试写标记，不能重复创建场景。

**B-8 跳过**
任意步骤点「跳过」：写完成标记，去首页；不创建场景、不调用 AI。之后不再出现引导；模板库仍在场景页可用。

**B-9 无需 AI**
整个引导不读取 AI 设置、不检查 Key、不发起 AI 请求；配置里没有任何 AI 字段时也能完成。

**B-10 中途退出**
未完成/未跳过时关闭应用，下次启动仍进入引导第一步；已填内容不持久化。

### C. 个性化规则

**C-1 过滤**
关注目标为空（场景页路径）→ 保留模板全部五类提醒；非空 → 只保留所选两类的提醒。

**C-2 裁切**
工作时段为空（场景页路径）→ 使用模板自带时段；非空 → 每个提醒的时段取模板时段与工作时段的交集；无交集或交集不足一个触发点的提醒丢弃。

**C-3 目标夹取**
裁切后若提醒的 `DailyTarget` 大于该窗口内最大可触发次数，夹到最大次数；`interval_min=0` 视为每天 1 次；`DailyTarget=0` 保持 0。最大次数复用 `reminder.FirePoints` 计算（用固定日期只取点数，点数与日期无关），不另写公式。

**C-4 缺失目标**
过滤/裁切后，若任一已选目标没有剩余提醒，预览返回缺失目标且 `valid=false`；`ApplyScenarioTemplate` 也拒绝该请求并返回 validation 错误（防御性双重校验）。

**C-5 摘要与生成时间**

- 全量副本（场景页）：`Summary = 模板摘要`。
- 个性化副本（引导）：`Summary = 模板摘要 + "（保留 N 条 · HH:MM–HH:MM）"`。
- 两种情况都写 `GeneratedAt = 创建时刻`（Unix 秒）；字段语义从「AI 生成时间」扩展为「最近一次生成计划的时间」。

**C-6 启用语义**
引导路径 `enable=true`：停用其他场景并启用新场景（沿用单选语义）。场景页路径 `enable=false`：只创建，不改变当前启用的场景。

**C-7 原子性**
场景、提醒项、停用其他场景在同一个数据库事务内完成；任一步失败全部回滚，不留下空场景或半套提醒。

### D. 数据、契约与护栏

**D-1 无 schema 变更**
不新增表、不新增列、不迁移数据；模板不是数据库行。`Scenario.GeneratedAt` 与 `Summary` 的注释按 C-5 更新（不再限定 AI 生成）。

**D-2 配置**
`config.Config` 新增 `OnboardingCompleted bool json:"onboarding_completed"`，默认 `false`；旧配置文件缺该键时自然为 `false`。

**D-3 绑定**
新增（名称可微调，语义不变）：

- `ListScenarioTemplates()` → 模板目录（含关注目标集合与完整提醒定义）。
- `PreviewScenarioTemplate(key, workStart, workEnd, goals)` → 预览（提醒、缺失目标、是否可用、下一次计划提醒）。
- `ApplyScenarioTemplate(key, workStart, workEnd, goals, enable)` → 创建结果（场景、提醒、下一次计划提醒）。
- `ShouldShowOnboarding()` → 是否显示引导；有场景时自动补完成标记。
- `CompleteOnboarding()` → 写完成标记并落盘。

`ApplyScenarioTemplate` 成功后发一次 `reminder:changed`，reason 用 `plan_changed`（场景与计划同时变化，前端只需刷新）。错误一律经 `apperr` 构造，前端按 code 分流。

**D-4 前端路由**
新增 `/welcome` 路由与向导布局 meta；`App.vue` 增加「保留标题栏、无侧栏」分支；`bare` 路由（`/pet`、`/alert`）行为不变。启动时在挂载前解析引导状态，避免首页闪现；纯 vite dev / 绑定失败时放行。

**D-5 单一真相**

- 模板内容、目标集合、过滤/裁切/目标夹取、下一次触发时间：只在 Go。
- 前端只做渲染与表单状态；不得复制过滤/裁切规则。
- 类型标签继续复用 `lib/reminder.ts` 的 `kindMeta`；目标集合由 Go 返回，前端不另写一份。

**D-6 护栏**

- `scenariotemplate` 目录测试：模板数量为 5（解析到 0 个必须失败）、key 唯一、名称/描述/摘要非空、每条提醒 kind 合法、标题/正文长度符合列宽、窗口合法、间隔/目标非负、每个模板覆盖五类目标。
- 组合测试：每个模板 × 每个目标二元组 × 默认工作时段 → 至少 2 条提醒、无缺失目标、`valid=true`。
- 裁切测试：窄工作时段 → 窗口落在交集内、无交集提醒被丢弃、目标被夹取、缺失目标被报告。
- 服务测试：全量创建、个性化创建、启用单选、重复创建、无 AI 配置、缺失目标拒绝、预览不落库、事务回滚（如可行）。
- 配置测试：完成标记默认 false、旧配置缺键、已有场景自动补齐、完成/跳过持久化。

## 4. 已定决策

1. 场景模板是**确定性内容**，不调用 AI、不需要 Key、离线可用。
2. 模板是**只读定义 + 实例化副本**，不是数据库内置行，也不加 `is_builtin` 标记；副本与自建场景同权；模板更新不回溯（ADR 0008）。
3. 规范术语为**场景模板**；「内置场景」只是口语叫法（`CONTEXT.md`）。
4. 本次切片包含**完整三步首次引导**，不只是模板库。
5. 引导三步为**工作时段 → 关注目标 → 选模板**；第 2 步**恰好选两类**。
6. 关注目标**过滤提醒类型**；模板覆盖全部五类，保证任意两类组合可用。
7. 引导工作时段**只用于本次裁切**，不写入全局配置。
8. 引导仅在**干净安装且无场景**时出现；跳过也写完成标记；已有场景的升级用户自动完成。
9. 引导是**独立全屏无侧栏页面**，保留标题栏。
10. 模板库固定在**场景页顶部**；卡片可展开完整预览。
11. 首批 **5 个模板**：久坐办公 / 会议密集日 / 备考冲刺 / 居家办公 / 护眼优先。
12. **仅引导路径自动启用**；场景页路径默认停用，避免静默替换。
13. 完成页提供**查看计划 / 开始使用**两个入口。
14. 场景页使用模板**原样实例化**（不过滤、不裁切、默认停用）。
15. 允许**重复实例化**同一模板，不查重、不提示。
16. 模板副本复用 `Summary` / `GeneratedAt`；`GeneratedAt` 语义扩展为「最近一次生成计划的时间」。
17. 模板内容更新**不回溯**已有副本。

## 5. 模板内容基线

### 5.1 通用文案（五类）

| kind | 标题 | 正文 |
|---|---|---|
| water | 喝水 | 起身倒一杯温水，慢慢喝完。 |
| stand | 站起来 | 起身站直，活动肩颈，喝口水再坐下。 |
| move | 走动一下 | 离开工位走几步，去窗边看看远处。 |
| rest | 让眼睛歇一会儿 | 看向远处，慢慢眨眼，放松眼睛。 |
| toilet | 去趟洗手间 | 起身走动，顺便补一杯水。 |

### 5.2 五个模板的提醒日程

> 触发次数按 `reminder.FirePoints` 的「窗口两端都含」语义计算。每日目标取触发次数的约 80%（四舍五入，至少 1），如厕固定为 0（不设目标）。所有时段必须是 `HH:MM` 且结束晚于开始。

| 模板 | 类型 | 间隔 | 时段 | 次数 | 目标 |
|---|---|---|---|---|---|
| 久坐办公 | water | 120 | 09:15–17:15 | 5 | 4 |
|  | stand | 180 | 09:30–17:30 | 3 | 2 |
|  | move | 240 | 10:00–16:00 | 2 | 2 |
|  | rest | 150 | 10:45–16:45 | 3 | 2 |
|  | toilet | 240 | 11:00–17:00 | 2 | 0 |
| 会议密集日 | water | 150 | 10:00–17:00 | 3 | 2 |
|  | stand | 240 | 10:30–16:30 | 2 | 2 |
|  | move | 240 | 11:15–15:15 | 2 | 2 |
|  | rest | 180 | 10:45–16:45 | 3 | 2 |
|  | toilet | 300 | 09:45–15:45 | 2 | 0 |
| 备考冲刺 | water | 120 | 09:15–17:15 | 5 | 4 |
|  | stand | 240 | 09:30–17:30 | 3 | 2 |
|  | move | 300 | 10:15–16:15 | 2 | 2 |
|  | rest | 120 | 09:00–17:00 | 5 | 4 |
|  | toilet | 300 | 11:30–16:30 | 2 | 0 |
| 居家办公 | water | 90 | 09:00–18:00 | 7 | 6 |
|  | stand | 180 | 09:45–17:45 | 3 | 2 |
|  | move | 180 | 09:30–15:30 | 3 | 2 |
|  | rest | 180 | 10:15–17:15 | 3 | 2 |
|  | toilet | 300 | 11:45–16:45 | 2 | 0 |
| 护眼优先 | water | 150 | 09:15–17:15 | 4 | 3 |
|  | stand | 240 | 09:45–17:45 | 3 | 2 |
|  | move | 300 | 10:15–16:15 | 2 | 2 |
|  | rest | 90 | 09:00–17:30 | 6 | 5 |
|  | toilet | 300 | 11:15–17:15 | 2 | 0 |

### 5.3 模板描述与摘要

| 模板 | summary | description（AI 可读的场景描述） |
|---|---|---|
| 久坐办公 | 标准坐班节奏：喝水、起身、走动、护眼与如厕均衡覆盖。 | 我每天长时间坐在电脑前办公，经常一坐就是两三个小时。希望提醒我按时喝水、站起来活动、走动一下、让眼睛休息，也别忘了去洗手间。 |
| 会议密集日 | 低频稳妥：减少打断，适合会议多的一天。 | 我一天里会议很多，经常连续几个小时离不开屏幕。希望提醒少而稳，不要频繁打断，但仍能保证喝水、起身、护眼和如厕。 |
| 备考冲刺 | 长专注节奏：喝水与护眼稳定，减少碎片化打断。 | 我最近在备考或冲刺项目，常常连续专注很久。希望提醒我喝水、起身、走动和护眼，节奏不要太碎，但喝水和护眼要稳定。 |
| 居家办公 | 补水与走动加强：弥补居家办公消失的通勤步数。 | 我在家办公，通勤和走动都变少了。希望提醒更频繁一些，督促我喝水、站起来、在屋里走动、让眼睛休息。 |
| 护眼优先 | 护眼优先：更密的远眺提醒，其余保持标准节奏。 | 我每天用眼强度很大，最想优先保护眼睛。希望用眼提醒更密一些，同时保留喝水、起身、走动和如厕的提醒。 |

> 模板的 `description` 会原样成为副本的场景描述；它对 AI 重新生成仍是可读输入。个性化副本不会改写描述（见第 9 节）。

### 5.4 排序

每个模板的提醒 `SortOrder` 按 water → stand → move → rest → toilet 的顺序赋值；计划页按该顺序展示。

## 6. 接口与数据（实现契约）

### 6.1 `internal/scenariotemplate`

```go
type Reminder struct {
    Title       string `json:"title"`
    Content     string `json:"content"`
    Kind        string `json:"kind"`
    IntervalMin int    `json:"interval_min"`
    WindowStart string `json:"window_start"`
    WindowEnd   string `json:"window_end"`
    DailyTarget int    `json:"daily_target"`
}

type Template struct {
    Key         string     `json:"key"`
    Name        string     `json:"name"`
    Description string     `json:"description"`
    Summary     string     `json:"summary"`
    Reminders   []Reminder `json:"reminders"`
}

type Catalog struct {
    Goals     []string   `json:"goals"`
    Templates []Template `json:"templates"`
}

type Options struct {
    WorkStart string
    WorkEnd   string
    Goals     []string
}

type Preview struct {
    Reminders    []Reminder `json:"reminders"`
    MissingGoals []string   `json:"missing_goals"`
    Valid        bool       `json:"valid"`
    NextFireAt   int64      `json:"next_fire_at"` // 由 service 填充
}

func LoadCatalog() Catalog
func Find(key string) (Template, bool)
func Personalize(t Template, opts Options) (Preview, error)
```

规则：

- `Personalize` 是纯函数，不读时钟、不写库。
- `Goals` 为空 = 全部五类；非空必须恰好 2 个且互不相同、均为合法目标。
- `WorkStart/WorkEnd` 同时为空 = 不裁切；只给一个或窗口非法 → validation 错误。
- 缺失目标与 `Valid` 由过滤/裁切结果计算；不因缺失目标返回 error（预览要能展示缺失）。
- `LoadCatalog` 返回防御性副本，调用方不得修改目录。

### 6.2 `internal/service/scenario.go`（或同包新文件）

```go
type ApplyTemplateResult struct {
    Scenario   *model.Scenario  `json:"scenario"`
    Reminders  []model.Reminder `json:"reminders"`
    NextFireAt int64            `json:"next_fire_at"`
}

func (s *Service) ListScenarioTemplates() scenariotemplate.Catalog
func (s *Service) PreviewScenarioTemplate(key, workStart, workEnd string, goals []string) (*scenariotemplate.Preview, error)
func (s *Service) ApplyScenarioTemplate(key, workStart, workEnd string, goals []string, enable bool) (*ApplyTemplateResult, error)
```

- `PreviewScenarioTemplate`：`Find` → `Personalize` → 用 `s.now()` 与 `reminder.NextFireAt` 填充 `NextFireAt`。
- `ApplyScenarioTemplate`：`Find` → `Personalize` → `Valid` 必须为 true → 事务。
- 事务内：
  1. 若 `enable`，先把现有启用场景全部停用（`UPDATE scenarios SET enabled=false WHERE enabled=true`；新场景尚未创建，不需要排除自己）；
  2. `Create` 场景（Name / Description / Summary / GeneratedAt / Enabled=enable）；
  3. 按模板顺序 `Create` 提醒（ScenarioID / Title / Content / Kind / IntervalMin / WindowStart / WindowEnd / DailyTarget / Enabled=true / SortOrder）；
  4. 提交。
- `NextFireAt` = 已创建提醒中 `reminder.NextFireAt(now, ...)` 的最小非零值；没有则 0。
- 错误：模板不存在 → `apperr.NotFound`；输入非法/缺失目标 → `apperr.Validation`；数据库 → `apperr.Wrap`。

### 6.3 `app.go` 绑定

- 按 6.2 的三个方法直通 service；`ApplyScenarioTemplate` 成功后 `a.emitReminderChanged(reminder.ReasonPlanChanged)`。
- `ShouldShowOnboarding() (bool, error)`：
  - `cfg.OnboardingCompleted == true` → false；
  - 否则查询场景；有场景 → 尝试写 `OnboardingCompleted=true` 并保存（失败只记日志），返回 false；
  - 无场景 → true。
- `CompleteOnboarding() error`：写 `OnboardingCompleted=true` 并 `cfg.Save()`；保存失败返回 `apperr.Wrap`。

### 6.4 配置

- `Config.OnboardingCompleted bool` + `json:"onboarding_completed"`；默认 false。
- 不加显式默认值逻辑：旧配置缺键自然为 false，由 `ShouldShowOnboarding` 的场景检查兜底。

### 6.5 前端

- **路由**：新增 `/welcome`，`meta: { title: '首次设置', wizard: true }`；`RouteMeta` 增加 `wizard?: boolean`；`App.vue` 增加 wizard 分支（TitleBar + 居中内容，无侧栏），`bare` 分支不变。
- **启动**：`main.ts` 在 `router.isReady()` 之后、`mount` 之前：
  - 若当前路由 `meta.bare` 为 true（宠物/强提醒窗口），跳过引导检查；
  - 否则调用 `ShouldShowOnboarding()`；true 且不在 `/welcome` → `router.replace('/welcome')`；false 且在 `/welcome` → `router.replace('/')`；
  - 调用失败：`logError` 并继续到首页，不阻塞启动。
- **`Welcome.vue`**：三步 + 完成页；状态机 `step: 1|2|3|done`；`skip` 调 `CompleteOnboarding`；`finish` 先 `ApplyScenarioTemplate(..., enable=true)`，成功后再 `CompleteOnboarding`；已创建但标记失败时记住结果，重试只写标记；错误用现有 `invoke` / `AppError` 展示。
- **`Scenarios.vue`**：模板区在「新建场景」之前；`ListScenarioTemplates` 加载；卡片展开预览；`使用模板` 调 `ApplyScenarioTemplate(key, '', '', [], false)`；成功后 `load()` 并提示；与现有 `useEvent(ReminderEvent.Changed)` 刷新兼容。空状态文案同时提示「从上方模板开始」与「手动描述」两条路径。
- **类型标签**：目标与模板类型统一走 `kindMeta`；目标集合由 Go 的 `Catalog.Goals` 返回，前端不写死清单。

## 7. 测试决策

### 7.1 Go 单元测试（模板目录与个性化）

- `internal/scenariotemplate/template_test.go`
  - 目录非空且恰好 5 个（0 个必须失败，护栏感知自己瞎了）。
  - 每个模板：key 唯一、名称/描述/摘要非空、每条提醒 kind 合法、标题 ≤50 字、正文 ≤200 字、窗口合法、间隔/目标非负。
  - 覆盖：每个模板包含全部五类目标。
  - 组合：每个模板 × 每个目标二元组 × 09:00–18:00 → `len(reminders) ≥ 2`、`MissingGoals` 为空、`Valid=true`、每条提醒 kind 在所选目标内。
  - 裁切：10:00–11:00、12:00–13:00 等窄窗口 → 所有提醒窗口落在交集内；无交集的提醒被丢弃；目标 ≤ 窗口内触发次数。
  - 缺失目标：03:00–04:00 或 17:00–17:10 等与部分模板窗口无有效交集的时段 → `Valid=false`，`MissingGoals` 精确列出没有提醒的目标。
  - 输入校验：单边工作时段、end ≤ start、1 个/3 个目标、重复目标、非法目标 → error。
  - 无目标/无时段 → 原样返回全部提醒（场景页路径）。
  - 目标夹取：断言 5.2 表内每个目标 ≤ 次数；窄窗口下目标随次数下降。

### 7.2 Go 服务测试

- `internal/service/scenario_template_test.go`（内存 SQLite，沿用现有 service 测试的建库方式）
  - 全量创建：无 AI 配置也能成功；场景名/描述/摘要/GeneratedAt；5 条提醒；默认停用。
  - 个性化创建：只保留所选两类；窗口被裁切；目标被夹取；场景启用；其他场景被停用。
  - 缺失目标：返回 validation 错误；数据库没有新增场景/提醒。
  - 重复创建：同一模板两次 → 两个独立场景、两套提醒。
  - 预览：不写库；`NextFireAt` > 0；缺失目标时 `Valid=false`。
  - 事务：失败注入（如可行）后没有半套数据；至少覆盖「缺失目标在写库前被拒绝」。
  - `ShouldShowOnboarding` / `CompleteOnboarding`：可用 config + service 的集成测试覆盖默认 false、有场景自动补齐、完成持久化。

### 7.3 静态与构建

- `make test`（Go 护栏 + 单元测试）通过。
- `make lint`（gofmt + Go 护栏 + go vet + ESLint + vue-tsc）通过。
- `make smoke` 通过（启动链路；不依赖 UI 内容）。

### 7.4 手工 GUI 验收（必须）

- 干净数据目录 → 启动进入 `/welcome`，无侧栏、标题栏可拖动/关闭；宠物窗口不跳转。
- 第 1 步非法时段 → 下一步禁用；默认 09:00–18:00。
- 第 2 步选 0/1/3 个 → 不能下一步；恰好 2 个 → 可以。
- 第 3 步预览随工作时段/目标变化；窄时段导致缺失目标 → 明确提示且不能完成。
- 完成 → 完成页显示场景名、条数、下一次计划提醒；「查看计划」定位到该场景；首页显示已启用场景。
- 跳过 → 首页，无新场景；重启不再出现引导。
- 已有场景的配置（模拟升级）→ 不出现引导。
- 场景页：模板卡片可展开；使用模板创建完整计划且默认停用；启用后提醒生效；重复使用生成两个副本。
- 未配置 AI Key：以上全部流程可用；场景卡片上的「AI 生成计划」仍按原逻辑提示未配置。
- 休息模式：模板里的 stand/move/rest 提醒照常进入预警 + 倒计时。

### 7.5 回归

- 现有 AI 生成计划路径不变：配置 Key 后生成/重新生成仍可用。
- 一次性提醒、快速添加、首页统计、计划页编辑不受影响。
- 场景删除仍级联清理提醒/触发/打卡。

## 8. 明确不做

- 首次引导里配置 AI（Key / 端点 / 模型）；AI 仍然是引导之外的「自定义」入口。
- 模板版本化、手动同步模板更新、把模板更新推给已有副本。
- 场景重命名（现有 `UpdateScenario` 无 UI，本次不补）。
- 从设置页重新运行首次引导。
- 多场景同时启用（单选语义不变）。
- 日历/会议感知的「会议密集日」；该模板只是静态的低频节奏。
- 场景页使用模板时的目标/时段个性化（只有引导路径个性化）。
- 全局工作时段配置；引导时段不持久化。
- 模板编辑 UI、模板来源徽标、模板市场/下载。
- 医疗建议、疾病/用药/孕期等医疗垂直内容。
- 跨设备同步/云端模板。

## 9. 补充说明

- **静默时段优先**：下一次计划提醒是排程时间，不扣除静默时段、智能暂停、自然停顿顺延；完成页文案用「下一次计划提醒」，不承诺实际投递时刻。
- **模板描述与 AI 重新生成**：副本保留模板描述。用户之后点「重新生成」时，AI 只看到该描述，不会自动带上工作时段/关注目标；这是本次的已知边界（AI 是自定义路径，不是模板个性化的延伸）。
- **工作时段不记忆**：引导选的时段只影响本次实例化；之后从场景页用模板或 AI 生成不会自动套用。用户可在计划页逐条调整。
- **重复副本**：同名场景是允许的；副本没有 `template_key`，无法在 UI 上区分来源，这是「无 schema 变更」的代价。
- **「干净安装」的判据**：`onboarding_completed=false` 且没有任何场景。只用过一次性提醒、从未建过场景的老用户会被视为未完成引导一次；可直接跳过，不影响数据。
- **标记写入失败的自愈**：若模板已创建但完成标记最终没写成功，下次启动因已有场景会自动补齐标记，不会重复进入向导，也不会丢计划。
- **宠物**：引导期间保持现有宠物显隐设置（默认可见），不做特殊隐藏；引导是主窗口内的页面，不新增窗口。
- **冒烟测试**：`scripts/smoke.sh` 只断言进程/产物/版本，不断言首页内容，因此首启进入 `/welcome` 不会破坏它。
- **数据量**：5 个模板 × 5 条提醒 = 25 条内置提醒定义，属于代码内容，不写数据库；升级只替换代码。

## 10. 实现与验收记录（2026-10-21）

- 模板目录与个性化纯函数：`internal/scenariotemplate/template.go`；护栏见 `template_test.go`。
- 模板实例化与预览：`internal/service/scenario_template.go`；绑定在 `app.go`。
- 场景页模板库：`frontend/src/views/Scenarios.vue`；展示文案工具在 `frontend/src/lib/template.ts`。
- 首次引导：`frontend/src/views/Welcome.vue`、`frontend/src/App.vue`、`frontend/src/router/index.ts`、`frontend/src/main.ts`；启动门禁绑定为 `ShouldShowOnboarding` / `CompleteOnboarding`。
- 配置：`internal/config/config.go` 新增 `onboarding_completed`。
- 验证：`make test`、`make lint`、`make smoke` 全绿。
- GUI 验收（临时 HOME + Wails MCP）：clean install 进入 `/welcome`；工作时段 → 喝水/放松 → 久坐办公 → 生成 2 条提醒并自动启用；完成页跳转计划页正确；场景页 5 个模板、展开预览、一键创建完整副本（默认停用）、启用后单选替换旧场景；跳过后重启不再出现；已有场景 + 标记 false 自动补齐；17:00–17:30 窄时段下走动缺失、完成按钮禁用、喝水窗口裁切为 17:00–17:15 且目标夹到 1。
- 验收使用 MCP 构建，未改动默认产物；验收后已恢复普通构建并重新打开原 `/Applications/Morrow.app`。
