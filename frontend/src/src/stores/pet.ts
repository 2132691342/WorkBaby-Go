import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiGet, apiPost } from '@/api/client'
import { useDialog } from '@/composables/useDialog'
import { t } from '@/i18n'
import { UploadPetSprite, OpenFileDialog } from '@/wailsjs/go/main/App'
import type { PetConfig, PetConfigReq, PetSprite, PetSpriteReq } from '@/types/api'

/**
 * 桌宠 store（Pinia 重构）。
 *
 * <p>职责：桌宠配置读取/保存 + sprite 资产管理。
 * 替代原 PetSpaceView.vue 内的本地 ref。
 *
 * <p><b>与后端的契约</b>：
 * <ul>
 *   <li>配置：{@code GET /api/v1/pet/config}</li>
 *   <li>保存配置：{@code POST /api/v1/pet/config/update}</li>
 *   <li>sprite 列表：{@code GET /api/v1/pet/sprites}</li>
 *   <li>新增 sprite：{@code POST /api/v1/pet/sprites}</li>
 *   <li>删除 sprite：{@code POST /api/v1/pet/sprites/{id}/delete}</li>
 * </ul>
 */
export const usePetStore = defineStore('pet', () => {
  const config = ref<PetConfig | null>(null)
  const sprites = ref<PetSprite[]>([])
  const error = ref<string | null>(null)
  const info = ref<string | null>(null)
  const loading = ref(false)

  const form = ref<PetConfigReq>({ enabled: false, mode: 'swing', scale: 1, bubble_enabled: true, bubble_duration_ms: 3000 })
  const spriteName = ref('')

  /** 同时加载配置与 sprite 列表，并把配置字段回填到表单。 */
  async function load(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const [c, s] = await Promise.all([apiGet<PetConfig>('/api/v1/pet/config'), apiGet<PetSprite[]>('/api/v1/pet/sprites')])
      config.value = c
      sprites.value = s
      form.value = { enabled: c.enabled, mode: c.mode, sprite_id: c.sprite_id, position_x: c.position_x, position_y: c.position_y, scale: c.scale, bubble_enabled: c.bubble_enabled, bubble_duration_ms: c.bubble_duration_ms }
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  /** 保存桌宠配置。 */
  async function save(): Promise<void> {
    error.value = null
    info.value = null
    try {
      config.value = await apiPost<PetConfig>('/api/v1/pet/config/update', form.value)
      info.value = t('common.saved')
      window.dispatchEvent(new Event('wb-avatar-refresh'))
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  /** 新增 sprite（内置标记恒为 false）。 */
  async function addSprite(): Promise<void> {
    error.value = null
    if (!spriteName.value.trim()) {
      error.value = t('common.nameRequired')
      return
    }
    try {
      await apiPost<PetSprite>('/api/v1/pet/sprites', { name: spriteName.value.trim(), is_builtin: false } satisfies PetSpriteReq)
      spriteName.value = ''
      await load()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  /** 删除指定 sprite。 */
  async function removeSprite(id: string): Promise<void> {
    const dialog = useDialog()
    if (!await dialog.confirm({ title: t('common.confirmDeleteTitle'), content: t('common.confirmDelete'), danger: true })) return
    try {
      await apiPost(`/api/v1/pet/sprites/${id}/delete`)
      await load()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  /** 从本地路径取文件名（去扩展名），作为上传 sprite 的默认名。 */
  function basenameFromPath(p: string): string {
    const seg = p.replace(/\\/g, '/').split('/').pop() ?? 'sprite'
    return seg.replace(/\.[^.]+$/, '') || 'sprite'
  }

  /**
   * 上传自定义 sprite：原生文件对话框选本地图片 → 绑定 UploadPetSprite(name, srcPath)。
   *
   * <p>修复点（痛点：重复选文件 + 上传后无预览）：
   * <ul>
   *   <li>不再叠加 el-upload 浏览器选择器——一次原生对话框即完成选图+上传，杜绝「选两次」；</li>
   *   <li>上传成功后本地增量插入列表并自动选中（form.sprite_id），不再整体 load() 重置
   *       表单导致刚选的形象预览不到；</li>
   *   <li>默认名取文件 basename（去扩展名），也可由调用方显式传入 name。</li>
   * </ul>
   */
  async function uploadSpriteFile(_file?: File, name?: string): Promise<PetSprite | null> {
    error.value = null
    info.value = null
    try {
      const selected = await OpenFileDialog(t('pet.pickSprite'), '*.png;*.jpg;*.jpeg;*.gif;*.webp')
      if (!selected) return null
      const created = await UploadPetSprite(name?.trim() || basenameFromPath(selected), selected)
      sprites.value = [created, ...sprites.value.filter((s) => s.id !== created.id)]
      // 自动选中刚上传的形象，预览立即更新（不触发整体 load，避免表单被 config 覆盖）
      if (form.value) form.value.sprite_id = created.id
      info.value = t('pet.uploaded', created.name)
      return created
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
      return null
    }
  }

  return {
    config,
    sprites,
    error,
    info,
    loading,
    form,
    spriteName,
    load,
    save,
    addSprite,
    removeSprite,
    uploadSpriteFile
  }
})
