/**
 * 消息块纯函数模型层。
 *
 * 后端 message_blocks 表 → 前端消息分块渲染的唯一归一化出口：
 * 历史复现（message.blocks）与流式态（streaming 临时结构）共用同一套块形状，
 * MessageList 组件只做渲染，不解析 payload。
 */
import type { Message, MessageBlock, MessageBlockKind } from '../../types/api'
import type { ToolCallInfo } from '../../stores/chat/ChatStreamDecoder'

/** 归一化后的块形状（组件渲染直接消费）。 */
export interface ResolvedBlock {
  kind: MessageBlockKind
  seq: number
  /** tool_call / tool_result / artifact / genui 的 JSON 解析结果；thinking 为纯文本。 */
  data: Record<string, unknown> | null
  /** thinking 块的文本内容。 */
  text: string
}

/** 工具调用块 payload（chat:tool / BlockToolCall）。 */
export interface ToolCallBlockData {
  id: string
  name: string
  arguments?: string
}

/** 工具结果块 payload（chat:tool-result / BlockToolResult）。 */
export interface ToolResultBlockData {
  tool_call_id: string
  name: string
  content?: string
  error?: string
  duration_ms?: number
  /** 审批拒绝（Refused 语义）：非故障，前端展示「已拒绝」而非错误态。 */
  refused?: boolean
}

/** 安全解析块 payload JSON；失败返回 null（脏数据不拖垮整条消息渲染）。 */
function parsePayload(block: MessageBlock): Record<string, unknown> | null {
  try {
    const v = JSON.parse(block.payload)
    return v && typeof v === 'object' ? (v as Record<string, unknown>) : null
  } catch {
    return null
  }
}

/** 单块归一化。 */
function resolveBlock(block: MessageBlock): ResolvedBlock {
  if (block.kind === 'thinking') {
    return { kind: block.kind, seq: block.seq, data: null, text: block.payload }
  }
  return { kind: block.kind, seq: block.seq, data: parsePayload(block), text: '' }
}

/**
 * 消息 → 渲染块序列（按 seq 升序；空/脏块过滤）。
 *
 * 优先取持久化 blocks；无块且消息带 tool_calls_json 时降级从 tool_calls_json 重建
 * tool_call 块（兼容 P0-M1 之前的历史消息，至少能看到调用过什么）。
 */
export function resolveMessageBlocks(msg: Message): ResolvedBlock[] {
  if (msg.blocks && msg.blocks.length > 0) {
    return msg.blocks
      .slice()
      .sort((a, b) => a.seq - b.seq)
      .map(resolveBlock)
  }
  // 历史消息降级：assistant.tool_calls_json → tool_call 块（无结果信息）
  if (msg.role === 'assistant' && msg.tool_calls_json) {
    try {
      const calls = JSON.parse(msg.tool_calls_json) as Array<{
        id?: string
        function?: { name?: string; arguments?: string }
      }>
      if (Array.isArray(calls)) {
        return calls.map((c, i) => ({
          kind: 'tool_call' as MessageBlockKind,
          seq: i,
          data: {
            id: c.id ?? String(i),
            name: c.function?.name ?? 'unknown',
            arguments: c.function?.arguments ?? ''
          },
          text: ''
        }))
      }
    } catch {
      /* 脏 JSON 忽略 */
    }
  }
  return []
}

/** 块是否为「带错误结果」的 tool_result（refused 不算错误）。 */
export function isFailedResultBlock(block: ResolvedBlock): boolean {
  if (block.kind !== 'tool_result') return false
  const d = block.data as Partial<ToolResultBlockData> | null
  return Boolean(d?.error)
}

/** 块是否为审批拒绝结果。 */
export function isRefusedResultBlock(block: ResolvedBlock): boolean {
  if (block.kind !== 'tool_result') return false
  const d = block.data as Partial<ToolResultBlockData> | null
  return Boolean(d?.refused)
}

/** 终止原因 → 展示级别（与后端 domain.StopReasonSeverity 契约一致）。 */
export type StopSeverity = 'info' | 'warning' | 'error'

const WARNING_REASONS = new Set([
  'cancelled',
  'max_turns',
  'tool_error_limit',
  'token_budget',
  'stagnation',
  'interrupted'
])

export function stopReasonSeverity(reason: string | null | undefined): StopSeverity {
  if (!reason) return 'info'
  if (reason === 'completed') return 'info'
  if (WARNING_REASONS.has(reason)) return 'warning'
  return 'error'
}

/** 终止原因 → 用户可读文案（i18n key 前缀 wb-stop-*，MessageList 直接消费）。 */
export function stopReasonText(reason: string | null | undefined): string {
  switch (reason) {
    case 'completed':
      return '已完成'
    case 'cancelled':
      return '已停止'
    case 'max_turns':
      return '达到轮次上限，可继续'
    case 'tool_error_limit':
      return '工具连续失败，已中止'
    case 'token_budget':
      return 'Token 预算耗尽'
    case 'stagnation':
      return '检测到重复调用，已熔断'
    case 'interrupted':
      return '上次运行被中断，可一键续跑'
    case 'error':
      return '运行出错'
    default:
      return ''
  }
}

// ===== diff 识别与解析=====

/** diff 行类型。 */
export interface DiffLine {
  type: 'add' | 'del' | 'hunk' | 'ctx'
  text: string
}

/**
 * 结果内容是否为 unified diff。
 * 信号（满足其一）：`diff --git` 头、`@@ -x +y @@` hunk 头、`+++ /---` 文件头对。
 * 启发式按 rune 前缀判定，误判面（markdown `---` 分割线 + `+++` 极少共存）可接受。
 */
export function looksLikeDiff(content: string): boolean {
  const s = content.slice(0, 4000)
  if (s.includes('diff --git ')) return true
  if (/^@@ -\d+(,\d+)? \+\d+(,\d+)? @@/m.test(s)) return true
  const plus = (s.match(/^\+\+\+ /gm) ?? []).length
  const minus = (s.match(/^--- /gm) ?? []).length
  return plus > 0 && minus > 0
}

/** unified diff → 着色行序列（`+++ /--- ` 开头是文件头不算增删行）。 */
export function parseDiffLines(content: string): DiffLine[] {
  const lines = content.split('\n')
  if (lines.length > 0 && lines[lines.length - 1] === '') lines.pop()
  return lines.map((text) => {
    if (text.startsWith('@@')) return { type: 'hunk' as const, text }
    if (text.startsWith('+++ ') || text.startsWith('--- ')) return { type: 'ctx' as const, text }
    if (text.startsWith('+')) return { type: 'add' as const, text }
    if (text.startsWith('-')) return { type: 'del' as const, text }
    return { type: 'ctx' as const, text }
  })
}

/** diff 行 → 配色 class（wb token；组件直接绑定）。 */
export function diffLineClass(type: DiffLine['type']): string {
  switch (type) {
    case 'add':
      return 'bg-wb-mint/10 text-wb-mint'
    case 'del':
      return 'bg-wb-danger/10 text-wb-danger'
    case 'hunk':
      return 'text-wb-primary-strong'
    default:
      return 'text-wb-muted'
  }
}

/**
 * 流式累积的工具状态 → 持久化块形状（流式终态兜底）。
 *
 * 后端 chat:done 先于前端 reload 到达时，拉到的权威消息可能还没写完；
 * 此时用本地 streamingTools 还原块序列，避免「回复结束后工具过程消失」。
 */
export function toolsToBlocks(tools: ToolCallInfo[], messageId: string): MessageBlock[] {
  if (tools.length === 0) return []
  const now = Date.now()
  const blocks: MessageBlock[] = []
  let seq = 0
  for (const t of tools) {
    blocks.push({
      id: `${messageId}-local-call-${seq}`,
      message_id: messageId,
      seq: seq++,
      kind: 'tool_call',
      payload: JSON.stringify({ id: t.id, name: t.name, arguments: t.args ?? '' }),
      created_at: now
    })
    if (t.result !== undefined) {
      const refused = t.result.startsWith('已拒绝')
      const isError = t.state === 'error'
      const content = isError ? t.result.replace(/^error: [^\n]*\n?/, '') : t.result
      blocks.push({
        id: `${messageId}-local-result-${seq}`,
        message_id: messageId,
        seq: seq++,
        kind: 'tool_result',
        payload: JSON.stringify({
          tool_call_id: t.id,
          name: t.name,
          content,
          error: isError && !refused ? t.result.replace(/^error: /, '').split('\n')[0] : '',
          refused
        }),
        created_at: now
      })
    }
  }
  return blocks
}

/** 块序列 → ToolCallInfo[]（配对 tool_call/tool_result，复用 TaskTimeline 渲染历史过程）。 */
export function blocksToToolCalls(blocks: ResolvedBlock[]): ToolCallInfo[] {
  const calls = new Map<string, ToolCallInfo>()
  for (const b of blocks) {
    if (b.kind === 'tool_call' && b.data) {
      const d = b.data as Partial<ToolCallBlockData>
      calls.set(d.id ?? '', {
        id: d.id ?? '',
        name: d.name ?? 'unknown',
        args: d.arguments,
        state: 'running'
      })
    } else if (b.kind === 'tool_result' && b.data) {
      const d = b.data as Partial<ToolResultBlockData>
      const text = d.error ? `error: ${d.error}\n${d.content ?? ''}` : (d.content ?? '')
      const hit = calls.get(d.tool_call_id ?? '')
      if (hit) {
        hit.result = d.refused ? '已拒绝：用户未批准该工具调用' : text
        hit.state = d.error && !d.refused ? 'error' : 'success'
        hit.duration_ms = d.duration_ms
      } else {
        calls.set(d.tool_call_id ?? '', {
          id: d.tool_call_id ?? '',
          name: d.name ?? 'unknown',
          result: d.refused ? '已拒绝：用户未批准该工具调用' : text,
          state: d.error && !d.refused ? 'error' : 'success',
          duration_ms: d.duration_ms
        })
      }
    }
  }
  return [...calls.values()]
}
