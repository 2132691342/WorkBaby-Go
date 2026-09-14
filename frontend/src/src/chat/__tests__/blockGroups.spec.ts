import { describe, expect, it } from 'vitest'
import { groupToolRuns } from '../models/blocks'

/** 造块：只需 kind + seq（分组只依赖这两者）。 */
const b = (kind: string, seq: number): { kind: string; seq: number } => ({ kind, seq })

describe('groupToolRuns', () => {
  it('连续工具块合并为一组，正文块切断分组', () => {
    const groups = groupToolRuns([
      b('text', 0),
      b('tool_call', 1),
      b('tool_result', 2),
      b('tool_call', 3),
      b('text', 4),
      b('tool_call', 5)
    ])
    // 正文 → 工具组(3) → 正文 → 工具组(1)
    expect(groups.map((g) => g.tools.length)).toEqual([0, 3, 0, 1])
    expect(groups[0].one?.kind).toBe('text')
    expect(groups[1].one).toBeNull()
    expect(groups[2].one?.kind).toBe('text')
  })

  it('孤立的 tool_result 也归入工具组（不因其缺少 call 而单独成组）', () => {
    const groups = groupToolRuns([b('tool_call', 0), b('tool_result', 1), b('tool_result', 2)])
    expect(groups).toHaveLength(1)
    expect(groups[0].tools.map((t) => t.seq)).toEqual([0, 1, 2])
  })

  it('分组不重排、不丢块（展开后仍是原始顺序）', () => {
    const input = [
      b('skill', 0),
      b('thinking', 1),
      b('tool_call', 2),
      b('tool_result', 3),
      b('text', 4),
      b('artifact', 5),
      b('genui', 6)
    ]
    const flat = groupToolRuns(input).flatMap((g) => (g.tools.length > 0 ? g.tools : [g.one!]))
    expect(flat.map((x) => x.seq)).toEqual(input.map((x) => x.seq))
    expect(flat.map((x) => x.kind)).toEqual(input.map((x) => x.kind))
  })

  it('空输入返回空数组（无块消息不渲染任何分组）', () => {
    expect(groupToolRuns([])).toEqual([])
  })
})
