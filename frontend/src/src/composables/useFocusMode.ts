import { ref } from 'vue'

/**
 * 焦点模式：单例 ref，跨组件共享。
 *
 * <p>开启后 MessageList 隐藏思考面板与工具时间线，仅保留用户/助手正文与最终交付。
 * 会话级 view 偏好（不持久化），刷新即重置——避免误关后用户找不到。
 */
const enabled = ref(false)

export function useFocusMode() {
  return {
    enabled,
    toggle(): void { enabled.value = !enabled.value },
    set(v: boolean): void { enabled.value = v }
  }
}
