# 小满跟着你在做什么（pet-activity-mirror）

## Why

BongoCat（24k 星）靠的不是玩法深度，而是**零延迟的 1:1 对应**：你按一个键它动一下，
你动鼠标它眼睛跟着。技术上是 `CGEventTap`（macOS）/ `Raw Input`（Windows）把每一次输入
映射成动作帧——代价是「输入监控」授权，且素材里必须有"打字"这一行。

Morrow 不做这件事，也不需要。因为**它已经知道你在做什么**：`sit_runtime.go` 每 5 秒
读一次前台应用（`pause.GetFrontmostApp`，NSWorkspace bundleID / `GetForegroundWindow`，
**零权限**）、映射成六类行为类别（coding / design / browsing / communication / media / other），
并经 `GetSitSnapshot` 送进宠物窗口。

缺的不是采集，是**一张映射表**：这些类别至今只影响气泡文字与久坐页的数字，
从未影响宠物的身体。于是宠物在"你在写代码"和"你在看视频"时表现得一模一样——
它对**你的状态**没有回应，只对自己的时钟有反应。

同时，图集 9 行里有 5 行（running / running-right / running-left / review / failed）
从未被任何状态机触达（`docs/pet-competitive-analysis.md` 的 G5）：素材付过费了，行为引擎没有。

本变更补上这条链路，且**不新增权限、不新增素材、不新增事件域、不新增开关**。

## What Changes

| # | 变化 | 落点 |
|---|------|------|
| 1 | 「活动档位」纯函数：空闲时长 → busy / recent / away，零新增状态、零新增采样 | `internal/sit/activity.go`（新） |
| 2 | 快照补 `activity{category, level}`；契约红线不变（仍无应用名/标识/窗口标题） | `internal/sit/contract.go`、`sit_runtime.go` |
| 3 | 类别 → 姿态映射（working / listening / watching），前端唯一真相 | `frontend/src/lib/pet.ts` |
| 4 | 镜像是**待机的替身**而非新状态：只在 `anim === idle` 时替换绘制行，含 12s 确认 + 20s 最小停留 | `Pet.vue` 的 `tickMirror()`，挂在既有 4 秒轮询上 |
| 5 | sprite 借用既有 `review` 行（埋头）；builtin 用三个静态姿态 class（前倾 / 托腮 / 注视） | `Pet.vue` 的 `<style>` |
| 6 | 门控与接线护栏：六个类别全覆盖、新常量必须被引用、压制条件必须齐 | `internal/guard/pet_behavior_test.go`、`parity_test.go` |

## Impact

- Affected specs: `pet-mirror`（新增能力，本轮首次建立基线）
- Affected code: `internal/sit/activity.go`（新）、`internal/sit/contract.go`、
  `sit_runtime.go`、`frontend/src/lib/sit.ts`、`frontend/src/lib/pet.ts`、
  `frontend/src/views/Pet.vue`、`internal/guard/pet_behavior_test.go`、`internal/guard/parity_test.go`
- **不改** `app.go`：`GetSitSnapshot` 绑定已存在，只扩展返回结构体字段
- **不改** `deps.yaml`：零新增依赖
- **不改** `internal/pause`：活动档位只是既有空闲信号的第二个消费者，不是第二处判定
- 文档：`docs/pet-character.md` 新增「镜像：它在跟着你」一节；`docs/map.md` 同步三行
- 设置页：应用画像的说明文案补一句「关掉后宠物不再跟随你的活动」——**不新增独立开关**

## Non-goals

- **按键级镜像**（照搬 BongoCat）：需要 macOS「输入监控」授权与 Windows 隐藏窗口注册，
  与 `internal/pause` 现在"全程零权限"的路线正面冲突；且图集没有"打字"行，
  真做等于换素材体系（Live2D 或新图集）= 重做宠物。
- **Live2D / 新素材**：本变更只在既有 9 行内做，缺口显式降级（见 design 第 6 节）。
- **新增 `pet:*` 事件**：状态一律走既有 4 秒轮询快照（与 pet-autonomy 同一约束）。
- **独立开关**：镜像挂在「应用画像」这一个既有同意之下，关掉画像即关掉镜像。
- **漫游 / 摸头 / 声音 / 投喂**：与"宠物在回应你"无关或风险未评估，各自单独决策。
- **让镜像影响气泡文案**：镜像只改体态。多说一句话就多一份打扰预算的竞争，
  而久坐软提醒与记忆气泡已经共享每天 5 次。
