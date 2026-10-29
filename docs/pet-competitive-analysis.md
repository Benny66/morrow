# 桌宠能力对比与优化方向（2026-10，第三轮竞品分析）

> 范围：只看**桌面宠物本身**（形象 / 行为 / 互动 / 状态承载 / 素材生态），
> 不重复 `docs/competitive-analysis-round2.md` 里的提醒、免打扰、数据复盘结论。
> 方法：GitHub 公开仓库的 README 与 API 元数据（星标/许可证/最近提交）+ Morrow 代码事实；
> 证据强度逐条标注，机制类结论标注「推导」。
> 竞品取样的一个偏向：**只看有真实用户量的开源项目**（≥300 星），避免拿玩具项目当参照。

## 结论先行

1. **业内已经分成三类，「宠物」这个词已经不够用了。**
   装饰型（eSheep、oneko.js）：只为"有个东西在动"；
   养成型（VPet、agentpet）：数值、投喂、等级、长期成长；
   状态型（clawd-on-desk、BongoCat、openpets）：宠物是**某个后台进程的可视状态面**。
   Morrow 的「提醒的化身」属于第三类，**方向是对的**——而且 2026 年这一类正在爆发
   （clawd-on-desk 上线 7 个月 6515 星），说明"宠物承载状态"不是小众审美。

2. **最该抄的不是玩法，而是"无提醒时也有状态"。**
   三类项目有一个共同前提：宠物**一直在**，所以它必须有**自己的时间**。
   小满目前只在三件事发生时变化：提醒到点、点击、待处理清空。其余 99% 的时间，
   它只是在呼吸。业内把它叫 idle micro-behavior（待机微行为），是"像活的"和
   "像一张会呼吸的贴图"之间唯一的差别。

3. **Morrow 在"存在感"这一层已经领先开源圈，不要为了追新功能把它弄坏。**
   透明置顶 + 捕获排除（`ContentProtectionEnabled`）+ 贴边 20px peek + hover 展开
   + 7 级气泡优先级 + 失焦降帧——这套组合在取样的 8 个项目里没有一个同时做到。
   VPet / eSheep 连"录屏时隐身"都没有；clawd 的 mini peek 是同一思路的更简版本。

4. **三个真实缺口**（按体感排序）：
   ① 无自主行为（不提醒时是静止的）；
   ② 成长每天归零（芽 = 今日完成率，次日 00:00 缩回 0.55 档）——这与"养成"的直觉相反；
   ③ 互动只有一个动作（点击 = 挥手），且**没有右键入口**（`Pet.vue` 无 contextmenu，
   设置项只能进主窗）。

5. **有一份素材已经付过费了：图集的 9 行里，有 5 行从未被触发。**
   `lib/pet.ts` 定义了 `running / running-right / running-left / review / failed` 五行，
   但 `Pet.vue` 的状态机从不切到它们（全库只在 `lib/pet.ts` 出现过这几个标识符）。
   也就是说：**走、跑、敲键盘、出错这四类动作，素材有了，行为引擎没有。**

6. **明确不要抄**：饥饿/口渴/体力数值（把陪伴变成打卡负担，且与"温和不强制"冲突）、
   摄像头/眨眼检测（ADR 0004 已否）、AI 语音对话（BYOK + 医疗边界 + 隐私三重风险）、
   多宠物同屏、排行榜/社交（把陪伴变成竞争）。

## 一、业内格局

### 1.1 取样与分类

| 项目 | 星标 | 技术栈 | 类型 | 核心机制（只列可迁移的） |
|---|---:|---|---|---|
| [BongoCat](https://github.com/ayangweb/BongoCat) | 24,218 | Rust + Tauri | 镜像型 | 键鼠/手柄**输入即动作**；眼球与爪随光标；Live2D 自带模型；离线无遥测 |
| [VPet](https://github.com/LorisYounger/VPet) | 7,045 | C# / WPF | 养成型 | 32 动作 × 4 状态 × 3 类型；**摸头 / 提起 / 爬墙**；工作时钟；创意工坊 + 代码插件 |
| [clawd-on-desk](https://github.com/rullerzhou-afk/clawd-on-desk) | 6,515 | Electron / JS | 状态型 | 12 个状态由 **agent hook + 日志**驱动；**最小显示时长 + pendingState 防抖**；睡眠序列 yawn→doze→collapse→sleep；mini 贴边探头 |
| [Mate-Engine](https://github.com/shinyflvre/Mate-Engine) | 3,811 | Unity / VRM | 养成型 | 3D VRM + 物理（可抓可扔）；Steam 分发 |
| [oneko.js](https://github.com/adryd325/oneko.js) | 1,337 | 单文件 JS | 装饰型 | 光标追逐的极简参照实现（27 行核心逻辑） |
| [openpets](https://github.com/OpenPetsHQ/openpets) | 1,332 | TS / Electron | 状态型 | 本地优先 + **插件 SDK**；"task-aware pets"（宠物感知任务） |
| [desktopPet / eSheep](https://github.com/Adrianotiger/desktopPet) | 1,164 | C# | 装饰型 | XML 动画表；**落到任务栏 / 沿窗口边缘走 / 被扔出去**；多宠物互相碰撞、成群睡觉 |
| [agentpet](https://github.com/ntd4996/agentpet) | 391 | Swift / SwiftUI | 养成型 | 监控 AI 编码 agent；**长期成长 + 等级 + 投喂**（不是每日归零） |

> 星标为 2026-10-26 抓取；许可证：BongoCat/VPet Apache-2.0、clawd AGPL-3.0、
> Mate-Engine 自定义、openpets/oneko/eSheep/agentpet 均为 MIT/Apache 类宽松许可。

### 1.2 三个值得记住的判断

**（a）星标最高的是"镜像型"，不是"养成型"。**
BongoCat 两万四千星，靠的不是玩法深度，而是**输入与动作的零延迟对应**——
你敲键盘它就敲，你动鼠标它眼睛就跟着。这提示一件事：让人愿意长期留着一个桌宠的，
不是数值系统，而是**"它在回应我"**。养成是留存手段，回应才是留存原因。

**（b）2026 年新增的量都在"状态型"。**
clawd-on-desk（2026-03 创建）、openpets（2026-05）、agentpet（2026-05）——
全部是"宠物显示某个后台进程正在干什么"。它们的共同设计不是画得更可爱，
而是**状态映射表 + 优先级仲裁 + 防抖**。这三件事 Morrow 已经在气泡上做了一份
（7 级优先级），但只做在**文字**上，没做在**身体**上。

**（c）"长时间无操作"是所有项目都要单独处理的一种状态。**
clawd 用四段睡眠序列（打哈欠 → 打盹 → 塌下 → 睡着），eSheep 是"成群睡觉"，
VPet 有生病/疲劳状态。小满目前对"你离开了"的回应是**一句话**：
`'你好像离开了，我先不打扰'`（`Pet.vue` 的 pauseBubbleText）。这是一条信息，
不是一个状态。

## 二、五层模型逐层对照

把桌宠拆成五层，逐层看差距。这套分层是本文的分析框架，不是业内的标准。

### 2.1 存在感（窗口与系统集成）

| 能力 | 业内 | Morrow | 判断 |
|---|---|---|---|
| 透明 / 置顶 / 无边框 | 普遍有 | 有（`pet_window.go`，含 macOS Backdrop + private_mac_apis） | 平手偏强：注释里连"少一层底色就是方块"都写清了 |
| 捕获排除（录屏隐身） | 取样项目的 README **均未提及**（非实测，可能只是没写） | 有（`ContentProtectionEnabled: true`） | 领先（依据为文档缺口，非实测） |
| 贴边收起到窄边 | clawd 的 mini 模式（贴右边探头） | 四边贴边 + 20px peek + hover 展开 | **领先**（四边 vs 一边） |
| 多屏 | eSheep 有；多数项目含糊 | 有（WorkArea 夹取 + 全局坐标还原） | 领先 |
| 省电 | clawd 把光标轮询从 20Hz 降到只在 idle 跑 | 12fps / 失焦 8fps / 隐藏时 rAF 自动停 | 平手 |
| 系统入口 | 托盘/菜单栏普遍有 | 有（v3 SystemTray） | 平手 |

> 这一层不需要优化，需要的是**守**。任何新行为都必须复用现有窗口与 `petMu`，
> 不要为"漫游"另开一条移动通道——`pet_dock.go` 里"程序化移动不算拖动"的
> 那两道防线（`petBubbleExpanded` / `petDockIgnoreUntil`）就是被这类需求咬出来的。

### 2.2 行为（没有输入时的自主性）

| 能力 | 业内 | Morrow | 差距 |
|---|---|---|---|
| 待机微动作 | VPet 32 组动作；BongoCat 呼吸+眨眼 | 呼吸 + 眨眼 + 摇芽（builtin）；sprite 只有 idle 循环 | **大** |
| 自主漫游 | eSheep 沿窗口边缘走、掉到任务栏；VPet 爬墙 | 无 | **大**（但素材已有 running-right/left） |
| 打盹 / 睡眠 | clawd 四段式；eSheep 成群睡 | 无（只有一句暂停气泡） | **大** |
| 被拖动时的姿态 | VPet「提起」；Mate-Engine 物理抓取 | 无（窗口被拖走，宠物姿态不变） | 中 |
| 朝向 / 眼神 | BongoCat 爪眼随光标；clawd idle 眼球跟随 | 无 | 中（成本极低） |
| 昼夜节律 | 多数没有（这是空位） | 无 | 中（差异化机会） |

**这一层是主战场。** 而且它有一个很好的性质：**大部分动作不需要新素材**。
builtin 形象是程序化 SVG，加一个"张望"只是两只眼睛的 `transform`；
加一个"打盹"只是把 `.pet__eyes-open` 的 `scaleY` 压到 0.1 + 芽收拢。

### 2.3 互动（用户能对它做什么）

| 互动 | 业内 | Morrow | 差距 |
|---|---|---|---|
| 点击反馈 | 普遍有 | 挥手 900ms（且只在零位移点击时可靠，见 `PET_CLICK_MAX_TRAVEL_PX` 注释） | 平手（实现有天花板） |
| 摸头 / 长按 | VPet 招牌动作；clawd 双击"戳戳"、四连击"东张西望" | 无 | **大** |
| 右键菜单 | 普遍有（设置/换形象/隐藏/退出） | **无**（`Pet.vue` 无 contextmenu） | **大**（宠物是常驻入口，却没有入口） |
| 拖拽 | 普遍有 | 有（wails 原生 + JS 兜底开关） | 平手 |
| 投喂 | VPet 核心玩法 | 无（**建议不做**，见第六节） | 有意留白 |

**右键菜单是这一层性价比最高的一项。** 一个每秒都在屏幕上的东西，用户对它
最自然的第一个动作就是右键；现在右键没有任何反应。注意：宠物本体的命中区
由 wails 的拖拽判定接管，右键是否被一并吃掉**需要实测**（`drag.ts` 的
`suppressEvent` 挂在 click/contextmenu 上），这决定 D3 是"加一个监听"还是
"先给一个 no-drag 的命中层"。

### 2.4 状态（它此刻在表达什么）

| 能力 | 业内 | Morrow | 差距 |
|---|---|---|---|
| 状态 → 动作的映射 | clawd 12 状态、VPet 32×4×3 | builtin 4 态 + sprite 9 行（只用到 4 行） | 中 |
| 优先级仲裁 | clawd「多会话 + 优先级解析」 | 有，且更细：7 级气泡优先级链 | **领先** |
| 防抖 | clawd「最小显示时长 + pendingState」 | 仅靠 `setAnim` 的 holdTimer | 中（可借鉴） |
| 长期成长 | agentpet 等级制；VPet 存档 | 芽 = **今日**完成率，次日归零 | **大** |
| 情绪 | VPet 疲劳/生病；小满有 3 态情绪（只作用于芽） | 有，但只在待机且不静音时可见 | 中 |

**"每天归零"是这一层最值得改的一件事。** 现在的效果是：昨天长到 1.1 倍、
叶子全展开，今天早上打开电脑，它缩回 0.55 倍——用户看到的是"我一夜之间退步了"。
`pet-character.md` 把"芽随连续打卡生长"列为未实现钩子，理由是"宠物窗口拿不到
打卡数据"；但**这个理由已经过时**：`Pet.vue` 每 4 秒轮询 `GetPetGrowth()`，
通道是通的，缺的只是 `PetGrowth` 结构体里没有 streak 字段。

### 2.5 内容与生态

| 能力 | 业内 | Morrow | 判断 |
|---|---|---|---|
| 素材格式 | VPet 工坊；eSheep XML；BongoCat Live2D | codexpet 图集（8×9）+ `pet.json` + 导入 | 平手（借了现成生态，很聪明） |
| 素材来源 | 创意工坊 / 模型站 | 复用 `~/.codex/pets`（只读）+ 本地导入 | 够用，无分发渠道 |
| 素材元信息 | 作者 / 预览 / 许可 | 只有 id / name / description | 缺 |
| 插件 / 扩展 | VPet 代码插件；openpets 插件 SDK | 无（**不建议做**） | 有意留白 |

> 素材生态不必自己造。真正的杠杆是**把 `pet.json` 的字段用满**（作者、预览图、
> 许可），这样将来接任何分发渠道都不用改格式；现在只解析 4 个字段。

## 三、差距清单（附证据）

| # | 缺口 | 现状证据（代码） | 业内对照 | 影响 |
|---|---|---|---|---|
| G1 | 无待机微行为 | `Pet.vue` 只要 `pendingCount === 0` 就 `setAnim(Idle)`，idle 是纯 CSS 呼吸 | VPet/BongoCat 都有 | 高：宠物显得"死" |
| G2 | 无睡眠态 | `pauseBubbleText` 里 `'你好像离开了，我先不打扰'` 只是一句话 | clawd 四段睡眠 | 高：离开/回来没有仪式 |
| G3 | 成长每日归零 | `growth.go`：`growthStage(今日 done, 今日 target)`；`DateKey` 翻篇即归 0 | agentpet 等级制 | 高：养成的反直觉 |
| G4 | 无右键菜单 | 全库无 `contextmenu` 监听 | 普遍有 | 高：常驻入口没有入口 |
| G5 | 5 行动画从未触发 | `PetAnim.Running*/Review/Failed` 仅在 `lib/pet.ts` 定义 | — | 中：素材浪费 |
| G6 | 无朝向/眼神 | 眼睛是两个静态 `ellipse` | BongoCat/clawd | 中：低成本的"活" |
| G7 | 点击判定的固有天花板 | `PET_CLICK_MAX_TRAVEL_PX` 注释：手抖 1px 这一轮既不能拖也不能挥手 | — | 中：交互手感不稳 |
| G8 | 状态切换无最小停留 | `setAnim` 只有 holdTimer，没有"最少显示 N 毫秒" | clawd pendingState | 中：优先级链切换可能闪 |
| G9 | 素材元信息缺失 | `petManifest` 只解析 id/displayName/description/version/spritesheetPath | VPet 工坊卡片 | 低 |

## 四、优化方向候选

评分标准：**体感收益 × 与"提醒的化身"定位的一致性**；成本/风险只在同分时裁决。
与第二轮一样，这是启发式排序，不是公式。

| 候选 | 解决 | 体感收益 | 定位一致性 | 成本/风险 | 建议 |
|---|---|---|---|---|---|
| **D1 待机行为引擎**（微动作 + 自动打盹 + 醒来） | G1 G2 | 5 | 5 | 2 | **P0** |
| **D2 长期成长**（芽改用长期档位，今日只影响神态） | G3 | 4 | 5 | 2 | **P0** |
| **D3 宠物右键菜单**（换形象/缩放/静音今天/打开主窗） | G4 | 4 | 4 | 1 | **P0** |
| **D4 状态机收口 + 最小停留** | G8 | 3 | 5 | 2 | P1（先做，D1 依赖它） |
| **D5 眼神朝向**（窗口内 hover 即可，不读全局光标） | G6 | 3 | 4 | 1 | P1 |
| **D6 未用图集行接进来**（review = AI 生成中；failed = 操作失败） | G5 | 3 | 4 | 2 | P1 |
| **D7 短距漫游**（沿底边左右走一小段） | G5 | 4 | 3 | 4 | P2（风险最高） |
| **D8 声音**（默认静音，仅完成时一声） | — | 2 | 2 | 3 | P2 |
| **D9 素材元信息 + 预览图** | G9 | 2 | 3 | 1 | P2 |
| **D10 摸头 / 长按** | G7 | 3 | 3 | 3 | P2（先解决点击判定） |

## 五、P0 详述

### D1 待机行为引擎

**做什么**：小满在没有提醒、没有暂停、没有气泡的"闲"状态下，每隔 40–90 秒
做一个小动作：张望（眼睛左右移，2 次）、伸懒腰（整体 `scaleY` 拉长再回）、
理芽（一片叶子轻抖）、打哈欠（张嘴 + 闭眼 1.2 秒）。
连续空闲超过 N 分钟（建议 8）进入**打盹**：闭眼、芽收拢、整体轻微起伏；
鼠标进入窗口或用户回来 → **醒来**（先惊一下：芽弹起 + 眼睛睁大，再回 idle）。

**为什么值得做**：这是"活的"和"贴图"的分界。而且成本低到不像话——
builtin 全是 SVG + CSS 关键帧，不需要一张新素材。

**边界（必须写进实现）**：

- **只在 builtin 模式做**。sprite 模式的图集只有 9 行固定动作，硬塞微动作会错行；
  sprite 模式用 `waving`（已有挥手行）作为唯一的"自主动作"即可。
- **门控优先于一切**：暂停 / 演示模式 / 休息倒计时 / 有待处理 / 贴边收起 /
  `prefers-reduced-motion` 时**不演**。这不是礼貌问题：D1 是"常驻循环"，
  按 `pet-character.md` 的既定分层，减弱动态效果时必须停掉它，只留事件反馈。
- **调度器只有一处**：新增的计时必须在**已有的 4 秒轮询循环**里推进，
  不要新增 `setInterval`（现在已经有 `pollTimer` + `collapseTimer` + `holdTimer` 三个，
  再加一个就开始互相打架了）。
- 微动作不改变 `PetAnim` 的语义值，它是**叠加层**（用独立 class），
  否则 `waiting` 与 `idle` 的优先级链会被污染。

**验收**：静置 3 分钟，宠物至少动过 2 次且没有一次在暂停/演示期间发生；
`prefers-reduced-motion` 下完全没有微动作；CPU 占用与改动前无可见差异。

### D2 长期成长

**做什么**：芽的**形态**改由长期指标驱动（建议：最长连续达标天数 `streak`，
单调不减），**今日**完成率只影响神态（叶子的抖动力度 / 高光亮度 / 是否轻微下垂）。

**为什么值得做**：现在 00:00 归零，等于每天早上告诉用户"你退步了"。
而 `pet-character.md` 的定位是"陪伴"，不是"当日 KPI 显示器"。
长期不回退，才会产生"我养着它"的感觉。

**怎么落（一处真相）**：

1. `internal/service/growth.go` 的 `PetGrowth` 增加 `StreakDays int`（或
   `LongTermStage int`），判定逻辑仍**只在这里算**；前端继续只做"整数 → class"。
2. 跨提醒的 streak 已有实现（`checkin.go` 的 `Streak` 按提醒项算），
   需要一层聚合（取最大 / 取当前启用场景内的最大），**这层聚合是新的判定逻辑，
   必须放在 service 且被 `growth_test.go` 的表驱动覆盖**。
3. 前端拆成两个 class：`pet--growth-N`（长期，改形态）+ `pet--today-{steady|tired}`
   （今日，改神态）。**不要**让今日影响 `scale`——那又变回随时会缩。

**风险**：形态变化太慢会"感知不到"。补偿：跨档时播一次"长高"（复用 `jumping`
的 1600ms 保持），并允许记忆气泡说一句"这是第 7 天"。注意该气泡与久坐气泡
共享每天 5 次预算，不要另开配额。

### D3 宠物右键菜单

**做什么**：在 `.pet__art` 上监听 `contextmenu`，弹一个原生（wails v3）或
HTML 小菜单：换形象 / 缩放 / 今天先这样 / 打开主窗 / 隐藏宠物。

**为什么值得做**：宠物是唯一"永远在眼前"的东西，却是唯一没有入口的。
现在想换形象必须：打开主窗 → 设置 → 找到宠物区。

**边界**：

- 菜单必须在 `--wails-draggable: no-drag` 的区域里，否则右键会变成拖拽。
- 菜单的动作全部复用**已有绑定方法**（`SavePetSettings` / `SetPetVisible` /
  `MuteSitToday` / `OpenMainWindow`），不要为菜单造新 API。
- macOS 的菜单栏已经有一份入口（`internal/tray`），两处文案要一致；
  一致性靠共用常量，不靠"记得改两处"。

## 六、明确不做（以及为什么）

| 不做 | 理由 |
|---|---|
| 饥饿 / 口渴 / 体力数值 | 把"陪伴"变成"又一个要维护的待办"。第二轮已定调"温和不强制"，数值惩罚与之直接冲突 |
| 投喂 / 打工 / 商店 | 同上；且需要一整套经济系统，与健康提醒无因果关系 |
| AI 语音对话 | BYOK 门槛 + 医疗建议边界 + 隐私三重风险；`pet-character.md` 已明确"台词来自内置模板，不调用 AI" |
| 摄像头 / 眨眼检测 | ADR 0004 已否，不重开 |
| 多宠物同屏 | 240px 窗口里两只宠物会互相遮挡；且会稀释"这一个在陪我"的关系 |
| 排行榜 / 社交 / 分享 | 把陪伴变成竞争与表演 |
| 插件 SDK | VPet / openpets 有，但它们本身就是平台型产品；Morrow 是单一用途应用，插件收益远小于维护成本 |

## 七、与项目铁律的对接（实现前先对齐）

| 铁律 | 对本次改动的要求 |
|---|---|
| 单一真相 | 生长判定只在 `growth.go`；微动作的时序/权重常量只在 `lib/pet.ts`；菜单文案与菜单栏共用常量 |
| 前端禁硬编码色值 | 微动作与睡眠态不得引入新颜色，只能用 `--color-primary-*` 与 `--color-success` 的透明度变化 |
| 事件契约 | **不为宠物新增事件域**。D1/D2 全走已有的 4 秒轮询快照；D6 若需 AI 状态，走已有域或给快照加字段 |
| 护栏要"感知自己瞎了" | `growth_test.go` 扩展长期档位边界；若新增"未使用动画行"的接线，加一条护栏断言 9 行都能被触达（**否则这类"写了但没接"的漂移没人发现**） |
| 分层纪律 | 新增的聚合（跨提醒 streak）放 service，不进 `app.go` |
| 验证入口统一 | 只记 `make test` / `make lint`；不新增脚本 |

## 八、怎么判断值不值（不引入遥测）

全部靠自用观察 + 本地数据：

1. **静置 5 分钟**：宠物是否至少动过 3 次，且没有一次在你暂停/开会时发生。
2. **隔夜再看**：第二天早上芽是否保持昨天的形态（D2 的核心验收）。
3. **右键**：不打开主窗，能不能完成"换个形象"和"今天别吵我"。
4. **录屏**：共享屏幕时宠物仍然不可见（回归项，D1 不应破坏捕获排除）。
5. **电量**：连续挂着 1 小时，Activity Monitor 里 Morrow 的 CPU 与改动前同量级
   （D1 最大的风险不是不好看，是变成耗电大户）。

## 九、证据质量与缺口

- **强**：8 个仓库的星标/许可证/最近提交时间（GitHub API，2026-10-26）；
  VPet / eSheep / BongoCat / clawd-on-desk 的机制描述来自各自 README；
  Morrow 现状全部来自代码（行号级证据见第三节）。
- **中**：星标数与产品价值的相关性（星标是注意力，不是留存）；
  "微动作能提升留存"是机制推导——取自 clawd / BongoCat 的设计选择，
  没有 A/B 或访谈证据。
- **弱 / 未验证**：漫游（D7）与 wails 拖拽/hit-test 的相互影响；
  D6 的"AI 生成中"是否已有可用事件（需看 `internal/ai` 与前端是否有对应事件）；
  macOS 上右键菜单与原生拖拽区的冲突。
- **缺口**：没有抓到 VPet / clawd 的**真实用户评论**（Steam 评论、App 评分），
  因此"用户到底因为什么留下"仍是推断。若要锁定 D1 的优先级，
  建议先花 30 分钟翻一页 Steam 评论里提到"可爱/互动/挂着"的原话。

## 十、来源

**仓库（API 元数据 + README，2026-10-26）**

- [ayangweb/BongoCat](https://github.com/ayangweb/BongoCat) — Live2D、输入即动作、离线无遥测
- [LorisYounger/VPet](https://github.com/LorisYounger/VPet) — 32×4×3 动画、摸头/提起/爬墙、创意工坊
- [rullerzhou-afk/clawd-on-desk](https://github.com/rullerzhou-afk/clawd-on-desk) — 12 状态、hook 驱动、睡眠序列、最小显示时长
- [shinyflvre/Mate-Engine](https://github.com/shinyflvre/Mate-Engine) — VRM + 物理
- [adryd325/oneko.js](https://github.com/adryd325/oneko.js) — 光标追逐最小实现
- [OpenPetsHQ/openpets](https://github.com/OpenPetsHQ/openpets) — 插件 SDK、task-aware
- [Adrianotiger/desktopPet](https://github.com/Adrianotiger/desktopPet) — XML 动画、窗口边缘行走、多宠物
- [ntd4996/agentpet](https://github.com/ntd4996/agentpet) — 长期成长/等级

**Morrow 代码事实**

- `pet_window.go`（透明置顶、捕获排除、多屏位置回落）
- `pet_dock.go`（贴边 20px、500ms 轮询、程序化移动的两道防线）
- `frontend/src/views/Pet.vue`（状态机、7 级气泡优先级、手势判定、4 秒轮询）
- `frontend/src/lib/pet.ts`（9 行动画映射、帧率、`PET_CLICK_MAX_TRAVEL_PX`）
- `internal/service/growth.go`（今日完成率 → 0..4，`DateKey` 翻篇归零）
- `internal/service/checkin.go`（按提醒项的 `Streak`）
- `docs/pet-character.md`（形象设定、人格、未实现钩子）

**相关决策（改动前应重读，避免推翻既有取舍）**

- `docs/adr/0004-smart-pause-local-signals-only.md`（只用本机活跃度，不用摄像头/屏幕内容）
- `docs/adr/0006-presentation-mode-capture-exclusion.md`（演示模式与捕获排除——D1 的微动作不得绕过它）
- `docs/adr/0007-pet-bubble-on-demand-expansion.md`（气泡按需扩窗——D1 不新增常驻扩窗）
- `docs/adr/0011-sassy-pet-personality.md`（人格与语气边界，台词不调 AI）
- `docs/competitive-analysis-round2.md`（温和不强制、严格模式不做、云端不做）
