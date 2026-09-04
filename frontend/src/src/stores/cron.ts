import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiGet, apiPost } from '@/api/client'
import { useDialog } from '@/composables/useDialog'
import { t } from '@/i18n'
import type { CronJob, CronJobReq, Workflow } from '@/types/api'

/**
 * Cron store（Pinia 重构 + snake_case 契约）。
 *
 * <p>职责：定时任务列表 CRUD + 工作流下拉选项 + 立即触发。
 */
export const useCronStore = defineStore('cron', () => {
  const jobs = ref<CronJob[]>([])
  const workflows = ref<Workflow[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)
  const info = ref<string | null>(null)
  const editingID = ref<string | null>(null)
  const triggeringID = ref<string | null>(null)

  const form = ref<CronJobReq>(blank())

  function blank(): CronJobReq {
    return { name: '', schedule: '', workflow_id: null, enabled: true }
  }

  /** 拉取定时任务 + 工作流列表。 */
  async function load(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const [j, w] = await Promise.all([
        apiGet<CronJob[]>('/api/v1/cron/jobs'),
        apiGet<Workflow[]>('/api/v1/workflows').catch(() => [] as Workflow[])
      ])
      jobs.value = j
      workflows.value = w
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  /** 进入编辑模式。 */
  function edit(j: CronJob): void {
    editingID.value = j.id
    form.value = { name: j.name, schedule: j.schedule, workflow_id: j.workflow_id, enabled: j.enabled }
  }

  /** 取消编辑/新建。 */
  function cancel(): void {
    editingID.value = null
    form.value = blank()
  }

  /** 提交创建或更新。 */
  async function submit(): Promise<void> {
    error.value = null
    info.value = null
    if (!form.value.name.trim() || !form.value.schedule.trim()) {
      error.value = t('cron.errRequired')
      return
    }
    try {
      if (editingID.value) {
        await apiPost<CronJob>(`/api/v1/cron/jobs/${editingID.value}/update`, form.value)
        info.value = t('common.updated')
      } else {
        await apiPost<CronJob>('/api/v1/cron/jobs', form.value)
        info.value = t('common.created')
      }
      cancel()
      await load()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  /** 删除指定定时任务。 */
  async function remove(id: string): Promise<void> {
    const dialog = useDialog()
    if (!await dialog.confirm({ title: t('common.confirmDeleteTitle'), content: t('common.confirmDelete'), danger: true })) return
    try {
      await apiPost(`/api/v1/cron/jobs/${id}/delete`)
      await load()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  /** 立即手动触发指定任务。 */
  async function triggerNow(id: string): Promise<boolean> {
    triggeringID.value = id
    error.value = null
    try {
      await apiPost(`/api/v1/cron/jobs/${id}/trigger`)
      info.value = t('cron.triggered')
      return true
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
      return false
    } finally {
      triggeringID.value = null
    }
  }

  return {
    jobs,
    workflows,
    loading,
    error,
    info,
    editingID,
    triggeringID,
    form,
    load,
    edit,
    cancel,
    submit,
    remove,
    triggerNow
  }
})
