#!/usr/bin/env bash
# ==========================================================================
# smoke.sh — 冒烟测试
# --------------------------------------------------------------------------
# 目标：把"能跑"变成可观察事实，而非嘴上说。
# 流程：构建 → 启动二进制 → 断言进程存活 + 运行时产物落盘 + 版本已到达应用 → trap 清理。
#
# 说明：Wails 无 headless 模式，冒烟定位为本地验证（CI 只跑编译不启动 GUI）。
#   - 绑定握手（调一个 Go 方法验证往返）需在 GUI 环境，故这里用"进程存活 +
#     数据库/config 落盘"作为 startup 链路成功的客观证据。
#   - 第 5 步断言 app.log 里的版本号：这是「-ldflags 注入链路真的通了」的
#     运行时证据。注入断了的话应用内版本会【静默】退回 "dev"
#     （链接器对不存在的 -X 符号静默忽略），只在这里能抓住。
#
# 【v3 差异】构建经 scripts/build.sh 而非裸 wails3：
#   冒烟要验的是【真实交付路径】产出的东西，而版本注入与构建资源渲染都在
#   build.sh 里（裸 `wails3 build` 既不注入版本、也不渲染 Info.plist）。
#   另外 macOS 的可运行产物是 .app，由 build.sh 走 `wails3 package` 产出
#   （v3 的 bare `wails3 build` 只出裸 Mach-O 二进制）。
# ==========================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# shellcheck source=lib/wails3-common.sh
. "$ROOT/scripts/lib/wails3-common.sh"

APP_NAME="$(bc_read_info productName)"
if [ -z "$APP_NAME" ]; then
  echo "错误：无法从 build/config.yml 的 info 段读取 productName" >&2
  exit 1
fi
VERSION="$(bc_version)"

# 平台相关的二进制路径与数据目录。
# 数据目录的规则与本仓 internal/appdir 一致：<os.UserConfigDir()>/<appName>
# （Windows: %AppData%，macOS: ~/Library/Application Support，Linux: ~/.config）。
BIN=""
case "$(uname -s)" in
  Darwin)
    BIN="build/bin/${APP_NAME}.app/Contents/MacOS/${APP_NAME}"
    DATA_DIR="$HOME/Library/Application Support/${APP_NAME}"
    ;;
  Linux)
    BIN="build/bin/${APP_NAME}"
    DATA_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/${APP_NAME}"
    ;;
  MINGW*|MSYS*|CYGWIN*)
    BIN="build/bin/${APP_NAME}.exe"
    DATA_DIR="$APPDATA/${APP_NAME}"
    ;;
  *)
    echo "不支持的平台: $(uname -s)" >&2
    exit 1
    ;;
esac

cleanup() {
  if [ -n "${APP_PID:-}" ] && kill -0 "$APP_PID" 2>/dev/null; then
    kill "$APP_PID" 2>/dev/null || true
    sleep 1
    kill -9 "$APP_PID" 2>/dev/null || true
  fi
  echo "已清理残留进程"
}
trap cleanup EXIT

# ---- 1. 构建（经 build.sh，含版本注入与构建资源渲染）----
echo "==> 构建 ${APP_NAME} ${VERSION} ..."
bash scripts/build.sh >/dev/null

if [ ! -f "$BIN" ]; then
  echo "错误：构建产物不存在 $BIN" >&2
  exit 1
fi

# ---- 2. 启动 ----
echo "==> 启动二进制 ..."
SMOKE_LOG="$(mktemp)"
"$BIN" > "$SMOKE_LOG" 2>&1 &
APP_PID=$!

# ---- 3. 断言进程存活 ----
sleep 4
if ! kill -0 "$APP_PID" 2>/dev/null; then
  echo "错误：进程启动后立即退出，日志如下：" >&2
  cat "$SMOKE_LOG" >&2
  rm -f "$SMOKE_LOG"
  exit 1
fi
echo "✓ 进程存活 (pid $APP_PID)"

# ---- 4. 断言运行时产物落盘（startup 链路成功的证据） ----
sleep 1
if [ ! -f "$DATA_DIR/config.json" ]; then
  echo "错误：config.json 未落盘，startup 链路可能未走通" >&2
  exit 1
fi
echo "✓ config.json 已落盘"

if [ ! -f "$DATA_DIR/${APP_NAME}.db" ]; then
  echo "错误：数据库文件未落盘" >&2
  exit 1
fi
echo "✓ 数据库文件已落盘"

# ---- 5. 断言注入的版本号真的到达了应用（app.log 首行）----
# logging.Init 会把版本写进启动首行（见 internal/logging/logging.go）。
# 这一条的失效形态是【完全静默】的：链接器的 -X 对不存在的符号不报错，
# 版本只是悄悄退回 "dev"——「系统显示新版本、应用内是 dev」正是本仓
# 曾经的长期状态（见 scripts/build.sh 头部）。
APP_LOG="$DATA_DIR/app.log"
if [ ! -f "$APP_LOG" ]; then
  echo "错误：app.log 未落盘" >&2
  exit 1
fi
if ! grep -aq "version=${VERSION}" "$APP_LOG"; then
  echo "错误：app.log 中未出现 version=${VERSION} —— 版本注入未到达应用。" >&2
  echo "      实际首行如下（若为 version=dev 则说明 -ldflags 注入链路断了）：" >&2
  head -1 "$APP_LOG" >&2
  exit 1
fi
echo "✓ 应用内版本 = ${VERSION}（app.log 已确认）"

# ---- 6. 断言 MCP 能力没有被编进默认产物 ----
# 护栏 17（TestMCPNotCompiledIntoDefaultBuild）管的是【机制层】：默认标签下 wails 的
# application 包里不含 mcp_enabled.go。这一条管【产物层】：真的构建出来的二进制里
# 不能有 MCP 的痕迹。两层都要，因为 MCP 打开的是一个能 eval 页面 JS、调用任意绑定
# 方法的本机端口，而它开着时【界面上没有任何提示】——属于没有症状的失效。
#
# 【为什么用 strings 而不是 go tool nm】生产构建带 -ldflags="-w -s"，Go 符号表已被
# 剥离（实测 `go tool nm <产物> | grep -c 'main\.main'` = 0），nm 只剩链接进来的 C 库
# 符号。字符串常量不受剥离影响。
#
# 【为什么必须用完整的 WAILS_MCP_* 名字，不能用 "mcp" 子串】实测在该产物上松散匹配
# "mcp" 会命中 xdmcp、asf-rmcp 与内嵌的 base64 图片数据；nm 输出里的 _memcpy 也含
# "mcp"。用子串只会得到一堆假阳性。
#
# 【为什么这些名字是可靠判据】它们只在 mcp_enabled.go 里作为字符串常量存在
# （`os.Getenv(mcpHostEnvVar)`，且常量值就是这些字面量），未启用时不会进产物。
# application.go 里虽然也出现这几个名字，但那处只是注释，不参与编译。
MCP_MARKERS="WAILS_MCP_HOST WAILS_MCP_TOKEN WAILS_MCP_PORT WAILS_MCP_TIMEOUT WAILS_MCP_HIDE_CURSOR"
MCP_MARKER_RE="$(echo "$MCP_MARKERS" | tr ' ' '|')"

if ! command -v strings >/dev/null 2>&1; then
  echo "错误：找不到 strings 命令，本步无法执行（不在缺失时静默通过）" >&2
  exit 1
fi

# 判据的正控：这些名字必须在 mcp_enabled.go 里真实存在。缺了它，本步就只是在查一个
# 早就改名的东西——那种断言永远为真，等于没有。
WAILS_MOD_DIR="$(go list -m -f '{{.Dir}}' github.com/wailsapp/wails/v3)"
MCP_SRC="$WAILS_MOD_DIR/pkg/application/mcp_enabled.go"
if [ ! -f "$MCP_SRC" ]; then
  echo "错误：找不到 $MCP_SRC —— 判据来源已变，请同步更新 smoke.sh 第 6 步" >&2
  exit 1
fi
# 用数组而非 `for m in $MCP_MARKERS`：后者依赖 shell 的单词切分，
# 换个 shell（zsh 默认不切分）就会把整串当成一个词，静默漏检。
read -r -a _mcp_marker_arr <<< "$MCP_MARKERS"
for m in "${_mcp_marker_arr[@]}"; do
  if ! grep -q "\"$m\"" "$MCP_SRC"; then
    echo "错误：$m 已不是 mcp_enabled.go 里的字符串常量 —— 判据失效，请同步更新 smoke.sh 第 6 步" >&2
    exit 1
  fi
done

# 把 strings 的输出落盘一次，而不是每次 `strings | grep`：
#   1. 快 —— 产物 15MB 量级，这步要查两遍（正控 + MCP）；
#   2. 必须 —— BSD 的 strings 在管道被提前关闭（grep -q 命中即退出）时会往 stderr 喷
#      「failed to flush output」。落盘后 grep 读文件，不会出现这个噪音。
STRINGS_DUMP="$(mktemp)"

# 扫描方法本身的正控：这个二进制必须能被 strings 读出已知内容（注入的版本号）。
# 否则「没找到 MCP」只是因为我们压根没读到东西。
strings "$BIN" > "$STRINGS_DUMP"
if ! grep -qF "$VERSION" "$STRINGS_DUMP"; then
  echo "错误：strings 未能从 $BIN 中读出注入的版本号 ${VERSION} ——" >&2
  echo "      扫描方法本身失效，本步的「不含 MCP」结论不可信。" >&2
  exit 1
fi

MCP_HITS="$(grep -cE "$MCP_MARKER_RE" "$STRINGS_DUMP" || true)"
if [ "$MCP_HITS" != "0" ]; then
  echo "错误：产物里出现 MCP 相关常量（$MCP_HITS 处）—— MCP 能力被编进了默认构建。" >&2
  # 用 grep -o 只打印命中的标记名：strings 的行可以长到几十 KB（里面裹着 base64 与
  # 各种常量串），整行输出会把真正的信息冲掉。
  echo "      命中的标记：" >&2
  grep -oE "$MCP_MARKER_RE" "$STRINGS_DUMP" | sort | uniq -c >&2
  echo "      MCP 会开一个能 eval 页面 JS、调用任意绑定方法的本机端口，" >&2
  echo "      绝不能进默认构建或任何发布路径。唯一受认可的启用方式是 WAILS_MCP=1。" >&2
  exit 1
fi
rm -f "$STRINGS_DUMP"
echo "✓ 产物不含 MCP（判据来源与扫描方法均已正控）"

rm -f "$SMOKE_LOG"
echo ""
echo "冒烟测试通过 ✓"
