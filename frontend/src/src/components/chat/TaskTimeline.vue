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
  Check,
  X,
  Copy,
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

// ===== M2：展开/收起全部 =====
/** null = 跟随默认（错误自动展开）；true/false = 用户强制全部展开/收起。 */
const openAll = ref<boolean | null>(null)

function detailOpen(tc: ToolCall): boolean {
  if (openAll.value !== null) return openAll.value
  if (tc.state === 'error') return true
  // 子 Agent 委派的結果即交付物，默认展开让用户直接看到摘要
  return isDelegate(tc) && !!tc.result
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
  return Sparkles
}

/**
 * 状态圆点配色（修正）：error 原先复用了品牌主色，与 success（绿）/running（黄）
 * 区分度不足且语义错误——失败不该用主色。现改为 danger 色。
 */
function stateDot(state?: string): string {
  if (state === 'success') return 'border-wb-mint/70 bg-wb-mint/30 text-wb-mint'
  if (state === 'error') return 'border-wb-danger/60 bg-wb-danger/15 text-wb-danger'
  return 'border-wb-lemon/70 bg-wb-lemon/30 text-wb-ink'
}

function stateLabel(state?: string): string {
  if (state === 'success') return t('chat.success')
  if (state === 'error') return t('chat.failed')
  return t('chat.running')
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
    <!-- 工具栏：展开/收起全部 -->
    <div v-if="props.tools.length > 1" class="mb-2 flex items-center justify-end gap-2 text-[11px]">
      <button
        type="button"
        class="text-wb-muted transition-colors hover:text-wb-primary"
        @click="openAll = openAll === true ? null : true"
      >
        {{ t('chat.expandAll') }}
      </button>
      <span class="text-wb-border">|</span>
      <button
        type="button"
        class="text-wb-muted transition-colors hover:text-wb-primary"
        @click="openAll = openAll === false ? null : false"
      >
        {{ t('chat.collapseAll') }}
      </button>
    </div>

    <ol class="space-y-0">
    <li v-for="(tc, i) in props.tools" :key="tc.id" class="relative flex gap-3" :class="tc.agent ? 'pl-4' : ''">
      <!-- 左侧：状态圆点 + 竖线 -->
      <div class="flex flex-col items-center">
        <span class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full border" :class="stateDot(tc.state)">
          <Loader2 v-if="tc.state === 'running'" class="h-3 w-3 animate-spin" />
          <Check v-else-if="tc.state === 'success'" class="h-3 w-3" />
          <X v-else-if="tc.state === 'error'" class="h-3 w-3" />
          <Sparkles v-else class="h-3 w-3" />
        </span>
        <span v-if="i < props.tools.length - 1" class="w-px flex-1 bg-wb-border" />
      </div>

      <!-- 右侧：步骤内容 -->
      <div class="min-w-0 flex-1 pb-3">
        <div class="flex items-center gap-2 text-xs">
          <component
            :is="isDelegate(tc) ? Bot : toolIcon(tc.name)"
            class="h-3.5 w-3.5 shrink-0"
            :class="isDelegate(tc) ? 'text-wb-lavender' : 'text-wb-primary-strong'"
          />
          <span v-if="isDelegate(tc)" class="font-medium text-wb-ink">
            {{ t('chat.subAgent') }} · {{ delegateMeta(tc).agent }}
          </span>
          <span v-else class="font-mono font-medium text-wb-ink">{{ tc.name }}</span>
          <span
            v-if="tc.agent && !isDelegate(tc)"
            class="shrink-0 rounded bg-wb-lavender/15 px-1 text-[10px] text-wb-lavender"
          >
            {{ tc.agent }}
          </span>
          <span :class="tc.state === 'error' ? 'text-wb-danger' : 'text-wb-muted'">
            {{ stateLabel(tc.state) }}
          </span>
          <span v-if="elapsedMs(tc) != null" class="ml-auto shrink-0 text-[11px] text-wb-muted">
            {{ fmtDuration(elapsedMs(tc)) }}
          </span>
          <!-- 复制整条调用 -->
          <button
            type="button"
            class="shrink-0 text-wb-muted transition-colors hover:text-wb-primary"
            :title="t('chat.copy')"
            @click="copyTool(tc)"
          >
            <component :is="copiedID === tc.id ? Check : Copy" class="h-3 w-3" />
          </button>
          <!-- 失败重试 -->
          <button
            v-if="props.retryable && tc.state === 'error'"
            type="button"
            class="shrink-0 text-[11px] text-wb-danger transition-colors hover:text-wb-primary"
            @click="emit('retry', tc)"
          >
            {{ t('chat.retryTool') }}
          </button>
        </div>
        <!-- 子 Agent 委派的任务描述（一眼看到委派了什么） -->
        <p
          v-if="isDelegate(tc) && delegateMeta(tc).task"
          class="mt-0.5 truncate text-[11px] text-wb-muted"
          :title="delegateMeta(tc).task"
        >
          {{ delegateMeta(tc).task }}
        </p>
        <details v-if="tc.args && !isDelegate(tc)" class="mt-1" :open="detailOpen(tc)">
          <summary class="cursor-pointer select-none text-[11px] text-wb-muted">{{ t('chat.args') }}</summary>
          <pre class="mt-1 overflow-x-auto whitespace-pre-wrap rounded-md bg-wb-primary/5 p-2 text-[11px] leading-relaxed text-wb-ink">{{ tc.args }}</pre>
        </details>
        <details v-if="tc.result" class="mt-1" :open="detailOpen(tc)">
          <summary
            class="cursor-pointer select-none text-[11px]"
            :class="tc.state === 'error' ? 'text-wb-danger' : 'text-wb-muted'"
          >
            {{ t('chat.result') }}
          </summary>
          <!-- diff 结果着色渲染：unified diff → 增删行高亮 -->
          <div
            v-if="looksLikeDiff(tc.result)"
            class="mt-1 max-h-64 overflow-auto rounded-md bg-wb-primary/5 p-2 font-mono text-[11px] leading-relaxed"
          >
            <div v-for="(ln, li) in parseDiffLines(tc.result)" :key="li" :class="diffLineClass(ln.type)" class="whitespace-pre-wrap">
              {{ ln.text }}
            </div>
          </div>
          <pre
            v-else
            class="mt-1 max-h-48 overflow-auto whitespace-pre-wrap rounded-md p-2 text-[11px] leading-relaxed"
            :class="tc.state === 'error' ? 'bg-wb-danger/10 text-wb-danger' : 'bg-wb-primary/5 text-wb-ink'"
          >{{ tc.result }}</pre>
        </details>
      </div>
    </li>
  </ol>
  </div>
</template>
