import { computed, ref } from 'vue'

/**
 * WorkBaby 前端 i18n。
 *
 * <p>采用极简自实现方案，避免引入 vue-i18n 依赖：
 * <ul>
 *   <li>内置 {@code messages} 完整 zh-CN / en-US 字典作为<b>权威来源</b>，
 *       切换语言即时生效、离线可用（不依赖后端往返与 cookie）</li>
 *   <li>{@link t} 支持 {@code {0}/{1}} 占位符插值（与 Spring MessageSource 一致）</li>
 * </ul>
 *
 * <p><b>为什么不再只依赖后端</b>：历史版本切换英文仅少数按钮生效，根因是
 * 后端字典缺 key + 各组件硬编码中文。现在前端内置全量字典，{@link setLocale}
 * 只改本地 {@code locale}，全站响应式刷新。
 *
 * <p>已移除对已下线后端端点 `/api/v1/i18n/messages` / `/api/v1/i18n/locale`
 * 的调用（I18nController 已删，调用只会 404 被静默吞掉）。
 */
import { zhCN } from './dict-zh'
import { enUS } from './dict-en'
import type { Dict } from './types'

/** 语言选择的本地持久化键。 */
const STORAGE_KEY = 'wb.locale'

/** 内置字典表（按 locale tag 索引）。 */
const messages: Record<string, Dict> = {
  'zh-CN': zhCN,
  'en-US': enUS
}

/** 恢复上次选择的语言；无记录或不可用时回落 zh-CN。 */
function restoreLocale(): string {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved && messages[saved]) return saved
  } catch {
    // 无 localStorage（隐私模式 / 非安全上下文）时用默认语言
  }
  return 'zh-CN'
}

/** 当前语言 tag（zh-CN / en-US）。 */
const locale = ref<string>(restoreLocale())

/** 当前 locale 的权威字典（内置；无后端覆盖）。 */
const dict = computed<Dict>(() => messages[locale.value] ?? zhCN)

/** 保留 init 为幂等空操作：历史调用方（App.vue onMounted）无需改动。 */
export async function init(): Promise<void> {
  // 已无后端 i18n 端点，本地字典即权威来源。
}

/** 切换语言：本地即时生效（内置字典）并持久化，重启后保持。 */
export function setLocale(lang: string): void {
  if (!messages[lang]) return
  locale.value = lang
  try {
    localStorage.setItem(STORAGE_KEY, lang)
  } catch {
    // 持久化失败不影响本次会话生效
  }
}

/** 响应式当前 locale tag。 */
export const currentLocale = computed(() => locale.value)

/**
 * 取翻译；找不到 key 时回退 {@code key} 本身（开发期便于排查）。
 * 占位符：{@code {0}} / {@code {1}} …
 */
export function t(key: string, ...args: unknown[]): string {
  const v = dict.value[key] ?? zhCN[key] ?? key
  if (args.length === 0) return v
  return v.replace(/\{(\d+)\}/g, (_m, idx) => {
    const i = Number(idx)
    const a = args[i]
    return a === undefined || a === null ? '' : String(a)
  })
}
