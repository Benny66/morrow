import js from '@eslint/js'
import vueParser from 'vue-eslint-parser'
import tsParser from '@typescript-eslint/parser'

// ==========================================================================
// 前端护栏（flat config）
// --------------------------------------------------------------------------
// 两条自定义规则把宪法铁律编译成会红的检查：
//   - no-node-imports:    渲染层禁 import node 能力，必须走 wails bindings。
//   - no-hardcoded-brand: 禁硬编码 hex 色值（应引 tokens）与品牌字符串。
// ==========================================================================

/** @type {import('eslint').Linter.FlatConfig[]} */
export default [
  {
    // bindings/** 是 wails3 generate bindings 的产物（含源码注释里的中文与方法名），
    // 与 v2 的 wailsjs/** 同性质：只读、不入 lint。改它没有意义——下次生成就被覆盖。
    ignores: ['dist/**', 'bindings/**', 'wailsjs/**', 'node_modules/**'],
  },

  // 基础 JS 规则
  js.configs.recommended,

  // Vue 文件用 vue-eslint-parser 解析，script 块用 TS parser
  {
    files: ['**/*.vue'],
    languageOptions: {
      parser: vueParser,
      parserOptions: {
        parser: tsParser,
        sourceType: 'module',
        ecmaVersion: 'latest',
      },
      globals: {
        window: 'readonly',
        document: 'readonly',
        navigator: 'readonly',
        console: 'readonly',
        localStorage: 'readonly',
      },
    },
  },
  {
    files: ['**/*.ts', '**/*.js'],
    languageOptions: {
      parser: tsParser,
      sourceType: 'module',
      ecmaVersion: 'latest',
      globals: {
        window: 'readonly',
        document: 'readonly',
        navigator: 'readonly',
        console: 'readonly',
        localStorage: 'readonly',
      },
    },
  },

  // TS 文件：关掉两条「非 TS 感知」的基础规则，改由 vue-tsc 兜
  //
  //   no-unused-vars：基础规则会把【类型注解里】的函数类型参数
  //     （`handler: (payload: T) => void`）当作真实绑定，误报未使用。
  //     改由 tsconfig 的 noUnusedLocals / noUnusedParameters 检查（带类型信息更准，
  //     且尊重 `_` 前缀豁免）。
  //
  //   no-undef：基础规则不认识 DOM/TS 类型名（ErrorEvent、PromiseRejectionEvent、
  //     HTMLInputElement…），把它们当未定义全局变量报错。TS 本身就会报
  //     "Cannot find name"（且比 ESLint 知道得更多），故按官方建议对 TS 关闭。
  //
  // 已知边界：没有 lang="ts" 的纯 JS <script> 块不在 vue-tsc 检查范围内，
  // 其未使用变量/未定义变量两边都管不到。本仓页面一律 <script setup lang="ts">，
  // 暂不构成缺口。
  {
    files: ['**/*.ts', '**/*.vue'],
    rules: {
      'no-unused-vars': 'off',
      'no-undef': 'off',
    },
  },

  // 自定义规则
  {
    files: ['src/**/*.vue', 'src/**/*.ts', 'src/**/*.js'],
    plugins: {
      guard: {
        rules: {
          'no-node-imports': { create: noNodeImportsRule },
          'no-hardcoded-brand': { create: noHardcodedBrandRule },
        },
      },
    },
    rules: {
      'guard/no-node-imports': 'error',
      'guard/no-hardcoded-brand': 'error',
    },
  },
]

// ---------------------------------------------------------------------------
// 规则 1：渲染层禁 import node 能力
// ---------------------------------------------------------------------------
function noNodeImportsRule(context) {
  const forbidden = /^(node:|\b(fs|child_process|path|os|crypto|http|https|net|tls|dns|dgram|cluster|worker_threads|process)\b)/

  return {
    ImportDeclaration(node) {
      const src = node.source.value
      if (typeof src === 'string' && forbidden.test(src)) {
        context.report({
          node,
          message: `渲染层禁止 import node 能力 "${src}"，必须经 wails bindings（preload 受控 API）`,
        })
      }
    },
  }
}

// ---------------------------------------------------------------------------
// 规则 2：禁硬编码 hex 色值（应引 tokens）
//   - 豁免 src/styles/ 目录（那是令牌真相源，本就该有 hex 定义）
//   - 全文件源码扫描（含 .vue 的 <style> 块）
// ---------------------------------------------------------------------------
function noHardcodedBrandRule(context) {
  const filename = context.filename || context.getPhysicalFilename?.() || ''

  return {
    Program(node) {
      // 令牌/主题定义文件豁免
      if (filename.includes('/styles/')) return

      const source = context.sourceCode.getText()
      const hexRegex = /#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})\b/g

      let m
      while ((m = hexRegex.exec(source)) !== null) {
        context.report({
          node,
          loc: context.sourceCode.getLocFromIndex(m.index),
          message: `禁止硬编码色值 "${m[0]}"，应引用设计令牌 var(--token-*)`,
        })
      }
    },
  }
}
