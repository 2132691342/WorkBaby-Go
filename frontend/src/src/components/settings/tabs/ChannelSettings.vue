<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { useSettingsStore } from '@/stores/settings'
import { useToast } from '@/composables/useToast'
import { t } from '@/i18n'

/**
 * 设置 · 通道 tab：SMTP 邮件配置。
 */
const settings = useSettingsStore()
const toast = useToast()
const { smtpConfig, error } = storeToRefs(settings)
const { saveSmtpConfig } = settings

async function handleSaveSmtp(): Promise<void> {
  if (await saveSmtpConfig()) {
    toast.success(t('settings.smtpSaved'))
  } else {
    toast.error(error.value ?? t('common.saveFailed'))
  }
}
</script>

<template>
  <section class="card p-4">
    <h2 class="mb-3 font-display text-sm font-semibold text-wb-ink">
      {{ t('settings.smtp') }}
    </h2>
    <el-form class="wb-el-form wb-form-grid" label-position="top">
      <!-- 单行开关不值得一个 section 标题，直接平铺 -->
      <div class="flex items-center gap-3 rounded-lg border border-wb-border bg-wb-surface-2 px-3 py-2">
        <el-switch
          :model-value="smtpConfig.enabled === 'true'"
          @update:model-value="(v: string | number | boolean) => (smtpConfig.enabled = String(v))"
        />
        <span class="text-sm text-wb-ink">{{ t('settings.smtpEnabled') }}</span>
      </div>

      <div class="wb-form-section">
        <h3 class="wb-form-section__title">{{ t('settings.section.server') }}</h3>
        <div class="wb-form-row wb-form-row--2">
          <el-form-item :label="t('settings.smtpHost')" class="wb-form-cell">
            <el-input v-model="smtpConfig.host" :placeholder="t('settings.smtpHostPlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('settings.smtpPort')" class="wb-form-cell">
            <el-input v-model="smtpConfig.port" :placeholder="t('settings.smtpPortPlaceholder')" />
          </el-form-item>
        </div>
      </div>

      <div class="wb-form-section">
        <h3 class="wb-form-section__title">{{ t('settings.section.auth') }}</h3>
        <div class="wb-form-row wb-form-row--2">
          <el-form-item :label="t('settings.smtpUsername')" class="wb-form-cell">
            <el-input v-model="smtpConfig.username" :placeholder="t('settings.smtpUsernamePlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('settings.smtpPassword')" class="wb-form-cell">
            <el-input v-model="smtpConfig.password" type="password" show-password :placeholder="t('settings.smtpPasswordPlaceholder')" />
          </el-form-item>
        </div>
      </div>

      <div class="wb-form-section">
        <h3 class="wb-form-section__title">{{ t('settings.section.sender') }}</h3>
        <el-form-item :label="t('settings.smtpFrom')" class="wb-form-cell">
          <el-input v-model="smtpConfig.from" :placeholder="t('settings.smtpFromPlaceholder')" />
        </el-form-item>
      </div>

      <div class="wb-form-actions">
        <el-button type="primary" @click="handleSaveSmtp">{{ t('settings.saveSmtp') }}</el-button>
      </div>
    </el-form>
  </section>
</template>
