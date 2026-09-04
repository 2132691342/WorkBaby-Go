<script setup lang="ts">
/**
 * 页面三态组件：loading / empty / error 三态收敛。
 *
 * <p>列表页统一用它渲染 loading / empty / error 三态，避免各页样式与文案漂移。
 * 本组件统一三态展示：
 * <ul>
 *   <li>loading → el-skeleton 骨架屏</li>
 *   <li>empty → el-empty 占位 + 文案</li>
 *   <li>error → 错误文案 + 重试按钮（emit retry）</li>
 * </ul>
 * 优先级：loading > error > empty（error 且 loading 时展示 loading，避免闪烁）。
 */
import { CircleCloseFilled } from '@element-plus/icons-vue'
import { t } from '@/i18n'

defineProps<{
  loading?: boolean
  /** 数据为空（非加载中且无错误时展示）。 */
  empty?: boolean
  /** 错误信息；非空展示错误态。 */
  error?: string | null
  /** 空态文案（缺省走 i18n）。 */
  emptyText?: string
  /** 错误态文案（缺省走 i18n）。 */
  errorText?: string
}>()

defineEmits<{ retry: [] }>()
</script>

<template>
  <!-- 骨架屏（loading 优先） -->
  <div v-if="loading" class="py-8">
    <el-skeleton :rows="4" animated />
  </div>

  <!-- 空态 -->
  <el-empty
    v-else-if="empty && !error"
    :image-size="72"
    :description="emptyText ?? t('ui.state.empty')"
  />

  <!-- 错误态 -->
  <div v-else-if="error" class="flex flex-col items-center gap-3 py-10 text-center">
    <el-icon class="text-3xl text-wb-danger"><CircleCloseFilled /></el-icon>
    <p class="max-w-md text-sm text-wb-muted">{{ errorText ?? t('ui.state.error') }}</p>
    <p class="max-w-md text-xs text-wb-muted/70">{{ error }}</p>
    <el-button size="small" @click="$emit('retry')">{{ t('ui.state.retry') }}</el-button>
  </div>
</template>
