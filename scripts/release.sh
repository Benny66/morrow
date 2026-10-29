#!/usr/bin/env bash
# ==========================================================================
# release.sh — 版本号管理 + 打包 + 生成 release 说明（可选）
# --------------------------------------------------------------------------
# 用法：bash scripts/release.sh <version>   （如 0.1.0）
# 做四件事：
#   1. 把 <version> 写进 build/config.yml 的 info.version（版本号单一真相）并渲染
#   2. 三平台打包（windows/macos/linux；构建经 scripts/build.sh 注入版本）
#   3. 生成 RELEASE_NOTES.md
#   4. 打印产物清单
# 说明：这是可选辅助脚本，CI 主流程不依赖它。发布需自行上传到托管平台。
# 注意：版本号必须是数字点分格式（不同平台打包对它有格式要求，见下）。
#
# 【v3 差异，改前必读】v3 的 `wails3 build` 既没有 -o（输出路径固定在 Taskfile 的
# BIN_DIR = build/bin），也没有 -platform（改用 GOOS= / ARCH= 变量）。故本脚本
# 不再能把产物直接写到 dist/，改为「先构建到 build/bin，再按发布命名复制进 dist」。
# 产物命名与 .github/workflows/release.yml 的「收集产物」保持一致——本地跑
# release.sh 与 CI 发版得到同一套资产名，才不会出现「本地叫这个、线上下载叫那个」。
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

APP_NAME="$(bc_read_info productName)"
if [ -z "$APP_NAME" ]; then
  echo "错误：无法从 build/config.yml 的 info 段读取 productName" >&2
  exit 1
fi

HOST="$(uname -s)"

# 更新版本号单一真相（格式校验 + 写入 + 渲染 + 回读都在 set-version.sh 里，
# 与 CI 的发布工作流共用同一处逻辑——两处各写一份版本是漂移的温床）。
bash scripts/set-version.sh "$VERSION"

echo "==> 打包版本 $VERSION ..."
rm -rf dist
mkdir -p dist

# 三平台打包。每步失败只提示跳过，不中断其余平台：
# 单机上不可能三个平台都打得出来（macOS 只能在 mac 上打、Linux 只能在 linux 上打，
# 只有 Windows 支持交叉编译），这不是失败。
declare -a MADE=()
win_ok=0; mac_ok=0; lin_ok=0

# ---- Windows/amd64（唯一支持交叉编译的目标）----
echo ""
echo "--- Windows/amd64 ---"
if command -v makensis >/dev/null 2>&1; then
  if WAILS_ACTION=package bash scripts/build.sh GOOS=windows ARCH=amd64 INSTALL_SCOPE=user; then
    cp "build/bin/${APP_NAME}-amd64-installer.exe" "dist/${APP_NAME}-${VERSION}-windows-amd64-setup.exe"
    win_ok=1
  fi
elif bash scripts/build.sh GOOS=windows ARCH=amd64; then
  echo "提示：未找到 makensis（NSIS），降级为裸 exe（非安装器）。" >&2
  cp "build/bin/${APP_NAME}.exe" "dist/${APP_NAME}-${VERSION}-windows-amd64.exe"
  win_ok=1
fi
[ "$win_ok" = 1 ] || echo "⚠ Windows 打包跳过（需 makensis，或当前环境无法交叉编译）" >&2

# ---- macOS/universal（含美化 dmg）----
echo ""
echo "--- macOS/universal ---"
if [ "$HOST" = "Darwin" ]; then
  # 经 package.sh 而非直接 build.sh：发布资产是【用户双击就能装的 dmg】，
  # 而不是裸 .app。package.sh 已把 universal + 美化 + 产出后回读校验串好。
  if bash scripts/package.sh macos; then
    cp "build/bin/${APP_NAME}.dmg" "dist/${APP_NAME}-${VERSION}-macos-universal.dmg"
    mac_ok=1
  fi
fi
[ "$mac_ok" = 1 ] || echo "⚠ macOS 打包跳过（只能在 mac 上打）" >&2

# ---- Linux/amd64 ----
echo ""
echo "--- Linux/amd64 ---"
if [ "$HOST" = "Linux" ]; then
  # v3.0.0 默认 GTK4 + WebKitGTK 6.0；不再传 webkit2_41（旧 GTK3 栈的标签，
  # 需要时用 -tags gtk3）。Linux 只进 CI 编译腿、不进发布矩阵。
  if bash scripts/build.sh GOOS=linux ARCH=amd64; then
    cp "build/bin/${APP_NAME}" "dist/${APP_NAME}-${VERSION}-linux-amd64"
    lin_ok=1
  fi
fi
[ "$lin_ok" = 1 ] || echo "⚠ Linux 打包跳过（只能在 linux 上打）" >&2

# ---- 生成 release 说明 ----
# 产物清单按【实际打出来的】生成，不写死三行：写死会让说明与实际资产不符，
# 而这类不符没人会发现（说明是给人看的，不参与任何校验）。
{
  echo "# ${APP_NAME} ${VERSION}"
  echo
  echo "## 变更"
  echo
  echo "- （请在此填写本次发布的变更内容）"
  echo
  echo "## 产物"
  echo
  if [ "$win_ok" = 1 ]; then
    if [ -f "dist/${APP_NAME}-${VERSION}-windows-amd64-setup.exe" ]; then
      echo "- \`${APP_NAME}-${VERSION}-windows-amd64-setup.exe\`"
    else
      echo "- \`${APP_NAME}-${VERSION}-windows-amd64.exe\`"
    fi
  fi
  [ "$mac_ok" = 1 ] && echo "- \`${APP_NAME}-${VERSION}-macos-universal.dmg\`"
  [ "$lin_ok" = 1 ] && echo "- \`${APP_NAME}-${VERSION}-linux-amd64\`"
  if [ "$win_ok$mac_ok$lin_ok" = "000" ]; then
    echo "- （本次未产出任何安装包——当前环境不支持任何目标平台的打包）"
  fi
  echo
  echo "## 校验"
  echo
  echo "- 冒烟测试：\`make smoke\`"
  echo "- 静态检查：\`make lint\`"
} > "RELEASE_NOTES.md"

echo ""
echo "✓ 完成，产物在 dist/，release 说明见 RELEASE_NOTES.md"
ls -la dist/ 2>/dev/null || true
