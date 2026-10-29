# design — 小满跟着你在做什么

## 1. 为什么不抄 BongoCat 的输入管道

| 维度 | BongoCat | Morrow 若要照搬 | 结论 |
|---|---|---|---|
| 采集 | `CGEventTap`（错误码可见 `tap_create_failed` / `permission_denied`） | 需 macOS「输入监控」授权、Windows 注册隐藏窗口收 Raw Input | ✗ 与 `internal/pause` 的零权限路线冲突 |
| 现有基础 | 整个 crate 专为输入而生（`bongocat-input` + gilrs 手柄 + 光标平滑） | 要从零建一条输入管道 + 权限引导 + 降级路径 | ✗ 成本与"健康提醒"这个目的完全不成比例 |
| 素材 | Live2D，模型自带"手按下去"的运动 | 图集 9 行是 codexpet 的外部约定，**没有打字行** | ✗ 要换素材体系 |
| 隐私面 | 读到每一次击键 | 读到每一次击键 | ✗ 收益为"更可爱"，代价是最敏感的一类数据 |

关键判断：**"知道你在做什么"不需要"知道你按了什么键"。**
前者由 `NSWorkspace.frontmostApplication`（macOS）/ `GetForegroundWindow` + `QueryFullProcessImageName`
（Windows）就能得到，两者都**不需要任何授权**；后者才是需要弹窗的那一类。
Morrow 已经站在便宜的那一侧了。

而且 `Snapshot` 有一条既有红线（`internal/sit/contract.go` 注释）：

> 这里不允许出现应用名、应用标识或窗口标题。

镜像只能消费**类别**，这与红线天然一致——本变更不改这条红线，只把类别从"气泡文案的输入"
扩展为"体态的输入"。

## 2. 镜像要表达什么：三类姿态

六类行为不是六种姿态。类别只是原料，姿态要回答的是"它此刻跟你在同一个节奏里吗"：

| 姿态 | 触发类别 | 语义 | sprite（图集 9 行） | builtin（程序化 SVG） |
|---|---|---|---|---|
| `working` | coding / design | 一起埋头干活 | `review`（行 7，此前从未触达） | `pet--mirror-working`：身体前倾、手部微颤 |
| `listening` | communication | 你说的我听着 | **保持 idle**（无对应行，见第 6 节） | `pet--mirror-listening`：托腮、头微侧 |
| `watching` | browsing / media | 一起看 | 保持 idle | `pet--mirror-watching`：头微仰、眼稍睁 |
| 空 | other / 无活动 | 不镜像 | idle | 不加 class |

**为什么 coding 与 design 合并**：两者的身体语言都是"手在动、头低着"，
分开只会多一个必须单独验证的分支，而用户在宠物身上分辨不出"我在 Figma 还是 VS Code"。
**为什么 browsing 与 media 合并**：都是"眼睛在追屏幕"，差别只在宠物该不该睡着——
而"睡着"已经由 `pet-autonomy` 的打盹管，不该在镜像里再做一次判定（铁律 1）。

## 3. 活动档位：由空闲时长直接算，不进 Tracker

需要区分的只有一件事：**你是真的在干活，还是只是没关窗口**。
`pause.GetIdleDuration()` 已经回答了它——它是一个由任意输入**重置**的单调时钟，
所以 `idle < 3 分钟` 等价于"最近 3 分钟里有过输入"，不需要任何滑动窗口状态。

| 档位 | 判据 | 含义 |
|---|---|---|
| `busy` | `idle < 20s` | 正在连续输入 |
| `recent` | `20s ≤ idle < 3min` | 刚动过，可能在看/在想 |
| `away` | `≥ 3min` | 不在键盘前 |

取值依据：

- **20s**：tick 间隔 5 秒，`busy` 意味着最近 4 个采样点里至少命中一次输入；
  再抬高会把"停下来想 30 秒"误判成离开。
- **3min**：比 `sit.FocusSwitchGrace`（2 分钟，切走宽限）略宽——
  看代码两分钟不动手是常态，`recent` 必须能覆盖它，否则 `working` 会持续闪烁。

**为什么不放进 `sit.Tracker`**：Tracker 里没有"当前类别"这个长期状态（`focusCategory`
只在专注会话里存在，且要满 10 分钟才产出 `Focus`）。为一个纯函数另开一套状态机，
等于给同一件事造第二个真相。故它是一个无状态函数：

```go
// internal/sit/activity.go
func ActivityFor(idle time.Duration) ActivityLevel
func NewActivity(category Category, idle time.Duration) *Activity // category 为空 → nil
```

`away` 时 `pose` 直接为空——人不在，宠物该进入待机/打盹（`pet-autonomy` 已有），
而不是维持一个"假装在陪"的姿态。

## 4. 镜像是 idle 的**替身**，不是新状态（本轮最关键的一条）

第一版想法是让 `tickMirror()` 调 `setAnim(PetAnim.Review)`。这会在实现时立刻碎掉：

`applyPending()` 是状态机的唯一收口，**待处理列表为空时它会无条件 `setAnim(Idle)`**
（`Pet.vue:775`）。而 `refreshPending()` 在事件到达、临时动作结束时都会跑——
于是镜像设好的行会被下一次 `applyPending` 打回 idle，且没有任何报错。

故采用替身模型：

```
绘制行 = PET_ANIM_ROW[ anim === Idle ? mirrorRow : anim ]
```

- `mirrorRow` 与 `mirrorPose` 是独立 ref，**永不写 `anim`**；
- `anim` 的语义、`holdTimer`、气泡优先级链、`canPlayMicro` 的判据全部不变；
- 待处理提醒、庆祝、挥手、惊醒、打盹一律天然优先——因为它们改的是 `anim` 或压制门控，
  而镜像只在 `anim === Idle` 时才被看见。

收益不止于"不打架"：镜像因此**不能**影响任何一次提醒的展示与 Ack 预算。
一个纯视觉的东西，永远不会有能力推迟一条健康提醒。

## 5. 防抖：确认窗口 + 最小停留 + 立即清除

`docs/pet-competitive-analysis.md` 的 G8：状态切换现在只有 `holdTimer`，
没有"最少显示 N 毫秒"，优先级链切换可能闪。镜像恰好是最容易闪的一种——
用户在两块屏之间来回切窗口，类别每 4 秒就可能变一次。

| 规则 | 取值 | 为什么 |
|---|---|---|
| 确认窗口 | 同一类别连续 **3 次轮询**（≈12s） | 一次切到 Slack 回句话不该让它换姿态 |
| 最小停留 | 换过之后 **20s** 内不再换 | 交替两个窗口时不会来回抽搐 |
| 清除 | **立即**，不受上面两条约束 | 压制条件（暂停/收起/打盹/无活动）是"它不该演"，不是"换个姿态演" |

即：**进入姿态要慢，退出姿态要快。** 这条不对称是本节的要点——
把两者都做成 20s，会让"我暂停了它还保持埋头 20 秒"变成一个小 bug。

调度全部挂在既有 4 秒轮询（`pollSettings()`）里，**不新增 `setInterval`**；
调用顺序放在 `tickDoze()` 之后、`tickMicro()` 之前，让微动作看到镜像已经决定的底态。

## 6. 素材缺口：`listening` 没有行，且"会议"根本不会走到这里

图集 9 行里没有任何一行能表达"听"。可选的两条都是错的：

- 借 `waiting`（行 2）：它在本应用的语义是"等你处理提醒"，与气泡优先级链绑定。
  借来演"听"会让"有待处理"与"在听你说话"长得一样 → 用户分不清要不要点它。
- 借 `running-right/left`（行 4/5）：那是走动，与"听"没有语义关系。

故 `listening` 在 **sprite 模式下降级为 idle**，只在 builtin 模式下有姿态。
这不是遗漏，是显式的降级：**没有素材的事不做假动作**。

还有一个反直觉的事实：**开会时镜像根本不会触发**。`internal/pause` 的会议检测会把状态置为
`Reason=meeting`（宠物进入"我先不打扰"），前端 `canApplyMirror()` 的 `paused` 判据因此为真。

【机制别记错】拦住它的**不是**采样：`sampleAppUsage()` 的排除条件只有
「画像关闭 / 平台不支持 / 场景失效 / 锁屏 / 长空闲（`ResetIdle`）」，**会议与演示模式不在其中**
——所以开会时 `activity` 仍然有值（通常是 `communication`），是前端门控把它挡下的。
写实现时若把"没有类别"当成真相，会以为在 Go 侧也可以顺手少判一层。

所以 `listening` 的真实场景是**用微信/飞书/钉钉打字聊天**，不是开会——这恰好是对的：
开会时它该安静，不该演托腮。

## 7. 已知限制（归档时应保留，不要当 bug 修）

1. **镜像只在"画像开启 + 久坐感知开启 + 场景生效 + 处于站/走提醒的时间窗内"时存在。**
   因为前台采样本身就挂在这组条件上（`sit_runtime.go` 的 `active` 来自 `activeContext()`，
   即"有启用的站/走提醒且当前在窗口内"）。没有安排工作计划的人，宠物不会镜像。
   要放宽就得把采样与时段的耦合拆开——那是另一件事（且会让"应用画像"在非工作时段
   也持续采集，需要重新评估隐私口径），本变更不碰。
2. **`activity` 会在 Morrow 自己是前台应用时消失**（`sit.IsSelfApp` 排除），
   于是用户盯着宠物看的时候，它反而不演了。这是既有采样的既定行为，接受。
3. **sprite 模式只有 `working` 有身体语言**，其余两类回 idle。
4. **镜像的开关就是画像的开关**：关掉画像 = 关掉镜像。理由见第 8 节。

## 8. 隐私：为什么不做独立开关

镜像消费的是"类别"，而类别已经在"应用画像"这个显式同意之下（`setProfileEnabled`
是唯一入口，写 `asked` 并可选删除历史）。再加一个 `PetMirror bool` 会带来：

- 一个新的配置项 + 一个设置页分支 + 一条 `config` 测试 + 一次绑定产物重生成；
- 一个更糟的心智模型：用户会问"画像开着但镜像关着，它到底在不在看我"。

故本变更**不新增开关**，改为在设置页画像说明里补一句：
「关掉后，宠物不再跟随你的活动改变姿态」。一句话换掉一个配置面，值得。
（若真出现"觉得宠物在学我、很烦"的反馈，正确做法是走 `今天先这样` 的既有静默通道，
而不是再加一个开关。）

另外两条既有边界继续有效，本变更不触碰：`Activity` 只含类别与档位；
`docs/adr/0004-smart-pause-local-signals-only.md` 的"不看屏幕内容"仍然成立
（前台应用标识不是屏幕内容）。

## 9. 风险

| 风险 | 处置 |
|---|---|
| 切窗口时姿态抽搐 | 进入要 12s 确认 + 20s 最小停留；退出立即（第 5 节） |
| 镜像抢走提醒的注意力 | 镜像永不写 `anim`，待处理/庆祝/挥手/惊醒/打盹天然优先；不产生气泡、不发声 |
| 与微动作互相覆盖（两者都动 `.pet__body-group`） | **不互斥，改"选择池过滤"**：CSS animation 会整段覆盖静态 transform，故只把抢同一元素的动作剔掉（`stretch` 抢身体层；`watching` 时 `look` 抢眼睛层）。详见 11.5 |
| `Snapshot` 加字段后前端类型报错 | 跑 `wails3 generate bindings -clean=true -ts -i`；这是"前端类型来自产物"的正常代价 |
| `snapshot()` 里新增一次 `GetIdleDuration()` 变成热路径 | 它是一次 `CGEventSourceSecondsSinceLastEventType`，与 `tick()` 同源；4 秒一次的调用可忽略 |
| reduced-motion 用户看到姿态 class 里的动画 | `pet--mirror-*` 的**动画部分**在既有媒体查询里停掉，静态倾斜保留（"姿态"不是"动态效果"） |
| 借用 `review` 行的观感未经确认：它可能读起来像"在翻文件"而不是"在敲键盘" | 这是本变更唯一靠肉眼裁决的一点。若真机目视后观感不对，**退回 idle 并把 `working` 降级为 builtin-only**（改 `PET_MIRROR_ROW` 一处即可，映射表与门控不动）；不要为了凑动作去动图集 |

## 10. 与铁律的对应

- **单一真相**：类别映射只在 `sit.CategoryFor`；活动档位只在 `sit.ActivityFor`；
  姿态映射表只在 `lib/pet.ts`；时间常量（确认/停留）也只在 `lib/pet.ts`。
  前端**不重算档位**，只做"类别 + 档位 → 姿态名"的查表。
- **禁硬编码色值**：三个姿态只改 `transform` 与 `opacity`，不引入任何新颜色。
- **事件契约不变**：不新增 `pet:*`，`parity_test.go` 只需为 `ActivityLevel` 增加一项镜像。
- **护栏要"感知自己瞎了"**：
  1. `PET_MIRROR_POSE_BY_CATEGORY` 必须**恰好**覆盖 `sit.AllCategories()` 的六项
     （缺项/多项都报错），解析到 0 条即 Fatal；
  2. `lib/pet.ts` 里 `PET_MIRROR_*` 的每个常量必须在 `Pet.vue` 的**实际代码**里被引用
     （剥掉 import 块与注释——沿用 pet-autonomy 踩过的坑）；
  3. `canApplyMirror()` 必须命中全部压制标识（沿用 `petGateSuppressors` 那张表）。
- **分层纪律**：`app.go` 不改；活动判定在 `sit`，采样在 `sit_runtime.go`，不在前端。
- **验证入口统一**：只记 `make test` / `make lint` / `make e2e-pet`。

## 11. 实施补充（落地时才发现，归档时不要丢）

### 11.1 `PET_MIRROR_ROW` 必须声明在 `PetAnim` 之后

`PET_MIRROR_ROW` 是模块求值时求值的对象字面量，里面写的是 `PetAnim.Review`。
第一版把它放在 `PetAnim` 声明之前，TS 类型检查**完全通过**（类型是声明合并的），
但运行时抛 `Cannot access 'PetAnim' before initialization`（TDZ），宠物页直接白屏。

这是本变更为数不多"静态检查看不见"的坑，已写进常量注释。

### 11.2 镜像门控刻意**不**包含 `prefersReducedMotion`

减弱动态约束的是"动"，不是"样子"。镜像的姿态是静态倾斜，动画部分（手臂摆动）
由 CSS 媒体查询停掉；若把 `prefersReducedMotion` 加进入口门控，减弱动态的用户
会永远看不到宠物在跟着他——不报错、不崩溃，只是功能对他不存在。

故 `pet_behavior_test.go` 里有两张门控表：`petGateSuppressors`（微动作，含该条）与
`petMirrorGateSuppressors`（镜像，不含），两者的差异写在注释里。

### 11.3 builtin 三姿态的观感：静帧差异小，靠"动"传达

截图验收（`make e2e-pet` 的 `state-mirror-*.png`）的结论：

- `listening` 最好认（一只手抬到脸边，轮廓变了）；
- `working` 与 `watching` 在**静帧**上几乎一样，区别只在 `working` 的手臂在摆、
  `watching` 的眼睛大 12%。这是刻意的取向（"余光里的一瞥"而不是表演），
  但也意味着**只靠单张截图无法验收 working**——真机手测要看它是否在动。
- `listening` 的抬手位移从 `-8/-12` 收到 `-6/-10`：再大一点手臂根部会离开身体轮廓，
  看着像断了。SVG 里手臂是两段 `line`，没有关节，这是素材本身的上限。

若后续觉得太不明显，正确的调法是**在 CSS 里加幅度**（或给 builtin 加一个新的芽朝向层），
不要改映射表、也不要让镜像去写 `anim`。

### 11.4 sprite 模式的 `working` 仍待真机确认

`review` 行的观感（像不像在敲键盘）无头浏览器里验证不了——`make e2e-pet` 只跑 builtin。
真机手测时若观感不对，按 `PET_MIRROR_ROW` 的注释退回 `PetAnim.Idle` 即可（一处改动）。

### 11.5 首轮上机验收失败后的三处修正（2026-10-27）

首轮装完用户反馈「看不到宠物做我现在的动作」。排查结论：**链路是通的，幅度等于没做**。
证据链：DB 里 `app_usage_sessions` 每轮都在写（说明 `sampleAppUsage` 跑通、类别解析成功）；
产物里 `activity,omitempty` 与 `pet--mirror-*` 都在（说明 Go/前端都是新版本）；
日志里 `宠物窗口已就绪 | {"pack":"builtin","mode":"builtin","scale":0.5}`（说明 builtin 模式）；
11:07–11:15 之间没有任何暂停日志（说明门控是开的）。

于是问题落在"看不见"上，三处修正：

| # | 问题 | 修正 |
|---|---|---|
| 1 | 幅度按 px 拍脑袋，没按**实际渲染尺寸**校准：`pet_scale=0.5` 时宠物只有 96×104，2–3px 位移只剩 1–1.5px | 改成"坐低 10px + 缩到 93% + 侧压 4°"这类**轮廓级**变化；`make e2e-pet` 增加 0.5 倍元素截图（`half-*.png`），并把包围盒量化出来（working 比 idle 矮 11 CSS px、重心低 6 CSS px） |
| 2 | 画板**上方没有余量**（芽尖 y≈7、阴影 y≈194），"抬头/站高"会被裁切 | 三个姿态全部改为"向下 + 缩放 + 倾斜"，不再用向上位移；`watching` 用放大 4% 表达"坐直" |
| 3 | `canPlayMicro()` 里加了"镜像生效就不演微动作"——镜像白天几乎一直生效，等于把 pet-autonomy 整天关掉 | 改成 `pickMicroAction()` 过滤选择池：只剔除抢同一元素的 `stretch`（身体层）与 `watching` 下的 `look`（眼睛层） |

另加一处可观测性：姿态切换写一行 `镜像姿态已切换 {pose, category, at}` 前端日志。
**"看得见但说不清"的东西必须有日志**——否则"没反应"这个报告无法区分「链路没通」
与「幅度太小」，只能靠猜。（这次是靠 DB 与产物反推出来的，成本远高于一行日志。）

### 11.6 幅度校准的正确姿势（下次直接照做）

不要用"像素"直觉，用**包围盒**：`scripts/e2e/pet_states.py` 会额外产出
`half-idle / half-mirror-*` 四张 0.5 倍元素截图，用 ffmpeg 解裸像素即可量出
包围盒宽高、top 与视觉重心。判定标准写在 `docs/pet-character.md`：
**姿态之间至少要有 10% 的轮廓差异**，否则在 96px 下读不出来。

### 11.7 代码审阅后的四项收口（同日）

审阅（对照源码逐条复查）发现一处隐私边界缺陷与若干"只有症状、没有报错"的薄弱点，处理如下：

| # | 问题 | 处置 |
|---|---|---|
| 1 | `snapshot()` 组装 `Activity` 只判 `Enabled`，没判 `ProfileEnabled`；`setProfileEnabled(false)` 也没清 `liveCategory` → 关掉画像后最长 5 秒仍在报告"他刚才在写代码" | 两处都补：门控加 `ProfileEnabled`，关闭时清 `liveCategory`。**撤回同意却还在输出结论，是越界** |
| 2 | 时序状态机（确认窗口/最小停留/立即清除）只存在于 `.vue` 里，本仓没有 JS 测试运行器，加一个要动 `deps.yaml` | **抽成纯函数 `advanceMirror()` 放进 `lib/pet.ts`**，再由 `scripts/e2e/mirror_logic.py` 在真实页面里 `import('/src/lib/pet.ts')` 直接断言 7 条时序。不新增依赖，复用既有 vite dev + Playwright |
| 3 | 点宠物会"弹"一下：挥手期间镜像 class 被摘掉，蜷着的宠物先站起来再蹲回去 | 挥手期间**保留**镜像 class，并给镜像的手臂规则加 `:not(.pet--waving)` 让位。断言 6/7 直接读 `getComputedStyle(arm).animationName`，防止"点了不挥手"这种不报错的问题 |
| 4 | `watching` 辨识度最弱（长宽比只 +0.07） | 加到 `scaleX(1.1) scaleY(0.93)`（长宽比 0.75 → 0.87）。不能再往"整体放大"要幅度——画板顶部只有 5 个单位余量 |

**护栏的连带修改**：`TestPetBehaviorConstantsAreConsumed` 原本只扫 `Pet.vue`，
状态机搬进库之后会把 `PET_MIRROR_CONFIRM_TICKS` / `PET_MIRROR_MIN_HOLD_MS` 误判成没人用。
改为"`Pet.vue` 或该文件的模块代码"两处任一命中即算消费，**但必须剥掉常量定义行**——
不剥的话每个常量都能被自己的名字蹭到，护栏退化成永远为真（已用注入 `PET_MIRROR_FAKE`
做红态验证：它确实报"没有被引用"）。

**为什么非要给这三个时序规则写断言**：它们错了都不会报错，症状只是"切太早/太晚/抽搐"，
靠人眼看几分钟根本定不了位。这轮"看不到镜像"的排查已经证明了：**没有可观测信号的规则，
等于没有规则**——先是靠 DB 与产物反推，再靠一行日志定位，代价远高于写七条断言。
