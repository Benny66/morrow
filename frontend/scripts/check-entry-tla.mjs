// 产物护栏：入口 chunk 不得含顶层 await。
//
// 【为什么必须查产物】生产构建会把共用模块并进入口 chunk，路由 chunk 于是静态
// import 入口；入口若在顶层 await router.isReady()，就形成「入口等路由、路由等
// 入口求值」的循环，打包版白屏且无任何报错（wails3 dev 各模块独立加载，看不出来）。
// 这个依赖图只有构建后才存在，所以检查挂在 npm run build 之后。
//
// 【为什么只扫入口】死锁的必要条件是入口有顶层 await；其他 chunk 里的顶层 await
// 不构成上述循环。
//
// 解析到 0 个入口时直接失败：护栏宁可报错也不静默放行（AGENTS.md 铁律 5）。

import { readFileSync } from 'node:fs'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { parse } from 'acorn'

const DIST = resolve(dirname(fileURLToPath(import.meta.url)), '../dist')
const FUNCTION_TYPES = new Set(['FunctionDeclaration', 'FunctionExpression', 'ArrowFunctionExpression'])

function fail(message) {
  console.error(`[check-entry-tla] 失败：${message}`)
  process.exit(1)
}

// 顶层 await 的两种语法形态：await 表达式与 for await...of。函数体内的不算。
function countTopLevelAwait(code) {
  const ast = parse(code, { ecmaVersion: 'latest', sourceType: 'module' })
  let hits = 0

  const visit = (node, inFunction) => {
    if (!node || typeof node !== 'object') return
    if (typeof node.type !== 'string') return

    const nested = inFunction || FUNCTION_TYPES.has(node.type)
    if (!nested) {
      if (node.type === 'AwaitExpression') hits += 1
      if (node.type === 'ForOfStatement' && node.await) hits += 1
    }

    for (const [key, value] of Object.entries(node)) {
      if (key === 'start' || key === 'end') continue
      if (Array.isArray(value)) {
        for (const child of value) visit(child, nested)
      } else {
        visit(value, nested)
      }
    }
  }

  visit(ast, false)
  return hits
}

// 护栏自检：检测逻辑本身坏了必须立刻报错，而不是“扫了个寂寞”也返回 0。
if (countTopLevelAwait('await Promise.resolve()') === 0) {
  fail('自检未识别出顶层 await，检测逻辑可能已失效，请同步更新本脚本')
}
if (countTopLevelAwait('async function f() { await Promise.resolve() }') !== 0) {
  fail('自检把函数体内的 await 误判为顶层，检测逻辑可能已失效，请同步更新本脚本')
}

function findModuleScripts() {
  const html = readFileSync(resolve(DIST, 'index.html'), 'utf8')
  const scripts = []
  for (const tag of html.matchAll(/<script\b[^>]*>/g)) {
    const type = tag[0].match(/\btype=["']module["']/)
    const src = tag[0].match(/\bsrc=["']([^"']+)["']/)
    if (type && src) scripts.push(src[1])
  }
  return scripts
}

const scripts = findModuleScripts()
if (scripts.length === 0) {
  fail('index.html 里没有任何 module script，产物结构可能已变更，请同步更新本脚本的解析规则')
}

for (const src of scripts) {
  if (/^https?:\/\//.test(src)) continue
  const file = resolve(DIST, src.replace(/^\.?\//, ''))
  const hits = countTopLevelAwait(readFileSync(file, 'utf8'))
  if (hits > 0) {
    fail(`${src} 含 ${hits} 处顶层 await。生产构建会因此与路由 chunk 循环等待（白屏）；` +
      '把启动逻辑收进异步函数，入口模块立即结束求值。')
  }
}

console.log(`[check-entry-tla] 通过：${scripts.length} 个入口 chunk 均无顶层 await`)
