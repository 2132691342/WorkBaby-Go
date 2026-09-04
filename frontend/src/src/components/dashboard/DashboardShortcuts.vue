<script setup lang="ts">
import { useRouter } from 'vue-router'
import {
  Brain, Film, Clock, ArrowRight, MessageSquare, GitBranch, Zap
} from '@/components/common/icons'
import { t } from '@/i18n'

/**
 * 仪表盘 · Bento 快捷入口。
 */
const router = useRouter()

const shortcuts = [
  {
    to: '/chat',
    titleKey: 'dashboard.go.chat',
    descKey: 'dashboard.go.chatDesc',
    icon: MessageSquare,
    tint: 'bg-wb-primary text-white',
    featured: true
  },
  {
    to: '/workflows',
    titleKey: 'dashboard.go.workflow',
    descKey: 'dashboard.go.workflowDesc',
    icon: GitBranch,
    tint: 'bg-wb-sky/15 text-wb-info',
    featured: false
  },
  {
    to: '/kdocs',
    titleKey: 'dashboard.go.kdocs',
    descKey: 'dashboard.go.kdocsDesc',
    icon: Brain,
    tint: 'bg-wb-lavender/15 text-wb-lavender',
    featured: false
  },
  {
    to: '/media',
    titleKey: 'dashboard.go.media',
    descKey: 'dashboard.go.mediaDesc',
    icon: Film,
    tint: 'bg-wb-warning/15 text-wb-warning',
    featured: false
  },
  {
    to: '/cron',
    titleKey: 'dashboard.go.cron',
    descKey: 'dashboard.go.cronDesc',
    icon: Clock,
    tint: 'bg-wb-success/15 text-wb-success',
    featured: false
  }
]
</script>

<template>
  <section class="card p-5">
    <h2 class="mb-3 flex items-center gap-2 font-display text-sm font-semibold text-wb-ink">
      <Zap class="h-4 w-4 text-wb-warning" />
      {{ t('dashboard.shortcuts') }}
    </h2>
    <div class="grid grid-cols-2 gap-3">
      <button
        v-for="s in shortcuts"
        :key="s.to"
        type="button"
        class="group flex flex-col items-start gap-2 rounded-xl border border-wb-border bg-wb-surface/60 p-3 text-left transition-all hover:-translate-y-0.5 hover:border-wb-primary/40 hover:shadow-[var(--wb-shadow-lg)]"
        :class="s.featured ? 'col-span-2 p-4' : 'col-span-1'"
        @click="void router.push(s.to)"
      >
        <div class="flex h-8 w-8 items-center justify-center rounded-lg" :class="s.tint">
          <component :is="s.icon" class="h-4 w-4" />
        </div>
        <div class="min-w-0">
          <div class="text-sm font-semibold text-wb-ink">{{ t(s.titleKey) }}</div>
          <div class="text-[11px] text-wb-muted">{{ t(s.descKey) }}</div>
        </div>
        <ArrowRight class="h-3.5 w-3.5 text-wb-muted transition-transform group-hover:translate-x-0.5 group-hover:text-wb-primary" />
      </button>
    </div>
  </section>
</template>
