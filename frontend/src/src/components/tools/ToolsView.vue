<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useToast } from '@/composables/useToast'
import { apiGet } from '@/api/client'
import { Wrench } from '@/components/common/icons'
import { t } from '@/i18n'

/**
 * 工具清单。
 *
 * <p>调 {@code GET /api/v1/tools} 反射 ToolRegistry @Tool 注解，前端按 group 分组渲染（el-table）。
 * 文案走 i18n 字典 {@code tool.group.*} / {@code tool.desc.*}，找不到则用 description 兜底。
 */
interface ToolParam {
  name: string
  type: string
  required: boolean
  description: string
}
interface ToolItem {
  name: string
  description: string
  group: string
  strict: boolean
  readOnly: boolean
  holderClass: string
  params: ToolParam[]
}

const toast = useToast()
const tools = ref<ToolItem[]>([])
const loading = ref(false)
const search = ref('')

async function load(): Promise<void> {
  loading.value = true
  try {
    tools.value = await apiGet<ToolItem[]>('/api/v1/tools')
  } catch (e) {
    toast.error(e instanceof Error ? e.message : String(e))
    tools.value = []
  } finally {
    loading.value = false
  }
}

onMounted(load)

/** 按 group 分组。 */
const groups = computed(() => {
  const map = new Map<string, ToolItem[]>()
  for (const it of tools.value) {
    if (!map.has(it.group)) map.set(it.group, [])
    map.get(it.group)!.push(it)
  }
  return Array.from(map.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([name, items]) => ({ name, items }))
})

const total = computed(() => tools.value.length)

function filtered(items: ToolItem[]): ToolItem[] {
  const q = search.value.trim().toLowerCase()
  if (!q) return items
  return items.filter((it) =>
    it.name.toLowerCase().includes(q) ||
    (it.description ?? '').toLowerCase().includes(q) ||
    (it.holderClass ?? '').toLowerCase().includes(q)
  )
}

/** group 名 → i18n key，找不到用原始名。 */
function groupLabel(name: string): string {
  const key = `tool.group.${name}`
  const v = t(key)
  return v === key ? name : v
}
</script>

<template>
  <div class="flex h-full flex-col overflow-y-auto text-wb-ink">
    <div class="mx-auto w-full max-w-4xl space-y-5 px-6 py-8">
      <header class="flex items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-wb-primary/10 text-wb-primary">
          <Wrench class="h-5 w-5" />
        </div>
        <div class="min-w-0">
          <h1 class="font-display text-lg font-semibold text-wb-ink">{{ t('tools.title') }}</h1>
          <p class="text-xs text-wb-muted">
            {{ t('tools.subtitle', total) }}
            <span v-if="search" class="ml-1 text-wb-primary">
              · {{ t('tools.filteredCount', filtered(tools).length) }}
            </span>
          </p>
        </div>
        <el-input v-model="search" :placeholder="t('tools.search')" clearable class="ml-auto !w-64" />
      </header>

      <div v-if="loading" class="text-sm text-wb-muted">{{ t('ui.status.loading') }}</div>
      <el-empty v-else-if="tools.length === 0" :description="t('tools.empty')" :image-size="80" class="py-8" />
      <section
        v-for="g in groups"
        v-else
        :key="g.name"
        class="card p-5"
      >
        <h2 class="mb-3 flex items-center gap-2 font-display text-sm font-semibold text-wb-ink">
          {{ groupLabel(g.name) }}
          <span class="text-xs font-normal tabular-nums text-wb-muted">
            {{ filtered(g.items).length }} / {{ g.items.length }}
          </span>
        </h2>
        <el-table v-if="filtered(g.items).length > 0" :data="filtered(g.items)" stripe class="wb-el-table">
          <el-table-column :label="t('tools.name')" width="180">
            <template #default="{ row }">
              <code class="font-mono text-xs text-wb-success">{{ (row as ToolItem).name }}</code>
            </template>
          </el-table-column>
          <el-table-column :label="t('tools.desc')" min-width="280" show-overflow-tooltip>
            <template #default="{ row }">
              <span class="text-xs text-wb-muted">{{ (row as ToolItem).description || t('tools.noDesc') }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('tools.permission')" width="90">
            <template #default="{ row }">
              <el-tag size="small" :type="(row as ToolItem).readOnly ? 'info' : 'warning'" effect="plain">
                {{ (row as ToolItem).readOnly ? 'R' : 'W' }}
              </el-tag>
            </template>
          </el-table-column>
        </el-table>
      </section>
    </div>
  </div>
</template>

