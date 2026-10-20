//go:build !production

package main

// debugBuild 标识当前为开发构建（未带 production 构建标签）。
//
// 判定依据是【构建标签】而非环境变量。归因在 v3 里变了：v2 时代 production 标签由
// `wails build` 自己加，v3 的 `wails3 build` 只是 Taskfile 的薄封装，`-tags production`
// 是【本仓自己的】BUILD_FLAGS 在 DEV!=true 分支里加的（见 build/Taskfile.yml 与
// build/{darwin,linux,windows}/Taskfile.yml）。故这条判定的可信度挂在那些 Taskfile 上：
// 改动 BUILD_FLAGS 时必须连带确认 production 标签还在，否则生产构建会静默退回开发态。
//
// 历史 bug：此处原读环境变量 WAILS_PRODUCTION，而该变量在 Wails 全库中并不存在，
// 判断永远不成立 → 生产态日志级别从未生效（一直按 Debug 输出）。Debug 噪音会快速
// 吃掉 5MB×3 的轮转窗口，把真正的错误冲走。改用构建标签后 dev/build 各自正确。
const debugBuild = true
