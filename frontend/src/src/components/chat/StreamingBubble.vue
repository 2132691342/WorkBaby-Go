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
  <!-- 原型 .bubble：正文裸排，块级内容（思考 / 时间线 / 卡片）各自自带边框 -->
  <div class="text-[13px] leading-[1.78] text-wb-ink">
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
    <div
      v-if="!focusMode && hasThinking"
      class="mb-2 overflow-hidden rounded-lg border border-wb-border bg-wb-surface-2"
    >
      <button
        type="button"
        class="flex w-full cursor-pointer select-none items-center gap-1.5 px-2.5 py-1.5 text-left text-[11.5px] text-wb-muted"
        @click="thinkingOpen = !thinkingOpen"
      >
        <Brain class="h-3.5 w-3.5" />
        <span>{{ thinkingOpen ? t('chat.thinking') : t('chat.thoughtDone') }}</span>
        <Loader2 v-if="!streamingContent" class="h-3 w-3 animate-spin" />
        <ChevronDown
          class="ml-auto h-3 w-3 transition-transform duration-200"
          :class="thinkingOpen ? 'rotate-180' : ''"
        />
      </button>
      <div class="wb-think-wrap" :style="{ maxHeight: thinkingOpen ? THINK_MAX_PX : '0px' }">
        <pre
          ref="thinkingBody"
          class="mx-2.5 mb-2 overflow-y-auto whitespace-pre-wrap border-t border-dashed border-wb-border pt-2 font-sans text-[11px] leading-[1.75] text-wb-muted/90"
        >{{ streamingThinking }}</pre>
      </div>
    </div>

    <!-- 任务步骤时间线（工具调用可视化）；焦点模式下隐藏（与 Claude Code focus 一致）。
         原型 .tl 自带边框与底色，这里不再套染色包装（消除双重边框） -->
    <div v-if="!focusMode && (tools.length > 0 || streamingSkill)" class="mb-3">
      <TaskTimeline :tools="tools" :skill-hit="streamingSkill" />
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
