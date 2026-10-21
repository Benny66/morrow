# 007 — 休息模式核心（状态机 + UI + CheckIn）

**Blocked by**: 无

## 目标

stand / move / rest 三类提醒到点时进入休息模式：覆盖层显示倒计时与一条与类型匹配的内置引导动作；倒计时结束自动记「完成」CheckIn；用户在倒计时中点击「提前结束」时弹出「算完成 / 跳过」二选一。water / toilet / custom 不进入休息模式，保持简单打卡。

## 涉及层

- [ ] 数据层：`internal/restmode/machine.go`（新）定义状态机（idle → warning → countdown → done / skip）；`internal/restmode/cue.go`（新）内置内容包（rest → 远眺/呼吸，stand → 肩颈拉伸，move → 走动）；`internal/service/checkin.go` 复用既有 `RecordCheckIn`
- [ ] 逻辑层：`internal/reminder/engine.go` 在 fire 投递前判断 kind，若为 stand/move/rest 则触发休息模式而非普通提醒；`app.go` 新增 `EnterRestMode` / `ExitRestMode` / `CompleteRestMode` / `SkipRestMode` 绑定方法
- [ ] UI 层：`frontend/src/views/Alert.vue` 新增休息模式覆盖层（倒计时 + 引导动作文案 + 完成/跳过按钮 + 提前结束按钮）；`frontend/src/lib/restmode.ts`（新）镜像状态机常量
- [ ] 测试：`internal/restmode/machine_test.go` 验证状态转换；`internal/restmode/cue_test.go` 验证内容包完整性；手动验收

## 验收标准

- stand / move / rest 到点时进入休息模式，显示倒计时与引导动作
- 倒计时结束自动记「完成」CheckIn
- 提前结束弹出「算完成 / 跳过」二选一
- water / toilet / custom 不进入休息模式
- 引导动作离线可用，不调用 AI
- `make test` 与 `make lint` 通过
- 覆盖 US-10、US-11、US-12、US-13、US-16
