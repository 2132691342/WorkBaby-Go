import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiGet, apiPost } from '@/api/client'
import { t } from '@/i18n'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast'
import type { Workflow, WorkflowReq, WorkflowExecution, ExecutionDetail, WorkflowNodeExecution } from '@/types/api'

/**
 * Workflows store：工作流列表 / JSON 定义编辑与校验 / 可视化开关 / 运行编排（run、暂停/恢复/取消、详情轮询、人工输入）。
 *
 * 契约要点：run 返回 {execution_id}；详情 GET /executions/{id} 返回 {execution, nodes}；控制类接口返回空，调用后回拉详情刷新。
 */
export const useWorkflowsStore = defineStore('workflows', () => {
  const dialog = useDialog()
  const toast = useToast()

  const workflows = ref<Workflow[]>([])
  const selected = ref<Workflow | null>(null)
  const executions = ref<WorkflowExecution[]>([])
  // 编辑器中的默认模板——Graph 是 JSON 字符串（WorkflowDO.Graph 持久化格式）。
  // 节点必须用 config（持久化 Graph 的字段名，DAG 层才叫 params）；类型必须是后端内置 7 种。
  const editing = ref<string>(
    JSON.stringify(
      {
        name: 'hello-world',
        nodes: [
          {
            id: 'fetch',
            type: 'http',
            config: { method: 'GET', url: 'https://httpbin.org/get' }
          }
        ],
        outputs: { body: 'fetch.body' }
      },
      null,
      2
    )
  )
  const error = ref<string | null>(null)
  const runningID = ref<string | null>(null)
  const currentExecID = ref<string | null>(null)
  const currentExec = ref<WorkflowExecution | null>(null)
  /** 当前执行的节点执行列表（画布高亮用；来自 GET /executions/:id 详情）。 */
  const currentExecNodes = ref<WorkflowNodeExecution[]>([])
  const showCreate = ref(false)
  const showGraph = ref(false)
  const newName = ref('')
  const newDesc = ref('')

  /** 加载工作流列表；无选中项且有数据时自动选中第一个。 */
  async function loadList(): Promise<void> {
    workflows.value = await apiGet<Workflow[]>('/api/v1/workflows')
    if (!selected.value && workflows.value.length > 0) {
      select(workflows.value[0])
    }
  }

  /** 选中工作流并加载其最近执行记录。 */
  async function select(w: Workflow): Promise<void> {
    selected.value = w
    editing.value = w.graph
    currentExec.value = null
    currentExecID.value = null
    executions.value = await apiGet<WorkflowExecution[]>(`/api/v1/workflows/${w.id}/executions?limit=20`)
  }

  /** 保存当前选中工作流的 JSON 定义（Graph 字段存 JSON）。 */
  async function save(): Promise<void> {
    if (!selected.value) return
    error.value = null
    try {
      const req: WorkflowReq = {
        name: selected.value.name,
        description: selected.value.description ?? '',
        graph: editing.value,
        enabled: selected.value.enabled
      }
      const updated = await apiPost<Workflow>(`/api/v1/workflows/${selected.value.id}/update`, req)
      selected.value = updated
      await loadList()
      toast.success(t('workflows.saveSuccess'))
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e)
      error.value = msg
      toast.error(t('workflows.saveFailed'), msg)
    }
  }

  /** 新建工作流（以当前编辑 YAML 为模板，替换 name 行）。 */
  async function doCreate(): Promise<void> {
    error.value = null
    if (!newName.value.trim()) {
      error.value = t('common.nameRequired')
      return
    }
    const yaml = editing.value.replace(/^name:.*$/m, `name: ${newName.value.trim()}`)
    try {
      // editing.value 是 JSON 字符串；如果模板被改坏（非合法 JSON）用最小空图兜底，
      // 避免后端 `graph json parse failed` 阻断创建流程。
      let graph = yaml
      try {
        JSON.parse(graph)
      } catch {
        graph = JSON.stringify({ name: newName.value.trim(), nodes: [], edges: [] })
      }
      const req: WorkflowReq = {
        name: newName.value.trim(),
        description: newDesc.value,
        graph,
        enabled: true
      }
      const w = await apiPost<Workflow>('/api/v1/workflows', req)
      showCreate.value = false
      newName.value = ''
      newDesc.value = ''
      await loadList()
      select(w)
      // 新建后默认进入可视化画布：新手「拖出第一条流程」比面对 JSON 更友好
      showGraph.value = true
      toast.success(t('workflows.createSuccess'))
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e)
      error.value = msg
      toast.error(t('workflows.createFailed'), msg)
    }
  }

  /** 删除工作流（使用 Dialog 确认）。 */
  async function doDelete(w: Workflow): Promise<void> {
    const ok = await dialog.confirm({
      title: t('common.confirmDelete'),
      content: t('common.confirmDeleteNamed', w.name),
      danger: true
    })
    if (!ok) return
    try {
      await apiPost(`/api/v1/workflows/${w.id}/delete`, {})
      if (selected.value?.id === w.id) selected.value = null
      await loadList()
      toast.success(t('common.deleted'))
    } catch (e) {
      toast.error(t('common.deleteFailed'), e instanceof Error ? e.message : String(e))
    }
  }

  /** 运行当前工作流并轮询执行直到终态。 */
  async function runWorkflow(): Promise<void> {
    if (!selected.value) return
    error.value = null
    runningID.value = selected.value.id
    try {
      const exec = await apiPost<{ execution_id: string }>(
        `/api/v1/workflows/${selected.value.id}/run`,
        { inputs: { test: 'p2-1' } }
      )
      currentExecID.value = exec.execution_id
      currentExec.value = null
      currentExecNodes.value = []
      await pollExecution()
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e)
      error.value = msg
      toast.error(t('workflows.runFailed'), msg)
    } finally {
      runningID.value = null
    }
  }

  /** 拉一次执行详情（GET /executions/:id 返回 {execution, nodes}）。 */
  async function fetchExecutionDetail(): Promise<void> {
    if (!currentExecID.value) return
    try {
      const d = await apiGet<ExecutionDetail>(`/api/v1/executions/${currentExecID.value}`)
      currentExec.value = d.execution
      currentExecNodes.value = d.nodes ?? []
    } catch {
      // 轮询失败保持上次状态，下一次循环重试
    }
  }

  /** 轮询当前执行（最多 60 次、每次 500ms），遇终态停止并刷新执行列表。 */
  async function pollExecution(): Promise<void> {
    if (!currentExecID.value) return
    for (let i = 0; i < 60; i++) {
      await fetchExecutionDetail()
      const st = currentExec.value?.status
      if (st === 'COMPLETED' || st === 'FAILED' || st === 'CANCELLED') {
        break
      }
      await new Promise((r) => setTimeout(r, 500))
    }
    if (selected.value) {
      executions.value = await apiGet<WorkflowExecution[]>(`/api/v1/workflows/${selected.value.id}/executions?limit=20`)
    }
  }

  /** 恢复当前暂停的执行（后端返回空；成功后回拉详情刷新状态）。 */
  async function doResume(): Promise<void> {
    if (!currentExecID.value) return
    try {
      await apiPost(`/api/v1/executions/${currentExecID.value}/resume`, {})
      await fetchExecutionDetail()
      toast.success(t('workflows.resumed'))
    } catch (e) {
      toast.error(t('workflows.resumeFailed'), e instanceof Error ? e.message : String(e))
    }
  }

  /** 暂停当前执行（后端返回空；成功后回拉详情刷新状态）。 */
  async function doPause(): Promise<void> {
    if (!currentExecID.value) return
    try {
      await apiPost(`/api/v1/executions/${currentExecID.value}/pause`, {})
      await fetchExecutionDetail()
      toast.success(t('workflows.paused'))
    } catch (e) {
      toast.error(t('workflows.pauseFailed'), e instanceof Error ? e.message : String(e))
    }
  }

  /** 取消当前执行（后端返回空；成功后回拉详情刷新状态）。 */
  async function doCancel(): Promise<void> {
    if (!currentExecID.value) return
    try {
      await apiPost(`/api/v1/executions/${currentExecID.value}/cancel`, {})
      await fetchExecutionDetail()
      toast.success(t('workflows.cancelled'))
    } catch (e) {
      toast.error(t('workflows.cancelFailed'), e instanceof Error ? e.message : String(e))
    }
  }

  /**
   * 提交人工输入（HUMAN_INPUT 节点）。
   * 调用 POST /api/v1/executions/{id}/input。
   */
  async function submitInput(executionID: string, input: string): Promise<boolean> {
    try {
      await apiPost(`/api/v1/executions/${executionID}/input`, { value: input })
      toast.success(t('workflows.inputSubmitted'))
      return true
    } catch (e) {
      toast.error(t('workflows.inputFailed'), e instanceof Error ? e.message : String(e))
      return false
    }
  }

  return {
    workflows,
    selected,
    executions,
    editing,
    error,
    runningID,
    currentExecID,
    currentExec,
    currentExecNodes,
    showCreate,
    showGraph,
    newName,
    newDesc,
    loadList,
    select,
    save,
    doCreate,
    doDelete,
    runWorkflow,
    pollExecution,
    doResume,
    doPause,
    doCancel,
    submitInput
  }
})
