import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiGet, apiPost } from '@/api/client'
import { t } from '@/i18n'
import type {
  WorkflowGraph,
  WorkflowGraphNode,
  WorkflowNodeTypeInfo
} from '@/types/api'

/** Vue Flow 节点内部形态。 */
interface VFNode {
  id: string
  type: string
  position: { x: number; y: number }
  data: { label: string; kind: string }
}

/** Vue Flow 边内部形态。 */
interface VFEdge {
  id: string
  source: string
  target: string
  sourceHandle?: string | null
  label?: string
}

/** 布局常量（仅为无持久化坐标的新节点计算默认位置用）。 */
const NODE_W = 180
const NODE_H = 60
const GAP_X = 70
const GAP_Y = 50



/**
 * WorkflowGraph store：加载 DAG（GET /workflows/{id}/graph）与节点目录（GET /workflows/node-types），
 * 驱动 Vue Flow 画布；维护位置/分支/参数编辑并保存（POST /workflows/{id}/update-graph，DAG wire 见 doc/11）。
 * 节点位置持久化（pos），缺失时自动布局补位。
 */
export const useWorkflowGraphStore = defineStore('workflowGraph', () => {
  const vfNodes = ref<VFNode[]>([])
  const vfEdges = ref<VFEdge[]>([])
  const nodeTypes = ref<WorkflowNodeTypeInfo[]>([])
  const error = ref<string | null>(null)
  const loading = ref(false)
  const saving = ref(false)
  const saveError = ref<string | null>(null)
  const selectedNodeID = ref<string | null>(null)

  let seq = 0
  let currentID: string | null = null
  /** 当前图（wire 格式，保存的唯一真相源）。 */
  let currentGraph: WorkflowGraph = { name: '', nodes: [], edges: [] }
  /** 图级输出映射（响应式，供编辑器渲染）。 */
  const graphOutputs = ref<Record<string, string>>({})
  /** 目录是否已加载（一次会话只拉一次）。 */
  let nodeTypesLoaded = false

  /** 节点类型显示名：优先 i18n，其次目录 label，最后 type。 */
  function typeLabel(type: string): string {
    const key = `wfGraph.kind.${type}`
    const localized = t(key)
    if (localized !== key) return localized
    const info = nodeTypes.value.find((x) => x.type === type)
    return info?.label ?? type
  }

  /** 加载节点目录（幂等）。 */
  async function ensureNodeTypes(): Promise<void> {
    if (nodeTypesLoaded) return
    try {
      nodeTypes.value = await apiGet<WorkflowNodeTypeInfo[]>('/api/v1/workflows/node-types')
      nodeTypesLoaded = true
    } catch {
      nodeTypes.value = []
    }
  }

  /** 加载工作流图 + 目录，渲染画布。 */
  async function load(id: string): Promise<void> {
    error.value = null
    loading.value = true
    selectedNodeID.value = null
    currentID = id
    // 重置画布（避免上次数据残留）
    vfNodes.value = []
    vfEdges.value = []
    currentGraph = { name: '', nodes: [], edges: [] }
    graphOutputs.value = {}
    try {
      await ensureNodeTypes()
      const g = await apiGet<WorkflowGraph>(`/api/v1/workflows/${id}/graph`)
      // 防御：后端可能漏字段导致 null
      currentGraph = {
        name: g?.name ?? '',
        nodes: Array.isArray(g?.nodes) ? g.nodes : [],
        edges: Array.isArray(g?.edges) ? g.edges : [],
        outputs: g?.outputs ?? {}
      }
      graphOutputs.value = currentGraph.outputs ?? {}
      applyToCanvas()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
      // 即使失败也保证画布是空数组，避免 null 触发下游 forEach
      vfNodes.value = []
      vfEdges.value = []
    } finally {
      loading.value = false
    }
  }

  /** 把 currentGraph 写入画布：位置缺省者自动布局补位（并回写）。 */
  function applyToCanvas(): void {
    const safeNodes = currentGraph.nodes ?? []
    const safeEdges = currentGraph.edges ?? []
    // 1) 记录哪些节点缺位置
    const missing = safeNodes.filter((n) => !n.pos)
    // 2) 为缺位置的节点计算拓扑分层坐标
    const posMap = autoPositions(currentGraph, missing.map((n) => n.id))
    missing.forEach((n) => {
      const p = posMap.get(n.id)
      if (p) n.pos = { x: p.x, y: p.y }
    })

    vfNodes.value = safeNodes.map((n) => ({
      id: n.id,
      type: 'default',
      position: { x: n.pos?.x ?? 0, y: n.pos?.y ?? 0 },
      data: { label: nodeDisplayLabel(n), kind: n.type }
    }))

    vfEdges.value = safeEdges.map((e, i) => ({
      id: `e${i}`,
      source: e.from ?? '',
      target: e.to ?? '',
      sourceHandle: e.source_handle ?? null,
      label: e.source_handle || undefined
    }))
  }

  /** 节点显示名：params.label 优先，否则类型名。 */
  function nodeDisplayLabel(n: WorkflowGraphNode): string {
    const l = n.params?.label
    return typeof l === 'string' && l.trim() ? l : typeLabel(n.type)
  }

  /** 拓扑分层给指定节点集分配坐标（Kahn；结果 Map<nodeID, {x,y}>）。 */
  function autoPositions(g: WorkflowGraph, targets: string[]): Map<string, { x: number; y: number }> {
    const targetSet = new Set(targets)
    const indeg = new Map<string, number>()
    const adj = new Map<string, string[]>()
    g.nodes.forEach((n) => {
      indeg.set(n.id, 0)
      adj.set(n.id, [])
    })
    g.edges.forEach((e) => {
      adj.get(e.from)?.push(e.to)
      indeg.set(e.to, (indeg.get(e.to) ?? 0) + 1)
    })
    const level = new Map<string, number>()
    const queue: string[] = []
    g.nodes.forEach((n) => {
      if ((indeg.get(n.id) ?? 0) === 0) {
        queue.push(n.id)
        level.set(n.id, 0)
      }
    })
    while (queue.length) {
      const cur = queue.shift()!
      for (const next of adj.get(cur) ?? []) {
        level.set(next, Math.max(level.get(next) ?? 0, (level.get(cur) ?? 0) + 1))
        const ni = (indeg.get(next) ?? 1) - 1
        indeg.set(next, ni)
        if (ni === 0) queue.push(next)
      }
    }
    const byLevel = new Map<number, string[]>()
    g.nodes.forEach((n) => {
      const lv = level.get(n.id) ?? 0
      const arr = byLevel.get(lv) ?? []
      arr.push(n.id)
      byLevel.set(lv, arr)
    })
    const out = new Map<string, { x: number; y: number }>()
    const occupied = new Set<string>() // `${x},${y}` 已有布局坐标
    g.nodes.forEach((n) => {
      if (n.pos) occupied.add(`${n.pos.x},${n.pos.y}`)
    })
    ;[...byLevel.entries()]
      .sort((a, b) => a[0] - b[0])
      .forEach(([lv, ids]) => {
        ids.forEach((id, i) => {
          if (!targetSet.has(id)) return
          let x = 60 + lv * (NODE_W + GAP_X)
          let y = 60 + i * (NODE_H + GAP_Y)
          // 避免与已有坐标碰撞（拖拽过/持久化的节点）
          while (occupied.has(`${x},${y}`)) y += NODE_H + GAP_Y
          occupied.add(`${x},${y}`)
          out.set(id, { x, y })
        })
      })
    return out
  }

  /** 手动连线；source_handle 仅在源是 condition 节点时写死为 handle 值。 */
  function connect(source: string, target: string, sourceHandle?: string | null): void {
    if (!source || !target || source === target) return
    const dup = currentGraph.edges.find((e) => e.from === source && e.to === target)
    if (dup) return
    currentGraph.edges = [
      ...currentGraph.edges,
      { from: source, to: target, source_handle: sourceHandle ?? undefined }
    ]
    applyToCanvas()
  }

  /** 删除边。 */
  function removeEdge(source: string, target: string, sourceHandle?: string | null): void {
    currentGraph.edges = currentGraph.edges.filter(
      (e) => !(e.from === source && e.to === target && (sourceHandle == null || e.source_handle === sourceHandle))
    )
    applyToCanvas()
  }

  /** 添加节点（目录里的真实类型）。 */
  function addNode(type: string, label?: string): string {
    const newID = `n_${Date.now().toString(36)}_${seq++}`
    const info = nodeTypes.value.find((x) => x.type === type)
    const name = label ?? info?.label ?? type
    // 默认配置从 schema 的 Default 灌入（temperature 0.2 等），避免保存时缺值
    const defaults: Record<string, unknown> = {}
    info?.fields
      .filter((f) => f.default !== undefined && f.default !== null)
      .forEach((f) => {
        defaults[f.name] = f.default
      })
    currentGraph = {
      ...currentGraph,
      nodes: [
        ...currentGraph.nodes,
        { id: newID, type, params: { ...defaults, label: name } }
      ]
    }
    applyToCanvas()
    return newID
  }

  /** 删除节点及关联边。 */
  function removeNode(id: string): void {
    if (selectedNodeID.value === id) selectedNodeID.value = null
    currentGraph = {
      ...currentGraph,
      nodes: currentGraph.nodes.filter((n) => n.id !== id),
      edges: currentGraph.edges.filter((e) => e.from !== id && e.to !== id)
    }
    applyToCanvas()
  }

  /** 拖拽结束：把坐标写回 currentGraph。 */
  function onNodeMoved(id: string, x: number, y: number): void {
    currentGraph = {
      ...currentGraph,
      nodes: currentGraph.nodes.map((n) => (n.id === id ? { ...n, pos: { x, y } } : n))
    }
  }

  function selectNode(id: string | null): void {
    selectedNodeID.value = id
  }

  function selectedNode(): WorkflowGraphNode | null {
    return currentGraph.nodes.find((n) => n.id === selectedNodeID.value) ?? null
  }

  /** 更新图级输出映射（key → nodeId.field）。 */
  function setOutputs(outputs: Record<string, string>): void {
    currentGraph = { ...currentGraph, outputs }
    graphOutputs.value = outputs
  }

  /** 更新节点 params（属性面板保存）。 */
  function updateNodeParams(id: string, params: Record<string, unknown>): void {
    currentGraph = {
      ...currentGraph,
      nodes: currentGraph.nodes.map((n) => (n.id === id ? { ...n, params } : n))
    }
    applyToCanvas()
  }

  /** 强制全量自动布局。 */
  function autoLayout(): void {
    const all = currentGraph.nodes.map((n) => n.id)
    const posMap = autoPositions(currentGraph, all)
    currentGraph = {
      ...currentGraph,
      nodes: currentGraph.nodes.map((n) => {
        const p = posMap.get(n.id)
        return p ? { ...n, pos: { x: p.x, y: p.y } } : n
      })
    }
    applyToCanvas()
  }

  /** 保存到后端并回拉权威数据。 */
  async function saveGraph(workflowID?: string): Promise<void> {
    const id = workflowID ?? currentID
    if (!id) {
      saveError.value = t('workflowGraph.noWorkflow')
      return
    }
    saving.value = true
    saveError.value = null
    try {
      await apiPost(`/api/v1/workflows/${id}/update-graph`, {
        name: currentGraph.name,
        nodes: currentGraph.nodes.map((n) => ({
          id: n.id,
          type: n.type,
          params: n.params ?? {},
          branch: n.branch ?? '',
          pos: n.pos ?? null
        })),
        edges: currentGraph.edges.map((e) => ({
          from: e.from,
          to: e.to,
          source_handle: e.source_handle ?? ''
        })),
        outputs: currentGraph.outputs ?? {}
      })
      await load(id)
    } catch (e) {
      saveError.value = e instanceof Error ? e.message : String(e)
    } finally {
      saving.value = false
    }
  }

  return {
    vfNodes,
    vfEdges,
    nodeTypes,
    graphOutputs,
    error,
    loading,
    saving,
    saveError,
    selectedNodeID,
    load,
    connect,
    removeEdge,
    addNode,
    removeNode,
    onNodeMoved,
    selectNode,
    selectedNode,
    updateNodeParams,
    setOutputs,
    autoLayout,
    saveGraph,
    typeLabel,
    nodeDisplayLabel
  }
})
