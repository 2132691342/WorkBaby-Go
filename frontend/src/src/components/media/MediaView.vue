<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useMediaStore } from '@/stores/media'
import { Film } from '@/components/common/icons'
import { t } from '@/i18n'
import { formatDateTime } from '@/utils/time'

/**
 * 媒体管线（D.续 Media · 卡片视觉版）。
 *
 * <p>三栏布局 + 卡片化产物：
 * <ul>
 *   <li>左：Presets 列表</li>
 *   <li>中：产物画廊（hover 放大 + 类型徽章 + 操作按钮组）</li>
 *   <li>右：生成面板（hero）</li>
 * </ul>
 */
const media = useMediaStore()
const { presets, artifacts, filter, error, generating, prompt, showCreate, form } = storeToRefs(media)
const { load, doCreate, doDelete, doActivate, doGenerate, deleteArtifact } = media

/** 当前预览的产物（modal）。 */
const previewArtifact = ref<typeof artifacts.value[number] | null>(null)
function openPreview(a: typeof artifacts.value[number]): void {
  previewArtifact.value = a
}
function closePreview(): void {
  previewArtifact.value = null
}
function onPreviewKey(e: KeyboardEvent): void {
  if (e.key === 'Escape') closePreview()
}

const kindEmoji = (k: string): string => {
  switch (k) {
    case 'image': return '🖼️'
    case 'video': return '🎬'
    case 'audio': return '🎵'
    case 'model3d': return '🧊'
    case 'vfx': return '✨'
    default: return '📄'
  }
}
const kindColor = (k: string): string => {
  switch (k) {
    case 'image': return 'bg-wb-sky/10 text-wb-sky border-wb-sky/40'
    case 'video': return 'bg-wb-lavender/10 text-wb-lavender border-wb-lavender/40'
    case 'audio': return 'bg-wb-primary/10 text-wb-primary-strong border-wb-mint/40'
    case 'model3d': return 'bg-wb-warning/10 text-wb-warning border-wb-warning/40'
    case 'vfx': return 'bg-wb-danger/10 text-wb-danger border-wb-danger/40'
    default: return 'bg-wb-surface text-wb-ink border-wb-border'
  }
}

const groupedArtifacts = computed(() => {
  const map = new Map<string, typeof artifacts.value>()
  for (const a of artifacts.value) {
    const list = map.get(a.kind) ?? []
    list.push(a)
    map.set(a.kind, list)
  }
  return Array.from(map.entries()).sort(([a], [b]) => a.localeCompare(b))
})

onMounted(() => {
  load()
  window.addEventListener('keydown', onPreviewKey)
})
onUnmounted(() => window.removeEventListener('keydown', onPreviewKey))
</script>

<template>
  <div class="flex h-full flex-col overflow-hidden bg-wb-bg text-wb-ink">
    <!-- Hero header -->
    <header class="flex items-center gap-3 border-b border-wb-border px-6 py-3">
      <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-wb-primary/10 text-wb-primary">
        <Film class="h-5 w-5" />
      </div>
      <div>
        <h1 class="font-display text-lg font-semibold text-wb-ink">{{ t('media.title') }}</h1>
        <div class="flex items-center gap-2 text-xs text-wb-muted">
          <span>{{ t('media.presetsCount', presets.length) }}</span>
          <span class="text-wb-muted">·</span>
          <span>{{ t('media.artifactsCount', artifacts.length) }}</span>
        </div>
      </div>
      <el-select
        v-model="filter"
        clearable
        :placeholder="t('media.allKinds')"
        class="ml-auto !w-32"
        @change="load"
      >
        <el-option value="image" label="image" />
        <el-option value="video" label="video" />
        <el-option value="audio" label="audio" />
        <el-option value="model3d" label="model3d" />
        <el-option value="vfx" label="vfx" />
      </el-select>
      <el-button type="primary" @click="showCreate = true">{{ t('media.newPreset') }}</el-button>
    </header>

    <div v-if="error" class="border-b border-wb-border bg-wb-danger/10 px-6 py-2 text-sm text-wb-danger">{{ error }}</div>

    <div class="grid min-h-0 flex-1 grid-cols-12 gap-4 overflow-hidden p-4">
      <!-- 左：Presets -->
      <aside class="card col-span-3 overflow-y-auto">
        <h3 class="mb-3 text-xs font-semibold uppercase tracking-wider text-wb-muted">{{ t('media.presets') }}</h3>
        <div v-if="presets.length === 0" class="rounded-lg border border-dashed border-wb-border p-4 text-center text-xs text-wb-muted">
          {{ t('media.noPresets') }}
        </div>
        <ul class="space-y-2">
          <li v-for="p in presets" :key="p.id" class="rounded-xl border border-wb-border bg-wb-surface-2 p-3 text-xs">
            <div class="flex items-center gap-2">
              <span class="text-base">{{ kindEmoji(p.kind) }}</span>
              <div class="min-w-0 flex-1">
                <div class="truncate font-medium text-wb-ink">{{ p.name }}</div>
                <div class="truncate text-wb-muted">{{ p.kind }} · {{ p.backend }}</div>
              </div>
            </div>
            <div class="mt-2 flex items-center justify-between">
              <el-button v-if="!p.is_default" size="small" type="success" @click="doActivate(p)">{{ t('media.activate') }}</el-button>
              <el-tag v-else size="small" type="success" effect="plain">{{ t('media.default') }}</el-tag>
              <el-button link type="danger" size="small" @click="doDelete(p)">{{ t('ui.btn.delete') }}</el-button>
            </div>
          </li>
        </ul>
      </aside>

      <!-- 中：产物画廊 -->
      <section class="card col-span-6 overflow-y-auto">
        <h3 class="mb-3 text-xs font-semibold uppercase tracking-wider text-wb-muted">{{ t('media.gallery') }}</h3>
        <div v-if="artifacts.length === 0" class="rounded-lg border border-dashed border-wb-border p-12 text-center text-sm text-wb-muted">
          {{ t('media.noArtifacts') }}
        </div>
        <div v-else class="space-y-6">
          <div v-for="[kind, items] in groupedArtifacts" :key="kind">
            <div class="mb-2 flex items-center gap-2 text-xs">
              <span class="text-base">{{ kindEmoji(kind) }}</span>
              <span class="font-semibold text-wb-ink">{{ kind }}</span>
              <span class="text-wb-muted">({{ items.length }})</span>
            </div>
            <div class="grid grid-cols-3 gap-3">
              <div
                v-for="a in items"
                :key="a.id"
                class="group relative cursor-pointer overflow-hidden rounded-xl border border-wb-border bg-wb-surface transition-all hover:scale-[1.02] hover:border-wb-primary/40 hover:shadow-lg hover:shadow-wb-primary/10"
                @click="openPreview(a)"
              >
                <div class="relative aspect-square overflow-hidden bg-wb-surface">
                  <img v-if="kind === 'image'" :src="a.preview_url" :alt="a.prompt || ''" class="h-full w-full object-cover" />
                  <video v-else-if="kind === 'video'" :src="a.preview_url" class="h-full w-full object-cover" muted />
                  <div v-else class="flex h-full items-center justify-center text-3xl text-wb-muted">
                    {{ kindEmoji(kind) }}
                  </div>
                  <span class="absolute left-2 top-2 rounded-md border px-2 py-0.5 text-[10px] font-semibold" :class="kindColor(kind)">
                    {{ kind }}
                  </span>
                  <span class="absolute right-2 top-2 rounded-md bg-black/60 px-2 py-0.5 text-[10px] opacity-0 transition-opacity group-hover:opacity-100">
                    {{ t('media.preview') }}
                  </span>
                </div>
                <div class="p-2">
                  <div class="truncate text-xs text-wb-ink" :title="a.prompt || ''">{{ a.prompt || t('media.noPrompt') }}</div>
                  <div class="mt-1 flex items-center justify-between text-[10px] text-wb-muted">
                    <span>{{ a.width }}×{{ a.height }}</span>
                    <button class="rounded bg-wb-danger/10 px-1.5 py-0.5 text-wb-danger opacity-0 transition-opacity hover:bg-wb-danger/20 group-hover:opacity-100" @click.stop="deleteArtifact(a)">
                      🗑
                    </button>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      <!-- 右：生成面板 -->
      <aside class="card col-span-3 overflow-y-auto">
        <h3 class="mb-3 text-xs font-semibold uppercase tracking-wider text-wb-muted">{{ t('media.generate') }}</h3>
        <el-input
          v-model="prompt"
          type="textarea"
          :rows="6"
          resize="none"
          :placeholder="t('media.generatePlaceholder')"
        />
        <el-button
          type="primary"
          class="mt-3 w-full"
          :loading="generating"
          :disabled="!prompt.trim()"
          @click="doGenerate"
        >
          🎨 {{ generating ? t('media.generating') : t('media.generate') }}
        </el-button>
        <p class="mt-2 text-[10px] text-wb-muted">
          {{ generating ? t('media.estimating') : t('media.generateHint') }}
        </p>
      </aside>
    </div>

    <!-- 新建对话框（el-dialog + el-form） -->
    <el-dialog v-model="showCreate" :title="t('media.newPresetTitle')" width="420px" append-to-body>
      <el-form class="wb-el-form" label-position="top" @submit.prevent="doCreate">
        <el-form-item :label="t('media.name')">
          <el-input v-model="form.name" :placeholder="t('media.name')" />
        </el-form-item>
        <el-form-item :label="t('media.kind')">
          <el-select v-model="form.kind" class="w-full">
            <el-option value="image" label="image" />
            <el-option value="video" label="video" />
            <el-option value="audio" label="audio" />
            <el-option value="model3d" label="model3d" />
            <el-option value="vfx" label="vfx" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('media.backend')">
          <el-select v-model="form.backend" class="w-full">
            <el-option value="offline" label="offline" />
            <el-option value="dashscope" label="dashscope" />
            <el-option value="replicate" label="replicate" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('media.modelOptional')">
          <el-input v-model="form.model" :placeholder="t('media.modelOptional')" />
        </el-form-item>
        <el-form-item>
          <el-switch v-model="form.is_default" />
          <span class="ml-2 text-xs text-wb-ink">{{ t('media.setDefault') }}</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreate = false">{{ t('media.cancel') }}</el-button>
        <el-button type="primary" @click="doCreate">{{ t('media.create') }}</el-button>
      </template>
    </el-dialog>

    <!-- 产物预览 modal（el-dialog，点击卡片放大） -->
    <el-dialog
      :model-value="previewArtifact !== null"
      width="720px"
      append-to-body
      @update:model-value="(v: boolean) => (!v ? closePreview() : undefined)"
    >
      <template #header>
        <div class="flex items-center gap-2">
          <el-tag
            size="small"
            :type="previewArtifact?.kind === 'image' ? 'primary' : previewArtifact?.kind === 'video' ? 'success' : 'warning'"
            effect="plain"
          >
            {{ previewArtifact?.kind }}
          </el-tag>
          <span class="font-mono text-xs text-wb-muted">{{ previewArtifact?.id.slice(0, 12) }}…</span>
        </div>
      </template>
      <div v-if="previewArtifact" class="flex min-h-0 items-center justify-center overflow-hidden">
        <img
          v-if="previewArtifact.kind === 'image'"
          :src="previewArtifact.preview_url"
          :alt="previewArtifact.prompt || ''"
          class="max-h-[50vh] max-w-full rounded object-contain"
        />
        <video
          v-else-if="previewArtifact.kind === 'video'"
          :src="previewArtifact.preview_url"
          controls
          class="max-h-[50vh] max-w-full rounded"
        />
        <div v-else class="text-6xl text-wb-muted">{{ kindEmoji(previewArtifact.kind) }}</div>
      </div>
      <div v-if="previewArtifact" class="mt-4 grid grid-cols-2 gap-3 text-xs sm:grid-cols-4">
        <div>
          <div class="text-[10px] uppercase tracking-wider text-wb-muted">{{ t('media.dimensions') }}</div>
          <div class="mt-1 text-wb-ink">{{ previewArtifact.width }}×{{ previewArtifact.height }}</div>
        </div>
        <div>
          <div class="text-[10px] uppercase tracking-wider text-wb-muted">{{ t('media.size') }}</div>
          <div class="mt-1 text-wb-ink">
            {{ previewArtifact.file_size ? `${(previewArtifact.file_size / 1024).toFixed(1)} KB` : '—' }}
          </div>
        </div>
        <div>
          <div class="text-[10px] uppercase tracking-wider text-wb-muted">MIME</div>
          <div class="mt-1 truncate font-mono text-wb-ink">{{ previewArtifact.mime_type ?? '—' }}</div>
        </div>
        <div>
          <div class="text-[10px] uppercase tracking-wider text-wb-muted">{{ t('media.created_at') }}</div>
          <div class="mt-1 text-wb-ink">{{ formatDateTime(previewArtifact.created_at) }}</div>
        </div>
        <div class="col-span-2 sm:col-span-4">
          <div class="text-[10px] uppercase tracking-wider text-wb-muted">Prompt</div>
          <div class="mt-1 text-wb-ink">{{ previewArtifact.prompt || '—' }}</div>
        </div>
      </div>
      <template #footer>
        <el-button type="primary" @click="deleteArtifact(previewArtifact!); closePreview()">{{ t('media.delete') }}</el-button>
        <a v-if="previewArtifact" :href="previewArtifact.download_url" download>
          <el-button type="primary">{{ t('media.download') }}</el-button>
        </a>
      </template>
    </el-dialog>
  </div>
</template>
