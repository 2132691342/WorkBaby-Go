<script setup lang="ts">
import { onMounted } from 'vue'
import { storeToRefs } from 'pinia'
import { FileText } from '@/components/common/icons'
import { useFilesStore } from '@/stores/files'
import { t } from '@/i18n'
import type { FileInfo } from '@/types/api'
import { formatDate } from '@/utils/time'

/**
 * 文件管理视图。
 *
 * <p>el-upload 上传 + el-table 列表；MIME 类型用 el-tag 染色。
 */
const filesStore = useFilesStore()
const { files, error, loading, uploading } = storeToRefs(filesStore)
const { load, upload, remove } = filesStore

function fmtSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

/** MIME → tag 类型（前端快速分类）。 */
function typeTag(type: string): 'success' | 'warning' | 'danger' | 'info' | 'primary' {
  if (type?.startsWith('image/')) return 'success'
  if (type?.startsWith('video/') || type?.startsWith('audio/')) return 'warning'
  if (type?.includes('pdf') || type?.includes('word') || type?.includes('excel')) return 'danger'
  if (type?.includes('json') || type?.includes('xml') || type?.includes('text')) return 'info'
  return 'primary'
}

function onFileChange(file: { raw?: File }): void {
  void handleUpload(file.raw)
}
async function handleUpload(file: File | undefined): Promise<void> {
  if (!file) return
  await upload(file)
}

onMounted(load)
</script>

<template>
  <div class="flex h-full flex-col overflow-y-auto text-wb-ink">
    <div class="mx-auto w-full max-w-3xl space-y-5 px-6 py-8">
      <!-- Hero header -->
      <header class="flex items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-wb-primary/10 text-wb-primary">
          <FileText class="h-5 w-5" />
        </div>
        <div>
          <h1 class="font-display text-lg font-semibold text-wb-ink">{{ t('file.title') }}</h1>
          <p class="text-xs text-wb-muted">{{ t('file.subtitle') }}</p>
        </div>
      </header>

      <!-- 顶部操作栏 -->
      <section class="card flex items-center justify-between p-4">
        <div class="text-sm text-wb-muted">
          {{ files.length }} {{ t('file.items') }}
        </div>
        <el-upload
          :show-file-list="false"
          :auto-upload="false"
          :disabled="uploading"
          @change="onFileChange"
        >
          <el-button type="primary" :loading="uploading">
            {{ uploading ? t('file.uploading') : t('file.upload') }}
          </el-button>
        </el-upload>
      </section>

      <div v-if="error" class="rounded-lg bg-wb-danger/15 px-3 py-2 text-sm text-wb-danger">
        {{ error }}
      </div>

      <!-- 列表（el-table） -->
      <div v-if="loading" class="text-sm text-wb-muted">{{ t('ui.status.loading') }}</div>
      <el-table v-else-if="files.length > 0" :data="files" stripe class="wb-el-table">
        <el-table-column :label="t('file.name')" min-width="240">
          <template #default="{ row }">
            <span class="font-medium text-wb-ink">{{ (row as FileInfo).original_name }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('file.type')" width="110">
          <template #default="{ row }">
            <el-tag size="small" :type="typeTag((row as FileInfo).file_type)" effect="plain">
              {{ (row as FileInfo).file_type || 'unknown' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('file.size')" width="90">
          <template #default="{ row }">
            <span class="text-xs text-wb-muted">{{ fmtSize((row as FileInfo).size) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('memory.center.col.time')" width="110">
          <template #default="{ row }">
            <span class="text-xs text-wb-muted">{{ formatDate((row as FileInfo).created_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('memory.center.col.actions')" width="70" fixed="right">
          <template #default="{ row }">
            <el-button link type="danger" size="small" @click="remove((row as FileInfo).id)">{{ t('ui.btn.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-else :description="t('file.empty')" :image-size="80" class="py-8" />
    </div>
  </div>
</template>

