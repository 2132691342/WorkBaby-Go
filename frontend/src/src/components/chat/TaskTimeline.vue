<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import type { Component } from 'vue'
import { useClipboard } from '@vueuse/core'
import {
  Clock,
  Type,
  Braces,
  Table,
  Database,
  BarChart3,
  Hash,
  Dices,
  Globe,
  Code2,
  Image as ImageIcon,
  LayoutTemplate,
  Cat,
  ScanText,
  FileText,
  FolderOpen,
  Calculator,
  Regex,
  PackageCheck,
  Sparkles,
  Brain,
  GitBranch,
  BookOpen,
  Check,
  X,
  Copy,
  ChevronDown,
  Loader2,
  Bot
} from '@/components/common/icons'
import { t } from '@/i18n'
import { looksLikeDiff, parseDiffLines, diffLineClass } from '@/chat/models/blocks'

/** 流式工具调用（与 chat store ToolCallInfo 对齐；含耗时）。 */
export interface ToolCall {
  id: string
  name: string
  args?: string
  result?: string
  state?: 'running' | 'success' | 'error'
  /** running 态实时计时用（与 ToolCallInfo.started_at 对齐）。 */
  startedAt?: number
  durationMs?: number
  /** 子 Agent 来源标签；空 = 主 Agent。 */
  agent?: string
}

const props = defineProps<{
  tools: ToolCall[]
  /** 失败工具是否展示「重试」按钮（仅历史末条 assistant 消息可重试，MessageList 控制）。 */
  retryable?: boolean
}>()

const emit = defineEmits<{
  /** 用户点击失败工具的「重试」→ MessageList 转 resendFrom 重新生成。 */
  retry: [tc: ToolCall]
}>()

// ===== 展开状态：受控 Set；错误与委派交付默认展开 =====
const expanded = ref<Set<string>>(new Set())

function toggle(tc: ToolCall): void {
  const next = new Set(expanded.value)
  if (next.has(tc.id)) next.delete(tc.id)
  else next.add(tc.id)
  expanded.value = next
}

function isExpanded(tc: ToolCall): boolean {
  if (expanded.value.has(tc.id)) return true
  if (tc.state === 'error') return true
  return isDelegate(tc) && !!tc.result
}

function setAll(open: boolean): void {
  expanded.value = new Set(open ? props.tools.map((x) => x.id) : [])
}

/** 有可展开详情（args 或 result）才显示展开箭头。 */
function hasDetail(tc: ToolCall): boolean {
  return !!(tc.args && !isDelegate(tc)) || !!tc.result
}

/** delegate_task = 子 Agent 委派卡片。 */
function isDelegate(tc: ToolCall): boolean {
  return tc.name === 'delegate_task'
}

/** 解析 delegate_task 参数 {agent, task}；坏 JSON 兜底。 */
function delegateMeta(tc: ToolCall): { agent: string; task: string } {
  try {
    const o = JSON.parse(tc.args || '{}') as { agent?: string; task?: string }
    return { agent: o.agent?.trim() || 'default', task: o.task ?? '' }
  } catch {
    return { agent: 'default', task: '' }
  }
}

/**
 * 行内参数摘要：从 args JSON 里挑最有信息量的单值（路径 / 命令 / 关键词），
 * 让「这一步在干什么」不用点开就知道——对标 Claude Code 的「● Read file.ts」单行形态。
 */
function argSummary(tc: ToolCall): string {
  if (!tc.args || isDelegate(tc)) return ''
  let o: Record<string, unknown>
  try {
    o = JSON.parse(tc.args) as Record<string, unknown>
  } catch {
    return oneLine(tc.args, 48)
  }
  const preferred = ['path', 'file_path', 'command', 'url', 'query', 'pattern', 'name', 'task', 'input', 'content']
  for (const k of preferred) {
    const v = o[k]
    if (typeof v === 'string' && v.trim()) return oneLine(v, 56)
  }
  for (const v of Object.values(o)) {
    if (typeof v === 'string' && v.trim()) return oneLine(v, 56)
  }
  return ''
}

function oneLine(s: string, max: number): string {
  const one = s.replace(/\s+/g, ' ').trim()
  return one.length > max ? `${one.slice(0, max)}…` : one
}

// ===== M2：单工具复制（args + result 原文）=====
const copiedID = ref<string | null>(null)
const { copy: copyToClipboard } = useClipboard({ legacy: true })

async function copyTool(tc: ToolCall): Promise<void> {
  const parts = [tc.name]
  if (tc.args) parts.push(`args: ${tc.args}`)
  if (tc.result) parts.push(`result: ${tc.result}`)
  try {
    await copyToClipboard(parts.join('\n'))
    copiedID.value = tc.id
    setTimeout(() => {
      if (copiedID.value === tc.id) copiedID.value = null
    }, 1500)
  } catch {
    /* 剪贴板不可用静默（工具结果仍可在面板查看） */
  }
}

/** 工具名 → 分类图标（前端静态映射，无需后端）。 */
function toolIcon(name: string): Component {
  const n = name.toLowerCase()
  if (n.includes('time') || n.startsWith('date_')) return Clock
  if (n.startsWith('text_')) return Type
  if (n.startsWith('json_')) return Braces
  if (n.startsWith('csv_')) return Table
  if (n.startsWith('data_')) return Database
  if (n.startsWith('chart_')) return BarChart3
  if (n.startsWith('hash_') || n.startsWith('base64_') || n.startsWith('url_')) return Hash
  if (n.startsWith('random_')) return Dices
  if (n.startsWith('http_') || n.startsWith('ip_')) return Globe
  if (n.startsWith('code_')) return Code2
  if (n.startsWith('image_') || n.startsWith('video_') || n.startsWith('audio_') || n.startsWith('model3d') || n.startsWith('vfx')) return ImageIcon
  if (n === 'gen_ui') return LayoutTemplate
  if (n.startsWith('pet_')) return Cat
  if (n.startsWith('ocr_')) return ScanText
  if (n.startsWith('pdf_') || n.startsWith('word_') || n.startsWith('excel_')) return FileText
  if (n.startsWith('file_') || n.startsWith('folder_') || n.startsWith('archive_')) return FolderOpen
  if (n.startsWith('math_')) return Calculator
  if (n.startsWith('regex_')) return Regex
  if (n.startsWith('present_')) return PackageCheck
  if (n === 'memory_write' || n.startsWith('memory_')) return Brain
  if (n === 'run_workflow' || n.startsWith('workflow_')) return GitBranch
  if (n.startsWith('knowledge_')) return BookOpen
  return Sparkles
}

/** 状态图标配色：running 柠檬黄、success 薄荷绿、error 红。 */
function stateIconClass(state?: string): string {
  if (state === 'success') return 'bg-wb-mint/15 text-wb-mint'
  if (state === 'error') return 'bg-wb-danger/15 text-wb-danger'
  return 'bg-wb-lemon/20 text-wb-lemon'
}

/**
 * running 态的实时计时。
 *
 * <p>长任务（OCR / 视频生成 / 大文件处理）跑几分钟时，只给一个转圈图标用户无法判断
 * 「是卡住了还是在跑」。有 running 工具才起 1s 定时器，跑完即停（不常驻）。
 */
const now = ref(Date.now())
let tickTimer: ReturnType<typeof setInterval> | null = null

const hasRunning = computed(() => props.tools.some((t) => t.state === 'running'))

watch(
  hasRunning,
  (running) => {
    if (running && tickTimer === null) {
      tickTimer = setInterval(() => {
        now.value = Date.now()
      }, 1000)
    } else if (!running && tickTimer !== null) {
      clearInterval(tickTimer)
      tickTimer = null
    }
  },
  { immediate: true }
)

onUnmounted(() => {
  if (tickTimer !== null) clearInterval(tickTimer)
})

/** 工具耗时：已收尾取 durationMs；running 取实时经过时长。 */
function elapsedMs(tc: ToolCall): number | undefined {
  if (tc.durationMs != null) return tc.durationMs
  if (tc.state === 'running' && tc.startedAt != null) return now.value - tc.startedAt
  return undefined
}

function fmtDuration(ms?: number): string {
  if (ms === undefined || ms === null) return ''
  if (ms < 1000) return `${ms}ms`
  const totalSec = ms / 1000
  if (totalSec < 60) return `${totalSec.toFixed(1)}s`
  const min = Math.floor(totalSec / 60)
  const sec = Math.round(totalSec % 60)
  return `${min}m${String(sec).padStart(2, '0')}s`
}
</script>

<template>
  <div>
    <!-- 头部：过程标题 + 计数 + 展开全部/收起全部 -->
    <div
      v-if="props.tools.length > 0"
      class="mb-1.5 flex items-center gap-2 text-[11px] font-medium text-wb-muted"
    >
      <span>{{ t('chat.processTitle') }}</span>
      <span class="rounded-full bg-wb-primary/10 px-1.5 py-px text-[10px] tabular-nums text-wb-primary-strong">
        {{ props.tools.length }}
      </span>
      <span v-if="props.tools.length > 1" class="ml-auto flex items-center gap-2 text-[11px] font-normal">
        <button type="button" class="transition-colors hover:text-wb-primary" @click="setAll(true)">
          {{ t('chat.expandAll') }}
        </button>
        <span class="text-wb-border">|</span>
        <button type="button" class="transition-colors hover:text-wb-primary" @click="setAll(false)">
          {{ t('chat.collapseAll') }}
        </button>
      </span>
    </div>

    <!-- 紧凑卡片流：单行摘要 + 点击展开详情 -->
    <ol class="space-y-1">
      <li
        v-for="tc in props.tools"
        :key="tc.id"
        class="wb-tool-card rounded-lg transition-colors"
        :class="tc.state === 'error' ? 'wb-tool-card-error' : ''"
      >
        <!-- 摘要行：状态 + 图标 + 工具名 + 参数摘要 + 耗时 + 操作 -->
        <div
          class="group flex min-w-0 items-center gap-2 px-2 py-1.5"
          :class="hasDetail(tc) ? 'cursor-pointer select-none' : ''"
          @click="hasDetail(tc) && toggle(tc)"
        >
          <span
            class="flex h-5 w-5 shrink-0 items-center justify-center rounded-md"
            :class="stateIconClass(tc.state)"
          >
            <Loader2 v-if="tc.state === 'running'" class="h-3 w-3 animate-spin" />
            <Check v-else-if="tc.state === 'success'" class="h-3 w-3" />
            <X v-else-if="tc.state === 'error'" class="h-3 w-3" />
            <Sparkles v-else class="h-3 w-3" />
          </span>

          <component
            :is="isDelegate(tc) ? Bot : toolIcon(tc.name)"
            class="h-3.5 w-3.5 shrink-0"
            :class="isDelegate(tc) ? 'text-wb-lavender' : 'text-wb-primary-strong'"
          />

          <span v-if="isDelegate(tc)" class="shrink-0 text-xs font-medium text-wb-ink">
            {{ t('chat.subAgent') }} · {{ delegateMeta(tc).agent }}
          </span>
          <span v-else class="shrink-0 font-mono text-xs font-medium text-wb-ink">{{ tc.name }}</span>

          <span
            v-if="tc.agent && !isDelegate(tc)"
            class="shrink-0 rounded bg-wb-lavender/15 px-1 text-[10px] text-wb-lavender"
          >
            {{ tc.agent }}
          </span>

          <!-- 行内参数摘要：一眼看到这一步做了什么 -->
          <span
            v-if="argSummary(tc) || (isDelegate(tc) && delegateMeta(tc).task)"
            class="min-w-0 flex-1 truncate font-mono text-[11px] text-wb-muted"
            :title="isDelegate(tc) ? delegateMeta(tc).task : argSummary(tc)"
          >
            {{ isDelegate(tc) ? delegateMeta(tc).task : argSummary(tc) }}
          </span>
          <span v-else class="min-w-0 flex-1" />

          <span
            v-if="elapsedMs(tc) != null"
            class="shrink-0 font-mono text-[10px] tabular-nums text-wb-muted"
          >
            {{ fmtDuration(elapsedMs(tc)) }}
          </span>
          <span v-if="tc.state === 'error'" class="shrink-0 text-[11px] text-wb-danger">
            {{ t('chat.failed') }}
          </span>

          <!-- hover 操作：复制 / 重试 -->
          <button
            type="button"
            class="hidden shrink-0 text-wb-muted transition-colors hover:text-wb-primary group-hover:block"
            :title="t('chat.copy')"
            @click.stop="copyTool(tc)"
          >
            <component :is="copiedID === tc.id ? Check : Copy" class="h-3 w-3" />
          </button>
          <button
            v-if="props.retryable && tc.state === 'error'"
            type="button"
            class="hidden shrink-0 text-[11px] text-wb-danger transition-colors hover:text-wb-primary group-hover:block"
            @click.stop="emit('retry', tc)"
          >
            {{ t('chat.retryTool') }}
          </button>

          <ChevronDown
            v-if="hasDetail(tc)"
            class="h-3 w-3 shrink-0 text-wb-muted transition-transform duration-200"
            :class="isExpanded(tc) ? 'rotate-180' : ''"
          />
        </div>

        <!-- 详情：参数 + 结果（diff 高亮渲染） -->
        <div v-if="isExpanded(tc)" class="wb-tool-detail space-y-1.5 px-2 pb-2 pl-9">
          <pre
            v-if="tc.args && !isDelegate(tc)"
            class="overflow-x-auto whitespace-pre-wrap rounded-md border border-wb-border/50 bg-wb-surface-2/60 p-2 font-mono text-[11px] leading-relaxed text-wb-ink"
          >{{ tc.args }}</pre>
          <div
            v-if="tc.result && looksLikeDiff(tc.result)"
            class="max-h-64 overflow-auto rounded-md border border-wb-border/50 bg-wb-surface-2/60 p-2 font-mono text-[11px] leading-relaxed"
          >
            <div v-for="(ln, li) in parseDiffLines(tc.result)" :key="li" :class="diffLineClass(ln.type)" class="whitespace-pre-wrap">
              {{ ln.text }}
            </div>
          </div>
          <pre
            v-else-if="tc.result"
            class="max-h-48 overflow-auto whitespace-pre-wrap rounded-md border border-wb-border/50 p-2 font-mono text-[11px] leading-relaxed"
            :class="tc.state === 'error' ? 'bg-wb-danger/10 text-wb-danger' : 'bg-wb-surface-2/60 text-wb-ink'"
          >{{ tc.result }}</pre>
        </div>
      </li>
    </ol>
  </div>
</template>

<style scoped>
/* 工具卡：默认透明融入消息，hover 浮现边界；错误卡常显红调左缘 */
.wb-tool-card {
  border-left: 2px solid transparent;
}
.wb-tool-card:hover {
  background: var(--wb-surface-2);
}
.wb-tool-card-error {
  border-left-color: var(--wb-danger);
  background: color-mix(in srgb, var(--wb-danger) 4%, transparent);
}
.wb-tool-detail {
  animation: wb-tool-in 0.18s ease-out both;
}
@keyframes wb-tool-in {
  from {
    opacity: 0;
    transform: translateY(-2px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
</style>
