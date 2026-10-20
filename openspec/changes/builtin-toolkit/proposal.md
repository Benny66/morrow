# 内置功能第一批（builtin-toolkit）

## Why

脚手架迁移到 Wails v3 后，上游自带了一批桌面应用常用件（剪贴板 / 全局快捷键 / 开机自启 /
系统通知 / 应用菜单 / 运行时 MCP 鉴权）。此前功能盘点已给出候选清单，本变更是用户确认的
第一批：把「每个桌面应用都要、且不接就静默缺失」的能力接进脚手架，并给到设置页的真实用法示范。

## What Changes

| # | 能力 | 来源 | 形态 |
|---|------|------|------|
| 1 | 主题三档（跟随系统/亮/暗） | 已有（Go 侧本就接受空串） | 前端补「跟随系统」档 + 系统偏好监听 |
| 2 | 剪贴板 | `app.Clipboard` | 绑定方法 + 设置页「复制数据目录」示范 |
| 3 | 系统通知 | `pkg/services/notifications` | 注册服务 + 绑定方法 + 设置页测试通知 |
| 4 | 全局快捷键（唤起/隐藏主窗口） | `app.GlobalShortcut` | 配置项 + 绑定方法 + 设置页设置入口 |
| 5 | 开机自启 | `app.Autostart` | 绑定方法 + 设置页开关 |
| 6 | 数据库恢复 | service 层新增 | 与既有导出成对：选文件 → 校验 → 原子替换 → 失败回滚 |
| 7 | MCP Token | 运行时 MCP 的 `WAILS_MCP_TOKEN` | 配置项 + 启动期透传 + 生成/查看绑定方法 |
| 8 | macOS Edit 菜单 | `app.Menu` + `EditMenu` role | darwin 下挂应用菜单，让 Cmd+C/V 生效 |

## Impact

- Affected specs: `builtin-toolkit`（新增能力规格）
- Affected code: `main.go`（MCP token 透传、通知服务注册、Edit 菜单）、`app.go`（新绑定方法）、
  `internal/config`（2 个新字段）、`internal/service`（ImportDatabase）、
  `internal/guard`（护栏 24–26）、`frontend/src/stores/app.ts`（主题三档）、
  `frontend/src/views/Settings.vue`（新设置项）
- 不新增任何第三方依赖（全部为 v3 内置 + 标准库）。

## Non-goals

- 通知的动作按钮（NotificationCategory）与定时通知——后续批次。
- 全数据目录打包备份（zip）——本批只补数据库恢复，与既有导出成对。
- 快捷键的用户自定义录制 UI——本批只有单个「唤起主窗口」快捷键的手输配置。
