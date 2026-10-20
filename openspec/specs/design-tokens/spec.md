# design-tokens Specification

## Purpose
TBD - created by archiving change design-system. Update Purpose after archive.
## Requirements
### Requirement: 设计令牌唯一真相
颜色、间距、圆角、阴影、动效 MUST 定义在统一的 tokens 文件中，组件只引用令牌变量，不写死具体值。

#### Scenario: 令牌集中
- **WHEN** 查看 `frontend/src/styles/` 下的令牌文件
- **THEN** 存在 colors / spacing / radius / shadow / typography / motion 六类令牌，且无分散在组件里的同类定义

#### Scenario: 组件引用令牌
- **WHEN** 检查组件样式
- **THEN** 颜色、间距、圆角均通过 `var(--token-*)` 引用，而非字面量

### Requirement: 单主色派生色阶
系统 MUST 支持只提供一个主色值，自动派生完整的色阶及 hover/active/边框/浅底/文字强调等语义色。

#### Scenario: 换肤只改主色
- **WHEN** 修改唯一的 `--color-primary` 变量
- **THEN** primary-50 至 primary-900 及所有语义色随之更新，全站视觉统一变化

### Requirement: 主题切换
系统 MUST 提供暗色与亮色主题，默认跟随系统偏好，可通过切换应用，且用户选择持久化到 config。

#### Scenario: 首次启动跟随系统
- **WHEN** 应用首次启动且 config 中主题为空（未设置）
- **THEN** 主题初始值为系统偏好（系统亮色则亮色，系统暗色则暗色），且 `<html>` 的 `data-theme` 与初始值一致

#### Scenario: 已设置则用 config 值
- **WHEN** 应用启动且 config 中主题非空（用户之前选过）
- **THEN** 主题初始值为 config 值，不再跟随系统

#### Scenario: 切换主题并持久化
- **WHEN** 用户触发主题切换
- **THEN** 全站配色随之切换、设置页按钮高亮同步更新、主题值写入 config.json（重启后保持）

#### Scenario: 手动选择后不再跟随系统
- **WHEN** 用户已手动选择过主题
- **THEN** 后续不再因系统偏好变化而自动改变主题

### Requirement: 禁硬编码色值
前端渲染层 MUST 禁止硬编码 hex 色值与品牌字符串，一律引用令牌或配置。

#### Scenario: 无硬编码色值
- **WHEN** 静态检查前端源码
- **THEN** 无字面量 hex 色值散落在组件中（违反由 guardrails 的 ESLint 规则强制）

