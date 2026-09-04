import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiGet } from '@/api/client'
import type { Session } from '@/types/api'

/**
 * Tasks store：任务列表（会话快照，{@code GET /api/v1/chat/sessions?page=1&page_size=50}）+ 过滤状态。
 */
export const useTasksStore = defineStore('tasks', () => {
  const sessions = ref<Session[]>([])
  const error = ref<string | null>(null)
  const loading = ref(false)
  const filter = ref<'all' | 'active' | 'empty'>('all')

  /** 加载任务（会话快照）列表。 */
  async function load(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const page = await apiGet<{ items: Session[]; total: number }>('/api/v1/chat/sessions?page=1&page_size=50')
      sessions.value = page.items
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  return {
    sessions,
    error,
    loading,
    filter,
    load
  }
})
