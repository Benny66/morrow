//go:build production

package main

// debugBuild 标识当前为开发构建；生产构建为 false。
//
// 生产构建会带上 production 标签——v3 里这是【本仓 Taskfile 的 BUILD_FLAGS】在
// DEV!=true 分支里加的，不是 CLI 代劳的（见 build/*/Taskfile.yml）。
const debugBuild = false
