/**
 * 时间格式化：全站日期展示的唯一入口，不引 dayjs（只需 ISO 串 → 可读串）。
 * `formatDateTime` 默认展示 / `formatDate` 仅日期 / `formatRelativeTime` 密集列表场景；
 * null / 空串 / 非法串统一返回 `—`，调用方无需判空。
 */
import { t } from '@/i18n'

/** 空值与非法值的统一占位符。 */
export const TIME_EMPTY = '—'

/**
 * 入参同时接受 ISO 串与毫秒时间戳（后端按 CLAUDE.md §2.8 统一返回毫秒整数）。
 */
function toDate(v: string | number | null | undefined): Date | null {
  if (v == null || v === '') return null
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? null : d
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n)
}

/** `14:05`（秒级精度，不带毫秒与时区后缀）。 */
export function formatDateTime(iso: string | number | null | undefined): string {
  const d = toDate(iso)
  if (!d) return TIME_EMPTY
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} `
    + `${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

/** ``。 */
export function formatDate(iso: string | number | null | undefined): string {
  const d = toDate(iso)
  if (!d) return TIME_EMPTY
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
}

/**
 * 会话列表等密集场景：
 * 今天 → `14:05`；昨天 → `昨天 14:05`；今年 → `8/29 14:05`；跨年 → `14:05`。
 */
export function formatRelativeTime(iso: string | number | null | undefined): string {
  const d = toDate(iso)
  if (!d) return TIME_EMPTY
  const hm = `${pad2(d.getHours())}:${pad2(d.getMinutes())}`
  const now = new Date()
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  if (d.getTime() >= startOfToday) return hm
  if (d.getTime() >= startOfToday - 86400000) return `${t('common.yesterday')} ${hm}`
  if (d.getFullYear() === now.getFullYear()) return `${d.getMonth() + 1}/${d.getDate()} ${hm}`
  return formatDateTime(iso)
}
