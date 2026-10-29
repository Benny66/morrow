# 统一命令入口——AI 与人只记 make <target>，不记零散脚本路径。
# 未实现的能力显式失败（exit 1），而非静默通过，避免误导。

# wails3 CLI 装在 $GOPATH/bin 下（可能不在 PATH），动态解析而非硬编码。
# 注意可执行名是 wails3（v2 的 CLI 叫 wails），见 go install .../v3/cmd/wails3。
WAILS := $(shell go env GOPATH)/bin/wails3

# 构建标签：macOS 桌面宠物的透明窗口依赖 WebKit 私有 API（drawsBackground）。
# 不加它的症状是「窗口配置正确、CSS 也写了 transparent，但屏幕上是一块底色」——
# 详见 pet_window.go 的 Mac.Backdrop 注释。非 darwin 平台上该 tag 无匹配文件、
# 无副作用，故统一带上，避免 dev 与打包产物行为不一致（dev 下看不到透明最容易误判）。
EXTRA_TAGS ?= private_mac_apis

# 项目自身的 Go 包（排除 node_modules 里被 npm 拉进来的 Go 代码，如 flatted）。
#
# 刻意用递归展开（=）而非立即展开（:=）：go list 会编译根包，而根包依赖
# frontend/dist；干净检出里该目录不存在，立即展开会让**任何** make 目标
# （包括 make help）都先喷一句 `pattern all:frontend/dist: no matching files found`。
# 递归展开只在使用到它的 recipe 里求值，那时 require-dist 前置检查已通过。
GO_PKGS = $(shell go list ./... | grep -v node_modules)

.PHONY: help dev dev-mcp build test lint smoke e2e-pet package install gen icon verify-gen require-dist

# 【目标名必须允许数字】`e2e-pet` 里有 2，早先的字符类是 `[a-zA-Z_-]`，
# 于是它明明有 ## 说明却永远不出现在 make help 里（没有任何报错）。加上 0-9。
# 【不能用 `.*?`】懒惰量词是 PCRE 的，macOS 自带 BSD grep 报
# `repetition-operator operand invalid`，整个 help 会退化成一行错误信息。
help: ## 列出所有命令及用途
	@echo "可用命令："
	@grep -E '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  make %-12s %s\n", $$1, $$2}'

dev: ## 启动开发态（wails3 dev，含前端热更新）
	EXTRA_TAGS=$(EXTRA_TAGS) $(WAILS) dev

# AI 基座：带 MCP 服务的开发态。跑起来后应用会在 127.0.0.1:9099 暴露 MCP，
# Agent 可以 eval 页面 JS、调绑定方法、读 DOM——「改完代码自动验收」靠它。
#
# 【刻意独立成一个 target，不合并进 dev】：mcp 标签会开一个能执行 JS、
# 调用任意绑定方法的本机端口，绝不能是默认行为。唯一受认可的启用方式就是下面
# 这个环境变量——wails3 见到 WAILS_MCP=1 才追加 mcp 标签。
#
# 【这条通道的边界，v3 beta.23 实测】WAILS_MCP=1 只在 `wails3 build` 与 `wails3 dev`
# 上生效——它们分别经由 internal/commands/task_wrapper.go 的 Build() 与 dev.go 读
# envTags() 并导出 EXTRA_TAGS。`wails3 package` **不读**（Package() 只是
# wrapTask("package", …)，不调用 envTags()）。后果：本仓 darwin 的交付路径走 package，
# 所以 `WAILS_MCP=1 make build` 在 macOS 上会【静默地】产出不含 MCP 的产物。
# 要验 MCP 就用本 target；要造带 MCP 的产物请用 `WAILS_MCP=1 wails3 build`。
#
# 该约束由 internal/guard/wiring_test.go 的 TestMCPTagNotInDefaultBuild（源码层）
# 与 TestMCPNotCompiledIntoDefaultBuild（机制层）、scripts/smoke.sh 第 6 步（产物层）共同强制。
dev-mcp: ## 启动开发态并打开 MCP 服务（AI 可操作运行中的应用）
	WAILS_MCP=1 EXTRA_TAGS=$(EXTRA_TAGS) $(WAILS) dev

# 走 scripts/build.sh 而非裸 wails build：版本号需同时注入产物（Info.plist /
# Windows 版本资源）与应用内（app.log / GetAppInfo），后者只能靠 ldflags，
# 且两者必须同源。见 scripts/build.sh 头部说明。
build: ## 编译当前平台产物（快速，.app 不封装 dmg）
	bash scripts/build.sh

# 前置条件：根包（main.go）用 //go:embed 嵌入前端产物，而 frontend/dist 被 gitignore
# ——干净检出里没有它，于是编译根包会失败。报错原文是
#   `main.go:24:12: pattern all:frontend/dist: no matching files found`
# 再配上 `FAIL <模块名> [setup failed]`，看不出真正原因（CI 因此长期红着，
# 本地首次 clone 也会撞上）。故在此显式拦一道并给出修复命令。
require-dist:
	@test -d frontend/dist || { \
	  echo "错误：frontend/dist 不存在。"; \
	  echo "      根包用 //go:embed 嵌入前端产物，编译它需要先构建前端。"; \
	  echo "      修复：cd frontend && npm install && npm run build"; \
	  exit 1; \
	}

test: require-dist ## 运行 Go 测试（含架构护栏）
	go test $(GO_PKGS)

# 前端类型检查不可省：开发态 vite 只剥离类型不做检查，缺了这步类型错误会漂到打包才炸。
# 注意 recipe 内的 `cd frontend` 只影响该行（每行一个 shell），不会泄漏给后续行。
lint: require-dist ## 静态检查（gofmt + Go 护栏 + go vet + 前端 ESLint + TS 类型）
	@test -z "$$(gofmt -l . | grep -v node_modules)" || (echo "gofmt 未通过，请运行 gofmt -w 修复:" && gofmt -l . | grep -v node_modules && exit 1)
	go test ./internal/guard/ && go vet $(GO_PKGS)
	cd frontend && npx eslint "src/**/*.{vue,ts,js}" && npm run typecheck

smoke: ## 冒烟测试（构建 → 启动 → 断言 → 清理）
	bash scripts/smoke.sh

# 宠物页的渲染层验收：起 vite dev + 无头浏览器，断言菜单能弹、四角不越界、菜单项文本对得上，
# 并把各状态（微动作/打盹/惊醒/神态）各截一张图。
#
# 【为什么不并进 test】它需要 python3 + playwright + 浏览器，是"本地验收工具"而非构建依赖；
# 并进去会让 `make test` 在没有这套环境的机器上直接红。故单独一个 target，
# 前置依赖缺失时显式失败并给出安装命令（见 scripts/e2e-pet.sh 头部）。
#
# 【它补的是哪一块】`.pet` 是 overflow:hidden、窗口只有 240×380、可点元素还要排除拖拽——
# 这类"渲染出来才看得见"的问题 go test / vue-tsc 都测不到。系统原生菜单与 wails 拖拽
# 命中测试仍只能在真机确认，见 openspec/changes/pet-context-menu/tasks.md 的手测项。
e2e-pet: ## 宠物页渲染验收（菜单交互断言 + 各状态截图，需 python3 + playwright）
	bash scripts/e2e-pet.sh

package: ## 打包真安装包（make package os=windows|macos|linux [scope=user|machine]）
	bash scripts/package.sh $(os) $(scope)

# 本地自用的一键安装：绑定 → 前端构建 → 测试 → 静态检查 → 构建 .app → 装进 /Applications。
# 与 package 的分工：package 出可分发的 dmg（含 Finder 美化布局），install 只解决
# 「在这台机器上装一个能跑的」，故不碰 dmg 流程。
install: ## 构建并安装到本机 /Applications
	bash scripts/install.sh

gen: ## 生成新模块（锚点注入 + 幂等）—— make gen name=<module>
	bash scripts/gen.sh $(name)

# 图标四处（appicon.png / icons.icns / icon.ico / 托盘内嵌那张）必须同源，
# 唯一真相是 build/icon/appicon.svg。不挂进 build 的理由见 scripts/gen-icon.sh 头部。
icon: ## 从 build/icon/appicon.svg 重新生成三平台图标资产
	bash scripts/gen-icon.sh

verify-gen: ## 生成器端到端验证（临时沙箱：生成 2 个模块 → 编译 → 护栏）
	bash scripts/verify-gen.sh
