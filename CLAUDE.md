# CLAUDE.md — Go 域规范

本文件约束 Go 侧代码（根目录 `main.go`/`app.go` 及 `internal/` 下的包）。
AI 在 Go 目录干活时读本份。跨端铁律见根 `AGENTS.md`。

## 命名

| 元素 | 规则 | 示例 |
|---|---|---|
| 包名 | 小写、单数、简短 | `model`、`database`、`service` |
| 文件名 | snake_case | `user_service.go` |
| 结构体/接口 | 大驼峰 | `UserService`、`BaseModel` |
| 方法/函数 | 大驼峰（导出）小驼峰（私有） | `GetUserList`、`getUserPermissionCodes` |
| 变量 | 小驼峰 | `pageSize`、`userIDs` |
| 常量 | 大驼峰 | `MaxPageSize` |

## JSON Tag

所有模型字段 MUST 带 snake_case 的 JSON tag：

```go
type User struct {
    BaseModel
    Username string `gorm:"uniqueIndex;size:50;not null" json:"username"`
    RealName string `gorm:"size:50" json:"real_name"`
}
```

## 分层纪律

Wails 下的分层是：**绑定方法（`app.go` 的 App 方法）→ service → model**。

- 模型只定义数据结构与字段 tag，不含业务逻辑。
- 数据库访问统一走 `internal/database` 与 `internal/model` 的注册表，
  业务包不得自行 `gorm.Open`。
- 新增模型必须登记进 `internal/model/model.go` 的 `AllModels()`（单一真相）。

## 错误处理

- 错误 MUST 显式处理，禁止忽略（`_ =` 需有注释说明）。
- 业务错误通过 `errors.New` 返回中文消息。
- 数据库记录不存在统一用 `errors.New("xxx不存在")`。
- 启动期致命错误用 `log.Fatalf` 显式退出，不得静默吞掉。

## 注释

- 注释使用中文。导出符号必须有注释说明用途。
