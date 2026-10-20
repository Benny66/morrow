// 附件输入的展示常量。真正的限额与格式判定在 Go 侧 internal/service/ai_input.go；
// 这里只用于前端提前提示/决定图标，不参与最终校验。

/** 单次请求的附件总数上限（文件 + 粘贴/拖拽图片）。 */
export const MAX_ATTACHMENTS = 8

/** 单张图片的字节上限（5MB）。 */
export const MAX_IMAGE_BYTES = 5 << 20

/** 支持的图片扩展名（不含点）。 */
export const IMAGE_EXTS = ['png', 'jpg', 'jpeg', 'webp', 'gif']

/** 支持的图片 MIME。 */
export const IMAGE_MIMES = ['image/png', 'image/jpeg', 'image/jpg', 'image/webp', 'image/gif']

/** 从路径推断附件类型；非图片一律按文本处理（后端再按格式表判定）。 */
export function attachmentKind(path: string): 'text' | 'image' {
  const dot = path.lastIndexOf('.')
  const ext = dot >= 0 ? path.slice(dot + 1).toLowerCase() : ''
  return IMAGE_EXTS.includes(ext) ? 'image' : 'text'
}

/** 取文件名（同时兼容 / 与 \ 分隔符）。 */
export function fileName(path: string): string {
  const parts = path.split(/[\\/]/)
  return parts[parts.length - 1] || path
}

/** MIME 是否为前端支持的图片类型。 */
export function isImageMime(mime: string): boolean {
  return IMAGE_MIMES.includes(mime.trim().toLowerCase())
}

/** 在 MIME 缺失时按文件名推断图片 MIME；无法推断返回空串。 */
export function mimeFromFileName(name: string): string {
  const ext = name.slice(name.lastIndexOf('.') + 1).toLowerCase()
  switch (ext) {
    case 'png':
      return 'image/png'
    case 'jpg':
    case 'jpeg':
      return 'image/jpeg'
    case 'webp':
      return 'image/webp'
    case 'gif':
      return 'image/gif'
    default:
      return ''
  }
}
