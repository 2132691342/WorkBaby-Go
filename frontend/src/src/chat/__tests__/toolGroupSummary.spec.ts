import { describe, expect, it } from 'vitest'
import { fmtTurnDuration, summarizeToolCalls, totalToolMs } from '@/chat/models/toolGroupSummary'

describe('summarizeToolCalls', () => {
  it('按动作聚合并保持首次出现顺序', () => {
    const parts = summarizeToolCalls([
      { name: 'file_read' },
      { name: 'exec' },
      { name: 'file_read' },
      { name: 'file_edit' },
      { name: 'file_grep' }
    ])
    expect(parts).toEqual([
      { action: 'read_files', count: 3 },
      { action: 'run_commands', count: 1 },
      { action: 'edit_files', count: 1 }
    ])
  })

  it('fail closed：缺失与未知工具归入 generic，不猜测合并', () => {
    const parts = summarizeToolCalls([{ name: 'mystery_tool' }, {}, { name: undefined } as never])
    expect(parts).toEqual([{ action: 'generic', count: 3 }])
  })

  it('空输入返回空 receipt', () => {
    expect(summarizeToolCalls([])).toEqual([])
  })
})

describe('totalToolMs / fmtTurnDuration', () => {
  it('已收尾求和 + running 实时补差', () => {
    const ms = totalToolMs(
      [
        { durationMs: 1000 },
        { state: 'running', startedAt: 10_000 },
        { state: 'success' } // 无耗时信息，忽略
      ],
      13_500
    )
    expect(ms).toBe(4500)
  })

  it('全无耗时信息返回 null', () => {
    expect(totalToolMs([{ state: 'success' }], 0)).toBeNull()
    expect(totalToolMs([], 0)).toBeNull()
  })

  it('人话时长：秒 / 分秒 / 时分', () => {
    expect(fmtTurnDuration(12_400)).toBe('12s')
    expect(fmtTurnDuration(200_000)).toBe('3m 20s')
    expect(fmtTurnDuration(3_900_000)).toBe('1h 5m')
    expect(fmtTurnDuration(null)).toBe('')
  })
})
