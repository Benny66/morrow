# 012 — P1：授权与降级

**Blocked by**: 010, 011

## 目标

专注与日历默认关闭；首次开启时弹出授权说明，明确「只读忙闲，不读内容」。用户拒绝授权后，静默降级为空闲/会议/全屏识别；不弹二次请求。

## 涉及层

- [ ] 数据层：`internal/config` 新增 `FocusAuthorizationGranted` 与 `CalendarAuthorizationGranted` 字段
- [ ] 逻辑层：`internal/pause/authorization.go`（新）封装授权状态查询与降级逻辑；`internal/pause/provider.go` 在授权被拒绝时自动禁用对应触发
- [ ] UI 层：`frontend/src/views/Settings.vue` 在授权被拒绝时显示「已降级」提示，并提供「重新授权」按钮（可选）
- [ ] 测试：`internal/pause/authorization_test.go` 验证降级逻辑；手动验收

## 验收标准

- 专注与日历默认关闭
- 首次开启时弹出授权说明
- 用户拒绝授权后，对应触发自动禁用，不弹二次请求
- 设置页显示「已降级」提示
- `make test` 与 `make lint` 通过
- 覆盖 US-24、US-25
