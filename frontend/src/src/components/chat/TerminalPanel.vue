<script setup lang="ts">
/**
 * 终端面板（右侧栏）：每会话一个持久 cmd.exe（cd / 环境变量跨命令保留）。
 *
 * <p>输出走增量轮询（since 游标），输入写一行命令；中文经后端 GBK 编解码，
 * 前端只见 UTF-8。ANSI 转义序列在渲染前剥离。
 */
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Terminal, RefreshCw, Square } from '@/components/common/icons'
import { apiGet, apiPost } from '@/api/client'
import { useChatStore } from '@/stores/chat'
import { t } from '@/i18n'

interface TerminalInfo { id: string; session_id: string; cwd: string }
interface TerminalOutput { seq: number; output: string; closed: boolean }

const chat = useChatStore()
const sessionId = computed(() => chat.currentID ?? '')

const term = ref<TerminalInfo | null>(null)
const output = ref('')
const since = ref(0)
const closed = ref(false)
const input = ref('')
const starting = ref(false)
const outEl = ref<HTMLElement | null>(null)
let pollTimer: number | null = null

/** 剥离 ANSI 转义（cmd 偶发颜色序列）。 */
function stripAnsi(s: string): string {
  return s.replace(/\x1b\[[0-9;?]*[a-zA-Z]/g, '').replace(/\r(?!\n)/g, '')
}

async function open(): Promise<void> {
  if (!sessionId.value || starting.value) return
  starting.value = true
  try {
    term.value = await apiPost<TerminalInfo>('/api/v1/terminal/open', { session_id: sessionId.value })
    closed.value = false
    startPoll()
  } finally {
    starting.value = false
  }
}

function startPoll(): void {
  stopPoll()
  pollTimer = window.setInterval(poll, 700)
  void poll()
}

function stopPoll(): void {
  if (pollTimer !== null) {
    window.clearInterval(pollTimer)
    pollTimer = null
  }
}

async function poll(): Promise<void> {
  if (!term.value) return
  try {
    const r = await apiGet<TerminalOutput>(
      `/api/v1/terminal/${term.value.id}/output?since=${since.value}`
    )
    if (r.output) {
      output.value += stripAnsi(r.output)
      // 面板缓冲上限：只留尾部 256KB（与后端环形缓冲一致）
      if (output.value.length > 256 * 1024) output.value = output.value.slice(-256 * 1024)
      await nextTick()
      outEl.value?.scrollTo({ top: outEl.value.scrollHeight })
    }
    since.value = r.seq
    if (r.closed) closed.value = true
  } catch {
    /* 单次轮询失败静默（后端重启 / 网络抖动） */
  }
}

async function run(): Promise<void> {
  const cmd = input.value
  if (!cmd.trim() || !term.value) return
  input.value = ''
  await apiPost(`/api/v1/terminal/${term.value.id}/run`, { id: term.value.id, command: cmd })
}

async function restart(): Promise<void> {
  if (term.value) {
    await apiPost(`/api/v1/terminal/${term.value.id}/stop`, {}).catch(() => undefined)
  }
  term.value = null
  output.value = ''
  since.value = 0
  await open()
}

function onKeydown(e: KeyboardEvent): void {
  if (e.key === 'Enter') {
    e.preventDefault()
    void run()
  }
}

watch(() => chat.currentID, () => {
  // 会话切换：终端按会话隔离，重开并清空视图
  stopPoll()
  term.value = null
  output.value = ''
  since.value = 0
  void open()
}, { immediate: true })

onBeforeUnmount(stopPoll)
</script>

<template>
  <div class="flex h-full min-h-0 flex-col text-xs">
    <!-- 工具行：cwd + 重开 -->
    <div class="flex shrink-0 items-center gap-1.5 border-b border-wb-border px-2.5 py-1.5">
      <Terminal class="h-3.5 w-3.5 shrink-0 text-wb-primary" />
      <span class="min-w-0 flex-1 truncate text-wb-muted" :title="term?.cwd">{{ term?.cwd ?? t('ui.status.loading') }}</span>
      <button type="button" class="btn btn-sm" :title="t('terminal.restart')" @click="restart">
        <RefreshCw class="ic ic-sm" />
      </button>
    </div>

    <!-- 输出 -->
    <div ref="outEl" class="terminal-out min-h-0 flex-1 overflow-y-auto px-2.5 py-2">
      <pre class="whitespace-pre-wrap break-all font-mono text-[11px] leading-4 text-wb-ink">{{ output || t('terminal.empty') }}</pre>
      <p v-if="closed" class="mt-2 text-[11px] text-wb-danger">{{ t('terminal.closed') }}</p>
    </div>

    <!-- 输入行 -->
    <div class="flex shrink-0 items-center gap-1.5 border-t border-wb-border px-2.5 py-1.5">
      <span class="font-mono text-wb-primary-strong">&gt;</span>
      <input
        v-model="input"
        class="min-w-0 flex-1 bg-transparent font-mono text-[11.5px] text-wb-ink outline-none placeholder:text-wb-muted"
        :placeholder="t('terminal.placeholder')"
        :disabled="!term || closed"
        spellcheck="false"
        @keydown="onKeydown"
      >
      <button
        type="button"
        class="btn btn-sm"
        :disabled="!term || closed || !input.trim()"
        @click="run"
      >
        <Square class="ic ic-sm" />
      </button>
    </div>
  </div>
</template>

<style scoped>
.terminal-out {
  background: color-mix(in srgb, var(--wb-ink) 4%, transparent);
}
</style>
