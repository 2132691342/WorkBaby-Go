<script setup lang="ts">
import { computed as computed2, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { Cpu } from '@/components/common/icons'
import { useDashboardStore } from '@/stores/dashboard'
import { t } from '@/i18n'

/**
 * 仪表盘 · 系统运行时折叠区。
 */
const dashboard = useDashboardStore()
const { stats } = storeToRefs(dashboard)

// el-collapse v-model 为激活项名数组
const systemCollapse = ref<string[]>([])

const system = computed2(() => stats.value?.system ?? null)
</script>

<template>
  <section v-if="system" class="card overflow-hidden">
    <el-collapse v-model="systemCollapse" class="dashboard-collapse">
      <el-collapse-item name="runtime">
        <template #title>
          <div class="flex items-center gap-2 font-display text-sm font-semibold text-wb-ink">
            <Cpu class="h-4 w-4 text-wb-info" />
            {{ t('dashboard.runtimeInfo') }}
            <el-tag size="small" type="info" effect="plain" class="ml-1">{{ t('dashboard.devOnly') }}</el-tag>
          </div>
        </template>
        <div class="grid grid-cols-2 gap-4 md:grid-cols-4">
          <div class="space-y-0.5">
            <div class="text-xs text-wb-muted">{{ t('dashboard.os') }}</div>
            <div class="text-sm text-wb-ink">{{ system.os_name }} · {{ system.os_arch }}</div>
            <div class="text-[10px] text-wb-muted">{{ t('dashboard.cpuCores', system.num_cpu) }}</div>
          </div>
          <div class="space-y-0.5">
            <div class="text-xs text-wb-muted">{{ t('dashboard.go_version') }}</div>
            <div class="text-sm text-wb-ink">{{ system.go_version }}</div>
            <div class="text-[10px] text-wb-muted">{{ t('dashboard.goroutines', system.goroutines) }}</div>
          </div>
          <div class="space-y-0.5">
            <div class="text-xs text-wb-muted">{{ t('dashboard.goHeap') }}</div>
            <div class="text-sm text-wb-ink">
              {{ system.heap_alloc_mb }} MB / {{ system.heap_sys_mb }} MB
            </div>
            <div class="text-[10px] text-wb-muted">{{ t('dashboard.heapHint') }}</div>
          </div>
          <div class="space-y-0.5">
            <div class="text-xs text-wb-muted">{{ t('dashboard.user_home') }}</div>
            <div class="truncate font-mono text-[11px] text-wb-ink" :title="system.user_home">{{ system.user_home }}</div>
          </div>
        </div>
      </el-collapse-item>
    </el-collapse>
  </section>
</template>

<style scoped>
/* el-collapse 去默认边框，贴合 card */
.dashboard-collapse {
  border-top: none;
  border-bottom: none;
}
.dashboard-collapse :deep(.el-collapse-item__header),
.dashboard-collapse :deep(.el-collapse-item__wrap) {
  background: transparent;
  border-bottom: none;
  color: var(--wb-ink);
}
.dashboard-collapse :deep(.el-collapse-item__header) {
  padding-left: 20px;
  padding-right: 20px;
}
.dashboard-collapse :deep(.el-collapse-item__content) {
  padding: 0 20px 16px;
}
</style>
