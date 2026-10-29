#!/usr/bin/env bash
# ==========================================================================
# package.sh — 参数化打包（真安装包）
# --------------------------------------------------------------------------
# 用法：bash scripts/package.sh [os] [scope]
#   - 无参              → 打包当前平台
#   - windows [user|machine] → 交叉编译 Windows .exe 安装器（mac/linux 上可用），
#                         scope 默认 user（免管理员）
#   - macos             → 仅 mac 上可用，产出美化 dmg（含 Applications 软链）
#   - linux             → 仅 linux 上可用，否则报错
#
# 母版防呆：检测到 __APP_NAME__ 占位符残留则拒绝打包（母版不能直接打包）。
#
# 【v3 差异】所有构建都经 scripts/build.sh，由它做版本注入与构建资源渲染。
# 本脚本不直接调 wails3 —— 否则版本注入就有了第二个入口，
# 又回到「各脚本各算一份版本」的双源漂移（见 build.sh 头部说明）。
# 需要「非 darwin 平台的真安装包」时，用环境变量请求 build.sh 走 package 动作：
#     WAILS_ACTION=package bash scripts/build.sh ...   （见下方 windows 分支）
# ==========================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# shellcheck source=lib/wails3-common.sh
. "$ROOT/scripts/lib/wails3-common.sh"

OS="${1:-}"
SCOPE="${2:-user}"   # windows 安装范围：user（默认，免管理员）/ machine

# ---- 母版防呆：占位符残留则拒绝打包 ----
# 注意：占位符写成下划线拆写形式，避免被 init.sh 的全局替换误伤而失效。
# 清单覆盖占位符真正的落点（v3 没有 wails.json，改查 build/config.yml 与 Taskfile.yml）。
PLACEHOLDER="__APP_""NAME__"
if grep -rq "$PLACEHOLDER" go.mod build/config.yml Taskfile.yml main.go 2>/dev/null; then
  echo "错误：检测到占位符残留——母版不能直接打包。" >&2
  echo "请先实例化：bash scripts/init.sh <your-app-name>" >&2
  exit 1
fi

# 应用名：build/config.yml 的 info.productName（v3 换掉了 wails.json 的 outputfilename）。
# 根 Taskfile.yml 的 APP_NAME 必须与它一致，两者的一致性由
# internal/guard/wiring_test.go 的 TestTaskfileAppNameMatchesConfig 核对。
APP_NAME="$(bc_read_info productName)"
if [ -z "$APP_NAME" ]; then
  echo "错误：无法从 build/config.yml 的 info 段读取 productName" >&2
  exit 1
fi

# 版本注入不在本脚本里做：统一由 scripts/build.sh 负责（读 build/config.yml 的
# info.version，渲染进产物 + 注入 -ldflags）。下面所有构建都经它调用，
# 避免「各脚本各算一份版本」的双源漂移。

# ---- 确定目标平台 ----
CURRENT="$(uname -s)"
case "$CURRENT" in
  Darwin) CUR_PLATFORM="macos" ;;
  Linux)  CUR_PLATFORM="linux" ;;
  MINGW*|MSYS*|CYGWIN*) CUR_PLATFORM="windows" ;;
  *) echo "不支持的当前平台: $CURRENT" >&2; exit 1 ;;
esac

TARGET="${OS:-$CUR_PLATFORM}"

# ---- 校验目标平台在当前机器是否可打包 ----
case "$TARGET" in
  macos)
    if [ "$CUR_PLATFORM" != "macos" ]; then
      echo "错误：macOS 包只能在 mac 上打包（Wails 不支持交叉编译 mac）。" >&2
      exit 1
    fi
    ;;
  linux)
    if [ "$CUR_PLATFORM" != "linux" ]; then
      echo "错误：Linux 包只能在 linux 上打包（Wails 不支持交叉编译 linux）。" >&2
      exit 1
    fi
    ;;
  windows)
    # windows 是唯一支持交叉编译的目标，mac/linux 上都可打
    ;;
  *)
    echo "用法: make package [windows|macos|linux]" >&2
    exit 1
    ;;
esac

# ---- 打包 ----
echo "==> 打包目标: $TARGET (当前: $CUR_PLATFORM) ..."

case "$TARGET" in
  macos)
    # 先清掉上一次的产物：v3 的 build.sh 没有 v2 的 -clean 开关，
    # 且 .app 的 create:app:bundle 只做 cp【不做清理】，残留的旧 .app 会被就地覆盖。
    rm -rf build/bin
    # 出 universal（arm64 + amd64）而非宿主单架构，理由有两条：
    #   1. 产物名与 CI 的发布资产名都写着 macos-universal（.github/workflows/release.yml），
    #      出单架构就是名不副实——而「名字说了什么就有什么」是本仓的既有要求。
    #   2. 本地 `make package` 应当能测到与用户下载到的同一个东西；单架构 dmg 测不出
    #      另一个架构上的问题。
    # v2 时代此处是 `wails build -clean`（宿主架构），universal 只在 release.sh 里做，
    # 于是「本地验过的」和「发出去的」不是同一个产物。v3 顺手把这个缺口合上。
    #
    # 显式请求 darwin:package:universal：`wails3 package` 的任务名固定是
    # `{{.GOOS}}:package`，没有「顺带 universal」的入口，故只能点名任务
    # （版本注入仍走 build.sh 的 VERSION=/COMMIT= 变量，不受影响）。
    WAILS_ACTION=darwin:package:universal bash scripts/build.sh
    command -v hdiutil >/dev/null 2>&1 || { echo "错误：未找到 hdiutil" >&2; exit 1; }

    APP="build/bin/${APP_NAME}.app"
    DMG="build/bin/${APP_NAME}.dmg"
    [ -d "$APP" ] || { echo "错误：未找到 .app 产物 $APP" >&2; exit 1; }

    # ---- 布局常量（单一来源）----
    # 窗口尺寸与图标落点必须同源：背景图箭头的锚点由 ICON_* 推导，
    # 与 osascript 写入的图标落点共用同一 container 坐标系，绝不依赖「宽/2」
    # （窗口 bounds 宽 ≠ content 视口宽，后者受 Finder 侧栏宽度影响而浮动）。
    WIN_X=100; WIN_Y=100; WIN_W=600; WIN_H=400
    ICON_Y=200
    ICON_APP_X=150          # 应用图标落点（container 坐标，position = 图标中心）
    ICON_APPS_X=450         # Applications 软链落点
    ARROW_CX=$(( (ICON_APP_X + ICON_APPS_X) / 2 ))   # 箭头水平中心 = 两图标中点

    # ---- 卷名：/Volumes/<name> 是 Finder 能解析的硬要求 ----
    # 用 mktemp -d 当挂载点会让 Finder 认不出卷（tell disk 报 -1728），
    # 整个布局静默失效。故必须挂到 /Volumes/<volname>，且与 -volname 同名。
    VOLNAME="$APP_NAME"
    if [ -e "/Volumes/$VOLNAME" ]; then
      VOLNAME="${APP_NAME}-$$"     # 同名冲突（残留挂载/同名卷）→ 加后缀规避
    fi
    MOUNT_PT="/Volumes/$VOLNAME"

    # ---- 1. 准备 staging 目录：app + Applications 软链 + 背景图 ----
    STAGING="$(mktemp -d)"
    TMP_DMG="$(mktemp -u).dmg"        # 可读写中间态
    VERIFY_PT="$(mktemp -d)"          # 回读校验挂载点（只读，无需 Finder 解析）
    cleanup_dmg() {
      hdiutil detach "$MOUNT_PT" -quiet 2>/dev/null || true
      hdiutil detach "$VERIFY_PT" -quiet 2>/dev/null || true
      rm -rf "$STAGING" "$VERIFY_PT" "$TMP_DMG" 2>/dev/null || true
    }
    trap cleanup_dmg EXIT

    cp -R "$APP" "$STAGING/"
    ln -s /Applications "$STAGING/Applications"     # 软链：拖进去即装

    # ---- 背景图：构建期合成（应用名 + 按锚点定位的箭头）----
    # 合成到临时文件，不污染入库的占位图 build/darwin/dmg-background.png。
    BG_SRC=""
    if command -v swift >/dev/null 2>&1 || [ -f build/darwin/dmg-background.tpl.png ] || [ -f build/darwin/dmg-background.png ]; then
      BG_TMP="$(mktemp -u).png"
      if bash scripts/compose-dmg-bg.sh "$BG_TMP" "$APP_NAME" "$ARROW_CX" "$ICON_Y" "$WIN_W" "$WIN_H"; then
        if [ -f "$BG_TMP" ] && [ -s "$BG_TMP" ]; then
          mkdir -p "$STAGING/.background"
          cp "$BG_TMP" "$STAGING/.background/bg.png"
          BG_SRC=".background:bg.png"
        fi
      fi
      rm -f "$BG_TMP" 2>/dev/null || true
    fi

    # ---- 2. 生成可读写 dmg（UDRW，才能写 Finder 布局） ----
    hdiutil create -volname "$VOLNAME" -srcfolder "$STAGING" -ov -format UDRW "$TMP_DMG" >/dev/null

    # ---- 3. 挂载到 /Volumes/<VOLNAME>（Finder 可解析；mktemp 路径不行）----
    hdiutil attach "$TMP_DMG" -mountpoint "$MOUNT_PT" -nobrowse -quiet

    # ---- 4. 设置 Finder 布局（窗口尺寸 + 背景图 + 图标定位）----
    # 布局设置失败不阻断（降级为无美化的 dmg + 软链仍可用）
    # 注意：窗口/图标尺寸【始终】执行；仅「背景图」一行受 BG_SRC 有无控制。
    BG_LINE=""
    if [ -n "$BG_SRC" ]; then
      BG_LINE="set background picture of theViewOptions to file \"$BG_SRC\""
    fi
    osascript <<APPLESCRIPT >/dev/null 2>&1 || echo "提示：Finder 布局设置失败，dmg 仍可用（无美化）" >&2
tell application "Finder"
  tell disk "$VOLNAME"
    open
    set current view of container window to icon view
    set toolbar visible of container window to false
    set statusbar visible of container window to false
    set bounds of container window to {$WIN_X, $WIN_Y, $((WIN_X + WIN_W)), $((WIN_Y + WIN_H))}
    set theViewOptions to icon view options of container window
    set arrangement of theViewOptions to not arranged
    set icon size of theViewOptions to 100
    $BG_LINE
    set position of item "${APP_NAME}.app" of container window to {$ICON_APP_X, $ICON_Y}
    set position of item "Applications" of container window to {$ICON_APPS_X, $ICON_Y}
    update without registering applications
    delay 1
    close
  end tell
end tell
APPLESCRIPT

    # ---- 5. 卸载 + 压缩为只读最终 dmg ----
    sync
    hdiutil detach "$MOUNT_PT" -quiet
    rm -f "$DMG"
    hdiutil convert "$TMP_DMG" -format UDZO -o "$DMG" >/dev/null

    # ---- 6. 回读校验：挂载成品，确认布局（.DS_Store + 背景图引用）真的写进去了 ----
    POLISHED=0
    if hdiutil attach "$DMG" -readonly -nobrowse -mountpoint "$VERIFY_PT" -quiet 2>/dev/null; then
      if [ -f "$VERIFY_PT/.DS_Store" ] && grep -aq "bg.png" "$VERIFY_PT/.DS_Store"; then
        POLISHED=1
      fi
      hdiutil detach "$VERIFY_PT" -quiet 2>/dev/null || true
    else
      echo "⚠ 无法校验：成品 dmg 挂载失败（不代表无美化）。校准写法可能已随 macOS 版本变化，请检查。" >&2
    fi

    trap - EXIT
    cleanup_dmg

    if [ "$POLISHED" = "1" ]; then
      echo "✓ 已生成: ${DMG}（含 Applications 软链 + 拖拽引导，美化已生效）"
    else
      echo "⚠ 已生成: ${DMG}（含 Applications 软链；无美化——本机无 GUI 会话或 Finder 不可用）" >&2
      if [ "${STRICT_POLISH:-0}" = "1" ]; then
        echo "错误：STRICT_POLISH=1 且美化未生效，按失败处理。" >&2
        exit 1
      fi
    fi
    ;;
  linux)
    # v3.0.0 起 Linux 默认栈是 GTK4 + WebKitGTK 6.0；旧 GTK3/WebKit2GTK 4.1
    # 只剩 `-tags gtk3` 过渡通道（v3.1 移除），不再需要 webkit2_41。
    # 目标发行版必须自备 GTK4 + WebKitGTK 6.0 运行库。
    #
    # Linux 的交付物就是裸二进制（见 build/linux/Taskfile.yml 的说明），
    # 故显式请求 build 动作——平台推断在 linux 上本来就是 build，写出来是为了自我说明。
    #
    # Linux 只进 CI 编译腿、不进发布矩阵（见 .github/workflows/build.yml），
    # 运行态仍需在目标发行版上人工确认。
    WAILS_ACTION=build bash scripts/build.sh
    echo "✓ 已生成: build/bin/${APP_NAME}"
    ;;
  windows)
    if command -v makensis >/dev/null 2>&1; then
      # WAILS_ACTION=package：目标平台是 windows（默认动作是 build），
      # 但我们要的是带 NSIS 的 package 任务（它会跑 create:nsis:installer）。
      # INSTALL_SCOPE 作为 Taskfile 变量经 build.sh 原样转发。
      WAILS_ACTION=package bash scripts/build.sh GOOS=windows ARCH=amd64 INSTALL_SCOPE="$SCOPE"
      echo "✓ 已生成: build/bin/（.exe 安装器，安装范围: ${SCOPE}）"
    else
      echo "提示：未找到 makensis（NSIS 编译器），无法生成 .exe 安装器。" >&2
      echo "  安装方式：macOS → brew install makensis；Windows → 装 NSIS (nsis.sourceforge.io)" >&2
      echo "  当前降级为裸 exe（非安装器）。" >&2
      bash scripts/build.sh GOOS=windows ARCH=amd64
      echo "✓ 已生成: build/bin/（裸 .exe，非安装器）"
    fi
    ;;
esac

echo ""
echo "打包完成，产物在 build/bin/ 目录。"
