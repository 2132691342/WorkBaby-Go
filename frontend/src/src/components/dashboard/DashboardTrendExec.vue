<script setup lang="ts">
import { computed as computed2, onBeforeUnmount, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { GitBranch } from '@/components/common/icons'
import { useDashboardStore } from '@/stores/dashboard'
import { useWbChartTheme } from '@/composables/useWbChartTheme'
import { formatDateTime } from '@/utils/time'
import { t } from '@/i18n'

/**
 * 仪表盘 · 趋势与工作流执行区：7/14/30 日柱状趋势 + 最近执行时间线。
 */
type ECharts = { setOption(o: unknown): void; dispose(): void }

const dashboard = useDashboardStore()
const { stats, trend } = storeToRefs(dashboard)
const chartTheme = useWbChartTheme()

const trendEl = ref<HTMLDivElement | null>(null)
let trendChart: ECharts | null = null

const recentExecutions = computed2(() => stats.value?.recent_executions ?? [])

/** 执行状态 → el-timeline type。 */
function execType(status: string): 'success' | 'danger' | 'warning' | 'primary' | 'info' {
  if (status === 'success' || status === 'completed') return 'success'
  if (status === 'failed' || status === 'error') return 'danger'
  if (status === 'running' || status === 'pending') return 'warning'
  return 'info'
}

function fmtExecTime(ts: number | null): string {
  if (!ts) return ''
  return formatDateTime(ts)
}

async function initTrendChart(el: HTMLDivElement): Promise<ECharts> {
  const core = await import('echarts/core')
  const { BarChart } = await import('echarts/charts')
  const { TooltipComponent, LegendComponent, GridComponent } = await import('echarts/components')
  const { CanvasRenderer } = await import('echarts/renderers')
  core.use([BarChart, TooltipComponent, LegendComponent, GridComponent, CanvasRenderer])
  return core.init(el) as unknown as ECharts
}

function renderTrendChart(): void {
  if (!trendEl.value || !trend.value) return
  const th = chartTheme.value
  void initTrendChart(trendEl.value).then((c) => {
    trendChart = c
    c.setOption({
      tooltip: { trigger: 'axis', backgroundColor: th.surface, borderColor: 'transparent', textStyle: { color: th.ink } },
      legend: {
        bottom: 0,
        textStyle: { color: th.muted },
        icon: 'roundRect'
      },
      grid: { left: 8, right: 8, top: 16, bottom: 36, containLabel: true },
      xAxis: {
        type: 'category',
        data: trend.value?.days ?? [],
        axisLabel: { color: th.muted, fontSize: 10 },
        axisLine: { lineStyle: { color: th.border } }
      },
      yAxis: {
        type: 'value',
        minInterval: 1,
        axisLabel: { color: th.muted, fontSize: 10 },
        splitLine: { lineStyle: { color: th.border } }
      },
      series: [
        {
          name: t('dashboard.trendSessions'),
          type: 'bar',
          barWidth: '32%',
          itemStyle: { borderRadius: [6, 6, 0, 0], color: th.primaryStrong },
          data: trend.value?.sessions ?? []
        },
        {
          name: t('dashboard.trendMessages'),
          type: 'bar',
          barWidth: '32%',
          itemStyle: { borderRadius: [6, 6, 0, 0], color: th.mint },
          data: trend.value?.messages ?? []
        }
      ]
    })
  })
}

watch(() => trend.value, (v) => {
  if (v) renderTrendChart()
}, { deep: true })

// 主题切换时重绘
watch(chartTheme, () => {
  if (trendChart) renderTrendChart()
})

onBeforeUnmount(() => {
  trendChart?.dispose()
})
</script>

<template>
  <div class="grid grid-cols-1 gap-5 lg:grid-cols-3">
    <section class="card p-5 lg:col-span-2">
      <h2 class="mb-3 flex items-center gap-2 font-display text-sm font-semibold text-wb-ink">
        {{ t('dashboard.trend') }}
      </h2>
      <div ref="trendEl" class="h-56 w-full" />
    </section>

    <section class="card p-5">
      <h2 class="mb-3 flex items-center gap-2 font-display text-sm font-semibold text-wb-ink">
        <GitBranch class="h-4 w-4 text-wb-info" />
        {{ t('dashboard.recent_executions') }}
      </h2>
      <el-empty v-if="recentExecutions.length === 0" :description="t('dashboard.noExecutions')" :image-size="80" />
      <el-timeline v-else class="dashboard-timeline">
        <el-timeline-item
          v-for="e in recentExecutions"
          :key="e.id"
          :type="execType(e.status)"
          size="normal"
        >
          <div class="min-w-0">
            <div class="flex items-center gap-1 text-xs text-wb-muted">
              <span class="font-mono text-[10px] text-wb-ink">{{ e.workflow_id.slice(0, 14) }}…</span>
              <span>·</span>
              <span>{{ fmtExecTime(e.started_at) }}</span>
            </div>
            <div v-if="e.error_msg" class="mt-0.5 line-clamp-1 text-xs text-wb-danger">{{ e.error_msg }}</div>
          </div>
        </el-timeline-item>
      </el-timeline>
    </section>
  </div>
</template>

<style scoped>
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
