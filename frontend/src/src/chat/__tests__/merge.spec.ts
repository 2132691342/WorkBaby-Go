import { describe, expect, it } from 'vitest'
import { mergeLoadedMessages, toUiMessages, resolveRunAssistant } from '../models/merge'
import type { Message } from '@/types/api'

function msg(over: Partial<Message> & { id: string }): Message {
  return { role: 'user', content: '', session_id: 'S1', status: 'completed', ...over } as Message
}

/**
 * 本地乐观消息与权威快照的对账规则。
 * 每条断言都对应一类真实事故：重影、重复堆叠、已删消息残留、断线丢增量。
 */
describe('mergeLoadedMessages', () => {
  it('按内容对账：本地乐观行被权威行替换，重复内容不堆叠', () => {
    // 本地乐观消息 + 权威同内容 → 只留权威行
    const replaced = mergeLoadedMessages(
      [msg({ id: 'local-1', role: 'user', content: '你好' })],
      [
        msg({ id: 'M1', role: 'user', content: '你好' }),
        msg({ id: 'M2', role: 'assistant', content: '你好呀' })
      ]
    )
    expect(replaced.map((m) => m.id)).toEqual(['M1', 'M2'])

    // 权威快照重复返回同内容 → 按 id 合并，不逐条堆叠
    const deduped = mergeLoadedMessages(
      [msg({ id: 'local-2', role: 'user', content: '查一下' })],
      [
        msg({ id: 'M1', role: 'user', content: '查一下' }),
        msg({ id: 'M2', role: 'assistant', content: '结果' })
      ]
    )
    expect(deduped.filter((m) => m.role === 'assistant')).toHaveLength(1)
    expect(deduped.filter((m) => m.role === 'user')).toHaveLength(1)
  })

  it('assistant 占位：空正文跳过（避免与流式气泡重影），已产出正文保留', () => {
    const skipped = mergeLoadedMessages(
      [msg({ id: 'local-1', role: 'user', content: '在吗' })],
      [
        msg({ id: 'M1', role: 'user', content: '在吗' }),
        msg({ id: 'M2', role: 'assistant', content: '', status: 'streaming' })
      ]
    )
    expect(skipped.map((m) => m.id)).toEqual(['M1'])

    const kept = mergeLoadedMessages([], [msg({ id: 'M3', role: 'assistant', content: '答案' })])
    expect(kept.map((m) => m.id)).toEqual(['M3'])
  })

  it('权威为准：无本地消息时直接采用快照；权威已删的本地行被丢弃', () => {
    const loaded = [msg({ id: 'M1', role: 'user', content: 'a' })]
    expect(mergeLoadedMessages([], loaded)).toEqual(loaded)

    const pruned = mergeLoadedMessages(
      [msg({ id: 'M1', role: 'user', content: 'a' }), msg({ id: 'M9', role: 'user', content: '已删' })],
      loaded
    )
    expect(pruned.map((m) => m.id)).toEqual(['M1'])
  })
})

describe('toUiMessages', () => {
  it('过滤 role=tool 行（仅供 LLM 上下文，过程由 TaskTimeline 回放）', () => {
    const loaded = [
      msg({ id: 'M1', role: 'user', content: '看下工作区有啥' }),
      msg({ id: 'M2', role: 'assistant', content: '回复' }),
      msg({ id: 'M3', role: 'tool', content: 'AI工具应用.md\nwails开发手册.md' })
    ]
    expect(toUiMessages(loaded).map((m) => m.id)).toEqual(['M1', 'M2'])
  })
})

/**
 * 流式收尾时本地累积正文如何与权威消息归位：
 * dedupe（权威已有）不 push、fill（权威空占位）原地填、missing（未落库）补一条。
 */
describe('resolveRunAssistant', () => {
  const REPLY = '工作区根目录下的内容……'

  it('dedupe：三种情境都不重复 push（tool 收尾 / 断线丢增量 / steer 插话）', () => {
    // tool 收尾：只查末条会漏判，导致整段回复重复
    expect(
      resolveRunAssistant(
        [
          msg({ id: 'M1', role: 'user', content: '看下工作区有啥' }),
          msg({ id: 'M2', role: 'assistant', content: REPLY }),
          msg({ id: 'M3', role: 'tool', content: 'AI工具应用.md' })
        ],
        REPLY
      )
    ).toBe('dedupe')

    // 断线丢增量：权威内容与流式累积不同，仍以权威为准
    expect(
      resolveRunAssistant(
        [
          msg({ id: 'M1', role: 'user', content: 'hi' }),
          msg({ id: 'M2', role: 'assistant', content: '权威完整内容' })
        ],
        '流式缺失前缀的部分内容'
      )
    ).toBe('dedupe')

    // steer 插话：注入的 user 排在回复之后，全列表匹配仍判 dedupe
    expect(
      resolveRunAssistant(
        [
          msg({ id: 'M1', role: 'user', content: '问题' }),
          msg({ id: 'M2', role: 'assistant', content: REPLY }),
          msg({ id: 'M3', role: 'user', content: '中途插话' })
        ],
        REPLY
      )
    ).toBe('dedupe')
  })

  it('fill：本 run assistant 为空占位时返回该消息原地填充', () => {
    const messages = [
      msg({ id: 'M1', role: 'user', content: 'hi' }),
      msg({ id: 'M2', role: 'assistant', content: '', status: 'streaming' })
    ]
    expect(resolveRunAssistant(messages, '答案')).toEqual({ kind: 'fill', message: messages[1] })
  })

  it('missing：权威快照里没有本 run 的 assistant（后端未落库）', () => {
    const messages = [msg({ id: 'M1', role: 'user', content: 'hi' })]
    expect(resolveRunAssistant(messages, '答案')).toBe('missing')
  })
})
