<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { ChevronDown } from '@/components/common/icons'
import type { Message } from '@/types/api'
import { t } from '@/i18n'
import { Sparkles, FileText, Code2, Clock, BookOpen } from '@/components/common/icons'
import MessageItem from '@/components/chat/MessageItem.vue'
import StreamingBubble from '@/components/chat/StreamingBubble.vue'
import ApprovalInline from '@/components/chat/ApprovalInline.vue'
import StopReasonBanner from '@/components/chat/StopReasonBanner.vue'
import AssistantAvatar from '@/components/chat/AssistantAvatar.vue'
import { useToast } from '@/composables/useToast'
import { useChatStore } from '@/stores/chat'

/** 空状态的一键示例 prompt，点击直接填入输入框。 */
const quickPrompts: { title: string; text: string; icon: unknown }[] = [
  {
    title: '写一份周报',
    text: '帮我把本周的工作整理成一份 Markdown 周报：包含「本周完成 / 进行中 / 下周计划 / 复思考」4 块',
    icon: FileText
  },
  {
    title: '解释一段代码',
    text: '帮我解释一下这段代码做了什么、有什么坑，以及怎么改进（贴代码）',
    icon: Code2
  },
  {
    title: '安排定时任务',
    text: '我想每天早上 9 点跑一次「昨日聊天日报」工作流，怎么配置？',
    icon: Clock
  },
  {
    title: '整理知识库',
    text: '把上传到知识库的 PDF 资料按主题分类，并告诉我怎么查',
    icon: BookOpen
  }
]

defineEmits<{
  useQuickPrompt: [text: string]
}>()

/**
 * 消息列表：滚动锚定 + 虚拟渲染窗口 + 空状态 + 条目流。
 *
 * <p>单条历史消息渲染内聚在 {@link MessageItem}，流式气泡在 {@link StreamingBubble}，
 * 审批卡在 {@link ApprovalInline}，终止横幅在 {@link StopReasonBanner}。
 * `messages`（当前会话快照）与 `streaming`（状态机信号）由 ChatView 传入，
 * 流式状态直接读 chat store。
 */
const props = defineProps<{
  messages: Message[]
  streaming: boolean
}>()

const chat = useChatStore()
const { pendingApproval, stopReason } = storeToRefs(chat)

const toast = useToast()

const scrollEl = ref<HTMLElement | null>(null)
// 用户是否停留在底部（上翻阅读历史时不强制拉底，F5）
const pinned = ref(true)
// 当前 hover 的消息 ID
const hoveredID = ref<string | null>(null)

function onScroll(): void {
  const el = scrollEl.value
  if (!el) return
  pinned.value = el.scrollHeight - el.scrollTop - el.clientHeight < 80
}

/**
 * 可恢复检查点：这些终态意味着「任务没做完，但可以接着做」。
 *
 * <p>实现上不新增后端接口——AgentScope 的会话状态是持久化的，重新发一条消息时
 * 历史会整体回灌，模型看得见自己上一轮做到哪儿。所以「继续」= 发一条明确的续跑指令，
 * 走正常发送链路即可。
 */
const RESUMABLE_REASONS = new Set(['cancelled', 'tool_error_limit', 'max_turns', 'token_budget', 'interrupted'])

const canContinue = computed(
  () => !props.streaming && !!stopReason.value && RESUMABLE_REASONS.has(stopReason.value)
)

async function onContinue(): Promise<void> {
  if (!canContinue.value) return
  try {
    // 崩溃/中断恢复优先走检查点续跑（同一 runID，已完成工具经 StepRecords 复用不重放）
    if (stopReason.value === 'interrupted') {
      const last = [...props.messages].reverse().find((m) => m.role === 'assistant')
      if (last?.run_id && last.status === 'failed') {
        await chat.resumeRun(last.run_id)
        return
      }
    }
    await chat.sendMessage(t('chat.continuePrompt'))
  } catch (e) {
    toast.error(t('chat.operationFailed'), e instanceof Error ? e.message : String(e))
  }
}

function scrollToBottom(): void {
  const el = scrollEl.value
  if (el) el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
  pinned.value = true
}

// 会话切换（messages 整体替换）时重置为贴底
watch(
  () => props.messages,
  () => {
    pinned.value = true
  }
)

// 内容变化时，仅当用户停留在底部才自动滚底
// 滚动改 rAF 节流 + behavior:'auto'（流式高频触发时 smooth 会堆积动画帧导致抖动）
let scrollRafID: number | null = null
watch(
  () => [props.messages.length, chat.streamingContent.length, props.streaming, chat.streamingTools.length],
  () => {
    if (scrollRafID != null) return // 已有 pending 帧，跳过（rAF 天然节流）
    scrollRafID = requestAnimationFrame(() => {
      scrollRafID = null
      const el = scrollEl.value
      if (el && pinned.value) {
        // behavior:'auto' 瞬时贴底——流式时 smooth 的动画队列会与下一帧滚动竞争，产生抖动
        el.scrollTo({ top: el.scrollHeight, behavior: 'auto' })
      }
    })
  }
)

/**
 * 修复：判断第 i 条消息是否在「当前可视线附近」。
 * 流式中只保留最后 1 条 + 上下 5 条；其余用 content-visibility: auto 跳过渲染。
 */
const NEAR_WINDOW = 5
function isNearCurrent(i: number): boolean {
  if (!props.streaming) return true
  const total = props.messages.length
  // 流式 + 最近添加 → 最后一条附近；空消息阶段（流开但还没首 token）放空也无所谓
  return i >= total - 1 - NEAR_WINDOW && i <= total - 1
}
</script>

<template>
  <div ref="scrollEl" class="flex-1 overflow-y-auto px-6 py-6" @scroll="onScroll">
    <!-- 空状态：克制卡片（实底 + 细边框，无渐变扫光）+ 4 个一键示例 prompt。 -->
    <div v-if="messages.length === 0 && !streaming" class="flex h-full items-center justify-center px-4">
      <div class="w-full max-w-xl">
        <div class="card-pop p-8 text-center">
          <div class="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-xl bg-wb-primary/10 text-wb-primary">
            <Sparkles class="h-6 w-6" />
          </div>
          <h3 class="text-base font-semibold text-wb-ink">{{ t('chat.emptyTitle') }}</h3>
          <p class="mt-2 text-sm text-wb-muted">{{ t('chat.emptyPrompt') }}</p>
          <p class="mt-1 text-xs text-wb-muted/70">{{ t('chat.emptyHint') }}</p>
        </div>
        <div class="mt-4 grid grid-cols-1 gap-2 sm:grid-cols-2">
          <button
            v-for="(s, i) in quickPrompts"
            :key="i"
            type="button"
            class="card-pop group flex items-start gap-3 px-4 py-3 text-left transition-all hover:border-wb-primary/40 hover:shadow-sm"
            @click="$emit('useQuickPrompt', s.text)"
          >
            <span class="mt-0.5 inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-wb-primary/10 text-wb-primary">
              <component :is="s.icon" class="h-3.5 w-3.5" />
            </span>
            <span class="min-w-0">
              <span class="block text-sm font-medium text-wb-ink">{{ s.title }}</span>
              <span class="mt-0.5 block truncate text-xs text-wb-muted">{{ s.text }}</span>
            </span>
          </button>
        </div>
      </div>
    </div>

    <div class="mx-auto max-w-3xl space-y-5">
      <div
        v-for="(m, i) in messages"
        :key="m.id"
        class="wb-msg-item group flex wb-msg-enter"
        :class="[m.role === 'user' ? 'justify-end' : 'justify-start', { 'wb-msg-item-near': isNearCurrent(i) }]"
        :style="{ animationDelay: (i % 5) * 60 + 'ms' }"
        @mouseenter="hoveredID = m.id"
        @mouseleave="hoveredID = null"
      >
        <!-- assistant 头像（桌宠形象，与桌宠窗口同源） -->
        <AssistantAvatar v-if="m.role !== 'user'" class="mr-3" />

        <MessageItem
          :message="m"
          :index="i"
          :is-last="i === messages.length - 1"
          :streaming="streaming"
          :hovered="hoveredID === m.id"
        />

        <!-- 用户头像：与 assistant 头像对称，平衡消息两侧视觉 -->
        <AssistantAvatar v-if="m.role === 'user'" role="user" class="ml-3" />
      </div>

      <!-- 流式中的 assistant 气泡 -->
      <div v-if="streaming" class="flex justify-start">
        <AssistantAvatar class="mr-3" speaking />
        <div class="max-w-[80%]">
          <div class="mb-1 flex items-center gap-2 px-1 text-xs text-wb-muted">
            <span class="font-medium text-wb-ink">WorkBaby</span>
          </div>
          <StreamingBubble />
        </div>
      </div>

      <!-- 审批内联：从页面顶部下移至此，贴近当前运行消息，
           与触发审批的工具调用保持空间上下文（红色=不可逆，警告=可恢复） -->
      <div v-if="pendingApproval" class="flex justify-start">
        <div class="w-full max-w-[80%]">
          <ApprovalInline />
        </div>
      </div>

      <!-- 终止原因横幅：中性终态（非 completed 才展示）——
           用户主动停止 / 工具失败限额是正常收场，区别于 error（error 有独立红色 alert）。
           P2-3：可恢复终态（cancelled / tool_error_limit / max_turns / token_budget）
           额外给一个「继续」按钮，一键接着上轮未完成的部分做，而不是让用户重新描述一遍任务。 -->
      <StopReasonBanner
        :stop-reason="stopReason"
        :streaming="streaming"
        @continue="onContinue"
        @dismiss="chat.dismissStopReason()"
      />
    </div>

    <!-- 滚动到底部按钮（用户上翻阅读历史时出现） -->
    <el-button
      v-if="!pinned"
      size="small"
      round
      class="absolute bottom-6 left-1/2 z-10 -translate-x-1/2 shadow-[var(--wb-shadow-lg)]"
      @click="scrollToBottom"
    >
      <el-icon class="mr-1"><ChevronDown /></el-icon>
      {{ t('chat.scrollToBottom') }}
    </el-button>
  </div>
</template>

<style scoped>
/* CSS-only 虚拟滚动：屏外消息跳过渲染；流式最后 1 条 + 上下 5 条强制可见。
   M2：contain-intrinsic-size 加 auto 关键字——Chromium 会记住每个条目
   最后一次渲染的实际高度作为占位估算，消除「估算 96px 与真实高度偏差
   导致的滚动条跳动」，长会话滚动稳定性显著提升（零 JS 成本）。 */
.wb-msg-item {
  content-visibility: auto;
  contain-intrinsic-size: auto 96px;
}
.wb-msg-item-near {
  content-visibility: visible;
}
</style>
