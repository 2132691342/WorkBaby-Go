import { describe, expect, it } from 'vitest'
import { countDiffLines, traceDelegateAgent, traceKind, traceTarget } from '@/chat/models/toolTrace'
import { parseDiffRows } from '@/chat/models/blocks'

describe('toolTrace', () => {
  it('maps tools to narrative kinds', () => {
    expect(traceKind('file_read')).toBe('read')
    expect(traceKind('file_edit')).toBe('edit')
    expect(traceKind('file_write')).toBe('write')
    expect(traceKind('exec')).toBe('exec')
    expect(traceKind('delegate_task')).toBe('delegate')
    expect(traceKind('unknown_tool')).toBe('other')
    expect(traceKind(undefined)).toBe('other')
  })

  it('splits file paths into name + dir', () => {
    const t = traceTarget(JSON.stringify({ path: 'internal/bootstrap/app.go' }))
    expect(t).toEqual({ main: 'app.go', sub: 'internal/bootstrap/' })
    const win = traceTarget(JSON.stringify({ path: 'C:\\Users\\LIKX\\skills\\qa_pptx.py' }))
    expect(win).toEqual({ main: 'qa_pptx.py', sub: 'C:\\Users\\LIKX\\skills\\' })
  })

  it('extracts command and query targets one-line', () => {
    const cmd = traceTarget(JSON.stringify({ command: 'go build ./...\nwith newline' }))
    expect(cmd?.main).toBe('go build ./... with newline')
    const q = traceTarget(JSON.stringify({ query: 'vue test' }))
    expect(q?.main).toBe('vue test')
  })

  it('returns null for empty args and falls back on bad json', () => {
    expect(traceTarget(undefined)).toBeNull()
    expect(traceTarget('')).toBeNull()
    expect(traceTarget('not json')?.main).toContain('not json')
  })

  it('extracts delegate agent with default fallback', () => {
    expect(traceDelegateAgent(JSON.stringify({ agent: 'code-reviewer', task: 'x' }))).toBe('code-reviewer')
    expect(traceDelegateAgent(JSON.stringify({ task: 'x' }))).toBe('default')
    expect(traceDelegateAgent('bad json')).toBe('default')
  })

  it('counts diff added/removed lines ignoring headers and hunks', () => {
    const diff = ['diff --git a/x b/x', '--- a/x', '+++ b/x', '@@ -1,2 +1,2 @@', 'old', '-gone', '+new', '+new2', 'ctx'].join('\n')
    expect(countDiffLines(diff)).toEqual({ added: 2, removed: 1 })
    expect(countDiffLines(undefined)).toEqual({ added: 0, removed: 0 })
  })
})

describe('parseDiffRows', () => {
  it('tracks old/new line numbers from hunk headers', () => {
    const diff = ['@@ -3,3 +3,4 @@', 'ctx', '-del', '+add', '+add2', 'ctx2'].join('\n')
    const rows = parseDiffRows(diff)
    expect(rows[0]).toMatchObject({ type: 'hunk', oldNo: null, newNo: null })
    expect(rows[1]).toMatchObject({ type: 'ctx', oldNo: 3, newNo: 3 })
    expect(rows[2]).toMatchObject({ type: 'del', oldNo: 4, newNo: null })
    expect(rows[3]).toMatchObject({ type: 'add', oldNo: null, newNo: 4 })
    expect(rows[4]).toMatchObject({ type: 'add', oldNo: null, newNo: 5 })
    expect(rows[5]).toMatchObject({ type: 'ctx', oldNo: 5, newNo: 6 })
  })

  it('falls back to new-side sequence for hunk-less diffs', () => {
    const rows = parseDiffRows(['+a', '-b', 'c'].join('\n'))
    expect(rows[0]).toMatchObject({ type: 'add', oldNo: null, newNo: 1 })
    expect(rows[1]).toMatchObject({ type: 'del', oldNo: null, newNo: null })
    expect(rows[2]).toMatchObject({ type: 'ctx', oldNo: null, newNo: 2 })
  })
})
