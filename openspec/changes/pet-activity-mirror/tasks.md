# tasks — 小满跟着你在做什么

> 状态：**阶段 1–6、7.1–7.3 与两轮审阅修正完成**（`make test` / `make lint` / `make e2e-pet` 全绿）；
> 7.4 剩余项是真机观感确认；8.4 随归档生成。
> 实施顺序按阶段推进，每个阶段结束跑一次 `make test`。
> 只记 `make <target>`，不记零散脚本。

## 1. Go：活动档位与快照

- [x] 1.1 `internal/sit/activity.go`（新）：`ActivityLevel` 三档
      （`ActivityBusy = "busy"` / `ActivityRecent = "recent"` / `ActivityAway = "away"`，
      字符串字面量常量——`parity_test.go` 只认 BasicLit）；
      `ActivityFor(idle time.Duration) ActivityLevel` 纯函数；
      `NewActivity(category Category, idle time.Duration) *Activity`——`category == ""` 时返回 `nil`
- [x] 1.2 阈值集中成 `ActivityBusyMax = 20 * time.Second`、`ActivityRecentMax = 3 * time.Minute`，
      注释写清"20s 对应 4 个采样点"与"3min 略宽于 `FocusSwitchGrace` 的理由"
- [x] 1.3 `internal/sit/contract.go`：`Activity{Category, Level}` + `Snapshot.Activity *Activity`；
      注释重申红线（无应用名/标识/窗口标题）
- [x] 1.4 `sit_runtime.go`：`sitRuntime.liveCategory`，由 `sampleAppUsage()` 的返回值赋值
      （不新增采样）；`resetInMemoryLocked()` 里清空
- [x] 1.5 `snapshot()` 在 `!settings.Enabled` 分支之后组装
      `base.Activity = sit.NewActivity(r.liveCategory, pause.GetIdleDuration())`
- [x] 1.6 `internal/sit/activity_test.go`：表驱动覆盖 0 / 19.9s / 20s / 2m59s / 3m / 20m / 负值；
      另加 `TestActivityThresholdsOrdered` 守住 `BusyMax < RecentMax`、`RecentMax > FocusSwitchGrace`、
      `RecentMax < ResetIdle` 三条序关系（改大 `FocusSwitchGrace` 就会红）

## 2. 前端契约镜像

- [x] 2.1 `frontend/src/lib/sit.ts` 新增 `SitActivity = { Busy, Recent, Away }`
      + `SitActivityValue` + `SitActivityInfo` 接口，`SitSnapshot` 补 `activity?`
- [x] 2.2 `internal/guard/parity_test.go` 的 `mirrors` 表增加「活动档位枚举」一项

## 3. 前端常量（单一真相）

- [x] 3.1 `PetMirrorPose = { Working, Listening, Watching }` + `PetMirrorPoseValue`
- [x] 3.2 `PET_MIRROR_POSE_BY_CATEGORY`：用 `[SitCategory.Coding]: 'working'` 计算键，六项齐全
- [x] 3.3 `PET_MIRROR_ROW`：`working → PetAnim.Review`，其余 → `PetAnim.Idle`
- [x] 3.4 `PET_MIRROR_CONFIRM_TICKS = 3`、`PET_MIRROR_MIN_HOLD_MS = 20000`、
      `PET_MIRROR_SHAKE_MS = 1600`（手臂摆动周期，CSS 通过 `--pet-mirror-shake-ms` 消费）

## 4. 前端接线（`Pet.vue`）

- [x] 4.1 `mirrorPose` / `mirrorRow` ref + `mirrorCandidate` / `mirrorCandidateTicks` / `mirrorSince` 普通变量
- [x] 4.2 `canApplyMirror()` 门控（含 `activity` 为空与档位 `away`；**不含** `prefersReducedMotion`，理由见 design 11.2）
- [x] 4.3 `tickMirror(nowMs)`：候选变更 → 重新计数并 return；稳定够久且过了最小停留 → `applyMirror`
- [x] 4.4 `pollSettings()` 里 `tickDoze()` 之后、`tickMicro()` 之前调用（**没有新增 `setInterval`**）
- [x] 4.5 `paintCurrentFrame()`：`PET_ANIM_ROW[anim === Idle ? mirrorRow : anim]`——**永不写 `anim`**
- [x] 4.6 ~~`canPlayMicro()` 增加 `mirrorPose !== ''` 条件~~ → **已改**：见「审阅修正」第 3 条，
      改为 `pickMicroAction()` 过滤选择池（只剔除抢同一元素的动作）
- [x] 4.7 模板根节点挂 `mirrorClassValue`（仅 builtin + 待机时非空）
- [x] 4.8 `onUnmounted` 里 `clearMirror()`（与 `clearMicroAction()` 并列）

## 5. 样式

- [x] 5.1 `pet--mirror-working`：坐低 10px + 缩放 93% + 侧压 4°，双手摆动（`pet-mirror-typing` keyframes）
- [x] 5.2 `pet--mirror-listening`：侧倾 6°，右手**绕肩旋转 -45°**（平移会把袖子挪进身体里）
- [x] 5.3 `pet--mirror-watching`：放大 4%，`.pet__eyes` 放大 15%（挂在父组，不碰眨眼的 `.pet__eyes-open`）
- [x] 5.4 既有 `prefers-reduced-motion` 里停掉手臂摆动、保留静态倾斜
- [x] 5.5 只用 `transform`，零新增 hex（ESLint 通过）

## 6. 护栏

- [x] 6.1 `petBehaviorConstantPattern` 加入 `MIRROR`
- [x] 6.2 `TestPetMirrorCoversAllCategories`：解析映射表与 `PetMirrorPose`，
      双向比对 `sit.AllCategories()`，并断言"每个姿态都有类别能产出它"；解析到 0 条即 Fatal
- [x] 6.3 抽出 `assertGateCovers()`，`canPlayMicro` 与 `canApplyMirror` 各自校验自己的压制表
- [x] 6.4 断开性验证（三处均已还原）：
      - 删掉映射表里的 `media` → FAIL：「类别 "media" 在 … 里没有映射」
      - `PET_MIRROR_MIN_HOLD_MS` 换成字面量 `20000` → FAIL：「定义了却没有被引用」
      - 从 `canApplyMirror()` 删掉 `dockSide` 判断 → FAIL：「漏掉了 已贴边收起（dockSide）」

## 7. 验证

- [x] 7.1 `make test` + `make lint` 全绿
- [x] 7.2 绑定重生成 `wails3 generate bindings -clean=true -ts -i`
      （`Snapshot` 加字段后 `vue-tsc` 立刻报 `Property 'activity' does not exist`，属正常）
- [x] 7.3 `make e2e-pet`：新增 `state-mirror-working/listening/watching` 三张截图，
      菜单断言仍全通过；三张均确认无误裁、无错位
- [ ] 7.4 真机手测（人工确认，不能自动化）：
      - [x] 类别随前台应用变化（**已用日志证实**，两轮）：
        `14:50:23 listening/communication` → `14:50:47 watching/browsing` →
        `14:55:50 working/coding` → `14:56:18 watching/browsing`（含最小停留生效）
      - [x] 零新增系统权限（全程只读前台应用与空闲时长）
      - [ ] 观感：编辑器里连续打字 → 宠物"坐低变小"；切聊天 → 侧倾抬手；切浏览器 → 变宽变扁
      - [ ] 停手 3 分钟 → 姿态消失、回待机（继续挂久则按既有规则打盹）
      - [ ] 来回 alt-tab 每 5 秒一次 → 姿态**不**变化
      - [ ] 点一下宠物 → 挥手照常（不应该"站起来再蹲回去"）
      - [ ] 手动暂停 → 下一次轮询内姿态立刻消失
      - [ ] 待处理提醒到点 → 立刻切「等待处理」，结束后按当前类别恢复
      - [ ] 开会（腾讯会议/Zoom）→ 进入暂停态，**不**出现「托腮」
      - [ ] 关掉应用画像 → 全天不再有任何姿态变化；且关闭后不再出现 `镜像姿态已切换` 日志
      - [ ] 若有导入素材包：确认 `review` 行观感（见 design 11.4）

## 8. 文档同步

- [x] 8.1 `docs/pet-character.md` 新增「镜像：它在跟着你」一节（三态表 + 三条约束 + 边界）
- [x] 8.2 `docs/map.md`：`internal/sit/`、`lib/pet.ts`、`views/Pet.vue` 三行同步
- [x] 8.3 `Settings.vue` 应用画像说明补一句（开启时"宠物也会跟着你在做什么改变姿态"，
      关闭时"宠物也不会跟随你的活动"）——不新增开关
- [ ] 8.4 `openspec/specs/pet-mirror/spec.md` 随归档生成（本变更 `specs/` 为 delta）

## 审阅修正（首轮上机验收「看不到」之后，2026-10-27）

排查把范围收窄到"幅度"而非"链路"（证据链见 `design.md` 11.5），改了三处：

1. **姿态幅度按实际渲染尺寸重算**：`pet_scale=0.5` → 屏幕上只有 96×104，原来的
   2–3px/1.5° 只剩 1–1.5px。改为轮廓级变化（坐低 10px + 93% 缩放 + 4° 倾斜）。
   `make e2e-pet` 同时产出 0.5 倍的 `half-*.png`，并可用包围盒量化：
   working 比 idle 矮 11 CSS px、重心低 6 CSS px。
2. **`watching` 不再"上抬"**：画板 192×208 上方几乎没有余量（芽尖 y≈7），
   上抬超过 6px 就裁切。三个姿态统一改成"向下 + 缩放 + 倾斜"。
3. **微动作不再整天禁演**：`canPlayMicro()` 里的镜像条件会把 pet-autonomy 在白天全关掉。
   改为 `pickMicroAction()` 只剔除 `stretch`（抢身体层）与 `watching` 下的 `look`（抢眼睛层）。

另加：姿态切换写一行 `镜像姿态已切换 {pose, category, at}` 前端日志——
"没反应"这类报告必须能一次区分「链路没通」与「幅度太小」。

## 审阅修正 · 第二轮（按建议把缺口补掉，2026-10-27）

- [x] 8.5 时序状态机抽成纯函数：`lib/pet.ts` 新增 `PetMirrorState` / `advanceMirror()`；
      `Pet.vue` 只剩"判门控、取候选、落到 ref"，不再自己数 tick
- [x] 8.6 新增 `scripts/e2e/mirror_logic.py`（挂进 `make e2e-pet`）：
      在真实页面里 `import('/src/lib/pet.ts')`，断言 7 条——
      3 次确认才进入 / 候选抖动不切换 / 20 秒最小停留 / 压制立即清除 /
      变 other 也要确认与停留 / 挥手压过镜像手臂（working、listening 各一条）
- [x] 8.7 断开性验证（均已还原）：
      去掉 `:not(.pet--waving)` → 第 6 条红（`pet-mirror-typing-…` 盖掉了挥手）；
      删掉最小停留判断 → 第 3、5 条红
- [x] 8.8 挥手不再打断姿态：`mirrorClassValue` 允许 `Waving`；镜像的手臂规则加
      `:not(.pet--waving)` 让位（否则同优先级下后写的镜像规则赢，"点了不挥手"且不报错）
- [x] 8.9 `watching` 加到 `scaleX(1.1) scaleY(0.93)`（长宽比 0.75 → 0.87）
- [x] 8.10 护栏连改：`TestPetBehaviorConstantsAreConsumed` 允许在 `lib/pet.ts` 模块代码内消费，
      但必须剥掉常量定义行；用注入 `PET_MIRROR_FAKE` 做过红态验证
- [x] 8.11 隐私边界：`snapshot()` 的 Activity 加判 `ProfileEnabled`；
      `setProfileEnabled(false)` 清 `liveCategory`（撤同意后不再输出结论）

### 明确保留的缺口（不是遗漏）

- **庆祝（`Jumping`）期间仍会摘掉镜像 class**：庆祝是 1600ms 的整身动作，
  叠一个蜷着的底态会变形。挥手才值得为它做让位。
- **休息陪做（`restAccompanying`）不拦镜像**：口径是"它也陪你歇着"。
  要改成拦，加一行 + 门控表加一项即可。
- **sprite 模式只有 `working`** 有身体语言（另两个降级为待机行），等新素材。

## 实施中新发现（已同步进 design 第 11 节）

1. **`PET_MIRROR_ROW` 写在 `PetAnim` 之前会白屏**：TS 类型检查全绿，运行时抛 TDZ
   （`Cannot access 'PetAnim' before initialization`）。静态检查看不见，只能靠顺序约定 + 注释。
2. **镜像门控刻意比微动作少一条 `prefersReducedMotion`**：减弱动态约束的是"动"不是"样子"。
   两条门控表分开写、并把差异写进注释，避免下一个人"顺手对齐"后让减弱动态用户彻底失去镜像。
3. **静帧看不出 working**：`make e2e-pet` 的三张截图里，`working` 与 `watching` 几乎一样——
   working 的区别在**手臂在摆**（动），眼睛大小差 12% 只是辅助。故截图只能验"有没有坏"，
   验不出"像不像在敲键盘"；后者留给 7.4 真机。
4. **`listening` 的抬手位移有物理上限**：SVG 的手臂是两段 `line`、没有关节，
   超过 `-8/-12` 就会离开身体轮廓。已收到 `-6/-10`。
5. **`Snapshot` 加字段的连锁反应**：`frontend/bindings/` 是产物，不加字段不重新生成，
   `vue-tsc` 会先报 `Property 'activity' does not exist`。这是"前端类型来自产物"的正常代价。
