<script setup lang="ts">
/**
 * 桌宠配置面板（完全重写）。
 *
 * <p>关键改动（vs 上版）：
 * <ol>
 *   <li>上传走 multipart（{@code POST /api/v1/pet/upload}），不再是 FileReader.readAsDataURL（不落库，关掉面板就丢）</li>
 *   <li>预览走 {@code <img src="/api/v1/pet/preview/{spriteID}"> + CSS keyframes}（呼吸 / 跳跃 / 犯困），
 *       <b>不用 WebGL</b>（透明置顶窗口的 GPU canvas 会被 DWM 重合成闪烁）</li>
 *   <li>真支持"用户上传自定义图片到桌宠"——上传后写入后端 pet 目录 + 落 PetSpriteDO + UI 切换 spriteID</li>
 *   <li>内置 SVG 角色选择：14 套（kitten / bunny / puppy / panda / robot / dragon / unicorn / slime /
 *       angel / devil / alien / phoenix / mermaid / witch）保留为可选项</li>
 *   <li>行为配置：enabled / mode（swing/jcef）/ 位置 / 缩放 / 气泡</li>
 * </ol>
 */
import { computed, onMounted, ref, watch } from 'vue'
import { Upload, X, Sparkles, Eye, EyeOff } from '@/components/common/icons'
import { useToast } from '@/composables/useToast'
import { apiGet, apiPost, apiUpload } from '@/api/client'
import { t } from '@/i18n'
import {
  PET_CHARACTERS,
  PET_MOODS,
  getPetSvg,
  getMoodFilter,
  type PetMood,
  type PetCharacter
} from '@/components/pet/PetCharacters'
import type { PetConfig, PetSprite } from '@/types/api'

const toast = useToast()

// 单机单用户配置主键（与后端 PetConfigService.DEFAULT_USER 对齐）
const DEFAULT_USER = 'default'

const config = ref<PetConfig>({
  user_id: DEFAULT_USER,
  enabled: false,
  mode: 'swing',
  sprite_id: null,
  position_x: 100,
  position_y: 100,
  scale: 1,
  bubble_enabled: true,
  bubble_duration_ms: 5000,
  updated_at: null
})

const sprites = ref<PetSprite[]>([])
const selectedCharacter = ref<string | null>(null)
const customSpriteID = ref<string | null>(null)
const selectedMood = ref<PetMood>('idle')
const loading = ref(false)
const saving = ref(false)
const uploading = ref(false)

// 当前显示的 sprite：用户上传 > 内置 SVG
const previewUrl = computed(() => {
  if (customSpriteID.value) return `/files/sprites/${customSpriteID.value}`
  if (selectedCharacter.value) return null  // 走 SVG
  return null
})

const inlineSvg = computed(() => {
  if (previewUrl.value) return null
  if (selectedCharacter.value) return getPetSvg(selectedCharacter.value, 120)
  return getPetSvg('kitten', 120)
})

const moodFilter = computed(() => getMoodFilter(selectedMood.value))

// 加载配置 + sprite 列表
async function loadConfig(): Promise<void> {
  loading.value = true
  try {
    config.value = await apiGet<PetConfig>('/api/v1/pet/config')
    sprites.value = await apiGet<PetSprite[]>('/api/v1/pet/sprites')
    // 反查当前 spriteID 是哪个角色：如果是上传的（ext in png/webp/gif），customSpriteID 复用；否则反查内置表
    const sid = config.value.sprite_id
    if (sid) {
      const sprite = sprites.value.find((s) => s.id === sid)
      if (sprite) {
        if (sprite.is_builtin) {
          selectedCharacter.value = sprite.name
        } else {
          customSpriteID.value = sprite.id
        }
      }
    }
  } catch {
    // 用默认配置
  } finally {
    loading.value = false
  }
}

// 切换内置角色
function selectCharacter(character: PetCharacter): void {
  selectedCharacter.value = character.id
  customSpriteID.value = null
  // 把内置角色注册成 sprite（如果还没注册过）
  let sprite = sprites.value.find((s) => s.is_builtin && s.name === character.id)
  if (!sprite) {
    const created: PetSprite = {
      id: `builtin-${character.id}`,
      name: character.id,
      file_path: null,
      format: 'svg',
      frame_count: null,
      is_builtin: true,
      created_at: 0
    }
    sprites.value = [created, ...sprites.value]
    config.value.sprite_id = created.id
    return
  }
  config.value.sprite_id = sprite.id
}

// 上传自定义图片
function onUploadFileChange(file: { raw?: File }): void {
  void handleUploadFile(file.raw)
}

async function handleUploadFile(file: File | undefined): Promise<void> {
  if (!file) return

  // 5MB 校验
  if (file.size > 5 * 1024 * 1024) {
    toast.warning(t('pet.fileTooLarge'))
    return
  }
  // 类型校验：宽容 file.type 缺失的情况（部分浏览器/系统对 png 不返回 mime）；
  // 后端仍按扩展名严格白名单 (png/webp/gif/jpg/jpeg)，前端只做 hint。
  const typeOk =
    !file.type /* 浏览器未识别 */ ||
    /^image\/(png|webp|gif|jpeg|jpg)$/.test(file.type) ||
    /\.(png|webp|gif|jpe?g)$/i.test(file.name)
  if (!typeOk) {
    toast.warning(t('pet.invalidFileType'))
    return
  }

  uploading.value = true
  try {
    const res = await apiUpload<{ sprite_id: string; name?: string; format: string; size: number }>(
      '/api/v1/pet/upload',
      file
    )
    customSpriteID.value = res.sprite_id
    selectedCharacter.value = null
    config.value.sprite_id = res.sprite_id
    // 刷新 sprite 列表
    sprites.value = await apiGet<PetSprite[]>('/api/v1/pet/sprites')
    toast.success(t('pet.uploadSuccess'))
  } catch (e) {
    toast.error(t('pet.uploadFailed'), e instanceof Error ? e.message : String(e))
  } finally {
    uploading.value = false
  }
}

function clearCustomImage(): void {
  customSpriteID.value = null
  config.value.sprite_id = null
}

async function saveConfig(): Promise<void> {
  saving.value = true
  try {
    await apiPost('/api/v1/pet/config/update', config.value)
    toast.success(t('pet.saveSuccess'))
  } catch (e) {
    toast.error(t('pet.saveFailed'), e instanceof Error ? e.message : String(e))
  } finally {
    saving.value = false
  }
}

watch(() => config.value.enabled, () => {
  // 启用状态实时反映
})

onMounted(loadConfig)
</script>

<template>
  <div class="space-y-6 text-wb-ink">
    <!-- 标题 -->
    <div>
      <h3 class="text-lg font-semibold">{{ t('pet.config') }}</h3>
      <p class="mt-1 text-sm text-wb-muted">{{ t('pet.configDesc') }}</p>
    </div>

    <!-- 预览区 -->
    <div class="rounded-2xl border border-wb-border bg-wb-surface/70 p-6 shadow-[var(--wb-shadow)]">
      <div class="mb-3 flex items-center justify-between">
        <span class="text-sm font-medium">{{ t('pet.preview') }}</span>
        <span v-if="config.enabled" class="inline-flex items-center gap-1 text-xs text-wb-success">
          <Eye class="h-3 w-3" />
          {{ t('pet.enabled') }}
        </span>
        <span v-else class="inline-flex items-center gap-1 text-xs text-wb-muted">
          <EyeOff class="h-3 w-3" />
          {{ t('pet.disabled') }}
        </span>
      </div>
      <div class="flex h-40 items-center justify-center overflow-hidden rounded-xl bg-wb-bg">
        <!-- 自定义图片（multipart 已落库） -->
        <img
          v-if="previewUrl"
          :src="previewUrl"
          alt="Custom Pet"
          class="pet-breathe max-h-32 max-w-32 object-contain"
        />
        <!-- 内置 SVG 角色（带呼吸 + 情绪滤镜） -->
        <div
          v-else-if="inlineSvg"
          class="pet-breathe pet-fade-in"
          :style="{ filter: moodFilter }"
          v-html="inlineSvg"
        />
        <span v-else class="text-wb-muted">{{ t('pet.noPet') }}</span>
      </div>
    </div>

    <!-- 情绪选择 -->
    <div>
      <h4 class="mb-3 text-sm font-medium">{{ t('pet.mood') }}</h4>
      <el-radio-group v-model="selectedMood" size="small">
        <el-radio-button v-for="mood in PET_MOODS" :key="mood.id" :value="mood.id">
          <span class="mr-1">{{ mood.icon }}</span>
          {{ t(mood.labelKey) }}
        </el-radio-button>
      </el-radio-group>
    </div>

    <!-- 内置角色选择 -->
    <div>
      <h4 class="mb-3 text-sm font-medium">{{ t('pet.builtinCharacters') }}</h4>
      <div class="grid grid-cols-5 gap-3 sm:grid-cols-7 lg:grid-cols-7">
        <button
          v-for="character in PET_CHARACTERS"
          :key="character.id"
          type="button"
          class="group flex flex-col items-center gap-2 rounded-xl border-2 p-3 transition-all"
          :class="
            selectedCharacter === character.id
              ? 'border-wb-primary bg-wb-primary/10'
              : 'border-wb-border hover:border-wb-primary/50 hover:bg-wb-primary/5'
          "
          @click="selectCharacter(character)"
        >
          <div
            class="h-12 w-12"
            v-html="getPetSvg(character.id, 48)"
          />
          <span class="text-xs font-medium text-wb-ink">{{ t(character.nameKey) }}</span>
        </button>
      </div>
    </div>

    <!-- 自定义上传 -->
    <div class="rounded-2xl border border-wb-border bg-wb-surface/70 p-4 shadow-sm">
      <h4 class="mb-3 text-sm font-medium">{{ t('pet.customCharacter') }}</h4>
      <div class="flex flex-wrap items-center gap-3">
        <el-upload
          :show-file-list="false"
          :auto-upload="false"
          :disabled="uploading"
          accept="image/png,image/webp,image/gif,image/jpeg,image/jpg,.png,.webp,.gif,.jpg,.jpeg"
          @change="onUploadFileChange"
        >
          <el-button type="primary" plain :loading="uploading">
            <Upload class="h-4 w-4" />
            {{ uploading ? t('pet.uploading') : t('pet.uploadImage') }}
          </el-button>
        </el-upload>
        <span class="text-xs text-wb-muted">{{ t('pet.uploadHint') }}</span>

        <el-button v-if="customSpriteID" type="danger" plain size="small" @click="clearCustomImage">
          <el-icon class="mr-1"><X /></el-icon>
          {{ t('pet.clearCustom') }}
        </el-button>
      </div>
      <div v-if="customSpriteID" class="mt-3 flex items-center gap-2 text-xs text-wb-muted">
        <Sparkles class="h-3 w-3 text-wb-primary" />
        <span>{{ t('pet.currentSpriteID', customSpriteID) }}</span>
      </div>
    </div>

    <!-- 行为设置 -->
    <div class="space-y-3 rounded-2xl border border-wb-border bg-wb-surface/70 p-4 shadow-sm">
      <h4 class="text-sm font-medium">{{ t('pet.behavior') }}</h4>

      <div class="flex items-center justify-between">
        <span class="text-sm text-wb-ink">{{ t('pet.enable') }}</span>
        <el-switch v-model="config.enabled" />
      </div>

      <div class="flex items-center justify-between">
        <span class="text-sm text-wb-ink">{{ t('pet.modeLabel') }}</span>
        <el-select v-model="config.mode" size="small" class="w-40">
          <el-option :label="t('pet.mode.swing')" value="swing" />
          <el-option :label="t('pet.mode.jcef')" value="jcef" />
        </el-select>
      </div>

      <div class="flex items-center justify-between">
        <span class="text-sm text-wb-ink">{{ t('pet.bubble') }}</span>
        <el-switch v-model="config.bubble_enabled" />
      </div>

      <div class="flex items-center justify-between gap-3">
        <span class="shrink-0 text-sm text-wb-ink">{{ t('pet.scale') }}</span>
        <div class="flex flex-1 items-center gap-3">
          <el-slider v-model="config.scale" :min="0.5" :max="3" :step="0.1" class="flex-1" />
          <span class="w-10 shrink-0 text-right text-xs tabular-nums text-wb-ink">{{ config.scale }}×</span>
        </div>
      </div>

      <div class="grid grid-cols-2 gap-3">
        <div>
          <span class="text-xs text-wb-muted">{{ t('pet.position_x') }}</span>
          <el-input-number v-model="config.position_x" size="small" class="mt-1! w-full" />
        </div>
        <div>
          <span class="text-xs text-wb-muted">{{ t('pet.position_y') }}</span>
          <el-input-number v-model="config.position_y" size="small" class="mt-1! w-full" />
        </div>
      </div>

      <div>
        <span class="text-xs text-wb-muted">{{ t('pet.bubbleDuration') }}</span>
        <el-input-number v-model="config.bubble_duration_ms" size="small" class="mt-1! w-full" />
      </div>
    </div>

    <!-- 保存按钮 -->
    <el-button
      type="primary"
      size="large"
      class="w-full"
      :loading="saving"
      :disabled="loading || uploading"
      @click="saveConfig"
    >
      {{ saving ? t('common.saving') : t('pet.save') }}
    </el-button>
  </div>
</template>

<style scoped>
/* 呼吸 */
@keyframes pet-breathe {
  0%, 100% { transform: scaleY(1); }
  50%      { transform: scaleY(1.008) translateY(-1px); }
}
.pet-breathe {
  animation: pet-breathe 3.6s ease-in-out infinite;
  transform-origin: 50% 100%;
}

/* 跳跃（happy） */
@keyframes pet-hop {
  0%, 100% { transform: translateY(0); }
  30%      { transform: translateY(-7px); }
  55%      { transform: translateY(0) scaleY(0.997); }
}

/* 入场 fade-in */
@keyframes pet-fade-in {
  from { opacity: 0; transform: translateY(4px); }
  to   { opacity: 1; transform: translateY(0); }
}
.pet-fade-in {
  animation: pet-fade-in 250ms ease-out both;
}

/* prefers-reduced-motion 适配（CLAUDE.md 规范） */
@media (prefers-reduced-motion: reduce) {
  .pet-breathe,
  .pet-fade-in {
    animation: none !important;
  }
}
</style>
