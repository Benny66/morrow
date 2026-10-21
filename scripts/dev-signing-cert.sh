#!/usr/bin/env bash
# ==========================================================================
# dev-signing-cert.sh — 为本地开发构建创建一张稳定的代码签名证书
# --------------------------------------------------------------------------
# 用法：bash scripts/dev-signing-cert.sh
#
# 【为什么需要】默认的 ad-hoc 签名（codesign --sign -）会把「指定要求」退化成
# 一个裸 cdhash：
#
#     designated => cdhash H"6bc9a59220e53f158784ed78459a688bf61fdedf"
#
# 每次重新编译 cdhash 都变，macOS 的 TCC 会把新二进制当成「另一个应用」，
# 已经给过的屏幕录制/专注/日历授权会被自动撤销。症状就是：
# 系统设置里勾上了屏幕录制，重建一次又变回未勾选。
#
# 换成一张固定的自签名证书之后，指定要求变成
#
#     designated => identifier "io.github.bennywen.morrow" and certificate leaf = H"..."
#
# 重建只改 cdhash，不改证书 → 系统认得出是同一个应用 → 授权保留。
#
# 【它做了什么】用 openssl 生成一张 10 年期的 codeSigning 证书，导入登录钥匙串，
# 并把 codesign 加进该私钥的访问白名单（否则每次签名都会弹钥匙串确认框）。
#
# 【安全边界】这是【本地开发用】的自签名证书：
#   - 只在本机钥匙串，不导出、不分发；
#   - 不能让 Gatekeeper 信任你的应用，只用于让 TCC 稳定识别同一个 app；
#   - 正式分发仍然走 Apple Developer ID + notarization，和这套完全无关。
# ==========================================================================
set -euo pipefail

IDENTITY_NAME="${1:-Morrow Dev}"
KEYCHAIN="${HOME}/Library/Keychains/login.keychain-db"

step() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
info() { printf '    %s\n' "$1"; }
die()  { printf '\n\033[31m✗ %s\033[0m\n' "$1" >&2; exit 1; }

step "检查是否已存在同名证书"
# 【为什么不用 find-identity -v】-v 只列「证书链被系统信任」的身份；自签名证书
# 默认是 CSSMERR_TP_NOT_TRUSTED，会被过滤掉，但它照样能用于 codesign。
# 判断「能不能签」只看它出不出现于 code signing policy 列表。
if security find-identity -p codesigning 2>/dev/null | grep -q "\"${IDENTITY_NAME}\""; then
  info "已存在：${IDENTITY_NAME}（无需重建）"
  security find-identity -p codesigning | grep "\"${IDENTITY_NAME}\""
  exit 0
fi

step "生成自签名证书"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# 【为什么带 codeSigning 的 EKU】没有 extendedKeyUsage=codeSigning 的证书
# 不会出现在 `security find-identity -p codesigning` 里，codesign 也就找不到它。
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "$WORK/key.pem" \
  -out "$WORK/cert.pem" \
  -days 3650 \
  -subj "/CN=${IDENTITY_NAME}/O=Morrow Local Dev" \
  -addext "basicConstraints=critical,CA:false" \
  -addext "keyUsage=critical,digitalSignature" \
  -addext "extendedKeyUsage=critical,codeSigning" \
  2>/dev/null || die "openssl 生成证书失败"

openssl pkcs12 -export \
  -inkey "$WORK/key.pem" \
  -in "$WORK/cert.pem" \
  -out "$WORK/identity.p12" \
  -name "${IDENTITY_NAME}" \
  -certpbe PBE-SHA1-3DES \
  -keypbe PBE-SHA1-3DES \
  -macalg sha1 \
  -passout pass:morrowdev \
  2>/dev/null || die "打包 pkcs12 失败"

step "导入登录钥匙串"
security import "$WORK/identity.p12" \
  -k "$KEYCHAIN" \
  -P morrowdev \
  -T /usr/bin/codesign \
  -T /usr/bin/security \
  >/dev/null || die "导入钥匙串失败（可能是钥匙串被锁，请先打开「钥匙串访问」解锁）"

# 【为什么需要 set-key-partition-list】macOS 的私钥默认只有创建它的程序能访问；
# 不加这一步，每次 codesign 都会弹一个「允许访问钥匙串」的对话框。
# 该命令会要求输入登录钥匙串的密码（系统弹窗），属预期行为。
step "授权 codesign 访问私钥"
read -r -s -p "    请输入登录钥匙串密码（用于授权 codesign，不会保存）: " KC_PASS || true
echo
printf '%s' "$KC_PASS" | security set-key-partition-list \
  -S apple-tool:,apple:,codesign: \
  -s -k "$KEYCHAIN" \
  >/dev/null 2>&1 || info "授权失败或已跳过；下次签名可能弹一次钥匙串确认框（不影响结果）"
unset KC_PASS

if ! security find-identity -p codesigning 2>/dev/null | grep -q "\"${IDENTITY_NAME}\""; then
  die "证书已导入但未被识别为可用签名身份。请打开「钥匙串访问」检查 ${IDENTITY_NAME} 是否在「我的证书」下且带私钥。"
fi

printf '\n\033[32m✓ 完成\033[0m\n\n'
security find-identity -p codesigning | grep -A 1 "Matching identities" | head -5
printf '\n注意：证书显示 CSSMERR_TP_NOT_TRUSTED 是正常的（自签名、未加入信任列表），\n'
printf '      不影响 codesign 使用，也不影响 TCC 认应用身份。\n\n'
printf '接下来：\n'
printf '  1. make install          （install.sh 会自动用这张证书签名）\n'
printf '  2. 重新在系统设置里勾选一次屏幕录制\n'
printf '  3. 之后每次重建都不需要重新授权\n\n'
