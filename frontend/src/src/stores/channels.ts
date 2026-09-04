import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiGet, apiPost } from '@/api/client'
import { useDialog } from '@/composables/useDialog'
import { t } from '@/i18n'
import type { ChannelConfig, ChannelConfigReq, ChannelMessageLog } from '@/types/api'

/**
 * 通道 store（Pinia 重构）。
 *
 * <p>职责：通道列表 CRUD + 消息日志查看 + start/stop/test 动作。
 * 替代原 ChannelsView.vue 内的本地 ref。
 *
 * <p><b>与后端的契约</b>：
 * <ul>
 *   <li>列表：{@code GET /api/v1/channels}</li>
 *   <li>消息日志：{@code GET /api/v1/channels/{id}/messages?limit=50}</li>
 *   <li>创建：{@code POST /api/v1/channels}</li>
 *   <li>更新：{@code POST /api/v1/channels/{id}/update}</li>
 *   <li>动作：{@code POST /api/v1/channels/{id}/{op}}（op = start|stop|test）</li>
 *   <li>删除：{@code POST /api/v1/channels/{id}/delete}</li>
 * </ul>
 */
export const useChannelsStore = defineStore('channels', () => {
  const channels = ref<ChannelConfig[]>([])
  const error = ref<string | null>(null)
  const info = ref<string | null>(null)
  const loading = ref(false)
  const editingID = ref<string | null>(null)
  const selectedID = ref<string | null>(null)
  const messages = ref<ChannelMessageLog[]>([])

  function blank(): ChannelConfigReq {
    return { channel_type: 'CONSOLE', enabled: true, config_json: '', webhook_url: '', email_to: '', email_display_name: '' }
  }

  const form = ref<ChannelConfigReq>(blank())

  /** 加载通道列表；无选中项时自动选中首个通道。 */
  async function load(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      channels.value = await apiGet<ChannelConfig[]>('/api/v1/channels')
      if (!selectedID.value && channels.value.length > 0) {
        await select(channels.value[0].id)
      }
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  /** 选中通道并加载其消息日志（失败时清空日志）。 */
  async function select(id: string): Promise<void> {
    selectedID.value = id
    try {
      messages.value = await apiGet<ChannelMessageLog[]>(`/api/v1/channels/${id}/messages?limit=50`)
    } catch {
      messages.value = []
    }
  }

  /** 进入编辑模式，回填表单（解析 configJson → 结构化字段）。 */
  function edit(c: ChannelConfig): void {
    editingID.value = c.id
    const parsed = parseChannelConfig(c.channel_type, c.config_json)
    form.value = {
      channel_type: c.channel_type,
      enabled: c.enabled,
      config_json: c.config_json ?? '',
      webhook_url: c.webhook_url ?? '',
      email_to: parsed.email_to,
      email_display_name: parsed.email_display_name
    }
  }

  /** 取消编辑，重置表单。 */
  function cancel(): void {
    editingID.value = null
    form.value = blank()
  }

  /** 提交新建或更新（自动将结构化字段组装为 configJson）。 */
  async function submit(): Promise<void> {
    error.value = null
    info.value = null
    if (!form.value.channel_type) {
      error.value = t('channel.errSelectType')
      return
    }
    // 邮箱通道：校验收件人地址
    if (form.value.channel_type === 'EMAIL' && !form.value.email_to?.trim()) {
      error.value = t('channel.errEmailTo')
      return
    }
    // Webhook 通道：校验 URL（必填 + http(s):// 开头）
    if (form.value.channel_type === 'WEBHOOK') {
      const url = (form.value.webhook_url ?? '').trim()
      if (!url) {
        error.value = t('channel.errWebhookUrl')
        return
      }
      if (!/^https?:\/\//i.test(url)) {
        error.value = t('channel.errWebhookUrlFormat')
        return
      }
    }
    // 组装 configJson
    const finalForm: ChannelConfigReq = {
      ...form.value,
      config_json: assembleChannelConfig(form.value.channel_type, form.value)
    }
    try {
      if (editingID.value) {
        await apiPost<ChannelConfig>(`/api/v1/channels/${editingID.value}/update`, finalForm)
        info.value = t('common.updated')
      } else {
        await apiPost<ChannelConfig>('/api/v1/channels', finalForm)
        info.value = t('common.created')
      }
      cancel()
      await load()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  /** 执行 start/stop/test 动作。 */
  async function action(id: string, op: 'start' | 'stop' | 'test'): Promise<void> {
    error.value = null
    info.value = null
    try {
      await apiPost(`/api/v1/channels/${id}/${op}`)
      info.value = t('channel.executed', op)
      await load()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  /** 删除通道。 */
  async function remove(id: string): Promise<void> {
    const dialog = useDialog()
    if (!await dialog.confirm({ title: t('common.confirmDeleteTitle'), content: t('common.confirmDelete'), danger: true })) return
    try {
      await apiPost(`/api/v1/channels/${id}/delete`)
      if (selectedID.value === id) selectedID.value = null
      await load()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  /** 解析 configJson → 结构化字段（用于编辑时回填）。 */
  function parseChannelConfig(channelType: string, configJson?: string | null): { email_to: string; email_display_name: string } {
    if (!configJson) return { email_to: '', email_display_name: '' }
    try {
      const obj = JSON.parse(configJson)
      if (channelType === 'EMAIL') {
        return { email_to: obj.to ?? '', email_display_name: obj.displayName ?? '' }
      }
    } catch { /* ignore parse error */ }
    return { email_to: '', email_display_name: '' }
  }

  /** 将结构化字段组装为 configJson 字符串。 */
  function assembleChannelConfig(channelType: string, form: ChannelConfigReq): string {
    if (channelType === 'EMAIL') {
      const obj: Record<string, string> = { to: form.email_to ?? '' }
      if (form.email_display_name) obj.displayName = form.email_display_name
      return JSON.stringify(obj)
    }
    return ''
  }

  return {
    channels,
    error,
    info,
    loading,
    editingID,
    selectedID,
    messages,
    form,
    load,
    select,
    edit,
    cancel,
    submit,
    action,
    remove
  }
})
