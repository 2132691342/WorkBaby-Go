<script setup lang="ts">
import { onMounted, ref, type Component } from 'vue'
import { storeToRefs } from 'pinia'
import { useRouter } from 'vue-router'
import { BookOpen, LayoutTemplate, PawPrint, ArrowRight, Rocket, X, Sparkles } from '@/components/common/icons'
import { useHomeStore } from '@/stores/home'
import { t } from '@/i18n'
import type { Session } from '@/types/api'
import { formatDateTime } from '@/utils/time'

/**
 * 首页（D.2.1 · WorkBaby HomePage 欢迎区 + Phase D 快速上手引导）。
 *
 * <p>布局（浅色、留白充足、居中欢迎）：
 * <ol>
 *   <li>欢迎区：WorkBaby mascot + 欢迎语 + 副标题</li>
 *   <li>快速上手引导卡（首次访问显示，可关闭，localStorage 记忆）</li>
 *   <li>3 个等宽圆角快捷卡片：能做什么 / 简历 / 领养桌宠</li>
 *   <li>最近会话列表（真实数据，来自 {@link useHomeStore}）</li>
 * </ol>
 */
const home = useHomeStore()
const { sessions, loading } = storeToRefs(home)
const router = useRouter()

interface QuickCard {
  to: string
  titleKey: string
  descKey: string
  icon: Component
  tint: string
}

const quickCards: QuickCard[] = [
  { to: '/chat', titleKey: 'home.card.what', descKey: 'home.card.what.desc', icon: BookOpen, tint: 'bg-wb-sky/15 text-wb-info' },
  { to: '/chat', titleKey: 'home.card.genui', descKey: 'home.card.genui.desc', icon: LayoutTemplate, tint: 'bg-wb-lavender/15 text-wb-lavender' },
  { to: '/pet', titleKey: 'home.card.pet', descKey: 'home.card.pet.desc', icon: PawPrint, tint: 'bg-wb-warning/15 text-wb-warning' }
]

function go(to: string): void {
  void router.push(to)
}

function fmt(iso: string | number | null): string {
  return formatDateTime(iso)
}

// ===== 快速上手引导（Phase D · 首次访问显示） =====
const GUIDE_KEY = 'workbaby.guide.onboarded'
const showGuide = ref(false)

const guideSteps = [
  { to: '/settings?tab=models', titleKey: 'home.guide.step1', descKey: 'home.guide.step1Desc' },
  { to: '/chat', titleKey: 'home.guide.step2', descKey: 'home.guide.step2Desc' },
  { to: '/chat', titleKey: 'home.guide.step3', descKey: 'home.guide.step3Desc' }
]

function dismissGuide(): void {
  showGuide.value = false
  try {
    localStorage.setItem(GUIDE_KEY, '1')
  } catch {
    // ignore
  }
}

onMounted(() => {
  void home.load()
  try {
    showGuide.value = localStorage.getItem(GUIDE_KEY) !== '1'
  } catch {
    showGuide.value = true
  }
})
</script>

<template>
  <div class="h-full overflow-y-auto text-wb-ink">
    <div class="mx-auto flex min-h-full w-full max-w-3xl flex-col justify-center px-6 py-12">
      <!-- 欢迎区 -->
      <header class="text-center">
        <div class="mx-auto mb-5 flex h-16 w-16 items-center justify-center rounded-xl bg-wb-primary/10 text-wb-primary">
          <Sparkles class="h-8 w-8" />
        </div>
        <h1 class="text-2xl font-bold text-wb-ink">{{ t('home.welcome') }}</h1>
        <p class="mt-2 text-sm text-wb-muted">{{ t('home.subtitle') }}</p>
      </header>

      <!-- 快速上手引导（el-card + el-steps；首次访问显示，可关闭） -->
      <el-card v-if="showGuide" class="mt-8 home-guide" shadow="never">
        <template #header>
          <div class="flex items-center justify-between">
            <h2 class="flex items-center gap-2 text-sm font-semibold text-wb-ink">
              <Rocket class="h-4 w-4 text-wb-primary-strong" />
              {{ t('home.guide.title') }}
            </h2>
            <el-button link :title="t('home.guide.dismiss')" @click="dismissGuide">
              <X class="h-4 w-4" />
            </el-button>
          </div>
        </template>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <el-card
            v-for="(g, i) in guideSteps"
            :key="g.to + i"
            shadow="never"
            class="guide-step"
            @click="go(g.to)"
          >
            <div class="flex items-start gap-3">
              <span class="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-wb-primary/10 text-xs font-semibold text-wb-primary">
                {{ i + 1 }}
              </span>
              <span class="min-w-0">
                <span class="block text-sm font-medium text-wb-ink">{{ t(g.titleKey) }}</span>
                <span class="mt-0.5 block text-xs text-wb-muted">{{ t(g.descKey) }}</span>
              </span>
            </div>
          </el-card>
        </div>
      </el-card>

      <!-- 3 个快捷卡片 -->
      <div class="mt-10 grid grid-cols-1 gap-4 sm:grid-cols-3">
        <button
          v-for="c in quickCards"
          :key="c.to"
          type="button"
          class="group card text-left transition-all hover:-translate-y-0.5 hover:border-wb-primary/40 hover:shadow-[var(--wb-shadow-lg)]"
          @click="go(c.to)"
        >
          <div class="mb-3 flex h-10 w-10 items-center justify-center rounded-xl" :class="c.tint">
            <component :is="c.icon" class="h-5 w-5" />
          </div>
          <div class="text-sm font-semibold text-wb-ink">{{ t(c.titleKey) }}</div>
          <div class="mt-1 text-xs text-wb-muted">{{ t(c.descKey) }}</div>
        </button>
      </div>

      <!-- 最近会话 -->
      <section class="mt-10">
        <div class="mb-3 flex items-center justify-between">
          <h2 class="text-sm font-semibold text-wb-ink">{{ t('home.recentSessions') }}</h2>
          <el-button link type="primary" @click="go('/chat')">
            {{ t('home.openChat') }}
            <ArrowRight class="h-3.5 w-3.5" />
          </el-button>
        </div>

        <div class="overflow-hidden rounded-2xl border border-wb-border bg-wb-surface/80">
          <div v-if="loading" class="p-6 text-center text-sm text-wb-muted">{{ t('ui.status.loading') }}</div>
          <el-empty v-else-if="sessions.length === 0" :description="t('chat.noSessions')" :image-size="80" />
          <el-table
            v-else
            :data="sessions.slice(0, 6)"
            class="wb-el-table"
            @row-click="() => go('/chat')"
          >
            <el-table-column :label="t('home.recentSessions')" min-width="200">
              <template #default="{ row }">
                <span class="font-medium text-wb-ink">{{ (row as Session).name || t('task.unnamed') }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('home.lastActive')" min-width="160">
              <template #default="{ row }">
                <span class="text-xs text-wb-muted">{{ fmt((row as Session).last_message_at) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="ID" width="90">
              <template #default="{ row }">
                <span class="font-mono text-xs text-wb-muted">{{ (row as Session).id.slice(0, 8) }}</span>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
/* 引导卡：可点击 + hover 高亮 */
.guide-step {
  cursor: pointer;
  transition: all 0.2s ease;
}
.guide-step:hover {
  border-color: color-mix(in srgb, var(--wb-primary) 40%, transparent);
}
.home-guide :deep(.el-card__header) {
  border-bottom: 1px solid var(--wb-border);
}
</style>
