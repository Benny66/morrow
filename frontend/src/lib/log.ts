// 管道契约：前端日志出口。
//
// 为什么必须有它：打包后的应用【没有 DevTools】。`console.*` 在开发态看着挺好，
// 生产态是一处无人可见的输出——用户遇到的异常就此消失，只能收到一句「它不好使了」。
//
// 【v3 换了机制，这是迁移里最容易白改的一处】：
//   - v2：`wailsjs/runtime/runtime` 的 LogDebug/LogInfo/LogWarning/LogError，
//     经 WailsInvoke 送给 `options.App.Logger`。
//   - v3：这几个函数在 `@wailsio/runtime` 里【根本不存在了】，Options.Logger 也
//     明确只管 Wails 系统消息、不管应用日志。取代它们的是官方 log 服务
//     （`pkg/services/log`）：main.go 用 application.NewService 注册它，绑定生成器
//     再产出下面 import 的这套接口。
//
//   接线漏掉的症状与 v2 一模一样——日志只剩 stdout、不报错、不崩溃。故由
//   internal/guard/wiring_test.go 断言 main.go 确实注册了这个服务。
//
// 因此本模块仍是前端日志【唯一】的正确出口；console 只在非 wails 环境（纯 vite dev）兜底。
//
// 本文件不放任何 UI：异常如何呈现给用户（toast/弹窗）是产品面，由下游决定。
// 需要接管时，用 installGlobalErrorHandler 的 hook 参数。

import { LogService } from '../../bindings/github.com/wailsapp/wails/v3/pkg/services/log'

/** 日志级别。与 Go 侧 slog 的级别对应（debug/info/warn/error）。 */
export type LogLevel = 'debug' | 'info' | 'warn' | 'error'

/**
 * 级别 → log 服务方法的映射。
 *
 * 写显式映射而不是 `LogService[level]` 索引：两者的名字并不一致
 * （warn 对应的是 Warning），索引写法既过不了类型检查，也不好读。
 */
const BRIDGE = {
  debug: LogService.Debug,
  info: LogService.Info,
  warn: LogService.Warning,
  error: LogService.Error,
} as const

/** 单条日志中 detail 的最大长度，超出截断——避免把巨大对象灌进日志文件。 */
const MAX_DETAIL_LENGTH = 2000

/** 把附加上下文序列化为可读文本；循环引用等异常情况退化为 String()。 */
function formatDetail(detail: unknown): string {
  if (detail === undefined || detail === null) return ''
  try {
    const text = typeof detail === 'string' ? detail : JSON.stringify(detail)
    if (text === undefined) return String(detail)
    return text.length > MAX_DETAIL_LENGTH ? `${text.slice(0, MAX_DETAIL_LENGTH)}…(已截断)` : text
  } catch {
    // JSON.stringify 对循环引用会抛错，此时退回最朴素的形式
    return String(detail)
  }
}

/**
 * 组装成单行文本。日志是行式存储，多行内容会破坏解析，故统一压成一行。
 * 不带级别前缀——级别由后端 logger 记录，写进消息里是重复。
 */
function compose(message: string, detail?: unknown): string {
  const tail = formatDetail(detail)
  const line = tail ? `${message} | ${tail}` : message
  return line.replace(/\s*\n\s*/g, ' ⏎ ')
}

/** 写 console。仅在日志桥不可用时使用。 */
function toConsole(level: LogLevel, line: string): void {
  const out = level === 'warn' ? console.warn : level === 'error' ? console.error : console.log
  out(line)
}

/**
 * 日志桥是否已被证实不可用。
 *
 * 置位后不再尝试 IPC——非 wails 环境（纯 vite dev）下每次尝试都会产生一个
 * 注定失败的请求和一个未处理的 rejection，把控制台淹掉。
 */
let bridgeUnavailable = false

/**
 * 写入后端日志文件；非 wails 环境退化为 console，保证日志不丢。
 *
 * 【v3 的调用是异步的，这是与 v2 最实质的差别】：`LogService.*` 返回 Promise，
 * 失败时是 **reject** 而不是同步抛错。v2 的 LogXxx 是同步函数，所以老代码那个
 * 光秃秃的 try/catch 在 v3 下【接不住任何东西】——必须挂 .catch，否则
 * 纯 vite dev 下每条日志都变成一条未处理的 rejection。
 */
function write(level: LogLevel, line: string): void {
  if (!bridgeUnavailable) {
    try {
      BRIDGE[level](line).catch(() => {
        bridgeUnavailable = true
        console.warn('Wails 日志桥不可用，后续日志输出到 console（纯 vite dev 环境？）')
        toConsole(level, line)
      })
      return
    } catch {
      // 同步抛错的分支保留：绑定层将来若改回同步实现，这里仍然兜得住。
      bridgeUnavailable = true
    }
  }
  toConsole(level, line)
}

/** 写一条 debug 日志。 */
export function logDebug(message: string, detail?: unknown): void {
  write('debug', compose(message, detail))
}

/** 写一条 info 日志。 */
export function logInfo(message: string, detail?: unknown): void {
  write('info', compose(message, detail))
}

/** 写一条警告日志。 */
export function logWarn(message: string, detail?: unknown): void {
  write('warn', compose(message, detail))
}

/** 写一条错误日志。 */
export function logError(message: string, detail?: unknown): void {
  write('error', compose(message, detail))
}
