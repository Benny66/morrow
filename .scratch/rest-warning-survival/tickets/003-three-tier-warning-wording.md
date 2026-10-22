# 003 — 预警措辞分三档，宠物气泡与菜单栏共用同一出处

**Blocked by**: 无（可与 001 并行开工）

**状态**: 已完成（2026-10-22；单测 + 护栏 + 注入证据；GUI 走查转 005）

> 规格：`docs/specs/2026-10-22-rest-warning-survival.md` 的 WS-13…WS-17、§5.2 的 WS-T9/T10/T11。
> 术语：`CONTEXT.md` 的「预警措辞（Warning Wording）」。

## 目标

预警那句活话终于跟剩余时间对得上：剩余 45 秒说「1 分钟后」，20 秒说「半分钟后」，
最后 10 秒和到点空窗说「马上」；菜单栏顶部那行与宠物气泡逐字相等，因为它们是同一个函数的返回值。

本票与 001 无阻塞关系：措辞只读 `Snapshot().RemainingSec`，判据只读占用事实。
两票并行时唯一要小心的是 `machine.go` 会被两边各改一次注释区——落笔前先 rebase 到对方之后。

## 涉及层

- [x] 逻辑层：`internal/restmode/machine.go`
  - `warningCueFor(kind string)` → 接剩余时间：按档位 `>30s` / `>10s 且 ≤30s` / `≤10s（含 0）` 选措辞。
  - 12 句（3 档 × stand/move/rest/兜底）全部集中在**这一个函数**里，逐句照规格 §3.C 的表；
    第三档一律「马上…」，任何档位都不许再出现与剩余时间矛盾的「1 分钟后」。
  - `Snapshot()` 内**先算 `RemainingSec` 再算 `CueText`**（现顺序 `:293-301` 是反的：
    先赋文案后算剩余，措辞一旦依赖剩余就会读到零值——这是本票最容易写错的一行）。
  - `TotalSec` 仍为 `WarningLeadTime`（60），`State`/`Kind`/`Title` 等字段语义不变。
  - 倒计时（`countdown`）阶段走 `CueFor(kind).Text`，本票**不动**（WS-17）。
- [x] 逻辑层：`app.go` 的 `trayStatusText`
  - 删掉 `:1228` 写死的 `"1 分钟后休息"`，改成复用同一快照里已经算好的 `snap.CueText`
    （该函数 `:1223` 已经调过一次 `Snapshot()`，不产生额外开销）。
  - 不新增每秒变化的文案：档位边界只有两处（60→30、30→10），
    `refreshTrayIfChanged`（`:1181-1200`）的"文本没变就不重建"因此仍成立，一次预警最多重建 NSMenu 2 次。
  - 同步 `:1213-1220` 的注释（不再存在独立的菜单栏预警文案）。
- [x] UI 层：**不改逻辑**，措辞继续由服务端算好经 `Snapshot.CueText` 下发，前端只渲染（铁律 1）。
  - `Pet.vue:1264-1266` 直接渲染 `cue_text`，无需改动；确认它在 `state==='warning'` 时取的是新文案。
  - `frontend/src/lib/restmode.ts` 头部对 `warning` 状态的注释现在写着「Pet.vue 显示『1 分钟后...』气泡」，
    改成「按剩余分三档，文案由 Go 侧 `Snapshot.cue_text` 给出」——注释漂移就是下一轮两处文案的起点。
  - **禁止**在 TS 里做任何档位判断或秒数拼接（`restmode.ts` 只镜像常量与类型）。
- [x] 测试
  - WS-T9 `NewMachineWithClock` + `EnterWarning(PlannedFireAt = base+45s)`，把时钟推到
    base / base+20 / base+40 / base+45（剩余 45 / 25 / 5 / 0 秒）→ `CueText` 依次为
    「1 分钟后…」「半分钟后…」「马上…」「马上…」；同时断言 `RemainingSec` 与档位不矛盾
    （即 `RemainingSec ≤ 10` 时 `CueText` 不得含「1 分钟后」）。
  - WS-T10 同样四档 × kind = move / rest / 未知 kind 兜底，共 9 组，逐字对齐 §3.C 表。
  - WS-T11 app 层：用 `&App{restMode: machine}` 构造（同 `app_presentation_test.go:190-200`），
    在 warning 态断言 `trayStatusText() == restMode.Snapshot().CueText`。
    这条是本票的"单一出处"证据——如果菜单栏另写了一份，它一定会红。
  - 既有快照用例回归：`warning_test.go` 里 `TestSnapshotIncludesWarningState` 断言的是
    "CueText 非空"，档位化后它仍应绿；不要顺手改成精确字符串（把措辞锁在测试里等于造出第三份出处）。
    同理 `machine_test.go` 里对 Warning 状态 `Remaining` 的用例不许改断言粒度。
- [x] 文档：`machine.go:29-31`（`StateWarning` 注释）与 `:44-48`（`WarningLeadTime` 的理由）
  补一句"措辞按剩余分档，档位边界与本常量的关系"。

## 验收标准

- `make test` / `make lint` 全绿，含 WS-T9 / T10 / T11。
- `grep -rn "分钟后" --include=*.go --include=*.ts --include=*.vue .` 的命中**只允许**在
  `internal/restmode/machine.go` 与本目录文档/注释里；出现第三处即为本票未完成
  （004 会把这条判据编译成会红的护栏）。
- 肉眼看一次：预警期间气泡从「1 分钟后…」在 30 秒与 10 秒边界各变一次，
  菜单栏同步（走查动作集中在 005，本票可用单测 + 一次临时 HOME 起界面确认）。

## 边界

- 不改 `WarningLeadTime` 的 60 秒提前量。
- 不动倒计时阶段的引导动作内容与时长（`cue.go`）。
- 不做逐秒倒计时文案、不加密宠物窗口的 4 秒轮询。
- 不改 `RestModeState` 枚举与前后端镜像。

## 实现记录（2026-10-22）

- 逻辑：`internal/restmode/machine.go` 里写死的四句换成措辞表
  `warningCues`（stand/move/rest 各三句）+ `warningCuesFallback`（兜底三句）共 12 句，
  配 `warningCueTier(remaining)` 两个下界常量 `warningCueHalf = 30s` / `warningCueSoon = 10s`；
  边界归给更短的一档（剩 30 整秒说"半分钟后"、剩 10 整秒和 0 秒说"马上"），保证话说得单调。
- `Snapshot()` 里**先算 `remaining` 再算 `CueText`**：原顺序是先赋文案后算剩余，
  措辞一旦依赖剩余就会静默读到零值——这是本票最容易写错的一行，已按正确顺序改并在代码上方注明原因。
- 菜单栏：`app.go` 的 `trayStatusText` 删掉写死的「1 分钟后休息」，改为取同一份快照里已算好的
  `snap.CueText`；`refreshTrayIfChanged` 的"文本没变就不重建"仍成立，
  一次预警最多在 60→30、30→10 两个边界各重建一次 NSMenu。
- 前端零逻辑改动：`Pet.vue` 继续渲染 `cue_text`；`lib/restmode.ts` 与 `Pet.vue` 里
  描述旧文案的两处注释改为"Go 侧按剩余分三档算好放进 `cue_text`"。
- 测试：`internal/restmode/warning_test.go` 新增 `TestWarningCueTiersFollowRemaining`
  （15 组：stand 六档边界 + move/rest/兜底各三档，期望值取自规格 §3.C 的表），
  并在每组里加一条"剩余 ≤10 秒不得再出现「1 分钟后」"的横向不变量；
  `app_tray_test.go` 新增 `TestTrayWarningTextIsSnapshotCue`——断言**相等**而不是断言字面量，
  这样措辞以后再改也不用动测试，同时它就是"单一出处"的证据。
- 注入证据：把 `app.go` 恢复成写死的「1 分钟后休息」后，`TestTrayWarningTextIsSnapshotCue`
  四个子用例全部变红（`菜单栏="1 分钟后休息" 快照="马上该起身拉伸了"`），改回即绿。
- 验证：`make test` / `make lint` 全绿。肉眼看三档随剩余变化的走查转 005。

