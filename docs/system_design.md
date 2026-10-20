# 增量架构设计 — 宠物「小满」芽随今日完成率生长

> 变更范围：仅增量。不改「小满」基础形象（水滴 + 芽 + 短手），只改芽的生长形态表达。
> 上游依据：`openspec/changes/sprout-growth/prd.md`（已定稿）。

---

## 1. 方案与关键决策

### 1.1 数据通路：新增只读绑定 `GetPetGrowth()`（不复用 `GetTodayStats()`）

**决策**：新增极轻量只读绑定 `App.GetPetGrowth()`，返回 `service.PetGrowth{DoneTotal, TargetTotal, Growth}`；宠物窗口不再调用 `GetTodayStats()`。

**理由**（重数据 vs IPC 成本）：
- `GetTodayStats()` 一次打 5 个查询（countPendingFires / ActiveScenario / ListReminders / completedByReminder / nextSnoozeFire），并为每条提醒算 streak、NextFireAt，再序列化 `Scenario` + 完整 `Reminders[]` + `NextTitle` 等大 DTO。宠物窗口每 4s 轮询一次，其中它只用到 `DoneTotal/TargetTotal` 两个 int。
- 宠物窗口是常驻 AlwaysOnTop 小窗，长期空转轮询大 DTO 是纯浪费（IPC 载荷、CPU、DB 压力都无谓放大）。
- `GetPetGrowth()` 只做 3 个查询，返回 3 个 int，IPC 载荷从 O(提醒数) 降到 O(1)。

**单一真相的保证**：`GetPetGrowth()` 不另写统计逻辑，与 `GetTodayStats()` 共用从后者抽出的共享助手 `activeProgress()`（见 §3.1）。二者 `DoneTotal/TargetTotal` 由同一段代码算出，语义完全一致（仅统计【当前启用场景】下【启用提醒】的完成/目标）。唯一多出的成本是 `completedByReminder` 仍按 60 天窗口扫描（streak 需要），本仓数据量级下可忽略——用「一段共享代码」换「两处漂移风险」是正确取舍。

### 1.2 完成率 → 档位：放后端（单一真相）

**决策**：`growthTier(done, target) int` 放在 Go service 层，`GetPetGrowth()` 直接返回 `Growth`（0..4）。前端只把 int 映射成 class，不复制边界判定。

**理由**：档位边界是【判定逻辑】不是【展示常量】。按 AGENTS.md 铁律 1「镜像只许复制常量与类型，不许复制逻辑」，边界算法只能有一处实现。与既有先例一致（`model.NormalizeKind` 只在 Go 侧实现，前端只镜像展示元数据）。

### 1.3 5 档视觉：class 切换 + 内层 `<g class="pet__growth">` 承载静态几何，动画不冲突

**决策**：在 `.pet__sprout` 内部再包一层 `<g class="pet__growth">`（茎 + 两叶 + 可选芽点）。档位静态几何（scale / 叶可见性 / 芽点）全部作用在 `.pet__growth`；既有 `pet-sprout-sway / pet-sprout-alert / pet-sprout-pop` 动画继续作用在外层 `.pet__sprout`。

**为什么不冲突**：现有 sway/alert/pop 都是 `animation` 改写 `.pet__sprout` 的 `transform`（rotate / scaleY）。若把档位 scale 也写在 `.pet__sprout` 上，动画的 `transform` 会覆盖基础 transform，生长态瞬间消失。把「静态档位几何」与「动态动画」分到两个不同元素上，二者通过父子 transform 自然叠加（父 rotate + 子 scale）。

**缩放锚点稳定**：`.pet__growth` 的 `transform-origin: 50% 100%`（茎基部 (96,46)）——缩放向上生长、茎基不动，因此外层 `.pet__sprout` 的 sway 旋转原点（同样是 50% 100%，落在茎基）不随档位漂移。

### 1.4 刷新时机：4s 轮询为真相源 + 事件/打卡后即时刷新（结合）

**决策**：三层刷新，互为兜底：

1. **4s 轮询**（沿用 `PET_SETTINGS_POLL_MS=4000` 的既有 `setInterval`）：`GetPetGrowth()` 常驻刷新，兜底「次日翻篇」「其它窗口打卡」「任何漏事件」——最终一致。
2. **`reminder:changed` 事件**（既有事件，不新增常量）：`handleChanged()` 里补一次 `refreshGrowth()`，覆盖从主窗口 / 通知按钮完成的打卡。
3. **宠物窗口自身点「完成」**：`actCurrent('done')` 成功后立即 `refreshGrowth()`，做到「点完成 → 芽及时长」，不等 4s。

**理由**：失焦常驻窗不能用纯事件（事件可能漏、午夜翻篇无事件），也不能只靠轮询（4s 延迟对「点完成」体感偏慢）。事件路径满足即时性，轮询路径保证最终一致与午夜归零。

### 1.5 降级

| 情况 | 档位 | 说明 |
|---|---|---|
| TargetTotal = 0 | 0（健康小苗） | `growthTier` 里 `target <= 0` 直接回 0；与「0% 小芽苗」同档同形（PRD 已确认「健康小苗 = 档位 0 的形态」） |
| DoneTotal = 0 且 Target > 0 | 0（小芽苗） | 正常「今天刚开始」 |
| GetPetGrowth 读不到（err） | 保持上次；首次失败回 0 | 前端 `refreshGrowth` catch 后不改 `growth`；`growth` 初值 0 |
| 次日凌晨翻篇 | 0 | date_key 由 `time.Now()` 本地日期自然驱动，下一轮 4s 轮询即归零 |

---

## 2. 文件清单（改 / 新增，相对路径）

### 后端
| 文件 | 动作 | 说明 |
|---|---|---|
| `internal/service/checkin.go` | 改 | 抽出共享助手 `activeProgress()`；`GetTodayStats()` 改调它（行为不变） |
| `internal/service/growth.go` | 新 | `PetGrowth` 结构、`Growth*` 常量、`growthTier()`、`GetPetGrowth()` |
| `internal/service/growth_test.go` | 新 | `growthTier` 边界单测（表驱动） |
| `app.go` | 改 | 新增绑定方法 `GetPetGrowth()`（委托 `a.svc`，不 import gorm/database） |
| `frontend/bindings/morrow/` | 重新生成 | `app.ts` / `app.js` 等，随 `wails3 generate bindings` / `make dev` 产出 |

### 前端
| 文件 | 动作 | 说明 |
|---|---|---|
| `frontend/src/lib/growth.ts` | 新 | `GrowthTier` 镜像常量 + `GrowthTierValue` + `PET_GROWTH_TRANSITION_MS`（仅镜像数值，不镜像判定） |
| `frontend/src/views/Pet.vue` | 改 | script 接线（`GetPetGrowth` / `growth` ref / `refreshGrowth` / 轮询+事件+打卡后刷新 / 降级）；template（`.pet__growth` 包裹 + `.pet__bloom`）；style（五档几何 + 过渡） |
| `docs/pet-character.md` | 改 | 把「芽随完成率生长」从「未实现扩展钩子」改为已实现，并补生长档位表 |

---

## 3. 数据结构与接口

### 3.1 Go 共享助手（checkin.go 抽出）

```go
// activeProgress 汇总「当前启用场景下、启用提醒」的今日完成数与目标数，
// 并返回构建仪表盘明细所需的中间数据。
// GetTodayStats 与 GetPetGrowth 共用，保证 DoneTotal/TargetTotal 只有一份算法。
func (s *Service) activeProgress(now time.Time, today string) (
	scenario *model.Scenario,
	reminders []model.Reminder,
	countsByReminder map[uint]map[string]int,
	doneTotal, targetTotal int,
	err error,
)
```

语义（从 `GetTodayStats` 原实现原样抽出，不改变行为）：
- 无启用场景 → `scenario == nil`，`doneTotal/targetTotal == 0`。
- 有启用场景 → 只累加 `Enabled` 提醒的 `DoneCount`（今天）与 `DailyTarget`。

`GetTodayStats()` 改调本助手，其余（countPendingFires / nextSnoozeFire / per-reminder 明细循环）保持原样。

### 3.2 新增 `PetGrowth` 与档位（growth.go）

```go
// PetGrowth 是宠物窗口「芽」生长所需的最小数据。
type PetGrowth struct {
	// DoneTotal 今日完成总次数。
	DoneTotal int `json:"done_total"`
	// TargetTotal 今日目标总次数（0 表示未设目标）。
	TargetTotal int `json:"target_total"`
	// Growth 生长档位 0..4（见 Growth* 常量），由后端统一计算。
	Growth int `json:"growth"`
}

// 生长档位（单一真相）。前端镜像见 frontend/src/lib/growth.ts 的 GrowthTier。
const (
	// GrowthSprout 小芽苗 / 健康小苗（完成率 0% 或 TargetTotal=0）。
	GrowthSprout = 0
	// GrowthLevel1 完成率 1%–33%。
	GrowthLevel1 = 1
	// GrowthLevel2 完成率 34%–66%。
	GrowthLevel2 = 2
	// GrowthLevel3 完成率 67%–99%。
	GrowthLevel3 = 3
	// GrowthBloomed 完成率 100%，完全展开。
	GrowthBloomed = 4
)

// growthTier 把 (done, target) 映射为生长档位（整数百分比，向下取整）。
func growthTier(done, target int) int {
	if target <= 0 || done <= 0 {
		return GrowthSprout
	}
	if done >= target {
		return GrowthBloomed
	}
	switch pct := done * 100 / target; {
	case pct <= 33:
		return GrowthLevel1
	case pct <= 66:
		return GrowthLevel2
	default:
		return GrowthLevel3
	}
}

// GetPetGrowth 返回宠物窗口「芽」生长所需的今日完成数、目标数与档位。
func (s *Service) GetPetGrowth() (*PetGrowth, error) {
	now := time.Now()
	today := now.Format(model.DateLayout)
	_, _, _, doneTotal, targetTotal, err := s.activeProgress(now, today)
	if err != nil {
		return nil, err
	}
	return &PetGrowth{
		DoneTotal:   doneTotal,
		TargetTotal: targetTotal,
		Growth:      growthTier(doneTotal, targetTotal),
	}, nil
}
```

### 3.3 app.go 绑定方法

```go
// GetPetGrowth 返回宠物芽生长所需的今日完成数、目标数与档位（极轻量，供宠物窗口轮询）。
func (a *App) GetPetGrowth() (*service.PetGrowth, error) {
	g, err := a.svc.GetPetGrowth()
	if err != nil {
		return nil, apperr.Wrap(err)
	}
	return g, nil
}
```

放置位置：`GetTodayStats()` 同段（「今日统计与打卡」分区），`// gen:bind` 之前。护栏 `bindings_test.go` 只扫 `app.go`，故方法必须写在这里。

### 3.4 前端镜像与五档几何

```ts
// frontend/src/lib/growth.ts（仅镜像数值，不镜像 growthTier 判定逻辑）
export const GrowthTier = {
  Sprout: 0,   // 小芽苗 / 健康小苗（0% 或 TargetTotal=0）
  Level1: 1,   // 1%–33%
  Level2: 2,   // 34%–66%
  Level3: 3,   // 67%–99%
  Bloomed: 4,  // 100%
} as const

export type GrowthTierValue = (typeof GrowthTier)[keyof typeof GrowthTier]

/** 档位切换的缓动时长（ms），对应 PRD R6。 */
export const PET_GROWTH_TRANSITION_MS = 300
```

**五档几何参数**（作用在 `.pet__growth` 整组，origin 在茎基 (96,46)）：

| 档位 | class | `.pet__growth` transform | 叶 | 附加元素 |
|---|---|---|---|---|
| 0 | `.pet--growth-0` | `scale(0.55)` | 两片小叶（现有 `d` 不变，整组缩小） | 无 |
| 1 | `.pet--growth-1` | `scale(0.72)` | 两叶 | 无 |
| 2 | `.pet--growth-2` | `scale(0.86)` | 两叶（颜色保持 `--color-success`） | 无 |
| 3 | `.pet--growth-3` | `scale(1.0)` | 两叶全开 | 无 |
| 4 | `.pet--growth-4` | `scale(1.12)` | 两叶全开 | `.pet__bloom` 芽点浮现（`fill: var(--color-success)`） |

> 茎高 / 叶 scale 统一用「整组 scale + origin 茎基」实现：苗 → 挺立 → 更高 → 全开 → 加芽点；stroke 随 scale 等比变细/变粗，恰好读成「幼苗细茎 → 成株粗茎」。各 scale 为初值，观感可微调。

### 3.5 类图

见 `docs/class-diagram.mermaid`。

---

## 4. 数据流

见 `docs/sequence-diagram.mermaid`。

---

## 5. 任务列表（有序，含依赖）

> 本变更为增量：无新增依赖、无新增配置/入口，故不设独立「项目基础设施」任务；T01（service 数据层）即本特性的基础设施层。

### T01 — 后端生长领域（共享助手 + PetGrowth + growthTier + 单测）
- **Source Files**：`internal/service/checkin.go`（改）、`internal/service/growth.go`（新）、`internal/service/growth_test.go`（新）
- **Dependencies**：无
- **Priority**：P0
- **验证**：`go test ./internal/service/...` 通过（growthTier 全边界 + 既有 GetTodayStats 测试不回归）。

### T02 — 绑定方法与生成产物（GetPetGrowth + 重新生成 + 护栏）
- **Source Files**：`app.go`（改）、`frontend/bindings/morrow/`（app.ts / app.js，重新生成）
- **Dependencies**：T01
- **Priority**：P0
- **验证**：`wails3 generate bindings`（或 `make dev`）后 `go test ./internal/guard/` 通过（bindings 对账含 `GetPetGrowth`、layer 护栏不越界）；绑定产物出现 `GetPetGrowth`。

### T03 — 前端镜像、接线与生长视觉 + 文档
- **Source Files**：`frontend/src/lib/growth.ts`（新）、`frontend/src/views/Pet.vue`（改）、`docs/pet-character.md`（改）
- **Dependencies**：T02
- **Priority**：P0
- **验证**：`make lint` 通过（ESLint 无硬编码 hex + `vue-tsc` 类型）；`make dev` 手动验收五场景（点完成→芽及时长、100% 全开、次日归零、TargetTotal=0 小苗、读不到→保持/小苗）。

---

## 6. 依赖包（预期零新增，确认）

**零新增**。Go 侧仅 stdlib（`time` 已有）；前端复用 Vue 3 与既有 `@wailsio/runtime` / bindings。`deps.yaml`、`go.mod`、`frontend/package.json` 均不改动。

---

## 7. 共享知识

- **档位枚举单一真相在 Go**：`internal/service/growth.go` 的 `Growth*` 常量（0..4）；前端 `lib/growth.ts` 仅镜像数值并附边界注释，**不复制 `growthTier` 判定逻辑**（AGENTS 铁律 1）。该枚举不是事件/错误码/页大小常量，**不进入 `parity_test.go`**，靠注释约定人工同步。
- **`GetPetGrowth` 返回 `growth` 为 int 0..4**，前端 class 后缀直接用该 int（`pet--growth-${growth}`）。
- **档位边界**：0%（或 TargetTotal=0）→ 0；1–33 → 1；34–66 → 2；67–99 → 3；100 → 4。
- **不新增事件常量**：生长刷新复用既有 `ReminderEvent.Changed` 与 4s 轮询，不新增宠物事件。
- **不新增颜色**：芽新增元素全部走既有 `--color-success`，不写死 hex（ESLint 强制）。
- **绑定纪律**：`app.go` 不 import gorm/database（`GetPetGrowth` 只委托 `a.svc`）；绑定方法必须写在 `app.go`（`bindings_test.go` 只扫该文件）；新增方法必须重新生成绑定产物，否则护栏报「绑定过期」。
- **`DoneTotal/TargetTotal` 语义**：与 `TodayStats` 完全一致（共用 `activeProgress`），仅统计【当前启用场景】下【启用提醒】的完成/目标。

---

## 8. 待明确事项

1. **「健康小苗」= 档位 0**：按 PRD「健康小苗档（档位 0 的形态）」理解，健康小苗与小芽苗共用档位 0 的同一种视觉，仅语义标签不同。若 PM 想要一个视觉上可区分的第 6 态，需回退重议（当前设计按同档处理）。
2. **100%「完全展开」的芽点**：设计里新增 `.pet__bloom`（茎尖小圆点，仅档位 4 出现）来表达「全开」。若希望更朴素（仅「更大的两叶」），删除该元素即可，其余几何不变。
3. **过渡时长**：建议 300ms（PRD R6），使用既有 `--ease-in-out` token；若与 sway/alert/pop 观感冲突，P0 可先瞬切（去掉 `transition`）。
4. **各档 scale 数值**：0.55 / 0.72 / 0.86 / 1.0 / 1.12 为初值，需按实际观感微调（不影响任何接口）。
5. **是否在气泡内显示「今日完成率 N%」**：PRD 未提，默认不做（芽即进度表达）。
6. **跨 100% 的强化庆祝（R5）**：P1，本变更不做。P0 下跨 100% 时档位自然到 4（全开），既有的「待处理清空 → jumping」庆祝照常播，二者视觉可叠加、语义不混淆。
