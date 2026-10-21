# 005 — 暂停设置界面

**Blocked by**: 001, 002, 003, 004

## 目标

设置页提供四项触发条件的独立开关（空闲 / 锁屏 / 会议 / 全屏），默认全部开启；关闭后对应触发不再生效。空闲阈值（3 / 5 / 10 分钟）可配置。

## 涉及层

- [ ] 数据层：复用 001-004 的 `internal/config` 字段
- [ ] 逻辑层：`internal/pause/provider.go` 读取配置，动态启用/禁用各触发
- [ ] UI 层：`frontend/src/views/Settings.vue` 新增「智能免打扰」分组，包含 4 个开关与空闲阈值下拉框；`frontend/bindings/` 重新生成
- [ ] 测试：`frontend/src/views/Settings.test.ts`（若存在）验证开关绑定；手动验收

## 验收标准

- 设置页显示 4 个开关，默认全部开启
- 关闭任一开关后，对应触发不再生效
- 空闲阈值可切换 3 / 5 / 10 分钟
- 配置持久化到 `config.json`
- `make test` 与 `make lint` 通过
- 覆盖 US-7
