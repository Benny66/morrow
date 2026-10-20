#!/usr/bin/env bash
# ==========================================================================
# init.sh — 脚手架实例化脚本
# --------------------------------------------------------------------------
# 用法：bash scripts/init.sh <name> [bundle-prefix]
#   <name>          项目名（如 myapp）
#   [bundle-prefix] macOS bundle id 前缀（如 com.mycompany），默认 com.example
# 从母版生成一个干净、可编译的新项目：
#   1. 校验 name 合法性
#   2. git clone 母版（天然排除 .git / settings.local.json / node_modules / build 产物）
#   3. 全局替换「项目名占位符」→ name（内容里含占位符的文本文件 + 名字含占位符的路径）
#   4. 替换「bundle 前缀占位符」→ bundle 前缀（build/config.yml 的 productIdentifier；
#      渲染出的 build/darwin/Info.plist 的 bundle id 由它派生）
#   5. 清空母版归档历史（openspec/changes/archive），保留 specs 能力基线
#   6. 重新 git init + 首次提交
#   7. 断言两个占位符残留均为空（内容 + 路径）+ 回读 bundle id 校验
#
# 【v2 → v3：占位符落点变了，别再退回扩展名白名单】
# 旧版按 *.go/*.mod/*.json/*.html/*.sh/*.md/*.vue/*.ts/*.plist 挑文件，
# 而 v3 把占位符放进了 build/config.yml 与 Taskfile.yml（都是 *.yml）、
# build/windows/nsis/*.nsh，以及【目录名】frontend/bindings/<go module>/。
# 白名单漏一项就是静默失效，故改成「按内容命中挑文件」+ 单独处理路径改名。
#
# 占位符一律经下方 PH_APP / PH_BUNDLE 变量使用，不写裸字面量——原因见那两行的注释。
# ==========================================================================
set -euo pipefail

NAME="${1:-}"
if [ -z "$NAME" ]; then
  echo "用法: $0 <name> [bundle-prefix]   （小写字母开头，可含数字/下划线/连字符）" >&2
  exit 1
fi

# bundle id 前缀：macOS 用它区分应用（登录项/权限授予/文件关联都按它记）。
# 默认 com.example 是 IANA 保留给示例的域名，明显是占位符——好过沿用 Wails 的
# 默认 com.wails.<name>（那等于宣称 Wails 是厂商，且与其他 Wails 项目撞名）。
BUNDLE_PREFIX="${2:-com.example}"
if ! printf '%s' "$BUNDLE_PREFIX" | grep -qE '^[a-z][a-z0-9]*(\.[a-z0-9]+)+$'; then
  echo "错误：bundle 前缀 '$BUNDLE_PREFIX' 不合法，须形如 com.mycompany（小写、点分）" >&2
  exit 1
fi

# 校验 name 合法性：小写字母开头，仅含小写字母/数字/下划线/连字符
if ! echo "$NAME" | grep -qE '^[a-z][a-z0-9_-]*$'; then
  echo "错误：项目名 '$NAME' 不合法，须匹配 ^[a-z][a-z0-9_-]*$" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="$ROOT/../$NAME"

if [ -e "$DEST" ]; then
  echo "错误：目标目录 $DEST 已存在，拒绝覆盖" >&2
  exit 1
fi

echo "==> 从母版生成项目 '$NAME' ..."

# 1. git clone 母版到目标目录（排除 .git 历史与全局 ignore 的 settings.local.json）
git clone "$ROOT" "$DEST" >/dev/null 2>&1

# 占位符拼写：必须【拆写】，不能写成完整的占位符字面量。
# 原因：本脚本自己也在替换范围内，裸写会被自己的替换改掉，断言随即失效
# （与 package.sh 的防呆同因，见其 PLACEHOLDER 注释）。
# 拆写后文件里不存在连续的占位符字面量，替换不会碰它，断言在母版与实例化副本里都成立。
# 注：此前能侥幸工作，是因为 bash 在替换发生前已把整个脚本读进内存——
# 依赖读取块大小的行为不可依赖，脚本一变大就可能读到改写后的内容。
PH_APP="__APP_""NAME__"
PH_BUNDLE="__BUNDLE_""PREFIX__"

# 2. 全局替换占位符（内容 + 路径）
#
# 【不要退回扩展名白名单】理由见文件头。这里按「内容里真的含占位符」选文件：
# grep -rIl 的 -I 会跳过二进制文件，天然覆盖任意扩展名与无扩展名文件（如 .gitignore）。
GREP_EXCLUDES=(--exclude-dir=node_modules --exclude-dir=dist --exclude-dir=.git)

# 2a. 内容替换
while IFS= read -r f; do
  # sed -i：BSD 与 GNU 的 -i 语义不同，两条都试（与 gen.sh 同一手法）
  sed -i '' "s/${PH_APP}/${NAME}/g" "$f" 2>/dev/null || sed -i "s/${PH_APP}/${NAME}/g" "$f"
  # bundle 前缀单独替换（取值来自第二个参数，与项目名无关）
  sed -i '' "s/${PH_BUNDLE}/${BUNDLE_PREFIX}/g" "$f" 2>/dev/null || sed -i "s/${PH_BUNDLE}/${BUNDLE_PREFIX}/g" "$f"
done < <(grep -rIl -e "$PH_APP" -e "$PH_BUNDLE" "$DEST" "${GREP_EXCLUDES[@]}" 2>/dev/null || true)

# 2b. 路径改名：占位符也出现在【目录名】里（frontend/bindings/<go module>/，
#     由 go.mod 的 module 名决定）。内容替换碰不到路径，而前端 import 已经被改成了
#     <name>——不改名就会出现「import 指向 bindings/<name>/，实际目录还叫
#     bindings/<占位符>/」，报错指向 import 行，很难联想到是目录没改名。
#     按路径深度倒序处理，避免先改父目录导致子路径失效。
#
#     注：frontend/bindings/ 已在 .gitignore 里，而母版是 git clone 出来的，
#     故【干净克隆里这段通常一条路径也命中】——那是正常的，不是失效。
#     真正的保证在下面的 5b（路径残留断言）：它查的是结果，不依赖这里命中几条。
while IFS= read -r p; do
  [ -e "$p" ] || continue
  base="$(basename "$p")"
  newname="$(printf '%s' "$base" | sed "s/${PH_APP}/${NAME}/g; s/${PH_BUNDLE}/${BUNDLE_PREFIX}/g")"
  [ "$newname" = "$base" ] && continue
  mv "$p" "$(dirname "$p")/$newname"
done < <(find "$DEST" \( -name "*${PH_APP}*" -o -name "*${PH_BUNDLE}*" \) \
           -not -path '*/node_modules/*' -not -path '*/.git/*' -not -path '*/dist/*' 2>/dev/null \
         | awk -F/ '{print NF"\t"$0}' | sort -rn | cut -f2-)

# 3. 清空母版归档历史，保留 specs 能力基线
rm -rf "$DEST/openspec/changes/archive"
mkdir -p "$DEST/openspec/changes/archive"

# 4. 重新 git init + 首次提交
rm -rf "$DEST/.git"
cd "$DEST"
git init -q
git add -A
git -c user.name="scaffold" -c user.email="scaffold@local" commit -q -m "初始化项目 ${NAME}（由 scaffold-init 生成）" 2>/dev/null || echo "（首次提交跳过：无提交者信息，不影响项目）"

# 5. 断言占位符残留为空（内容；-I 忽略二进制文件，避免误报裸二进制产物）
#    两个占位符都要查：漏掉 bundle 前缀会让产物 bundle id 变成「占位符.myapp」，
#    而这种错误要到 macOS 侧栏/权限授予时才显形。
for PH in "$PH_APP" "$PH_BUNDLE"; do
  RESIDUE=$(grep -rIn -- "$PH" "$DEST" "${GREP_EXCLUDES[@]}" 2>/dev/null || true)
  if [ -n "$RESIDUE" ]; then
    echo "错误：以下文件仍残留 $PH，替换遗漏：" >&2
    echo "$RESIDUE" >&2
    exit 1
  fi
done

# 5b. 断言占位符残留为空（路径）——内容干净不等于结构干净。
#     grep 只看文件内容，看不见「目录名还叫 <占位符>」。漏检的症状是前端 import
#     指向不存在的路径（报错指向 import 行），或产物以占位符命名。
PATH_RESIDUE="$(find "$DEST" \( -name "*${PH_APP}*" -o -name "*${PH_BUNDLE}*" \) \
  -not -path '*/node_modules/*' -not -path '*/.git/*' -not -path '*/dist/*' 2>/dev/null || true)"
if [ -n "$PATH_RESIDUE" ]; then
  echo "错误：以下【路径】仍含占位符，改名遗漏：" >&2
  echo "$PATH_RESIDUE" >&2
  exit 1
fi

# 6. 回读校验：bundle id 必须真的进了 Info.plist（占位符替换「没生效」比「替换错」更难发现）
#
# 【v3 的注意点】这里的 Info.plist 是【渲染产物】，它的真相是 build/config.yml。
# 本步骤查的是「替换 + 渲染」这条链真的走通了——而不是「Info.plist 里该有个字面量」。
# 换句话说：如果哪天有人在 init.sh 里直接改 Info.plist 而不改 config.yml，
# 下一次 update:build-assets 就会把它覆盖回去；故下面第 2b 步的占位符残留检查
# 覆盖 config.yml，才是这条链的源头保证。
PLIST="$DEST/build/darwin/Info.plist"
if ! grep -q "<string>${BUNDLE_PREFIX}\." "$PLIST"; then
  echo "错误：$PLIST 的 bundle id 未按 '${BUNDLE_PREFIX}.' 生成" >&2
  exit 1
fi
CONFIG_YML="$DEST/build/config.yml"
if ! grep -q "productIdentifier: \"${BUNDLE_PREFIX}\." "$CONFIG_YML"; then
  echo "错误：$CONFIG_YML 的 info.productIdentifier 未按 '${BUNDLE_PREFIX}.' 生成" >&2
  exit 1
fi

echo ""
echo "✓ 项目已生成：$DEST"
echo ""
echo "下一步："
echo "  cd $NAME"
echo "  make dev              # 启动开发态"
echo "  make gen name=asset   # 生成你的第一个模块"
echo ""
echo "注意："
echo "  · macOS bundle id 现为 ${BUNDLE_PREFIX}.${NAME}"
if [ "$BUNDLE_PREFIX" = "com.example" ]; then
  echo "    用的是默认占位前缀 com.example —— 发布前请改成你的公司域名："
  echo "    改 build/config.yml 的 info.productIdentifier，然后跑"
  echo "    wails3 task common:update:build-assets（并【不要】直接改 Info.plist，"
  echo "    它是渲染产物，会被覆盖）"
fi
echo "  · 版本号在 build/config.yml 的 info.version（v3 换掉了 wails.json 的 info.productVersion），"
echo "    发布时用 bash scripts/release.sh <version> 更新：它会写回单一真相并渲染进产物"
