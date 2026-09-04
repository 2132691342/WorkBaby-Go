<script setup lang="ts">
import { onMounted } from 'vue'
import { storeToRefs } from 'pinia'
import { Radio } from '@/components/common/icons'
import { useChannelsStore } from '@/stores/channels'
import { useAsyncAction } from '@/composables/useAsyncAction'
import { t } from '@/i18n'
import type { ChannelConfig } from '@/types/api'

/**
 * 通道管理视图。
 *
 * <p>本视图只是 store 的「薄壳」——所有状态/方法都在 {@link useChannelsStore} 中。
 * <p>el-form（channelType el-select + enabled el-switch）+ el-table 通道列表 + 消息日志 el-table。
 */
const channelsStore = useChannelsStore()
const { channels, error, info, loading, editingID, selectedID, messages, form } = storeToRefs(channelsStore)
const { load, select, edit, cancel, action, remove } = channelsStore

// T25 扩展：新建/保存通道包一层，按钮可感知提交中（disabled + 文案回显），防重复提交
const submitting = useAsyncAction(() => channelsStore.submit())

/** 支持的通道类型。WEBHOOK 通用回站（飞书 / 钉钉 / Slack / Discord / 自建等）。 */
const TYPES: { value: 'CONSOLE' | 'EMAIL' | 'WEBHOOK'; label: string; hint: string }[] = [
  { value: 'CONSOLE', label: 'CONSOLE', hint: t('channel.consoleHint') },
  { value: 'EMAIL', label: 'EMAIL', hint: t('channel.emailSmtpNote') },
  { value: 'WEBHOOK', label: 'WEBHOOK', hint: t('channel.webhookHint') }
]

/** 当前类型对应的提示文案。 */
const typeHint = (): string => TYPES.find((x) => x.value === form.value.channel_type)?.hint ?? ''

function statusType(s: string): 'success' | 'danger' | 'info' {
  switch (s) {
    case 'running':
      return 'success'
    case 'failed':
      return 'danger'
    default:
      return 'info'
  }
}

/** 通道类型的展示文案：列表里给短标签，新建表单给 type 名。 */
function channelTypeLabel(type: string): string {
  if (type === 'CONSOLE') return t('channel.type.console')
  if (type === 'EMAIL') return t('channel.type.email')
  if (type === 'WEBHOOK') return t('channel.type.webhook')
  return type
}

onMounted(load)
</script>

<template>
  <div class="flex h-full flex-col overflow-y-auto text-wb-ink">
    <div class="mx-auto w-full max-w-5xl space-y-5 px-6 py-8">
      <!-- Hero header -->
      <header class="flex items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-wb-primary/10 text-wb-primary">
          <Radio class="h-5 w-5" />
        </div>
        <div>
          <h1 class="font-display text-lg font-semibold text-wb-ink">{{ t('channel.title') }}</h1>
          <p class="text-xs text-wb-muted">{{ t('channel.subtitle') }}</p>
        </div>
      </header>

      <div v-if="error" class="rounded-md bg-wb-danger/15 px-3 py-2 text-sm text-wb-danger">{{ error }}</div>
      <div v-if="info" class="rounded-md bg-wb-success/15 px-3 py-2 text-sm text-wb-success">{{ info }}</div>

      <!-- 新建/编辑（el-form） -->
      <section class="card p-5">
        <h2 class="mb-3 font-display text-sm font-semibold text-wb-ink">{{ editingID ? t('channel.edit') : t('channel.new') }}</h2>
        <el-form class="wb-el-form wb-form-grid" label-position="top" @submit.prevent="submitting.run()">
          <div class="wb-form-row wb-form-row--2">
            <el-form-item :label="t('channel.type')" class="wb-form-cell">
              <el-select v-model="form.channel_type" class="w-full">
                <el-option v-for="tp in TYPES" :key="tp.value" :value="tp.value" :label="tp.label" />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('channel.enabled')" class="wb-form-cell">
              <div class="flex h-8 items-center">
                <el-switch v-model="form.enabled" />
              </div>
            </el-form-item>
          </div>

          <!-- 类型说明卡片（所有类型通用） -->
          <div class="flex items-start gap-2 rounded-lg border border-wb-border bg-wb-surface-2 px-3 py-2 text-xs text-wb-muted">
            <span class="font-mono text-[10px] uppercase tracking-wider text-wb-primary">{{ form.channel_type }}</span>
            <span>{{ typeHint() }}</span>
          </div>

          <!-- EMAIL 通道：结构化表单 -->
          <div v-if="form.channel_type === 'EMAIL'" class="rounded-xl border border-wb-border bg-wb-primary/[0.03] p-4">
            <p class="mb-3 text-xs font-medium text-wb-primary-strong">{{ t('channel.emailConfig') }}</p>
            <div class="wb-form-row wb-form-row--2">
              <el-form-item :label="t('channel.email_to')" class="wb-form-cell">
                <el-input v-model="form.email_to" type="email" :placeholder="t('channel.emailToPlaceholder')" />
              </el-form-item>
              <el-form-item :label="t('channel.email_display_name')" class="wb-form-cell">
                <el-input v-model="form.email_display_name" :placeholder="t('channel.emailDisplayNamePlaceholder')" />
              </el-form-item>
            </div>
          </div>

          <!-- WEBHOOK 通道：URL + Secret -->
          <div v-if="form.channel_type === 'WEBHOOK'" class="rounded-xl border border-wb-border bg-wb-primary/[0.03] p-4">
            <p class="mb-3 text-xs font-medium text-wb-primary-strong">{{ t('channel.webhookConfig') }}</p>
            <el-form-item :label="t('channel.webhook_url')" class="wb-form-cell">
              <el-input
                v-model="form.webhook_url"
                :placeholder="t('channel.webhookUrlPlaceholder')"
                clearable
              />
            </el-form-item>
            <p class="wb-form-hint">{{ t('channel.webhookSecretNote') }}</p>
          </div>

          <div class="wb-form-actions">
            <el-button v-if="editingID" @click="cancel">{{ t('ui.btn.cancel') }}</el-button>
            <el-button type="primary" native-type="submit" :loading="submitting.pending.value">
              {{ editingID ? (submitting.pending.value ? t('channel.saving') : t('ui.btn.save')) : (submitting.pending.value ? t('channel.creating') : t('ui.btn.create')) }}
            </el-button>
          </div>
        </el-form>
      </section>

      <div class="grid grid-cols-1 gap-5 lg:grid-cols-2">
        <!-- 通道列表（el-table） -->
        <section class="card p-5">
          <h2 class="mb-3 font-display text-sm font-semibold text-wb-ink">{{ t('channel.list') }}</h2>
          <div v-if="loading" class="text-sm text-wb-muted">{{ t('ui.status.loading') }}</div>
          <el-table
            v-else-if="channels.length > 0"
            :data="channels"
            :highlight-current-row="true"
            :current-row-key="selectedID ?? undefined"
            stripe
            class="wb-el-table"
            @current-change="(row?: ChannelConfig) => row && select(row.id)"
          >
            <el-table-column :label="t('channel.type')" min-width="120">
              <template #default="{ row }">
                <span class="font-medium text-wb-ink">{{ channelTypeLabel((row as ChannelConfig).channel_type) }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('channel.desc')" min-width="220" show-overflow-tooltip>
              <template #default="{ row }">
                <span v-if="(row as ChannelConfig).webhook_url" class="font-mono text-xs text-wb-muted">
                  {{ (row as ChannelConfig).webhook_url }}
                </span>
                <span v-else-if="(row as ChannelConfig).channel_type === 'EMAIL'" class="text-xs text-wb-muted">
                  {{ t('channel.emailConfigured') }}
                </span>
                <span v-else-if="(row as ChannelConfig).channel_type === 'CONSOLE'" class="text-xs text-wb-muted">
                  {{ t('channel.consoleHint') }}
                </span>
                <span v-else class="text-xs text-wb-muted">—</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('channel.status')" width="90">
              <template #default="{ row }">
                <el-tag size="small" :type="statusType((row as ChannelConfig).status)" effect="plain">
                  {{ (row as ChannelConfig).status }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('memory.center.col.actions')" width="240" fixed="right">
              <template #default="{ row }">
                <el-button link type="success" size="small" @click="action((row as ChannelConfig).id, 'start')">{{ t('channel.start') }}</el-button>
                <el-button link type="warning" size="small" @click="action((row as ChannelConfig).id, 'stop')">{{ t('channel.stop') }}</el-button>
                <el-button link size="small" @click="action((row as ChannelConfig).id, 'test')">{{ t('channel.test') }}</el-button>
                <el-button link type="primary" size="small" @click="edit(row as ChannelConfig)">{{ t('ui.btn.edit') }}</el-button>
                <el-button link type="danger" size="small" @click="remove((row as ChannelConfig).id)">{{ t('ui.btn.delete') }}</el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-else :description="t('channel.empty')" :image-size="80" class="py-6" />
        </section>

        <!-- 消息日志 -->
        <section class="card p-5">
          <h2 class="mb-3 font-display text-sm font-semibold text-wb-ink">{{ t('channel.log') }}</h2>
          <div v-if="!selectedID" class="rounded-lg bg-wb-primary/5 p-6 text-center text-sm text-wb-muted">{{ t('channel.selectLog') }}</div>
          <el-table v-else-if="messages.length > 0" :data="messages" stripe class="wb-el-table">
            <el-table-column :label="t('channel.direction')" width="100">
              <template #default="{ row }">
                <el-tag size="small" :type="row.direction === 'inbound' ? 'info' : 'success'" effect="plain">
                  {{ row.direction }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('channel.content')" min-width="200" show-overflow-tooltip>
              <template #default="{ row }">
                <span class="text-xs text-wb-ink">{{ row.content_summary }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('memory.center.col.time')" width="90">
              <template #default="{ row }">
                <span class="text-xs text-wb-muted">{{ row.created_at?.slice(11, 19) }}</span>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-else-if="selectedID" :description="t('channel.noMessages')" :image-size="80" class="py-6" />
        </section>
      </div>
    </div>
  </div>
</template>

