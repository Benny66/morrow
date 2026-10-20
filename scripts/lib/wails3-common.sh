#!/usr/bin/env bash
# ==========================================================================
# scripts/lib/wails3-common.sh — 构建脚本共用函数（被 source，不单独执行）
# --------------------------------------------------------------------------
# 【为什么单独一个文件】下面这些规则被 scripts/build.sh 与 scripts/set-version.sh
# 共同需要。复制两份是漂移的温床（铁律 1：单一真相）——比如改了一处的 YAML
# 解析规则而漏了另一处，症状是「脚本说写好了、构建却读到旧值」。
#
# 【与 Go 护栏的关系】本文件的解析规则必须与 internal/guard/wiring_test.go 的
# parseBuildConfigInfo / plistStringValue 保持一致。它们一个用 bash+awk、一个用
# Go，是因为前者要能在没有 Go 的机器上跑、后者要能进 make test；两者断言的
# 【事实】必须相同。改了这里请同步改那边（反之亦然）。
#
# 【前置约定】所有函数都假定当前工作目录 = 项目根（调用方负责 cd）。
# ==========================================================================

# ---- wails3_cli：定位 wails3 可执行文件，找不到则报错退出 ----
# 可执行名是 wails3（v2 的 CLI 叫 wails，两者不是同一个东西）。
# Git Bash 下可执行名带 .exe，故多路兜底后仍找不到才失败。
wails3_cli() {
  local cli
  cli="$(go env GOPATH)/bin/wails3"
  if [ ! -x "$cli" ]; then
    if [ -x "${cli}.exe" ]; then
      cli="${cli}.exe"
    else
      cli="$(command -v wails3 || true)"
    fi
  fi
  if [ -z "$cli" ]; then
    echo "错误：找不到 wails3 CLI（v3 的可执行名是 wails3，不是 v2 的 wails）。" >&2
    echo "      安装：go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.23" >&2
    exit 1
  fi
  printf '%s' "$cli"
}

# ---- bc_read_info：读 build/config.yml 的 info 段的某个键 ----
# 手写行级解析而非引 YAML 库：格式由本仓自己的 config.yml 决定，解析不到立即报错。
# 【必须限定在 info: 段内】：文件顶层还有一个 `version: '3'`（Taskfile 格式版本），
# 不限定段就会先撞上它，把 '3' 当成版本号。
# 取值去掉 YAML 引号与空白（`version: "1.0.0"` → `1.0.0`）。
bc_read_info() {
  awk -F: -v key="$1" '
    /^[^ \t]/ { in_info = ($1 == "info"); next }
    in_info {
      k = $1
      gsub(/^[ \t]+/, "", k)
      if (k == key) { print $2; exit }
    }
  ' build/config.yml | tr -d '\042\047' | tr -d '[:space:]'
}

# ---- bc_render_assets：把 build/config.yml 的 info 段渲染进构建资源 ----
# 【这是 v3 必须显式做的一步，v2 没有】v2 的 `wails build` 会自动把 wails.json
# 的 info 段写进 Info.plist；v3 必须跑 `wails3 task common:update:build-assets`。
# 而且 v3 各平台任务里【只有 package 挂了这一步】—— bare `wails3 build` 不渲染，
# 偏偏 Windows 的 windows:build 里的 generate:syso 要读 build/windows/info.json
# 生成 exe 版本资源。故凡是要构建的路径，都必须先跑这里。
# 漏掉的症状：应用内显示新版本、系统「显示简介」显示旧版本——不报任何错。
bc_render_assets() {
  local cli log
  cli="$(wails3_cli)"
  log="$(mktemp)"
  if ! "$cli" task common:update:build-assets >"$log" 2>&1; then
    echo "错误：渲染构建资源失败（wails3 task common:update:build-assets）：" >&2
    cat "$log" >&2
    rm -f "$log"
    exit 1
  fi
  rm -f "$log"
  echo "  ✓ 构建资源已按 build/config.yml 渲染"
}

# ---- bc_version：读并校验版本号（单一真相），不合法即报错退出 ----
bc_version() {
  local v
  v="$(bc_read_info version)"
  if [ -z "$v" ]; then
    echo "错误：build/config.yml 的 info 段未声明 version —— 版本号单一真相处缺失" >&2
    exit 1
  fi
  # NSIS 的 VIProductVersion 形如 "${INFO_PRODUCTVERSION}.0"，不接受非数字版本；
  # 非数字（如 git describe 的 abc1234）会让 Windows 打包在最后一步失败。
  if ! printf '%s' "$v" | grep -qE '^[0-9]+(\.[0-9]+){2,3}$'; then
    echo "错误：版本 '$v' 不是数字点分格式（如 0.1.0）" >&2
    echo "      NSIS 的 VIProductVersion 不接受非数字版本，Windows 打包会失败：" >&2
    echo "      build/windows/nsis/project.nsi 的 VIProductVersion \"\${INFO_PRODUCTVERSION}.0\"" >&2
    exit 1
  fi
  printf '%s' "$v"
}

# ---- plist_string：取 plist 中某个 key 的 <string> 取值 ----
# 用法：plist_string <plist 路径> <key>
# 手写解析而非 plutil：plutil 只存在于 macOS，而构建脚本要在三平台跑。
plist_string() {
  awk -v key="$2" '
    index($0, "<key>" key "</key>") { want = 1; next }
    want && /<string>/ {
      s = $0
      sub(/.*<string>/, "", s)
      sub(/<\/string>.*/, "", s)
      gsub(/^[ \t]+|[ \t]+$/, "", s)
      print s
      exit
    }
  ' "$1"
}
