// 管道契约：Go↔TS 的错误协议前端侧。
//
// 与 Go 侧 internal/apperr 成对：
//   - Go 的 apperr.MarshalError 把错误序列化为 JSON 字节（经 Wails 的 MarshalError）。
//   - 前端收到的是 Error 实例，其 .message 是那段 JSON。
//   - 本模块负责 JSON.parse 还原为 AppError，并容错退化。
//
// 【v3 改了机制，但本模块一行都不用动】——这正好是契约层的价值：
//   - v2：ErrorFormatter 的返回值被前端 `new Error(payload)` 强转，
//     故 Go 侧【必须】返回字符串，返回对象会变成 "[object Object]"。
//   - v3：MarshalError 交给框架序列化，不再走强转那条路。前端拿到的仍然是
//     一个 message 里装着 JSON 的 Error，故下面的解析逻辑与结论都成立。
//
// 这是「管道」而非「产品面」：只负责把错误归一化成稳定形状、并记录调用耗时，
// 不做任何 UI（toast / 弹窗由下游按 AppError.code 自行决定）。
//
// 依赖方向单向：invoke → log（慢调用告警）。异常上报在 lib/error-report.ts，
// 它依赖本模块与 log，不反向被依赖——避免 log ←→ invoke 的双向循环。

import { logWarn } from './log'

/** 归一化后的应用错误。code 供分流，message 可直接展示。 */
export interface AppError {
  code: string
  message: string
  detail?: string
}

/** 与 Go 侧 internal/apperr 的错误码常量保持一致（单一真相在 Go，此处为镜像）。 */
export const ErrorCode = {
  NotFound: 'not_found',
  Validation: 'validation',
  Conflict: 'conflict',
  Internal: 'internal',
} as const

/**
 * 把任意 reject 值归一化为 AppError。
 *
 * 正常路径：MarshalError 产出的 JSON → 挂在 Error.message 上 → parse 还原。
 * 容错路径：解析失败（非本协议的错误，如 Wails 内部错误）→ 退化为 internal + 原文，
 * 保证下游拿到的永远是稳定形状，不会撞见 "[object Object]"。
 */
export function normalizeError(raw: unknown): AppError {
  const text = raw instanceof Error ? raw.message : String(raw)
  try {
    const parsed = JSON.parse(text)
    if (parsed && typeof parsed.code === 'string' && typeof parsed.message === 'string') {
      return parsed as AppError
    }
  } catch {
    // 落到兜底
  }
  return { code: ErrorCode.Internal, message: text || '未知错误' }
}

/**
 * 超过该耗时（毫秒）的绑定调用会记一条告警。
 *
 * 取值依据：本地进程间调用的正常量级在个位数毫秒；50ms 已属明显异常，
 * 又不会把正常的首帧调用（如读取配置）误报成问题。
 */
export const SLOW_CALL_MS = 50

// 启动期的调用聚合。刻意不逐条记录：绝大多数调用只有几毫秒，逐条记会让日志量
// 随调用次数线性增长，把真正要看的告警淹没。聚合值由启动日志一行带出。
let callCount = 0
let maxMs = 0
let slowestLabel = ''

/**
 * 返回启动期绑定调用的聚合统计，供启动日志一行带出。
 *
 * 每次启动都有一行「IPC 概况」，且不随调用量增长——这是刻意的：
 * 逐条记录会让日志量失控，而汇总足以回答「这次启动的 IPC 代价有多大」。
 */
export function ipcStats(): { ipc_calls: number; ipc_max_ms: number; ipc_slowest: string } {
  return {
    ipc_calls: callCount,
    ipc_max_ms: Math.round(maxMs),
    ipc_slowest: slowestLabel,
  }
}

/**
 * 包裹一次绑定调用：归一化错误，并记录耗时（超阈值时告警）。
 *
 * 用法（下游范例）：
 *   try {
 *     const data = await invoke(() => ListAssets(req), 'ListAssets')
 *   } catch (e) {
 *     const err = e as AppError
 *     if (err.code === ErrorCode.NotFound) { ... }  // 按 code 分流
 *   }
 *
 * @param label 调用点标签，仅用于慢调用告警里定位。**不自动猜测**：本函数拿到的是
 *   闭包（`() => ListAssets(req)`），从源码猜名字在 `usePagedList` 这类路径下会取到
 *   `fetcher` 这种误导性结果——一个错的名字比没有名字更糟，故默认如实记为「未标注调用」。
 *
 * 不强迫下游使用——直接 await 绑定方法也能跑，只是拿不到归一化形状与耗时统计。
 */
export async function invoke<T>(fn: () => Promise<T>, label = '未标注调用'): Promise<T> {
  const started = performance.now()
  try {
    return await fn()
  } catch (raw) {
    throw normalizeError(raw)
  } finally {
    // 放在 finally：失败的调用同样可能慢，而那正是最需要看到的一次
    const ms = performance.now() - started
    callCount++
    if (ms > maxMs) {
      maxMs = ms
      slowestLabel = label
    }
    if (ms >= SLOW_CALL_MS) {
      logWarn('绑定调用偏慢', { label, ms: Math.round(ms) })
    }
  }
}
