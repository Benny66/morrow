# 009 — 多屏强提醒

**Blocked by**: 无

## 目标

强提醒窗口覆盖所有显示器，每个屏幕都可独立处理提醒；主屏行为不回归——主屏仍优先显示待处理卡片与操作入口。

## 涉及层

- [ ] 数据层：无新增
- [ ] 逻辑层：`alert_window.go` 修改 `alertGeometry` 返回所有显示器的 bounds 列表；`main.go` 为每个显示器创建一个 alert 窗口；主屏窗口标记为 `primary`，显示完整 UI；副屏窗口仅显示遮罩与简化卡片
- [ ] UI 层：`frontend/src/views/Alert.vue` 根据窗口是否为 `primary` 渲染完整 UI 或简化 UI
- [ ] 测试：手动验收多屏覆盖；`make test` 与 `make lint` 通过

## 验收标准

- 所有显示器都显示强提醒遮罩
- 主屏显示完整待处理卡片与操作入口
- 副屏显示简化卡片（仅标题与完成/跳过按钮）
- 任一屏幕处理提醒后，所有屏幕的强提醒消失
- `make test` 与 `make lint` 通过
- 覆盖 US-18、US-19
