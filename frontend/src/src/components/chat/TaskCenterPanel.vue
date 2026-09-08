<script setup lang="ts">
/**
 * 后台任务中心。
 *
 * <p>展示 TaskService 中的任务列表（流式事件 task:* 实时追加）；
 * 不阻塞聊天的长任务（调研、批量处理）在此看结果。
 */
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useChatStore } from '@/stores/chat'
import { useToast } from '@/composables/useToast'
import { getApiBase } from '@/api/http'
import { t } from '@/i18n'
import {
  Loader2,
  CircleCheck,
  CircleX,
  Plus,
  ListTree
} from '@/components/common/icons'

const chat = useChatStore()
const { tasks, currentID } = storeToRefs(chat)
const toast = useToast()

const showSubmit = ref(false)
const newPrompt = ref('')
const newAgent = ref('default')

const agentOptions = ['default', 'coding', 'research', 'writer']

const tasksView = computed(() => tasks.value)

function fmtTime(ms: number): string {
  if (!ms) return ''
  const d = new Date(ms)
  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function stateIcon(state: string) {
  switch (state) {
    case 'running':
      return Loader2 as unknown
    case 'completed':
      return CircleCheck as unknown
    case 'failed':
      return CircleX as unknown
    case 'cancelled':
      return ListTree as unknown
    default:
      return ListTree as unknown
  }
}

function stateClass(state: string): string {
  switch (state) {
    case 'running':
      return 'text-wb-info'
    case 'completed':
      return 'text-wb-mint'
    case 'failed':
      return 'text-wb-danger'
    case 'cancelled':
      return 'text-wb-muted'
    default:
      return 'text-wb-warning'
  }
}

async function submitTask(): Promise<void> {
  const prompt = newPrompt.value.trim()
  if (!prompt || !currentID.value) return
  const t1 = await chat.submitTask(currentID.value, newAgent.value, prompt)
  if (t1) {
    toast.success(t('tasks.submitted', t1.id))
    newPrompt.value = ''
    showSubmit.value = false
  } else {
    toast.error(t('tasks.submitFailed'))
  }
}

async function cancel(id: string): Promise<void> {
  if (await chat.cancelTask(id)) {
    toast.info(t('tasks.cancelling'))
  }
}

/**
 * 任务事件长连接：scope=task 且不带 runId → 收全部后台任务生命周期变化。
 * 面板之前只在挂载时拉一次列表，任务跑完界面纹丝不动，看着就像没在跑。
 */
let es: EventSource | null = null
function openTaskStream(): void {
  const base = getApiBase()
  if (!base) return
  es = new EventSource(`${base}/events?scope=task`)
  const refresh = (): void => {
    void chat.loadTasks()
  }
  for (const name of ['task:created', 'task:started', 'task:done']) {
    es.addEventListener(name, refresh)
  }
}

onMounted(() => {
  void chat.loadTasks()
  openTaskStream()
})

onBeforeUnmount(() => {
  es?.close()
  es = null
})
</script>

<template>
  <div class="flex h-full flex-col text-wb-ink">
    <div class="flex shrink-0 items-center justify-between border-b border-wb-border px-3 py-2">
      <div class="flex items-center gap-2">
        <span class="inline-flex h-5 w-5 items-center justify-center rounded-md bg-wb-primary/10 text-wb-primary">
          <ListTree class="h-3 w-3" />
        </span>
        <span class="text-xs font-medium text-wb-ink">{{ t('tasks.title') }}</span>
        <span class="text-[10px] text-wb-muted tabular-nums">{{ tasksView.length }}</span>
      </div>
      <el-button v-if="currentID" size="small" type="primary" plain @click="showSubmit = !showSubmit">
        <el-icon class="mr-1"><Plus /></el-icon>
        <span class="text-[10px]">{{ t('tasks.new') }}</span>
      </el-button>
    </div>

    <!-- 提交表单 -->
    <div v-if="showSubmit" class="shrink-0 space-y-2 border-b border-wb-border bg-wb-surface-2 px-3 py-2">
      <el-input
        v-model="newPrompt"
        type="textarea"
        :rows="3"
        resize="none"
        :placeholder="t('tasks.promptPlaceholder')"
      />
      <div class="flex items-center gap-2">
        <el-select v-model="newAgent" class="!flex-1" size="small">
          <el-option v-for="a in agentOptions" :key="a" :value="a" :label="a" />
        </el-select>
        <el-button size="small" type="primary" :disabled="!newPrompt.trim()" @click="submitTask">
          {{ t('tasks.submit') }}
        </el-button>
      </div>
    </div>

    <!-- 任务列表 -->
    <div class="min-h-0 flex-1 overflow-y-auto p-2">
      <div v-if="tasksView.length === 0" class="px-2 py-8 text-center text-xs text-wb-muted">
        {{ t('tasks.empty') }}
      </div>
      <div v-for="tk in tasksView" :key="tk.id" class="mb-2 rounded-md border border-wb-border bg-wb-surface p-2">
        <div class="flex items-center gap-2">
          <component :is="stateIcon(tk.state)" class="h-3.5 w-3.5 shrink-0" :class="[stateClass(tk.state), tk.state === 'running' ? 'animate-spin' : '']" />
          <span class="truncate text-xs font-medium text-wb-ink">{{ tk.agent }}</span>
          <span class="ml-auto text-[10px] text-wb-muted">{{ fmtTime(tk.created_at) }}</span>
        </div>
        <div class="mt-1 truncate text-[11px] text-wb-ink" :title="tk.prompt">{{ tk.prompt }}</div>
        <div v-if="tk.state === 'running' || tk.state === 'pending'" class="mt-1.5 flex justify-end">
          <el-button size="small" text type="danger" @click="cancel(tk.id)">
            <span class="text-[10px]">{{ t('tasks.cancel') }}</span>
          </el-button>
        </div>
        <div v-else-if="tk.state === 'completed' && tk.result" class="mt-1.5 rounded bg-wb-surface-2 px-2 py-1 text-[10px] text-wb-ink line-clamp-3">
          {{ tk.result }}
        </div>
        <div v-else-if="tk.state === 'failed' && tk.error" class="mt-1.5 rounded bg-wb-danger/5 px-2 py-1 text-[10px] text-wb-danger">
          {{ tk.error }}
        </div>
      </div>
    </div>
  </div>
</template>