#!/usr/bin/env bash
# ==========================================================================
# set-version.sh — 更新版本号【单一真相】（build/config.yml 的 info.version）
# --------------------------------------------------------------------------
# 用法：bash scripts/set-version.sh <version>   （如 0.1.0）
#
# 版本号只在这里写入，其余一切由它派生：
#   macOS Info.plist / Windows exe 版本资源 / NSIS 注册表（wails3 渲染）
#   + 应用内 app.log 与 GetAppInfo（scripts/build.sh 注入 -ldflags）
#
# 【v3 与 v2 的差异，改前必读】
#   v2：版本号在 wails.json 的 info.productVersion，且 `wails build` 会自动渲染
#       进 Info.plist —— 写完 json 就不管了。
#   v3：版本号在 build/config.yml 的 info.version，且渲染【必须显式】跑
#       `wails3 task common:update:build-assets`。
#   故本脚本写完必须顺手渲染一次，否则 build/darwin/Info.plist 会停在旧值，
#   而 internal/guard/wiring_test.go 的护栏 10b 会（正确地）报红。
#
# 本地 release.sh 与 CI 的发布工作流共用本脚本——两处各写一份版本是漂移的温床。
# ==========================================================================
set -euo pipefail

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  echo "用法: $0 <version>   （如 0.1.0）" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# shellcheck source=lib/wails3-common.sh
. "$ROOT/scripts/lib/wails3-common.sh"

# 格式校验：NSIS 的 VIProductVersion 形如 "${INFO_PRODUCTVERSION}.0"，要求数字点分。
# 非数字版本（v1.2.3 的前缀、git describe 的 abc1234）会让 Windows 打包在最后一步失败，
# 故在写入前就拒绝，而不是等打包打到一半才炸。
if ! printf '%s' "$VERSION" | grep -qE '^[0-9]+(\.[0-9]+){2,3}$'; then
  echo "错误：版本 '$VERSION' 不是数字点分格式（如 0.1.0）" >&2
  echo "      Windows 的 NSIS 不接受非数字版本，见 build/windows/nsis/project.nsi" >&2
  exit 1
fi

CONFIG="build/config.yml"
if [ ! -f "$CONFIG" ]; then
  echo "错误：找不到 $CONFIG（v3 的版本号单一真相所在）" >&2
  exit 1
fi

# ---- 写入 info 段的 version ----
# 【必须限定在 info: 段内】：文件顶层还有一个 `version: '3'`（Taskfile 格式版本），
# 全局替换会把它也改掉，构建体系当场坏掉。
# 用 awk 整文件重写而非 sed -i：既避开 BSD/GNU sed 的 -i 语义差异，
# 也不需要 sed 的反向引用（BSD sed 与其他实现的表现不一致）。
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
awk -v ver="$VERSION" '
  {
    line = $0
    # 顶格（无缩进）的行是段边界：info 段从 `info:` 起，到下一个顶格键止
    if (line ~ /^[^ \t]/) {
      in_info = (line ~ /^info:[ \t]*$/)
      print line
      next
    }
    if (in_info && line ~ /^[ \t]+version:[ \t]*/) {
      indent = line
      sub(/[^ \t].*$/, "", indent)          # 保留原缩进
      print indent "version: \"" ver "\""   # 本仓 config.yml 用双引号，保持一致
      replaced++
      next
    }
    print line
  }
  END {
    # 命中数必须恰好 1：0 处说明 info 段没有 version 键（或缩进写法变了），
    # >1 处说明段边界判断失效。两种都立即失败，不留下「部分改写」的中间态。
    if (replaced != 1) {
      printf("错误：在 build/config.yml 的 info 段命中 %d 处 version，期望恰好 1 处\n", replaced) > "/dev/stderr"
      exit 1
    }
  }
' "$CONFIG" > "$TMP"

mv "$TMP" "$CONFIG"

# 回读确认：「写没写进去」与「写对了」是两件事——静默失败会让产物带错版本
GOT="$(bc_read_info version)"
if [ "$GOT" != "$VERSION" ]; then
  echo "错误：写入 $CONFIG 的 info.version 失败（读到 '$GOT'，期望 '$VERSION'）" >&2
  exit 1
fi
echo "✓ 版本单一真相已更新为 ${VERSION}（${CONFIG} 的 info.version）"

# ---- 顺手渲染，让「真相」与「渲染产物」在提交时即一致 ----
# v2 由 `wails build` 代劳，v3 必须显式做（见头部说明）。
bc_render_assets

# 回读渲染结果：这一步证明「真相 → 产物」的链路真的通了，而不只是文件被改了。
RENDERED_PLIST="build/darwin/Info.plist"
if [ -f "$RENDERED_PLIST" ]; then
  GOT="$(plist_string "$RENDERED_PLIST" CFBundleShortVersionString)"
  if [ "$GOT" != "$VERSION" ]; then
    echo "错误：$RENDERED_PLIST 的 CFBundleShortVersionString 为 '$GOT'，期望 '$VERSION'" >&2
    exit 1
  fi
  echo "✓ 构建资源已同步（${RENDERED_PLIST} = ${GOT}）"
fi
