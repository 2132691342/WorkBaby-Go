<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { BookOpen, FolderOpen, FileText, Upload } from '@/components/common/icons'
import { OpenFileDialog } from '@/wailsjs/go/main/App'
import { apiGet, apiPost } from '@/api/client'
import { useKnowledgeDocsStore } from '@/stores/kdocs'
import { t } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { useToast } from '@/composables/useToast'
import type { KnowledgeDoc } from '@/types/api'

/**
 * 知识文档库。
 *
 * <p>文档 = 知识库条目（name + source + source_type + 索引状态）。
 * 注意：顶部 header / 工具栏始终渲染，避免初次加载或异常时整页空白（截图复现）。
 */
const kdocs = useKnowledgeDocsStore()
const { docs, groups, filterGroup, search, error, showCreate, form, editing } = storeToRefs(kdocs)
const { load, doSave, startEdit, doDelete } = kdocs

/** source_type 选项（中英 i18n 双语，避免硬编码英文给中文用户）。 */
const SOURCE_TYPES = computed(() => [
  { value: 'text', label: t('kdoc.sourceType.text') },
  { value: 'file', label: t('kdoc.sourceType.file') },
  { value: 'url', label: t('kdoc.sourceType.url') }
])

/** 初始加载与失败重试按钮共用。 */
const loading = ref(false)
async function reload(): Promise<void> {
  loading.value = true
  try {
    await load()
  } finally {
    loading.value = false
  }
}

onMounted(reload)

/** el-dialog 关闭。 */
function closeDialog(): void {
  showCreate.value = false
  editing.value = null
}

/** 打开新建对话框。 */
function openCreate(): void {
  openCreateWithType('text')
}

/** 按 source 类型打开新建对话框（空状态卡片点击）。 */
function openCreateWithType(sourceType: 'text' | 'file' | 'url'): void {
  editing.value = null
  form.value = { name: '', source: '', source_type: sourceType }
  showCreate.value = true
}

const toast = useToast()

// ===== 查看详情：列表只有增删改，用户需要不进入编辑即可核对文档元数据 =====
const viewing = ref<KnowledgeDoc | null>(null)
function openView(row: KnowledgeDoc): void {
  viewing.value = row
}

/** 从文件取 basename（去扩展名），作为文档默认名。 */
function basenameNoExt(p: string): string {
  const seg = p.replace(/\\/g, '/').split('/').pop() ?? ''
  return seg.replace(/\.[^.]+$/, '') || ''
}

/** 列表「来源」列展示：file 型只显示文件名（源文件已受管复制到知识库目录，不向用户暴露磁盘路径）。
 *  text/url 仍显示原文/URL，便于识别。 */
function displaySource(d: KnowledgeDoc): string {
  if (d.source_type === 'file') {
    return d.source.replace(/\\/g, '/').split('/').pop() ?? d.source
  }
  return d.source
}

/** file 型文档：原生文件对话框选路径 → 回填 source，免手动粘贴路径（痛点修复）。 */
async function pickSourceFile(): Promise<void> {
  try {
    const selected = await OpenFileDialog(t('kdoc.pickFileTitle'), DOC_FILTER)
    if (!selected) return
    form.value.source = selected
    if (!form.value.name?.trim()) {
      form.value.name = basenameNoExt(selected)
    }
  } catch (e) {
    toast.error(t('kdoc.pickFileFailed'), e instanceof Error ? e.message : String(e))
  }
}

// ===== 导入文件到受管知识库（后端复制进 {home}/knowledge 并后台索引）=====
const importingFile = ref(false)
/** 导入进度文案：上传中 → 后台索引中… */
const importStatus = ref('')

/** 与后端 managedDocExts 白名单一致：pdf / html / txt / json / yaml / xml / md / csv */
const DOC_FILTER = '*.pdf;*.html;*.htm;*.txt;*.md;*.markdown;*.csv;*.json;*.yaml;*.yml;*.xml'

/** 导入 + 状态轮询：完成后自动刷新列表。 */
async function importManagedFile(): Promise<void> {
  try {
    const selected = await OpenFileDialog(t('kdoc.pickFileTitle'), DOC_FILTER)
    if (!selected) return
    importingFile.value = true
    importStatus.value = t('kdoc.importCopying')
    const doc = await apiPost<KnowledgeDoc>('/api/v1/kdocs/import-file', {
      name: basenameNoExt(selected),
      source_path: selected,
      folder_id: null
    })
    toast.success(t('kdoc.importDone', doc?.name ?? basenameNoExt(selected)))
    importStatus.value = t('kdoc.importIndexing')
    await reload()
    // 后台索引状态轮询（pending/parsing → indexed/failed 后刷新一次）
    const id = doc?.id
    if (!id) return
    const start = Date.now()
    let last = doc.status ?? 'pending'
    while ((last === 'pending' || last === 'parsing') && Date.now() - start < 90_000) {
      await new Promise((r) => setTimeout(r, 1200))
      try {
        const cur = await apiGet<KnowledgeDoc>(`/api/v1/kdocs/${id}`)
        if (cur && cur.status !== last) {
          last = cur.status
          if (cur.status === 'indexed') {
            importStatus.value = t('kdoc.importIndexed', cur.chunk_count)
            toast.success(t('kdoc.importIndexed', cur.chunk_count))
          } else if (cur.status === 'failed') {
            importStatus.value = ''
            toast.error(t('kdoc.importFailed'), cur.error_msg || '')
          }
          await reload()
        }
      } catch {
        /* 单次查询失败不中断轮询 */
      }
    }
  } catch (e) {
    toast.error(t('kdoc.importError'), e instanceof Error ? e.message : String(e))
  } finally {
    importingFile.value = false
    importStatus.value = ''
  }
}
</script>

<template>
  <div class="flex h-full flex-col overflow-y-auto text-wb-ink">
    <div class="mx-auto w-full max-w-5xl space-y-5 px-6 py-8">
      <header class="flex items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-wb-primary/10 text-wb-primary">
          <BookOpen class="h-5 w-5" />
        </div>
        <div>
          <h1 class="font-display text-lg font-semibold text-wb-ink">{{ t('kdoc.title') }}</h1>
          <p class="text-xs text-wb-muted">{{ t('kdoc.count', docs.length, groups.length) }}</p>
        </div>
        <div class="ml-auto flex items-center gap-2">
          <span v-if="importStatus" class="inline-flex items-center gap-1 text-xs text-wb-muted">
            <span class="h-1.5 w-1.5 animate-pulse rounded-full bg-wb-primary" />
            {{ importStatus }}
          </span>
          <el-button :loading="importingFile" @click="importManagedFile">
            <Upload class="h-3.5 w-3.5" />
            <span class="ml-1">{{ t('kdoc.importFile') }}</span>
          </el-button>
          <el-button type="primary" @click="openCreate">{{ t('kdoc.newDoc') }}</el-button>
        </div>
      </header>

      <div class="flex flex-wrap items-center gap-3">
        <el-input
          v-model="search"
          :placeholder="t('kdoc.searchPlaceholder')"
          clearable
          class="!w-64"
          @keyup.enter="reload"
        />
        <el-select v-model="filterGroup" :placeholder="t('kdoc.allGroups')" clearable class="!w-44" @change="reload">
          <el-option v-for="g in groups" :key="g" :value="g" :label="g" />
        </el-select>
        <el-button @click="reload">{{ t('knowledge.search') }}</el-button>
      </div>

      <div v-if="error" class="rounded-lg bg-wb-danger/15 px-3 py-2 text-sm text-wb-danger">
        {{ error }}
        <el-button link type="primary" class="ml-2" @click="reload">{{ t('common.retry') }}</el-button>
      </div>

      <div v-if="loading && docs.length === 0" class="flex items-center gap-2 text-sm text-wb-muted">
        <span class="inline-block h-2 w-2 animate-pulse rounded-full bg-wb-primary" />
        {{ t('ui.status.loading') }}
      </div>

      <el-table v-else-if="docs.length > 0" :data="docs" stripe class="wb-el-table">
        <el-table-column :label="t('kdoc.titleField')" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">
            <span class="font-medium text-wb-ink">{{ row.name }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('kdoc.sourceTypeLabel')" width="120">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ row.source_type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('kdoc.sourceField')" min-width="240" show-overflow-tooltip>
          <template #default="{ row }">
            <span class="text-xs text-wb-muted">{{ displaySource(row as KnowledgeDoc) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('kdoc.chunkCount')" width="80" align="center">
          <template #default="{ row }">
            <span class="text-xs text-wb-muted">{{ row.chunk_count }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('kdoc.statusLabel')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'indexed' ? 'success' : row.status === 'failed' ? 'danger' : 'info'" size="small">
              {{ t(`kdoc.status.${row.status || 'pending'}`) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('memory.center.col.time')" width="160">
          <template #default="{ row }">
            <span class="text-xs text-wb-muted">{{ formatDateTime(row.updated_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('memory.center.col.actions')" width="170" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openView(row)">{{ t('kdoc.viewDoc') }}</el-button>
            <el-button link type="primary" size="small" @click="startEdit(row)">{{ t('ui.btn.edit') }}</el-button>
            <el-button link type="danger" size="small" @click="doDelete(row)">{{ t('ui.btn.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 空状态：明确的引导卡，附带 3 种 source 的说明 + 主按钮 -->
      <section v-else class="card flex flex-col items-center px-6 py-10 text-center">
        <div class="flex h-14 w-14 items-center justify-center rounded-2xl bg-wb-primary/10 text-wb-primary">
          <BookOpen class="h-7 w-7" />
        </div>
        <h3 class="mt-4 font-display text-base font-semibold text-wb-ink">{{ t('kdoc.emptyTitle') }}</h3>
        <p class="mt-1 max-w-md text-xs text-wb-muted">{{ t('kdoc.emptyHint') }}</p>
        <div class="mt-6 grid w-full max-w-2xl grid-cols-1 gap-3 sm:grid-cols-3">
          <button
            type="button"
            class="rounded-xl border border-wb-border bg-wb-surface-2 px-4 py-3 text-left transition-colors hover:border-wb-primary/40 hover:bg-wb-surface"
            @click="openCreateWithType('text')"
          >
            <p class="text-xs font-semibold text-wb-primary-strong">{{ t('kdoc.sourceType.text') }}</p>
            <p class="mt-1 text-[11px] leading-relaxed text-wb-muted">{{ t('kdoc.emptyHintText') }}</p>
          </button>
          <button
            type="button"
            class="rounded-xl border border-wb-border bg-wb-surface-2 px-4 py-3 text-left transition-colors hover:border-wb-primary/40 hover:bg-wb-surface"
            @click="openCreateWithType('file')"
          >
            <p class="text-xs font-semibold text-wb-primary-strong">{{ t('kdoc.sourceType.file') }}</p>
            <p class="mt-1 text-[11px] leading-relaxed text-wb-muted">{{ t('kdoc.emptyHintFile') }}</p>
          </button>
          <button
            type="button"
            class="rounded-xl border border-wb-border bg-wb-surface-2 px-4 py-3 text-left transition-colors hover:border-wb-primary/40 hover:bg-wb-surface"
            @click="openCreateWithType('url')"
          >
            <p class="text-xs font-semibold text-wb-primary-strong">{{ t('kdoc.sourceType.url') }}</p>
            <p class="mt-1 text-[11px] leading-relaxed text-wb-muted">{{ t('kdoc.emptyHintUrl') }}</p>
          </button>
        </div>
        <el-button type="primary" class="mt-6" @click="openCreate">{{ t('kdoc.newDoc') }}</el-button>
      </section>
    </div>

    <el-dialog
      :model-value="showCreate"
      :title="editing ? t('kdoc.editDoc') : t('kdoc.newDocTitle')"
      width="560px"
      append-to-body
      @update:model-value="(v: boolean) => (!v ? closeDialog() : undefined)"
    >
      <el-form class="wb-el-form wb-form-grid" label-position="top" @submit.prevent="doSave">
        <el-form-item :label="t('kdoc.titleField')" class="wb-form-cell">
          <el-input v-model="form.name" :placeholder="t('kdoc.titleField')" clearable />
        </el-form-item>
        <el-form-item :label="t('kdoc.sourceTypeLabel')" class="wb-form-cell">
          <el-select v-model="form.source_type" class="w-full">
            <el-option v-for="st in SOURCE_TYPES" :key="st.value" :value="st.value" :label="st.label" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('kdoc.sourceField')" class="wb-form-cell">
          <!-- file 型：原生「选择文件」对话框，免手动粘贴路径 -->
          <div v-if="form.source_type === 'file'" class="mb-2 flex w-full items-center gap-2">
            <el-button size="small" type="primary" plain @click="pickSourceFile">
              <el-icon class="mr-1"><FolderOpen /></el-icon>
              {{ t('kdoc.pickFile') }}
            </el-button>
            <span v-if="form.source" class="inline-flex min-w-0 items-center gap-1 truncate rounded bg-wb-primary/10 px-2 py-1 text-xs text-wb-primary-strong">
              <FileText class="h-3 w-3 shrink-0" />
              <span class="truncate">{{ form.source }}</span>
            </span>
          </div>
          <el-input
            v-model="form.source"
            type="textarea"
            :rows="form.source_type === 'file' ? 3 : 8"
            :placeholder="form.source_type === 'file' ? t('kdoc.sourcePlaceholderFile') : t('kdoc.sourcePlaceholder')"
            clearable
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="closeDialog">{{ t('ui.btn.cancel') }}</el-button>
        <el-button type="primary" @click="doSave">{{ editing ? t('ui.btn.save') : t('ui.btn.create') }}</el-button>
      </template>
    </el-dialog>

    <!-- 文档详情抽屉：只读元数据核对（状态/分块/来源/失败原因） -->
    <el-drawer
      :model-value="viewing !== null"
      :title="viewing?.name ?? ''"
      size="420px"
      append-to-body
      @update:model-value="(v: boolean) => (!v ? (viewing = null) : undefined)"
    >
      <div v-if="viewing" class="flex flex-col gap-3 text-sm">
        <div class="flex items-center gap-2">
          <el-tag :type="viewing.status === 'indexed' ? 'success' : viewing.status === 'failed' ? 'danger' : 'info'" size="small">
            {{ t(`kdoc.status.${viewing.status || 'pending'}`) }}
          </el-tag>
          <el-tag size="small" effect="plain">{{ viewing.source_type }}</el-tag>
        </div>
        <div class="flex items-center justify-between border-b border-wb-border pb-2">
          <span class="text-xs text-wb-muted">{{ t('kdoc.chunkCount') }}</span>
          <span class="text-wb-ink">{{ viewing.chunk_count }}</span>
        </div>
        <div class="border-b border-wb-border pb-2">
          <div class="mb-1 text-xs text-wb-muted">{{ t('kdoc.sourceField') }}</div>
          <div class="break-all font-mono text-xs text-wb-ink">{{ viewing.source }}</div>
        </div>
        <div v-if="viewing.error_msg" class="rounded-lg bg-wb-danger/10 px-3 py-2 text-xs text-wb-danger">
          {{ viewing.error_msg }}
        </div>
        <div class="text-xs text-wb-muted">{{ t('kdoc.viewUpdatedAt') }}：{{ formatDateTime(viewing.updated_at) }}</div>
      </div>
    </el-drawer>
  </div>
</template>
