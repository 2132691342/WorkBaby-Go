<script setup lang="ts">
import { Layers } from '@/components/common/icons'
import { t } from '@/i18n'

/**
 * 轮次中队列：助手流式回复期间用户可继续输入，消息入队并在当前轮结束后自动出队发送。
 */
export interface QueuedMessage {
  id: string
  text: string
}

defineProps<{
  queue: QueuedMessage[]
}>()

const emit = defineEmits<{
  remove: [id: string]
  flush: []
}>()
</script>

<template>
  <div v-if="queue.length > 0" class="flex flex-col gap-1.5">
    <div class="flex items-center justify-between px-1 text-[10px] text-wb-muted">
      <span class="inline-flex items-center gap-1">
        <el-icon :size="11"><Layers /></el-icon>
        {{ t('queue.label', queue.length) }}
      </span>
      <el-button text size="small" @click="emit('flush')">{{ t('queue.sendNow') }}</el-button>
    </div>
    <div class="flex flex-wrap gap-1.5 px-1">
      <el-tag
        v-for="m in queue"
        :key="m.id"
        closable
        type="primary"
        effect="plain"
        size="small"
        @close="emit('remove', m.id)"
      >
        <span class="max-w-[220px] truncate">{{ m.text }}</span>
      </el-tag>
    </div>
  </div>
</template>
