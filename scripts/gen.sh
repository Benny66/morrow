#!/usr/bin/env bash
# ==========================================================================
# gen.sh — 新模块生成器
# --------------------------------------------------------------------------
# 用法：make gen name=<module>   （如 name=asset）
# 从 _example/ 模板生成四段：model + service + 绑定方法 + 前端页面，
# 并用锚点注入到 AllModels() / app.go / 路由 / 菜单。
#
# 原则（详见 openspec/changes/example-module/design.md）：
#   - fail-fast：动任何文件前，先校验锚点存在 + 目标未被占用。
#   - 幂等：目标文件已存在则报错退出，绝不覆盖业务代码。
#   - 占位符替换：Example/example → 目标模块的 PascalCase/snake_case。
#   - 注入统一用 awk 读临时文件（不依赖 sed -i / sed 多行，跨 macOS/GNU）。
# ==========================================================================
set -euo pipefail

NAME="${1:-}"
if [ -z "$NAME" ]; then
  echo "用法: $0 <module-name>   （小写单数，如 asset）" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PASCAL="$(echo "$NAME" | awk '{print toupper(substr($0,1,1)) substr($0,2)}')"
SNAKE="$NAME"

# ---- fail-fast 前置校验：锚点必须存在 ----
ANCHORS=(
  "internal/model/model.go|// gen:model"
  "app.go|// gen:import"
  "app.go|// gen:bind"
  "frontend/src/router/index.ts|// gen:route"
  "frontend/src/layouts/AppShell.vue|// gen:menu"
)
for entry in "${ANCHORS[@]}"; do
  file="${entry%%|*}"
  marker="${entry#*|}"
  if ! grep -qF "$marker" "$file" 2>/dev/null; then
    echo "错误：锚点 '$marker' 在 $file 中不存在——写法可能已变更，请同步更新生成器" >&2
    exit 1
  fi
done

# ---- fail-fast 前置校验：目标不得已存在（幂等） ----
TARGETS=(
  "internal/model/${SNAKE}.go"
  "internal/service/${SNAKE}_service.go"
  "frontend/src/views/${SNAKE}/${PASCAL}List.vue"
)
for t in "${TARGETS[@]}"; do
  if [ -e "$t" ]; then
    echo "错误：目标文件 $t 已存在，拒绝覆盖业务代码" >&2
    exit 1
  fi
done

# ---- 工具函数 ----
# 占位符替换：读 stdin，替换 Example/example，写 stdout
#
# 【注意范围：它会替换模板里的【每一处】Example/example，注释也不例外】
# 故 _example/ 下的模板注释不要写这两类字面量：
#   - 写了模板目录名，生成物里的注释会指向一个不存在的目录（实测坑：注释里写
#     「本文件在 _example/ 下」，生成出来变成「本文件在 _asset/ 下」）；
#   - init.sh 对模块名占位符的替换同理，会把注释改成语义错误的句子。
# 一句话：模板注释里只描述规则，不要引用会被改写的名字。
replace() {
  sed -e "s/Example/${PASCAL}/g" -e "s/example/${SNAKE}/g"
}

# 在目标文件的锚点行之前，插入内容文件的所有行（awk 读文件，POSIX 可靠）
insert_before_anchor() {
  local target="$1" marker="$2" content_file="$3"
  awk '{
    if (index($0, marker) > 0) {
      while ((getline line < CONTENT) > 0) print line
      close(CONTENT)
    }
    print
  }' CONTENT="$content_file" marker="$marker" "$target" > "$target.tmp" && mv "$target.tmp" "$target"
}

# 幂等插入：目标文件已含该行则跳过（比较时忽略首尾空白）。
# 用途：import 注入。每次 make gen 都注入同样的 import，
# 非幂等会让第 2 个模块起产生重复 import（编译报 redeclared）。
# 注意：用 substr 剥离首尾 Tab/空格，不用 gsub 的交替正则——
# macOS BSD awk 对 `a|b` 形式的 gsub 模式处理与 GNU awk 不一致。
insert_line_if_absent() {
  local target="$1" marker="$2" line="$3"
  if awk -v want="$line" '
    function trim(s) {
      sub(/^[ \t]+/, "", s)
      sub(/[ \t]+$/, "", s)
      return s
    }
    { if (trim($0) == trim(want)) found = 1 }
    END { exit !found }
  ' "$target"; then
    return 0   # 已存在，跳过
  fi
  local tmp
  tmp=$(mktemp)
  printf '%s\n' "$line" > "$tmp"
  insert_before_anchor "$target" "$marker" "$tmp"
  rm -f "$tmp"
}

# ---- 生成文件（从 _example 模板复制 + 占位符替换） ----
replace < "_example/model/example.go" > "internal/model/${SNAKE}.go"
replace < "_example/service/example_service.go" > "internal/service/${SNAKE}_service.go"
mkdir -p "frontend/src/views/${SNAKE}"
replace < "_example/frontend/ExampleList.vue" > "frontend/src/views/${SNAKE}/${PASCAL}List.vue"

# ---- 锚点注入（内容先写临时文件，再 awk 插入） ----

# 0. app.go import：在 `// gen:import` 前幂等插入 internal/model + internal/page。
# 绑定方法签名用了 page.Request / page.Result（分页契约），故两个 import 都要有。
# 幂等：重复生成模块不会产生重复 import（否则编译报 redeclared）。
insert_line_if_absent "app.go" "// gen:import" '	"morrow/internal/model"'
insert_line_if_absent "app.go" "// gen:import" '	"morrow/internal/page"'

# 1. AllModels() 注册模型
TMP=$(mktemp)
printf '\t\t&%s{},\n' "$PASCAL" > "$TMP"
insert_before_anchor "internal/model/model.go" "// gen:model" "$TMP"
rm -f "$TMP"

# 2. app.go 绑定方法
TMP=$(mktemp)
replace < "_example/bindings/app_example.go.txt" > "$TMP"
insert_before_anchor "app.go" "// gen:bind" "$TMP"
rm -f "$TMP"

# 3. 路由
TMP=$(mktemp)
cat > "$TMP" <<EOF
  {
    path: '/${SNAKE}',
    name: '${SNAKE}',
    component: () => import('../views/${SNAKE}/${PASCAL}List.vue'),
    meta: { title: '${PASCAL}' },
  },
EOF
insert_before_anchor "frontend/src/router/index.ts" "// gen:route" "$TMP"
rm -f "$TMP"

# 4. 菜单
TMP=$(mktemp)
cat > "$TMP" <<EOF
  { path: '/${SNAKE}', label: '${PASCAL}', icon: '◇' },
EOF
insert_before_anchor "frontend/src/layouts/AppShell.vue" "// gen:menu" "$TMP"
rm -f "$TMP"

# ---- 生成物格式化 ----
# 锚点注入把 import 追加到 import 块【尾部】，天然不满足 gofmt 的字母序
# （model/page 会排在 service/tray 之后）——不修的话 `make gen` 之后 `make lint`
# 必然变红，且报错指向 app.go 而非生成器，很难联想到是 gen.sh 的锅。
# 生成器对自己产出做格式化是常规做法，gofmt 也是 Go 项目的既有前提。
if command -v gofmt >/dev/null 2>&1; then
  gofmt -w app.go internal/model/model.go "internal/model/${SNAKE}.go" \
    "internal/service/${SNAKE}_service.go"
else
  echo "警告：未找到 gofmt，生成物的 import 顺序可能不合规（make lint 会报 app.go）" >&2
fi

echo "已生成模块 ${NAME}（PascalCase: ${PASCAL}）"
echo "  - model:        internal/model/${SNAKE}.go"
echo "  - service:      internal/service/${SNAKE}_service.go"
echo "  - 前端页面:      frontend/src/views/${SNAKE}/${PASCAL}List.vue"
echo "  - 已注入:       AllModels() / app.go 绑定 / 路由 / 菜单"
echo "下一步：填 TODO 处业务逻辑，然后 make dev 重新生成 bindings。"
