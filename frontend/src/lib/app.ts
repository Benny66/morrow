// 管道契约：应用级事件与启动参数。
//
// 与 Go 侧成对：
//   - 事件动作常量镜像 Go `main` 包的 `ActionSecondInstance`，由
//     internal/guard/parity_test.go 断言一致（漂移即失败）。
//   - 载荷字段镜像 Go 的 `App.SecondInstancePayload`。
//
// 二次启动（应用已在运行时再次打开）的启动参数经事件推送；
// 首次启动的参数没有事件可等（那时前端还没订阅，必然丢），改用绑定查询：
//
//   const args = await invoke(() => GetLaunchArgs())

import { onEvent } from './event'

/** 应用级事件动作常量。单一真相在 Go 的 main 包（Action*），此处为镜像。 */
export const AppEvent = {
  /** 应用被二次启动（携带当次启动参数）。 */
  SecondInstance: 'second-instance',
} as const

/** `app:second-instance` 事件的载荷，对应 Go 的 App.SecondInstancePayload（字段名需一致）。 */
export interface SecondInstancePayload {
  /** 二次启动时的命令行参数。 */
  args: string[]
  /** 二次启动时的工作目录。 */
  working_dir: string
}

/** 二次启动事件名（`<domain>:<action>`，domain 为 app）。 */
export const SECOND_INSTANCE_EVENT = `app:${AppEvent.SecondInstance}`

/**
 * 订阅「应用被二次启动」。返回取消订阅函数。
 *
 * 组件内请改用 composables/useEvent 版本的包装，或自行在卸载时调用返回值。
 * 例：收到参数后打开对应文件/唤起某个页面。
 */
export function onSecondInstance(
  handler: (payload: SecondInstancePayload) => void,
): () => void {
  return onEvent<SecondInstancePayload>(SECOND_INSTANCE_EVENT, handler)
}
