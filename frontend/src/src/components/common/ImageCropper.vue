<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { t } from '@/i18n'

/**
 * 图片裁剪 / 旋转编辑器（Canvas 实现，零依赖）。
 *
 * <p>交互：拖拽平移选区 + 90° 步进旋转 + 缩放滑块；确认输出 PNG Blob 交给调用方上传。
 * 初始缩放按「短边铺满画布」计算（cover），保证裁剪结果不留白边。
 *
 * <p>只做位图变换，不动原文件；桌宠头像与聊天背景共用同一个形象来源。
 */
const props = withDefaults(defineProps<{
  modelValue: boolean
  /** 待编辑的源图片；置非空时载入画布。 */
  source: File | null
  /** 输出宽高比（宽/高）；1 = 正方形。 */
  aspect?: number
  /** 输出宽度（像素）。 */
  outputWidth?: number
}>(), {
  source: null,
  aspect: 1,
  outputWidth: 512
})

const emit = defineEmits<{
  'update:modelValue': [boolean]
  confirm: [Blob]
}>()

const canvasRef = ref<HTMLCanvasElement | null>(null)
const img = ref<HTMLImageElement | null>(null)
const objectUrl = ref('')
const rotation = ref(0)
const scale = ref(1)
const offX = ref(0)
const offY = ref(0)
const dragging = ref(false)
let lastX = 0
let lastY = 0

const outH = computed(() => Math.round(props.outputWidth / (props.aspect || 1)))

function release(): void {
  if (objectUrl.value) {
    URL.revokeObjectURL(objectUrl.value)
    objectUrl.value = ''
  }
}

function load(src: File): void {
  release()
  objectUrl.value = URL.createObjectURL(src)
  const el = new Image()
  el.onload = () => {
    img.value = el
    reset()
    draw()
  }
  el.src = objectUrl.value
}

function reset(): void {
  rotation.value = 0
  scale.value = 1
  offX.value = 0
  offY.value = 0
}

/** 短边铺满画布的基础缩放；旋转 90/270 度时宽高互换。 */
function fitScale(): number {
  const el = img.value
  if (!el) return 1
  const swap = rotation.value % 180 !== 0
  const iw = swap ? el.naturalHeight : el.naturalWidth
  const ih = swap ? el.naturalWidth : el.naturalHeight
  if (iw <= 0 || ih <= 0) return 1
  return Math.max(props.outputWidth / iw, outH.value / ih)
}

function draw(): void {
  const c = canvasRef.value
  const el = img.value
  if (!c || !el) return
  const ctx = c.getContext('2d')
  if (!ctx) return
  const w = props.outputWidth
  const h = outH.value
  c.width = w
  c.height = h
  ctx.clearRect(0, 0, w, h)
  ctx.save()
  ctx.translate(w / 2 + offX.value, h / 2 + offY.value)
  ctx.rotate((rotation.value * Math.PI) / 180)
  const s = fitScale() * scale.value
  ctx.scale(s, s)
  ctx.drawImage(el, -el.naturalWidth / 2, -el.naturalHeight / 2)
  ctx.restore()
}

function rotate(): void {
  rotation.value = (rotation.value + 90) % 360
  draw()
}

function onPointerDown(e: PointerEvent): void {
  dragging.value = true
  lastX = e.clientX
  lastY = e.clientY
}

function onPointerMove(e: PointerEvent): void {
  if (!dragging.value) return
  const c = canvasRef.value
  if (!c || c.clientWidth <= 0) return
  // 画布 CSS 宽度与输出宽度不同，位移需按比例换算
  const ratio = props.outputWidth / c.clientWidth
  offX.value += (e.clientX - lastX) * ratio
  offY.value += (e.clientY - lastY) * ratio
  lastX = e.clientX
  lastY = e.clientY
  draw()
}

function onPointerUp(): void {
  dragging.value = false
}

function confirm(): void {
  const c = canvasRef.value
  if (!c || !img.value) return
  draw()
  c.toBlob((b) => {
    if (b) emit('confirm', b)
  }, 'image/png')
}

function close(): void {
  emit('update:modelValue', false)
}

watch(() => props.source, (f) => {
  if (f) load(f)
})

watch(() => props.modelValue, (v) => {
  if (!v) release()
})

onBeforeUnmount(release)
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    :title="t('cropper.title')"
    width="440px"
    append-to-body
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <div class="flex flex-col items-center gap-3">
      <canvas
        ref="canvasRef"
        class="cursor-grab rounded-xl border border-wb-border bg-wb-bg"
        :style="{ width: '320px', aspectRatio: String(aspect || 1) }"
        @pointerdown="onPointerDown"
        @pointermove="onPointerMove"
        @pointerup="onPointerUp"
        @pointerleave="onPointerUp"
      />
      <p class="text-xs text-wb-muted">{{ t('cropper.hint') }}</p>

      <div class="flex w-full items-center gap-3">
        <span class="shrink-0 text-xs text-wb-muted">{{ t('cropper.zoom') }}</span>
        <el-slider
          v-model="scale"
          :min="0.2"
          :max="4"
          :step="0.05"
          class="flex-1"
          @input="draw"
        />
        <span class="w-10 shrink-0 text-right text-xs tabular-nums">{{ scale.toFixed(2) }}×</span>
      </div>

      <div class="flex w-full items-center gap-2">
        <el-button size="small" @click="rotate">
          {{ t('cropper.rotate') }} 90°
        </el-button>
        <el-button size="small" @click="reset(); draw()">
          {{ t('cropper.reset') }}
        </el-button>
        <span class="flex-1" />
        <el-button size="small" @click="close">{{ t('ui.btn.cancel') }}</el-button>
        <el-button size="small" type="primary" :disabled="!img" @click="confirm">
          {{ t('ui.btn.save') }}
        </el-button>
      </div>
    </div>
  </el-dialog>
</template>
