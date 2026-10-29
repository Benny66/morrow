# tasks — 小满的自主时间

> 状态：阶段 0–5 与 6.1 完成（`make test` / `make lint` 全绿）；6.2–6.7 是**真机手测**，待人工确认。
> 实施顺序按阶段推进，每个阶段结束跑一次 `make test`。
> 只记 `make <target>`，不记零散脚本。

## 1. Go：长期档位

- [x] 1.1 `internal/service/growth.go`：`PetGrowth` 增加 `TotalDoneDays int json:"total_done_days"`
      （累计完成天数：`action=done` 的**不同 `date_key` 个数**，全局口径、不按场景过滤，
      理由见 `design.md` 第 2 节）
- [x] 1.2 `internal/service/growth.go`：新增 `longTermStage(days int) int`——阈值表集中成包级切片
      `longTermStageThresholds = []int{1, 7, 30, 100}`，循环取档，不写 if 链
- [x] 1.3 `PetGrowth` 增加 `Stage int`（形态档 = `max(longTermStage(total), growthStage(today))`）
      与 `TodayStage int`（今日神态档，沿用现有 `growthStage`）
      ——**`Stage` 的语义改为"形态档"**，旧前端读到它只会得到"持平或更大"的值
- [x] 1.4 `internal/service/growth_test.go`：`TestLongTermStage` 表驱动覆盖 0 / 1 / 6 / 7 / 29 / 30 / 99 / 100 /
      负数；`TestPetShapeStage` 断言"今日档高时形态不低、今日归零时形态等于长期档"；
      另加 `TestLongTermStageTableMatchesStageCount`（阈值表长度 = 档位刻度，防静默封顶）

## 2. 前端常量（单一真相）

- [x] 2.1 `frontend/src/lib/pet.ts` 新增：
      `PET_MICRO_ACTION_MIN_MS = 40000`、`PET_MICRO_ACTION_MAX_MS = 90000`、
      `PET_MICRO_ACTION_MS = 1200`（单次时长上限）、`PET_DOZE_AFTER_MS = 8 * 60 * 1000`、
      `PET_SLEEP_FPS = 4`、`PET_WAKE_MS = 300`；每条都写了"为什么是这个数"，并注明
      `PET_SLEEP_FPS` 与既有 11 行禁令的关系（禁令只约束常驻待机）
- [x] 2.2 `PetAnim` **不变**；微动作单独导出 `PetMicro = { Look, Stretch, Groom, Yawn }`
      + `PET_MICRO_POOL`（带权重顺序）+ `PetTodayMood`，注释说明它不是 `PetAnim`

## 3. 前端：行为调度器（`Pet.vue`）

- [x] 3.1 `microAction` / `nextMicroActionIn` / `dozeSince` 等普通变量，在既有 4 秒轮询 tick 内递减
      （**没有新增 `setInterval`**，只加了两个一次性 `setTimeout`：微动作结束、惊醒结束）
- [x] 3.2 `canPlayMicro()` 单一门控：无待处理、未暂停、未演示建议、气泡不在场、未贴边收起、
      未休息倒计时/预警、`prefers-reduced-motion` 未命中、`anim` 必须是 idle
- [x] 3.3 触发时按 `PET_MICRO_POOL` 权重随机选一个，`PET_MICRO_ACTION_MS` 后自动清除；
      不调用 `setAnim`、不改 `anim`
- [x] 3.4 打盹：`pauseReason ∈ {idle, lock}` 且持续 `PET_DOZE_AFTER_MS` → `dozing`；
      用**真实时间差**判定（锁屏期间计时器会被节流，数 tick 会永远等不到）；有待处理时不进入
- [x] 3.5 醒来：`wakeUp()` 幂等（轮询解除 / `onPetEnter` / 出现待处理三个入口），
      播一次 300ms 惊醒后回 idle
- [x] 3.6 贴边收起态：`canPlayMicro()` 与 `shouldDozeNow()` 都直接返回 false

## 4. 样式（`Pet.vue` 的 `<style>`）

- [x] 4.1 四个微动作关键帧：`pet-micro-look` / `pet-micro-stretch` / `pet-micro-groom` / `pet-micro-yawn`
- [x] 4.2 `pet--micro` 期间呼吸 `animation-play-state: paused`（**不是删掉**）
- [x] 4.3 打盹：闭眼 `scaleY(0.1)`、芽收拢、`.pet__svg` 呼吸拉长到 6.4s
- [x] 4.4 三档今日神态 class（`pet--today-full/part/thin`），用 `:not(.pet--sit-*)` 把优先级让给久坐情绪
- [x] 4.5 全部只用 `--color-primary-*` / `--color-success` 的透明度变化，无新增 hex
- [x] 4.6 减弱动态：既有媒体查询已覆盖呼吸/摇芽/眨眼/等待抖动；微动作由 JS 门控挡在源头，
      惊醒与挥手/庆祝保留

## 5. 护栏

- [x] 5.1 `internal/guard/pet_behavior_test.go`：解析 `Pet.vue` 的 `canPlayMicro` 函数体，断言命中
      pendingCount / paused / suggestionVisible / dockSide / prefersReducedMotion 五个标识；
      找不到函数体即 Fatal
- [x] 5.2 同文件第二条：`lib/pet.ts` 里匹配 `PET_(MICRO|DOZE|SLEEP|WAKE)_*` 的**每个**常量
      必须在 `Pet.vue` 的**实际代码**里被引用（import 块与注释都已剥掉）；解析到 0 个即 Fatal
- [x] 5.3 红/绿破坏性验证：
      - 删掉 `if (dockSide.value !== '') return false` → 门控护栏 FAIL，报"已贴边收起（dockSide）"
      - 把 `PET_SLEEP_FPS` 换成字面量 `4` → 常量护栏 FAIL，报"PET_SLEEP_FPS 没有被引用"
      - 两处改动均已还原，`diff` 与备份一致

## 6. 验证

- [x] 6.1 `make test` + `make lint` 全绿（含 gofmt / go vet / ESLint / vue-tsc）
- [x] 6.1a **新增数据库层测试**（`internal/service/growth_db_test.go`）：
      用真实 SQLite 覆盖 `totalDoneDays` 的三条口径（同日去重 / 跨场景都算 / 跳过与软删除不算），
      并用可注入时钟模拟跨天断言「形态不缩回」（`TestPetGrowthKeepsShapeAcrossDays`）
- [x] 6.1b **浏览器端目视冒烟**（vite dev + Playwright，240×380）：待机、微动作四种、
      打盹、惊醒、今日神态、高档形态各截一张，确认类挂上后宠物不变形/不裁切
- [ ] 6.2 静置 5 分钟：至少 2 次微动作；暂停后立刻不再出现；演示模式下完全不出现
- [ ] 6.3 锁屏 10 分钟回来：先看到惊醒，再回 idle；不是直接停在 idle
- [ ] 6.4 打盹期间到点提醒：立刻醒来并切 `waiting`，气泡正常出现，不丢提醒
- [ ] 6.5 隔夜：芽形态**不缩小**；跨档时长高一次
      ——「不缩小」这条逻辑已由 6.1a 的 `TestPetGrowthKeepsShapeAcrossDays` 覆盖，
      剩下的只是人眼确认"跨档时那次长高动画"好不好看
- [ ] 6.6 `prefers-reduced-motion`：无微动作、无打盹起伏，挥手/庆祝仍在
- [ ] 6.7 连续挂 1 小时：Activity Monitor 里 CPU 与改动前同量级；录屏仍看不到宠物（捕获排除回归）

## 7. 文档同步

- [x] 7.1 `docs/pet-character.md`：新增「待机行为」「形态与神态：两条轨道」两节；
      删掉"芽 = 今日完成率"的旧表述与"没有打卡数据通道"的过时结论
- [x] 7.2 `docs/map.md`：`views/Pet.vue`、`lib/pet.ts`、`internal/service` 三行同步
- [ ] 7.3 `openspec/specs/pet-autonomy/spec.md` 随归档生成（本变更的 `specs/` 为 delta，归档时合并）

## 审阅修正（self-review）

对照源码复查后修的 3 处口径问题（都不是逻辑错，但都会误导后来的人）：

1. **样式注释还在用旧口径**：`Pet.vue` 里那组 `.pet--growth-N` 的注释写着
   「芽随今日完成率生长」——而它们现在由**形态档**（= max(长期, 今日)）驱动。
   注释不改，下一个人就会以为跨天归零是"设计如此"。
2. **规格里的帧率说法只覆盖一半**：原文「打盹期间动画帧率 SHALL 降至 4fps」——
   builtin 形象根本没有 rAF 循环，它的"变慢"是 CSS 动画时长。已改成两种模式分别表述，
   否则归档后基线是错的。
3. **今日神态的初值是 `thin`（蔫）**：挂载后到第一次 `syncGrowth` 之间会先渲染一帧，
   新用户打开宠物看到的是"一株蔫芽"。改成 `part`（中性），扫一眼就知道没数据。

另记一条**设计取舍**（不是 bug，写进 `design.md` 3.2）：打盹依赖智能免打扰开启。
关掉自动暂停后，宠物再也不会睡。要修应该让 `pause` 在用户关掉自动暂停时仍上报
`Reason=idle`（只作信号），而不是在宠物侧另判一套空闲——那会造出第二处真相。

## 实施中新发现

- **护栏第一版被 import 骗过**：常量护栏最初只剥离注释，`PET_SLEEP_FPS` 只要在 import 列表里
  出现就判"已引用"——把实际用法换成字面量 `4` 之后它仍然是绿的。补了 `stripImportBlocks`
  才真正红。这类"看起来在测、其实在测自己"的护栏，只有做破坏性验证才会暴露。
- **打盹降帧要分两条路**：rAF 循环只在 sprite 模式跑，builtin 全由 CSS 驱动。
  于是 `PET_SLEEP_FPS` 只影响 sprite，builtin 的"变慢"是 `.pet--dozing` 把呼吸时长改成 6.4s。
  两条路不改同一处，已在 `design.md` 3.1 记录，避免后人以为常量没生效。
- **形态档必须在 Go 侧合成**：一开始想在前端 `max(stage, today_stage)`，但那样"档位判定"
  就出现了第二处实现（铁律 1）。最终 `Stage` 直接由 service 返回合成后的值，前端只做
  "整数 → class"，`TodayStage` 单独给神态用。
- **绑定必须重新生成**：`PetGrowth` 加了字段后 `vue-tsc` 立刻报
  `Property 'today_stage' does not exist`，跑 `wails3 generate bindings -clean=true -ts -i` 即解。
  这是"前端类型来自产物"的正常代价，不是手改 models.ts 的理由。

## 审阅修正（第二轮）

- **形态回退**：旧版配置首次运行把 `max(长期, 今日)` 播种成高水位一次
  （`config.PetShapeHighWater/PetShapeSeeded`），之后今日档只走 `TodayStage`；
  全新安装的高水位从 0 开始。
  新增 `TestComposePetShape`、`TestPetShapeNeverShrinks` 与 DB 回归
  `TestPetGrowthUpgradeDayKeepsShapeNextMorning`（今日 100%、累计 1 天，次日仍为 4 档）。
- **无目标不蔫**：`PetGrowth` 增加 `has_target`；前端在 false 时用中性神态。
- **sprite 惊醒**：`wakeUp()` 在 sprite 模式用 `jumping` 行播 `PET_WAKE_MS`。
- **回显回落**：`ensurePetOnScreenLocked` 在设置保存/显隐/演示退出/贴边展开的显示路径上
  检查当前位置是否仍在任一屏幕，否则回落主屏右下角。
- **单一真相/视觉**：微动作时长、打盹呼吸周期、菜单 padding 改由 CSS 变量注入；
  理芽关键帧以 `--leaf-left-angle` 为基准，不再从 0deg 跳变。
- **规格/文档**：`pet-autonomy` spec 改为「高水位 + 一次性迁移」，补已知限制；
  `docs/pet-character.md` 同步。6.2–6.7 真机手测仍未执行。
