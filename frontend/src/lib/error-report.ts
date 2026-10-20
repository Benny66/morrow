// 管道契约：前端异常上报（全局兜底 + Vue 错误钩子）。
//
// 与 log.ts 分开是刻意的依赖方向问题：log.ts 是**纯日志出口**（只依赖 Wails 运行时），
// 而 invoke.ts 需要在调用超时时记一条日志。若「异常上报」留在 log.ts 里，
// 就会变成 log.ts ←→ invoke.ts 的双向依赖（log 用 invoke 的归一化，invoke 用 log 的写日志）。
// 拆开后依赖是单向的：
//
//   error-report.ts ──→ log.ts
//          └──────────→ invoke.ts（归一化）
//   invoke.ts ────────→ log.ts（慢调用告警）
//
// 归一化逻辑仍然只有一份（invoke.ts 的 normalizeError），没有复制。

import { type AppError, normalizeError } from './invoke'
import { logError } from './log'

/**
 * 上报一个异常：归一化为 AppError 后写入日志，并交给可选的 hook。
 *
 * @param raw 任意抛出物（Error / AppError / 字符串 / 其它）
 * @param context 出错位置的人类可读描述（如 'Vue 渲染'、'未处理的 Promise 拒绝'）
 * @param hook 下游接管入口（toast 等），不传则只记日志
 */
export function reportError(
  raw: unknown,
  context: string,
  hook?: (err: AppError, context: string) => void,
): AppError {
  // 复用错误契约的归一化，保证日志里的 code/message 与调用方 catch 到的是同一形状
  const err = normalizeError(raw)
  logError(
    `[${context}] ${err.message}`,
    err.detail ? { code: err.code, detail: err.detail } : { code: err.code },
  )
  hook?.(err, context)
  return err
}

// 当前已安装的处理器，供幂等安装与卸载使用。
let onWindowError: ((event: ErrorEvent) => void) | null = null
let onUnhandledRejection: ((event: PromiseRejectionEvent) => void) | null = null

/**
 * 安装全局错误兜底，返回卸载函数。
 *
 * 三处都要挂，缺一不可：
 *   - `error`             ：同步运行时错误、资源加载失败
 *   - `unhandledrejection`：未 catch 的 Promise —— 绑定调用漏 catch 是重灾区
 *   - Vue 的 errorHandler ：组件树内的渲染/生命周期异常，上面两个覆盖不到
 *     （由 main.ts 用 createVueErrorHandler() 挂到 app.config.errorHandler）
 *
 * 幂等：重复调用只保留最后一次安装。
 */
export function installGlobalErrorHandler(
  hook?: (err: AppError, context: string) => void,
): () => void {
  uninstallGlobalErrorHandler()

  onWindowError = (event: ErrorEvent) => {
    // resource 类错误（图片/脚本加载失败）没有 error 对象，退化为消息文本
    reportError(event.error ?? event.message, 'window.onerror', hook)
  }
  onUnhandledRejection = (event: PromiseRejectionEvent) => {
    reportError(event.reason, '未处理的 Promise 拒绝', hook)
  }

  window.addEventListener('error', onWindowError)
  window.addEventListener('unhandledrejection', onUnhandledRejection)

  return uninstallGlobalErrorHandler
}

/** 卸载全局错误兜底（未安装时为空操作）。 */
export function uninstallGlobalErrorHandler(): void {
  if (onWindowError) {
    window.removeEventListener('error', onWindowError)
    onWindowError = null
  }
  if (onUnhandledRejection) {
    window.removeEventListener('unhandledrejection', onUnhandledRejection)
    onUnhandledRejection = null
  }
}

/**
 * 生成 Vue 的 errorHandler，供 main.ts 挂到 `app.config.errorHandler`。
 *
 * 刻意不在这里 import Vue 类型：`lib/` 是管道层，不依赖框架。
 * 返回的函数签名与 Vue 期望的一致（多余的参数忽略即可）。
 */
export function createVueErrorHandler(
  hook?: (err: AppError, context: string) => void,
): (err: unknown, instance: unknown, info: string) => void {
  return (err: unknown, _instance: unknown, info: string) => {
    reportError(err, `Vue ${info || '异常'}`, hook)
  }
}
