<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { Radio, Plus } from '@/components/common/icons'
import { useChannelsStore } from '@/stores/channels'
import { useAsyncAction } from '@/composables/useAsyncAction'
import { t } from '@/i18n'
import FormDialog from '@/components/common/FormDialog.vue'
import Field from '@/components/common/Field.vue'
import type { ChannelConfig } from '@/types/api'

/**
 * 通道管理视图。
 *
 * <p>本视图只是 store 的「薄壳」——所有状态/方法都在 {@link useChannelsStore} 中。
 * <p>新建 / 编辑走统一表单弹窗（与全局 FormDialog 一致），列表 + 消息日志常驻页面。
 */
const channelsStore = useChannelsStore()
const { channels, error, info, loading, editingID, selectedID, messages, form } = storeToRefs(channelsStore)
const { load, select, action, remove } = channelsStore

// T25 扩展：新建/保存通道包一层，按钮可感知提交中（disabled + 文案回显），防重复提交
const submitting = useAsyncAction(() => channelsStore.submit())

// ===== 新建 / 编辑统一弹窗 =====
const showForm = ref(false)

function openCreate(): void {
  channelsStore.cancel() // 重置为空白表单
  showForm.value = true
}

function openEdit(c: ChannelConfig): void {
  channelsStore.edit(c)
  showForm.value = true
}

async function submitForm(): Promise<void> {
  await submitting.run()
  if (!error.value) showForm.value = false
}

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
  <div class="scroll wb-ui">
    <div class="wrap" style="max-width: 960px">
      <!-- Hero header（原型 16 屏） -->
      <header class="hero">
        <div class="tile"><Radio class="ic" /></div>
        <div>
          <h1>{{ t('channel.title') }}</h1>
          <p>{{ t('channel.subtitle') }}</p>
        </div>
        <span class="sp" />
        <button class="btn btn-primary" @click="openCreate">
          <Plus class="ic ic-sm" />
          {{ t('channel.new') }}
        </button>
      </header>

      <div v-if="error && !showForm" class="alert a-danger">{{ error }}</div>
      <div v-if="info" class="alert a-success">{{ info }}</div>

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
              <el-button link type="primary" size="small" @click="openEdit(row as ChannelConfig)">{{ t('ui.btn.edit') }}</el-button>
              <el-button link type="danger" size="small" @click="remove((row as ChannelConfig).id)">{{ t('ui.btn.delete') }}</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-else :description="t('channel.empty')" :image-size="80" class="py-6" />
      </section>

      <!-- 消息日志 -->
      <section class="card p-5 mt6">
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

    <!-- 新建 / 编辑统一弹窗 -->
    <FormDialog
      v-model="showForm"
      :title="editingID ? t('channel.edit') : t('channel.new')"
      :submitting="submitting.pending.value"
      :confirm-text="editingID ? t('ui.btn.save') : t('ui.btn.create')"
      @confirm="submitForm"
      @close="channelsStore.cancel(); showForm = false"
    >
      <div class="wb-fgrid">
        <Field :label="t('channel.type')">
          <select v-model="form.channel_type" class="input">
            <option v-for="tp in TYPES" :key="tp.value" :value="tp.value">{{ tp.label }}</option>
          </select>
        </Field>
        <Field :label="t('channel.enabled')">
          <div class="flex items-center" style="height: 28px">
            <span class="switch" :class="{ on: form.enabled }" style="cursor: pointer" @click="form.enabled = !form.enabled" />
          </div>
        </Field>

        <p class="wb-fsect__title">{{ typeHint() }}</p>

        <template v-if="form.channel_type === 'EMAIL'">
          <Field :label="t('channel.email_to')">
            <input v-model="form.email_to" class="input" type="email" :placeholder="t('channel.emailToPlaceholder')" />
          </Field>
          <Field :label="t('channel.email_display_name')">
            <input v-model="form.email_display_name" class="input" :placeholder="t('channel.emailDisplayNamePlaceholder')" />
          </Field>
        </template>

        <template v-if="form.channel_type === 'WEBHOOK'">
          <Field :label="t('channel.webhook_url')" :hint="t('channel.webhookSecretNote')" full>
            <input v-model="form.webhook_url" class="input mono" :placeholder="t('channel.webhookUrlPlaceholder')" />
          </Field>
        </template>
      </div>
    </FormDialog>
  </div>
</template>

