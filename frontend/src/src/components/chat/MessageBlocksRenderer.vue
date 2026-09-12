<script setup lang="ts">
/**
 * 按事件到达顺序渲染 assistant 的完整过程块
 * （thinking / text / tool_call / tool_result / artifact / skill / genui）。
 * 同时接受历史稳定序列（ResolvedBlock[]）与流式暂存序列（StreamingBlock[]），
 * 内部统一转为 RenderBlock 后只写一遍渲染逻辑。
 */
import { computed, ref, watch } from 'vue'
import { Brain, Sparkles, Loader2, Square, Check, X, ChevronDown } from '@/components/common/icons'
import { t } from '@/i18n'
import { looksLikeDiff, parseDiffLines, diffLineClass } from '@/chat/models/blocks'
import { toolIcon, toolLabel } from '@/chat/models/toolVisuals'
import type { ResolvedBlock } from '@/chat/models/blocks'
import type { StreamingBlock } from '@/chat/models/streamingBlocks'
import type { UiNode } from '@/components/genui/GenUiRenderer.vue'
import GenUiRenderer from '@/components/genui/GenUiRenderer.vue'
import MarkdownRenderer from '@/components/chat/MarkdownRenderer.vue'

const props = defineProps<{
  /** 历史块序列（来自 chat.models.blocks）。 */
  blocks?: ResolvedBlock[] | null
  /** 流式块序列（来自 chat.models.streamingBlocks）。 */
  streaming?: StreamingBlock[] | null
  /** 消息正文（message.content；持久化模型里 text 不入块，作为最后一条 text 块补齐渲染）。
   *  流式期此字段为空（streamingContent 在 StreamingBubble 单独渲染 +cursor 增强）。 */
  content?: string
  /** 是否流式中（用于决定『光标』/running 态显示）。 */
  streaming_mode?: boolean
  /** 流式 tick（running 工具的实时计时）。 */
  now?: number
  /** 失败工具可重试（仅历史末条 assistant 消息，MessageList 控制）。 */
  retryable?: boolean
}>()

const emit = defineEmits<{
  retry: [toolCallId: string]
}>()
void emit

/** 统一渲染块：两种输入归一为同一结构。 */
interface RenderBlock {
  kind: 'thinking' | 'text' | 'tool_call' | 'tool_result' | 'artifact' | 'skill' | 'genui'
  /** 块内顺序（用于 DOM key）。 */
  seq: number
  /** thinking / text 全文。 */
  text: string
  /** 结构化数据：text 块为 null；其他 kind 按需使用 */
  data: Record<string, unknown> | null
  /** tool_call 块。 */
  call?: { id: string; name: string; arguments: string }
  /** tool_result 块。 */
  result?: { toolCallId: string; name: string; content: string; error?: string; durationMs?: number; refused?: boolean; uiHint?: string }
  /** skill 块。 */
  skill?: Record<string, unknown>
  /** artifact 块。 */
  artifact?: { name: string; data: unknown }
  /** genui 块。 */
  genui?: UiNode
  /** running 态（流式期）：tool_result 还未到 → 显示转圈。 */
  running?: boolean
}

const renderBlocks = computed<RenderBlock[]>(() => {
  const out: RenderBlock[] = []
  if (props.blocks && props.blocks.length > 0) {
    for (let i = 0; i < props.blocks.length; i++) {
      const b = props.blocks[i]
      const rb: RenderBlock = { kind: b.kind as RenderBlock['kind'], seq: i, text: b.text, data: null }
      if (b.kind === 'tool_call' && b.data) {
        rb.call = {
          id: String(b.data.id ?? ''),
          name: String(b.data.name ?? 'unknown'),
          arguments: String(b.data.arguments ?? '')
        }
      } else if (b.kind === 'tool_result' && b.data) {
        rb.result = {
          toolCallId: String(b.data.tool_call_id ?? ''),
          name: String(b.data.name ?? 'unknown'),
          content: String(b.data.content ?? ''),
          error: typeof b.data.error === 'string' ? b.data.error : undefined,
          durationMs: typeof b.data.duration_ms === 'number' ? b.data.duration_ms : undefined,
          refused: b.data.refused === true,
          uiHint: typeof b.data.ui_hint === 'string' ? b.data.ui_hint : undefined
        }
      } else if (b.kind === 'skill' && b.data) {
        rb.skill = b.data
      } else if (b.kind === 'artifact' && b.data) {
        rb.artifact = { name: String(b.data.name ?? 'artifact'), data: b.data.data }
      } else if (b.kind === 'genui' && b.data) {
        rb.genui = (b.data as { root?: UiNode }).root ?? (b.data as unknown as UiNode)
      }
      out.push(rb)
    }
  } else if (props.streaming && props.streaming.length > 0) {
    for (let i = 0; i < props.streaming.length; i++) {
      const b = props.streaming[i]
      const rb: RenderBlock = { kind: b.kind as RenderBlock['kind'], seq: i, text: b.text, data: null }
      if (b.kind === 'tool_call') {
        rb.call = {
          id: b.toolCallId ?? b.id,
          name: b.name ?? 'unknown',
          arguments: String(b.data?.arguments ?? '')
        }
      } else if (b.kind === 'tool_result') {
        rb.result = {
          toolCallId: b.toolCallId ?? '',
          name: b.name ?? 'unknown',
          content: b.text,
          error: typeof b.data?.error === 'string' ? b.data.error : undefined,
          durationMs: b.durationMs,
          refused: b.state === 'refused'
        }
        rb.running = b.state === 'running'
      } else if (b.kind === 'skill') {
        rb.skill = b.data ?? undefined
      } else if (b.kind === 'artifact') {
        rb.artifact = { name: String(b.data?.name ?? 'artifact'), data: b.data?.data }
      } else if (b.kind === 'genui') {
        rb.genui = (b.data as { root?: UiNode })?.root ?? (b.data as unknown as UiNode)
      }
      out.push(rb)
    }
  }
  // 历史消息的正文（message.content）补为最后一条 text 块。
  // 流式期不传 content（streamingContent 在 StreamingBubble 用专属 slot 渲染 + cursor 增强）。
  const tail = (props.content ?? '').trim()
  if (tail) {
    const seq = out.length
    out.push({ kind: 'text', seq, text: tail, data: null })
  }
  return out
})

/**
 * 把 tool_call 与同 tool_call_id 的 tool_result 配对为单个工具单元（避免同一工具渲染两次）；
 * 无对应 call 的孤立 tool_result 退化为单独展示。
 */
const renderUnits = computed(() => {
  const blocks = renderBlocks.value
  // 第一遍：建 call_id → result 的索引
  const resultByCallID = new Map<string, RenderBlock>()
  for (const b of blocks) {
    if (b.kind === 'tool_result' && b.result) {
      resultByCallID.set(b.result.toolCallId, b)
    }
  }
  // 第二遍：合并 call 与 result；从结果列表剔除已配对的 tool_result
  const out: RenderBlock[] = []
  for (const b of blocks) {
    if (b.kind === 'tool_call' && b.call) {
      const r = resultByCallID.get(b.call.id)
      if (r) {
        out.push({
          ...b,
          result: r.result,
          running: r.running
        })
      } else {
        out.push(b)
      }
    } else if (b.kind === 'tool_result') {
      // 已配对 → 跳过；未配对（无对应 call）→ 单独展示
      if (!resultByCallID.get(b.result?.toolCallId ?? '') || !blocks.some((x) => x.kind === 'tool_call' && x.call?.id === b.result?.toolCallId)) {
        out.push(b)
      }
      // 注意：上面条件等价于『存在对应的 tool_call 且已被合并』则跳过；否则保留。
    } else {
      out.push(b)
    }
  }
  return out
})

// ===== 折叠状态（历史块用；流式期不折叠） =====
const localOpen = ref(new Map<string, boolean>())
function isExpandable(b: RenderBlock): boolean {
  if (b.kind === 'tool_call') return !!(b.call?.arguments || b.result?.content)
  if (b.kind === 'tool_result') return !!b.result?.content
  return false
}

function isError(b: RenderBlock): boolean {
  return !!b.result?.error && !b.result.refused
}

/** 工具调用 args → 一行摘要（沿用 TaskTimeline.argSummary 口径：从 JSON 提取 path/command/url/query 等关键字段）。
 *  返回空表示不显示摘要（工具头部只展示名字）。 */
function argSummary(b: RenderBlock): string {
  if (!b.call?.arguments) return ''
  // 子 Agent 委派卡有专属渲染，跳过摘要
  if (b.call.name === 'delegate_task') return ''
  let o: Record<string, unknown>
  try {
    o = JSON.parse(b.call.arguments) as Record<string, unknown>
  } catch {
    return oneLine(b.call.arguments, 56)
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

/** 工具结果的内容展示：识别行列表（file_list / doc_reader 等多行输出）按行渲染；其余按原文。 */
function toolResultLines(content: string): string[] {
  if (!content) return []
  return content.split('\n').filter((l) => l.length > 0)
}

function isOpen(b: RenderBlock): boolean {
  const key = `${b.kind}:${b.seq}`
  const m = localOpen.value.get(key)
  if (m !== undefined) return m
  return isError(b) || (props.streaming_mode === true && b.kind === 'tool_call')
}
function toggle(b: RenderBlock): void {
  const key = `${b.kind}:${b.seq}`
  const next = new Map(localOpen.value)
  next.set(key, !isOpen(b))
  localOpen.value = next
}

// ===== 思考面板：thinking 块在流式中自动展开；其他默认折叠 =====
const thinkingOpen = ref(true)
watch(
  () => renderBlocks.value.some((b) => b.kind === 'text'),
  (hasText, prev) => {
    // 首条 text 出现 → 自动折叠思考
    if (hasText && !prev) thinkingOpen.value = false
  }
)
</script>

<template>
  <div class="wb-blocks">
    <!-- 块按顺序渲染：thinking / tool_call / tool_result / artifact / skill / genui / text。
         注意：循环 renderUnits（配对后的视图），不是 renderBlocks（原始数据）；
         这样 tool_call 与对应 tool_result 合并展示为一个工具单元，避免视觉上的『两次工具』。 -->
    <template v-for="b in renderUnits" :key="`${b.kind}:${b.seq}`">
      <!-- 思考块 -->
      <div v-if="b.kind === 'thinking' && b.text" class="wb-block-thinking">
        <button
          type="button"
          class="wb-think-toggle"
          :class="{ open: thinkingOpen }"
          @click="thinkingOpen = !thinkingOpen"
        >
          <Brain class="wb-ic" />
          <span>{{ thinkingOpen ? t('chat.thinking') : t('chat.thoughtDone') }}</span>
          <ChevronDown class="wb-chev" :class="{ rotate: thinkingOpen }" />
        </button>
        <pre v-if="thinkingOpen" class="wb-think-body">{{ b.text }}</pre>
      </div>

      <!-- 文本块：内置 MarkdownRenderer 作为默认渲染，父组件可用 #text slot 覆盖（流式期挂光标）。
           关键：内联 fallback 必填——若父组件未传 slot，slot 内部为空会让历史文本消失
           （早期重构的回归 bug：MessageItem 没传 #text，块里没渲染任何东西）。 -->
      <div v-else-if="b.kind === 'text' && b.text" class="wb-block-text">
        <slot name="text" :content="b.text" :streaming="streaming_mode === true && b === renderUnits[renderUnits.length - 1]">
          <div class="flex items-end gap-1">
            <MarkdownRenderer :content="b.text" :streaming="streaming_mode === true && b === renderUnits[renderUnits.length - 1]" />
            <span v-if="streaming_mode === true && b === renderUnits[renderUnits.length - 1]" class="wb-cursor" />
          </div>
        </slot>
      </div>

      <!-- 工具单元：tool_call 与 result 配对，单一头部 + 展开区。
           流式期 running → 头部 spinner；成功 → 绿勾；refused → 黄 X；error → 红方块。 -->
      <div v-else-if="b.kind === 'tool_call' && b.call" class="wb-block-tool wb-tool" :class="{ open: isOpen(b), err: isError(b) }">
        <button
          type="button"
          class="wb-tool-hd"
          :style="!isExpandable(b) ? 'cursor: default' : ''"
          @click="isExpandable(b) && toggle(b)"
        >
          <span class="wb-tool-st">
            <Loader2 v-if="b.running" class="wb-loader animate-spin" />
            <Check v-else-if="b.result && !b.result.refused && !b.result.error" class="wb-ok" />
            <X v-else-if="b.result?.refused" class="wb-refused" />
            <Square v-else-if="b.result && b.result.error" class="wb-fail" />
            <Square v-else class="wb-pend" />
          </span>
          <component :is="toolIcon(b.call.name)" class="wb-ic text-wb-primary-strong" />
          <span class="wb-tool-nm" :title="b.call.name">{{ toolLabel(b.call.name) }}</span>
          <!-- 关键参数摘要（path / command / url / query ...）—— 不展开也能看到工具在干啥 -->
          <span v-if="argSummary(b)" class="wb-tool-arg" :title="argSummary(b)">{{ argSummary(b) }}</span>
          <span v-if="b.result?.durationMs != null" class="wb-tool-ms">
            {{ (b.result.durationMs / 1000).toFixed(1) }}s
          </span>
        </button>
        <div v-if="isOpen(b) && (b.call.arguments || b.result?.content)" class="wb-tool-bd">
          <p v-if="b.call.arguments" class="wb-tool-lb">args</p>
          <pre v-if="b.call.arguments" class="wb-tool-pre"><code>{{ b.call.arguments }}</code></pre>
          <p v-if="b.result?.content" class="wb-tool-lb">result</p>
          <!-- 多行结果按行展示（file_list / doc_reader 等） -->
          <ul v-if="b.result?.content && toolResultLines(b.result.content).length > 1" class="wb-tool-lines">
            <li v-for="(ln, li) in toolResultLines(b.result.content)" :key="li">
              <span v-if="b.result && b.result.name === 'file_list'" class="wb-tool-line-path">📄</span>
              <span v-else-if="b.result && b.result.name === 'file_glob'" class="wb-tool-line-path">🔍</span>
              <code>{{ ln }}</code>
            </li>
          </ul>
          <pre v-else-if="b.result?.content && (b.result.uiHint === 'diff' || looksLikeDiff(b.result.content))" class="wb-tool-pre"><span
              v-for="(ln, li) in parseDiffLines(b.result.content)"
              :key="li"
              :class="diffLineClass(ln.type)"
            >{{ ln.text }}
</span></pre>
          <pre v-else-if="b.result?.content" class="wb-tool-pre">{{ b.result.content }}</pre>
        </div>
      </div>

      <!-- 孤立 tool_result（无对应 tool_call，理论上不应发生）：退化为单独展示 -->
      <div v-else-if="b.kind === 'tool_result' && b.result" class="wb-block-tool wb-tool" :class="{ open: isOpen(b), err: isError(b) }">
        <button type="button" class="wb-tool-hd" @click="isExpandable(b) && toggle(b)">
          <span class="wb-tool-st">
            <Loader2 v-if="b.running" class="wb-loader animate-spin" />
            <Check v-else-if="!b.result.refused && !b.result.error" class="wb-ok" />
            <X v-else-if="b.result.refused" class="wb-refused" />
            <Square v-else class="wb-fail" />
          </span>
          <component :is="toolIcon(b.result.name)" class="wb-ic text-wb-primary-strong" />
          <span class="wb-tool-nm">{{ toolLabel(b.result.name) }}</span>
        </button>
        <pre v-if="isOpen(b) && b.result.content" class="wb-tool-bd">{{ b.result.content }}</pre>
      </div>

      <!-- Skill 命中：单行 chip -->
      <div v-else-if="b.kind === 'skill' && b.skill" class="wb-block-skill">
        <span class="wb-skill-chip"><Sparkles class="wb-ic text-wb-lavender" /> {{ t('chat.skillHit', b.skill.name ?? '') }}</span>
      </div>

      <!-- Artifact 块：简化展示 -->
      <div v-else-if="b.kind === 'artifact' && b.artifact" class="wb-block-artifact">
        <div class="wb-art-card">{{ b.artifact.name }}</div>
      </div>

      <!-- GenUI：内联渲染 -->
      <div v-else-if="b.kind === 'genui' && b.genui" class="wb-block-genui">
        <GenUiRenderer :node="b.genui" />
      </div>
    </template>
  </div>
</template>

<style scoped>
.wb-blocks {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.wb-block-thinking {
  display: flex;
  flex-direction: column;
}
.wb-think-toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 2px 6px;
  border: 0;
  border-radius: 4px;
  background: transparent;
  font-size: 11.5px;
  color: var(--wb-muted);
  cursor: pointer;
  align-self: flex-start;
  transition: color 0.15s ease, background 0.15s ease;
}
.wb-think-toggle:hover {
  background: var(--wb-surface-hover);
  color: var(--wb-ink);
}
.wb-think-toggle .wb-chev {
  width: 11px;
  height: 11px;
  transition: transform 0.18s ease;
}
.wb-think-toggle.open .wb-chev {
  transform: rotate(180deg);
}
.wb-think-body {
  margin: 4px 0 0 22px;
  padding: 0 0 0 10px;
  border-left: 2px solid var(--wb-border);
  font-family: inherit;
  font-size: 11.5px;
  line-height: 1.75;
  white-space: pre-wrap;
  color: var(--wb-muted);
  max-height: 8rem;
  overflow-y: auto;
}

.wb-block-text {
  display: contents;
}

/* 工具行：与 TaskTimeline 同形态但精简（无外框） */
.wb-tool {
  display: flex;
  flex-direction: column;
  border-radius: 6px;
  background: transparent;
  transition: background 0.15s ease;
}
.wb-tool-hd {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 2px 6px;
  border: 0;
  background: transparent;
  text-align: left;
  cursor: pointer;
  align-self: flex-start;
  border-radius: 4px;
}
.wb-tool-hd:hover {
  background: var(--wb-surface-hover);
}
.wb-ic {
  width: 13px;
  height: 13px;
}
.wb-tool-nm {
  font-size: 11.5px;
  font-weight: 500;
  color: var(--wb-ink);
}
.wb-tool-st {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 13px;
  height: 13px;
}
.wb-ok { color: var(--wb-mint); width: 13px; height: 13px; }
.wb-refused { color: var(--wb-warning); width: 13px; height: 13px; }
.wb-fail { color: var(--wb-danger); width: 13px; height: 13px; }
.wb-loader { width: 12px; height: 12px; color: var(--wb-muted); }
.wb-tool-ms {
  font-size: 10.5px;
  color: var(--wb-muted);
  font-variant-numeric: tabular-nums;
  margin-left: 2px;
}
.wb-tool-bd {
  margin: 4px 0 0 24px;
  padding: 6px 8px;
  background: var(--wb-surface);
  border-radius: 6px;
  max-height: 14rem;
  overflow: auto;
  overscroll-behavior: contain;
}
.wb-tool-lb {
  margin: 0 0 2px;
  font-size: 9.5px;
  font-weight: 500;
  color: var(--wb-muted);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.wb-tool-pre {
  margin: 0 0 6px;
  font-family: ui-monospace, monospace;
  font-size: 11.5px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--wb-ink);
}
.wb-tool-lines {
  list-style: none;
  padding: 0;
  margin: 0 0 4px;
  font-family: ui-monospace, monospace;
  font-size: 11.5px;
  line-height: 1.6;
}
.wb-tool-lines li {
  display: flex;
  align-items: baseline;
  gap: 6px;
  padding: 1px 4px;
  border-radius: 3px;
}
.wb-tool-lines li:hover {
  background: var(--wb-surface-hover);
}
.wb-tool-line-path {
  flex: none;
  font-size: 10px;
  opacity: 0.7;
}
.wb-tool-arg {
  font-size: 10.5px;
  color: var(--wb-muted);
  font-family: ui-monospace, monospace;
  max-width: 28rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.wb-tool.err .wb-tool-hd {
  background: color-mix(in srgb, var(--wb-danger) 8%, transparent);
  border-radius: 4px;
}

.wb-block-skill {
  display: flex;
}
.wb-skill-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--wb-lavender) 14%, transparent);
  color: var(--wb-lavender);
  font-size: 11px;
}
.wb-art-card {
  display: inline-flex;
  align-items: center;
  padding: 4px 10px;
  border-radius: 6px;
  background: color-mix(in srgb, var(--wb-mint) 8%, transparent);
  color: var(--wb-mint);
  font-size: 11.5px;
}
</style>