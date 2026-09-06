<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { Brain, ChevronDown, Loader2 } from '@/components/common/icons'
import TaskTimeline from '@/components/chat/TaskTimeline.vue'
import ArtifactCard from '@/components/chat/ArtifactCard.vue'
import GenUiRenderer from '@/components/genui/GenUiRenderer.vue'
import UsageBadge from '@/components/chat/UsageBadge.vue'
import MarkdownRenderer from '@/components/chat/MarkdownRenderer.vue'
import { useChatStore } from '@/stores/chat'
import { useFocusMode } from '@/composables/useFocusMode'
import { t } from '@/i18n'

/**
 * 流式中的 assistant 气泡：
 * 思考面板（自动展开/折叠 + 流式自动滚底）+ 工具卡片 + 交付卡片 + GenUI + 流式正文 + 用量徽标。
 *
 * <p>流式状态直接读 chat store；焦点模式下隐藏思考与工具时间线。
 */
const chat = useChatStore()
const {
  streamingContent,
  streamingThinking,
  streamingTools,
  streamingStats,
  streamingArtifacts,
  streamingGenUi,
  streamingRetry
} = storeToRefs(chat)
const { enabled: focusMode } = useFocusMode()

const hasThinking = computed(() => streamingThinking.value.length > 0)
const tools = computed(() => streamingTools.value)

// ===== 思考面板：思考中自动展开，正文开始产出后自动折叠；展开期间流式自动滚底 =====
const thinkingOpen = ref(true)
const thinkingBody = ref<HTMLElement | null>(null)

watch(
  () => streamingContent.value.length,
  (now, prev) => {
    if (now > 0 && prev === 0) thinkingOpen.value = false
  }
)

watch(streamingThinking, async () => {
  if (!thinkingOpen.value) return
  await nextTick()
  const el = thinkingBody.value
  if (el) el.scrollTop = el.scrollHeight
})

function onThinkingToggle(e: Event): void {
  thinkingOpen.value = (e.target as HTMLDetailsElement).open
}
</script>

<template>
  <div class="rounded-lg border border-wb-border bg-wb-surface px-4 py-3 text-sm leading-relaxed text-wb-ink">
    <!-- 建流瞬时错误自动重试提示（限流/5xx；正文恢复自动消失） -->
    <div
      v-if="streamingRetry"
      class="mb-2 flex items-center gap-2 rounded-lg bg-wb-lemon/10 px-2 py-1 text-[11px] text-wb-lemon"
    >
      <Loader2 class="h-3 w-3 animate-spin" />
      {{ t('chat.retryHint', streamingRetry.attempt, Math.max(1, Math.round(streamingRetry.delay_ms / 1000))) }}
    </div>
    <!-- 流式思考面板：思考中自动展开，正文开始产出后自动折叠；展开期间流式自动滚底 -->
    <details
      v-if="!focusMode && hasThinking"
      class="group mb-2 rounded-lg border border-wb-lavender/20 bg-wb-lavender/[0.07]"
      :open="thinkingOpen"
      @toggle="onThinkingToggle"
    >
      <summary class="flex cursor-pointer select-none items-center gap-1.5 px-2.5 py-1.5 text-xs text-wb-muted">
        <Brain class="h-3.5 w-3.5 text-wb-lavender" />
        <span>{{ thinkingOpen ? t('chat.thinking') : t('chat.thoughtDone') }}</span>
        <Loader2 v-if="!streamingContent" class="h-3 w-3 animate-spin text-wb-lavender" />
        <ChevronDown
          class="ml-auto h-3 w-3 transition-transform duration-200"
          :class="thinkingOpen ? 'rotate-180' : ''"
        />
      </summary>
      <pre
        ref="thinkingBody"
        class="mx-2.5 mb-2 max-h-48 overflow-y-auto whitespace-pre-wrap font-sans text-[11px] leading-relaxed text-wb-muted/90"
      >{{ streamingThinking }}</pre>
    </details>

    <!-- 任务步骤时间线（工具调用可视化）；焦点模式下隐藏（与 Claude Code focus 一致） -->
    <div v-if="!focusMode && tools.length > 0" class="mb-3 rounded-xl border border-wb-border bg-wb-primary/[0.03] p-3">
      <TaskTimeline :tools="tools" />
    </div>

    <!-- 交付成果卡片（present_files 交付） -->
    <ArtifactCard v-if="streamingArtifacts" :payload="streamingArtifacts" class="mb-3" />

    <!-- GenUI 内联渲染（gen_ui 工具结果） -->
    <div v-if="streamingGenUi" class="wb-genui mb-3 rounded-xl border border-wb-border bg-wb-primary/[0.03] p-3">
      <GenUiRenderer :node="streamingGenUi" />
    </div>

    <div v-if="streamingContent" class="flex items-end gap-1">
      <!-- 修复：流式光标独立放在 MarkdownRenderer 之后，
           不受 markdown 内部 v-html 重写影响，光标动画稳定不闪烁 -->
      <MarkdownRenderer :content="streamingContent" :streaming="true" />
      <span v-if="streamingContent" class="wb-cursor" />
    </div>
    <span v-else-if="tools.length === 0 && !streamingArtifacts && !streamingGenUi" class="wb-cursor" />
  </div>

  <!-- 流式用量（后端 chat:stats 事件；每轮结束刷新累计值） -->
  <UsageBadge
    v-if="streamingStats"
    :usage="{
      input_tokens: streamingStats.input_tokens,
      output_tokens: streamingStats.output_tokens,
      total_tokens: streamingStats.total_tokens,
      latency_ms: streamingStats.latency_ms,
      cost: streamingStats.cost
    }"
    live
  />
</template>
