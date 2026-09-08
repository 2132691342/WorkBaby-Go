<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import type { Component } from 'vue'
import { useClipboard } from '@vueuse/core'
import {
  Clock,
  ListTree,
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
  <!-- 原型 .tl / .tool 折叠列表：标题栏 + 工具行（点击展开 args/result）。
       保留现有能力：状态图标 / 工具图标 / 行内 arg 摘要 / 实时计时 / 复制 / 重试 / 子 Agent 委派卡片 -->
  <div v-if="props.tools.length > 0" class="tl">
    <div class="tl-hd">
      <ListTree class="ic" style="width: 13px; height: 13px" />
      <span>{{ t('chat.processTitle') }}</span>
      <span class="cnt">{{ props.tools.length }}</span>
      <button v-if="props.tools.length > 1" type="button" @click="setAll(true)">{{ t('chat.expandAll') }}</button>
      <button v-if="props.tools.length > 1" type="button" @click="setAll(false)">{{ t('chat.collapseAll') }}</button>
    </div>

    <div
      v-for="tc in props.tools"
      :key="tc.id"
      class="tool"
      :class="{ open: isExpanded(tc) || tc.state === 'error', err: tc.state === 'error' }"
    >
      <button
        type="button"
        class="tool-hd"
        :style="!hasDetail(tc) ? 'cursor: default' : ''"
        @click="hasDetail(tc) && toggle(tc)"
      >
        <!-- 状态图标 -->
        <span class="st">
          <Loader2 v-if="tc.state === 'running'" class="run animate-spin" />
          <Check v-else-if="tc.state === 'success'" class="ok" />
          <X v-else-if="tc.state === 'error'" class="fail" />
          <Clock v-else class="pend" />
        </span>

        <!-- 工具图标（子 Agent 委派用 Bot） -->
        <component
          :is="isDelegate(tc) ? Bot : toolIcon(tc.name)"
          class="ic"
          :class="isDelegate(tc) ? 'text-wb-lavender' : 'text-wb-primary-strong'"
        />

        <!-- 名称 -->
        <span v-if="isDelegate(tc)" class="nm" style="font-family: inherit">
          {{ t('chat.subAgent') }} · {{ delegateMeta(tc).agent }}
        </span>
        <span v-else class="nm">{{ tc.name }}</span>

        <!-- 子 Agent 来源徽标 -->
        <span
          v-if="tc.agent && !isDelegate(tc)"
          class="shrink-0 rounded bg-wb-lavender/15 px-1 text-[10px] text-wb-lavender"
        >
          {{ tc.agent }}
        </span>

        <!-- 行内 arg 摘要 -->
        <span
          v-if="argSummary(tc) || (isDelegate(tc) && delegateMeta(tc).task)"
          class="arg"
          :title="isDelegate(tc) ? delegateMeta(tc).task : argSummary(tc)"
        >
          {{ isDelegate(tc) ? delegateMeta(tc).task : argSummary(tc) }}
        </span>

        <!-- 耗时 -->
        <span v-if="elapsedMs(tc) != null" class="ms">{{ fmtDuration(elapsedMs(tc)) }}</span>

        <!-- hover 操作 -->
        <button
          v-if="hasDetail(tc)"
          type="button"
          class="shrink-0 text-wb-muted transition-colors hover:text-wb-primary"
          :title="t('chat.copy')"
          @click.stop="copyTool(tc)"
        >
          <component :is="copiedID === tc.id ? Check : Copy" style="width: 12px; height: 12px" />
        </button>
        <button
          v-if="props.retryable && tc.state === 'error'"
          type="button"
          class="shrink-0 text-[11px] text-wb-danger transition-colors hover:text-wb-primary"
          @click.stop="emit('retry', tc)"
        >
          {{ t('chat.retryTool') }}
        </button>

        <ChevronDown v-if="hasDetail(tc)" class="chev" />
      </button>

      <div v-if="isExpanded(tc) || tc.state === 'error'" class="tool-bd">
        <template v-if="isDelegate(tc)">
          <p class="lb">task</p>
          <pre>{{ delegateMeta(tc).task || '—' }}</pre>
        </template>
        <template v-else-if="tc.args">
          <p class="lb">args</p>
          <pre>{{ tc.args }}</pre>
        </template>
        <template v-if="tc.result && looksLikeDiff(tc.result)">
          <p class="lb">result</p>
          <pre class="diff"><span
            v-for="(ln, li) in parseDiffLines(tc.result)"
            :key="li"
            :class="diffLineClass(ln.type)"
          >{{ ln.text }}
</span></pre>
        </template>
        <template v-else-if="tc.result">
          <p class="lb">result</p>
          <pre :class="tc.state === 'error' ? 'diff' : ''">{{ tc.result }}</pre>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 原型 .tool / .tool-bd 的全局样式已在 wb-ui.css 里定义；这里只做
   展开动效（与 .approve / .tl 行展开保持一致感）。 */
.tool-bd {
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
