<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { Brain, Search, Trash2 } from '@/components/common/icons'
import { useMemoryStore } from '@/stores/memory'
import { t } from '@/i18n'
import type { MemoryEpisode } from '@/types/api'
import { formatDateTime } from '@/utils/time'

/**
 * 记忆中心（照 prd/WorkBaby-UI-Prototype.html 08 屏）：
 * hero + 统一召回（RRF 跨三类） + 三 tab：长期 delta / 语义 fact / 程序 procedure。
 * 视觉走 wb-ui 设计系统层。
 */
type MemoryTab = 'episodes' | 'facts' | 'procedures'

const memory = useMemoryStore()
const {
  episodes, total, loading, error,
  facts, procedures,
  recallResults, recallLoading
} = storeToRefs(memory)
const { load, loadFacts, loadProcedures, recall, doDelete } = memory

const activeTab = ref<MemoryTab>('episodes')
const recallQuery = ref('')

function switchTab(tab: string | number): void {
  const k = String(tab) as MemoryTab
  activeTab.value = k
  if (k === 'episodes' && episodes.value.length === 0) void load()
  if (k === 'facts' && facts.value.length === 0) void loadFacts()
  if (k === 'procedures' && procedures.value.length === 0) void loadProcedures()
}

function setPane(pane: MemoryTab): void {
  activeTab.value = pane
  switchTab(pane)
}

function doRecall(): void {
  if (!recallQuery.value.trim()) return
  void recall(recallQuery.value)
}

function fmtTime(iso: string | number | null): string {
  return formatDateTime(iso)
}

// 刷新当前 tab：episodes/facts/procedures 各自拉取（模板里通过键盘 / 后续页面复用）
function refreshActive(): void {
  if (activeTab.value === 'episodes') void load()
  else if (activeTab.value === 'facts') void loadFacts()
  else void loadProcedures()
}
// 仅保留供未来热键复用，避免 TS6133
void refreshActive

const tabButtons = computed(() => [
  { id: 'episodes' as MemoryTab, label: t('memory.center.tab.episodes'), count: episodes.value.length },
  { id: 'facts' as MemoryTab, label: t('memory.center.tab.facts'), count: facts.value.length },
  { id: 'procedures' as MemoryTab, label: t('memory.center.tab.procedures'), count: procedures.value.length }
])

function badgeKindClass(kind: string): string {
  switch (kind) {
    case 'episode': return 'b-primary'
    case 'fact': return 'b-info'
    case 'procedure': return 'b-warning'
    default: return 'b-neutral'
  }
}

onMounted(() => {
  void load()
})
</script>

<template>
  <div class="scroll wb-ui">
    <div class="wrap wrap-lg">
      <!-- Hero -->
      <header class="hero">
        <div class="tile"><Brain class="ic" /></div>
        <div>
          <h1>{{ t('memory.center.title') }}</h1>
          <p>{{ t('memory.center.subtitle') }}</p>
        </div>
        <span class="sp" />
        <span class="badge b-neutral">{{ t('memory.center.total', total) }}</span>
      </header>

      <div v-if="error" class="alert a-danger">{{ error }}</div>

      <!-- 统一召回 -->
      <div class="card">
        <div class="sec-title mb10">
          <Search class="ic" />{{ t('memory.center.recall') }}
        </div>
        <div class="flex-r mb10">
          <div class="field-wrap" style="flex: 1">
            <Search class="ic ic-sm" />
            <input v-model="recallQuery" class="input with-icon" :placeholder="t('memory.center.recallPlaceholder')" @keyup.enter="doRecall" />
          </div>
          <button class="btn btn-primary" :disabled="recallLoading" @click="doRecall">{{ t('memory.center.recallBtn') }}</button>
        </div>
        <div v-if="recallResults.length > 0" class="rowlist" style="border-top: 1px solid var(--wb-border)">
          <div v-for="(r, idx) in recallResults" :key="`${r.kind}-${idx}`" class="rli">
            <span class="badge" :class="badgeKindClass(r.kind)">{{ t(`memory.center.kind.${r.kind}`) }}</span>
            <div class="grow">
              <h5>{{ r.title }}</h5>
              <p>{{ r.snippet || r.source }}</p>
            </div>
            <span class="badge b-neutral">{{ r.score.toFixed(2) }}</span>
          </div>
        </div>
      </div>

      <!-- 三 tab -->
      <div class="flex-r">
        <div class="seg">
          <button v-for="b in tabButtons" :key="b.id" :class="{ on: activeTab === b.id }" @click="setPane(b.id)">
            {{ b.label }} · {{ b.count }}
          </button>
        </div>
      </div>

      <div v-show="activeTab === 'episodes'" class="card p-sm">
        <div class="tbl-wrap">
          <table class="tbl">
            <thead>
              <tr>
                <th style="min-width: 260px">{{ t('memory.center.col.summary') }}</th>
                <th>{{ t('memory.center.col.source') }}</th>
                <th>{{ t('memory.center.col.importance') }}</th>
                <th class="ta-r">{{ t('memory.center.col.time') }}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              <tr v-if="loading"><td colspan="5" class="empty">{{ t('ui.status.loading') }}</td></tr>
              <tr v-else-if="episodes.length === 0"><td colspan="5" class="empty">{{ t('memory.center.emptyEpisodes') }}</td></tr>
              <tr v-for="e in episodes" v-else :key="e.id">
                <td>{{ e.summary }}</td>
                <td class="muted">{{ (e as Record<string, unknown>).source as string }}</td>
                <td class="mono">{{ (((e as Record<string, unknown>).importance as number) ?? 0).toFixed(2) }}</td>
                <td class="ta-r mono">{{ fmtTime(e.created_at) }}</td>
                <td>
                  <div class="tbl-actions">
                    <button class="btn-icon" style="color: var(--wb-danger)" :title="t('memory.center.col.actions')" @click="doDelete(e as MemoryEpisode)">
                      <Trash2 class="ic ic-sm" />
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div v-show="activeTab === 'facts'" class="card p-sm">
        <div class="tbl-wrap">
          <table class="tbl">
            <thead>
              <tr>
                <th>Subject</th>
                <th>Key</th>
                <th>Value</th>
                <th class="ta-r">{{ t('memory.center.col.importance') }}</th>
                <th>{{ t('memory.center.col.source') }}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              <tr v-if="facts.length === 0"><td colspan="6" class="empty">{{ t('memory.center.emptyFacts') }}</td></tr>
              <tr v-for="f in facts" v-else :key="f.key">
                <td class="mono">{{ f.subject }}</td>
                <td class="mono">{{ f.key }}</td>
                <td>{{ f.value }}</td>
                <td class="ta-r mono">{{ f.confidence?.toFixed(2) }}</td>
                <td class="muted">{{ f.source }}</td>
                <td>
                  <div class="tbl-actions">
                    <button class="btn btn-sm">{{ t('memory.center.edit') }}</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div v-show="activeTab === 'procedures'" class="card p-sm">
        <div class="tbl-wrap">
          <table class="tbl">
            <thead>
              <tr>
                <th style="min-width: 180px">{{ t('memory.center.col.name') }}</th>
                <th>{{ t('memory.center.col.signature') }}</th>
                <th>{{ t('memory.center.col.source') }}</th>
                <th class="ta-r">{{ t('memory.center.col.time') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="procedures.length === 0"><td colspan="4" class="empty">{{ t('memory.center.emptyProcedures') }}</td></tr>
              <tr v-for="p in procedures" v-else :key="p.id">
                <td style="font-weight: 500">{{ p.name }}</td>
                <td class="mono muted">{{ (p as Record<string, unknown>).signature as string || (p.steps ?? []).join(' → ') }}</td>
                <td class="muted">{{ (p as Record<string, unknown>).source as string || '—' }}</td>
                <td class="ta-r mono">{{ fmtTime(p.last_used_at ?? p.created_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>
