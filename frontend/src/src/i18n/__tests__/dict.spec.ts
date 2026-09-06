import { describe, expect, it } from 'vitest'
import { zhCN } from '../dict-zh'
import { enUS } from '../dict-en'
import { t } from '..'

/** 全部前端源码文本（Vite raw glob：不引入 node 类型依赖）。 */
const sources = import.meta.glob('../../**/*.{vue,ts}', {
  query: '?raw',
  import: 'default',
  eager: true
}) as Record<string, string>

/** 源码中 t('key') 实际引用的 key（模板与 script 一并覆盖）。 */
function usedKeys(): string[] {
  const keys = new Set<string>()
  for (const [path, src] of Object.entries(sources)) {
    if (path.includes('/i18n/dict-') || path.includes('__tests__')) continue
    for (const m of src.matchAll(/\bt\(\s*'([^']+)'/g)) keys.add(m[1])
  }
  return [...keys].sort()
}

describe('i18n 字典完整性', () => {
  const keys = usedKeys()

  it('扫描到足够多的引用 key，避免 glob 失效导致测试空跑', () => {
    expect(keys.length).toBeGreaterThan(500)
  })

  it('引用的 key 在 zh-CN 字典中全部存在', () => {
    const missing = keys.filter((k) => !(k in zhCN))
    expect(missing, `zh-CN 缺失：${missing.join(', ')}`).toEqual([])
  })

  it('引用的 key 在 en-US 字典中全部存在', () => {
    const missing = keys.filter((k) => !(k in enUS))
    expect(missing, `en-US 缺失：${missing.join(', ')}`).toEqual([])
  })

  it('t() 不回退为 key 本身（回退即界面露出原始 key）', () => {
    const leaked = keys.filter((k) => t(k) === k)
    expect(leaked, `回退 key：${leaked.join(', ')}`).toEqual([])
  })
})

describe('t() 占位符插值契约', () => {
  it('未传参数时保留占位符原形（漏传即肉眼可见，避免回退空串掩盖 bug）', () => {
    expect(t('common.confirmDeleteNamed')).toBe('确认删除「{0}」？')
  })
})
