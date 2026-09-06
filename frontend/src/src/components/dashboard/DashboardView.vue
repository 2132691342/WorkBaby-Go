<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useDashboardStore } from '@/stores/dashboard'
import DashboardHero from '@/components/dashboard/DashboardHero.vue'
import DashboardActivity from '@/components/dashboard/DashboardActivity.vue'
import DashboardShortcuts from '@/components/dashboard/DashboardShortcuts.vue'
import DashboardTokenCharts from '@/components/dashboard/DashboardTokenCharts.vue'
import DashboardTrendExec from '@/components/dashboard/DashboardTrendExec.vue'
import DashboardRuntime from '@/components/dashboard/DashboardRuntime.vue'

/**
 * 仪表盘：Hero 问候 → 活动流/Bento 入口 → Token 趋势与缓存命中环形图
 * → 趋势柱状图与工作流执行 → 折叠的系统运行时。
 * 数据源 GET /api/v1/dashboard/*；各区块组件经 dashboard store 自取数据。
 */
const dashboard = useDashboardStore()
const tokenChartsRef = ref<InstanceType<typeof DashboardTokenCharts> | null>(null)

async function refresh(): Promise<void> {
  await dashboard.load()
  await dashboard.loadTrend()
  // 按用户当前选择的 scope 重取（保留切过 week/month 的状态）
  await tokenChartsRef.value?.reload()
}

onMounted(() => {
  void dashboard.load()
  void dashboard.loadTrend()
  void dashboard.loadTokenTrend({ scope: 'today' })
})
</script>

<template>
  <div class="wb-dashboard flex h-full overflow-y-auto bg-transparent text-wb-ink">
    <div class="mx-auto w-full max-w-6xl space-y-5 px-6 py-6">
      <!-- ==================== Hero ==================== -->
      <DashboardHero @refresh="refresh" />

      <!-- ==================== Row 1：活动流 + Bento 快捷入口 ==================== -->
      <div class="grid grid-cols-1 gap-5 lg:grid-cols-3">
        <DashboardActivity />
        <DashboardShortcuts />
      </div>

      <!-- ==================== Row 2：Token 消耗三线折线 + 缓存命中率 ==================== -->
      <DashboardTokenCharts ref="tokenChartsRef" />

      <!-- ==================== Row 2b：趋势 + 工作流执行 ==================== -->
      <DashboardTrendExec />

      <!-- ==================== Row 3：折叠的系统运行时 ==================== -->
      <DashboardRuntime />

      <div v-if="dashboard.error" class="rounded-lg bg-wb-danger/10 px-3 py-2 text-sm text-wb-danger">
        {{ dashboard.error }}
      </div>
    </div>
  </div>
</template>
