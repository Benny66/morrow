#!/usr/bin/env bash
# ==========================================================================
# verify-gen.sh — 生成器端到端验证
# --------------------------------------------------------------------------
# 目的：把「make gen 能不能产出可编译的模块」变成会失败的检查。
#
# 为什么需要它：gen.sh 是管道的入口，但本仓长期从未真跑过一次——
# 实测曾发现三个真实 bug：
#   1. 绑定片段用了 page.Request，但 gen.sh 只注入 model import → 缺 import 编译失败。
#   2. app.go import 注入非幂等 → 生成第 2 个模块即重复 import（redeclared）编译失败。
#   3. import 追加在 import 块尾部 → 不满足 gofmt 字母序 → make gen 后 make lint 必红，
#      且报错指向 app.go，看不出是生成器的锅。
# 三者都不是靠读代码能可靠发现的，必须真跑。
#
# 流程（全程在临时副本里，不污染工作区；清理由 trap 在任何退出路径上完成）：
#   1. 把当前工作区复制到临时目录（替换占位符，母版也能跑）
#   2. 连续生成两个模块（第 2 个专测幂等）+ 断言 import 幂等
#   3. wails3 build 生成 bindings（新绑定方法需要它，否则护栏会报绑定过期）
#   4. go build 断言编译通过
#   5. gofmt 断言格式合规（gofmt 范围是【整个工作区】，不限于生成物）
#   6. go test 断言护栏通过（含前端镜像一致性 + Taskfile/占位符一致性）
#
# 用法：bash scripts/verify-gen.sh    （或 make verify-gen）
# ==========================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SANDBOX="$(mktemp -d)/verifygen"
PH_APP="__APP_""NAME__"
cleanup() { rm -rf "$(dirname "$SANDBOX")" 2>/dev/null || true; }
trap cleanup EXIT

echo "==> 1/6 复制工作区到临时沙箱 ..."
mkdir -p "$(dirname "$SANDBOX")"
# 排除 .git（无必要且大）与 build 产物（编译会重建）
rsync -a --exclude '.git' --exclude 'build/bin' "$ROOT/" "$SANDBOX/" 2>/dev/null

cd "$SANDBOX"

# 母版含占位符，gen.sh/编译都不认；实例化为一个合法项目名。
# 非母版（已实例化）时无占位符，此步等价 no-op。
#
# 本步骤是 scripts/init.sh 的忠实预演，且【不维护文件类型白名单】：
# v3 把占位符的落点扩到了 *.yml（build/config.yml 的 productName、Taskfile.yml 的
# APP_NAME/MODULE）与 build/windows/nsis/*.nsh，白名单一旦漏项就是静默失效。
# 故改为「内容里真的含占位符的文本文件」——用 grep -rIl 选文件，天然覆盖任意扩展名。
GREP_EXCLUDES=(--exclude-dir=node_modules --exclude-dir=dist --exclude-dir=.git)

# 1a. 内容替换
while IFS= read -r f; do
  sed -i '' "s/${PH_APP}/verifygen/g" "$f" 2>/dev/null || sed -i "s/${PH_APP}/verifygen/g" "$f"
done < <(grep -rIl -- "$PH_APP" "$SANDBOX" "${GREP_EXCLUDES[@]}" 2>/dev/null || true)

# 1b. 路径改名：占位符也出现在【目录名】里（frontend/bindings/<go module>/ 由
#     go.mod 的 module 名决定）。内容替换碰不到路径，漏了会让前端的 import
#     指向不存在的目录，报错却指向 import 行而非「目录没改名」。
#     按深度倒序改，避免先改父目录导致子路径失效。
while IFS= read -r p; do
  [ -e "$p" ] || continue
  base="$(basename "$p")"
  newname="$(printf '%s' "$base" | sed "s/${PH_APP}/verifygen/g")"
  [ "$newname" = "$base" ] && continue
  mv "$p" "$(dirname "$p")/$newname"
done < <(find "$SANDBOX" -name "*${PH_APP}*" \
           -not -path '*/node_modules/*' -not -path '*/.git/*' -not -path '*/dist/*' 2>/dev/null \
         | awk -F/ '{print NF"\t"$0}' | sort -rn | cut -f2-)

echo "==> 2/6 连续生成两个模块（第 2 个专测幂等）..."
bash scripts/gen.sh asset  >/dev/null
bash scripts/gen.sh widget >/dev/null

# 断言：import 幂等——每个 import 只应出现一次
for imp in 'internal/model' 'internal/page'; do
  n=$(grep -c "verifygen/$imp\"" app.go || true)
  if [ "$n" -ne 1 ]; then
    echo "错误：import $imp 在 app.go 出现 $n 次（期望 1）——gen.sh 的 import 注入非幂等" >&2
    exit 1
  fi
done
echo "  ✓ import 幂等"

echo "==> 3/6 生成 wails3 bindings（新绑定方法需要它，否则护栏会报绑定过期）..."
# 直接调 wails3 而非 scripts/build.sh：本步只关心「生成器 → 绑定 → 编译」这条链，
# 不需要 build.sh 的版本注入与构建资源渲染（那两件事假不了本步的结论，只是更慢）。
#
# 【输出必须留到失败时打印，不能丢进 /dev/null】这一步失败的原因几乎总是具体的：
# 生成的 .vue 里 import 路径过时、Go 侧签名与 page 契约不匹配……
# 实测因为把输出重定向到 /dev/null，一次真实的 TS2307（模板仍 import v2 的
# frontend/wailsjs/go/main/App）被压成一句「生成物或绑定生成有问题」，
# 排查方向被误导到 bindings 生成，而真因在 _example 模板里。
BUILD_LOG="$(mktemp)"
if ! "$(go env GOPATH)/bin/wails3" build >"$BUILD_LOG" 2>&1; then
  echo "错误：wails3 build 失败——生成物或绑定生成有问题。原始输出末 40 行：" >&2
  tail -40 "$BUILD_LOG" >&2
  rm -f "$BUILD_LOG"
  exit 1
fi
rm -f "$BUILD_LOG"
echo "  ✓ bindings 生成"

echo "==> 4/6 断言生成物可编译 ..."
if ! go build ./... 2>&1; then
  echo "错误：生成物编译失败——gen.sh 产出的代码有问题" >&2
  exit 1
fi
echo "  ✓ 编译通过"

echo "==> 5/6 断言格式合规（对齐 make lint 的 gofmt 检查）..."
# 曾经的 bug：锚点注入把 import 追加到块尾部 → 顺序不符 gofmt → make gen 后
# make lint 必红，且报错指向 app.go，很难联想到是生成器的锅。
# 范围是整个工作区（与 make lint 一致），故列出文件也未必是生成器造的
# ——底下的提示把两种可能都说清楚，避免误导排查方向。
unformatted="$(gofmt -l . 2>/dev/null | grep -v node_modules || true)"
if [ -n "$unformatted" ]; then
  echo "错误：以下文件未通过 gofmt（生成物或工作区原有文件），make lint 会红：" >&2
  echo "$unformatted" >&2
  echo "若为生成器注入所致，请修 scripts/gen.sh；否则运行 gofmt -w 修复。" >&2
  exit 1
fi
echo "  ✓ 格式合规"

echo "==> 6/6 断言护栏通过 ..."
# 这一步【同时】是「占位符替换清单是否完整」的验证入口：
# 护栏里有「Taskfile.yml 的 MODULE 必须等于 go.mod 的 module」与
# 「Taskfile.yml 的 APP_NAME 必须等于 build/config.yml 的 productName」两条，
# 而第 1 步的替换若漏了 *.yml，两者就会对不上 → 这里必然变红。
# 故不需要单独为「init.sh 的替换清单」写测试：跑通本脚本即证明覆盖完整。
if ! go test ./internal/guard/ 2>&1; then
  echo "错误：生成物未通过契约护栏" >&2
  exit 1
fi
echo "  ✓ 护栏通过"

echo ""
echo "生成器端到端验证通过 ✓"
