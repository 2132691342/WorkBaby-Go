<script setup lang="ts">
/**
 * 辅助对话面板（ZCode「辅助对话」对标）：右栏与主任务并行、互不打断的独立小会话。
 *
 * <p>空态是启动页四卡（辅助对话可用；审查/终端/浏览器为后续版本占位）；
 * 进入会话后：继承主会话历史作模型上下文（界面上从空白开始），可调工具、走审批、
 * 可从主对话划选带引用过来。面板关闭 / 主任务删除即终止，不进任务列表。
 */
import { nextTick, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { Bot, Send, Loader2, Square, MessageSquare, ScrollText, Terminal, Globe } from '@/components/common/icons'
import { useSideChatStore } from '@/stores/sideChat'
import { useChatStore } from '@/stores/chat'
import { t } from '@/i18n'
import MarkdownRenderer from '@/components/chat/MarkdownRenderer.vue'
import MessageBlocksRenderer from '@/components/chat/MessageBlocksRenderer.vue'
import { resolveMessageBlocks } from '@/chat/models/blocks'
import type { Message } from '@/types/api'

const side = useSideChatStore()
const chat = useChatStore()
const { sideSession, messages, loading, streaming, streamingBlocks, streamingContent, streamingRetry, pendingApprovals, error } = storeToRefs(side)

const props = defineProps<{
  /** 面板是否可见：可见（或主会话变化）时才拉取/恢复辅助会话，避免启动期竞态。 */
  visible: boolean
}>()

const listEl = ref<HTMLElement | null>(null)
const inputEl = ref<HTMLTextAreaElement | null>(null)
const inputText = ref('')

// 面板可见 / 主会话切换 → 跟随恢复该主会话的辅助会话
watch(
  () => [props.visible, chat.currentID] as const,
  ([vis]) => {
    if (vis) void side.open(chat.currentID ?? null)
  },
  { immediate: true }
)

// 面板可见时预填草稿（划选引用带过来）
watch(
  () => side.draft,
  (d) => {
    if (d) {
      inputText.value = d
      side.draft = ''
      void focusInput()
    }
  }
)

watch(
  () => [messages.value.length, streaming.value, streamingContent.value, streamingBlocks.value.length],
  scheduleScroll
)

let raf = 0
function scheduleScroll(): void {
  if (raf) return
  raf = requestAnimationFrame(() => {
    raf = 0
    const el = listEl.value
    if (el) el.scrollTo({ top: el.scrollHeight, behavior: 'auto' })
  })
}

async function focusInput(): Promise<void> {
  await nextTick()
  inputEl.value?.focus()
}

/** 启动页：点「辅助对话」卡片创建/复用并聚焦输入框。 */
async function startSide(): Promise<void> {
  if (!chat.currentID) return
  await side.ensure(chat.currentID)
  await focusInput()
}

function submit(): void {
  const text = inputText.value
  if (!text.trim() || streaming.value) return
  inputText.value = ''
  void side.send(text)
}

function onKeydown(e: KeyboardEvent): void {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    submit()
  }
}

/** 附件渲染占位：辅助对话 v1 不带附件，user 消息直接按文本渲染。 */
function isUser(m: Message): boolean {
  return m.role === 'user'
}
</script>

<template>
  <!-- 启动页：四卡（ZCode 空面板形态）。仅辅助对话可用，其余为后续版本占位 -->
  <div v-if="!sideSession" class="flex h-full flex-col items-center justify-center gap-3 p-4">
    <p class="text-base font-semibold text-wb-ink">{{ t('side.openTitle') }}</p>
    <p class="mb-1 text-xs text-wb-muted">{{ t('side.openHint') }}</p>
    <div class="grid w-full max-w-[22rem] grid-cols-3 gap-2">
      <button type="button" class="side-start-card" @click="startSide">
        <MessageSquare class="h-5 w-5" />
        <span>{{ t('side.title') }}</span>
      </button>
      <button type="button" class="side-start-card opacity-45" disabled :title="t('side.comingSoon')">
        <ScrollText class="h-5 w-5" />
        <span>{{ t('side.review') }}</span>
      </button>
      <button type="button" class="side-start-card opacity-45" disabled :title="t('side.comingSoon')">
        <Terminal class="h-5 w-5" />
        <span>{{ t('side.terminal') }}</span>
      </button>
      <button type="button" class="side-start-card opacity-45" disabled :title="t('side.comingSoon')">
        <Globe class="h-5 w-5" />
        <span>{{ t('side.browser') }}</span>
      </button>
    </div>
  </div>

  <!-- 会话视图 -->
  <div v-else class="flex h-full min-h-0 flex-col">
    <div ref="listEl" class="min-h-0 flex-1 space-y-3 overflow-y-auto px-3 py-3">
      <div v-if="loading && messages.length === 0" class="py-6 text-center text-xs text-wb-muted">
        {{ t('ui.status.loading') }}
      </div>

      <div v-for="m in messages" :key="m.id" class="flex flex-col" :class="isUser(m) ? 'items-end' : 'items-start'">
        <!-- 用户：气泡 -->
        <div v-if="isUser(m)" class="msg-u max-w-[92%]">
          <span class="whitespace-pre-wrap">{{ m.content }}</span>
        </div>
        <!-- 助手：过程块 + 正文（窄面板复用主链路渲染器，迹线一致） -->
        <div v-else class="w-full min-w-0">
          <MessageBlocksRenderer :blocks="resolveMessageBlocks(m)" :content="m.content ?? ''" :streaming_mode="false" />
        </div>
      </div>

      <!-- 流式中：过程块 + 光标 -->
      <div v-if="streaming" class="w-full min-w-0">
        <div v-if="streamingRetry" class="mb-1.5 flex items-center gap-1.5 rounded-lg bg-wb-lemon/10 px-2 py-1 text-[11px] text-wb-lemon">
          <Loader2 class="h-3 w-3 animate-spin" />
          {{ t('chat.retryHint', streamingRetry.attempt, Math.max(1, Math.round(streamingRetry.delay_ms / 1000))) }}
        </div>
        <MessageBlocksRenderer v-if="streamingBlocks.length > 0" :streaming="streamingBlocks" :streaming_mode="true">
          <template #text="{ content, streaming: isLive }">
            <div class="flex items-end gap-1">
              <MarkdownRenderer :content="content" :streaming="isLive" />
              <span v-if="isLive" class="wb-cursor" />
            </div>
          </template>
        </MessageBlocksRenderer>
        <template v-else>
          <div v-if="streamingContent" class="flex items-end gap-1">
            <MarkdownRenderer :content="streamingContent" :streaming="true" />
            <span class="wb-cursor" />
          </div>
          <span v-else class="wb-cursor" />
        </template>
      </div>

      <!-- 错误（轻量红条） -->
      <div v-if="error && !streaming" class="alert a-danger py-1.5 text-[11px]">{{ error }}</div>

      <!-- 审批内联（辅助会话的工具同样走权限确认） -->
      <div v-for="a in pendingApprovals" :key="a.id" class="rounded-lg border border-wb-warning/40 bg-wb-warning/10 p-2 text-[11px]">
        <p class="mb-1 font-medium text-wb-ink">{{ a.command }}</p>
        <p v-if="a.reason" class="mb-1.5 text-wb-muted">{{ a.reason }}</p>
        <div class="flex items-center gap-2">
          <button type="button" class="btn btn-sm" @click="side.decideApproval(a.id, true)">{{ t('chat.approvalApprove') }}</button>
          <button type="button" class="btn btn-sm" style="color: var(--wb-danger)" @click="side.decideApproval(a.id, false)">{{ t('chat.approvalDeny') }}</button>
        </div>
      </div>
    </div>

    <!-- 输入区 -->
    <div class="shrink-0 border-t border-wb-border p-2">
      <div class="rounded-xl border border-wb-border bg-wb-surface-2 p-1.5 focus-within:border-wb-primary/50">
        <textarea
          ref="inputEl"
          v-model="inputText"
          rows="2"
          class="w-full resize-none bg-transparent px-1.5 py-1 text-xs text-wb-ink outline-none placeholder:text-wb-muted"
          :placeholder="t('side.placeholder')"
          @keydown="onKeydown"
        />
        <div class="flex items-center justify-end gap-1.5">
          <Bot v-if="!streaming" class="h-3.5 w-3.5 text-wb-muted" :title="t('side.inheritHint')" />
          <button
            type="button"
            class="btn btn-sm"
            :class="streaming ? '' : 'btn-primary'"
            :disabled="!streaming && !inputText.trim()"
            @click="streaming ? side.cancel() : submit()"
          >
            <Loader2 v-if="streaming" class="ic ic-sm animate-spin" />
            <Square v-else-if="false" class="ic ic-sm" />
            <Send v-else class="ic ic-sm" />
            <span v-if="streaming">{{ t('chat.stop') }}</span>
          </button>
        </div>
      </div>
      <p class="mt-1 px-1 text-[10px] text-wb-muted">{{ t('side.inheritHint') }}</p>
    </div>
  </div>
</template>

<style scoped>
.side-start-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: 14px 8px;
  border: 1px solid var(--wb-border);
  border-radius: 10px;
  background: var(--wb-surface-2);
  color: var(--wb-ink);
  font-size: 11.5px;
  cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease;
}
.side-start-card:not(:disabled):hover {
  border-color: var(--wb-primary);
  background: color-mix(in srgb, var(--wb-primary) 8%, var(--wb-surface-2));
}
.side-start-card:disabled {
  cursor: default;
}
</style>
