<script setup lang="ts">
import { onMounted, computed, defineAsyncComponent, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { GitBranch, Plus, Play, Eye, Save, Trash2, X, Activity } from '@/components/common/icons'
import { useWorkflowsStore } from '@/stores/workflows'
import { WORKFLOW_TEMPLATES } from '@/components/workflows/workflowTemplates'
import { useAsyncAction } from '@/composables/useAsyncAction'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast'
import { apiPost } from '@/api/client'
import { t } from '@/i18n'
// Vue Flow 编辑器体积较大，按需加载（「动态 import」）
const WorkflowGraphView = defineAsyncComponent(() => import('@/components/workflows/WorkflowGraphView.vue'))

/**
 * 工作流管理页：左侧工作流卡片列表 + 右侧可视化编辑器 / JSON 定义 / 执行抽屉。
 * 小白友好：新建对话框内置模板一键起步（AI 写手 / 网页摘要 / 目录速览）。
 */
const store = useWorkflowsStore()
const dialog = useDialog()
const toast = useToast()
const {
  workflows,
  selected,
  executions,
  editing,
  error,
  currentExec,
  pendingInput,
  showCreate,
  showGraph,
  newName,
  newDesc
} = storeToRefs(store)
const {
  loadList,
  select,
  save,
  doCreate,
  doDelete,
  doResume,
  doPause,
  doCancel,
  submitInput
} = store

const running = useAsyncAction(() => store.runWorkflow())

/** 人工输入草稿；提交成功后清空。 */
const inputDraft = ref('')

/** 提交人工输入：放行阻塞在 HumanInput 节点上的执行。 */
async function sendInput(): Promise<void> {
  const p = pendingInput.value
  if (!p) return
  const ok = await submitInput(p.executionID, p.nodeID, inputDraft.value.trim())
  if (ok) inputDraft.value = ''
}

// （用户原话：「没必要为我是展示执行框，编辑区域太小」）：
// 执行面板从与编辑器对半分的 grid 改为右侧可折叠抽屉，编辑器默认全宽；运行时自动弹出。
const showExec = ref(false)
watch(currentExec, (e) => {
  if (e) showExec.value = true
})

const canRun = computed(() => selected.value && selected.value.enabled)
const enabledCount = computed(() => workflows.value.filter((w) => w.enabled).length)

/** 新建对话框选中的模板 id；null = 空白工作流。再次点击已选中项可取消选择。 */
const pickedTemplate = ref<string | null>(null)

function toggleTemplate(id: string): void {
  pickedTemplate.value = pickedTemplate.value === id ? null : id
}

/** 打开新建对话框（可预选模板，供空态快捷卡片复用）。 */
function openCreate(templateId?: string): void {
  pickedTemplate.value = templateId ?? null
  showCreate.value = true
}

/** 创建：选了模板则预填 Graph，否则走空白/编辑器底稿。 */
async function createWorkflow(): Promise<void> {
  const tpl = WORKFLOW_TEMPLATES.find((x) => x.id === pickedTemplate.value)
  await doCreate(tpl?.graph)
  pickedTemplate.value = null
}

/** 状态徽标：el-tag type。 */
function statusType(s: string): 'success' | 'danger' | 'warning' | 'info' | 'primary' {
  switch (s) {
    case 'completed':
      return 'success'
    case 'failed':
      return 'danger'
    case 'running':
      return 'warning'
    case 'paused':
      return 'info'
    case 'cancelled':
      return 'primary'
    default:
      return 'primary'
  }
}

/** 执行输出结构化展示：把 outputs 对象拆成 key/value 行，替代裸 JSON.stringify。 */
const execOutputRows = computed(() => {
  const o = currentExec.value?.outputs
  if (!o || typeof o !== 'object') return []
  return Object.entries(o as Record<string, unknown>).map(([k, v]) => ({
    key: k,
    value: typeof v === 'string' ? v : JSON.stringify(v)
  }))
})

/** 删除带 confirm（替代 silent delete）。 */
async function confirmDelete(): Promise<void> {
  if (!selected.value) return
  const ok = await dialog.confirm({
    title: t('workflow.deleteTitle'),
    content: t('workflow.deleteConfirm', selected.value.name),
    danger: true
  })
  if (ok) {
    try {
      await doDelete(selected.value)
      toast.success(t('workflow.deletedSuccess'))
    } catch (e) {
      toast.error(t('workflow.deleteFailed'), e instanceof Error ? e.message : String(e))
    }
  }
}

/** 格式化 JSON 文本（本地美化）。 */
function formatJson(): void {
  if (!editing.value.trim()) return
  try {
    editing.value = JSON.stringify(JSON.parse(editing.value), null, 2)
    toast.success(t('workflow.jsonValid'))
  } catch {
    toast.error(t('workflow.jsonInvalid'))
  }
}

/** 校验 JSON 定义（调后端 validate，覆盖语义错误如环/必填缺失）。 */
async function validateJson(): Promise<void> {
  if (!editing.value.trim()) return
  try {
    await apiPost('/api/v1/workflows/validate', { graph: editing.value })
    toast.success(t('workflow.jsonValid'))
  } catch (e) {
    toast.error(t('workflow.jsonInvalid'), e instanceof Error ? e.message : String(e))
  }
}

onMounted(loadList)
</script>

<template>
  <div class="wb-ui flex h-full flex-col bg-wb-bg text-wb-ink">
    <!-- 顶部 hero（原型 wf-hero） -->
    <header class="wf-hero">
      <div class="tile"><GitBranch class="ic" /></div>
      <div>
        <h1 style="font-size: 15px; font-weight: 600">{{ t('workflow.title') }}</h1>
        <div class="flex-r fs11 muted" style="gap: 6px">
          <span>{{ t('workflow.count', workflows.length) }}</span>
          <span>·</span>
          <span style="color: var(--wb-success)">{{ t('workflow.enabledCount', enabledCount) }}</span>
        </div>
      </div>
      <span class="sp" />
      <button class="btn btn-primary" @click="openCreate()">
        <Plus class="ic ic-sm" />
        <span>{{ t('workflow.new') }}</span>
      </button>
    </header>

    <div class="flex min-h-0 flex-1">
      <!-- 左侧：列表（原型 wf-list / wf-item） -->
      <aside class="wf-list">
        <div v-if="workflows.length === 0" class="empty">{{ t('workflow.empty') }}</div>
        <button
          v-for="w in workflows"
          :key="w.id"
          class="wf-item"
          :class="{ on: selected?.id === w.id }"
          @click="select(w)"
        >
          <div class="flex-r mb8">
            <h4 style="flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ w.name }}</h4>
            <span class="led" :class="w.enabled ? 'g' : 'n'" />
          </div>
          <p v-if="w.description">{{ w.description }}</p>
        </button>
      </aside>

      <!-- 右侧：编辑器 + 执行 -->
      <main class="flex min-w-0 flex-1 flex-col">
        <!-- 空列表：整页引导 + 模板一键起步 -->
        <div v-if="!selected && workflows.length === 0" class="flex flex-1 items-center justify-center px-8">
          <div class="w-full max-w-lg rounded-2xl border border-dashed border-wb-border bg-wb-surface/70 p-10 text-center">
            <div class="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-wb-primary/10 text-wb-primary">
              <GitBranch class="h-7 w-7" />
            </div>
            <h3 class="mt-4 font-display text-base font-semibold text-wb-ink">{{ t('workflow.emptyGuideTitle') }}</h3>
            <p class="mt-2 text-xs leading-relaxed text-wb-muted">{{ t('workflow.emptyGuideBody') }}</p>
            <el-button type="primary" class="mt-5" @click="openCreate()">
              <Plus class="h-4 w-4" />
              <span>{{ t('workflow.new') }}</span>
            </el-button>
            <!-- 模板一键起步：比「从零拖节点」对小白的门槛低一个量级 -->
            <div class="mt-6 grid grid-cols-1 gap-2 sm:grid-cols-3">
              <button
                v-for="tpl in WORKFLOW_TEMPLATES"
                :key="tpl.id"
                type="button"
                class="group flex flex-col items-center gap-2 rounded-xl border border-wb-border bg-wb-surface px-3 py-4 transition-all hover:border-wb-primary/40 hover:shadow-sm"
                @click="openCreate(tpl.id)"
              >
                <span class="inline-flex h-9 w-9 items-center justify-center rounded-lg bg-wb-primary/10 text-wb-primary">
                  <component :is="tpl.icon" class="h-4.5 w-4.5" />
                </span>
                <span class="text-xs font-medium text-wb-ink">{{ t(tpl.nameKey) }}</span>
                <span class="text-[11px] leading-snug text-wb-muted">{{ t(tpl.descKey) }}</span>
              </button>
            </div>
          </div>
        </div>
        <div v-else-if="!selected" class="flex flex-1 items-center justify-center text-wb-muted">
          <div class="text-center">
            <GitBranch class="mx-auto h-12 w-12 text-wb-muted/40" />
            <p class="mt-3 text-sm">{{ t('workflow.selectHint') }}</p>
          </div>
        </div>
        <template v-else>
          <div class="flex items-center gap-2 border-b border-wb-border bg-wb-surface/40 px-5 py-3">
            <div class="font-display font-semibold text-wb-ink">{{ selected.name }}</div>
            <div class="ml-auto flex gap-2">
              <el-button type="primary" :disabled="!canRun || running.pending.value" :loading="running.pending.value" @click="running.run()">
                <Play class="h-3.5 w-3.5" />
                <span>{{ running.pending.value ? t('workflow.running') : t('workflow.run') }}</span>
              </el-button>
              <el-button @click="showGraph = !showGraph">
                <Eye class="h-3.5 w-3.5" />
                <span>{{ showGraph ? t('workflow.text') : t('workflow.visual') }}</span>
              </el-button>
              <el-button :type="showExec ? 'primary' : 'default'" @click="showExec = !showExec">
                <Activity class="h-3.5 w-3.5" />
                <span>{{ t('workflow.execution') }}</span>
              </el-button>
              <el-button @click="save">
                <Save class="h-3.5 w-3.5" />
                <span>{{ t('workflow.save') }}</span>
              </el-button>
              <el-button type="danger" @click="confirmDelete">
                <Trash2 class="h-3.5 w-3.5" />
                <span>{{ t('workflow.delete') }}</span>
              </el-button>
            </div>
          </div>

          <div v-if="error" class="border-b border-wb-danger/30 bg-wb-danger/10 px-5 py-2 text-sm text-wb-danger">
            {{ error }}
          </div>

          <div class="flex min-h-0 flex-1 gap-3 p-3">
            <!-- YAML 编辑器 / 可视化 -->
            <div class="flex min-w-0 flex-1 flex-col overflow-hidden rounded-xl border border-wb-border bg-wb-surface">
              <div class="flex items-center justify-between border-b border-wb-border px-4 py-2 text-xs text-wb-muted">
                <span>{{ showGraph ? t('workflow.dag') : t('workflow.definitionJson') }}</span>
                <span class="flex items-center gap-2">
                  <template v-if="!showGraph">
                    <button type="button" class="rounded bg-wb-primary/10 px-2 py-0.5 text-wb-primary-strong hover:bg-wb-primary/20" @click="formatJson">
                      {{ t('workflow.format') }}
                    </button>
                    <button type="button" class="rounded bg-wb-success/10 px-2 py-0.5 text-wb-success hover:bg-wb-success/20" @click="validateJson">
                      {{ t('workflow.validate') }}
                    </button>
                  </template>
                  <span class="text-[10px]">{{ t('workflow.ctrlS') }}</span>
                </span>
              </div>
              <WorkflowGraphView v-if="showGraph && selected" :workflow_id="selected.id" class="min-h-0 flex-1" />
              <el-input
                v-else
                v-model="editing"
                type="textarea"
                resize="none"
                spellcheck="false"
                class="yaml-editor"
              />
            </div>

            <!-- 执行抽屉（可折叠；运行时自动弹出） -->
            <aside v-if="showExec" class="flex w-80 shrink-0 flex-col overflow-hidden rounded-xl border border-wb-border bg-wb-surface">
              <div class="flex items-center justify-between border-b border-wb-border px-4 py-2 text-xs text-wb-muted">
                <span>{{ t('workflow.execution') }}</span>
                <div class="flex items-center gap-2">
                  <el-tag v-if="currentExec" size="small" :type="statusType(currentExec.status)" effect="plain">
                    {{ currentExec.status }}
                  </el-tag>
                  <el-button link size="small" @click="showExec = false">
                    <X class="h-3.5 w-3.5" />
                  </el-button>
                </div>
              </div>
              <div class="min-h-0 flex-1 overflow-auto p-4 text-xs">
                <div v-if="!currentExec" class="text-wb-muted">{{ t('workflow.runHint') }}</div>
                <template v-else>
                  <div v-if="currentExec.error_msg" class="mb-3 rounded-md bg-wb-danger/10 px-2 py-1.5 text-wb-danger">
                    {{ currentExec.error_msg }}
                  </div>
                  <!-- 输出结构化展示：每个输出变量一张卡，替代裸 JSON 串 -->
                  <div v-if="execOutputRows.length" class="space-y-2">
                    <div
                      v-for="row in execOutputRows"
                      :key="row.key"
                      class="rounded-lg border border-wb-border bg-wb-surface/60 p-2"
                    >
                      <div class="mb-1 font-mono text-[10px] uppercase tracking-wide text-wb-muted">{{ row.key }}</div>
                      <div class="whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed text-wb-ink">{{ row.value }}</div>
                    </div>
                  </div>
                  <div v-else-if="currentExec.status === 'completed'" class="text-wb-muted">
                    {{ t('workflow.noOutput') }}
                  </div>
                  <!-- 人工输入：HumanInput 节点等待时，阻塞的执行必须由这里放行 -->
                  <div
                    v-if="pendingInput"
                    class="mb-3 rounded-lg border border-wb-primary/40 bg-wb-primary/5 p-2"
                  >
                    <div class="mb-1.5 text-[11px] font-medium text-wb-primary-strong">
                      {{ t('workflow.inputRequiredTitle') }}
                    </div>
                    <div v-if="pendingInput.prompt" class="mb-2 whitespace-pre-wrap break-words text-[11px] leading-relaxed text-wb-ink">
                      {{ pendingInput.prompt }}
                    </div>
                    <el-input
                      v-model="inputDraft"
                      type="textarea"
                      :rows="3"
                      resize="none"
                      :placeholder="t('workflow.inputPlaceholder')"
                    />
                    <div class="mt-2 flex justify-end">
                      <el-button type="primary" size="small" :disabled="!inputDraft.trim()" @click="sendInput">
                        {{ t('workflow.inputSubmit') }}
                      </el-button>
                    </div>
                  </div>
                  <div class="mt-3 flex gap-2">
                    <el-button v-if="currentExec.status === 'paused'" type="primary" size="small" @click="doResume">
                      {{ t('workflow.resume') }}
                    </el-button>
                    <el-button v-if="currentExec.status === 'running'" size="small" @click="doPause">
                      {{ t('workflow.pause') }}
                    </el-button>
                    <el-button
                      v-if="!['completed', 'failed', 'cancelled'].includes(currentExec.status)"
                      type="danger"
                      size="small"
                      @click="doCancel"
                    >
                      {{ t('workflow.cancel') }}
                    </el-button>
                  </div>
                </template>
              </div>

              <details class="border-t border-wb-border">
                <summary class="cursor-pointer px-4 py-2 text-xs text-wb-muted hover:bg-wb-primary/5">
                  {{ t('workflow.recent', executions.length) }}
                </summary>
                <ul class="space-y-1 p-2 text-xs">
                  <li
                    v-for="e in executions"
                    :key="e.id"
                    class="flex items-center justify-between rounded-md bg-wb-surface/60 px-2 py-1"
                  >
                    <span class="truncate font-mono">{{ e.id.slice(0, 12) }}…</span>
                    <el-tag size="small" :type="statusType(e.status)" effect="plain">{{ e.status }}</el-tag>
                  </li>
                </ul>
              </details>
            </aside>
          </div>
        </template>
      </main>
    </div>

    <!-- 新建对话框：模板一键起步 + 名称/描述 -->
    <el-dialog v-model="showCreate" :title="t('workflow.newTitle')" width="480px" append-to-body>
      <div class="mb-4">
        <div class="mb-2 text-xs font-medium text-wb-ink">{{ t('workflow.tplPick') }}</div>
        <div class="grid grid-cols-2 gap-2">
          <button
            type="button"
            class="flex items-center gap-2 rounded-xl border p-3 text-left transition-all"
            :class="pickedTemplate === null ? 'border-wb-primary/50 bg-wb-primary/10' : 'border-wb-border bg-wb-surface hover:border-wb-primary/30'"
            @click="pickedTemplate = null"
          >
            <Plus class="h-4 w-4 shrink-0 text-wb-primary" />
            <span class="text-xs font-medium text-wb-ink">{{ t('workflow.tplBlank') }}</span>
          </button>
          <button
            v-for="tpl in WORKFLOW_TEMPLATES"
            :key="tpl.id"
            type="button"
            class="flex items-center gap-2 rounded-xl border p-3 text-left transition-all"
            :class="pickedTemplate === tpl.id ? 'border-wb-primary/50 bg-wb-primary/10' : 'border-wb-border bg-wb-surface hover:border-wb-primary/30'"
            @click="toggleTemplate(tpl.id)"
          >
            <component :is="tpl.icon" class="h-4 w-4 shrink-0 text-wb-primary" />
            <span class="min-w-0">
              <span class="block truncate text-xs font-medium text-wb-ink">{{ t(tpl.nameKey) }}</span>
              <span class="block truncate text-[10px] text-wb-muted">{{ t(tpl.descKey) }}</span>
            </span>
          </button>
        </div>
      </div>
      <el-form class="wb-el-form" label-position="top" @submit.prevent="createWorkflow">
        <el-form-item :label="t('workflow.name')">
          <el-input v-model="newName" :placeholder="t('workflow.name')" />
        </el-form-item>
        <el-form-item :label="t('workflow.desc')">
          <el-input v-model="newDesc" :placeholder="t('workflow.desc')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreate = false">{{ t('ui.btn.cancel') }}</el-button>
        <el-button type="primary" @click="createWorkflow">{{ t('ui.btn.create') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
/* YAML 编辑器：等宽字体 + 撑满剩余高度 */
.yaml-editor {
  height: 100%;
  font-family: var(--font-mono);
  font-size: 12px;
}
.yaml-editor :deep(.el-textarea__inner) {
  height: 100%;
  background: transparent;
  box-shadow: none;
  color: var(--wb-ink);
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.6;
}
</style>
