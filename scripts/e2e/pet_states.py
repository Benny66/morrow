"""新增 CSS 状态的目视冒烟：给根节点挂上状态类后逐张截图（由 scripts/e2e-pet.sh 调起）。

【为什么只截图不断言】微动作要等 40–90 秒、打盹要等 8 分钟，无头浏览器里等不起；
但"这些类挂上去之后宠物会不会变形/错位/消失"是纯 CSS 的事，立刻能看。
断言交给 pet_menu.py，观感交给人眼——截图就是给人眼看的那一份。

状态类的来源见 frontend/src/views/Pet.vue 的根节点 :class 绑定。
"""

import argparse
import os
import sys

from playwright.sync_api import sync_playwright

VIEWPORT = {"width": 240, "height": 380}

# 类名 → (要挂的类, 截图前等待毫秒)；等待是为了让动画走到中段而不是起始帧。
# 顺序有意义：后一个状态截图前会清掉前一个，故不必担心叠加。
STATES = {
    "micro-look": (["pet--micro", "pet--micro-look"], 400),
    "micro-stretch": (["pet--micro", "pet--micro-stretch"], 500),
    "micro-groom": (["pet--micro", "pet--micro-groom"], 500),
    "micro-yawn": (["pet--micro", "pet--micro-yawn"], 550),
    "dozing": (["pet--dozing"], 800),
    "waking": (["pet--waking"], 120),
    # 镜像姿态（pet-activity-mirror）：都是静态倾斜，等待只为让 working 的手臂摆动
    # 走到中段——起始帧与静止帧看起来一样，那样截图就验不出"手在动"。
    "mirror-working": (["pet--mirror-working"], 700),
    "mirror-listening": (["pet--mirror-listening"], 300),
    "mirror-watching": (["pet--mirror-watching"], 300),
    "today-thin": (["pet--today-thin"], 300),
    "growth-4": (["pet--growth-4"], 400),
}

# 每轮截图前要清掉的类：它们都挂在同一个根节点上，且互斥。
# growth/today 用前缀清，避免 growth-0、today-part 这类初值残留成叠加态。
STRIP_PREFIXES = ("pet--micro", "pet--growth", "pet--today", "pet--mirror")
STRIP_EXACT = ("pet--dozing", "pet--waking")


def launch_browser(p):
    args = ["--no-proxy-server"]
    headless = not bool(os.environ.get("PET_E2E_HEADED"))
    try:
        return p.chromium.launch(channel="chrome", args=args, headless=headless)
    except Exception:
        return p.chromium.launch(args=args, headless=headless)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--out-dir", required=True)
    args = parser.parse_args()

    with sync_playwright() as p:
        browser = launch_browser(p)
        page = browser.new_page(viewport=VIEWPORT, device_scale_factor=2)
        page.goto(f"{args.base_url}/#/pet", wait_until="domcontentloaded")
        page.wait_for_selector(".pet__art", timeout=15000)
        page.wait_for_timeout(500)

        for name, (classes, wait_ms) in STATES.items():
            page.evaluate(
                """({ classes, prefixes, exact }) => {
                    const root = document.querySelector('.pet');
                    root.className = root.className
                      .split(' ')
                      .filter((c) => c
                        && !prefixes.some((p) => c.startsWith(p))
                        && !exact.includes(c))
                      .join(' ');
                    for (const c of classes) root.classList.add(c);
                }""",
                {"classes": classes, "prefixes": list(STRIP_PREFIXES), "exact": list(STRIP_EXACT)},
            )
            page.wait_for_timeout(wait_ms)
            path = f"{args.out_dir}/state-{name}.png"
            page.screenshot(path=path)
            print(f"已截图 {path}")

        # 镜像姿态的【真实尺寸】复核：settings 里 pet_scale 常见是 0.5，
        # 也就是屏幕上只有 96×104。全窗口截图会把这个尺寸掩盖掉，
        # 所以这里把 .pet__art 压到 0.5 再对元素本身截图——判"看得见吗"必须用这个尺寸。
        #
        # 【必须先冻结呼吸】待机呼吸是 .pet__svg 上的 scale(1.03/0.97) 循环，
        # 抓图时落在哪个相位是随机的，会让包围盒有 ±3% 的抖动——而姿态之间的
        # 差异本来就在 5–12% 量级，不冻结就量不出可信的数字。
        page.add_style_tag(content=".pet__svg { animation: none !important; }")
        HALF_W, HALF_H = 96, 104
        for name in ("idle", "mirror-working", "mirror-listening", "mirror-watching"):
            classes = STATES[name][0] if name in STATES else []
            page.evaluate(
                """({ classes, prefixes, exact, w, h }) => {
                    const root = document.querySelector('.pet');
                    root.className = root.className
                      .split(' ')
                      .filter((c) => c
                        && !prefixes.some((p) => c.startsWith(p))
                        && !exact.includes(c))
                      .join(' ');
                    for (const c of classes) root.classList.add(c);
                    const art = document.querySelector('.pet__art');
                    art.style.width = w + 'px';
                    art.style.height = h + 'px';
                }""",
                {
                    "classes": classes,
                    "prefixes": list(STRIP_PREFIXES),
                    "exact": list(STRIP_EXACT),
                    "w": HALF_W,
                    "h": HALF_H,
                },
            )
            page.wait_for_timeout(600 if "mirror-working" in name else 300)
            path = f"{args.out_dir}/half-{name}.png"
            page.locator(".pet__art").screenshot(path=path)
            print(f"已截图 {path}（0.5 倍真实尺寸）")

        browser.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
