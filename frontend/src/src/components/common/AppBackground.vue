<script setup lang="ts">
/**
 * 全局背景层（克制现代工具风）。
 *
 * <p>默认纯色（--wb-bg）；仅当用户上传了自定义背景图时显示（高斯模糊 + 半透明 overlay 保对比度）。
 * 无背景时不再渲染任何渐变 mesh / grain —— 桌面工具应有的冷静底色。
 */
defineProps<{
  /** 可选：用户上传背景图 URL（未接时纯色）。 */
  backgroundUrl?: string | null
}>()
</script>

<template>
  <div class="pointer-events-none fixed inset-0 -z-10 overflow-hidden">
    <!-- 用户上传背景（高斯模糊 + 半透明 overlay 保对比度） -->
    <template v-if="backgroundUrl">
      <div
        class="absolute inset-0 bg-cover bg-center"
        :style="{ backgroundImage: `url(${backgroundUrl})`, filter: 'blur(18px)' }"
      />
      <div class="absolute inset-0 bg-white/70 dark:bg-black/40" />
    </template>
  </div>
</template>
