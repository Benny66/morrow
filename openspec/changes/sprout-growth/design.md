# 增量设计（最终版）— 宠物「小满」芽随今日完成率生长

> 本文件是最终实现依据，与磁盘上的代码一致。此前的 `design-final.md` 与本文档同源
> （复用 GetTodayStats 投影 + growthStage），其余更早草稿（sumTodayTotals / activeProgress /
> growthTier / 返回 done+target+growth）一律作废，不要据此实现或审查。

## 1. 决策摘要（最终实现）

| # | 决策 | 实现 |
|---|---|---|
| D1 | 数据通路 | 新增 `App.GetPetGrowth()` → `Service.PetGrowth()`，**内部复用 `GetTodayStats()` 投影**，不另写计数 SQL |
| D2 | 档位计算 | 后端 `growthStage(done,target)` 整数分段 0–4，前端不换算 |
| D3 | 返回结构 | `PetGrowth{DateKey, Stage}`（只回 date_key + stage，不回 done/target） |
| D4 | 视觉 | 内层 `.pet__sprout-body`(scale) + `.pet__leaf--left/right`(rotate)，与父 `.pet__sprout` 动画分层不冲突 |
| D5 | 刷新 | `reminder:changed` 事件 + 4s 轮询 + 挂载首拉，收口 `syncGrowth()` |
| D6 | 降级 | TargetTotal=0→0；读不到→保持上次+logWarn；首启→0 |

## 2. 档位（growthStage，整数运算）

| 档位 | 常量 | 完成率 | 含义 |
|---|---|---|---|
| 0 | GrowthStageSeedling | 0%（或未设目标/无完成） | 小芽苗/健康小苗 |
| 1 | GrowthStageSprout | 1%–33% | 芽挺立 |
| 2 | GrowthStageHalf | 34%–66% | 茎变高 |
| 3 | GrowthStageAlmost | 67%–99% | 叶全开 |
| 4 | GrowthStageFull | 100%+ | 完全展开 |

边界（`done*100/target` 向下取整）：`target<=0||done<=0→0`；`pct>=100→4`；`pct>=67→3`；`pct>=34→2`；其余→1。

## 3. 视觉五档几何（.pet__sprout-body 整体 scale，origin 茎基 (96,46)）

| 档位 | scale | 左叶 | 右叶 |
|---|---|---|---|
| 0 | 0.55 | +6°(内收) | -6°(内收) |
| 1 | 0.70 | 0° | 0° |
| 2 | 0.85 | -8° | +8° |
| 3 | 1.00 | -14° | +14° |
| 4 | 1.10 | -20° | +20° |

叶数量恒 2；颜色 `--color-success`；transition 300ms `--ease-in-out`。负角=展开、正角=内收（左叶），右叶镜像。

## 4. 文件清单（已实现）

- `internal/service/growth.go`（新）：GrowthStage* 常量 + PetGrowth{date_key,stage} + growthStage + PetGrowth()（复用 GetTodayStats）
- `internal/service/growth_test.go`（新）：growthStage 表驱动 14 用例
- `app.go`（改）：GetPetGrowth 绑定（委托 a.svc.PetGrowth，err 经 apperr.Wrap）
- `frontend/src/lib/pet.ts`（改）：GrowthStage 类型 + growthStageClass()（仅 number→class+夹取）
- `frontend/src/views/Pet.vue`（改）：growth ref + syncGrowth + 接线 + .pet__sprout-body 模板 + 五档 CSS
- `frontend/bindings/morrow/app.ts` + `models.ts`（生成产物，手写补齐，make gen 覆盖）

注：`internal/service/checkin.go` **未改动**（PetGrowth 复用整段 GetTodayStats，无共享助手抽取）。

## 5. 共享知识

- 档位唯一真相：`growthStage`（仅 growth.go 一处）。
- DoneTotal/TargetTotal 唯一真相：`GetTodayStats`（PetGrowth 复用它投影，不另写 SQL）。
- 前端不重算档位、不镜像档位常量，只据 stage 整数映射 CSS class（`growthStageClass` 只做夹取，无阈值）。
- 不新增事件常量（复用 ReminderEvent.Changed）、不新增模型、不新增依赖、不新增颜色。
- 绑定方法在 app.go，错误经 apperr，不 import gorm/database。

## 6. 待验证

- 未编译（本会话无 shell）。需在有 shell 环境：`make gen`（重生成绑定）→ `make test` → `make lint`。
- 手工验收五场景：点完成→芽及时长 / 100%全开 / 次日归零 / Target=0 小苗 / 读不到保持上次。
