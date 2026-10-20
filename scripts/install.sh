#!/usr/bin/env bash
# ==========================================================================
# install.sh — 一键构建并安装「健康 AI 提醒」到本机 /Applications
# --------------------------------------------------------------------------
# 用法：bash scripts/install.sh    （或 make install）
#
# 为什么需要它：本项目的代码是在没有 shell 的环境里写的（AI 侧无法执行命令），
# 所以编译这一关必须由用户在本地真实跑一次。本脚本把「生成绑定 → 装前端依赖 →
# 前端构建 → Go 测试 → 静态检查 → 构建 .app → 装进 /Applications」串成一条命令，
# 任何一步失败都会立刻停下并打印失败原因，不会把一个半成品装到系统里。
#
# 【为什么先测试再构建】测试与静态检查是这个仓库的护栏（架构分层、模型注册、
# 契约镜像、依赖登记）。跳过它们直接打包，等于把一堆"约定"重新交回人脑去记。
#
# 【为什么不用 make package】make package 走的是 dmg 美化流程（挂载 Finder 布局），
# 本地自用只需要一个能跑的 .app；出 dmg 是分发场景，用 make package os=macos。
# ==========================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# 应用名从 build/config.yml 读，避免脚本里硬编码一个会漂移的名字。
APP_NAME="$(awk -F'"' '/^[[:space:]]*productName:/ {print $2; exit}' build/config.yml)"
if [ -z "$APP_NAME" ]; then
  echo "错误：无法从 build/config.yml 的 info 段读取 productName" >&2
  exit 1
fi
DEST="/Applications/${APP_NAME}.app"

step() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
info() { printf '    %s\n' "$1"; }
die()  { printf '\n\033[31m✗ %s\033[0m\n' "$1" >&2; exit 1; }

# ---- 0. 前置检查（早失败好过编译到一半才炸）----
step "检查构建环境"
command -v go   >/dev/null 2>&1 || die "未找到 go。请先安装 Go 1.25+（brew install go）"
command -v node >/dev/null 2>&1 || die "未找到 node。请先安装 Node 20+（brew install node）"
command -v npm  >/dev/null 2>&1 || die "未找到 npm（随 Node 一起安装）"
info "go:   $(go version | awk '{print $3}')"
info "node: $(node --version)"

# ---- 1. Wails v3 CLI（版本与 go.mod 钉死的一致，不用 @latest）----
WAILS3="$(go env GOPATH)/bin/wails3"
if [ ! -x "$WAILS3" ]; then
  step "安装 Wails v3 CLI（首次需要几分钟）"
  go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.23 \
    || die "wails3 CLI 安装失败。检查网络或代理后重试。"
fi
[ -x "$WAILS3" ] || die "wails3 不在 $WAILS3，无法继续"
info "wails3: $WAILS3"

# ---- 2. 生成前端绑定 ----
# frontend/bindings/** 是 wails3 从 app.go 的导出方法生成的 TS 产物，不入库。
# 漏跑的症状：Go 护栏（internal/guard/bindings_test.go）报「有 N 个导出绑定方法
# 但找不到生成的绑定产物」，前端则整片 import 失败。
step "生成 Go↔TS 绑定"
if ! "$WAILS3" generate bindings -clean=true -ts 2>/dev/null; then
  info "参数形式未被当前 CLI 接受，回退到默认语法"
  "$WAILS3" generate bindings || die "生成绑定失败"
fi

# ---- 3. 前端依赖与构建 ----
# frontend/dist 是根包 //go:embed 的前置条件；没有它 go build 直接报
# `pattern all:frontend/dist: no matching files found`。
step "安装并构建前端"
cd frontend
if [ ! -d node_modules ]; then
  info "首次安装前端依赖（几分钟）"
  npm install || die "npm install 失败"
fi
npm run build || die "前端构建失败（上面是 vue-tsc / vite 的错误详情）"
cd ..

# ---- 4. 测试与静态检查（护栏）----
step "运行 Go 测试（含架构护栏）"
go test ./... || die "测试未通过。上面列出了失败用例；请勿带着红测试打包。"

step "运行静态检查（gofmt + 护栏 + vet + ESLint + 类型检查）"
make lint || die "静态检查未通过。按上面的提示修完再重跑。"

# ---- 5. 构建 macOS .app ----
# 走 scripts/build.sh 而不是裸 wails3：版本号需要同时注入产物与应用内，
# 它是唯一的版本注入点。
step "构建 macOS 应用"
bash scripts/build.sh || die "构建失败"

APP="build/bin/${APP_NAME}.app"
[ -d "$APP" ] || die "未找到构建产物 $APP"

# ---- 6. 安装到 /Applications ----
step "安装到 ${DEST}"
if pgrep -x "$APP_NAME" >/dev/null 2>&1; then
  info "检测到正在运行的实例，先退出"
  osascript -e "tell application \"${APP_NAME}\" to quit" >/dev/null 2>&1 || true
  sleep 1
fi
if [ -d "$DEST" ]; then
  info "覆盖旧版本"
  rm -rf "$DEST" || die "删除旧版本失败，请检查 $DEST 的权限"
fi
cp -R "$APP" "$DEST" || die "复制到 /Applications 失败。可手动把 $APP 拖进「应用程序」文件夹。"
# 本地构建的产物本没有隔离标记；清一次是为了避免此前从网络拿过同名包留下的标记拦路。
xattr -dr com.apple.quarantine "$DEST" >/dev/null 2>&1 || true

printf '\n\033[32m✓ 安装完成：%s\033[0m\n' "$DEST"
printf '  首次启动请到「设置」页填写 AI API Key（默认端点 https://api.deepseek.com/v1，模型 deepseek-v4.1-flash）。\n'
printf '  打开应用：open "%s"\n\n' "$DEST"
open "$DEST"
