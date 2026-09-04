import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiGet, apiPost } from '@/api/client'
import { t } from '@/i18n'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast'
import type { MemoryEpisode, MemoryEpisodeReq, MemoryFact, MemoryProcedure, RecallEntry } from '@/types/api'

/**
 * Memory store（Pinia 重构 + 阶段 1-4 API 补全）。
 *
 * <p>职责：情景记忆浏览/搜索 + 手动写入 + 三类记忆统一浏览。
 *
 * <p><b>与后端的契约</b>：
 * <ul>
 *   <li>关键词搜索：{@code GET /api/v1/memory/search?q=}</li>
 *   <li>最近记忆：{@code GET /api/v1/memory/episodes?limit=50}</li>
 *   <li>总数统计：{@code GET /api/v1/memory/stats}</li>
 *   <li>手动写入：{@code POST /api/v1/memory/episodes}</li>
 *   <li>删除：{@code POST /api/v1/memory/episodes/{id}/delete}</li>
 *   <li>统一召回：{@code GET /api/v1/memory/recall?q=}</li>
 *   <li>语义记忆：{@code GET /api/v1/memory/facts}</li>
 *   <li>程序记忆：{@code GET /api/v1/memory/procedures}</li>
 * </ul>
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

  /** 初始化时加载三类记忆。 */
  async function initAll(): Promise<void> {
    await Promise.all([load(), loadFacts(), loadProcedures()])
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
    load,
    doCreate,
    addTag,
    removeTag,
    doDelete,
    recall,
    loadFacts,
    loadProcedures,
    initAll
  }
})
