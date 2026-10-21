# 008 — 休息模式预警 + 宠物陪做

**Blocked by**: 007

## 目标

stand / move / rest 三类提醒到点前 60 秒，宠物气泡与菜单栏同步显示预警文案；休息模式中宠物进入「陪做」状态，与引导动作同步。

## 涉及层

- [ ] 数据层：复用 007 的 `internal/restmode/machine.go` 状态机
- [ ] 逻辑层：`internal/reminder/engine.go` 在 fire 前 60 秒触发预警事件；`app.go` 新增 `GetRestModeState` 绑定方法返回当前状态（idle / warning / countdown / done / skip）与剩余秒数
- [ ] UI 层：`frontend/src/views/Pet.vue` 新增 `warning` 与 `accompanying` 状态；`frontend/src/views/Alert.vue` 休息模式覆盖层同步宠物状态；`internal/tray/tray.go` 菜单栏菜单项显示预警文案
- [ ] 测试：`internal/restmode/warning_test.go` 验证预警时机；手动验收宠物与菜单栏状态

## 验收标准

- 到点前 60 秒宠物气泡显示预警文案（如「1 分钟后该起身拉伸了」）
- 菜单栏菜单项同步显示预警
- 休息模式中宠物进入「陪做」状态
- 预警不发系统通知
- `make test` 与 `make lint` 通过
- 覆盖 US-9、US-27
