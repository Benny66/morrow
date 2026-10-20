# native-dialogs Specification

## Purpose
TBD - created by archiving change runtime. Update Purpose after archive.
## Requirements
### Requirement: 原生对话框封装
应用 MUST 封装文件/目录/保存/消息对话框为前端可调用的绑定方法。

#### Scenario: 选文件
- **WHEN** 前端调用选择文件绑定方法
- **THEN** 弹出系统原生文件选择框，返回所选路径

#### Scenario: 选目录
- **WHEN** 前端调用选择目录绑定方法
- **THEN** 弹出系统原生目录选择框，返回所选目录路径

#### Scenario: 消息弹窗
- **WHEN** 前端调用消息弹窗绑定方法
- **THEN** 弹出原生消息框，返回用户选择结果

