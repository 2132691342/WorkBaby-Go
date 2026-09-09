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
  Square,
  ChevronDown,
  Loader2,
  Bot
} from '@/components/common/icons'
import { t } from '@/i18n'
import type { SkillHit } from '@/types/api'
import { looksLikeDiff, parseDiffLines, diffLineClass } from '@/chat/models/blocks'

/** 流式工具调用（与 chat store ToolCallInfo 对齐；含耗时）。 */
export interface ToolCall {
  id: string
  name: string
  args?: string
  result?: string
  state?: 'running' | 'success' | 'error' | 'stopped'
  /** running 态实时计时用（与 ToolCallInfo.started_at 对齐）。 */
  startedAt?: number
  durationMs?: number
  /** 子 Agent 来源标签；空 = 主 Agent。 */
  agent?: string
}

const props = defineProps<{
  tools: ToolCall[]
  /** 本轮命中的 Skill；非空时作为时间线首行（叙事顺序：技能命中 → 工具 → 回答）。 */
  skillHit?: SkillHit | null
  /** 失败工具是否展示「重试」按钮（仅历史末条 assistant 消息可重试，MessageList 控制）。 */
  retryable?: boolean
}>()

const emit = defineEmits<{
  /** 用户点击失败工具的「重试」→ MessageList 转 resendFrom 重新生成。 */
  retry: [tc: ToolCall]
}>()

// ===== 展开状态：显式覆盖优先，其次按默认策略 =====
//
// 默认策略（只看异常与委派交付）与用户显式操作必须分开存：
// 混在一个 Set 里会出现「失败项永远展开、收起全部对它无效」——
// 失败的工具调用往往结果最长，恰恰是最需要能收起来的。
const PREF_KEY = 'wb.toolTimelineAutoOpen'

/** 是否自动展开异常 / 委派交付（用户选过「收起全部」后记住，后续轮次不再强展开）。 */
const autoOpen = ref(readPref())

function readPref(): boolean {
  try {
    return localStorage.getItem(PREF_KEY) !== 'false'
  } catch {
    return true
  }
}

function writePref(v: boolean): void {
  try {
    localStorage.setItem(PREF_KEY, String(v))
  } catch {
    // 隐私模式下不可写，仅本次会话生效
  }
}

/** 工具 id → 用户显式指定的展开态。 */
const manual = ref<Map<string, boolean>>(new Map())

function defaultExpanded(tc: ToolCall): boolean {
  return tc.state === 'error' || (isDelegate(tc) && !!tc.result)
}

function isExpanded(tc: ToolCall): boolean {
  const m = manual.value.get(tc.id)
  if (m !== undefined) return m
  return autoOpen.value && defaultExpanded(tc)
}

function toggle(tc: ToolCall): void {
  const next = new Map(manual.value)
  next.set(tc.id, !isExpanded(tc))
  manual.value = next
}

/** 全部展开 / 全部收起；同时把选择记成默认策略，之后的新时间线也照此办理。 */
function setAll(open: boolean): void {
  autoOpen.value = open
  writePref(open)
  manual.value = new Map(props.tools.map((x) => [x.id, open]))
}

/** 当前是否已全部展开（决定头部按钮文案）。 */
const allExpanded = computed(
  () => props.tools.length > 0 && props.tools.every((t) => isExpanded(t))
)

/** 有可展开详情（args 或 result）才显示展开箭头。 */
function hasDetail(tc: ToolCall): boolean {
  return !!(tc.args && !isDelegate(tc)) || !!tc.result
}

/** delegate_task = 子 Agent 委派卡片。 */
function isDelegate(tc: ToolCall): boolean {
  return tc.name === 'delegate_task'
}

// ===== 技能命中行 =====
/** 命中行展开态：默认折叠（只是背景信息，不抢占注意力）。 */
const skillOpen = ref(false)

/** 命中行展开详情：描述 + 来源 + 注入规模 + 本轮放开的工具。 */
const skillDetail = computed(() => {
  const s = props.skillHit
  if (!s) return ''
  const lines: string[] = []
  if (s.description) lines.push(s.description)
  const meta = [s.source, s.version].filter((x) => !!x).join(' · ')
  if (meta) lines.push(`${t('chat.skillSource')}：${meta}`)
  if (s.injected_chars) lines.push(t('chat.skillInjected', s.injected_chars))
  lines.push(
    `${t('chat.skillTools')}：${s.tools && s.tools.length > 0 ? s.tools.join(' / ') : t('chat.skillNoLimit')}`
  )
  return lines.join('\n')
})

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
  <div v-if="props.tools.length > 0 || props.skillHit" class="tl">
    <div class="tl-hd">
      <ListTree class="ic" style="width: 13px; height: 13px" />
      <span>{{ t('chat.processTitle') }}</span>
      <span class="cnt">{{ props.tools.length + (props.skillHit ? 1 : 0) }}</span>
      <button v-if="props.tools.length > 1" type="button" @click="setAll(!allExpanded)">
        {{ allExpanded ? t('chat.collapseAll') : t('chat.expandAll') }}
      </button>
    </div>

    <!-- 技能命中：先于所有工具，回答「这一轮为什么按这个套路走」 -->
    <div v-if="props.skillHit" class="tool" :class="{ open: skillOpen }">
      <button type="button" class="tool-hd" @click="skillOpen = !skillOpen">
        <span class="st"><Check class="ok" /></span>
        <Sparkles class="ic text-wb-lavender" />
        <span class="nm">{{ t('chat.skillHit', props.skillHit.name) }}</span>
        <span class="arg">{{ t('chat.skillHitSummary') }}</span>
        <ChevronDown class="chev" />
      </button>
      <div v-if="skillOpen" class="tool-bd">
        <p class="lb">{{ t('chat.skillInjectedTitle') }}</p>
        <pre>{{ skillDetail }}</pre>
      </div>
    </div>

    <div
      v-for="tc in props.tools"
      :key="tc.id"
      class="tool"
      :class="{ open: isExpanded(tc), err: tc.state === 'error' }"
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
          <Square v-else-if="tc.state === 'stopped'" class="pend" />
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

        <!-- hover 操作：行头本身是 button，这里只能用 span（button 嵌套会被浏览器
             提前闭合外层，破坏行结构与折叠交互），键盘可达性由 role/tabindex 补上 -->
        <span
          v-if="hasDetail(tc)"
          role="button"
          tabindex="0"
          class="shrink-0 cursor-pointer text-wb-muted transition-colors hover:text-wb-primary"
          :title="t('chat.copy')"
          @click.stop="copyTool(tc)"
          @keydown.enter.prevent.stop="copyTool(tc)"
          @keydown.space.prevent.stop="copyTool(tc)"
        >
          <component :is="copiedID === tc.id ? Check : Copy" style="width: 12px; height: 12px" />
        </span>
        <span
          v-if="props.retryable && tc.state === 'error'"
          role="button"
          tabindex="0"
          class="shrink-0 cursor-pointer text-[11px] text-wb-danger transition-colors hover:text-wb-primary"
          @click.stop="emit('retry', tc)"
          @keydown.enter.prevent.stop="emit('retry', tc)"
          @keydown.space.prevent.stop="emit('retry', tc)"
        >
          {{ t('chat.retryTool') }}
        </span>

        <ChevronDown v-if="hasDetail(tc)" class="chev" />
      </button>

      <div v-if="isExpanded(tc)" class="tool-bd">
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
/* 超长结果限高：一条失败调用不该把整屏占满（内部可滚动看全文） */
.tool-bd pre {
  max-height: 14rem;
  overflow: auto;
  overscroll-behavior: contain;
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
