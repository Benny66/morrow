#!/usr/bin/env bash
# ==========================================================================
# build.sh — 带版本注入的 wails3 构建（make build / package.sh / release.sh 共用）
# --------------------------------------------------------------------------
# 为什么需要它：版本号必须同时到达四处，且四处【一致】——
#   1. macOS Info.plist（CFBundleShortVersionString / CFBundleVersion）
#   2. Windows exe 版本资源（build/windows/info.json）
#   3. Windows NSIS 安装器（VIFileVersion / 注册表 DisplayVersion）
#   4. 应用内（app.log 首行 / GetAppInfo）—— 只能靠 -ldflags 注入
#
# 前三处由 wails3 从 build/config.yml 的 info 段渲染；第四处 wails3 不管。
# 若各脚本各算各的，就会出现「系统显示 1.0.0、日志显示 dev」这类双源漂移
# （本仓曾长期如此：Info.plist 恒为 Wails 的默认值）。
#
# 【v3 与 v2 的两处关键差异 —— 改本脚本前必读】
#
#   a) v3 **不自动渲染**构建资源。
#      v2 的 `wails build` 会顺带把 wails.json 的 info 段写进 Info.plist；
#      v3 必须显式跑 `wails3 task common:update:build-assets`（见 lib 的
#      bc_render_assets）。而且 v3 各平台任务里【只有 package 挂了这一步】——
#      bare `wails3 build` 根本不渲染，偏偏 Windows 的 windows:build 里的
#      generate:syso 要读 build/windows/info.json 生成 exe 版本资源。
#      故本脚本必须自己跑，不能指望下游任务。漏掉的症状：应用内显示新版本、
#      系统「显示简介」显示旧版本——正是本脚本当初存在要消灭的那种漂移。
#
#   b) v3 的 `wails3 build` **不转发 -ldflags**（实测 beta.23 的 wrapTask：
#      只转发 EXTRA_TAGS / OBFUSCATED / GARBLE_ARGS / GOOS / ARCH）。
#      故第四处不能靠命令行 ldflags 注入，改经 Taskfile 变量传递：
#          scripts/build.sh → `wails3 build VERSION=x.y.z COMMIT=abc`
#          → 根 Taskfile.yml 的 VERSION_LDFLAGS
#          → 各平台 BUILD_FLAGS 的 -ldflags
#      链子断了的症状：应用内版本永远退回 "dev"，且【没有任何报错】——
#      链接器的 -X 对不存在的符号是静默忽略的（故本脚本在产物上回读版本号）。
#
# 版本号【单一真相 = build/config.yml 的 info.version】，本脚本是唯一注入点。
#
# 用法：bash scripts/build.sh [wails3 的其余参数...]
#   bash scripts/build.sh                          # 等价 make build
#   bash scripts/build.sh GOOS=windows ARCH=amd64  # 交叉编译（v2 是 -platform windows/amd64）
#   bash scripts/build.sh -tags webkit2_41         # Linux：只有 4.1 版 WebKitGTK 时
#   bash scripts/build.sh OBFUSCATED=true          # garble 混淆（macOS 经 package，故用变量）
#
# 环境变量 WAILS_ACTION 可显式覆盖动作（默认按目标平台推断）：
#   WAILS_ACTION=package                    # 打真安装包（windows 出 NSIS）
#   WAILS_ACTION=darwin:package:universal   # 出 universal .app（含冒号即视为显式任务名）
# 供 package.sh 用。见下方动作选择处。
# ==========================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# 共用函数：wails3 CLI 定位 / build/config.yml 读取 / 构建资源渲染 / plist 取值。
# 与 set-version.sh 同源，避免两份解析规则漂移（见该文件的说明）。
# shellcheck source=lib/wails3-common.sh
. "$ROOT/scripts/lib/wails3-common.sh"

# ---- 目标平台 ----
# v3 用 GOOS= / ARCH= 变量表达目标平台（v2 是 -platform windows/amd64）。
# 环境变量 GOOS 优先（wails3 自己也先读它，见 wrapTask），其次命令行 GOOS=，最后宿主。
target_goos() {
  local goos="${GOOS:-}"
  local a
  for a in "$@"; do
    case "$a" in GOOS=*) goos="${a#GOOS=}" ;; esac
  done
  if [ -z "$goos" ]; then
    case "$(uname -s)" in
      Darwin) goos=darwin ;;
      Linux)  goos=linux ;;
      MINGW*|MSYS*|CYGWIN*) goos=windows ;;
      *)      goos="$(uname -s | tr '[:upper:]' '[:lower:]')" ;;
    esac
  fi
  printf '%s' "$goos"
}
TARGET_GOOS="$(target_goos "$@")"

# ---- 动作选择：package 还是 build ----
# 【本脚本唯一「看平台下菜」的地方，原因是 v3 的产物形态与 v2 不同】
#   - v2：`wails build` 在 macOS 上直接产出可运行的 .app。
#   - v3：`wails3 build` 只产出裸 Mach-O 二进制。macOS 的 GUI 应用需要 bundle
#         （证据在本仓自己身上：build/darwin/Taskfile.yml 的 run 任务也要先包一个
#         .dev.app 才能跑），可运行产物走 `wails3 package`
#         （= 渲染资源 + 编译 + create:app:bundle，不含 dmg）。
#   - windows / linux 的裸可执行文件本身就能跑，故仍走 `wails3 build`
#     （windows 的 package 会多做 NSIS 安装器，那是 make package 的事）。
# 代价：`wails3 package` 不接受 build 类 flag（beta.23 的 flags.Package 只内嵌
# Common，无 -tags/-obfuscated）。要在这条路径上开它们就传 Taskfile 变量，如
# `bash scripts/build.sh OBFUSCATED=true`。
#
# 显式覆盖：环境变量 WAILS_ACTION。
#   WAILS_ACTION=build|package   → 上下两个动作之一（package.sh 打 Windows 安装器时用：
#                                  目标平台是 windows、默认动作会是 build，但它要 package）。
#   WAILS_ACTION=<显式任务名>     → 直接 `wails3 task <名>`。用于平台级任务的变体，
#                                   如 darwin:package:universal —— `wails3 package`
#                                   没有「顺带出 universal」的入口，它的任务名固定为
#                                   `{{.GOOS}}:package`。
# 用环境变量而非命令行 flag，是为了不污染 "$@" ——那些参数要原样转发给 wails3，
# 插进去还得再剥掉。
ACTION="${WAILS_ACTION:-}"
if [ -z "$ACTION" ]; then
  ACTION=build
  if [ "$TARGET_GOOS" = "darwin" ]; then
    ACTION=package
  fi
fi
WAILS_SUBCMD=""
case "$ACTION" in
  build|package)
    WAILS_SUBCMD="$ACTION"
    ;;
  *:*)
    # 显式任务名（含命名空间冒号）
    WAILS_SUBCMD="task"
    ;;
  *)
    echo "错误：WAILS_ACTION 只能是 build、package，或一个含冒号的显式任务名（如 darwin:package:universal），收到 '$ACTION'" >&2
    exit 1
    ;;
esac

# ---- 版本号 / 产物名（单一真相 + 格式校验：早失败好过打包到最后一步才炸）----
VERSION="$(bc_version)"
APP_NAME="$(bc_read_info productName)"
if [ -z "$APP_NAME" ]; then
  echo "错误：build/config.yml 的 info 段未声明 productName —— 产物名与 CFBundleName 都靠它" >&2
  exit 1
fi
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || true)"

# ---- 显式渲染构建资源（v3 必须的一步，见头部差异 a）----
bc_render_assets

echo "==> 构建 ${APP_NAME} ${VERSION}${COMMIT:+ (提交 ${COMMIT})} → 目标 ${TARGET_GOOS}（wails3 ${WAILS_SUBCMD} ${ACTION}）"

# 【参数顺序要紧】命令行 flag 必须排在 VERSION= / COMMIT= 之前：Go 的 flag 解析
# 遇到第一个非 flag 参数就停止，`VERSION=x` 不含 `-` 前缀、会被当作位置参数
# 原样交给 Taskfile 变量，排在它后面的 flag 就再也不被解析了。
if [ "$WAILS_SUBCMD" = "task" ]; then
  "$(wails3_cli)" task "$ACTION" "$@" VERSION="$VERSION" COMMIT="$COMMIT"
else
  "$(wails3_cli)" "$WAILS_SUBCMD" "$@" VERSION="$VERSION" COMMIT="$COMMIT"
fi

# ---- 回读校验 ----
# 只断言「config.yml 被读到、且真的进了产物」，这是本脚本存在的全部意义。
# 不做这一步，版本没生效只会在用户看「显示简介」时才发现（本仓曾长期如此）。
echo "==> 回读校验 ..."

# 1) 渲染结果：build/darwin/Info.plist 的版本必须是 VERSION。
#    它是 create:app:bundle 【直接拷贝】进 .app 的那一份（见 build/darwin/Taskfile.yml），
#    所以它对了，macOS 侧的版本载体就对了。这一份在任意平台都会被渲染，故不设平台条件。
RENDERED_PLIST="build/darwin/Info.plist"
if [ -f "$RENDERED_PLIST" ]; then
  GOT="$(plist_string "$RENDERED_PLIST" CFBundleShortVersionString)"
  if [ "$GOT" != "$VERSION" ]; then
    echo "错误：$RENDERED_PLIST 的 CFBundleShortVersionString 为 '$GOT'，期望 '$VERSION'" >&2
    echo "      —— update:build-assets 没生效，请检查 build/config.yml 的 info.version" >&2
    exit 1
  fi
  echo "  ✓ 构建资源版本 = ${GOT}"
fi

# 2) 产物：存在 + 内含注入的版本号
case "$TARGET_GOOS" in
  darwin)
    ARTIFACT="build/bin/${APP_NAME}.app/Contents/MacOS/${APP_NAME}"
    ARTIFACT_PLIST="build/bin/${APP_NAME}.app/Contents/Info.plist"
    ;;
  windows)
    ARTIFACT="build/bin/${APP_NAME}.exe"
    ARTIFACT_PLIST=""
    ;;
  linux)
    ARTIFACT="build/bin/${APP_NAME}"
    ARTIFACT_PLIST=""
    ;;
  *)
    echo "提示：未识别的目标平台 '${TARGET_GOOS}'，跳过产物回读校验" >&2
    ARTIFACT=""
    ARTIFACT_PLIST=""
    ;;
esac

if [ -n "$ARTIFACT" ]; then
  if [ ! -f "$ARTIFACT" ]; then
    echo "错误：未找到构建产物 $ARTIFACT" >&2
    echo "      若产物名与预期不符，多半是根 Taskfile.yml 的 APP_NAME 与" >&2
    echo "      build/config.yml 的 info.productName 漂移（护栏会拦这个）。" >&2
    exit 1
  fi
  echo "  ✓ 产物存在 ${ARTIFACT}"

  # 产物里必须能找到注入的版本字符串 —— 这是「VERSION 变量真的走完了
  # build.sh → Taskfile VERSION_LDFLAGS → 平台 BUILD_FLAGS → 链接器」这条链的
  # 唯一客观证据。链子断了不会有任何报错（-X 对不存在的符号静默忽略），
  # 只表现为应用内版本恒为 dev，所以必须在这里变红。
  if grep -aqF "$VERSION" "$ARTIFACT"; then
    echo "  ✓ 版本号已注入产物（应用内版本 = ${VERSION}）"
  else
    echo "错误：产物中找不到版本字符串 '$VERSION' —— -ldflags 注入链路断了" >&2
    echo "      检查根 Taskfile.yml 的 VERSION_LDFLAGS 是否拼进了平台 BUILD_FLAGS。" >&2
    echo "      链接器对 -X 的错误符号静默忽略，链子断了不会报错，只会让应用内版本退回 dev。" >&2
    exit 1
  fi

  if [ -n "$ARTIFACT_PLIST" ] && [ -f "$ARTIFACT_PLIST" ]; then
    GOT="$(plist_string "$ARTIFACT_PLIST" CFBundleShortVersionString)"
    if [ "$GOT" != "$VERSION" ]; then
      echo "错误：产物 $ARTIFACT_PLIST 的版本为 '$GOT'，期望 '$VERSION'" >&2
      exit 1
    fi
    echo "  ✓ 产物 Info.plist 版本 = ${GOT}"
  fi
fi
