<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { ArrowDown } from '@element-plus/icons-vue'
import { useSettingsStore } from '@/stores/settings'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { t } from '@/i18n'
import PageState from '@/components/common/PageState.vue'
import type { AiProvider } from '@/types/api'

/**
 * 设置 · 模型 tab：新增 Provider + 模型列表 + 编辑弹窗 + 连通测试 + 熔断徽标。
 */
const settings = useSettingsStore()
const toast = useToast()
const dialog = useDialog()
const { providers, providerKinds, form, loading, error, circuitStates, chatDefaults } = storeToRefs(settings)
const { load, addProvider, updateProvider, removeProvider, testProvider, resetCircuit, loadChatDefaults, saveChatDefaults } = settings

/** 全局默认参数：进页面即拉取当前 KV 值（未配置显示为空 = 内置兜底）。 */
onMounted(() => {
  void loadChatDefaults()
})

/** el-input-number 清空可能回吐 undefined；统一归一为 null（= 未配置）再保存。 */
async function handleSaveChatDefaults(): Promise<void> {
  chatDefaults.value = {
    default_temperature: chatDefaults.value.default_temperature ?? null,
    default_thinking: chatDefaults.value.default_thinking || null,
    compression_ratio: chatDefaults.value.compression_ratio ?? null,
    max_input_chars: chatDefaults.value.max_input_chars ?? null
  }
  await saveChatDefaults()
}

/** 测试中的 provider id（按钮 pending 态；后端已加 15s 超时，最迟 15s 返回结果）。 */
const testingID = ref<string | null>(null)

/** 新增表单的上下文窗口输入（字符串，提交时解析为 int 写入 form.context_window）。 */
const contextWindowInput = ref<number | undefined>(undefined)

/** 新增 Provider：contextWindow（el-input-number，undefined=null）后委派 store.addProvider。 */
async function handleAddProvider(): Promise<void> {
  form.value.context_window = contextWindowInput.value ?? null
  // 压缩比例缺省 0.9（预留 10% 给生成/工具结果）
  if (form.value.compress_ratio === null || form.value.compress_ratio === undefined) {
    form.value.compress_ratio = 0.9
  }
  // store 已补 toast 反馈；成功才清空输入，失败保留让用户修正
  const ok = await addProvider()
  if (ok) contextWindowInput.value = undefined
}

/** 模型连通测试：开始立即 info 反馈 → 结果 toast。 */
async function handleTestProvider(p: AiProvider): Promise<void> {
  if (testingID.value) return
  testingID.value = p.id
  toast.info(t('settings.testingProvider', p.name))
  try {
    const r = await testProvider(p.id)
    if (r.ok) {
      toast.success(t('settings.providerOk', p.name))
    } else {
      toast.error(t('settings.providerFail', p.name, r.error ?? 'unknown'))
    }
  } finally {
    testingID.value = null
  }
}

/** 删除 provider 加二次确认（破坏性操作规范）。 */
async function handleRemoveProvider(p: AiProvider): Promise<void> {
  const ok = await dialog.confirm({
    title: t('settings.providerDeleteTitle'),
    content: t('settings.providerDeleteConfirm', p.name),
    danger: true
  })
  if (!ok) return
  try {
    await removeProvider(p.id)
    toast.success(t('settings.providerDeleted'))
  } catch (e) {
    toast.error(t('chat.operationFailed'), e instanceof Error ? e.message : String(e))
  }
}

// A4 痛点：清空熔断 — toast 反馈
async function handleResetCircuit(id: string): Promise<void> {
  if (await resetCircuit(id)) {
    toast.success(t('settings.circuitReset'))
  } else {
    toast.error(error.value ?? t('common.saveFailed'))
  }
}

/** A3 痛点：把 CircuitState.state 翻译成人类可读的徽标。
 *  OPEN = registry 未就绪（apiKey 缺失/构建失败/刚新增未加载），给出原因而非误导性"0s 后探活"。 */
function circuitBadge(p: AiProvider): { label: string; cls: string } | null {
  const s = circuitStates.value.get(p.id)
  if (!s) return null
  if (!p.enabled) {
    return { label: '已禁用', cls: 'bg-wb-surface-2 text-wb-muted' }
  }
  if (s.state === 'OPEN') {
    const reason = s.reason || '未就绪（缺少 API Key 或连接配置）'
    return { label: `不可用：${reason}`, cls: 'bg-wb-danger/15 text-wb-danger' }
  }
  if (s.state === 'HALF_OPEN') {
    return { label: '半开探活中', cls: 'bg-wb-warning/15 text-wb-warning' }
  }
  // CLOSED 不显示徽标（默认健康状态）
  return null
}

// ===== 查看详情：只读抽屉，不进编辑即可核对连接与参数 =====
const viewingProvider = ref<AiProvider | null>(null)
function openViewProvider(p: AiProvider): void {
  viewingProvider.value = p
}

function effTemperature(p: AiProvider): string {
  return p.temperature != null && p.temperature > 0 ? String(p.temperature) : t('settings.providerTemperatureDefault')
}

function effThinking(p: AiProvider): string {
  return p.thinking_effort ? t(`chat.effort.${p.thinking_effort}`) : t('settings.thinkingEffortDefault')
}

// ===== 编辑模型（el-dialog + el-form） =====
const editingProviderID = ref<string | null>(null)
const editingProviderDraft = ref<{
  name: string; api_key: string; base_url: string; model: string; alias: string
  context_window: number | null; max_output_tokens: number | null
  compress_ratio: number | null; temperature: number | null; top_p: number | null
  thinking_effort: string | null; thinking_style: string | null
  supports_tool_call: boolean | null; supports_vision: boolean | null; supports_reasoning: boolean | null
}>({
  name: '', api_key: '', base_url: '', model: '', alias: '',
  context_window: null, max_output_tokens: null,
  compress_ratio: 0.9, temperature: null, top_p: null,
  thinking_effort: null, thinking_style: null,
  supports_tool_call: null, supports_vision: null, supports_reasoning: null
})
const editingProviderFeedback = ref<string | null>(null)

/** el-dialog 双向开关（editingProviderID 非空即打开）。 */
const editVisible = computed({
  get: () => editingProviderID.value !== null,
  set: (v: boolean) => {
    if (!v) cancelEditProvider()
  }
})

async function startEditProvider(p: AiProvider): Promise<void> {
  editingProviderID.value = p.id
  editingProviderDraft.value = {
    name: p.name,
    api_key: '',
    base_url: p.base_url ?? '',
    model: p.model,
    alias: p.alias ?? '',
    context_window: p.context_window ?? null,
    max_output_tokens: p.max_output_tokens ?? null,
    compress_ratio: p.compress_ratio ?? 0.9,
    temperature: p.temperature ?? null,
    top_p: p.top_p ?? null,
    thinking_effort: p.thinking_effort ?? null,
    thinking_style: p.thinking_style ?? '',
    supports_tool_call: p.supports_tool_call ?? null,
    supports_vision: p.supports_vision ?? null,
    supports_reasoning: p.supports_reasoning ?? null
  }
  editingProviderFeedback.value = null
}

function cancelEditProvider(): void {
  editingProviderID.value = null
  editingProviderFeedback.value = null
}

async function saveEditProvider(): Promise<void> {
  const editingID = editingProviderID.value
  if (!editingID) return
  const editing = providers.value.find((x) => x.id === editingID)
  if (!editing) return
  editingProviderFeedback.value = null
  if (!editingProviderDraft.value.name.trim() || !editingProviderDraft.value.model.trim()) {
    editingProviderFeedback.value = t('common.nameModelRequired')
    return
  }
  const ok = await updateProvider(editing.id, {
    name: editingProviderDraft.value.name.trim(),
    kind: editing.kind,
    api_key: editingProviderDraft.value.api_key.trim() || undefined,
    base_url: editingProviderDraft.value.base_url.trim() || undefined,
    model: editingProviderDraft.value.model.trim(),
    alias: editingProviderDraft.value.alias.trim() || undefined,
    tier: editing.tier,
    enabled: editing.enabled,
    context_window: editingProviderDraft.value.context_window,
    max_output_tokens: editingProviderDraft.value.max_output_tokens,
    compress_ratio: editingProviderDraft.value.compress_ratio ?? 0.9,
    temperature: editingProviderDraft.value.temperature,
    top_p: editingProviderDraft.value.top_p,
    thinking_effort: editingProviderDraft.value.thinking_effort,
    thinking_style: editingProviderDraft.value.thinking_style || null,
    supports_tool_call: editingProviderDraft.value.supports_tool_call,
    supports_vision: editingProviderDraft.value.supports_vision,
    supports_reasoning: editingProviderDraft.value.supports_reasoning
  })
  if (ok) cancelEditProvider()
}

/** 行内下拉命令分发（查看详情 / 重置熔断）。 */
function onRowCommand(cmd: string, p: AiProvider): void {
  if (cmd === 'view') openViewProvider(p)
  else if (cmd === 'resetCircuit') void handleResetCircuit(p.id)
}

/** 思维方言选项（auto + 后端 llm.AllThinkingStyles；与后端枚举一一对应）。 */
const thinkingStyles = [
  { value: '', label: 'settings.thinkingStyleAuto' },
  { value: 'none', label: 'settings.thinkingStyleNone' },
  { value: 'enabled', label: 'settings.thinkingStyleEnabled' },
  { value: 'adaptive', label: 'settings.thinkingStyleAdaptive' },
  { value: 'reasoning_effort', label: 'settings.thinkingStyleEffort' },
  { value: 'enable_thinking', label: 'settings.thinkingStyleEnableBool' }
]

/** 能力三态选项：null=按模型名与协议自动判定。 */
const capabilityOptions = [
  { value: null, label: 'settings.capAuto' },
  { value: true, label: 'common.enabled' },
  { value: false, label: 'common.disabled' }
]

/** 模型类型下拉（**从后端 /api/v1/ai-provider/kinds 拉** —— 不能前端写死）。
 *  后端 domain.AllProviderKindMetas 与 internal/llm/<kind>/ 实现一一对应；
 *  加新 ProviderKind 时：domain 加枚举 + domain.AllProviderKindMetas 加条目 +
 *  internal/llm/<kind>/ 实现 Provider 接口 + registry.buildOne 加 case —— 前端 0 改动。 */
const kinds = computed(() => providerKinds.value)

/** 按当前 kind 返回 Base URL placeholder（从后端元数据，兜底 OpenAI）。 */
const baseUrlPlaceholder = computed(() => {
  const k = kinds.value.find((x) => x.kind === form.value.kind)
  if (k?.base_url_default) return k.base_url_default
  return 'https://api.openai.com/v1'
})
/** Base URL 下方提示：要求含版本段（v1 / v4 / ...）。 */
const baseUrlHint = computed(() => t('settings.baseUrlHint'))
/** 模型 placeholder：从后端元数据，缺省 gpt-4o。 */
const modelPlaceholder = computed(() => {
  const k = kinds.value.find((x) => x.kind === form.value.kind)
  return k?.model_placeholder || 'gpt-4o'
})
/** 模型下方提示：必须用上游返回的精确模型名。 */
const modelHint = computed(() => t('settings.modelHint'))

// 切换 kind：仅当用户没填过 base_url 时填一次默认值；model 必须用户自己填（避免「自动填了模型却连不通」）。
watch(() => form.value.kind, (kind) => {
  const k = kinds.value.find((x) => x.kind === kind)
  if (!k) return
  if (!form.value.base_url && k.base_url_default) form.value.base_url = k.base_url_default
}, { immediate: true })

defineExpose({ load })
</script>

<template>
  <div>
    <!-- 聊天默认参数：全局兜底层；Provider 级配置优先生效，这里留空 = 内置默认 -->
    <section class="card mb-5 p-5">
      <h2 class="mb-1 font-display text-sm font-semibold text-wb-ink">
        {{ t('settings.chatDefaults.title') }}
      </h2>
      <p class="mb-4 text-xs text-wb-muted">{{ t('settings.chatDefaults.desc') }}</p>
      <el-form class="wb-el-form" label-position="top" @submit.prevent="handleSaveChatDefaults">
        <div class="wb-form-row wb-form-row--2">
          <el-form-item :label="t('settings.chatDefaults.temperature')" class="wb-form-cell">
            <el-input-number
              v-model="chatDefaults.default_temperature"
              :min="0"
              :max="2"
              :step="0.05"
              :precision="2"
              :placeholder="t('settings.chatDefaults.builtin')"
              class="!w-full"
              controls-position="right"
            />
            <div class="wb-form-hint">{{ t('settings.chatDefaults.temperatureTip') }}</div>
          </el-form-item>
          <el-form-item :label="t('settings.chatDefaults.thinking')" class="wb-form-cell">
            <el-select
              v-model="chatDefaults.default_thinking"
              class="!w-full"
              clearable
              :placeholder="t('settings.chatDefaults.builtin')"
            >
              <el-option value="off" :label="t('chat.effort.off')" />
              <el-option value="low" :label="t('chat.effort.low')" />
              <el-option value="medium" :label="t('chat.effort.medium')" />
              <el-option value="high" :label="t('chat.effort.high')" />
            </el-select>
            <div class="wb-form-hint">{{ t('settings.chatDefaults.thinkingTip') }}</div>
          </el-form-item>
          <el-form-item :label="t('settings.chatDefaults.compression')" class="wb-form-cell">
            <el-input-number
              v-model="chatDefaults.compression_ratio"
              :min="0.1"
              :max="1"
              :step="0.01"
              :precision="2"
              :placeholder="t('settings.chatDefaults.builtin')"
              class="!w-full"
              controls-position="right"
            />
            <div class="wb-form-hint">{{ t('settings.chatDefaults.compressionTip') }}</div>
          </el-form-item>
          <el-form-item :label="t('settings.chatDefaults.maxInput')" class="wb-form-cell">
            <el-input-number
              v-model="chatDefaults.max_input_chars"
              :min="100"
              :step="1000"
              :placeholder="t('settings.chatDefaults.builtin')"
              class="!w-full"
              controls-position="right"
            />
            <div class="wb-form-hint">{{ t('settings.chatDefaults.maxInputTip') }}</div>
          </el-form-item>
        </div>
        <div class="wb-form-actions">
          <el-button type="primary" @click="handleSaveChatDefaults">
            {{ t('ui.btn.save') }}
          </el-button>
        </div>
      </el-form>
    </section>

    <section class="card mb-5 p-5">
      <h2 class="mb-4 font-display text-sm font-semibold text-wb-ink">
        {{ t('settings.addProvider') }}
      </h2>
      <el-form class="wb-el-form wb-form-grid" label-position="top" @submit.prevent="handleAddProvider">
        <!-- 基本 -->
        <div class="wb-form-section">
          <h3 class="wb-form-section__title">{{ t('settings.section.basic') }}</h3>
          <div class="wb-form-row wb-form-row--2">
            <el-form-item :label="t('settings.name')" class="wb-form-cell">
              <el-input v-model="form.name" :placeholder="t('settings.name')" clearable />
            </el-form-item>
            <el-form-item :label="t('settings.kindTitle')" class="wb-form-cell">
              <el-select v-model="form.kind" class="w-full">
                <el-option
                  v-for="k in kinds"
                  :key="k.kind"
                  :value="k.kind"
                  :label="k.label"
                />
              </el-select>
              <div class="wb-form-hint">{{ t('settings.kindHint') }}</div>
            </el-form-item>
          </div>
        </div>

        <!-- 连接 -->
        <div class="wb-form-section">
          <h3 class="wb-form-section__title">{{ t('settings.section.connection') }}</h3>
          <div class="wb-form-row wb-form-row--2">
            <el-form-item :label="t('settings.api_key')" class="wb-form-cell">
              <el-input v-model="form.api_key" type="password" show-password :placeholder="t('settings.apiKeyPlaceholder')" />
            </el-form-item>
            <el-form-item :label="t('settings.base_url')" class="wb-form-cell">
              <el-input v-model="form.base_url" :placeholder="baseUrlPlaceholder" class="font-mono" />
              <div class="wb-form-hint">{{ baseUrlHint }}</div>
            </el-form-item>
            <el-form-item :label="t('settings.model')" class="wb-form-cell">
              <el-input v-model="form.model" :placeholder="modelPlaceholder" class="font-mono" />
              <div class="wb-form-hint">{{ modelHint }}</div>
            </el-form-item>
            <el-form-item :label="t('settings.tier')" class="wb-form-cell">
              <el-select v-model="form.tier" class="w-full">
                <el-option value="tier1" label="tier1" />
                <el-option value="tier2" label="tier2" />
              </el-select>
            </el-form-item>
          </div>
        </div>

        <!-- 行为 -->
        <div class="wb-form-section">
          <h3 class="wb-form-section__title">{{ t('settings.section.behavior') }}</h3>
          <div class="wb-form-row wb-form-row--2">
            <el-form-item :label="t('settings.context_window')" class="wb-form-cell">
              <!-- step=1：避免Chrome表单校验(值-min)%step===0拦截 128000 等常用值 -->
              <el-input-number
                v-model="contextWindowInput"
                :min="1"
                :step="1"
                :placeholder="t('settings.context_window')"
                class="!w-full"
                controls-position="right"
              />
            </el-form-item>
            <el-form-item :label="t('settings.compress_ratio')" class="wb-form-cell">
              <!-- step=0.01：避免Chrome表单校验拦截 0.7/0.85 等常用值 -->
              <el-input-number
                v-model="form.compress_ratio"
                :min="0.1"
                :max="1"
                :step="0.01"
                :precision="2"
                :placeholder="t('settings.compress_ratio')"
                class="!w-full"
                controls-position="right"
              />
              <div class="wb-form-hint">{{ t('settings.compressRatioTip') }}</div>
            </el-form-item>
            <el-form-item :label="t('settings.providerTemperature')" class="wb-form-cell">
              <el-input-number
                v-model="form.temperature"
                :min="0"
                :max="2"
                :step="0.05"
                :precision="2"
                :placeholder="t('settings.providerTemperatureDefault')"
                class="!w-full"
                controls-position="right"
              />
              <div class="wb-form-hint">{{ t('settings.providerTemperatureTip') }}</div>
            </el-form-item>
            <el-form-item :label="t('settings.thinking_effort')" class="wb-form-cell">
              <el-select
                v-model="form.thinking_effort"
                class="!w-full"
                clearable
                :placeholder="t('settings.thinkingEffortDefault')"
              >
                <el-option value="off" :label="t('chat.effort.off')" />
                <el-option value="low" :label="t('chat.effort.low')" />
                <el-option value="medium" :label="t('chat.effort.medium')" />
                <el-option value="high" :label="t('chat.effort.high')" />
              </el-select>
              <div class="wb-form-hint">{{ t('settings.thinkingEffortTip') }}</div>
            </el-form-item>
          </div>
        </div>

        <div class="wb-form-actions">
          <el-button type="primary" native-type="submit">{{ t('settings.addProvider') }}</el-button>
        </div>
      </el-form>
    </section>

    <section class="card p-5">
      <h2 class="mb-3 font-display text-sm font-semibold text-wb-ink">
        {{ t('settings.models') }}
        <span v-if="providers.length > 0" class="ml-2 text-xs font-normal text-wb-muted">
          ({{ providers.length }})
        </span>
      </h2>
      <PageState
        :loading="loading"
        :empty="providers.length === 0"
        :error="error"
        @retry="load"
      />
      <el-table
        v-if="!loading && providers.length > 0"
        :data="providers"
        stripe
        class="wb-el-table"
      >
        <el-table-column :label="t('settings.name')" min-width="220">
          <template #default="{ row }">
            <div class="flex flex-wrap items-center gap-2">
              <span class="font-medium text-wb-ink">{{ (row as AiProvider).name }}</span>
              <el-tag size="small" type="info" effect="plain">{{ (row as AiProvider).kind }}</el-tag>
              <el-tag size="small" :type="(row as AiProvider).enabled ? 'success' : 'info'" effect="plain">
                {{ (row as AiProvider).enabled ? t('common.enabled') : t('common.disabled') }}
              </el-tag>
              <span
                v-if="circuitBadge(row as AiProvider)"
                class="rounded px-1.5 py-0.5 text-[10px] font-medium"
                :class="circuitBadge(row as AiProvider)!.cls"
                :title="t('settings.circuitHint')"
              >
                {{ circuitBadge(row as AiProvider)!.label }}
              </span>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('settings.model')" min-width="140">
          <template #default="{ row }">
            <span class="font-mono text-xs text-wb-ink">{{ row.model }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('settings.base_url')" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <span class="font-mono text-xs text-wb-muted">{{ row.base_url || t('settings.noBaseUrl') }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('settings.api_key')" min-width="110">
          <template #default="{ row }">
            <span class="font-mono text-xs text-wb-muted">{{ row.api_key_masked || t('settings.noKey') }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('memory.center.col.actions')" width="240" fixed="right">
          <template #default="{ row }">
            <div class="flex items-center gap-0.5">
              <el-button link type="primary" size="small" :loading="testingID === row.id" @click="handleTestProvider(row as AiProvider)">
                {{ testingID === row.id ? t('settings.testing') : t('settings.test') }}
              </el-button>
              <el-button link type="primary" size="small" @click="startEditProvider(row as AiProvider)">{{ t('ui.btn.edit') }}</el-button>
              <el-button link type="danger" size="small" @click="handleRemoveProvider(row as AiProvider)">{{ t('ui.btn.delete') }}</el-button>
              <el-dropdown trigger="click" @command="(cmd: string) => onRowCommand(cmd, row as AiProvider)">
                <el-button link type="primary" size="small" class="!ml-1">
                  {{ t('settings.moreActions') }}<el-icon class="ml-0.5"><ArrowDown /></el-icon>
                </el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="view">{{ t('settings.viewProvider') }}</el-dropdown-item>
                    <el-dropdown-item
                      v-if="(row as AiProvider).enabled && (circuitStates.get(row.id)?.state === 'OPEN' || circuitStates.get(row.id)?.state === 'HALF_OPEN')"
                      command="resetCircuit"
                    >
                      {{ t('settings.resetCircuit') }}
                    </el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-else-if="!loading" :description="t('settings.empty')" :image-size="80" class="py-4" />
    </section>

    <!-- ===== 编辑模型弹窗（el-dialog + el-form） ===== -->
    <el-dialog v-model="editVisible" :title="t('ui.btn.edit')" width="480px" append-to-body>
      <el-form class="wb-el-form" label-position="top">
        <div class="grid grid-cols-2 gap-3">
          <el-form-item :label="t('settings.name')">
            <el-input v-model="editingProviderDraft.name" />
          </el-form-item>
          <el-form-item :label="t('settings.api_key')">
            <el-input v-model="editingProviderDraft.api_key" type="password" show-password :placeholder="t('settings.apiKeyPlaceholder')" />
          </el-form-item>
        </div>
        <el-form-item :label="t('settings.base_url')">
          <el-input v-model="editingProviderDraft.base_url" :placeholder="baseUrlPlaceholder" class="font-mono" />
          <div class="wb-form-hint">{{ baseUrlHint }}</div>
        </el-form-item>
        <el-form-item :label="t('settings.model')">
          <el-input v-model="editingProviderDraft.model" :placeholder="modelPlaceholder" class="font-mono" />
          <div class="wb-form-hint">{{ modelHint }}</div>
        </el-form-item>
        <el-form-item :label="t('settings.context_window')">
          <el-input-number v-model="editingProviderDraft.context_window" :min="1" :step="1" controls-position="right" class="!w-56" />
        </el-form-item>
        <el-form-item :label="t('settings.max_output_tokens')">
          <el-input-number v-model="editingProviderDraft.max_output_tokens" :min="1" :step="1" controls-position="right" class="!w-56" />
          <div class="w-full text-xs text-wb-muted">{{ t('settings.maxOutputTokensTip') }}</div>
        </el-form-item>
        <el-form-item :label="t('settings.compress_ratio')">
          <el-input-number
            v-model="editingProviderDraft.compress_ratio"
            :min="0.1"
            :max="1"
            :step="0.01"
            :precision="2"
            controls-position="right"
            class="!w-56"
          />
          <div class="w-full text-xs text-wb-muted">{{ t('settings.compressRatioTip') }}</div>
        </el-form-item>
        <el-form-item :label="t('settings.providerTemperature')">
          <el-input-number
            v-model="editingProviderDraft.temperature"
            :min="0"
            :max="2"
            :step="0.05"
            :precision="2"
            :placeholder="t('settings.providerTemperatureDefault')"
            controls-position="right"
            class="!w-56"
          />
          <div class="w-full text-xs text-wb-muted">{{ t('settings.providerTemperatureTip') }}</div>
        </el-form-item>
        <el-form-item :label="t('settings.top_p')">
          <el-input-number
            v-model="editingProviderDraft.top_p"
            :min="0"
            :max="1"
            :step="0.05"
            :precision="2"
            controls-position="right"
            class="!w-56"
          />
          <div class="w-full text-xs text-wb-muted">{{ t('settings.topPTip') }}</div>
        </el-form-item>
        <el-form-item :label="t('settings.thinking_effort')">
          <el-select v-model="editingProviderDraft.thinking_effort" class="!w-56" clearable :placeholder="t('settings.thinkingEffortDefault')">
            <el-option value="off" :label="t('chat.effort.off')" />
            <el-option value="low" :label="t('chat.effort.low')" />
            <el-option value="medium" :label="t('chat.effort.medium')" />
            <el-option value="high" :label="t('chat.effort.high')" />
          </el-select>
          <div class="w-full text-xs text-wb-muted">{{ t('settings.thinkingEffortTip') }}</div>
        </el-form-item>
        <el-form-item :label="t('settings.thinking_style')">
          <el-select v-model="editingProviderDraft.thinking_style" class="!w-56">
            <el-option v-for="s in thinkingStyles" :key="s.value" :value="s.value" :label="t(s.label)" />
          </el-select>
          <div class="w-full text-xs text-wb-muted">{{ t('settings.thinkingStyleTip') }}</div>
        </el-form-item>
        <div class="grid grid-cols-3 gap-3">
          <el-form-item :label="t('settings.capToolCall')">
            <el-select v-model="editingProviderDraft.supports_tool_call" class="!w-full">
              <el-option v-for="o in capabilityOptions" :key="String(o.value)" :value="o.value as boolean | null" :label="t(o.label)" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('settings.capVision')">
            <el-select v-model="editingProviderDraft.supports_vision" class="!w-full">
              <el-option v-for="o in capabilityOptions" :key="String(o.value)" :value="o.value as boolean | null" :label="t(o.label)" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('settings.capReasoning')">
            <el-select v-model="editingProviderDraft.supports_reasoning" class="!w-full">
              <el-option v-for="o in capabilityOptions" :key="String(o.value)" :value="o.value as boolean | null" :label="t(o.label)" />
            </el-select>
          </el-form-item>
        </div>
        <p v-if="editingProviderFeedback" class="text-xs text-wb-danger">{{ editingProviderFeedback }}</p>
      </el-form>
      <template #footer>
        <el-button @click="cancelEditProvider">{{ t('ui.btn.cancel') }}</el-button>
        <el-button type="primary" @click="saveEditProvider">{{ t('ui.btn.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- Provider 详情抽屉：只读元数据（连接/参数/熔断状态） -->
    <el-drawer
      :model-value="viewingProvider !== null"
      :title="viewingProvider?.name ?? ''"
      size="420px"
      append-to-body
      @update:model-value="(v: boolean) => (!v ? (viewingProvider = null) : undefined)"
    >
      <div v-if="viewingProvider" class="flex flex-col gap-3 text-sm">
        <div class="flex flex-wrap items-center gap-2">
          <el-tag size="small" type="info" effect="plain">{{ viewingProvider.kind }}</el-tag>
          <el-tag size="small" effect="plain">{{ viewingProvider.tier }}</el-tag>
          <el-tag size="small" :type="viewingProvider.enabled ? 'success' : 'info'">
            {{ viewingProvider.enabled ? t('common.enabled') : t('common.disabled') }}
          </el-tag>
        </div>
        <div class="flex items-center justify-between border-b border-wb-border pb-2">
          <span class="text-xs text-wb-muted">{{ t('settings.model') }}</span>
          <span class="font-mono text-xs text-wb-ink">{{ viewingProvider.model }}</span>
        </div>
        <div class="border-b border-wb-border pb-2">
          <div class="mb-1 text-xs text-wb-muted">{{ t('settings.base_url') }}</div>
          <div class="break-all font-mono text-xs text-wb-ink">{{ viewingProvider.base_url || t('settings.noBaseUrl') }}</div>
        </div>
        <div class="flex items-center justify-between border-b border-wb-border pb-2">
          <span class="text-xs text-wb-muted">{{ t('settings.api_key') }}</span>
          <span class="font-mono text-xs text-wb-ink">{{ viewingProvider.api_key_masked || t('settings.noKey') }}</span>
        </div>
        <div class="flex items-center justify-between border-b border-wb-border pb-2">
          <span class="text-xs text-wb-muted">{{ t('settings.context_window') }}</span>
          <span class="text-wb-ink">{{ viewingProvider.context_window?.toLocaleString() ?? '—' }}</span>
        </div>
        <div class="flex items-center justify-between border-b border-wb-border pb-2">
          <span class="text-xs text-wb-muted">{{ t('settings.max_output_tokens') }}</span>
          <span class="text-wb-ink">{{ viewingProvider.max_output_tokens?.toLocaleString() || '—' }}</span>
        </div>
        <div class="flex flex-wrap items-center gap-1.5 border-b border-wb-border pb-2">
          <span class="mr-1 text-xs text-wb-muted">{{ t('settings.capToolCall') }} / {{ t('settings.capVision') }} / {{ t('settings.capReasoning') }}</span>
          <span class="rounded px-1.5 py-0.5 text-[10px] font-medium" :class="viewingProvider.tool_call_effective ? 'bg-wb-success/15 text-wb-success' : 'bg-wb-surface-2 text-wb-muted'">{{ t('settings.capToolCall') }}</span>
          <span class="rounded px-1.5 py-0.5 text-[10px] font-medium" :class="viewingProvider.vision_effective ? 'bg-wb-success/15 text-wb-success' : 'bg-wb-surface-2 text-wb-muted'">{{ t('settings.capVision') }}</span>
          <span class="rounded px-1.5 py-0.5 text-[10px] font-medium" :class="viewingProvider.reasoning_effective ? 'bg-wb-success/15 text-wb-success' : 'bg-wb-surface-2 text-wb-muted'">{{ t('settings.capReasoning') }}</span>
        </div>
        <div class="flex items-center justify-between border-b border-wb-border pb-2">
          <span class="text-xs text-wb-muted">{{ t('settings.thinking_style') }}</span>
          <span class="font-mono text-xs text-wb-ink">{{ viewingProvider.thinking_style || `${t('settings.capAuto')} → ${viewingProvider.thinking_style_resolved || 'none'}` }}</span>
        </div>
        <div class="flex items-center justify-between border-b border-wb-border pb-2">
          <span class="text-xs text-wb-muted">{{ t('settings.compress_ratio') }}</span>
          <span class="text-wb-ink">{{ viewingProvider.compress_ratio ?? '—' }}</span>
        </div>
        <div class="flex items-center justify-between border-b border-wb-border pb-2">
          <span class="text-xs text-wb-muted">{{ t('settings.providerTemperature') }}</span>
          <span class="text-wb-ink">{{ effTemperature(viewingProvider) }}</span>
        </div>
        <div class="flex items-center justify-between border-b border-wb-border pb-2">
          <span class="text-xs text-wb-muted">{{ t('settings.thinking_effort') }}</span>
          <span class="text-wb-ink">{{ effThinking(viewingProvider) }}</span>
        </div>
        <div v-if="circuitBadge(viewingProvider)" class="rounded-lg px-3 py-2 text-xs" :class="circuitBadge(viewingProvider)!.cls">
          {{ circuitBadge(viewingProvider)!.label }}
        </div>
      </div>
    </el-drawer>
  </div>
</template>
