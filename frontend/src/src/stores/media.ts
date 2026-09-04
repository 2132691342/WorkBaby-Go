import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiGet, apiPost } from '@/api/client'
import { useDialog } from '@/composables/useDialog'
import { t } from '@/i18n'
import type { MediaPreset, MediaArtifact, MediaPresetReq, MediaGenerateReq } from '@/types/api'

/**
 * Media store（Pinia 重构）。
 *
 * <p>职责：媒体 Preset CRUD + 切换激活 + 立即生成 + 最近产物画廊。
 * 替代原 MediaView.vue 内的本地 ref。
 *
 * <p><b>与后端的契约</b>：
 * <ul>
 *   <li>Preset 列表：{@code GET /api/v1/media/presets}（可选 {@code ?kind=} 过滤）</li>
 *   <li>产物列表：{@code GET /api/v1/media/artifacts?limit=20}</li>
 *   <li>创建 preset：{@code POST /api/v1/media/presets}</li>
 *   <li>删除 preset：{@code POST /api/v1/media/presets/{id}/delete}</li>
 *   <li>激活 preset：{@code POST /api/v1/media/presets/{id}/activate}</li>
 *   <li>生成产物：{@code POST /api/v1/media/generate}</li>
 *   <li>删除产物：{@code POST /api/v1/media/artifacts/{id}/delete}</li>
 * </ul>
 */
export const useMediaStore = defineStore('media', () => {
  const presets = ref<MediaPreset[]>([])
  const artifacts = ref<MediaArtifact[]>([])
  const filter = ref<string>('')
  const error = ref<string | null>(null)
  const generating = ref(false)
  const prompt = ref(t('media.defaultPrompt'))
  const showCreate = ref(false)
  const form = ref<MediaPresetReq>({ name: '', kind: 'image', backend: 'offline', model: '', enabled: true, is_default: false })

  /** 拉取 preset 列表（按 filter 过滤）与最近产物。 */
  async function load(): Promise<void> {
    try {
      presets.value = await apiGet<MediaPreset[]>(`/api/v1/media/presets${filter.value ? '?kind=' + filter.value : ''}`)
      artifacts.value = await apiGet<MediaArtifact[]>('/api/v1/media/artifacts?limit=20')
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  /** 新建 preset 并在成功后刷新列表。 */
  async function doCreate(): Promise<void> {
    if (!form.value.name.trim()) {
      error.value = t('common.nameRequired')
      return
    }
    try {
      await apiPost('/api/v1/media/presets', { ...form.value, model: form.value.model || null })
      showCreate.value = false
      form.value = { name: '', kind: 'image', backend: 'offline', model: '', enabled: true, is_default: false }
      await load()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  /** 删除 preset 并在成功后刷新列表。 */
  async function doDelete(p: MediaPreset): Promise<void> {
    const dialog = useDialog()
    if (!await dialog.confirm({ title: t('common.confirmDeleteTitle'), content: t('common.confirmDeleteNamed', p.name), danger: true })) return
    await apiPost(`/api/v1/media/presets/${p.id}/delete`, {})
    await load()
  }

  /** 激活 preset 并在成功后刷新列表。 */
  async function doActivate(p: MediaPreset): Promise<void> {
    await apiPost(`/api/v1/media/presets/${p.id}/activate`, {})
    await load()
  }

  /** 按 prompt 生成产物并前置插入画廊。 */
  async function doGenerate(): Promise<void> {
    if (!prompt.value.trim()) return
    generating.value = true
    error.value = null
    try {
      const req: MediaGenerateReq = { kind: 'image', prompt: prompt.value }
      const r = await apiPost<MediaArtifact>('/api/v1/media/generate', req)
      artifacts.value = [r, ...artifacts.value]
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      generating.value = false
    }
  }

  /** 删除产物并从画廊过滤。 */
  async function deleteArtifact(a: MediaArtifact): Promise<void> {
    const dialog = useDialog()
    if (!await dialog.confirm({ title: t('common.confirmDeleteTitle'), content: t('common.confirmDelete'), danger: true })) return
    await apiPost(`/api/v1/media/artifacts/${a.id}/delete`, {})
    artifacts.value = artifacts.value.filter((x) => x.id !== a.id)
  }

  return {
    presets,
    artifacts,
    filter,
    error,
    generating,
    prompt,
    showCreate,
    form,
    load,
    doCreate,
    doDelete,
    doActivate,
    doGenerate,
    deleteArtifact
  }
})
