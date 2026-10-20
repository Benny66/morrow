#!/usr/bin/env bash
# ==========================================================================
# compose-dmg-bg.sh — dmg 背景图构建期合成
# --------------------------------------------------------------------------
# 用法：bash scripts/compose-dmg-bg.sh <out.png> <app_name> <arrow_x> <arrow_y> [w] [h]
#
# 从模板 build/darwin/dmg-background.tpl.png 合成成品背景图：
#   - 以模板为底（含基调色 + 淡「拖拽到此」提示）
#   - 叠加应用名（模板文字区，x = arrow_x）
#   - 叠加拖拽箭头（尖端正落在 arrow_x，与 Finder 图标落点同源）
#
# 完整降级链（缺任一环都不阻断打包，只降级）：
#   1. 有模板 + swift 可用  → 合成（带应用名与箭头）
#   2. 有模板 + swift 不可用 → 直接复制模板
#   3. 无模板               → 回退到占位图 build/darwin/dmg-background.png
#   4. 都没有               → 静默跳过（返回 0，由调用方判断是否设背景）
#
# 为何箭头坐标由参数传入：Finder 的窗口 bounds 宽 ≠ content 视口宽（受侧栏影响），
# 「居中」必然错位。唯一稳的对齐是「箭头与图标共用同一 container 绝对坐标」。
# ==========================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TLP="$ROOT/build/darwin/dmg-background.tpl.png"
FALLBACK="$ROOT/build/darwin/dmg-background.png"

OUT="${1:?用法: compose-dmg-bg.sh <out.png> <app_name> <arrow_cx> <arrow_cy> [w] [h]}"
APP_NAME="${2:-}"
ARROW_CX="${3:-0}"
ARROW_CY="${4:-0}"
W="${5:-600}"
H="${6:-400}"

# 选基底：模板优先，缺失则占位图，都没有则报错
if [ -f "$TLP" ]; then
  BASE="$TLP"
  HAVE_BASE=1
elif [ -f "$FALLBACK" ]; then
  BASE="$FALLBACK"
  HAVE_BASE=1
  echo "提示：无背景模板（${TLP}），回退到占位图。" >&2
else
  echo "提示：既无模板也无占位图，跳过背景合成。" >&2
  exit 0
fi

# 空应用名 → 不做文字叠加，直接复制基底
if [ -z "$APP_NAME" ]; then
  cp "$BASE" "$OUT"
  exit 0
fi

# swift 不可用 → 降级为复制模板
if ! command -v swift >/dev/null 2>&1; then
  echo "提示：未找到 swift，背景图降级为模板（无应用名）。" >&2
  cp "$BASE" "$OUT"
  exit 0
fi

# ---- 合成：以模板为底，叠加应用名 + 箭头 ----
swift - "$BASE" "$OUT" "$APP_NAME" "$ARROW_CX" "$ARROW_CY" "$W" "$H" <<'SWIFT' 2>&1 || {
import Foundation
import AppKit

let args = CommandLine.arguments
guard args.count >= 8 else { exit(2) }
let basePath = args[1]
let outPath  = args[2]
let appName  = args[3]
let arrowX   = CGFloat(Double(args[4]) ?? 0)   // 箭头水平中心（= 两图标落点中点）
let arrowY   = CGFloat(Double(args[5]) ?? 0)   // 箭头所在行（= 图标中心行）
let width    = CGFloat(Double(args[6]) ?? 600)
let height   = CGFloat(Double(args[7]) ?? 400)

guard let baseImage = NSImage(contentsOfFile: basePath) else { exit(3) }

// 按【像素】尺寸绘制：NSImage.lockFocus 会跟随屏幕 backing scale（Retina 下 2×），
// 导致 600×400 变成 1200×800。显式建 bitmap rep 并指定像素尺寸，保证导出精确 W×H。
let pxW = Int(width)
let pxH = Int(height)
guard let rep = NSBitmapImageRep(
    bitmapDataPlanes: nil, pixelsWide: pxW, pixelsHigh: pxH,
    bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
    colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0
) else { exit(3) }
rep.size = NSSize(width: width, height: height)   // 1 point == 1 pixel

NSGraphicsContext.saveGraphicsState()
guard let ctx = NSGraphicsContext(bitmapImageRep: rep) else { exit(3) }
NSGraphicsContext.current = ctx
// 翻转 Y：让 (0,0) 落在左上，与 Finder 图标坐标（左上原点）一致
let xform = NSAffineTransform()
xform.translateX(by: 0, yBy: height)
xform.scaleX(by: 1, yBy: -1)
xform.concat()

// 以模板为底铺满
baseImage.draw(in: NSRect(origin: .zero, size: NSSize(width: width, height: height)),
               from: .zero, operation: .copy, fraction: 1.0)

// 应用名：居中于 arrowX，落在箭头下方的文字区（Y 已翻转为左上原点，
// 故「下方」= y 更大；箭头在 arrowY，名字放 arrowY+ 侧）
let para = NSMutableParagraphStyle()
para.alignment = .center
let attrs: [NSAttributedString.Key: Any] = [
    .font: NSFont(name: "Helvetica-Bold", size: 26)
           ?? NSFont.systemFont(ofSize: 26, weight: .bold),
    .foregroundColor: NSColor(calibratedWhite: 0.28, alpha: 1.0),
    .paragraphStyle: para,
]
let textRect = NSRect(x: arrowX - 150, y: arrowY + 90, width: 300, height: 40)
(appName as NSString).draw(in: textRect, withAttributes: attrs)

// 拖拽箭头：以 (arrowX, arrowY) 为【中心】横向摆放，视觉上连接左应用与右 Applications
// （Y 已翻转为左上原点；arrowY 即图标中心行，arrowX 即两图标落点的中点）
let half  = CGFloat(60)
let tip   = NSPoint(x: arrowX + half, y: arrowY)
let tail  = NSPoint(x: arrowX - half, y: arrowY)
let headY = CGFloat(11)
let headX = CGFloat(26)
NSColor(calibratedWhite: 0.55, alpha: 1.0).setStroke()
let shaft = NSBezierPath()
shaft.lineWidth = 6
shaft.lineCapStyle = .round
shaft.move(to: tail)
shaft.line(to: NSPoint(x: tip.x - headX + 4, y: tip.y))
shaft.stroke()
let head = NSBezierPath()
head.lineWidth = 5
head.lineJoinStyle = .round
head.move(to: NSPoint(x: tip.x - headX, y: tip.y + headY))
head.line(to: tip)
head.line(to: NSPoint(x: tip.x - headX, y: tip.y - headY))
head.stroke()

NSGraphicsContext.restoreGraphicsState()

guard let png = rep.representation(using: .png, properties: [:]) else { exit(4) }
do {
    try png.write(to: URL(fileURLWithPath: outPath))
} catch {
    FileHandle.standardError.write("写 PNG 失败: \(error)\n".data(using: .utf8)!)
    exit(5)
}
SWIFT
  echo "提示：swift 合成失败，背景图降级为模板（无应用名）。" >&2
  cp "$BASE" "$OUT"
}

exit 0
