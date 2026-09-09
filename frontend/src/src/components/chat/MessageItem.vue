<script setup lang="ts">
import { computed, ref } from 'vue'
import { useClipboard } from '@vueuse/core'
import { Copy, Check, Pencil, RefreshCw, Trash2, GitBranch, FileText, Brain, ChevronDown } from '@/components/common/icons'
import type { Message } from '@/types/api'
import { t } from '@/i18n'
import { useChatStore } from '@/stores/chat'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { useFocusMode } from '@/composables/useFocusMode'
import MarkdownRenderer from '@/components/chat/MarkdownRenderer.vue'
import TaskTimeline from '@/components/chat/TaskTimeline.vue'
import UsageBadge from '@/components/chat/UsageBadge.vue'
import { resolveMessageBlocks, blocksToToolCalls, stripThinkBlocks, messageSkillHit } from '@/chat/models/blocks'
import { splitMentions } from '@/chat/models/tokens'
import type { ToolCallInfo } from '@/stores/chat/ChatStreamDecoder'

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

/** 历史消息的过程块 → ToolCallInfo（纯函数已在 blocks.ts 单测覆盖）。 */
const historyTools = computed<ToolCallInfo[]>(() =>
  blocksToToolCalls(resolveMessageBlocks(props.message))
)

/** 本条消息命中的 Skill（skill 块回放；刷新后执行过程首行仍在）。 */
const historySkillHit = computed(() => messageSkillHit(props.message))

/** 本条消息关联的文件变更（chat:file-change 已流式累积；历史消息按 run_id 精确对应）。 */
const fileChanges = computed(() => {
  if (!props.message.run_id) return []
  return chat.fileChanges.filter((c) => c.run_id === props.message.run_id)
})

/** 变更动作 → 展示色。 */
function changeTint(action: string): string {
  if (action === 'create') return 'text-wb-mint'
  if (action === 'delete') return 'text-wb-danger'
  return 'text-wb-primary-strong'
}
function changeTag(action: string): string {
  if (action === 'create') return t('changes.action.create')
  if (action === 'delete') return t('changes.action.delete')
  return t('changes.action.modify')
}

function fmtTime(iso: string | number): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const now = new Date()
  const sameDay = d.toDateString() === now.toDateString()
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  return sameDay ? hm : `${d.getMonth() + 1}/${d.getDate()} ${hm}`
}

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
    <!-- 顶部 meta：名字 + 时间 + 操作按钮（原型密度：10.5px 弱化灰） -->
    <div class="mb-1 flex items-center gap-1 px-1 text-[10.5px] text-wb-muted">
      <span v-if="!isUser" class="font-medium text-wb-ink">WorkBaby</span>
      <span v-if="fmtTime(message.created_at)">{{ fmtTime(message.created_at) }}</span>

      <!-- 操作按钮（hover 显示；隐藏态 pointer-events-none 防幽灵 tooltip） -->
      <div
        class="flex items-center gap-0.5 transition-opacity"
        :class="hovered ? 'opacity-100' : 'pointer-events-none opacity-0'"
      >
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

    <!-- 历史消息的思考过程回看（原型 .think 结构：圆角容器 + .hd 折叠按钮 + .bd 正文；
         wb-ui.css 里 .think.open > .hd .chev 自动 rotate，.think.open > .bd 自动 display:block） -->
    <div
      v-if="!focusMode && !isUser && message.thinking && !editing"
      class="think mt-1 w-full"
      :class="{ open: thinkingOpen }"
    >
      <button
        type="button"
        class="hd w-full text-left"
        style="background: transparent; border: 0"
        @click="thinkingOpen = !thinkingOpen"
      >
        <Brain class="ic" />
        <span>{{ t('chat.thoughtDone') }}</span>
        <ChevronDown class="ic chev" />
      </button>
      <pre class="bd max-h-48 overflow-y-auto whitespace-pre-wrap font-sans">{{ message.thinking }}</pre>
    </div>

    <!-- 历史过程块复现：工具调用/结果落库，刷新/切会话后完整回放（原型 .tl 自带边框，不再双重包装）；
         M2：末条 assistant 的失败工具可一键重试（转 resendFrom） -->
    <div v-if="!isUser && (historyTools.length > 0 || historySkillHit) && !editing" class="mt-2 w-full">
      <TaskTimeline
        :tools="historyTools"
        :skill-hit="historySkillHit"
        :retryable="!streaming && isLast"
        @retry="onToolRetry"
      />
    </div>

    <!-- 气泡 / 编辑模式（原型 02 屏）：用户 = .msg-u 主色实底 + 14/14/3/14 圆角；
         assistant = .bubble 正文裸排（不套盒子），工具时间线 / 变更 / 思考块各自自带边框 -->
    <div
      v-if="!editing"
      class="text-[13px]"
      :class="
        isUser
          ? 'rounded-[14px] rounded-br-[3px] bg-wb-primary px-[15px] py-[11px] leading-[1.75] text-white shadow-[var(--wb-shadow)]'
          : 'leading-[1.78] text-wb-ink'
      "
    >
      <span v-if="isUser" class="whitespace-pre-wrap"><template v-for="(seg, si) in userSegments" :key="si"><span v-if="seg.kind === 'mention'" class="utk">{{ seg.text }}</span><span v-else-if="seg.kind === 'cmd'" class="utk">{{ seg.text }}</span><template v-else>{{ seg.text }}</template></template></span>
      <MarkdownRenderer v-else-if="message.content?.trim()" :content="message.content" :streaming="false" />
      <!-- assistant 整轮只跑了工具、没写收尾文本：给个简洁占位，避免「消息空白但工具齐全」的违和 -->
      <span v-else-if="historyTools.length > 0 || historySkillHit" class="text-[12px] text-wb-muted">
        {{ t('chat.toolsOnlyMessage', historyTools.length) }}
      </span>
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

    <!-- 本轮文件变更摘要（chat:file-change 按 run_id 关联，一眼看到改了什么文件） -->
    <div
      v-if="!isUser && fileChanges.length > 0 && !editing"
      class="mt-1 w-full rounded-xl border border-wb-border bg-wb-mint/[0.04] p-3"
    >
      <div class="mb-1.5 flex items-center gap-1.5 text-[11px] font-medium text-wb-ink">
        <FileText class="h-3.5 w-3.5 text-wb-mint" />
        {{ t('changes.title') }}
        <span class="text-wb-muted">·</span>
        <span class="text-wb-muted">{{ fileChanges.length }} {{ t('changes.files') }}</span>
      </div>
      <ul class="space-y-1">
        <li v-for="chg in fileChanges" :key="chg.id" class="flex items-center gap-2 text-xs">
          <span
            class="w-11 shrink-0 rounded px-1 text-center text-[10px] font-medium"
            :class="[changeTint(chg.action), `bg-wb-primary/[0.04]`]"
          >
            {{ changeTag(chg.action) }}
          </span>
          <span class="min-w-0 flex-1 truncate font-mono text-[11px] text-wb-ink" :title="chg.rel_path">{{ chg.rel_path }}</span>
          <span class="shrink-0 font-mono text-[10px] tabular-nums">
            <span class="text-wb-mint">+{{ chg.added_lines }}</span>
            <span class="text-wb-danger">-{{ chg.removed_lines }}</span>
          </span>
          <span v-if="chg.rolled_back" class="shrink-0 rounded bg-wb-warning/15 px-1 text-[9px] text-wb-warning">{{ t('changes.rolledBack') }}</span>
        </li>
      </ul>
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
  </div>
</template>
