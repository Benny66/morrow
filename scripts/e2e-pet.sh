#!/usr/bin/env bash
# 宠物页 E2E：起 vite dev → 用 Playwright 打开 /#/pet → 跑断言与截图 → 清理。
#
# 【为什么需要它】宠物这块的坑几乎都不是逻辑错误，而是"渲染出来才看得见"的：
#   - `.pet` 是 overflow:hidden，菜单/气泡越界会被直接裁掉且不报错；
#   - 可点元素忘了 --wails-draggable: no-drag，click 会被拖拽通道吞掉；
#   - 窗口只有 240×380，任何多出来的一行文字都会把下面的东西挤出去。
# 这些用 go test / vue-tsc 都测不到。本脚本用真实浏览器把它们钉住。
#
# 【它测不到什么】系统原生右键菜单、wails 拖拽命中测试、绑定动作的真实效果
# ——那三件只能在真机（make dev / 打包产物）里确认，见 tasks.md 的手测项。
#
# 前置依赖（不在 deps.yaml 里，因为它不是构建依赖，只是本地验收工具）：
#   python3 -m pip install playwright
#   python3 -m playwright install chromium     # 装了系统 Chrome 可跳过
#
# 用法：
#   make e2e-pet
#   PET_E2E_PORT=5300 make e2e-pet     # 端口被占时换一个
#   PET_E2E_HEADED=1 make e2e-pet      # 看着它跑（调试用）

# 【编码坑：变量后面紧跟中文标点必须用 ${VAR} 花括号】
# macOS 自带的 bash 是 3.2，它会把全角标点的第一个字节吃进变量名：写
#   echo "（$BASE_URL）"
# 在 set -u 下会报成 `BASE_URL\xef: unbound variable`，看着像变量没定义，
# 实际是变量名被污染了。加花括号即可；本脚本里所有紧挨中文标点的展开都用 ${}。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${PET_E2E_PORT:-5199}"
OUT_DIR="$ROOT/build/e2e-pet"
BASE_URL="http://127.0.0.1:$PORT"

fail() {
  echo "错误：$*" >&2
  exit 1
}

# ---- 前置检查：缺什么就说清楚怎么补，不要跑到一半才报一堆栈 ----

command -v python3 >/dev/null 2>&1 || fail "找不到 python3。装一个，或把它加进 PATH。"
python3 -c "import playwright" >/dev/null 2>&1 || fail "python3 里没有 playwright。
      修复：python3 -m pip install playwright && python3 -m playwright install chromium
      （已经装了系统 Chrome 的话，第二条可以跳过：脚本会优先用 Chrome。）"
test -d "$ROOT/frontend/node_modules" || fail "frontend/node_modules 不存在。
      修复：cd frontend && npm install"

# 【为什么直接调 node_modules/.bin/vite，而不是 npx vite】
# 本机装了 pyenv，`npx` / `node` 都先经过它的 shims。shims 每次调用可能触发 rehash，
# 而 rehash 要抢 `/Users/<user>/.pyenv/shims/.pyenv-shim` 的锁；并发一多就会看到
#   pyenv: cannot rehash: couldn't acquire lock ... for 60 seconds
# 于是 vite 卡在启动前、日志一个字都没有，最后表现为"等待 dev server 就绪超时"。
# 直接指向项目内的可执行文件可以完全绕开 shim 链，启动也从 ~3s 降到 ~1s。
VITE_BIN="$ROOT/frontend/node_modules/.bin/vite"
[ -x "$VITE_BIN" ] || fail "$VITE_BIN 不存在或不可执行。
      修复：cd frontend && npm install"

mkdir -p "$OUT_DIR"

# ---- 起 dev server ----
#
# 【为什么是 vite 而不是 wails3 dev】本脚本只验渲染层：vite 起得快、无窗口、可无头。
# 代价是绑定调用全部失败——但宠物页对此是设计过的（initTheme 有 catch、meta.bare 跳过引导），
# 挂载不受影响；失败只会体现在数据上（宠物停在默认形象/0 档）。

echo "启动 vite dev（${BASE_URL}）…"
cd "$ROOT/frontend"
"$VITE_BIN" --port "$PORT" --strictPort >"$OUT_DIR/vite.log" 2>&1 &
VITE_PID=$!

cleanup() {
  if kill -0 "$VITE_PID" >/dev/null 2>&1; then
    kill "$VITE_PID" >/dev/null 2>&1 || true
    wait "$VITE_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# 就绪探测：最多等 30 秒。--noproxy 是必须的——开发机若配了 HTTP_PROXY，
# 请求 localhost 也会被代理接管，表现为 502 而服务其实好好的。
ready=0
for _ in $(seq 1 100); do
  if curl -s --noproxy '*' -o /dev/null --max-time 1 "$BASE_URL/"; then
    ready=1
    break
  fi
  if ! kill -0 "$VITE_PID" >/dev/null 2>&1; then
    echo "vite 已退出，日志：" >&2
    tail -20 "$OUT_DIR/vite.log" >&2
    fail "dev server 启动失败（端口 $PORT 可能被占用；换一个：PET_E2E_PORT=5300 make e2e-pet）"
  fi
  sleep 0.3
done
[ "$ready" = 1 ] || { tail -20 "$OUT_DIR/vite.log" >&2; fail "等待 dev server 就绪超时（${BASE_URL}）"; }

# ---- 跑用例 ----

echo
echo "== 镜像时序断言 =="
python3 "$ROOT/scripts/e2e/mirror_logic.py" --base-url "$BASE_URL"

echo
echo "== 菜单交互断言 =="
python3 "$ROOT/scripts/e2e/pet_menu.py" --base-url "$BASE_URL" --out-dir "$OUT_DIR"

echo
echo "== 各状态截图 =="
python3 "$ROOT/scripts/e2e/pet_states.py" --base-url "$BASE_URL" --out-dir "$OUT_DIR"

echo
echo "通过。截图与 dev server 日志在：${OUT_DIR}"
