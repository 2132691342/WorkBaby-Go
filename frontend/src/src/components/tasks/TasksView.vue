<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { storeToRefs } from 'pinia'
import { useRouter } from 'vue-router'
import { useTasksStore } from '@/stores/tasks'
import { Activity } from '@/components/common/icons'
import type { Session } from '@/types/api'
import { t } from '@/i18n'
import { formatDateTime } from '@/utils/time'

/**
 * 任务视图（照 prd/WorkBaby-UI-Prototype.html 05 屏）：
 * hero + seg 过滤 + wb-ui 表格（会话 / 消息 / 状态 / 最近活动 / 打开）。
 * 本视图只是 store 的「薄壳」——状态/方法都在 {@link useTasksStore} 中。
 */
const tasks = useTasksStore()
const { sessions, error, loading, filter } = storeToRefs(tasks)
const router = useRouter()

onMounted(tasks.load)

const filtered = computed<Session[]>(() => {
  if (filter.value === 'active') return sessions.value.filter((s) => (s.message_count ?? 0) > 0)
  if (filter.value === 'empty') return sessions.value.filter((s) => (s.message_count ?? 0) === 0)
  return sessions.value
})

function fmt(v: string | number | null): string {
  return formatDateTime(v)
}

function openSession(id: string): void {
  void router.push(`/chat/${id}`)
}
</script>

<template>
  <div class="scroll wb-ui">
    <div class="wrap wrap-md">
      <!-- Hero -->
      <header class="hero">
        <div class="tile"><Activity class="ic" /></div>
        <div>
          <h1>{{ t('task.title') }}</h1>
          <p>{{ t('task.subtitle') }}</p>
        </div>
        <span class="sp" />
        <div class="seg">
          <button :class="{ on: filter === 'all' }" @click="filter = 'all'">{{ t('task.all') }}</button>
          <button :class="{ on: filter === 'active' }" @click="filter = 'active'">{{ t('task.active') }}</button>
          <button :class="{ on: filter === 'empty' }" @click="filter = 'empty'">{{ t('task.emptyFilter') }}</button>
        </div>
      </header>

      <div v-if="error" class="alert a-danger"><span>{{ error }}</span></div>

      <!-- 任务表格 -->
      <div class="card p-sm">
        <div v-if="loading" class="empty">{{ t('ui.status.loading') }}</div>
        <div v-else-if="filtered.length === 0" class="empty">{{ t('task.empty') }}</div>
        <div v-else class="tbl-wrap">
          <table class="tbl">
            <thead>
              <tr>
                <th style="min-width: 220px">{{ t('task.name') }}</th>
                <th class="ta-r">{{ t('task.messageCount') }}</th>
                <th>{{ t('task.status') }}</th>
                <th class="ta-r">{{ t('task.lastActive') }}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              <tr v-for="s in filtered" :key="s.id">
                <td><span style="font-weight: 500">{{ s.name || t('task.unnamed') }}</span></td>
                <td class="ta-r mono">{{ s.message_count ?? 0 }}</td>
                <td>
                  <span class="badge" :class="s.active ? 'b-success' : 'b-neutral'">
                    <span class="dot" />{{ s.active ? 'active' : 'closed' }}
                  </span>
                </td>
                <td class="ta-r mono">{{ fmt(s.last_message_at ?? s.created_at) }}</td>
                <td>
                  <div class="tbl-actions">
                    <button class="btn btn-sm" @click="openSession(s.id)">{{ t('task.open') }}</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>
