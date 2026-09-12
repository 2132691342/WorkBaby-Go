import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { apiGet, apiPost } from '@/api/client'
import { getApiBase } from '@/api/http'
import { t } from '@/i18n'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast'
import type {
  Workflow,
  WorkflowReq,
  WorkflowExecution,
  ExecutionDetail,
  WorkflowNodeExecution,
  WorkflowPendingInput
} from '@/types/api'

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

  /** 当前执行里等待人工输入的节点上下文（由 workflow:input-required 事件填充）。 */
  const inputEvent = ref<WorkflowPendingInput | null>(null)

  /**
   * 待人工输入：事件载荷优先（带提问文案与 TTL）；页面刷新后事件已错过，回退到节点执行状态。
   */
  const pendingInput = computed<WorkflowPendingInput | null>(() => {
    if (inputEvent.value) return inputEvent.value
    const node = currentExecNodes.value.find((n) => n.status === 'waiting_input')
    if (!node || !currentExecID.value) return null
    return { executionID: currentExecID.value, nodeID: node.node_id, prompt: '', ttlSeconds: 0 }
  })

  /** workflow SSE 连接：监听当前执行的实时事件（started / node-* / input-required / paused / resumed / 终态）。 */
  let workflowES: EventSource | null = null

  function stopWatchWorkflow(): void {
    if (workflowES) {
      workflowES.close()
      workflowES = null
    }
  }

  /** 订阅 workflow 进度：runWorkflow 拿到 executionID 后调用；任一终态事件自动关闭。 */
  function watchWorkflowEvents(executionID: string): void {
    stopWatchWorkflow()
    const base = getApiBase()
    if (!base) return
    const es = new EventSource(
      `${base}/events?scope=workflow&runId=${encodeURIComponent(executionID)}`
    )
    workflowES = es
    const onProgress = (): void => {
      // 节点级事件直接刷新详情（拿到最新 outputs / status）
      void fetchExecutionDetail()
    }
    // 人工输入：prompt 与 TTL 只在事件载荷里（详情接口不返回），必须在此捕获
    es.addEventListener('workflow:input-required', (ev: MessageEvent) => {
      inputEvent.value = parseInputRequired(String(ev.data), executionID)
      void fetchExecutionDetail()
    })
    es.addEventListener('workflow:started', onProgress)
    es.addEventListener('workflow:node-start', onProgress)
    es.addEventListener('workflow:node-done', onProgress)
    // 暂停 / 恢复：同一执行仍在推进，刷新详情而非关流
    es.addEventListener('workflow:paused', onProgress)
    es.addEventListener('workflow:resumed', onProgress)
    es.addEventListener('workflow:completed', () => {
      inputEvent.value = null
      void fetchExecutionDetail()
      stopWatchWorkflow()
    })
    es.addEventListener('workflow:failed', () => {
      inputEvent.value = null
      void fetchExecutionDetail()
      stopWatchWorkflow()
    })
    es.addEventListener('workflow:cancelled', () => {
      inputEvent.value = null
      void fetchExecutionDetail()
      stopWatchWorkflow()
    })
    es.onerror = (): void => {
      // watch 关闭 / 网络抖动由后端心跳 + 浏览器重连兜底；这里只防泄漏
      if (workflowES !== es) return
    }
  }

  /**
   * 解析 workflow:input-required 载荷。字段名沿用后端 camelCase（executionID / nodeID），
   * 不做归一化——sse.go 的订阅过滤同样按 executionID 取值，改名会让事件投递不到订阅者。
   */
  function parseInputRequired(raw: string, executionID: string): WorkflowPendingInput | null {
    try {
      const m = JSON.parse(raw) as Record<string, unknown>
      const nodeID = typeof m.nodeID === 'string' ? m.nodeID : ''
      if (!nodeID) return null
      return {
        executionID: typeof m.executionID === 'string' ? m.executionID : executionID,
        nodeID,
        prompt: typeof m.prompt === 'string' ? m.prompt : '',
        ttlSeconds: typeof m.ttlSeconds === 'number' ? m.ttlSeconds : 0
      }
    } catch {
      return null
    }
  }

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

  /** 新建工作流（可选传入模板 Graph 预填节点；否则以当前编辑器 JSON 为底）。 */
  async function doCreate(templateGraph?: string): Promise<void> {
    error.value = null
    if (!newName.value.trim()) {
      error.value = t('common.nameRequired')
      return
    }
    try {
      // 模板优先：解析模板 JSON 并写入名称；否则沿用编辑器 JSON（改坏则空图兜底），
      // 避免后端 `graph json parse failed` 阻断创建流程。
      let graph: string
      if (templateGraph) {
        try {
          const obj = JSON.parse(templateGraph) as Record<string, unknown>
          obj.name = newName.value.trim()
          graph = JSON.stringify(obj)
        } catch {
          graph = JSON.stringify({ name: newName.value.trim(), inputs: {}, nodes: [], outputs: {} })
        }
      } else {
        graph = editing.value.replace(/^name:.*$/m, `name: ${newName.value.trim()}`)
        try {
          JSON.parse(graph)
        } catch {
          graph = JSON.stringify({ name: newName.value.trim(), nodes: [], edges: [] })
        }
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
        // 手动运行不预设输入：需要的变量由工作流自身的图级 inputs 声明
        { inputs: {} }
      )
      currentExecID.value = exec.execution_id
      currentExec.value = null
      currentExecNodes.value = []
      watchWorkflowEvents(exec.execution_id)
      await pollExecution()
      // 不在此关流：终态由事件监听自行关闭，挂起态（等待人工输入 / 暂停）需要保留连接
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
      // 后端实际值是小写；前后端契约对齐后此处只比较小写
      if (st === 'completed' || st === 'failed' || st === 'cancelled') {
        break
      }
      // 挂起态交给 SSE 事件驱动：继续空轮询只会白等满 30s，且用户提交后拿不到实时进度
      if (st === 'paused' || pendingInput.value) {
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
   *
   * nodeID 必传：后端 resolver.Deliver 按 executionID|nodeID 拼 waiter key，
   * 不传永远匹配不到，节点会卡到 ttlSeconds 后超时。
   */
  async function submitInput(executionID: string, nodeID: string, input: string): Promise<boolean> {
    try {
      await apiPost(`/api/v1/executions/${executionID}/input`, { node_id: nodeID, value: input })
      inputEvent.value = null
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
    pendingInput,
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
    submitInput,
    stopWatchWorkflow
  }
})
