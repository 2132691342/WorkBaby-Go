<script setup lang="ts">
/**
 * 工作流 DAG 可视化。
 *
 * <p>调色板与属性面板均由后端 node-types 目录驱动（真实节点类型 + 字段 schema）；
 * condition 节点渲染 true/false 双出口 handle；节点坐标拖拽后持久化到 Graph JSON。
 */
import { computed, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { VueFlow, Position, Handle } from '@vue-flow/core'
import type { Connection } from '@vue-flow/core'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import { useWorkflowGraphStore } from '@/stores/workflowGraph'
import { useWorkflowsStore } from '@/stores/workflows'
import { useChatStore } from '@/stores/chat'
import { useDialog } from '@/composables/useDialog'
import { apiGet } from '@/api/client'
import type { AiProvider, ToolInfo, WorkflowNodeField } from '@/types/api'
import { GitBranch } from '@/components/common/icons'
import { t } from '@/i18n'

const props = defineProps<{ workflow_id: string }>()

const store = useWorkflowGraphStore()
const wfStore = useWorkflowsStore()
const chat = useChatStore() // model 下拉数据源：用户已配置的模型列表
const dialog = useDialog()
const { vfNodes, vfEdges, nodeTypes, graphOutputs, error, loading, saving, saveError, selectedNodeID } = storeToRefs(store)

/** 调色板点击：添加节点并自动选中，让新手能立即在右侧填参数（比「加完找不到」友好）。 */
function onPaletteAdd(type: string): void {
  const id = store.addNode(type)
  if (id) store.selectNode(id)
}

// ===== 执行状态高亮：监听同页 WorkflowsView 发起的运行，把节点执行状态映射到画布 =====
const { currentExec, currentExecNodes } = storeToRefs(wfStore)
const execStateMap = ref<Record<string, string>>({})
watch(
  [currentExec, currentExecNodes],
  ([exec, nodes]) => {
    if (!exec || !nodes) {
      execStateMap.value = {}
      return
    }
    // 只显示属于本工作流的执行，避免上次运行残留误标
    if (exec.workflow_id && exec.workflow_id !== props.workflow_id) {
      execStateMap.value = {}
      return
    }
    const m: Record<string, string> = {}
    for (const n of nodes) m[n.node_id] = n.status
    execStateMap.value = m
  },
  { deep: false }
)

// ===== 图级输出编辑区（outputs: key → nodeId.field） =====
const outputRows = ref<{ key: string; value: string }[]>([])
watch(
  graphOutputs,
  (o) => {
    outputRows.value = Object.entries(o ?? {}).map(([key, value]) => ({ key, value }))
  },
  { deep: false, immediate: true }
)
function addOutputRow(): void {
  outputRows.value.push({ key: '', value: '' })
}
function removeOutputRow(i: number): void {
  outputRows.value.splice(i, 1)
}
/** 保存前把编辑区行写入 store.outputs。 */
function applyOutputs(): void {
  const m: Record<string, string> = {}
  for (const r of outputRows.value) {
    const k = r.key.trim()
    const v = r.value.trim()
    if (k && v) m[k] = v
  }
  store.setOutputs(m)
}
/** 画布保存：先提交 outputs 再调后端。 */
async function onSaveCanvas(): Promise<void> {
  applyOutputs()
  await store.saveGraph(props.workflow_id)
}

// ===== 动态下拉数据（provider / tool 来自真实列表） =====
const providers = ref<AiProvider[]>([])
const toolList = ref<ToolInfo[]>([])
async function loadDynamicOptions(): Promise<void> {
  try {
    providers.value = await apiGet<AiProvider[]>('/api/v1/ai-provider')
  } catch {
    providers.value = []
  }
  try {
    toolList.value = await apiGet<ToolInfo[]>('/api/v1/tools')
  } catch {
    toolList.value = []
  }
}

// ===== 属性面板 =====
const panelLabel = ref('')
const draft = ref<Record<string, unknown>>({})
const feedback = ref<string | null>(null)

const selectedInfo = computed(() => {
  const n = store.selectedNode()
  if (!n) return null
  return nodeTypes.value.find((x) => x.type === n.type) ?? null
})

/** 字段当前值。 */
function fieldValue(f: WorkflowNodeField): unknown {
  return draft.value[f.name] ?? ''
}

watch(selectedNodeID, () => {
  const n = store.selectedNode()
  feedback.value = null
  if (!n) {
    panelLabel.value = ''
    draft.value = {}
    return
  }
  panelLabel.value = typeof n.params?.label === 'string' ? (n.params.label as string) : ''
  const d: Record<string, unknown> = {}
  for (const f of store.nodeTypes.find((x) => x.type === n.type)?.fields ?? []) {
    const v = n.params?.[f.name]
    if (v === undefined || v === null) {
      // 无值：number 留空，json 留空，其余空串
      d[f.name] = f.type === 'number' ? undefined : ''
    } else if (f.type === 'json' && typeof v !== 'string') {
      d[f.name] = JSON.stringify(v, null, 2)
    } else {
      d[f.name] = v
    }
  }
  draft.value = d
})

/** select 选项：schema 静态 options 优先；providerID / toolName / model 走动态数据。 */
function fieldOptions(f: WorkflowNodeField): string[] {
  if (f.options && f.options.length > 0) return f.options
  if (f.name === 'providerID') return providers.value.map((p) => p.id)
  if (f.name === 'toolName') return toolList.value.map((x) => x.name)
  // model 下拉：小白记不住模型名，直接从已配置模型取（跨 provider 去重）
  if (f.name === 'model') return [...new Set(chat.models.map((m) => m.model))]
  return []
}

function onFieldInput(f: WorkflowNodeField, v: unknown): void {
  draft.value[f.name] = v
  feedback.value = null
}

/** 保存节点：schema 校验 + JSON 解析后写入 params 并持久化。 */
async function saveNode(): Promise<void> {
  const n = store.selectedNode()
  if (!n) return
  const fields = store.nodeTypes.find((x) => x.type === n.type)?.fields ?? []
  const params: Record<string, unknown> = { label: panelLabel.value.trim() || store.typeLabel(n.type) }
  for (const f of fields) {
    const raw = draft.value[f.name]
    if (raw === undefined || raw === null || raw === '') continue
    if (f.type === 'number') {
      const num = Number(raw)
      if (Number.isNaN(num)) {
        feedback.value = `${f.label}：${t('wfGraph.numInvalid')}`
        return
      }
      params[f.name] = num
    } else if (f.type === 'json') {
      try {
        params[f.name] = JSON.parse(String(raw))
      } catch {
        feedback.value = `${f.label}：${t('wfGraph.jsonInvalid')}`
        return
      }
    } else {
      params[f.name] = raw
    }
  }
  for (const f of fields) {
    if (f.required && (params[f.name] === undefined || params[f.name] === '')) {
      feedback.value = `${f.label} ${t('wfGraph.required')}`
      return
    }
  }
  store.updateNodeParams(n.id, params)
  await store.saveGraph(props.workflow_id)
}

/** 删除节点：破坏性操作，加二次确认 + 自动保存。
 *  之前直接 removeNode 没二次确认且没立即保存，导致用户以为「删两次才删掉」（其实是切回来未持久化）。 */
async function deleteNode(): Promise<void> {
  const n = store.selectedNode()
  if (!n) return
  const ok = await dialog.confirm({
    title: t('wfGraph.deleteNode'),
    content: t('wfGraph.deleteNodeConfirm', (n.params?.label as string) || n.id),
    danger: true
  })
  if (!ok) return
  store.removeNode(n.id)
  await store.saveGraph(props.workflow_id)
}

// ===== 画布事件 =====
function onConnect(c: Connection): void {
  store.connect(c.source, c.target, c.sourceHandle)
}
function onNodeDragStop(e: { node: { id: string; position: { x: number; y: number } } }): void {
  store.onNodeMoved(e.node.id, e.node.position.x, e.node.position.y)
}

// ===== 节点配色（按真实类型） =====
function kindStyle(kind: string | undefined): { bg: string; border: string; text: string } {
  switch (kind) {
    case 'llm':
      return { bg: 'bg-wb-lavender/10', border: 'border-wb-lavender/60', text: 'text-wb-lavender' }
    case 'tool':
      return { bg: 'bg-wb-sky/10', border: 'border-wb-sky/60', text: 'text-wb-sky' }
    case 'http':
      return { bg: 'bg-wb-primary/10', border: 'border-wb-primary/60', text: 'text-wb-primary-strong' }
    case 'code':
      return { bg: 'bg-wb-mint/10', border: 'border-wb-mint/60', text: 'text-wb-mint' }
    case 'condition':
      return { bg: 'bg-wb-warning/10', border: 'border-wb-warning/60', text: 'text-wb-warning' }
    case 'human_input':
      return { bg: 'bg-wb-info/10', border: 'border-wb-info/60', text: 'text-wb-info' }
    case 'channel':
      return { bg: 'bg-wb-danger/10', border: 'border-wb-danger/60', text: 'text-wb-danger' }
    default:
      return { bg: 'bg-wb-surface', border: 'border-wb-border', text: 'text-wb-ink' }
  }
}

const styledNodes = computed(() =>
  (vfNodes.value ?? []).map((n) => ({
    id: n.id,
    type: n.type ?? 'default',
    position: n.position ?? { x: 0, y: 0 },
    data: { ...(n.data ?? {}), execState: execStateMap.value[n.id] ?? '' },
    style: {
      padding: 0,
      border: 'none',
      background: 'transparent',
      width: '160px'
    }
  }))
)

/** 执行状态 → 节点外框/光晕类。 */
function execRing(state: string | undefined): string {
  switch (state) {
    case 'RUNNING':
      return 'ring-2 ring-wb-primary/70 shadow-[0_0_0_4px_rgba(92,124,250,0.15)]'
    case 'COMPLETED':
      return 'ring-1 ring-wb-success/70'
    case 'FAILED':
      return 'ring-2 ring-wb-danger'
    case 'SKIPPED':
      return 'opacity-45 saturate-50'
    default:
      return ''
  }
}
/** 执行状态 → 状态点颜色。 */
function execDot(state: string | undefined): string {
  switch (state) {
    case 'RUNNING':
      return 'bg-wb-primary animate-pulse'
    case 'COMPLETED':
      return 'bg-wb-success'
    case 'FAILED':
      return 'bg-wb-danger'
    case 'SKIPPED':
      return 'bg-wb-muted'
    default:
      return ''
  }
}

/**
 * 边颜色按节点类型语义映射：值 = CSS 变量字符串，
 * Vue Flow 的 SVG edge 走 DOM 样式解析，var()/color-mix() 随主题即时生效。
 * 人类输入 = sky×primary 混色，通道 = danger×lavender 混色，与 llm/tool 区分。
 */
const EDGE_COLOR: Record<string, string> = {
  llm: 'var(--wb-lavender)',
  tool: 'var(--wb-sky)',
  http: 'var(--wb-primary-strong)',
  code: 'var(--wb-mint)',
  condition: 'var(--wb-lemon)',
  human_input: 'color-mix(in srgb, var(--wb-sky) 55%, var(--wb-primary-strong))',
  channel: 'color-mix(in srgb, var(--wb-danger) 60%, var(--wb-lavender))',
  default: 'var(--wb-muted)'
}

const styledEdges = computed(() =>
  (vfEdges.value ?? []).map((e, i) => {
    const src = (vfNodes.value ?? []).find((n) => n.id === e.source)
    const kind = src?.data?.kind ?? 'default'
    const color = EDGE_COLOR[kind] ?? EDGE_COLOR.default
    return {
      id: `e${i}`,
      source: e.source ?? '',
      target: e.target ?? '',
      sourceHandle: e.sourceHandle ?? null,
      label: e.label || undefined,
      labelStyle: { fontSize: 10, fill: 'var(--wb-muted)' },
      style: { stroke: color, strokeWidth: 2 },
      animated: kind === 'condition',
      data: {}
    }
  })
)

function nodeKind(np: { data?: Record<string, unknown> }): string {
  return (np.data?.kind as string | undefined) ?? 'node'
}
function nodeLabel(np: { data?: Record<string, unknown> }): string {
  return (np.data?.label as string | undefined) ?? ''
}
function nodeExecState(np: { data?: Record<string, unknown> }): string {
  return (np.data?.execState as string | undefined) ?? ''
}

onMounted(() => {
  void store.load(props.workflow_id)
  void loadDynamicOptions()
})
watch(() => props.workflow_id, (id) => {
  void store.load(id)
})
</script>

<template>
  <div class="flex h-full w-full bg-wb-bg">
    <!-- 左侧节点调色板（目录驱动） -->
    <aside class="w-56 overflow-y-auto border-r border-wb-border bg-wb-surface p-3">
      <div class="mb-3 flex items-center justify-between">
        <h3 class="text-xs font-semibold uppercase tracking-wider text-wb-muted">{{ t('wfGraph.palette') }}</h3>
        <span class="rounded bg-wb-primary/5 px-1.5 py-0.5 text-[10px] text-wb-muted">{{ vfNodes.length }}</span>
      </div>
      <div class="space-y-1.5">
        <button
          v-for="k in (nodeTypes ?? [])"
          :key="k.type"
          type="button"
          class="group flex w-full items-center gap-2 rounded-lg border border-wb-border bg-wb-surface px-2.5 py-2 text-left text-xs transition-all hover:border-wb-primary/40 hover:bg-wb-surface-hover"
          :title="t('wfGraph.addNode', store.typeLabel(k.type))"
          @click="onPaletteAdd(k.type)"
        >
          <span class="h-2 w-2 shrink-0 rounded-full" :class="kindStyle(k.type).border" />
          <span class="flex-1 text-wb-ink">{{ store.typeLabel(k.type) }}</span>
          <span class="text-wb-muted opacity-0 transition-opacity group-hover:opacity-100">＋</span>
        </button>
        <p v-if="!nodeTypes || nodeTypes.length === 0" class="pt-1 text-[10px] text-wb-muted">{{ t('wfGraph.noTypes') }}</p>
      </div>
      <button
        type="button"
        class="mt-4 w-full rounded-lg border border-wb-border bg-wb-surface px-2.5 py-1.5 text-xs text-wb-ink transition-colors hover:bg-wb-surface-hover"
        @click="store.autoLayout"
      >
        {{ t('wfGraph.autoLayout') }}
      </button>
      <button
        type="button"
        class="mt-2 w-full rounded-lg bg-wb-primary px-2.5 py-1.5 text-xs font-medium text-white transition-colors hover:bg-wb-primary-strong disabled:opacity-50"
        :disabled="saving"
        @click="onSaveCanvas"
      >
        {{ saving ? t('wfGraph.saving') : t('wfGraph.save') }}
      </button>
      <p v-if="saveError" class="mt-2 text-[10px] text-wb-danger">{{ saveError }}</p>

      <!-- 图级输出（outputs: key → nodeId.field） -->
      <div class="mt-4 border-t border-wb-border pt-3">
        <div class="mb-1 flex items-center justify-between">
          <h3 class="text-xs font-semibold uppercase tracking-wider text-wb-muted">{{ t('wfGraph.outputs') }}</h3>
          <button type="button" class="rounded-md bg-wb-primary/10 px-1.5 text-xs text-wb-primary-strong hover:bg-wb-primary/20" @click="addOutputRow">
            ＋
          </button>
        </div>
        <p class="mb-2 text-[10px] leading-relaxed text-wb-muted">{{ t('wfGraph.outputsHint') }}</p>
        <div v-if="outputRows.length === 0" class="rounded-md border border-dashed border-wb-border px-2 py-2 text-[10px] text-wb-muted">
          {{ t('wfGraph.outputsEmpty') }}
        </div>
        <div v-else class="space-y-1.5">
          <div v-for="(r, i) in outputRows" :key="i" class="grid grid-cols-[1fr_1.5fr_auto] items-center gap-1">
            <input v-model="r.key" :placeholder="t('wfGraph.outputsKey')" class="input font-mono text-[10px]" />
            <input v-model="r.value" :placeholder="t('wfGraph.outputsRef')" class="input font-mono text-[10px]" />
            <button type="button" class="rounded-md border border-wb-border px-1 text-[10px] text-wb-muted hover:bg-wb-danger/10 hover:text-wb-danger" @click="removeOutputRow(i)">
              ✕
            </button>
          </div>
        </div>
      </div>

      <p class="mt-3 text-[10px] leading-relaxed text-wb-muted">{{ t('wfGraph.saveHint') }}</p>
    </aside>

    <!-- 画布 -->
    <div class="flex min-w-0 flex-1 flex-col bg-wb-bg">
      <div class="flex items-center justify-between border-b border-wb-border px-4 py-2">
        <span class="text-xs text-wb-muted">{{ t('wfGraph.dagHint') }}</span>
        <span class="font-mono text-[10px] text-wb-muted">{{ vfNodes.length }} nodes / {{ vfEdges.length }} edges</span>
      </div>
      <div class="relative min-h-0 flex-1">
        <div v-if="error" class="p-3 text-sm text-wb-danger">{{ error }}</div>
        <VueFlow
          v-else-if="!loading"
          :nodes="styledNodes"
          :edges="styledEdges"
          fit-view-on-init
          :min-zoom="0.2"
          :max-zoom="2"
          delete-key-code="Delete"
          class="vue-flow"
          @connect="onConnect"
          @node-click="(e) => store.selectNode(e.node.id)"
          @node-drag-stop="onNodeDragStop"
        >
          <template #node-default="nodeProps">
            <div
              class="relative rounded-lg border transition-colors"
              :class="[
                kindStyle(nodeKind(nodeProps)).bg,
                kindStyle(nodeKind(nodeProps)).border,
                kindStyle(nodeKind(nodeProps)).text,
                execRing(nodeExecState(nodeProps))
              ]"
            >
              <!-- 输入口 -->
              <Handle type="target" :position="Position.Left" class="!h-2.5 !w-2.5" />
              <div class="flex items-center gap-1.5 border-b border-wb-border/40 px-2.5 py-1.5">
                <span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="kindStyle(nodeKind(nodeProps)).border" />
                <span class="text-[10px] font-mono uppercase tracking-wider opacity-70">
                  {{ nodeKind(nodeProps) }}
                </span>
                <!-- 执行状态点（右上角；仅运行中有意义） -->
                <span v-if="execDot(nodeExecState(nodeProps))" class="ml-auto h-1.5 w-1.5 rounded-full" :class="execDot(nodeExecState(nodeProps))" />
              </div>
              <div class="px-3 py-2 text-center">
                <div class="truncate text-xs font-semibold">{{ nodeLabel(nodeProps) }}</div>
              </div>

              <!-- 输出口：condition 双分支（true / false），其余单出口 -->
              <template v-if="nodeKind(nodeProps) === 'condition'">
                <Handle
                  type="source"
                  :position="Position.Right"
                  id="true"
                  class="!h-2.5 !w-2.5 !bg-wb-success"
                  :style="{ top: '32%' }"
                />
                <span class="pointer-events-none absolute right-2 top-[22%] text-[9px] font-mono text-wb-success">T</span>
                <Handle
                  type="source"
                  :position="Position.Right"
                  id="false"
                  class="!h-2.5 !w-2.5 !bg-wb-danger"
                  :style="{ top: '72%' }"
                />
                <span class="pointer-events-none absolute right-2 top-[64%] text-[9px] font-mono text-wb-danger">F</span>
              </template>
              <Handle v-else type="source" :position="Position.Right" class="!h-2.5 !w-2.5" />
            </div>
          </template>
        </VueFlow>

        <!-- 空画布新手引导：loading 结束后无节点时展示（点左侧添加节点即消失） -->
        <div
          v-if="!loading && !error && vfNodes.length === 0"
          class="pointer-events-none absolute inset-0 flex items-center justify-center"
        >
          <div class="pointer-events-auto w-80 rounded-2xl border border-dashed border-wb-border bg-wb-surface/85 p-5 text-center backdrop-blur-sm">
            <div class="mx-auto flex h-10 w-10 items-center justify-center rounded-xl bg-wb-primary/10 text-wb-primary">
              <GitBranch class="h-5 w-5" />
            </div>
            <h3 class="mt-3 text-sm font-semibold text-wb-ink">{{ t('wfGraph.emptyTitle') }}</h3>
            <ol class="mt-3 space-y-1.5 text-left text-xs text-wb-muted">
              <li>{{ t('wfGraph.emptyStep1') }}</li>
              <li>{{ t('wfGraph.emptyStep2') }}</li>
              <li>{{ t('wfGraph.emptyStep3') }}</li>
            </ol>
            <p class="mt-3 rounded-lg bg-wb-primary/5 px-3 py-1.5 text-[11px] text-wb-primary-strong">
              {{ t('wfGraph.emptyStart') }}
            </p>
          </div>
        </div>
      </div>
    </div>

    <!-- 节点属性面板（schema 驱动） -->
    <aside v-if="selectedNodeID" class="w-80 shrink-0 overflow-y-auto border-l border-wb-border bg-wb-primary/[0.04] p-3">
      <div class="mb-3 flex items-center justify-between">
        <h3 class="text-xs font-semibold uppercase tracking-wider text-wb-muted">{{ t('wfGraph.panelTitle') }}</h3>
        <div class="flex items-center gap-2">
          <span
            v-if="store.selectedNode()?.branch"
            class="rounded bg-wb-warning/10 px-1.5 py-0.5 text-[10px] font-medium text-wb-warning"
            :title="t('wfGraph.branchHint')"
          >
            {{ t('wfGraph.branchOf') }} {{ store.selectedNode()?.branch }}
          </span>
          <button
            type="button"
            class="rounded-md px-2 py-0.5 text-xs text-wb-muted hover:bg-wb-surface-hover hover:text-wb-ink"
            @click="store.selectNode(null)"
          >
            ✕
          </button>
        </div>
      </div>

      <!-- 显示名 -->
      <label class="block text-xs text-wb-muted">
        {{ t('wfGraph.label') }}
        <input v-model="panelLabel" class="input mt-1" :placeholder="store.typeLabel(store.selectedNode()?.type ?? '')" />
      </label>

      <!-- schema 字段 -->
      <template v-if="selectedInfo">
        <div
          v-for="f in selectedInfo.fields"
          :key="f.name"
          class="mt-3"
        >
          <div class="mb-1 flex items-baseline justify-between">
            <span class="text-xs font-medium text-wb-ink">
              {{ f.label }}
              <span v-if="f.required" class="text-wb-danger">*</span>
            </span>
            <span v-if="f.type === 'json'" class="font-mono text-[9px] text-wb-muted">JSON</span>
          </div>
          <!-- number -->
          <el-input-number
            v-if="f.type === 'number'"
            :model-value="fieldValue(f) as number | undefined"
            :min="f.name === 'temperature' ? 0 : undefined"
            :max="f.name === 'temperature' ? 2 : undefined"
            :step="0.1"
            controls-position="right"
            class="!w-full"
            @update:model-value="(v: number | undefined) => onFieldInput(f, v)"
          />
          <!-- select（model 虽为 string 类型也走下拉：小白记不住模型名） -->
          <el-select
            v-else-if="f.type === 'select' || f.name === 'model'"
            :model-value="fieldValue(f) as string"
            clearable
            filterable
            class="!w-full"
            :placeholder="f.name === 'providerID' ? t('wfGraph.placeholderProvider') : f.name === 'toolName' ? t('wfGraph.placeholderTool') : ''"
            @update:model-value="(v: string) => onFieldInput(f, v)"
          >
            <el-option v-for="o in fieldOptions(f)" :key="o" :value="o" :label="o" />
          </el-select>
          <!-- textarea / json -->
          <el-input
            v-else-if="f.type === 'textarea' || f.type === 'json'"
            :model-value="fieldValue(f) as string"
            type="textarea"
            :rows="f.type === 'json' ? 5 : 4"
            resize="vertical"
            class="!w-full font-mono"
            @update:model-value="(v: string) => onFieldInput(f, v)"
          />
          <!-- text -->
          <el-input
            v-else
            :model-value="fieldValue(f) as string"
            class="!w-full"
            @update:model-value="(v: string) => onFieldInput(f, v)"
          />
          <p v-if="f.description" class="mt-1 text-[10px] leading-relaxed text-wb-muted">{{ f.description }}</p>
        </div>
      </template>
      <p v-else class="mt-3 text-[10px] text-wb-muted">{{ t('wfGraph.noSchema') }}</p>

      <p v-if="feedback" class="mt-3 rounded-md bg-wb-danger/10 px-2 py-1 text-xs text-wb-danger">{{ feedback }}</p>

      <div class="mt-4 flex gap-2">
        <button
          type="button"
          class="flex-1 rounded-lg bg-wb-primary px-2.5 py-1.5 text-xs font-medium text-white hover:bg-wb-primary-strong disabled:opacity-50"
          :disabled="saving"
          @click="saveNode"
        >
          {{ saving ? t('wfGraph.saving') : t('wfGraph.save') }}
        </button>
        <button
          type="button"
          class="rounded-lg bg-wb-danger/10 px-2.5 py-1.5 text-xs text-wb-danger hover:bg-wb-danger/20"
          @click="deleteNode"
        >
          {{ t('wfGraph.deleteNode') }}
        </button>
      </div>
      <p v-if="saveError" class="mt-2 text-[10px] text-wb-danger">{{ saveError }}</p>
      <p class="mt-3 text-[10px] leading-relaxed text-wb-muted">{{ t('wfGraph.panelHint') }}</p>
    </aside>
  </div>
</template>

<style scoped>
.vue-flow {
  background: radial-gradient(circle at center, rgb(148 163 184 / 0.15), rgb(241 245 249));
}
/* 节点输入/输出口：去掉默认灰色，改用父元素主题色（用 currentColor 继承 kind 色） */
:deep(.vue-flow__handle) {
  background-color: var(--wb-border-strong);
}
</style>
