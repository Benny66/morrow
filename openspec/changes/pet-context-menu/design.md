# design — 宠物右键菜单

## 1. Spike 结论（已由源码确认，不再是待验证项）

原本计划"先挂日志、人工右键三次"。实际改为**直接读 wails 运行时的实现**——
这比人工观察更确定，也和本仓的习惯一致（`PET_CLICK_MAX_TRAVEL_PX` 的注释就是读
`drag.ts` 得来的，文章里还读过 WebKit 的 `Element.cpp`）。

证据：`frontend/node_modules/@wailsio/runtime/dist/drag.js`（v3.0.0-beta.23）。

### Spike A：右键事件确实会到达页面

三条判据：

1. **拖拽只由主键 arm**。`update()` 里 `pressed` 是"新按下的位"，只有 `pressed & 1`
   才进 `primaryDown()`，而 `primaryDown` 里才 `canDrag = isDraggableEvent(...)`。
   右键按下只让 `buttons` 记上 `1 << 2`，既不 arm 也不置 `dragging`。
2. **contextmenu 的抑制条件只有 `dragging || resizing`**。
   `suppressEvent()`（第 78–95 行）对 `click`/`contextmenu`/`dblclick` 的处理是：
   仅当 `dragging || resizing` 为真才 `stopImmediatePropagation + preventDefault`。
3. **拖拽进行中的右键会被吞**：`update()` 第 143–151 行 `dragging && event.button !== 0`
   → 抑制。这正是期望行为（拖拽中不该弹菜单）。

结论：**用 `contextmenu` 作事件源，不需要 `mousedown(button===2)` 兜底，也不需要
no-drag 的"菜单把手"兜底方案**。实现见 `Pet.vue` 的 `onPetContextMenu`。

### Spike B：菜单画在窗口内（自绘），Go 侧零改动

选了**自绘 HTML**，否掉原生菜单：

- 菜单 5 项在默认 240×380 窗口里放得下；小缩放窗口由宽度上限与滚动兜底（见下）；
- 原生菜单要新增绑定方法（`app.go` 长出 API），而入口类需求不该往管道契约上长东西；
- 自绘能直接用设计令牌，与气泡同一套视觉。

两个实现细节，都是踩过才知道的：

- **`position: fixed` 而不是 `absolute`**：`.pet` 有 `padding` 与 `overflow: hidden`，
  absolute 的 `left/top` 以 padding 盒为基准，会和 `event.clientX/Y` 差一个 padding；
  fixed 与视口同源，且不被祖先 `overflow` 裁剪。
- **位置必须夹在窗口内**（`Math.min/max`）：宠物窗口只有 240×380，右键点在下缘时
  菜单会跑出窗口，而 `.pet` 是 `overflow: hidden`——超出部分直接消失，不报错。
- **宽高都要跟窗口走**：菜单首选宽 168px，但宠物缩放 0.5 时窗口只有 120×190。
  故内联 `maxWidth: calc(100vw - 2×padding)` 收窄宽度、`maxHeight` 收口高度并允许
  内部滚动；高度仍以渲染后实测为准（条目有内边距，最后一项还多一行说明）。
- **菜单打开期间跳过气泡 fit**：气泡用 CSS 让位后尺寸变 0，若继续跑气泡适配，
  窗口会缩回基础尺寸——已经夹好的菜单随即被裁、窗口位置也变了。
  故 `fitBubbleOnce` 在 `menuVisible` 时直接返回，关闭菜单后再补一次 fit。

### 仍然需要人工确认的一条

`contextmenu` 在**打包态**（非 dev）的 WKWebView 里是否会被系统原生菜单拦截。
源码只能证明 wails 不吞它，证明不了 WebKit 不弹系统菜单；实现里已经
`@contextmenu.prevent`，但真机右键时若出现系统菜单，说明 preventDefault 的时机或
目标元素需要调整。这一条留在手测清单（`tasks.md` 6.2）。

## 2. 菜单项与动作映射（只用既有绑定）

| 菜单项 | 处理函数 | 复用的绑定 | 备注 |
|---|---|---|---|
| 换形象 | `cyclePetPack` | `ListPetPacks` + `GetPetSettings` + `SetPetSettings` | 顺序即 `ListPetPacks` 的返回顺序；到末尾回卷 |
| 缩放 | `cyclePetScale` | `GetPetSettings` + `SetPetSettings` | 四档 0.75 / 1 / 1.25 / 1.5，到顶回到底档 |
| 今天先这样 | `muteTodayFromMenu` | `MuteSitToday` | 与气泡「更多」里的同一动作同源 |
| 打开主窗 | `openMainWindow` | `OpenMainWindow` | **复用既有函数**，不为菜单另写一份 |
| 隐藏宠物 | `hidePetFromMenu` | `SetPetSettings({pet_visible:false})` | 文案带「可在设置里重新打开」 |

> 处理函数名不是随手起的：`lib/pet.ts` 的 `PetMenuAction` 与它们是同名契约，
> 护栏 `pet_menu_test.go` 按名字逐个校验 `function <名字>(` 存在——
> 加了菜单项却忘了写分发，测试阶段就红，而不是等用户点了没反应。

`SetPetSettings` 收的是完整结构体，故三处（换形象 / 缩放 / 隐藏）都必须先用**已轮询到的
`GetPetSettings` 快照**拼出完整入参，不能只传要改的字段——只传一个字段会把另外两个字段
写成零值（`pet_scale:0` → 回落默认、`pet_pack_id:""` → 回落 builtin），
表现为"换个形象顺手把缩放重置了"。这是本变更最容易踩的一个坑。

反过来，有两件事 `SetPetSettings` **已经做好**，不要重做（`app.go:1746–1812`）：

- 演示模式下它会保持隐藏（`petShouldBeVisible` 把演示模式算进可见性判断），
  所以菜单点「隐藏」不会在退出演示后被意外显示出来；
- 重新显示前会先 `restorePetFromDockLocked`，不会 `Show` 出屏幕外那块
  ——这正是"隐藏必须有回程"这条规格的现成实现。

结论：D3 在 Go 侧确实**一行都不用改**；它的价值全在"把入口放到宠物身上"。

## 3. 菜单与既有 UI 的互斥关系

宠物窗口里同时可能存在：气泡（7 级优先级）、倒计时覆盖、菜单。
三者在同一块 240×380 的透明窗口里，只能有一个可见。

规则：
- 菜单弹出前先收起气泡（复用既有的"单气泡"约束，不新造一个互斥状态机）；
- 菜单打开期间不播微动作（`pet-autonomy` 的门控里加一条：菜单可见 → 不演）；
- 菜单打开时窗口位置不变（不扩窗、不移动），避免与 `pet_dock` 的轮询互相咬合。

## 4. 为什么不做子菜单

`换形象` 与 `缩放` 天然像子菜单，但透明小窗里的子菜单在鼠标移出主菜单区时极易被
`mouseleave` 关掉（窗口本身会收到 `mouseleave`），实现成本远高于收益。
故做成"点击循环切换"：换形象 = 下一个，缩放 = 下一档。想精确选择就去设置页——
菜单的价值是**少走一步**，不是替代设置页。

## 5. 风险

| 风险 | 处置 |
|---|---|
| 右键触发拖拽，窗口被拖走一点 | Spike A 里专测"按下微动"；兜底用 `mousedown` + `preventDefault` |
| 菜单被窗口裁剪 | 菜单高度按 `5 × 32 + padding` 预留，并断言不超过窗口高度 |
| 隐藏后找不回来 | 文案写明回程；设置页已有「显示桌面宠物」开关；不提供"隐藏后无入口"的路径 |
| 与 `pet-autonomy` 的微动作打架 | 两边共用同一个门控函数；菜单可见作为门控的一项 |
| 菜单与菜单栏文案漂移 | 菜单项标题只在 `lib/pet.ts` 定义一次，菜单栏若已有同名项则引用同一常量 |

## 6. 与铁律的对应

- **单一真相**：菜单项标题与顺序只在 `lib/pet.ts`；宠物缩放的合法档位仍以
  `config.MinPetScale/MaxPetScale` 为准（前端不另立上下界）。
- **禁硬编码色值**：菜单背景/边框/文字全部走 token 与表面色变量。
- **事件契约不变**：菜单是纯前端交互，不发事件。
- **护栏**：`internal/guard/pet_menu_test.go` 断言 `lib/pet.ts` 里每个菜单 action
  都能在 `Pet.vue` 找到对应的绑定调用；解析 0 个即报错（防"加了菜单项但没接线"）。
- **验证入口统一**：`make test` / `make lint`，不新增脚本。
