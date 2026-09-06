import { describe, expect, it } from 'vitest'
import { mergeLoadedMessages, toUiMessages, resolveRunAssistant } from '../models/merge'
import type { Message } from '@/types/api'

function msg(over: Partial<Message> & { id: string }): Message {
  return { role: 'user', content: '', session_id: 'S1', status: 'completed', ...over } as Message
}

describe('mergeLoadedMessages', () => {
  it('本地乐观消息被同内容权威行替换，不产生重复', () => {
    const local = [msg({ id: 'local-1', role: 'user', content: '你好' })]
    const loaded = [
      msg({ id: 'M1', role: 'user', content: '你好' }),
      msg({ id: 'M2', role: 'assistant', content: '你好呀', status: 'completed' })
    ]
    const out = mergeLoadedMessages(local, loaded)
    expect(out.map((m) => m.id)).toEqual(['M1', 'M2'])
  })

  it('流式期间的空 assistant 占位被跳过，避免与流式气泡重影', () => {
    const local = [msg({ id: 'local-1', role: 'user', content: '在吗' })]
    const loaded = [
      msg({ id: 'M1', role: 'user', content: '在吗' }),
      msg({ id: 'M2', role: 'assistant', content: '', status: 'streaming' })
    ]
    const out = mergeLoadedMessages(local, loaded)
    expect(out.map((m) => m.id)).toEqual(['M1'])
  })

  it('已产出正文的 assistant 占位不再跳过（收尾后可见）', () => {
    const local: Message[] = []
    const loaded = [msg({ id: 'M2', role: 'assistant', content: '答案', status: 'completed' })]
    expect(mergeLoadedMessages(local, loaded).map((m) => m.id)).toEqual(['M2'])
  })

  it('权威快照返回重复内容时按 id 合并，不逐条堆叠', () => {
    const local = [msg({ id: 'local-1', role: 'user', content: '查一下' })]
    const loaded = [
      msg({ id: 'M1', role: 'user', content: '查一下' }),
      msg({ id: 'M2', role: 'assistant', content: '结果', status: 'completed' })
    ]
    const out = mergeLoadedMessages(local, loaded)
    expect(out.filter((m) => m.role === 'assistant')).toHaveLength(1)
    expect(out.filter((m) => m.role === 'user')).toHaveLength(1)
  })

  it('本地无消息时直接采用权威快照', () => {
    const loaded = [msg({ id: 'M1', role: 'user', content: 'a' })]
    expect(mergeLoadedMessages([], loaded)).toEqual(loaded)
  })

  it('权威已删除的本地行被丢弃', () => {
    const local = [
      msg({ id: 'M1', role: 'user', content: 'a' }),
      msg({ id: 'M9', role: 'user', content: '已删' })
    ]
    const loaded = [msg({ id: 'M1', role: 'user', content: 'a' })]
    expect(mergeLoadedMessages(local, loaded).map((m) => m.id)).toEqual(['M1'])
  })
})

describe('toUiMessages', () => {
  it('过滤 role=tool 行（LLM 上下文用，过程由 TaskTimeline 回放）', () => {
    const loaded = [
      msg({ id: 'M1', role: 'user', content: '看下工作区有啥' }),
      msg({ id: 'M2', role: 'assistant', content: '回复' }),
      msg({ id: 'M3', role: 'tool', content: 'AI工具应用.md\nwails开发手册.md' })
    ]
    expect(toUiMessages(loaded).map((m) => m.id)).toEqual(['M1', 'M2'])
  })
})

describe('resolveRunAssistant', () => {
  const REPLY = '工作区根目录下的内容……'

  it('工具场景：assistant 前置、tool 收尾时仍能判 dedupe（回归：只查末条会漏判导致整段重复）', () => {
    const messages = [
      msg({ id: 'M1', role: 'user', content: '看下工作区有啥' }),
      msg({ id: 'M2', role: 'assistant', content: REPLY }),
      msg({ id: 'M3', role: 'tool', content: 'AI工具应用.md' })
    ]
    expect(resolveRunAssistant(messages, REPLY)).toBe('dedupe')
  })

  it('本 run assistant 为空占位时返回 fill', () => {
    const messages = [
      msg({ id: 'M1', role: 'user', content: 'hi' }),
      msg({ id: 'M2', role: 'assistant', content: '', status: 'streaming' })
    ]
    const v = resolveRunAssistant(messages, '答案')
    expect(v).toEqual({ kind: 'fill', message: messages[1] })
  })

  it('权威快照无本 run assistant（后端未落库）时返回 missing', () => {
    const messages = [msg({ id: 'M1', role: 'user', content: 'hi' })]
    expect(resolveRunAssistant(messages, '答案')).toBe('missing')
  })

  it('末尾 assistant 内容与流式累积不同（断线丢增量）仍以权威为准，不 push', () => {
    const messages = [
      msg({ id: 'M1', role: 'user', content: 'hi' }),
      msg({ id: 'M2', role: 'assistant', content: '权威完整内容' })
    ]
    expect(resolveRunAssistant(messages, '流式缺失前缀的部分内容')).toBe('dedupe')
  })

  it('steer 注入：注入的 user 在回复之后，全文匹配全列表仍判 dedupe', () => {
    const messages = [
      msg({ id: 'M1', role: 'user', content: '问题' }),
      msg({ id: 'M2', role: 'assistant', content: REPLY }),
      msg({ id: 'M3', role: 'user', content: '中途插话' })
    ]
    expect(resolveRunAssistant(messages, REPLY)).toBe('dedupe')
  })
})
