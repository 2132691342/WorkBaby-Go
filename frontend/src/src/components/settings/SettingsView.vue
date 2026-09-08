<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useSettingsStore } from '@/stores/settings'
import { useAdminStore } from '@/stores/admin'
import { t } from '@/i18n'
import { Settings as SettingsIcon } from '@/components/common/icons'
import ProviderSettings from '@/components/settings/tabs/ProviderSettings.vue'
import ChannelSettings from '@/components/settings/tabs/ChannelSettings.vue'
import SearchSettings from '@/components/settings/tabs/SearchSettings.vue'
import AppearanceSettings from '@/components/settings/tabs/AppearanceSettings.vue'
import AdvancedSettings from '@/components/settings/tabs/AdvancedSettings.vue'
import AboutSettings from '@/components/settings/tabs/AboutSettings.vue'

/**
 * 设置中心视图：tab 导航 + 路由 query 同步。
 *
 * <p>6 个 tab 各自独立组件（settings/tabs/）：
 * <ul>
 *   <li><b>模型</b> — ProviderSettings（新增/列表/编辑/连通测试/熔断徽标）</li>
 *   <li><b>通道</b> — ChannelSettings（SMTP）</li>
 *   <li><b>搜索</b> — SearchSettings（DuckDuckGo）</li>
 *   <li><b>外观</b> — AppearanceSettings（语言/主题/背景/字体）</li>
 *   <li><b>高级</b> — AdvancedSettings（exec 白名单）</li>
 *   <li><b>关于</b> — AboutSettings（版本/运行环境/数据目录）</li>
 * </ul>
 * 支持 {@code /settings?tab=about} 直接定位到指定 tab（原 /admin 路由重定向到此处）。
 */
type SettingsTab = 'models' | 'channels' | 'search' | 'appearance' | 'advanced' | 'about'

const settings = useSettingsStore()
const adminStore = useAdminStore()
const route = useRoute()
const router = useRouter()

const tabs: { id: SettingsTab; labelKey: string }[] = [
  { id: 'models', labelKey: 'settings.tab.models' },
  { id: 'channels', labelKey: 'settings.tab.channels' },
  { id: 'search', labelKey: 'settings.tab.search' },
  { id: 'appearance', labelKey: 'settings.tab.appearance' },
  { id: 'advanced', labelKey: 'settings.tab.advanced' },
  { id: 'about', labelKey: 'settings.tab.about' }
]

/** 当前激活 tab：优先路由 query（settings?tab=about），默认 models；el-tabs v-model 直驱。 */
const activeTab = ref<SettingsTab>('models')

/** el-tabs 切换 → 同步路由 query（settings?tab=xxx，支持刷新直达）。 */
function onTabChange(name: string | number): void {
  void router.replace({ query: { ...route.query, tab: String(name) } })
}

onMounted(async () => {
  const q = route.query.tab
  if (q && tabs.some((x) => x.id === q)) activeTab.value = q as SettingsTab
  void settings.load()
  void adminStore.load()
})
</script>

<template>
  <div class="scroll wb-ui">
    <div class="wrap wrap-md">
      <!-- Hero（原型 20 屏） -->
      <header class="hero">
        <div class="tile"><SettingsIcon class="ic" /></div>
        <div>
          <h1>{{ t('settings.title') }}</h1>
          <p>{{ t('settings.subtitle') }}</p>
        </div>
      </header>

      <div v-if="settings.error" class="alert a-info">
        {{ settings.error }}
      </div>

      <!-- ===== el-tabs 全局导航 ===== -->
      <el-tabs v-model="activeTab" class="settings-tabs" @tab-change="onTabChange">
        <el-tab-pane :label="t('settings.tab.models')" name="models">
          <ProviderSettings />
        </el-tab-pane>

        <el-tab-pane :label="t('settings.tab.channels')" name="channels">
          <ChannelSettings />
        </el-tab-pane>

        <el-tab-pane :label="t('settings.tab.search')" name="search">
          <SearchSettings />
        </el-tab-pane>

        <el-tab-pane :label="t('settings.tab.appearance')" name="appearance">
          <AppearanceSettings />
        </el-tab-pane>

        <el-tab-pane :label="t('settings.tab.advanced')" name="advanced">
          <AdvancedSettings />
        </el-tab-pane>

        <el-tab-pane :label="t('settings.tab.about')" name="about">
          <AboutSettings />
        </el-tab-pane>
      </el-tabs>
    </div>
  </div>
</template>

<style scoped>
/* ElementPlus tabs 与 wb 主题融合 + 密度收紧 */
.settings-tabs :deep(.el-tabs__nav-wrap::after) {
  background-color: var(--wb-border);
}
.settings-tabs :deep(.el-tabs__item) {
  color: var(--wb-muted);
  padding: 0 14px;
  height: 38px;
  line-height: 38px;
}
.settings-tabs :deep(.el-tabs__item.is-active) {
  color: var(--wb-primary);
}
.settings-tabs :deep(.el-tabs__active-bar) {
  background-color: var(--wb-primary);
}
/* 稀疏内容页（通道/搜索）不再被 tabs 默认留白撑高 */
.settings-tabs :deep(.el-tabs__header) {
  margin-bottom: 12px;
}
.settings-tabs :deep(.el-tabs__content) {
  padding-top: 0;
}
</style>
