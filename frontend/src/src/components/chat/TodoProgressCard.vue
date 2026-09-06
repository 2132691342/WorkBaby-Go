<script setup lang="ts">
/**
 * Todo 进度卡：消费 chat.todoState（流式 chat:todo 事件增量）。
 *
 * <p>与 chat:stream 流同帧同步：模型列计划 → 用户看到「待办清单」出现；
 * 逐步 mark_done/mark_undone 时勾选态实时翻转。空状态不渲染卡片。
 *
 * <p>全部完成（progress=100%）后自动折叠成单行摘要，避免长会话中
 * 已收尾的计划持续占用视线焦点。
 */
import { computed, ref, watch } from 'vue'
import { ChevronDown, CircleCheck, Circle } from '@/components/common/icons'
import { useChatStore } from '@/stores/chat'
import { t } from '@/i18n'

const chat = useChatStore()
const items = computed(() => chat.todoState?.items ?? [])
const done = computed(() => items.value.filter((x) => x.done).length)
const total = computed(() => items.value.length)
const progress = computed(() => (total.value === 0 ? 0 : Math.round((done.value / total.value) * 100)))
const isDone = computed(() => total.value > 0 && done.value === total.value)

const collapsed = ref(false)

watch(isDone, (v) => {
  if (v) collapsed.value = true
})
</script>

<template>
  <div v-if="total > 0" class="rounded-xl border border-wb-border bg-wb-surface shadow-sm">
    <button
      type="button"
      class="flex w-full items-center gap-2 px-3 py-2 text-left"
      :aria-expanded="!collapsed"
      @click="collapsed = !collapsed"
    >
      <span class="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-md bg-wb-primary/10 text-wb-primary">
        <svg class="h-3 w-3" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
          <path d="M3 4.5h10M3 8h10M3 11.5h6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
        </svg>
      </span>
      <span class="text-xs font-medium text-wb-ink">{{ t('chat.todo.title') }}</span>
      <span class="text-[10px] text-wb-muted tabular-nums">{{ done }} / {{ total }}</span>
      <span class="ml-auto flex items-center gap-1.5 text-[10px] text-wb-muted tabular-nums">
        <span v-if="!collapsed" class="h-1 w-12 overflow-hidden rounded-full bg-wb-surface-2">
          <span
            class="block h-full rounded-full bg-wb-primary transition-all duration-300 ease-out"
            :style="{ width: progress + '%' }"
          />
        </span>
        <span v-else class="text-wb-mint">✓</span>
        <span>{{ progress }}%</span>
        <ChevronDown
          class="h-3 w-3 transition-transform duration-200"
          :class="collapsed ? '' : 'rotate-180'"
        />
      </span>
    </button>
    <ul v-if="!collapsed" class="space-y-1 px-3 pb-3">
      <li
        v-for="item in items"
        :key="item.id"
        class="flex items-center gap-2 text-xs leading-snug"
        :class="item.done ? 'text-wb-muted' : 'text-wb-ink'"
      >
        <component
          :is="item.done ? CircleCheck : Circle"
          class="h-3.5 w-3.5 shrink-0"
          :class="item.done ? 'text-wb-mint' : 'text-wb-muted'"
        />
        <span :class="item.done ? 'line-through' : ''">{{ item.title }}</span>
      </li>
    </ul>
  </div>
</template>
