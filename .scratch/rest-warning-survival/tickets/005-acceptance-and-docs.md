# 005 — 实机两条事故场景走查 + 文档与注释收口

**Blocked by**: 001、002、003、004

**状态**: 部分完成（实机六阶段走查：5 PASS + 1 SKIP；文档已收口）

> 规格：`docs/specs/2026-10-22-rest-warning-survival.md` 的 WS-19、WS-20、§5.3 完成定义。
> 本票是审计与验收，**不夹带新功能**；发现缺陷回到对应票修，不在此顺手改。

## 目标

在真实 GUI 里复现 2026-10-22 的两条事故路径，看到预警按时消失；
同时把散在注释/前端说明里的旧语义（"写死一句 1 分钟后"、"空窗先留着"）收干净，
让下一个人读代码读到的是新判据。

## 走查环境（沿用本仓既有办法）

- 构建：`WAILS_MCP=1 wails3 build -tags private_mac_apis`，产物起在临时 HOME 下的 `/tmp/**.dev.app`。
- **单实例坑（必须先处理）**：`main.go` 的 `SingleInstance.UniqueID` 不含 HOME，
  临时产物会把手交给已安装的 `/Applications/Morrow.app` 并 `exit 0`——
  要么先停用户实例，要么临时改 UniqueID 重编译（验完必须还原，且不许进 commit）。
- 窗口被遮挡时 `document.hidden=true`，量出来的尺寸与可见性全是假的：
  先 `window_control show/focus` 再判读。
- 造数据要 `DELETE` + 清 `sqlite_sequence` 再插，否则外键对不上会静默算成 0。
- 收尾：清理临时环境，并重新打开用户的 `/Applications/Morrow.app`。

## 涉及层

- [ ] 走查场景 A：暂停压在预警窗口内（今天的事故）
  - 造法：临时 HOME 的 DB 里把「起身活动」的 `interval_min` 调成 1（只改这份副本，别碰用户真实库）。
  - 进入暂停用**演示模式**（托盘菜单「进入演示模式」，`reason=presentation`）——
    比真锁屏便宜，且与会议/全屏/空闲走的是同一条 `paused` 分支；
    若时间允许再补一次真实锁屏，两者都要看到同一结果。
  - 期望：预警出现 → 进入暂停后气泡**在 ≤4 秒内**换成「演示模式中，我先隐身」/「屏幕已锁定，我先不打扰」（WS-9、WS-12）；
    日志里能看到撤销原因走的是 paused 而不是落定（002 的那条断言在真机上的对应物）。
  - 反证要求：如果这里看到气泡仍挂「1 分钟后…」，本票不许通过，回到 002 修。
- [ ] 走查场景 B：持续打字导致安静投递
  - 造法：同样 `interval_min=1`；在到点前开始持续输入（MCP 键盘事件或真实敲键），
    把 fire 推到「顺延到上限 → 转安静投递」。
  - 期望：预警正常亮 ≤60 秒（顺延期间允许超过计划时刻最多一个上限，WS-5）→
    安静投递发生后气泡在 ≤4 秒消失（WS-2）→ 待处理卡片仍在，点「完成」正常结算。
  - 同时确认第三档措辞真的出现过：到点前 10 秒内气泡应为「马上该起身拉伸了」而不是「1 分钟后…」（WS-13）。
- [ ] 走查场景 C（顺手，成本极低）：撤销后能重新预警
  - 场景 B 结束后再等一条 1 分钟间隔的 fire 进入窗口，确认 `warning` 能重新建起来、
    `RemainingSec > 0`（WS-8 的真机版）。今天这条是被僵尸态挡住的。
- [ ] 隐私与噪声确认
  - 预警全程不弹系统通知、不进覆盖层（US-9 的"只告知不打断"没有因本次改动漂移）。
  - 新增的 `tickWarnings` 日志不落应用名/输入内容，`app.log` 体积增长可忽略。
- [ ] 测试
  - `make test` / `make lint` 全绿（含 001/002/003/004 的全部新用例与护栏）。
  - 复核 001/002/004 记录里的"注入错误值变红"证据是否都留在票文档里；缺哪条补哪条。
- [ ] 文档与注释收口（WS-19、WS-20）
  - `internal/restmode/machine.go:29-31`、`:44-48`、`:56-57`；
  - `internal/reminder/engine.go:254-257`、`:275-282`（判据已换成落定事实）；
  - `app.go:1213-1220`（菜单栏不再有独立预警文案）；
  - `frontend/src/lib/restmode.ts` 头部 `warning` 状态说明；
  - `CONTEXT.md`「休息预警」「预警措辞」与 `docs/adr/0012-rest-warning-survival.md` 的文件路径/术语引用逐条校对；
  - `AGENTS.md` 铁律 1 的唯一真相清单补「预警措辞」条目、`docs/map.md` 补 004 的新护栏；
  - 检查全仓再无「Pet.vue 显示『1 分钟后…』」这类旧描述残留（grep 一遍，别靠记忆）。

## 验收标准

- 三个场景各有截图或 DOM/绑定断言存进本票「实现记录」，写清实测时刻与剩余秒数；
  **测试全绿不算这条的证据**（本 bug 正是绿测试盖不住的那一类）。
- 现网那条僵尸预警：重启临时实例后确认不再出现；用户实例的僵尸态随重启清除，
  在记录里注明"该现象只存在于旧二进制"。
- 文档四处（CONTEXT / ADR / AGENTS / map）与本组票的实现对齐，引用路径全部存在。
- 完成后把本组票的交付状态（分支、commit、是否 push）追加到项目记忆，
  并更新 `.scratch/rest-warning-survival/tickets/index.md` 的状态列。

## 边界

- 不修 Windows 实机、不跑 60/90 分钟长跑（这些仍在 `sit-awareness-and-sassy-pet` 的 012 上挂着）。
- 不改安静投递的投递形态、不改顺延上限、不做逐秒文案（规格 §6 明确不做）。
- 不 push、不合并分支，除非用户明确指示。

## 实现记录（2026-10-23）

**环境**：用户授权后 `osascript quit` 停掉 `/Applications/Morrow.app`；
把 `wails3 build -tags production,private_mac_apis,mcp` 产出的新二进制（`build/bin/Morrow`，
构建日志确认标签）塞进 `/tmp/morrow-gui-app/Morrow.app` 的旧 bundle 壳并 ad-hoc 重签；
`HOME=/tmp/morrow-gui-home` 启动，MCP 在 `http://127.0.0.1:9099/mcp`（日志里读到 url 才确认端口）。
**用户真实 DB 与 config 全程未动**，所有种子数据写在临时 HOME 的副本里。
收尾：验收实例 kill 掉 → 再 `open -a /Applications/Morrow.app`（单实例锁按 app 名共享，顺序反了就会把手交给旧实例并 exit 0）。

| 阶段 | 结果 | 实测值 |
| --- | --- | --- |
| 1 三档措辞 | PASS | 剩余 27s → `半分钟后该起身拉伸了`；8s → `马上该起身拉伸了`；宠物 DOM 类名 `bubble bubble--warning` 且 innerText 与快照逐字一致；第一档在阶段 3 抓到（remaining=50 → `1 分钟后该起身拉伸了`） |
| 2 暂停（演示模式） | PASS，**但没验到引擎分支** | 预警 remaining=30 → `SetPresentationMode(true)` → 25 秒后 `state=idle`。日志里**没有**「休息预警撤销：进入智能免打扰」那一行——进入演示模式在 app 层就有 `handleRestModeOnPresentationEnter` 先撤销了，所以这条走的是 app 钩子而不是 `Tick` 的 paused 分支 |
| 3 停用 | PASS | 预警 remaining=50 → `reminders.enabled=0` → 25 秒后 `idle`，引擎日志 `休息预警撤销：预告的触发已落定或不再存在 reminder_id=1 planned_fire_at=1792720680` |
| 4 正常循环后能力还在 | PASS | 到点 → `countdown`（引导动作文案，remaining=119）→ `SkipRestMode` → `idle` → 下一条预警 `remaining=29`。僵尸态没回来 |
| 5 安静投递（今天 8 次里 7 次走的这条） | PASS（前三次是我判错） | 见下 |

阶段 5 连续判错三次，都记下来，因为它们是这类实机验收最容易糊弄过去的地方：
1. 间隔设 1 分钟 → 安静投递刚落定，下一条 fire 已进 60 秒窗口，「该撤销」被「新的该亮」顶替；
2. 第二次末态读到 `countdown` —— 那是预警**正确推进**成倒计时，不是卡死；
3. 第三次用 `f.tell()` 取日志偏移，而刚打开未读取时 `tell()` 恒为 0，
   于是抓到上一轮的陈旧「转安静投递」行，判据被污染。

第四次把判据改成"落定后必须 idle，或指向**更晚**触发点（`remaining_sec > 0`）"——
卡死形态永远是 `warning + remaining=0`，而新预警能建起来本身就是旧的被撤销过的证据
（`EnterWarning` 只接受 idle）。实测：日志连续三条「保留：仍待投递」（同一个
`planned_fire_at=1792721520`，正是顺延窗口）→ 10:13:10 一条「撤销：已落定或不再存在」→
落定后 +20s `remaining=28`、+40s `remaining=8`，文案随剩余降到第二、第三档。判定 PASS。

**未闭环的一处（如实记录，不算通过）**：阶段 6/7 想用空闲暂停（`pause_idle_minutes=1`）
在实机上直接验证 `Tick` 的 paused 分支，95 秒完全不注入输入也没进入 `reason=idle` 暂停，判 SKIP，
没有继续追原因。也就是说引擎那条 paused 分支目前**只有单测**（WS-T2 / WS-9）覆盖，
而用户今天的真实事故（17:15 锁屏压在预警窗口内）走的正是这一支。
下一轮如果要在实机上钉死它，需要能可控地把系统空闲时钟推过阈值的手段。

**其他核对**：菜单栏与气泡同源（WS-15）由 `TestTrayWarningTextIsSnapshotCue` 断言相等，
实机没读 NSMenu（MCP 只能看 DOM）；预警全程不弹系统通知、不进覆盖层，临时实例日志里
新增的保留/撤销两行是唯一新增噪声。用户的 `/Applications/Morrow.app` 仍是旧二进制——
重启只清掉内存里的僵尸态，不防复发，要真正修好得安装带本次修复的构建。

