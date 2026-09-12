import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiGet, apiPost } from '@/api/client'
import { t } from '@/i18n'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast'
import type { InboxItem, InboxStats, MemoryEpisode, MemoryEpisodeReq, MemoryFact, MemoryProcedure, RecallEntry } from '@/types/api'

/**
 * 记忆中心 store：三类记忆的浏览 / 搜索 / 手动写入 / 删除，端点见 doc/16。
 */
export const useMemoryStore = defineStore('memory', () => {
  const dialog = useDialog()
  const toast = useToast()

  const episodes = ref<MemoryEpisode[]>([])
  const total = ref(0)
  const query = ref('')
  const loading = ref(false)
  const error = ref<string | null>(null)
  const showCreate = ref(false)
  const createForm = ref<MemoryEpisodeReq>({ summary: '', transcript: '', tags: [] })
  const newTag = ref('')

  // 新增：三类记忆数据
  const recallResults = ref<RecallEntry[]>([])
  const facts = ref<MemoryFact[]>([])
  const procedures = ref<MemoryProcedure[]>([])
  const recallLoading = ref(false)
  const factsLoading = ref(false)
  const proceduresLoading = ref(false)

  // 回写收件箱：run 终局抽取的候选（事实 / 程序 / 技能草稿）待人工评审
  const inbox = ref<InboxItem[]>([])
  const inboxStats = ref<InboxStats>({ pending: 0, approved: 0, rejected: 0 })
  const inboxLoading = ref(false)

  /** 按 query 拼 url 加载记忆列表，并取 total 统计。 */
  async function load(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      if (query.value.trim()) {
        episodes.value = await apiGet<MemoryEpisode[]>(
          `/api/v1/memory/search?q=${encodeURIComponent(query.value.trim())}&k=20`
        )
      } else {
        episodes.value = await apiGet<MemoryEpisode[]>('/api/v1/memory/episodes?limit=50')
      }
      const stats = await apiGet<{ total: number }>('/api/v1/memory/stats')
      total.value = stats.total
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  /** 手动写入一条情景记忆。 */
  async function doCreate(): Promise<void> {
    if (!createForm.value.summary.trim()) {
      error.value = t('knowledge.errSummary')
      return
    }
    try {
      await apiPost('/api/v1/memory/episodes', {
        ...createForm.value,
        tags: createForm.value.tags || []
      })
      showCreate.value = false
      createForm.value = { summary: '', transcript: '', tags: [] }
      newTag.value = ''
      await load()
      toast.success(t('knowledge.createSuccess'))
    } catch (e) {
      toast.error(t('knowledge.createFailed'), e instanceof Error ? e.message : String(e))
    }
  }

  /** 向新建表单追加一个标签。 */
  function addTag(): void {
    const tag = newTag.value.trim()
    if (!tag) return
    createForm.value.tags = [...(createForm.value.tags || []), tag]
    newTag.value = ''
  }

  /** 按索引移除新建表单中的一个标签。 */
  function removeTag(idx: number): void {
    createForm.value.tags = (createForm.value.tags || []).filter((_, i) => i !== idx)
  }

  /** 删除一条记忆（使用 Dialog 确认）。 */
  async function doDelete(ep: MemoryEpisode): Promise<void> {
    const ok = await dialog.confirm({
      title: t('common.confirmDelete'),
      content: t('common.confirmDeleteNamed', ep.summary),
      danger: true
    })
    if (!ok) return
    try {
      await apiPost(`/api/v1/memory/episodes/${ep.id}/delete`, {})
      await load()
      toast.success(t('common.deleted'))
    } catch (e) {
      toast.error(t('common.deleteFailed'), e instanceof Error ? e.message : String(e))
    }
  }

  // ===== 新增：三类记忆 API 对接 =====

  /** 统一召回（RRF 融合三类记忆）。 */
  async function recall(q: string): Promise<void> {
    if (!q.trim()) {
      recallResults.value = []
      return
    }
    recallLoading.value = true
    try {
      recallResults.value = await apiGet<RecallEntry[]>(`/api/v1/memory/recall?q=${encodeURIComponent(q.trim())}&k=10`)
    } catch (e) {
      toast.error(t('memory.recallFailed'), e instanceof Error ? e.message : String(e))
      recallResults.value = []
    } finally {
      recallLoading.value = false
    }
  }

  /** 加载语义记忆（facts）。 */
  async function loadFacts(): Promise<void> {
    factsLoading.value = true
    try {
      facts.value = await apiGet<MemoryFact[]>('/api/v1/memory/facts?limit=50')
    } catch (e) {
      toast.error(t('memory.loadFailed'), e instanceof Error ? e.message : String(e))
    } finally {
      factsLoading.value = false
    }
  }

  /** 加载程序记忆（procedures）。 */
  async function loadProcedures(): Promise<void> {
    proceduresLoading.value = true
    try {
      procedures.value = await apiGet<MemoryProcedure[]>('/api/v1/memory/procedures?limit=50')
    } catch (e) {
      toast.error(t('memory.loadFailed'), e instanceof Error ? e.message : String(e))
    } finally {
      proceduresLoading.value = false
    }
  }

  // ===== 回写收件箱 =====

  /** 加载收件箱条目（status 缺省只看待审）。 */
  async function loadInbox(status = 'pending'): Promise<void> {
    inboxLoading.value = true
    try {
      inbox.value = await apiGet<InboxItem[]>(`/api/v1/memory/inbox?status=${status}&limit=100`)
    } catch (e) {
      toast.error(t('memory.loadFailed'), e instanceof Error ? e.message : String(e))
    } finally {
      inboxLoading.value = false
    }
  }

  /** 加载收件箱概览统计。 */
  async function loadInboxStats(): Promise<void> {
    try {
      inboxStats.value = await apiGet<InboxStats>('/api/v1/memory/inbox/stats')
    } catch {
      /* 统计失败不影响列表展示 */
    }
  }

  /** 合入一条候选（写入语义记忆 / 程序记忆 / 技能库）。 */
  async function approveInbox(id: string): Promise<void> {
    try {
      await apiPost(`/api/v1/memory/inbox/${id}/approve`, {})
      toast.success(t('common.saved'))
    } catch (e) {
      toast.error(t('common.saveFailed'), e instanceof Error ? e.message : String(e))
    }
    await Promise.all([loadInbox(), loadInboxStats()])
  }

  /** 忽略一条候选。 */
  async function rejectInbox(id: string): Promise<void> {
    try {
      await apiPost(`/api/v1/memory/inbox/${id}/reject`, {})
    } catch (e) {
      toast.error(t('common.saveFailed'), e instanceof Error ? e.message : String(e))
    }
    await Promise.all([loadInbox(), loadInboxStats()])
  }

  /** 初始化时加载三类记忆。 */
  async function initAll(): Promise<void> {
    await Promise.all([load(), loadFacts(), loadProcedures(), loadInbox(), loadInboxStats()])
  }

  return {
    episodes,
    total,
    query,
    loading,
    error,
    showCreate,
    createForm,
    newTag,
    recallResults,
    facts,
    procedures,
    recallLoading,
    factsLoading,
    proceduresLoading,
    inbox,
    inboxStats,
    inboxLoading,
    load,
    doCreate,
    addTag,
    removeTag,
    doDelete,
    recall,
    loadFacts,
    loadProcedures,
    loadInbox,
    loadInboxStats,
    approveInbox,
    rejectInbox,
    initAll
  }
})
