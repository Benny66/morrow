#!/usr/bin/env bash
# ==========================================================================
# gen-icon.sh — 从 build/icon/appicon.svg 生成三平台图标资产（make icon）
# --------------------------------------------------------------------------
# 为什么需要它：图标有三处消费者，格式各不相同——
#   1. build/appicon.png        1024 PNG，被 main.go 用 //go:embed 嵌入作托盘图标
#   2. build/darwin/icons.icns  macOS 坞栏 / 访达 显示的那一个
#   3. build/windows/icon.ico   Windows exe 版本资源与 NSIS 安装器
#
# 三者必须同源。不同源的症状没有报错：改完图标，Dock 里是新的，托盘还是旧的。
# 唯一真相是 SVG，本脚本负责「栅格化 + 派生」，换图标只跑它。
#
# 【为什么不挂进 make build】
# 栅格化需要一个可用的 SVG 渲染器，而构建机不一定有。把它做成 build 的前置依赖，
# 等于让「这台机器没装 librsvg」变成「整个应用构建不了」——用一个装饰性资源的缺失
# 去阻断主链路，代价不对等。故独立成 target：图标变了才跑，跑不了时构建照常。
#
# 用法：bash scripts/gen-icon.sh   （等价 make icon）
# ==========================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SVG="$ROOT/build/icon/appicon.svg"
PNG="$ROOT/build/appicon.png"
SIZE=1024

if [ ! -f "$SVG" ]; then
  echo "错误：找不到矢量源 $SVG" >&2
  exit 1
fi

# 暂存目录：Chrome 方案需要一个包装页，跑完即删。
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# ---- 栅格化：按画质排优先级，用第一个可用的 ----
# rsvg-convert（librsvg）与 cairosvg 是矢量直出，质量最好；sharp 次之；
# Chrome 是「机器上装了浏览器但没装 librsvg」时的兜底，仍是矢量渲染而非位图放大。
RENDERER=""

try_rsvg() {
  command -v rsvg-convert >/dev/null 2>&1 || return 1
  rsvg-convert -w "$SIZE" -h "$SIZE" -o "$PNG" "$SVG"
}

try_cairosvg() {
  command -v python3 >/dev/null 2>&1 || return 1
  python3 -c 'import cairosvg' >/dev/null 2>&1 || return 1
  python3 -m cairosvg "$SVG" -o "$PNG" -W "$SIZE" -H "$SIZE"
}

try_sharp() {
  command -v node >/dev/null 2>&1 || return 1
  node -e 'require("sharp")' >/dev/null 2>&1 || return 1
  node -e '
    const sharp = require("sharp");
    sharp(process.argv[1], { density: 384 })
      .resize(Number(process.argv[3]), Number(process.argv[3]), {
        fit: "contain",
        background: { r: 0, g: 0, b: 0, alpha: 0 },
      })
      .png()
      .toFile(process.argv[2])
      .catch((e) => { console.error(e.message); process.exit(1); });
  ' "$SVG" "$PNG" "$SIZE"
}

try_chrome() {
  local chrome=""
  local c
  for c in \
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
    "/Applications/Chromium.app/Contents/MacOS/Chromium" \
    "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge" \
    "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser" \
    "$(command -v google-chrome 2>/dev/null || true)" \
    "$(command -v chromium 2>/dev/null || true)" \
    "$(command -v chromium-browser 2>/dev/null || true)"; do
    if [ -n "$c" ] && [ -x "$c" ]; then chrome="$c"; break; fi
  done
  [ -n "$chrome" ] || return 1

  # 包装页：让 SVG 正好铺满 1024×1024 视口，并且零外边距。
  # 直接把 SVG 当文档截图也能出图，但 Chrome 对独立 SVG 文档的排版留白不可控，
  # 会得到「图标偏左上角、右下角空一块」的图——这类错误肉眼很难发现。
  cat > "$TMP/shot.html" <<HTML
<!doctype html><html><head><meta charset="utf-8"><style>
  html,body{margin:0;padding:0;background:transparent;overflow:hidden}
  img{display:block;width:${SIZE}px;height:${SIZE}px}
</style></head><body>
<img src="file://$SVG">
</body></html>
HTML

  # --default-background-color=00000000 是保留透明通道的关键；缺了它，
  # 四个角会被填成不透明白色，macOS 的坞栏里就是一个白框。
  "$chrome" --headless=new --disable-gpu --hide-scrollbars \
    --force-device-scale-factor=1 \
    --allow-file-access-from-files \
    --default-background-color=00000000 \
    --virtual-time-budget=3000 \
    --window-size="$SIZE,$SIZE" \
    --screenshot="$PNG" \
    "file://$TMP/shot.html" >/dev/null 2>&1 \
  || "$chrome" --headless --disable-gpu --hide-scrollbars \
    --force-device-scale-factor=1 \
    --allow-file-access-from-files \
    --default-background-color=00000000 \
    --virtual-time-budget=3000 \
    --window-size="$SIZE,$SIZE" \
    --screenshot="$PNG" \
    "file://$TMP/shot.html" >/dev/null 2>&1
}

for r in try_rsvg try_cairosvg try_sharp try_chrome; do
  rm -f "$PNG"
  if $r 2>/dev/null && [ -s "$PNG" ]; then
    RENDERER="${r#try_}"
    break
  fi
done

if [ -z "$RENDERER" ]; then
  cat >&2 <<'MSG'
错误：找不到可用的 SVG 渲染器，图标未生成。
      装任意一个即可：
        brew install librsvg        # 推荐，质量最好
        brew install cairo          # 然后 pip install cairosvg
        npm i -g sharp-cli          # 或用本机 Chrome（本脚本会自动找）
      不想装东西的话：打开 build/icon/preview.html，点「导出 appicon.png（1024）」
      存到 build/appicon.png，然后手工跑：
        cd build && wails3 generate icons -input appicon.png \
          -macfilename darwin/icons.icns -windowsfilename windows/icon.ico
MSG
  exit 1
fi
# 注意：变量后面紧跟全角标点时必须写成 ${VAR}。macOS 自带的 bash 3.2 在非 UTF-8
# locale 下会把多字节字符的首字节吞进变量名，配合 set -u 直接报 unbound variable
# （报错信息里的变量名会多出一个乱码字符，很难一眼看出是这个问题）。
echo "==> 栅格化（渲染器：${RENDERER}）→ build/appicon.png"

# ---- 校验：尺寸与透明通道 ----
# 不做这步的话，一个「渲染成了 1024×1024 白底方图」的错误会一路漂到打包产物里，
# 只在用户看 Dock 时才发现。这里能提前拦住最常见的两种翻车。
if command -v sips >/dev/null 2>&1; then
  W="$(sips -g pixelWidth "$PNG" | awk '/pixelWidth/{print $2}')"
  H="$(sips -g pixelHeight "$PNG" | awk '/pixelHeight/{print $2}')"
  if [ "$W" != "$SIZE" ] || [ "$H" != "$SIZE" ]; then
    echo "错误：产出为 ${W}x${H}，期望 ${SIZE}x${SIZE}" >&2
    exit 1
  fi
  if [ "$(sips -g hasAlpha "$PNG" | awk '/hasAlpha/{print $2}')" != "yes" ]; then
    echo "警告：产出没有透明通道——四角会被填成实色，坞栏里是个方框而不是圆角卡。" >&2
    echo "      若用的是浏览器兜底方案，检查 --default-background-color=00000000 是否生效。" >&2
  fi
  echo "  ✓ ${W}x${H}，透明通道正常"
fi

# ---- 派生 icns / ico ----
WAILS3="${WAILS3:-}"
if [ -z "$WAILS3" ]; then
  if command -v wails3 >/dev/null 2>&1; then
    WAILS3="$(command -v wails3)"
  else
    WAILS3="$(go env GOPATH 2>/dev/null || true)/bin/wails3"
  fi
fi

if [ ! -x "$WAILS3" ]; then
  cat >&2 <<MSG
错误：找不到 wails3 CLI（试过 PATH 与 \$(go env GOPATH)/bin）。
      build/appicon.png 已更新，但 icons.icns / icon.ico 仍是旧图——不要就这样打包。
      修复：go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.23
MSG
  exit 1
fi

echo "==> 派生 icons.icns / icon.ico"
(
  cd "$ROOT/build"
  "$WAILS3" generate icons \
    -input appicon.png \
    -macfilename darwin/icons.icns \
    -windowsfilename windows/icon.ico
)

echo "==> 完成。三处资产已同源。"
