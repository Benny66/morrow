# 宠物右键菜单（pet-context-menu）

## Why

宠物是**唯一永远在屏幕上**的东西，却是**唯一没有入口**的东西：`Pet.vue` 全库没有
`contextmenu` 监听。现在想换个形象，路径是「打开主窗 → 设置 → 找到宠物区」——
对一个每秒都在眼前的挂件来说，这个路径长到不合理。菜单栏虽然有入口（`internal/tray`），
但它不在宠物旁边，用户的第一反应仍然是"对着它点右键"。

本变更是分析文档里的 D3，也是三件 P0 里最小的一件：**不加新能力，只补一个入口**。

## What Changes

| # | 变化 | 落点 |
|---|------|------|
| 1 | 右键宠物本体弹出菜单：换形象 / 缩放 / 今天先这样 / 打开主窗 / 隐藏宠物 | `Pet.vue` 新增菜单层 |
| 2 | 所有菜单动作复用**既有绑定方法**（`ListPetPacks` / `SetPetSettings` / `MuteSitToday` / `OpenMainWindow` / `GetPetSettings`） | 不新增 API、不改 `app.go` |
| 3 | 「隐藏宠物」标注回程：文案写明可在设置中重新打开 | 菜单文案常量 |
| 4 | 菜单与气泡互斥：菜单弹出前先收起气泡 | 复用既有单气泡约束 |

## Impact

- Affected specs: `pet-interaction`（新增能力规格，本轮首次建立基线）
- Affected code: `frontend/src/views/Pet.vue`（菜单层 + 事件）、
  `frontend/src/lib/pet.ts`（菜单项常量与顺序）、`internal/guard/pet_menu_test.go`（新）
- **不改** `app.go`、**不改** `internal/config`、**不改** `deps.yaml`
- 顺序约束：本变更与 `pet-autonomy` 都动 `Pet.vue`，但互不依赖。
  建议 `pet-autonomy` 先合，避免菜单层与微动作调度器在同一处冲突。

## Non-goals

- **摸头 / 长按 / 双击**：交互深度问题，且受 wails 原生拖拽命中测试限制
  （`PET_CLICK_MAX_TRAVEL_PX` 注释已说明"手抖 1px 这一轮既不能拖也不能挥手"），
  需要先解决命中测试，单独立项。
- **原生菜单 vs 自绘菜单的取舍不在 proposal 阶段决定**：由 spike 定（见 `design.md`）。
- **宠物上的通知/待处理卡片**：已有气泡与强提醒承载，不在这里叠第二套。
- **多宠物切换 UI**：素材包已经是单选，不引入"宠物列表"。
