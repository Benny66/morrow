# 小满的自主时间（pet-autonomy）

## Why

桌宠在「没有提醒」的 99% 时间里只是一个会呼吸的贴图：`idle` 是纯 CSS 循环，
用户离开时只有一句气泡文案（`'你好像离开了，我先不打扰'`）。而业内（VPet / clawd-on-desk /
BongoCat）把它当作**一直在场的状态载体**，靠待机微行为与睡眠序列维持"活着"的错觉——
clawd 甚至为"长时间无操作"专门做了四段式睡眠序列。

同时，芽的形态由**今日**完成率驱动（`growth.go` 的 `growthStage(done, target)`），
`date_key` 翻篇即归零：昨天长到 1.1 倍、叶子全开，今早打开电脑缩回 0.55 倍。
用户看到的是"我一夜之间退步了"，这与 `pet-character.md` 定的"陪伴"定位相反。

本变更补上这两件事，且**不新增权限、不新增素材、不新增事件域**。
证据与业内对照见 `docs/pet-competitive-analysis.md` 的 D1 / D2。

## What Changes

| # | 变化 | 落点 |
|---|------|------|
| 1 | 待机微行为：张望 / 伸懒腰 / 理芽 / 打哈欠，40–90s 随机一次 | `Pet.vue` 新增调度器（挂在既有 4 秒轮询 tick 上）+ 纯 CSS 关键帧 |
| 2 | 打盹与惊醒：暂停原因为 idle/lock 持续 8 分钟后打盹；回来先"惊一下"再回 idle | 复用已轮询的 `PauseState.Reason`；`lib/pet.ts` 新增 `PET_SLEEP_FPS` |
| 3 | 长期成长：芽的**形态**改由「累计达标天数」档位驱动，单调不减 | `internal/service/growth.go` 新增 `TotalGoalDays` 与 `longTermStage()` |
| 4 | 今日神态：今日完成率不再改形态，只改神态（叶子抖动力度 / 高光 / 轻垂） | `Pet.vue` 拆成两级 class |
| 5 | 护栏：长期档位边界表驱动 + 宠物门控接线断言 | `growth_test.go` 扩展；`internal/guard/pet_gate_test.go` 新增 |

## Impact

- Affected specs: `pet-autonomy`（新增能力规格，本轮首次建立基线）
- Affected code: `internal/service/growth.go`、`internal/service/growth_test.go`、
  `frontend/src/lib/pet.ts`、`frontend/src/views/Pet.vue`、`internal/guard/pet_gate_test.go`（新）
- **不改** `app.go`：`GetPetGrowth` 绑定已存在，只扩展返回结构体字段
- **不改** `deps.yaml`：零新增依赖
- 文档：`docs/pet-character.md` 的状态表需要同步（取消"芽形态=今日完成率"的旧表述）

## Non-goals

- **漫游**（沿屏幕边缘走、窗口移动）：会与 `pet_dock.go` 的 500ms 轮询判定互相咬合，
  风险单独评估，不进本变更。
- **sprite 模式的微动作**：图集只有 9 行固定动作，硬塞会错行；本变更只做 builtin。
- **声音、摸头、AI 台词**：分别涉及打扰成本、命中测试、隐私边界，各自单独决策。
- **饥饿 / 体力 / 投喂**：把陪伴变成待办，与"温和不强制"冲突（见分析文档第六节）。
- **新增 `pet:*` 事件**：事件契约里没有宠物域，多一个域就多一处镜像要维护。
  新状态一律走既有 4 秒轮询快照。
