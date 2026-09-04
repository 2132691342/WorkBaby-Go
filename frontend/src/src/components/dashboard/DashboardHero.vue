<script setup lang="ts">
import { computed } from 'vue'
import { storeToRefs } from 'pinia'
import {
  Wrench, RefreshCw, MessageSquare, Activity, Zap
} from '@/components/common/icons'
import { useDashboardStore } from '@/stores/dashboard'
import { t } from '@/i18n'

/**
 * 仪表盘 · Hero 区：问候 + 4 微指标 + 7 日 sparkline + 趋势范围切换。
 */
const dashboard = useDashboardStore()
const { stats, trend, trendRange } = storeToRefs(dashboard)

defineEmits<{ refresh: [] }>()

// ===== Hero 4 微指标 =====
const heroMetrics = computed(() => {
  const s = stats.value
  return [
    {
      key: 'todayMessages',
      label: t('dashboard.today_messages'),
      value: s?.today_messages ?? 0,
      icon: MessageSquare,
      tint: 'bg-wb-primary/15 text-wb-primary-strong'
    },
    {
      key: 'todaySessions',
      label: t('dashboard.today_sessions'),
      value: s?.today_sessions ?? 0,
      icon: Activity,
      tint: 'bg-wb-success/15 text-wb-success'
    },
    {
      key: 'todayTokens',
      label: t('dashboard.today_tokens'),
      value: s?.today_tokens ?? 0,
      icon: Zap,
      tint: 'bg-wb-warning/15 text-wb-warning'
    },
    {
      key: 'aiToolsTotal',
      label: t('dashboard.aiTools'),
      value: s?.ai_tools_total ?? 0,
      icon: Wrench,
      tint: 'bg-wb-info/15 text-wb-info'
    }
  ]
})

/** Hero 7 日 sparkline 数据（messages）。 */
const sparklinePoints = computed(() => {
  const tr = trend.value
  if (!tr || tr.messages.length === 0) return ''
  const data = tr.messages
  const max = Math.max(...data, 1)
  const w = 100
  const h = 28
  const step = w / (data.length - 1 || 1)
  return data
    .map((v, i) => `${(i * step).toFixed(1)},${(h - (v / max) * h).toFixed(1)}`)
    .join(' ')
})

/** 区间内零消息：不画误导性平线，改给引导文案。 */
const trendEmpty = computed(() => {
  const tr = trend.value
  if (!tr || tr.messages.length === 0) return true
  return tr.messages.reduce((a, b) => a + b, 0) === 0
})

/** 24h 简报问候语（基于时间）。 */
const greeting = computed(() => {
  const hour = new Date().getHours()
  if (hour < 6) return t('dashboard.greetingLateNight')
  if (hour < 12) return t('dashboard.greetingMorning')
  if (hour < 18) return t('dashboard.greetingAfternoon')
  return t('dashboard.greetingEvening')
})
</script>

<template>
  <section class="card relative overflow-hidden p-6">
    <div
      class="absolute inset-0 -z-10 opacity-60"
      style="background: radial-gradient(60% 50% at 0% 0%, color-mix(in srgb, var(--wb-primary) 12%, transparent), transparent 60%)"
    />
    <div class="flex items-start justify-between gap-4">
      <div class="min-w-0">
        <div class="flex items-center gap-2">
          <span class="badge-primary inline-flex items-center gap-1">
            <span class="h-1.5 w-1.5 rounded-full bg-wb-success" />
            {{ t('dashboard.live') }}
          </span>
          <span class="text-xs text-wb-muted">{{ t('dashboard.today') }}</span>
        </div>
        <h1 class="mt-2 font-display text-2xl font-bold text-wb-ink">{{ greeting }}</h1>
        <p class="mt-1 text-sm text-wb-muted">{{ t('dashboard.subtitle') }}</p>
      </div>
      <el-button :title="t('dashboard.refresh')" @click="$emit('refresh')">
        <RefreshCw class="h-3.5 w-3.5" />
        <span class="hidden sm:inline">{{ t('dashboard.refresh') }}</span>
      </el-button>
    </div>

    <!-- 4 微指标 -->
    <div class="mt-5 grid grid-cols-2 gap-3 lg:grid-cols-4">
      <div
        v-for="m in heroMetrics"
        :key="m.key"
        class="flex items-center gap-3 rounded-xl border border-wb-border bg-wb-surface/70 p-3"
      >
        <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg" :class="m.tint">
          <component :is="m.icon" class="h-5 w-5" />
        </div>
        <div class="min-w-0">
          <div class="text-xl font-bold tabular-nums text-wb-ink">{{ m.value }}</div>
          <div class="truncate text-[11px] text-wb-muted">{{ m.label }}</div>
        </div>
      </div>
    </div>

    <!-- 7 日 sparkline -->
    <div class="mt-4 flex items-center gap-3 rounded-xl border border-wb-border bg-wb-surface/60 p-3">
      <div class="flex flex-col">
        <span class="text-xs text-wb-muted">{{ t('dashboard.weekTrend') }}</span>
        <span class="text-sm font-medium text-wb-ink">{{ t('dashboard.messagesPerDay') }}</span>
      </div>
      <div class="ml-auto flex-1 max-w-xs">
        <svg v-if="!trendEmpty" viewBox="0 0 100 28" class="h-7 w-full" preserveAspectRatio="none">
          <polyline
            :points="sparklinePoints || '0,14 100,14'"
            fill="none"
            :style="{ stroke: 'var(--wb-primary-strong)' }"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
        <p v-else class="text-center text-[11px] text-wb-muted">{{ t('dashboard.trendEmpty') }}</p>
      </div>
      <el-radio-group v-model="trendRange" size="small" @change="(r: string | number | boolean) => void dashboard.loadTrend(Number(r))">
        <el-radio-button v-for="r in [7, 14, 30]" :key="r" :value="r">
          {{ r }}{{ t('dashboard.trendUnit') }}
        </el-radio-button>
      </el-radio-group>
    </div>
  </section>
</template>
