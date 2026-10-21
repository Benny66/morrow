# 宠物打卡自愈、防重与可见错误

**Blocked by**: 无

> 规格：B-2、B-3、B-4、B-5、B-6

## 目标

宠物气泡里的完成/推迟失败时不再沉默：已被处理就自愈收起，真失败显示可重试文案；连点只发一次请求。

## 涉及层

- [ ] UI 层（`frontend/src/views/Pet.vue`）
  - `actCurrent` 增加 `busy` in-flight 守卫；完成/推迟按钮 `:disabled="busy"`，补禁用态样式。
  - 失败时先 `refreshPending()`：
    - 当前 fire 已不在列表 → 不显示错误；列表为空按原逻辑庆祝，否则切换到下一条。
    - 当前 fire 仍在 → 设置 `bubbleError = '操作失败，请再试一次'`。
  - 错误文案在动作开始、当前 fire 变化、气泡切换时清除；保留「打开客户端」。
  - 错误行使用现有令牌色（如 `--color-danger`），不写死 hex。
- [ ] 逻辑层
  - 复用 `ListPendingFires`；不新增绑定、不新增事件。
- [ ] 测试
  - 手动 1：`sqlite3` 把当前待处理 fire 改成 `done` → 点完成 → 气泡消失，无错误行。
  - 手动 2：`sqlite3` 持有 `BEGIN IMMEDIATE` 制造 `database is locked` → 点完成 → 显示「操作失败，请再试一次」；释放后重试成功。
  - 手动 3：快速连点 10 次 → 日志只有 1 次 `CompleteReminder`。

## 验收标准

- 已处理场景：气泡自愈消失，不需要反复点击。
- 真失败：错误行可见，重试路径可用；0.5 档的可见高度由 005 的实测扩窗兜住，本票在 1.0 档验收。
- 连点只有一次绑定调用。
- 无 `console.log`；`make lint` 通过。
