<script setup lang="ts">
import { computed } from 'vue'
import { storeToRefs } from 'pinia'
import { Activity } from '@/components/common/icons'
import { useDashboardStore } from '@/stores/dashboard'
import { t } from '@/i18n'

/**
 * 仪表盘 · 活动流：最近消息时间线。
 */
const dashboard = useDashboardStore()
const { stats } = storeToRefs(dashboard)

const recentMessages = computed(() => stats.value?.recent_messages ?? [])

function fmtMsgTime(ts: number): string {
  if (!ts) return ''
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ''
  const now = Date.now()
  const diff = (now - d.getTime()) / 1000
  if (diff < 60) return t('dashboard.justNow')
  if (diff < 3600) return t('dashboard.minutesAgo', Math.floor(diff / 60))
  if (diff < 86400) return t('dashboard.hoursAgo', Math.floor(diff / 3600))
  return t('dashboard.daysAgo', Math.floor(diff / 86400))
}
</script>

<template>
  <section class="card p-5 lg:col-span-2">
    <div class="mb-3 flex items-center justify-between">
      <h2 class="flex items-center gap-2 font-display text-sm font-semibold text-wb-ink">
        <Activity class="h-4 w-4 text-wb-primary" />
        {{ t('dashboard.activity') }}
      </h2>
      <RouterLink to="/chat" class="text-xs text-wb-primary-strong hover:underline">
        {{ t('dashboard.viewAll') }}
      </RouterLink>
    </div>
    <el-empty v-if="recentMessages.length === 0" :description="t('dashboard.noActivity')" :image-size="80" />
    <el-timeline v-else class="dashboard-timeline">
      <el-timeline-item
        v-for="m in recentMessages"
        :key="m.id"
        :type="m.role === 'assistant' ? 'primary' : 'info'"
        :hollow="m.role !== 'assistant'"
        size="normal"
      >
        <div class="min-w-0">
          <div class="flex items-center gap-2 text-xs text-wb-muted">
            <span class="font-medium text-wb-ink">{{ m.role === 'assistant' ? 'WorkBaby' : t('dashboard.you') }}</span>
            <span>·</span>
            <span>{{ fmtMsgTime(m.created_at) }}</span>
          </div>
          <div class="mt-0.5 line-clamp-2 text-sm text-wb-ink">{{ m.content }}</div>
        </div>
      </el-timeline-item>
    </el-timeline>
  </section>
</template>

<style scoped>
/* el-timeline 融入 wb 主题：紧凑间距 + 节点色跟随主题 */
.dashboard-timeline {
  padding-left: 4px;
}
.dashboard-timeline :deep(.el-timeline-item) {
  padding-bottom: 12px;
}
.dashboard-timeline :deep(.el-timeline-item__tail) {
  border-left-color: var(--wb-border);
}
</style>
