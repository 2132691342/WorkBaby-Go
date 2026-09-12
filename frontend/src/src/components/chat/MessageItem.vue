<script setup lang="ts">
import { computed, ref } from 'vue'
import { useClipboard } from '@vueuse/core'
import { Copy, Check, Pencil, RefreshCw, Trash2, GitBranch, FileText, ChevronDown, Paperclip, Wrench } from '@/components/common/icons'
import type { Message, MessageAttachment } from '@/types/api'
import { t } from '@/i18n'
import { useChatStore } from '@/stores/chat'
import { formatRelativeTime } from '@/utils/time'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { useFocusMode } from '@/composables/useFocusMode'
import MessageBlocksRenderer from '@/components/chat/MessageBlocksRenderer.vue'
import MarkdownRenderer from '@/components/chat/MarkdownRenderer.vue'
import InlineDiffCard from '@/components/chat/InlineDiffCard.vue'
import UsageBadge from '@/components/chat/UsageBadge.vue'
import { resolveMessageBlocks, stripThinkBlocks, blocksToToolCalls } from '@/chat/models/blocks'
import { summarizeToolCalls, totalToolMs, fmtTurnDuration } from '@/chat/models/toolGroupSummary'
import { splitMentions } from '@/chat/models/tokens'

/**
 * 单条历史消息：
 * meta 行 + 操作按钮（复制/编辑/重新生成/分叉/删除）+ 气泡 + 历史工具时间线
 * + 文件变更摘要 + 思考回看（焦点模式隐藏）+ 用量徽标。
 *
 * <p>会话级行为（resendFrom / deleteMessage / forkFrom）直接走 chat store；
 * 编辑态为条目内聚（同一时刻允许多条进入编辑，互不干扰）。
 */
const props = defineProps<{
  message: Message
  index: number
  /** 是否最后一条（决定「重新生成」按钮可见性）。 */
  isLast: boolean
  streaming: boolean
  /** 父级 hover 态（控制操作按钮显隐）。 */
  hovered: boolean
}>()

const chat = useChatStore()
const toast = useToast()
const dialog = useDialog()
const { enabled: focusMode } = useFocusMode()

const copiedID = ref(false)
const editing = ref(false)
const editingContent = ref('')
/** 历史消息思考回看折叠态（默认折叠）。 */
const thinkingOpen = ref(false)

const isUser = computed(() => props.message.role === 'user')

/** 用户消息分段：`@引用` 渲染成气泡，与输入框镜像高亮同源（一眼看出带了什么上下文）。 */
const userSegments = computed(() => splitMentions(props.message.content ?? ''))

/** 用户消息附件：图片出缩略图，其余出文件 chip（刷新/切会话后仍可回看）。 */
const userAttachments = computed<MessageAttachment[]>(() =>
  (props.message.attachments ?? []).filter((a) => !!a?.id)
)

/** 历史消息的过程块 → ToolCallInfo（纯函数已在 blocks.ts 单测覆盖）。 */
/** 历史消息的过程块（按 seq 排序）—— MessageBlocksRenderer 直接消费。 */
const historyBlocks = computed(() => resolveMessageBlocks(props.message))

// ===== turn 折叠叙事 =====
// 忙碌回合默认把「思考 + 工具」压成一行迹线，正文（答案）照常显示；点开恢复完整过程。
// 简单回合（过程 < 2 步）不折叠。
const traceTools = computed(() => blocksToToolCalls(historyBlocks.value))
const traceProcessCount = computed(
  () => traceTools.value.length + historyBlocks.value.filter((b) => b.kind === 'thinking' && !!b.text).length
)
const traceFoldable = computed(
  () => !isUser.value && !!props.message.content?.trim() && traceProcessCount.value >= 2
)
/** 迹线展开态（默认折叠）。 */
const traceOpen = ref(false)
/** 迹线摘要：聚合 receipt（读取文件 3 · 执行命令 2）+ 总耗时。 */
const traceSummary = computed(() => {
  const parts = summarizeToolCalls(traceTools.value).map((p) => t(`tool.receipt.${p.action}`, p.count))
  const dur = fmtTurnDuration(
    totalToolMs(
      traceTools.value.map((tc) => ({ durationMs: tc.duration_ms, startedAt: tc.started_at, state: tc.state })),
      Date.now()
    )
  )
  if (dur) parts.push(dur)
  return parts
})
/** 当前是否处于折叠叙事态（焦点模式只留正文，不走折叠）。 */
const traceCollapsed = computed(() => traceFoldable.value && !traceOpen.value && !focusMode.value)

/** 本条消息关联的文件变更（chat:file-change 已流式累积；历史消息按 run_id 精确对应）。 */
const fileChanges = computed(() => {
  if (!props.message.run_id) return []
  return chat.fileChanges.filter((c) => c.run_id === props.message.run_id)
})

/** 消息时间统一走 utils/time。 */
const fmtTime = formatRelativeTime

/**
 * 复制文本到剪贴板（接入 @vueuse/core）。
 *
 * <p>{@code legacy: true} 提供 execCommand 兜底；JCEF WebView 在非安全上下文下
 * navigator.clipboard 可能不可用，legacy 模式保证降级路径仍在。失败返回 false。
 */
const { copy: copyToClipboard } = useClipboard({ legacy: true })

/** 剥离 Markdown 语法 → 纯文本（复制出来是纯文本而非 MD 源码）；think 块一并剥离。 */
function stripMarkdown(md: string): string {
  return stripThinkBlocks(md)
    .replace(/```[a-zA-Z0-9_-]*\n?([\s\S]*?)```/g, (_m, code: string) => code.trim() + '\n')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/^\s{0,3}#{1,6}\s+/gm, '')
    .replace(/^(\s*)[-*+]\s+/gm, '$1• ')
    .replace(/^\s*\d+\.\s+/gm, '')
    .replace(/[*_~]{1,2}([^*_~]+)[*_~]{1,2}/g, '$1')
    .replace(/^\s*> ?/gm, '')
    .trim()
}

async function copyMessage(): Promise<void> {
  try {
    await copyToClipboard(stripMarkdown(props.message.content))
    copiedID.value = true
    setTimeout(() => (copiedID.value = false), 1500)
    toast.success(t('chat.copied'))
  } catch {
    /* 剪贴板失败静默（WebView 权限受限场景） */
  }
}

// ===== 消息操作 =====

function startEdit(): void {
  editing.value = true
  editingContent.value = stripMarkdown(props.message.content)
}

function cancelEdit(): void {
  editing.value = false
  editingContent.value = ''
}

async function saveEdit(): Promise<void> {
  const content = editingContent.value.trim()
  if (!content) return
  if (content !== props.message.content) {
    try {
      // 编辑重发：截断到该消息（含）之后 → 用新内容重新流式生成
      await chat.resendFrom(props.message.id, content)
      toast.success(t('chat.editResent'))
    } catch (e) {
      toast.error(t('chat.operationFailed'), e instanceof Error ? e.message : String(e))
    }
  }
  cancelEdit()
}

/** 重新生成 = 截断最后一条 assistant 及其父 user 消息 → 重发父 user 内容。 */
async function regenerate(): Promise<void> {
  if (props.streaming) return
  const parent = chat.messages[props.index - 1]
  if (!parent || parent.role !== 'user') {
    toast.warning(t('chat.regenerateUnavailable'))
    return
  }
  try {
    await chat.resendFrom(parent.id, parent.content)
  } catch (e) {
    toast.error(t('chat.operationFailed'), e instanceof Error ? e.message : String(e))
  }
}

/** 失败工具重试：等价「从此条 assistant 的父 user 消息重新生成」。 */
async function onToolRetry(): Promise<void> {
  if (props.streaming) return
  const parent = chat.messages[props.index - 1]
  if (!parent || parent.role !== 'user') {
    toast.warning(t('chat.regenerateUnavailable'))
    return
  }
  try {
    await chat.resendFrom(parent.id, parent.content)
  } catch (e) {
    toast.error(t('chat.operationFailed'), e instanceof Error ? e.message : String(e))
  }
}

async function deleteMessage(): Promise<void> {
  const ok = await dialog.confirm({
    title: t('chat.deleteMessage'),
    content: t('chat.deleteMessageConfirm'),
    danger: true
  })
  if (!ok) return
  try {
    await chat.deleteMessage(props.message.id)
    toast.success(t('chat.deletedSuccess'))
  } catch (e) {
    toast.error(t('chat.operationFailed'), e instanceof Error ? e.message : String(e))
  }
}

/**
 * 从此处分叉——把「到这条消息为止（含）」的对话复制到一个新会话并切过去。
 *
 * <p>与「重新生成」互补：重新生成只能从最后一条重来（且会剪掉原对话），
 * 分叉回到任意中间点开新支线，原会话原样保留。任务走偏想换个思路时用这个。
 */
async function forkFrom(): Promise<void> {
  if (props.streaming) return
  try {
    await chat.forkFrom(props.message.id)
    toast.success(t('chat.forked'), t('chat.forkedDetail'))
  } catch (e) {
    toast.error(t('chat.operationFailed'), e instanceof Error ? e.message : String(e))
  }
}
</script>

<template>
  <!-- 原型 .msg-a > .body{flex:1}：assistant 正文列占满（正文裸排无盒子），用户气泡限宽 76% -->
  <div
    class="flex flex-col"
    :class="isUser ? 'max-w-[76%] items-end' : 'w-full min-w-0 items-start'"
  >
    <!-- 思考回看：无边框轻量行，展开后正文只留左侧细竖线，
         不套盒子、不抢正文注意力（折叠叙事态下思考属于过程迹线，不单独出） -->
    <div v-if="!focusMode && !traceCollapsed && !isUser && message.thinking && !editing">
      <button
        type="button"
        class="flex items-center gap-1.5 rounded px-1 py-0.5 text-[11.5px] text-wb-muted transition-colors hover:bg-wb-surface-hover hover:text-wb-ink"
        @click="thinkingOpen = !thinkingOpen"
      >
        <Brain class="h-3.5 w-3.5" />
        <span>{{ t('chat.thoughtDone') }}</span>
        <ChevronDown
          class="h-3 w-3 transition-transform duration-200"
          :class="thinkingOpen ? 'rotate-180' : ''"
        />
      </button>
      <pre
        v-if="thinkingOpen"
        class="mt-1 max-h-48 overflow-y-auto whitespace-pre-wrap border-l-2 border-wb-border pl-3 font-sans text-[11.5px] leading-[1.75] text-wb-muted"
      >{{ message.thinking }}</pre>
    </div>

    <!-- 历史过程块按序渲染：thinking / tool_call / tool_result / artifact / skill / genui 时序穿插。
         message.content 作为最后一条 text 块传入（持久化模型里正文不入块）。
         焦点模式仅保留正文（渲染层根据 streaming_mode 标记折叠非正文块）。 -->
    <div v-if="!isUser && !editing" class="w-full">
      <!-- 折叠叙事态：过程压成一行迹线（思考 + 工具 receipt + 耗时），正文照常显示 -->
      <template v-if="traceCollapsed">
        <button
          type="button"
          class="flex items-center gap-1.5 self-start rounded px-1 py-0.5 text-[11.5px] text-wb-muted transition-colors hover:bg-wb-surface-hover hover:text-wb-ink"
          @click="traceOpen = true"
        >
          <Wrench class="h-3.5 w-3.5" />
          <span>{{ t('chat.processTitle') }}</span>
          <span v-for="p in traceSummary" :key="p" class="text-wb-muted">· {{ p }}</span>
          <span class="text-wb-primary-strong">{{ t('chat.traceExpand') }}</span>
          <ChevronDown class="h-3 w-3" />
        </button>
        <MarkdownRenderer :content="message.content ?? ''" />
      </template>

      <!-- 展开态：按序穿插的完整过程 + 正文（MessageBlocksRenderer 会把正文补为末尾 text 块） -->
      <template v-else>
        <button
          v-if="traceFoldable"
          type="button"
          class="flex items-center gap-1.5 self-start rounded px-1 py-0.5 text-[11.5px] text-wb-muted transition-colors hover:bg-wb-surface-hover hover:text-wb-ink"
          @click="traceOpen = false"
        >
          <Wrench class="h-3.5 w-3.5" />
          <span>{{ t('chat.processTitle') }}</span>
          <span class="text-wb-primary-strong">{{ t('chat.traceCollapse') }}</span>
          <ChevronDown class="h-3 w-3 rotate-180" />
        </button>
        <MessageBlocksRenderer
          :blocks="historyBlocks"
          :content="message.content ?? ''"
          :streaming_mode="false"
          :retryable="!streaming && isLast"
          @retry="onToolRetry"
        />
      </template>
    </div>

    <!-- 气泡样式统一收口在 wb-ui.css 的语义类（.msg-u / .bubble）：
         视觉令牌（底色/圆角/内边距/字号/行高）只定义一处，布局（对齐/限宽）在条目层，
         这样改一次全局生效，不会出现「同一种气泡两种内联值」的漂移。 -->
    <div v-if="!editing" :class="isUser ? 'msg-u' : 'bubble'">
      <span v-if="isUser" class="whitespace-pre-wrap"><template v-for="(seg, si) in userSegments" :key="si"><span v-if="seg.kind === 'mention'" class="utk">{{ seg.text }}</span><span v-else-if="seg.kind === 'cmd'" class="utk">{{ seg.text }}</span><template v-else>{{ seg.text }}</template></template></span>
      <!-- 用户附件（图片缩略图 / 文件 chip）：粘贴与拖拽的图片在这里回看 -->
      <div v-if="isUser && userAttachments.length > 0" class="mt-2 flex flex-wrap gap-2">
        <a
          v-for="a in userAttachments"
          :key="a.id"
          :href="a.url"
          target="_blank"
          rel="noopener"
          class="block overflow-hidden rounded-lg border border-white/25 transition-opacity hover:opacity-90"
          :title="a.name"
        >
          <img v-if="a.kind === 'image'" :src="a.url" :alt="a.name" class="max-h-40 max-w-[220px] object-cover" />
          <span v-else class="flex items-center gap-1 px-2 py-1 text-[11px] text-white/90">
            <Paperclip class="h-3 w-3" />{{ a.name }}
          </span>
        </a>
      </div>
    </div>

    <!-- 编辑模式 -->
    <div v-else class="flex w-full flex-col gap-2">
      <el-input
        v-model="editingContent"
        type="textarea"
        :rows="3"
        resize="none"
        @keydown.enter.ctrl="saveEdit"
        @keydown.esc="cancelEdit"
      />
      <div class="flex items-center gap-2">
        <el-button type="primary" size="small" @click="saveEdit">
          {{ t('chat.save') }}
        </el-button>
        <el-button size="small" @click="cancelEdit">{{ t('chat.cancel') }}</el-button>
        <span class="text-[10px] text-wb-muted">{{ t('chat.editHint') }}</span>
      </div>
    </div>

    <!-- 本轮文件变更（chat:file-change 按 run_id 关联）：内联 diff 卡，
         折叠态一行看清「改了哪个文件、增删多少」，点开就地看 diff / 回滚（Cursor 风格）。
         不再让用户为了看一眼 diff 而离开当前阅读上下文去侧栏。 -->
    <div v-if="!isUser && fileChanges.length > 0 && !editing" class="mt-2 w-full space-y-1.5">
      <div class="flex items-center gap-1.5 text-[11px] font-medium text-wb-ink">
        <FileText class="h-3.5 w-3.5 text-wb-mint" />
        {{ t('changes.title') }}
        <span class="text-wb-muted">·</span>
        <span class="text-wb-muted">{{ fileChanges.length }} {{ t('changes.files') }}</span>
      </div>
      <InlineDiffCard v-for="chg in fileChanges" :key="chg.id" :change="chg" />
    </div>

    <!-- 用量元信息：悬浮看精确值；为空隐藏 -->
    <UsageBadge
      v-if="!isUser && !editing"
      :usage="{
        input_tokens: message.input_tokens,
        output_tokens: message.output_tokens,
        cache_read_tokens: message.cache_read_tokens,
        total_tokens: message.total_tokens,
        latency_ms: message.latency_ms,
        cost: message.cost
      }"
    />

    <!-- 悬浮操作行：hover 出现在消息底部右侧（时间 + 复制/编辑/重生成/分叉/删除）。
         常驻占位 h-6 但内容透明，避免 hover 出现时布局跳动；隐藏态
         pointer-events-none 防幽灵 tooltip -->
    <div
      class="flex h-6 items-center gap-0.5 self-end transition-opacity"
      :class="hovered ? 'opacity-100' : 'pointer-events-none opacity-0'"
    >
      <span
        v-if="fmtTime(message.created_at)"
        class="mr-1 text-[10.5px] tabular-nums text-wb-muted/70"
      >{{ fmtTime(message.created_at) }}</span>
      <el-tooltip :content="t('chat.copy')" placement="top">
        <el-button text size="small" circle @click="copyMessage">
          <el-icon :size="13" :color="copiedID ? 'var(--wb-mint)' : undefined">
            <component :is="copiedID ? Check : Copy" />
          </el-icon>
        </el-button>
      </el-tooltip>

      <el-tooltip v-if="isUser" :content="t('chat.edit')" placement="top">
        <el-button text size="small" circle @click="startEdit">
          <el-icon :size="13"><Pencil /></el-icon>
        </el-button>
      </el-tooltip>

      <el-tooltip
        v-if="!isUser && isLast && !streaming"
        :content="t('chat.regenerate')"
        placement="top"
      >
        <el-button text size="small" circle @click="regenerate">
          <el-icon :size="13"><RefreshCw /></el-icon>
        </el-button>
      </el-tooltip>

      <!-- 从此处分叉（assistant 消息；复制到该条为止的对话到新会话） -->
      <el-tooltip v-if="!isUser && !streaming" :content="t('chat.fork')" placement="top">
        <el-button text size="small" circle @click="forkFrom">
          <el-icon :size="13"><GitBranch /></el-icon>
        </el-button>
      </el-tooltip>

      <el-tooltip :content="t('chat.delete')" placement="top">
        <el-button text size="small" circle @click="deleteMessage">
          <el-icon :size="13" color="var(--wb-danger)"><Trash2 /></el-icon>
        </el-button>
      </el-tooltip>
    </div>
  </div>
</template>
