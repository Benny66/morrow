"""镜像状态机的时序断言（由 scripts/e2e-pet.sh 调起）。

【为什么要有这一条】`advanceMirror()` 里三条时序规则——进入要 3 次确认、换过之后
20 秒内不再换、压制条件立即退出——都是"写错了不会报错"的那种：症状只是宠物切换
太早/太晚/抽搐，而且只有盯着屏幕几分钟才看得出来。本仓没有 JS 测试运行器
（package.json 只有 typecheck / build，引一个要动 deps.yaml），但 vite dev server
会把 `src/lib/pet.ts` 当成普通 ES 模块供出来——于是可以直接在页面里 `import()` 它，
把纯函数当单测跑，不新增任何依赖。

【为什么直接 import 源码而不是测 DOM】这三条规则的入参/出参全是数据，
测 DOM 只能得到"某个时刻有没有 class"，测不出"第几次轮询才该切"。
"""

import argparse
import sys

from playwright.sync_api import sync_playwright

VIEWPORT = {"width": 240, "height": 380}

# 在页面上下文里跑：纯函数、无 DOM 依赖、无网络。
CASES_JS = """
async () => {
  const m = await import('/src/lib/pet.ts');
  const results = [];
  const record = (name, ok, got, want) => results.push({ name, ok, got, want });
  const step = (s, want, now, allowed = true) =>
    m.advanceMirror(s, { allowed, want, nowMs: now });

  // 1) 进入要连续 3 次确认：第 1、2 次只记账，不切换。
  let s1 = { ...m.PET_MIRROR_INITIAL };
  s1 = step(s1, 'working', 0);
  const a1 = s1.pose;
  s1 = step(s1, 'working', 4000);
  const a2 = s1.pose;
  s1 = step(s1, 'working', 8000);
  const a3 = s1.pose;
  record('1 三次确认后才进入', a1 === '' && a2 === '' && a3 === 'working',
    [a1, a2, a3].join(','), ',working');

  // 2) 候选每轮都变（两块屏来回切）→ 永远进不去。
  let s2 = { ...m.PET_MIRROR_INITIAL };
  const seen = [];
  for (let i = 0; i < 10; i += 1) {
    s2 = step(s2, i % 2 === 0 ? 'working' : 'watching', i * 4000);
    seen.push(s2.pose);
  }
  record('2 候选抖动不切换', seen.every((p) => p === ''),
    seen.join('|') || '(空)', '(始终为空)');

  // 3) 最小停留：t=8000 进入 working 后，20 秒内不许换。
  let s3 = { ...m.PET_MIRROR_INITIAL };
  s3 = step(s3, 'working', 0);
  s3 = step(s3, 'working', 4000);
  s3 = step(s3, 'working', 8000);
  const h = [];
  for (const t of [12000, 16000, 20000, 24000, 28000]) {
    s3 = step(s3, 'watching', t);
    h.push(s3.pose);
  }
  record('3 20 秒最小停留', h.slice(0, 4).every((p) => p === 'working') && h[4] === 'watching',
    h.join(','), 'working,working,working,working,watching');

  // 4) 压制条件成立 → 同一次 tick 就清空（退出快），并重置候选与计数。
  const c = step(s3, 'watching', 32000, false);
  record('4 压制立即清除',
    c.pose === '' && c.candidate === '' && c.ticks === 0,
    c.pose + '/' + c.candidate + '/' + c.ticks, '/(空)/0');

  // 5) 类别变成 other（want 为空）属于"换个姿态演"，仍要确认 + 最小停留，
  //    不能像压制那样立刻消失——否则在 other 与 coding 之间切换会闪。
  let s5 = { ...m.PET_MIRROR_INITIAL };
  s5 = step(s5, 'working', 40000);
  s5 = step(s5, 'working', 44000);
  s5 = step(s5, 'working', 48000);
  const o = [];
  for (const t of [52000, 56000, 60000, 68000]) {
    s5 = step(s5, '', t);
    o.push(s5.pose);
  }
  record('5 变 other 也要确认与停留',
    o.slice(0, 3).every((p) => p === 'working') && o[3] === '',
    o.join(','), 'working,working,working,');

  // 6) 挥手必须压过镜像的手臂规则。镜像的样式写在本文件之后，
  //    若某天有人去掉样式里的 `:not(.pet--waving)`，点了宠物就不挥手了——
  //    不报错、不崩溃，只是"点了没反应"。这里直接读计算后的 animation-name。
  //
  // 【为什么用前缀匹配】Pet.vue 是 <style scoped>，Vue 会给 keyframes 加哈希后缀
  // （实测 `pet-arm-wave-555aa2f7`）。只认不带后缀的名字会让这条断言永远红。
  const root = document.querySelector('.pet');
  const arm = document.querySelector('.pet__arm--right');
  const armNameOf = (classes) => {
    root.className = root.className
      .split(' ')
      .filter((c) => c && !c.startsWith('pet--mirror') && c !== 'pet--waving')
      .join(' ');
    for (const c of classes) root.classList.add(c);
    return getComputedStyle(arm).animationName;
  };
  const waveOnWorking = armNameOf(['pet--mirror-working', 'pet--waving']);
  const waveOnListening = armNameOf(['pet--mirror-listening', 'pet--waving']);
  record('6 挥手压过镜像手臂（working）',
    waveOnWorking.startsWith('pet-arm-wave') && !waveOnWorking.includes('mirror'),
    waveOnWorking, 'pet-arm-wave*');
  record('7 挥手压过镜像手臂（listening）',
    waveOnListening.startsWith('pet-arm-wave') && !waveOnListening.includes('mirror'),
    waveOnListening, 'pet-arm-wave*');

  return results;
}
"""


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", required=True)
    args = parser.parse_args()

    with sync_playwright() as p:
        browser = p.chromium.launch(channel="chrome", args=["--no-proxy-server"], headless=True)
        page = browser.new_page(viewport=VIEWPORT)
        # 需要真实页面上下文（vite 的模块图在那里）；路由本身不重要。
        page.goto(f"{args.base_url}/#/pet", wait_until="domcontentloaded")
        page.wait_for_selector(".pet__art", timeout=15000)

        results = page.evaluate(CASES_JS)
        browser.close()

    failed = [r for r in results if not r["ok"]]
    for r in results:
        mark = "✓" if r["ok"] else "✗"
        print(f"  {mark} {r['name']}  实际={r['got']}  期望={r['want']}")
    if failed:
        print(f"\n失败 {len(failed)} 条：镜像时序规则被改坏了，"
              f"改之前先读 lib/pet.ts 的 advanceMirror 注释", file=sys.stderr)
        return 1
    print(f"PASS: 镜像时序 {len(results)} 条断言全部通过")
    return 0


if __name__ == "__main__":
    sys.exit(main())
