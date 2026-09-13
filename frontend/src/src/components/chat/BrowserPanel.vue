<script setup lang="ts">
/**
 * 浏览器面板（右侧栏）：托管浏览器的远程视图。
 *
 * <p>视口截图轮询（JPEG），点击/滚动/按键经后端 CDP 派发回真实页面；
 * 截图像素坐标 → CSS 坐标按 shot.w 与显示宽度等比换算。
 */
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Globe, RefreshCw, ArrowUp, ArrowDown } from '@/components/common/icons'
import { apiGet, apiPost } from '@/api/client'
import { t } from '@/i18n'
import { useToast } from '@/composables/useToast'

interface BrowserStatus { running: boolean; browser: string; url: string; title: string }
interface Screenshot { image: string; w: number; h: number }

const toast = useToast()

const status = ref<BrowserStatus | null>(null)
const shot = ref<Screenshot | null>(null)
const urlInput = ref('')
const typeInput = ref('')
const busy = ref(false)
const imgEl = ref<HTMLImageElement | null>(null)
let pollTimer: number | null = null

const running = computed(() => status.value?.running ?? false)

async function loadStatus(): Promise<void> {
  try {
    status.value = await apiGet<BrowserStatus>('/api/v1/browser/status')
    if (status.value.running && !urlInput.value) urlInput.value = status.value.url
  } catch {
    /* 后端未就绪时静默 */
  }
}

async function start(): Promise<void> {
  busy.value = true
  try {
    status.value = await apiPost<BrowserStatus>('/api/v1/browser/start', {})
    urlInput.value = status.value.url
    startPoll()
  } catch (e) {
    toast.error(t('browser.startFailed'), e instanceof Error ? e.message : String(e))
  } finally {
    busy.value = false
  }
}

async function stop(): Promise<void> {
  stopPoll()
  shot.value = null
  await apiPost('/api/v1/browser/stop', {}).catch(() => undefined)
  await loadStatus()
}

function startPoll(): void {
  stopPoll()
  pollTimer = window.setInterval(async () => {
    if (!running.value) return
    try {
      shot.value = await apiGet<Screenshot>('/api/v1/browser/screenshot')
    } catch {
      /* 轮询失败静默 */
    }
  }, 1600)
  void refreshShot()
}

function stopPoll(): void {
  if (pollTimer !== null) {
    window.clearInterval(pollTimer)
    pollTimer = null
  }
}

async function refreshShot(): Promise<void> {
  try {
    shot.value = await apiGet<Screenshot>('/api/v1/browser/screenshot')
  } catch {
    /* 静默 */
  }
}

async function navigate(): Promise<void> {
  if (!urlInput.value.trim()) return
  busy.value = true
  try {
    await apiPost('/api/v1/browser/navigate', { url: urlInput.value.trim() })
    await refreshShot()
  } catch (e) {
    toast.error(t('browser.navFailed'), e instanceof Error ? e.message : String(e))
  } finally {
    busy.value = false
  }
}

/** 截图点击：显示坐标 → CSS 视口坐标。 */
async function onClickShot(e: MouseEvent): Promise<void> {
  if (!shot.value || !imgEl.value) return
  const rect = imgEl.value.getBoundingClientRect()
  const x = Math.round(((e.clientX - rect.left) / rect.width) * shot.value.w)
  const y = Math.round(((e.clientY - rect.top) / rect.height) * shot.value.h)
  await apiPost('/api/v1/browser/click', { x, y }).catch(() => undefined)
  await refreshShot()
}

async function scrollPage(dy: number): Promise<void> {
  await apiPost('/api/v1/browser/scroll', { dx: 0, dy }).catch(() => undefined)
  await refreshShot()
}

async function typeSend(submit: boolean): Promise<void> {
  if (!typeInput.value && !submit) return
  await apiPost('/api/v1/browser/type', { text: typeInput.value, submit }).catch(() => undefined)
  typeInput.value = ''
  await refreshShot()
}

function onTypeKeydown(e: KeyboardEvent): void {
  if (e.key === 'Enter') {
    e.preventDefault()
    void typeSend(true)
  }
}

watch(running, (v) => {
  if (v) startPoll()
  else stopPoll()
}, { immediate: true })

onBeforeUnmount(stopPoll)
</script>

<template>
  <div class="flex h-full min-h-0 flex-col text-xs">
    <!-- 未启动引导 -->
    <div v-if="!running" class="flex flex-1 flex-col items-center justify-center gap-3 p-4 text-center">
      <Globe class="h-6 w-6 text-wb-muted" />
      <p class="text-wb-ink">{{ t('browser.startTitle') }}</p>
      <p class="max-w-[15rem] text-[11px] text-wb-muted">{{ t('browser.startHint') }}</p>
      <button type="button" class="btn btn-primary btn-sm" :disabled="busy" @click="start">
        <Globe class="ic ic-sm" />
        {{ t('browser.start') }}
      </button>
    </div>

    <template v-else>
      <!-- 地址栏 -->
      <div class="flex shrink-0 items-center gap-1.5 border-b border-wb-border px-2.5 py-1.5">
        <Globe class="h-3.5 w-3.5 shrink-0 text-wb-primary" />
        <input
          v-model="urlInput"
          class="min-w-0 flex-1 rounded-md border border-wb-border bg-wb-surface-2 px-2 py-1 text-[11px] text-wb-ink outline-none focus:border-wb-primary/50"
          :placeholder="t('browser.urlPlaceholder')"
          spellcheck="false"
          @keydown.enter="navigate"
        >
        <button type="button" class="btn btn-sm" :title="t('browser.refresh')" @click="refreshShot">
          <RefreshCw class="ic ic-sm" />
        </button>
        <button type="button" class="btn btn-sm" style="color: var(--wb-danger)" :title="t('browser.stop')" @click="stop">
          ×
        </button>
      </div>

      <!-- 远程视图 -->
      <div class="min-h-0 flex-1 overflow-y-auto bg-wb-surface-2">
        <img
          v-if="shot"
          ref="imgEl"
          :src="`data:image/jpeg;base64,${shot.image}`"
          class="browser-shot w-full cursor-crosshair select-none"
          :title="status?.title"
          @click="onClickShot"
        >
        <div v-else class="flex h-full items-center justify-center text-[11px] text-wb-muted">
          {{ t('ui.status.loading') }}
        </div>
      </div>

      <!-- 交互行：滚动 + 输入 -->
      <div class="shrink-0 border-t border-wb-border px-2.5 py-1.5">
        <div class="mb-1.5 flex items-center justify-center gap-2">
          <button type="button" class="btn btn-sm" @click="scrollPage(-500)">
            <ArrowUp class="ic ic-sm" />
          </button>
          <button type="button" class="btn btn-sm" @click="scrollPage(500)">
            <ArrowDown class="ic ic-sm" />
          </button>
        </div>
        <div class="flex items-center gap-1.5">
          <input
            v-model="typeInput"
            class="min-w-0 flex-1 rounded-md border border-wb-border bg-wb-surface-2 px-2 py-1 text-[11px] text-wb-ink outline-none focus:border-wb-primary/50"
            :placeholder="t('browser.typePlaceholder')"
            @keydown="onTypeKeydown"
          >
          <button type="button" class="btn btn-sm" @click="typeSend(false)">{{ t('browser.type') }}</button>
          <button type="button" class="btn btn-primary btn-sm" @click="typeSend(true)">{{ t('browser.submit') }}</button>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.browser-shot {
  display: block;
  border-radius: 6px;
}
</style>
