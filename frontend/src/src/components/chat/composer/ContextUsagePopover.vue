<script setup lang="ts">
import { computed } from 'vue'
import { t } from '@/i18n'

/** 上下文窗口占用指示条：used / max 百分比，≥90% 红、≥70% 橙、其余绿。 */
const props = defineProps<{
  used: number
  max: number
}>()

const pct = computed(() => (props.max <= 0 ? 0 : Math.min(100, Math.round((props.used / props.max) * 100))))

const color = computed(() => {
  if (pct.value >= 90) return 'var(--wb-danger)'
  if (pct.value >= 70) return 'var(--wb-warning)'
  return 'var(--wb-success)'
})

/** 千分位精简显示：128000 → 128k，方便塞进底部窄行。 */
function compact(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1000) return `${Math.round(n / 1000)}k`
  return String(n)
}

const detail = computed(() => `${compact(props.used)} / ${compact(props.max)}`)
</script>

<template>
  <el-tooltip :content="`${t('chat.ctxUsage')} ${detail}`" placement="top">
    <div class="flex shrink-0 cursor-default items-center gap-1.5">
      <el-progress
        :percentage="pct"
        :color="color"
        :stroke-width="5"
        :show-text="false"
        class="w-12"
      />
      <span class="text-[10px] tabular-nums text-wb-muted">{{ pct }}%</span>
    </div>
  </el-tooltip>
</template>

<style scoped>
/* el-progress 自带 margin/line-height，在底部窄行里需要压掉 */
:deep(.el-progress) {
  line-height: 1;
}
:deep(.el-progress-bar__outer) {
  background-color: color-mix(in srgb, var(--wb-border) 70%, transparent);
}
</style>