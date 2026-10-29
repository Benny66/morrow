# tasks — 宠物右键菜单

> 状态：阶段 0–5 与 6.1 完成（`make test` / `make lint` / 前端构建全绿）；6.2–6.8 是**真机手测**，待人工确认。
> 阶段 0 的两个 spike 改为**源码级确认**（比挂日志人工点更确定），结论已写回 `design.md` 第 1 节。

## 0. Spike

- [x] 0.1 Spike A：读 `@wailsio/runtime/dist/drag.js`（v3.0.0-beta.23）确认三件事——
      拖拽只由主键 arm（`pressed & 1`）；`contextmenu` 仅在 `dragging || resizing` 时被抑制；
      拖拽中的右键 mousedown 会被吞（`dragging && event.button !== 0`）。
      **结论：用 `contextmenu`，不需要 `mousedown` 兜底，也不需要 no-drag 的"菜单把手"**
- [x] 0.2 Spike B：菜单 5 项 × 32px + 8px = 168px，窗口 380px 高放得下；
      选**自绘 HTML**，Go 侧零改动。两个实现细节：用 `position: fixed`（避开 `.pet` 的
      padding 基准与 `overflow: hidden` 裁剪）、位置用 `Math.min/max` 夹在窗口内
- [x] 0.3 结论写回 `design.md` 第 1 节（含被否掉的"原生菜单 / mousedown 兜底"方案与原因）

## 1. 常量与数据

- [x] 1.1 `frontend/src/lib/pet.ts`：`PetMenuItem` / `PetMenuAction` / `PET_MENU_ITEMS`
      （5 项，标题与顺序在此唯一）
- [x] 1.2 `PET_MENU_SCALE_STEPS = [0.75, 1, 1.25, 1.5]`，注释说明上下界仍以
      `config.MinPetScale/MaxPetScale` 为准；另加 `PET_MENU_WIDTH` / `PET_MENU_ITEM_HEIGHT` /
      `PET_MENU_PADDING`（不留魔法数字）

## 2. 菜单层（`Pet.vue`）

- [x] 2.1 状态：`menuVisible` / `menuPos` / `menuRef` / `menuBusy`
- [x] 2.2 事件源：`.pet__art` 上 `@contextmenu.prevent="onPetContextMenu"`（同步 preventDefault）
- [x] 2.3 关闭路径：点菜单外（window `mousedown` 捕获）、`Escape`、窗口失焦、执行任一菜单项后
      ——四条都实现（失焦是最后补的：规格里写了，但最容易漏）
- [x] 2.4 菜单项动作：`cyclePetPack` / `cyclePetScale` / `muteTodayFromMenu` / `openMainWindow`（复用）
      / `hidePetFromMenu`
- [x] 2.5 `withPetSettings(patch)` 作为**唯一**提交入口，禁止菜单项里手写结构体字面量

## 3. 样式

- [x] 3.1 `.pet__menu`：168px、圆角、`--bg-raised` 表面、`--border-subtle` 边框、`--shadow-md`
- [x] 3.2 `.pet__menu-item`：纵向两行（标题 + 可选说明），hover 用 `--bg-hover`
- [x] 3.3 定位：`position: fixed` + JS 夹取，不会被 240×380 裁掉
- [x] 3.4 菜单容器与菜单项都声明 `--wails-draggable: no-drag`（否则 click 被拖拽通道吞掉）

## 4. 互斥

- [x] 4.1 `canPlayMicro()` 加入 `menuVisible`（菜单可见 → 不演微动作）
- [x] 4.2 菜单打开时气泡让位：`.pet--menu .bubble { display: none }`（只影响渲染，不动数据）
- [x] 4.3 贴边收起态右键不弹（此刻用户是在"划过"窄边，想要的是展开）

## 5. 护栏

- [x] 5.1 `internal/guard/pet_menu_test.go`：`lib/pet.ts` 的每个 `action` 必须在 `Pet.vue` 里有
      `function <名字>(`；动作名重复也报错；解析 0 个即 Fatal
- [x] 5.2 同文件第二条：`.pet__menu` 规则块必须含 `--wails-draggable: no-drag`（防"菜单能弹但点不动"）
- [x] 5.3 红/绿破坏性验证：
      - 临时加一项 `action: 'notWiredHandler'` → FAIL，点名该动作
      - 删掉 `.pet__menu` 的 no-drag 声明 → FAIL，报错直指"点了没反应"
      - 两处均已还原

## 6. 验证

- [x] 6.1 `make test` + `make lint` + `npm run build:dev` 全绿
- [ ] 6.2a **浏览器端 E2E（自动化）**：vite dev + Playwright 打开 `/#/pet`（240×380），
      右键宠物 → 菜单出现；菜单项文本与顺序与 `PET_MENU_ITEMS` 一致；`Escape` /
      点菜单外 / 失焦 / 点菜单项都能关闭；四角位置（左上/右上/左下/右下）与
      小窗口 120×190 下菜单包围盒均完整落在窗口内。
      **审阅修正**：旧脚本在纯 vite 下点的是被禁用的「换形象」，且少算了一条失败说明，
      会在超时后中断——已改为点「打开主窗」、补外点/失焦/小窗口用例。
      本机未安装 Python Playwright，脚本修正后尚未重跑，故保持未勾选。
- [ ] 6.2b 打包态右键**没有系统原生菜单**（浏览器里测不到；需要 wails 窗口）
- [ ] 6.3 换形象：连续右键切换 3 次，形象按 `ListPetPacks` 顺序轮转，**缩放保持不变**
- [ ] 6.4 缩放：四档循环，到顶回到底档；重启后仍是该档
- [ ] 6.5 隐藏：宠物消失 → 设置页「显示桌面宠物」能重新打开，且回到原位置
- [ ] 6.6 贴边收起状态下不弹菜单；悬停展开后右键正常
- [ ] 6.7 演示模式下右键仍可用（菜单是用户主动操作，不属于"打扰"）
- [ ] 6.8 点菜单项不会被当成拖动（窗口位置不变、动作有反应）
      ——浏览器里无 wails 拖拽通道，只能在真机确认

## 7. 文档同步

- [x] 7.1 `docs/map.md`：`views/Pet.vue` 描述补"右键菜单"；`lib/pet.ts` 补"菜单项单一真相"
- [x] 7.2 `README.md` 桌面宠物段落补一句入口（顺带改掉两处"芽随今日完成率"过时表述）
- [x] 7.3 E2E 固化为 `make e2e-pet`（`scripts/e2e-pet.sh` + `scripts/e2e/pet_{menu,states}.py`）：
      编排负责起 vite / 就绪探测 / 退出清理，缺依赖时显式失败并给安装命令；
      产物写 `build/e2e-pet/` 并加进 `.gitignore`。**不并入 `make test`**——
      它需要 python3 + playwright + 浏览器，并进去会让没有这套环境的机器直接红

## 审阅修正（self-review）

逐条对照源码复查后发现并修掉的 4 处：

1. **护栏是假绿的（最重要）**：`TestPetMenuActionsAreWired` 第一版只断言"处理函数存在"，
   于是把 `runMenuItem` 里某个 `case` 删掉之后它仍然是绿的——函数在，但永远没人调。
   补了第二条断言 `case '<action>':`，并做了红态验证（删 `cyclePetScale` 的 case →
   FAIL 且点名）。护栏的价值在"删了会红"，不在"写了会过"。
2. **菜单常量无人守**：`PET_MENU_WIDTH / PADDING / SCALE_STEPS` 原本不在任何护栏的
   引用检查里（前缀只匹配 `MICRO|DOZE|SLEEP|WAKE`）。已把 `MENU` 加进前缀。
3. **收起时不关菜单**：菜单开着时把宠物拖到边缘收起，窗口移到屏幕外、菜单跟着离开可视区；
   下次划过窄边展开时，菜单会以过期位置突然出现。已在 `pet:dock` 事件里关掉。
4. **README 重复**："右键宠物可换形象、调缩放、今天先这样、打开主窗或隐藏；支持缩放、隐藏
   与导入素材包"——后半句的"缩放、隐藏"与前半句重复，已删。
5. **E2E 脚本对 `npx` 的依赖是个隐藏的单点故障**：本机 pyenv 的 shims 在并发下抢不到
   rehash 锁时，`npx vite` 会静默卡 60 秒（日志一个字都没有），最后表现为
   "等待 dev server 就绪超时"——看起来像脚本写错了。改成直接调
   `frontend/node_modules/.bin/vite`，完全绕开 shim 链，启动也从 ~3s 降到 ~1s。

## 实施中新发现

- **浏览器 E2E 当场抓到 6px 溢出（已修）**：菜单真实高度是 178px（条目有内边距，
  且「隐藏宠物」多一行说明），而按 `5 × 32 + 8` 估算是 168px。差出的 6px 会让菜单
  下缘越过 380px 窗口底边——`.pet` 是 `overflow: hidden`，最后一项会被裁掉**且不报错**。
  修法：先用常量粗放一次（避免首帧闪到窗口外），`nextTick` 后用 `offsetHeight/offsetWidth`
  实测再夹一次。同时删掉了 `PET_MENU_ITEM_HEIGHT`——高度不是常数，留着它会诱导下一个人
  再算一次错的。
- **`overflow: hidden` 让"菜单能不能看见"变成概率问题**：绝对定位元素一旦越界就直接消失、
  不报错。所以位置必须夹取，且用 `fixed` 而不是 `absolute`（后者以 `.pet` 的 padding 盒
  为基准，与 `event.clientX/Y` 差一个内边距）。
- **右键与拖拽通道并不冲突，但菜单与拖拽通道会**：右键本身不会启动拖拽（源码已证），
  真正要防的是"菜单项是点击区却被 `.pet` 的 `--wails-draggable: drag` 继承"——
  于是每个可点元素都得显式 `no-drag`。气泡和气泡按钮早就各自写过这条注释，
  这次是第三处，已经由护栏钉住，不再靠"记得写"。
- **规格里最容易漏的一条是"窗口失焦关闭"**：它不是必要的（点外面也能关），
  但写在 spec 里就要实现。实现时发现 `onBlur` 已存在（原用于手势收尾），
  复用比新挂一个监听更稳。
- **纯前端 E2E 的边界要说清**：浏览器里能验"渲染 / 夹取 / 关闭 / 菜单项是否可点"，
  验不了"wails 拖拽通道 / 系统原生菜单 / 绑定动作真的生效"。这三件仍然只能真机跑。
- **固化 `make e2e-pet` 时踩到两个与宠物无关、但都会静默失败的坑**（都已修）：
  1. `make help` 的目标名正则 `^[a-zA-Z_-]+:` **不含数字**，于是 `e2e-pet` 明明写了
     `## ` 说明却永远不出现在 help 里，没有任何报错——新入口等于没有入口。
  2. macOS 自带 bash 是 3.2，**全角标点的首个字节会被吃进变量名**：
     `echo "（$BASE_URL）"` 在 `set -u` 下报 `BASE_URL\xef: unbound variable`，
     看起来像变量没定义。紧挨中文标点的展开一律改 `${VAR}`。
  另外 `help` 里的 `.*?` 是 PCRE 懒惰量词，BSD grep 直接报
  `repetition-operator operand invalid`——`make help` 一直是坏的，顺手修了。

## 审阅修正（第二轮）

- **菜单与气泡**：`fitBubbleOnce` 在 `menuVisible` 时直接返回，关闭后再补一次 fit；
  气泡让位不再触发窗口缩回，菜单不会被夹取后被裁掉。
- **小窗口**：菜单宽度以 `maxWidth: calc(100vw - 2×padding)` 收窄，高度以 `maxHeight`
  收口并允许内部滚动；E2E 新增 120×190 用例。
- **失败提示**：素材包读取失败与「没有导入包」分开，前者显示「读取素材包失败，请重试」。
- **E2E**：点始终可用的「打开主窗」；补外点、失焦、小窗口断言；实现 `PET_E2E_HEADED`；
  `pet_states.py` 补 `micro-groom`，并按前缀清理 growth/today 类。
- **规格**：贴边收起态不弹菜单写回 `pet-interaction`；“完整可见”改为窗口内不被裁、
  过小可滚动。6.2b–6.8 真机手测仍未执行。
