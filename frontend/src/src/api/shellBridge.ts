/**
 * 桌面壳 JS Bridge：Wails v2 JS runtime 不含对话框 / 桌宠绑定，本文件作为薄封装收敛调用方。
 * 浏览器预览态（未注入 window.go）抛错时返回 ok:false，调用方自行降级。
 */
import { OpenDirectoryDialog, PetMove, PetToggleMode } from '@/wailsjs/go/main/App'

export interface ShellResponse<T = unknown> {
  ok: boolean
  data?: T
  error?: string
  cancelled?: boolean
}

/**
 * 统一壳调用入口：按 action 分发给对应 Wails 绑定。
 * 浏览器预览态（未注入 window.go）抛错时返回 ok:false，调用方自行降级。
 */
export async function invokeShell<T = unknown>(action: string, params?: unknown): Promise<ShellResponse<T>> {
  try {
    switch (action) {
      case 'pet.move': {
        const p = (params ?? {}) as { dx?: number; dy?: number }
        const r = await PetMove(p.dx ?? 0, p.dy ?? 0)
        return { ok: true, data: r as unknown as T }
      }
      case 'pet.toggle': {
        const r = await PetToggleMode()
        return { ok: true, data: r as unknown as T }
      }
      default:
        return { ok: false, error: `unknown shell action: ${action}` }
    }
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : String(e) }
  }
}

/** 调原生壳目录选择器；取消或失败返回 null。 */
export async function pickDirectoryViaShell(initialPath?: string): Promise<string | null> {
  try {
    const picked = await OpenDirectoryDialog('选择工作区目录', initialPath ?? '')
    return picked && picked.trim() !== '' ? picked : null
  } catch {
    return null
  }
}
