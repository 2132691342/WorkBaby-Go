/**
 * 错误消费层（Wails AppError ↔ JS Error）。
 *
 * <p>Wails 把 Go *pkg.AppError 序列化为 JS Error 时：
 *   message = `${code} ${humanMessage}`
 *   真正结构化字段前端拿不到（Go struct → JS Error 仅 message 字符串）。
 * 因此本层把 message 按 "[code] human: details" 解析；解析失败回退统一文案。
 *
 * <p>doc/15 §7 与后端 pkg/apperror.go 一一对应。
 */

interface AppErrorShape {
  code: number
  message?: string
  details?: string
}

const FALLBACK = '操作失败，请重试'

export function errorMessage(e: unknown): string {
  if (typeof e === 'string') return e
  if (e instanceof Error) {
    const m = e.message
    // Wails 返回的 message 形如 "[3001] provider not ready: <name>"，直接透传
    return m || FALLBACK
  }
  return FALLBACK
}

export function errorCode(e: unknown): number | undefined {
  const s = e as Partial<AppErrorShape> | undefined
  if (typeof s?.code === 'number') return s.code
  // 从 message 头部解析 "[3001]"
  if (e instanceof Error) {
    const m = e.message.match(/^\[(\d+)\]/)
    if (m) return Number(m[1])
  }
  return undefined
}

export interface ToastLike {
  error(msg: string, detail?: string): void
  warning(msg: string, detail?: string): void
  success(msg: string): void
}

export function handleError(e: unknown, _opts: Record<number, () => void> = {}): void {
  // 全局兜底：调用方按需 import element-plus ElMessage
  // 为避免在 util 里引 UI 组件，调用方应在 catch 里显式 toast
  console.error('[WorkBaby error]', e)
}
