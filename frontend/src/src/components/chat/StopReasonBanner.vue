<script setup lang="ts">
import { computed } from 'vue'
import { RefreshCw } from '@/components/common/icons'
import { t } from '@/i18n'
import { RESUMABLE_STOP_REASONS, stopReasonSeverity, stopReasonText } from '@/chat/models/blocks'

/**
 * 流终止原因横幅：把 MessageList 里那段 el-alert + 「继续」按钮抽成组件。
 *
 * <p>可恢复终态（RESUMABLE_STOP_REASONS）展示「继续」按钮，
 * 用户一键接着上轮未完成的部分做。
 */
const props = defineProps<{
  stopReason: string | null
  streaming: boolean
}>()

const emit = defineEmits<{
  continue: []
  dismiss: []
}>()

const text = computed(() => {
  const key = stopReasonText(props.stopReason ?? '')
  return key ? t(key) : t('chat.stoppedByUser')
})
const type = computed(() => stopReasonSeverity(props.stopReason ?? '') as 'info' | 'warning' | 'error')
const canContinue = computed(
  () => !props.streaming && !!props.stopReason && RESUMABLE_STOP_REASONS.has(props.stopReason)
)
const show = computed(
  () => !props.streaming && !!props.stopReason && props.stopReason !== 'completed'
)

function onContinue(): void {
  if (props.streaming) return
  emit('continue')
}
</script>

<template>
  <el-alert
    v-if="show"
    :title="text"
    :type="type"
    show-icon
    closable
    :close-text="t('chat.dismiss')"
    class="!rounded-xl"
    @close="emit('dismiss')"
  >
    <div v-if="canContinue" class="mt-1">
      <el-button size="small" text type="primary" :disabled="streaming" @click="onContinue">
        <el-icon class="mr-1"><RefreshCw /></el-icon>
        {{ t('chat.continueRun') }}
      </el-button>
    </div>
  </el-alert>
</template>
