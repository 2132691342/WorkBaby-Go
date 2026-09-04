import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ChatStreamEvent, Message, MessageBlock } from '@/types/api'
import { mapSSEEvent } from '@/api/stream'
import { applyStreamUpdate, decodeStreamEvent } from '@/stores/chat/ChatStreamDecoder'
import type { ApprovalRequest, StreamEventUpdate, ToolCallInfo } from '@/stores/chat/ChatStreamDecoder'
import { StreamEventBatcher } from '@/stores/chat/StreamEventBatcher'
import { blocksToToolCalls, resolveMessageBlocks, stopReasonSeverity, stopReasonText } from '@/chat/models/blocks'

/**
 * 流式管线端到端契约（SSE → 解码 → 批渲染 → 历史块还原 → 终因横幅）。
 * 分四段：事件映射、解码副作用、批渲染时序、历史消息还原与终态展示。
 */

// ===== 1. SSE 事件映射 =====
describe('SSE → 前端事件', () => {
  it('增量/思考/工具调用映射', () => {
    expect(mapSSEEvent('chat:stream', { delta: 'hi' })).toEqual({ type: 'content', data: 'hi' })
    expect(mapSSEEvent('chat:thinking', { delta: 'reasoning' })).toEqual({ type: 'thinking', data: 'reasoning' })
    expect(mapSSEEvent('chat:tool', { id: 't1', name: 'exec', arguments: '{}' })).toEqual({
      type: 'tool_call',
      data: { id: 't1', name: 'exec', args: '{}', agent: '' }
    })
  })

  it('工具结果成功/失败区分 state', () => {
    expect(mapSSEEvent('chat:tool-result', { id: 't1', name: 'file_read', content: 'ok', error: '' })).toEqual({
      type: 'tool_result',
      data: { id: 't1', name: 'file_read', output: 'ok', state: 'success', agent: '' }
    })
    expect(mapSSEEvent('chat:tool-result', { id: 't1', name: 'exec', content: '', error: 'boom' })).toEqual({
      type: 'tool_result',
      data: { id: 't1', name: 'exec', output: '', state: 'error', agent: '' }
    })
  })

  it('终因归一化：原始 finish_reason 归一为领域原因，未知原因不惊扰', () => {
    // LLM 原始 finish_reason（stop / end_turn / length）不得直通前端，否则横幅文案不命中
    expect(mapSSEEvent('chat:done', { status: 'completed', reason: 'end_turn', stop_reason: 'end_turn' }))
      .toEqual({ type: 'stopped', data: { reason: 'completed' } })
    expect(mapSSEEvent('chat:done', { stop_reason: 'length' })).toEqual({
      type: 'stopped',
      data: { reason: 'token_budget' }
    })
    // 领域原因（用户中断）必须保留
    expect(mapSSEEvent('chat:done', { reason: 'cancelled' })).toEqual({
      type: 'stopped',
      data: { reason: 'cancelled' }
    })
    expect(mapSSEEvent('chat:gap', { run_id: 'r1', last_seq: 3 }))
      .toEqual({ type: 'gap', data: { run_id: 'r1', last_seq: 3 } })
    expect(mapSSEEvent('chat:unknown', {})).toBeNull()
    expect(mapSSEEvent('workflow:started', {})).toBeNull()
  })
})

// ===== 2. 解码器：事件 → 状态更新 =====
describe('解码器', () => {
  it('审批请求/决策映射', () => {
    expect(
      decodeStreamEvent({
        type: 'tool_approval_request',
        data: { id: 'APR_1', command: 'go test', reason: '需要你确认', risk: 'irreversible' }
      })
    ).toEqual({ setApproval: { id: 'APR_1', command: 'go test', reason: '需要你确认', risk: 'irreversible' } })

    expect(decodeStreamEvent({ type: 'approval_decided', data: { id: 'APR_1', decision: 'approved' } }))
      .toEqual({ clearApproval: { id: 'APR_1', decision: 'approved' } })
    expect(decodeStreamEvent({ type: 'approval_decided', data: null })).toBeNull()
  })

  it('终因映射（含未定义终因兜底）', () => {
    expect(decodeStreamEvent({ type: 'stopped', data: { reason: 'cancelled' } })).toEqual({ setStopReason: 'cancelled' })
  })

  /** 最小 ref state（满足 applyStreamUpdate 签名）。 */
  function makeState(pending: ApprovalRequest | null = null) {
    return {
      streamingContent: { value: '' },
      streamingThinking: { value: '' },
      streamingTools: { value: [] as ToolCallInfo[] },
      streamingStats: { value: null },
      streamingArtifacts: { value: null },
      streamingGenUi: { value: null },
      pendingApproval: { value: pending },
      error: { value: null as string | null },
      stopReason: { value: null as string | null }
    }
  }
  const sample = (id: string): ApprovalRequest => ({ id, command: 'go test', reason: '需确认' })

  it('clearApproval 仅 id 匹配才清（防误清并发新请求）', () => {
    const s1 = makeState(sample('APR_1'))
    applyStreamUpdate({ clearApproval: { id: 'APR_1', decision: 'approved' } }, s1)
    expect(s1.pendingApproval.value).toBeNull()

    const s2 = makeState(sample('APR_2'))
    applyStreamUpdate({ clearApproval: { id: 'APR_1', decision: 'approved' } }, s2)
    expect(s2.pendingApproval.value).toEqual(sample('APR_2'))

    const s3 = makeState(null)
    applyStreamUpdate({ clearApproval: { id: 'APR_1', decision: 'denied' } }, s3)
    expect(s3.pendingApproval.value).toBeNull()
  })

  it('setApproval → clearApproval 全链路最终回到 null', () => {
    const state = makeState(null)
    applyStreamUpdate({ setApproval: sample('APR_1') }, state)
    expect(state.pendingApproval.value).toEqual(sample('APR_1'))
    applyStreamUpdate({ clearApproval: { id: 'APR_1', decision: 'denied' } }, state)
    expect(state.pendingApproval.value).toBeNull()
  })

  it('子 Agent 生命周期：start 建 running 任务，done 合并终态', () => {
    const start = decodeStreamEvent({ type: 'subagent_start', data: { sub_run_id: 'RUN_c', agent: 'coding' } })
    expect(start?.upsertTask?.state).toBe('running')
    expect(start?.upsertTask?.agent).toBe('coding')

    const state = { ...makeState(), tasks: { value: [] as import('@/types/api').BackgroundTask[] } }
    applyStreamUpdate(start!, state)
    const done = decodeStreamEvent({ type: 'subagent_done', data: { sub_run_id: 'RUN_c', agent: 'coding' } })
    applyStreamUpdate(done!, state)
    expect(state.tasks.value).toHaveLength(1)
    expect(state.tasks.value[0].state).toBe('completed')
    expect(state.tasks.value[0].finished_at).toBeGreaterThan(0)

    const err = decodeStreamEvent({ type: 'subagent_error', data: { sub_run_id: 'RUN_x', agent: 'research', message: 'boom' } })
    expect(err?.updateTask?.state).toBe('failed')
    applyStreamUpdate(err!, state)
    expect(state.tasks.value).toHaveLength(1) // 未 start 过的 id 不产生悬空任务
  })
})

// ===== 3. 批渲染：一帧一渲染，内容不丢不重 =====
describe('批渲染', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  function recorder(): { calls: StreamEventUpdate[][]; apply: (u: StreamEventUpdate[]) => void } {
    const calls: StreamEventUpdate[][] = []
    return { calls, apply: (u) => calls.push(u) }
  }
  const content = (s: string): ChatStreamEvent => ({ type: 'content', data: s })

  it('一帧内多条增量只触发一次 apply 且拼接完整', () => {
    const { calls, apply } = recorder()
    const b = new StreamEventBatcher(apply)
    b.push(content('你'))
    b.push(content('好'))
    b.push(content('呀'))
    expect(calls).toHaveLength(0)

    vi.advanceTimersByTime(50)
    expect(calls).toHaveLength(1)
    expect(calls[0].map((u) => u.appendContent).join('')).toBe('你好呀')
  })

  it('终态与审批事件不被延迟，且排在积压增量之后', () => {
    const { calls, apply } = recorder()
    const b = new StreamEventBatcher(apply)
    b.push(content('已生成内容'))
    b.push({ type: 'stopped', data: { reason: 'cancelled' } })
    expect(calls).toHaveLength(2)
    expect(calls[0][0].appendContent).toBe('已生成内容')
    expect(calls[1][0].setStopReason).toBe('cancelled')

    const r2 = recorder()
    const b2 = new StreamEventBatcher(r2.apply)
    b2.push({ type: 'tool_approval_request', data: { id: 'a1', command: 'rm -rf build', reason: 'danger', risk: 'irreversible' } })
    expect(r2.calls[0][0].setApproval?.risk).toBe('irreversible')
  })

  it('flush 幂等；dispose 丢弃未落定增量', () => {
    const { calls, apply } = recorder()
    const b = new StreamEventBatcher(apply)
    b.push(content('尾巴'))
    b.flush()
    b.flush()
    vi.advanceTimersByTime(50)
    expect(calls).toHaveLength(1)

    const r2 = recorder()
    const b2 = new StreamEventBatcher(r2.apply)
    b2.push(content('会被丢弃'))
    b2.dispose()
    vi.advanceTimersByTime(50)
    expect(r2.calls).toHaveLength(0)
  })

  it('长流多帧按序不丢', () => {
    const { calls, apply } = recorder()
    const b = new StreamEventBatcher(apply)
    for (let i = 0; i < 10; i++) {
      b.push(content(String(i)))
      vi.advanceTimersByTime(50)
    }
    b.flush()
    expect(calls).toHaveLength(10)
    expect(calls.flatMap((c) => c.map((u) => u.appendContent)).join('')).toBe('0123456789')
  })
})

// ===== 4. 历史消息还原与终态展示 =====
describe('历史消息还原', () => {
  function block(seq: number, kind: MessageBlock['kind'], payload: unknown): MessageBlock {
    return {
      id: `BLOCK_${seq}`,
      message_id: 'MESSAGE_1',
      seq,
      kind,
      payload: typeof payload === 'string' ? payload : JSON.stringify(payload),
      created_at: 0
    }
  }
  function msg(fields: Partial<Message>): Message {
    return {
      id: 'MESSAGE_1', session_id: 'SESSION_1', seq: 2, run_id: 'RUN_1', role: 'assistant',
      content: 'done', tool_call_id: null, tool_calls_json: null, status: 'completed',
      stop_reason: null, model: null, created_at: 0, updated_at: 0,
      ...fields
    } as Message
  }

  it('块按 seq 升序还原，脏 payload 不抛错', () => {
    const m = msg({
      blocks: [
        block(2, 'tool_result', { tool_call_id: 't1', name: 'exec', content: 'ok' }),
        block(1, 'tool_call', { id: 't1', name: 'exec', arguments: '{"cmd":"ls"}' })
      ]
    })
    const out = resolveMessageBlocks(m)
    expect(out.map((b) => b.seq)).toEqual([1, 2])

    const broken = msg({ blocks: [block(1, 'tool_call', '{broken json')] })
    expect(resolveMessageBlocks(broken)[0].data).toBeNull()
  })

  it('无 blocks 时从 tool_calls_json 降级重建（旧历史消息）', () => {
    const m = msg({
      tool_calls_json: JSON.stringify([
        { id: 't9', type: 'function', function: { name: 'file_read', arguments: '{"path":"a.txt"}' } }
      ])
    })
    const out = resolveMessageBlocks(m)
    expect(out[0].kind).toBe('tool_call')
    expect(out[0].data).toMatchObject({ id: 't9', name: 'file_read' })
  })

  it('call/result 配对：成功、失败、拒绝（拒绝≠故障）、孤儿', () => {
    const paired = blocksToToolCalls([
      { kind: 'tool_call', seq: 1, data: { id: 't1', name: 'exec', arguments: '{"cmd":"ls"}' }, text: '' },
      { kind: 'tool_result', seq: 2, data: { tool_call_id: 't1', name: 'exec', content: 'ok', duration_ms: 12 }, text: '' }
    ])
    expect(paired).toEqual([
      { id: 't1', name: 'exec', args: '{"cmd":"ls"}', result: 'ok', state: 'success', duration_ms: 12 }
    ])

    const failed = blocksToToolCalls([
      { kind: 'tool_call', seq: 1, data: { id: 't1', name: 'exec' }, text: '' },
      { kind: 'tool_result', seq: 2, data: { tool_call_id: 't1', name: 'exec', error: 'boom', content: '' }, text: '' },
      { kind: 'tool_call', seq: 3, data: { id: 't2', name: 'file_write' }, text: '' },
      { kind: 'tool_result', seq: 4, data: { tool_call_id: 't2', name: 'file_write', refused: true, content: '{"refused":true}' }, text: '' }
    ])
    expect(failed[0].state).toBe('error') // 工具报错 → error
    expect(failed[1].state).toBe('success') // 审批拒绝是「被拒」而非「故障」，成功态+文案标注
    expect(failed[1].result).toContain('已拒绝')

    const orphan = blocksToToolCalls([
      { kind: 'tool_result', seq: 1, data: { tool_call_id: 'tx', name: 'exec', content: 'hi' }, text: '' }
    ])
    expect(orphan[0]).toMatchObject({ id: 'tx', name: 'exec', state: 'success' })
  })

  it('终因分级与文案（防横幅空白）', () => {
    expect(stopReasonSeverity('completed')).toBe('info')
    expect(stopReasonSeverity(null)).toBe('info')
    expect(stopReasonSeverity('cancelled')).toBe('warning')
    expect(stopReasonSeverity('max_turns')).toBe('warning')
    expect(stopReasonSeverity('tool_error_limit')).toBe('warning')
    expect(stopReasonSeverity('token_budget')).toBe('warning')
    expect(stopReasonSeverity('stagnation')).toBe('warning')
    expect(stopReasonSeverity('error')).toBe('error')
    expect(stopReasonSeverity('unknown_reason')).toBe('error')

    for (const r of ['completed', 'cancelled', 'max_turns', 'tool_error_limit', 'token_budget', 'stagnation', 'error']) {
      expect(stopReasonText(r)).not.toBe('')
    }
  })
})
