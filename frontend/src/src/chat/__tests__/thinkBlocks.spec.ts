import { describe, expect, it } from 'vitest'
import { stripThinkBlocks } from '../models/blocks'

describe('stripThinkBlocks', () => {
  it('移除闭合 think 块，保留正文', () => {
    expect(stripThinkBlocks('<think>推理过程</think>\n\n答案')).toBe('答案')
  })

  it('移除多个 think 块（跨行）', () => {
    const raw = '<think>先想</think>开头<think>\n多行\n推理\n</think>结尾'
    expect(stripThinkBlocks(raw)).toBe('开头结尾')
  })

  it('流式未闭合的尾部 think 前缀一并移除', () => {
    expect(stripThinkBlocks('正文前半<think>还没说完')).toBe('正文前半')
  })

  it('无 think 块原样返回（含普通 <> 文本）', () => {
    const raw = '普通 <b>正文</b> 保持不变'
    expect(stripThinkBlocks(raw)).toBe(raw)
  })

  it('纯 think 内容剥离后为空串', () => {
    expect(stripThinkBlocks('<think>只有推理</think>')).toBe('')
  })
})
