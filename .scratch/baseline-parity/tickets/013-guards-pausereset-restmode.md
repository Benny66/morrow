# 013 — 护栏：PauseReason 与休息模式状态

**Blocked by**: 001, 007

## 目标

`internal/guard/` 新增两条护栏：
1. `PauseReason` 枚举在 Go 与前端镜像中一致（parity guard）
2. 休息模式状态机必须覆盖「idle / warning / countdown / done / skip」五种状态（state coverage guard）

## 涉及层

- [ ] 数据层：`frontend/src/lib/pause.ts`（新）镜像 `PauseReason` 枚举；`frontend/src/lib/restmode.ts` 镜像休息模式状态
- [ ] 逻辑层：`internal/guard/parity_test.go` 新增 `TestPauseReasonParity` 与 `TestRestModeStateParity`
- [ ] UI 层：无新增
- [ ] 测试：`internal/guard/parity_test.go` 自身即为测试

## 验收标准

- `PauseReason` 枚举在 Go 与前端镜像中一致；修改任一端时护栏报红
- 休息模式状态机覆盖 5 种状态；新增状态时护栏要求同步更新镜像
- `make test` 与 `make lint` 通过
- 覆盖 spec 中的「护栏」章节
