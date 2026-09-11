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
  streamingTurn,
  lastCheckpointTurn,
  streamingSkill,
  streamingArtifacts,
  streamingGenUi,
  streamingRetry
} = storeToRefs(chat)
const { enabled: focusMode } = useFocusMode()

const hasThinking = computed(() => streamingThinking.value.length > 0)
const tools = computed(() => streamingTools.value)

// ===== 思考面板：思考中自动展开，正文开始产出后自动折叠；展开期间流式自动滚底 =====
// 视口固定 7 行（11px × 1.75 行高 ≈ 134px）：再高就喧宾夺主，再矮就看不到推理推进。
const THINK_MAX_PX = '134px'
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
</script>

<template>
  <!-- 与历史消息同一套语义类（.bubble）：流式与终态正文的排版完全同源，
       避免「流式时一个样、结束后另一个样」的跳变 -->
  <div class="bubble">
    <!-- 建流瞬时错误自动重试提示（限流/5xx；正文恢复自动消失） -->
    <div
      v-if="streamingRetry"
      class="mb-2 flex items-center gap-2 rounded-lg bg-wb-lemon/10 px-2 py-1 text-[11px] text-wb-lemon"
    >
      <Loader2 class="h-3 w-3 animate-spin" />
      {{ t('chat.retryHint', streamingRetry.attempt, Math.max(1, Math.round(streamingRetry.delay_ms / 1000))) }}
    </div>
    <!-- 流式思考面板：思考中自动展开，正文开始产出后自动折叠。
         不用 <details>：原生 details 折叠是瞬时塌陷，思考完那一刻整条消息突然矮一截，
         观感上像内容丢失；改成 max-height 过渡 + 固定 7 行视口（内部滚动），
         思考期间始终是同一个高度，收起时也是缓动收拢。 -->
    <!-- 流式思考面板：无边框轻量行（与历史 MessageItem 的思考行同形态），
         思考中自动展开、正文开始产出后自动折叠；展开时正文只留左侧细竖线 -->
    <div v-if="!focusMode && hasThinking">
      <button
        type="button"
        class="flex cursor-pointer select-none items-center gap-1.5 rounded px-1 py-0.5 text-left text-[11.5px] text-wb-muted transition-colors hover:bg-wb-surface-hover hover:text-wb-ink"
        @click="thinkingOpen = !thinkingOpen"
      >
        <Brain class="h-3.5 w-3.5" />
        <span>{{ thinkingOpen ? t('chat.thinking') : t('chat.thoughtDone') }}</span>
        <Loader2 v-if="!streamingContent" class="h-3 w-3 animate-spin" />
        <ChevronDown
          class="h-3 w-3 transition-transform duration-200"
          :class="thinkingOpen ? 'rotate-180' : ''"
        />
      </button>
      <div class="wb-think-wrap" :style="{ maxHeight: thinkingOpen ? THINK_MAX_PX : '0px' }">
        <pre
          ref="thinkingBody"
          class="mx-1 mb-1 overflow-y-auto whitespace-pre-wrap border-l-2 border-wb-border pl-3 pt-1 font-sans text-[11px] leading-[1.75] text-wb-muted/90"
        >{{ streamingThinking }}</pre>
      </div>
    </div>

    <!-- 轮次标记：多轮任务里「第 N 轮」是「还在干活」的关键信号；
         单轮（turn<=1）不显示，避免给简单问答加噪声。检查点位点仅在多轮时一并展示。 -->
    <div
      v-if="!focusMode && streamingTurn > 1"
      class="mb-1.5 flex items-center gap-1.5 px-1 text-[11px] text-wb-muted"
    >
      <span class="font-medium text-wb-ink/70">{{ t('chat.turnLabel', streamingTurn) }}</span>
      <span v-if="lastCheckpointTurn" class="text-wb-muted/70">
        {{ t('chat.checkpointLabel', lastCheckpointTurn) }}
      </span>
    </div>

    <!-- 任务步骤时间线（工具调用可视化）；焦点模式下隐藏。
         compact：流式期间工具行内联进正文流，不加外框（盒子会把阅读节奏切成两段）。 -->
    <div v-if="!focusMode && (tools.length > 0 || streamingSkill)" class="mb-3">
      <TaskTimeline :tools="tools" :skill-hit="streamingSkill" compact />
    </div>

    <!-- 交付成果卡片（present_files 交付） -->
    <ArtifactCard v-if="streamingArtifacts" :payload="streamingArtifacts" class="mb-3" />

    <!-- GenUI 内联渲染（gen_ui 工具结果） -->
    <div v-if="streamingGenUi" class="wb-genui mb-3 rounded-xl border border-wb-border bg-wb-primary/[0.03] p-3">
      <GenUiRenderer :node="streamingGenUi" />
    </div>

    <div v-if="streamingContent" class="flex items-end gap-1">
      <!-- 流式光标独立放在 MarkdownRenderer 之后，不受 v-html 重写影响，动画稳定不闪烁 -->
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

<style scoped>
/* 思考视口的展开/收起过渡：缓动收拢替代 details 的瞬时塌陷 */
.wb-think-wrap {
  transition: max-height 0.24s ease;
  overflow: hidden;
}
@media (prefers-reduced-motion: reduce) {
  .wb-think-wrap {
    transition: none;
  }
}
</style>
