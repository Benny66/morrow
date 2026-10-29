"""宠物右键菜单的浏览器端断言（由 scripts/e2e-pet.sh 调起）。

断言这些事：
1. 宠物页在纯 vite 环境能挂载（绑定失败被吞，不该白屏）；
2. 右键宠物能弹出菜单，且菜单完整落在窗口内——默认 240×380 与 120×190
   各测一次（后者对应宠物缩放 0.5，菜单必须跟着窗口收窄）；
3. 菜单项文本/顺序与 lib/pet.ts 的表一致；「隐藏宠物」始终有回程说明，
   「换形象」在无后端时显示读取失败而不是"去导入素材包"；
4. Escape / 点菜单外 / 窗口失焦 / 点菜单项都能关闭菜单，且页面不崩。

【为什么用文本断言菜单】菜单文案是产品的单一真相，散落成两处就会出现
"菜单写换形象、代码里其实是换缩放"。测试里硬编码期望值不算第二处实现。

【纯 vite 的边界】绑定调用会失败，所以菜单里「换形象」是禁用态；
这里刻意点「打开主窗」——它不依赖后端数据，能验证"点完关闭且不崩"。
"""

import argparse
import os

from playwright.sync_api import sync_playwright

DEFAULT_VIEWPORT = {"width": 240, "height": 380}
# 宠物缩放到 0.5 时的窗口尺寸（petBaseWidth/Height × 0.5）。
SMALL_VIEWPORT = {"width": 120, "height": 190}

# 期望的菜单项（顺序即显示顺序）
EXPECTED_LABELS = ["换形象", "缩放", "今天先这样", "打开主窗", "隐藏宠物"]

# 纯 vite 下 ListPetPacks 必然失败：第一条是「换形象」的失败说明，
# 第二条是「隐藏宠物」的回程说明。真实后端里第一条会消失或换成导入提示。
EXPECTED_HINTS = ["读取素材包失败，请重试", "可在设置里重新打开"]


def launch_browser(p):
    """优先用系统 Chrome（省一次浏览器下载），没有再回落到 playwright 自带的 chromium。

    --no-proxy-server 是必须的：开发机若配了 HTTP_PROXY，连 127.0.0.1 也会走代理，
    表现为页面 502 而 dev server 其实好好的。
    """
    args = ["--no-proxy-server"]
    headless = not bool(os.environ.get("PET_E2E_HEADED"))
    try:
        return p.chromium.launch(channel="chrome", args=args, headless=headless)
    except Exception:
        return p.chromium.launch(args=args, headless=headless)


def assert_inside(box: dict, label: str, failures: list[str], viewport: dict) -> None:
    if box is None:
        failures.append(f"{label}：菜单没有可见包围盒")
        return
    right = box["x"] + box["width"]
    bottom = box["y"] + box["height"]
    if box["x"] < 0 or box["y"] < 0 or right > viewport["width"] or bottom > viewport["height"]:
        failures.append(
            f"{label}：菜单越界 {box}（右={right:.0f} 下={bottom:.0f}，"
            f"窗口 {viewport['width']}×{viewport['height']}）——"
            f"越界部分会被窗口裁掉且不报错"
        )


def open_menu(page) -> bool:
    """右键宠物中心；菜单没出来返回 False。"""
    art = page.locator(".pet__art").first
    box = art.bounding_box()
    if box is None:
        return False
    art.click(button="right", position={"x": box["width"] / 2, "y": box["height"] / 2})
    try:
        page.wait_for_selector(".pet__menu", timeout=3000)
        return True
    except Exception:
        return False


def menu_closed(page) -> bool:
    return page.locator(".pet__menu").count() == 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--out-dir", required=True)
    args = parser.parse_args()

    failures: list[str] = []
    browser = None
    with sync_playwright() as p:
        browser = launch_browser(p)
        page = browser.new_page(viewport=DEFAULT_VIEWPORT, device_scale_factor=2)
        console_errors: list[str] = []
        page.on("console", lambda m: console_errors.append(m.text) if m.type == "error" else None)
        page.goto(f"{args.base_url}/#/pet", wait_until="domcontentloaded")

        try:
            page.wait_for_selector(".pet__art", timeout=15000)
        except Exception:
            page.screenshot(path=f"{args.out_dir}/00-mount-failed.png")
            print("FAIL: .pet__art 没有渲染出来，浏览器端挂载失败")
            print("控制台错误：", console_errors[:5])
            browser.close()
            return 1

        page.wait_for_timeout(600)
        page.screenshot(path=f"{args.out_dir}/01-idle.png")

        if not open_menu(page):
            page.screenshot(path=f"{args.out_dir}/02-menu-missing.png")
            browser.close()
            print("FAIL: 右键后没有出现 .pet__menu")
            return 1

        page.wait_for_timeout(200)
        page.screenshot(path=f"{args.out_dir}/03-menu-open.png")

        menu = page.locator(".pet__menu").first
        mbox = menu.bounding_box()
        print(f"菜单包围盒: {mbox}")
        assert_inside(mbox, "默认窗口", failures, DEFAULT_VIEWPORT)

        labels = [t.strip() for t in menu.locator(".pet__menu-label").all_inner_texts()]
        print(f"菜单项: {labels}")
        if labels != EXPECTED_LABELS:
            failures.append(f"菜单项与期望不符：{labels}，期望 {EXPECTED_LABELS}")

        hints = [t.strip() for t in menu.locator(".pet__menu-hint").all_inner_texts()]
        print(f"说明行: {hints}")
        if hints != EXPECTED_HINTS:
            failures.append(f"说明行应为 {EXPECTED_HINTS}，实际 {hints}")

        page.keyboard.press("Escape")
        page.wait_for_timeout(200)
        if not menu_closed(page):
            failures.append("Escape 没有关闭菜单")
        page.screenshot(path=f"{args.out_dir}/04-after-escape.png")

        # ---- 点菜单外关闭 ----
        if open_menu(page):
            page.mouse.click(2, 2)
            page.wait_for_timeout(150)
            if not menu_closed(page):
                failures.append("点击菜单外没有关闭菜单")
        else:
            failures.append("验证外点关闭时菜单没有打开")

        # ---- 窗口失焦关闭 ----
        if open_menu(page):
            page.evaluate("() => window.dispatchEvent(new Event('blur'))")
            page.wait_for_timeout(150)
            if not menu_closed(page):
                failures.append("窗口失焦没有关闭菜单")
        else:
            failures.append("验证失焦关闭时菜单没有打开")

        # ---- 点一个始终可用的菜单项：动作会因缺少后端而失败，但不能让页面崩掉 ----
        if open_menu(page):
            # 索引 3 = 打开主窗；「换形象」在纯 vite 下是禁用态，点它 Playwright 会卡到超时。
            page.locator(".pet__menu-item").nth(3).click()
            page.wait_for_timeout(300)
            if not menu_closed(page):
                failures.append("点菜单项后菜单没有关闭")
            if page.locator(".pet__art").count() == 0:
                failures.append("点菜单项后宠物本体消失了（页面崩了）")
        else:
            failures.append("验证点项关闭时菜单没有打开")
        page.screenshot(path=f"{args.out_dir}/05-after-item-click.png")

        # ---- 四个角：夹取真正被考验的地方 ----
        art = page.locator(".pet__art").first
        box = art.bounding_box()
        if box is None:
            failures.append("宠物本体没有包围盒")
        else:
            corners = {
                "右下": {"x": box["width"] - 2, "y": box["height"] - 2},
                "右上": {"x": box["width"] - 2, "y": 2},
                "左下": {"x": 2, "y": box["height"] - 2},
                "左上": {"x": 2, "y": 2},
            }
            for name, pos in corners.items():
                art.click(button="right", position=pos)
                try:
                    page.wait_for_selector(".pet__menu", timeout=3000)
                except Exception:
                    failures.append(f"{name}角右键没有弹出菜单")
                    continue
                page.wait_for_timeout(80)
                cbox = page.locator(".pet__menu").first.bounding_box()
                print(f"{name}角右键 → 菜单 {cbox}")
                assert_inside(cbox, f"{name}角", failures, DEFAULT_VIEWPORT)
                page.keyboard.press("Escape")
                page.wait_for_timeout(80)

        # ---- 小缩放窗口（120×190）：菜单宽度必须跟着窗口收窄 ----
        page.set_viewport_size(SMALL_VIEWPORT)
        page.wait_for_timeout(300)
        page.screenshot(path=f"{args.out_dir}/06-small-window.png")
        if not open_menu(page):
            failures.append("小窗口下右键没有弹出菜单")
        else:
            page.wait_for_timeout(150)
            sbox = page.locator(".pet__menu").first.bounding_box()
            print(f"小窗口菜单包围盒: {sbox}")
            assert_inside(sbox, "小窗口", failures, SMALL_VIEWPORT)
            small_labels = [
                t.strip() for t in page.locator(".pet__menu .pet__menu-label").all_inner_texts()
            ]
            if small_labels != EXPECTED_LABELS:
                failures.append(f"小窗口菜单项与期望不符：{small_labels}")
            page.screenshot(path=f"{args.out_dir}/07-small-menu.png")
            page.keyboard.press("Escape")
            page.wait_for_timeout(100)

        browser.close()

    if failures:
        for item in failures:
            print(f"FAIL: {item}")
        return 1
    print("PASS: 菜单渲染 / 四角夹取 / 小窗口收窄 / 四种关闭路径 / 菜单项内容 全部通过")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
