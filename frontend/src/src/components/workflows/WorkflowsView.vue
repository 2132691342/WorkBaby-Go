<script setup lang="ts">
import { onMounted, computed, defineAsyncComponent, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { GitBranch, Plus, Play, Eye, Save, Trash2, X, Activity } from '@/components/common/icons'
import { useWorkflowsStore } from '@/stores/workflows'
import { useAsyncAction } from '@/composables/useAsyncAction'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast'
import { apiPost } from '@/api/client'
import { t } from '@/i18n'
// Vue Flow 编辑器体积较大，按需加载（「动态 import」）
const WorkflowGraphView = defineAsyncComponent(() => import('@/components/workflows/WorkflowGraphView.vue'))

/**
 * 工作流管理页：左侧工作流卡片列表 + 右侧 JSON 定义 / 可视化编辑器 / 执行抽屉。
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
  doCancel
} = store

const running = useAsyncAction(() => store.runWorkflow())

// （用户原话：「没必要为我是展示执行框，编辑区域太小」）：
// 执行面板从与编辑器对半分的 grid 改为右侧可折叠抽屉，编辑器默认全宽；运行时自动弹出。
const showExec = ref(false)
watch(currentExec, (e) => {
  if (e) showExec.value = true
})

const canRun = computed(() => selected.value && selected.value.enabled)
const enabledCount = computed(() => workflows.value.filter((w) => w.enabled).length)

/** 状态徽标：el-tag type。 */
function statusType(s: string): 'success' | 'danger' | 'warning' | 'info' | 'primary' {
  switch (s) {
    case 'COMPLETED':
      return 'success'
    case 'FAILED':
      return 'danger'
    case 'RUNNING':
      return 'warning'
    case 'PAUSED':
      return 'info'
    case 'CANCELLED':
      return 'primary'
    default:
      return 'primary'
  }
}

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
  <div class="flex h-full flex-col bg-wb-bg text-wb-ink">
    <!-- 顶部 hero -->
    <header class="flex items-center justify-between gap-3 border-b border-wb-border bg-wb-surface/40 px-6 py-4">
      <div class="flex items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-wb-primary/10 text-wb-primary">
          <GitBranch class="h-5 w-5" />
        </div>
        <div>
          <h1 class="font-display text-base font-semibold text-wb-ink">{{ t('workflow.title') }}</h1>
          <div class="flex items-center gap-2 text-xs text-wb-muted">
            <span>{{ t('workflow.count', workflows.length) }}</span>
            <span>·</span>
            <span class="text-wb-success">{{ t('workflow.enabledCount', enabledCount) }}</span>
          </div>
        </div>
      </div>
      <el-button type="primary" @click="showCreate = true">
        <Plus class="h-4 w-4" />
        <span>{{ t('workflow.new') }}</span>
      </el-button>
    </header>

    <div class="flex min-h-0 flex-1">
      <!-- 左侧：列表 -->
      <aside class="w-72 shrink-0 overflow-y-auto border-r border-wb-border bg-wb-surface/40 p-3">
        <div v-if="workflows.length === 0" class="rounded-lg border border-dashed border-wb-border p-6 text-center text-xs text-wb-muted">
          {{ t('workflow.empty') }}
        </div>
        <ul class="space-y-2">
          <li v-for="w in workflows" :key="w.id">
            <button
              class="group w-full rounded-xl border p-3 text-left transition-all"
              :class="
                selected?.id === w.id
                  ? 'border-wb-primary/40 bg-wb-primary/10'
                  : 'border-wb-border bg-wb-surface/40 hover:border-wb-primary/30 hover:bg-wb-surface/70'
              "
              @click="select(w)"
            >
              <div class="flex items-center gap-2">
                <span
                  class="inline-block h-2 w-2 shrink-0 rounded-full"
                  :class="w.enabled ? 'bg-wb-success' : 'bg-wb-muted'"
                />
                <span class="flex-1 truncate text-sm font-medium text-wb-ink">{{ w.name }}</span>
              </div>
              <div v-if="w.description" class="mt-1 truncate text-xs text-wb-muted">
                {{ w.description }}
              </div>
              <div class="mt-1 flex items-center justify-between text-[10px] text-wb-muted">
                <span class="font-mono">{{ w.id.slice(0, 8) }}…</span>
                <span v-if="w.enabled" class="badge-success">{{ t('workflow.enabled') }}</span>
                <span v-else class="badge-neutral">{{ t('workflow.disabled') }}</span>
              </div>
            </button>
          </li>
        </ul>
      </aside>

      <!-- 右侧：编辑器 + 执行 -->
      <main class="flex min-w-0 flex-1 flex-col">
        <!-- 空列表：整页引导（而非左侧一行小字 + 右侧选择提示），第一次使用也能看懂做什么 -->
        <div v-if="!selected && workflows.length === 0" class="flex flex-1 items-center justify-center px-8">
          <div class="w-full max-w-md rounded-2xl border border-dashed border-wb-border bg-wb-surface/70 p-10 text-center">
            <div class="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-wb-primary/10 text-wb-primary">
              <GitBranch class="h-7 w-7" />
            </div>
            <h3 class="mt-4 font-display text-base font-semibold text-wb-ink">{{ t('workflow.emptyGuideTitle') }}</h3>
            <p class="mt-2 text-xs leading-relaxed text-wb-muted">{{ t('workflow.emptyGuideBody') }}</p>
            <p class="mt-3 text-xs leading-relaxed text-wb-muted">{{ t('workflow.emptyGuideStep') }}</p>
            <el-button type="primary" class="mt-5" @click="showCreate = true">
              <Plus class="h-4 w-4" />
              <span>{{ t('workflow.new') }}</span>
            </el-button>
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
            <div class="font-mono text-xs text-wb-muted">{{ selected.id.slice(0, 8) }}…</div>
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
              <div class="min-h-0 flex-1 overflow-auto p-4 font-mono text-xs">
                <div v-if="!currentExec" class="text-wb-muted">{{ t('workflow.runHint') }}</div>
                <template v-else>
                  <div class="mb-2 text-wb-muted">id: {{ currentExec.id }}</div>
                  <div v-if="currentExec.error_msg" class="mb-2 text-wb-danger">err: {{ currentExec.error_msg }}</div>
                  <div v-if="currentExec.outputs" class="mb-2 text-wb-success">
                    output: {{ JSON.stringify(currentExec.outputs) }}
                  </div>
                  <div class="mt-3 flex gap-2">
                    <el-button v-if="currentExec.status === 'PAUSED'" type="primary" size="small" @click="doResume">
                      {{ t('workflow.resume') }}
                    </el-button>
                    <el-button v-if="currentExec.status === 'RUNNING'" size="small" @click="doPause">
                      {{ t('workflow.pause') }}
                    </el-button>
                    <el-button
                      v-if="!['COMPLETED', 'FAILED', 'CANCELLED'].includes(currentExec.status)"
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

    <!-- 新建对话框（el-dialog） -->
    <el-dialog v-model="showCreate" :title="t('workflow.newTitle')" width="420px" append-to-body>
      <el-form class="wb-el-form" label-position="top" @submit.prevent="doCreate">
        <el-form-item :label="t('workflow.name')">
          <el-input v-model="newName" :placeholder="t('workflow.name')" />
        </el-form-item>
        <el-form-item :label="t('workflow.desc')">
          <el-input v-model="newDesc" :placeholder="t('workflow.desc')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreate = false">{{ t('ui.btn.cancel') }}</el-button>
        <el-button type="primary" @click="doCreate">{{ t('ui.btn.create') }}</el-button>
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