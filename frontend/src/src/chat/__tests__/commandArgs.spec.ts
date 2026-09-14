import { describe, expect, it } from 'vitest'
import { expandCommandPrompt, expandSlashDraft, tokenizeArgs } from '../commandArgs'

describe('commandArgs', () => {
  it('切分参数时保留引号内的空格', () => {
    expect(tokenizeArgs('src/app.ts "my module"')).toEqual(['src/app.ts', 'my module'])
    expect(tokenizeArgs("'a b' c")).toEqual(['a b', 'c'])
    expect(tokenizeArgs('   ')).toEqual([])
  })

  it('展开 $ARGUMENTS 与位置参数', () => {
    expect(expandCommandPrompt('审查 $1，范围 $2', 'auth.ts "src/core"')).toBe('审查 auth.ts，范围 src/core')
    expect(expandCommandPrompt('全部参数：$ARGUMENTS', 'a b c')).toBe('全部参数：a b c')
    // 未提供参数：占位符注入空串，不把字面量 $ARGUMENTS 发给模型
    expect(expandCommandPrompt('审查 $1 / $ARGUMENTS', '')).toBe('审查  / ')
  })

  it('参数正文里的占位符不被二次展开', () => {
    // 先替换位置参数再替换 $ARGUMENTS，否则参数里的 $1 会被当成占位符吃掉
    expect(expandCommandPrompt('$ARGUMENTS', 'keep $1 literally')).toBe('keep $1 literally')
  })

  it('只对带模板的自定义命令做草稿展开', () => {
    const commands = [
      { name: 'review', prompt: '审查 $ARGUMENTS' },
      { name: 'compact' },
      { name: 'noop', prompt: '' },
    ]
    expect(expandSlashDraft('/review 登录模块', commands)).toBe('审查 登录模块')
    // 无模板的内置命令与普通文本原样通过
    expect(expandSlashDraft('/compact', commands)).toBe('/compact')
    expect(expandSlashDraft('/noop x', commands)).toBe('/noop x')
    expect(expandSlashDraft('看看这个文件', commands)).toBe('看看这个文件')
    expect(expandSlashDraft('/unknown x', commands)).toBe('/unknown x')
  })
})
