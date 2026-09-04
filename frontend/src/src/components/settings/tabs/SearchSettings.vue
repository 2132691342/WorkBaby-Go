<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { useSettingsStore } from '@/stores/settings'
import { useToast } from '@/composables/useToast'
import { t } from '@/i18n'

/**
 * 设置 · 搜索 tab：DuckDuckGo 联网搜索配置。
 */
const settings = useSettingsStore()
const toast = useToast()
const { webSearchConfig, error } = storeToRefs(settings)
const { saveWebSearchConfig } = settings

async function handleSaveWebSearch(): Promise<void> {
  if (await saveWebSearchConfig()) {
    toast.success(t('settings.websearchSaved'))
  } else {
    toast.error(error.value ?? t('common.saveFailed'))
  }
}
</script>

<template>
  <section class="card p-5">
    <h2 class="mb-4 font-display text-sm font-semibold text-wb-ink">
      {{ t('settings.websearch') }}
    </h2>
    <el-form class="wb-el-form wb-form-grid" label-position="top">
      <div class="wb-form-section">
        <h3 class="wb-form-section__title">{{ t('settings.section.status') }}</h3>
        <div class="flex items-center gap-3 rounded-lg border border-wb-border bg-wb-surface-2 px-3 py-2.5">
          <el-switch
            :model-value="webSearchConfig.enabled === 'true'"
            @update:model-value="(v: string | number | boolean) => (webSearchConfig.enabled = String(v))"
          />
          <span class="text-sm text-wb-ink">{{ t('settings.websearchEnabled') }}</span>
        </div>
        <p class="wb-form-hint mt-2">{{ t('settings.websearchHint') }}</p>
      </div>
      <div class="wb-form-actions">
        <el-button type="primary" @click="handleSaveWebSearch">{{ t('settings.saveWebSearch') }}</el-button>
      </div>
    </el-form>
  </section>
</template>
