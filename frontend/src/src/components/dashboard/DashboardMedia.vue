<script setup lang="ts">
import { computed as computed2 } from 'vue'
import { storeToRefs } from 'pinia'
import { Film, Image as ImageIcon } from '@/components/common/icons'
import { useDashboardStore } from '@/stores/dashboard'
import { t } from '@/i18n'

/**
 * 仪表盘 · 最近媒体区。
 */
const dashboard = useDashboardStore()
const { stats } = storeToRefs(dashboard)

const recentMedia = computed2(() => stats.value?.recent_media ?? [])

/** 媒体产物走本地受管文件服务（AssetServer /files/**，非业务 API）。 */
function mediaUrl(id: string): string {
  return `/files/media/${id}`
}
function fmtBytes(n: number | null | undefined): string {
  if (!n) return '—'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}
</script>

<template>
  <section class="card p-5">
    <div class="mb-3 flex items-center justify-between">
      <h2 class="flex items-center gap-2 font-display text-sm font-semibold text-wb-ink">
        <Film class="h-4 w-4 text-wb-warning" />
        {{ t('dashboard.recent_media') }}
      </h2>
      <RouterLink to="/media" class="text-xs text-wb-primary-strong hover:underline">
        {{ t('dashboard.viewAll') }}
      </RouterLink>
    </div>
    <div v-if="recentMedia.length === 0" class="rounded-lg bg-wb-warning/5 p-6 text-center text-sm text-wb-muted">
      {{ t('dashboard.noMedia') }}
    </div>
    <div v-else class="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4">
      <div
        v-for="m in recentMedia"
        :key="m.id"
        class="group relative aspect-square overflow-hidden rounded-xl border border-wb-border bg-wb-surface/60 transition-all hover:-translate-y-0.5 hover:border-wb-primary/40 hover:shadow-[var(--wb-shadow)]"
      >
        <img
          v-if="m.mime_type?.startsWith('image/')"
          :src="mediaUrl(m.id)"
          :alt="m.prompt ?? m.kind"
          loading="lazy"
          class="h-full w-full object-cover"
        />
        <div v-else class="flex h-full w-full items-center justify-center bg-wb-primary/5 text-wb-primary">
          <ImageIcon class="h-8 w-8" />
        </div>
        <div class="absolute inset-x-0 bottom-0 p-2 opacity-0 transition-opacity group-hover:opacity-100">
          <div class="line-clamp-2 text-[10px] text-white">{{ m.prompt ?? m.kind }}</div>
          <div class="mt-0.5 text-[9px] text-white/70">{{ m.kind }} · {{ fmtBytes(m.file_size) }}</div>
        </div>
      </div>
    </div>
  </section>
</template>
