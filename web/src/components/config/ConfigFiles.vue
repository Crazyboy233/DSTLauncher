<script setup>
// 生成结果预览：每个文件一张卡，只读。

import UiPill from '@/components/ui/UiPill.vue';

defineProps({
  files: { type: Array, required: true },
});
</script>

<template>
  <div v-for="f in files" :key="f.path" class="file-card">
    <div class="file-head">
      <span class="file-path">{{ f.path }}</span>
      <UiPill :tone="f.onDisk ? 'ok' : 'warn'">{{ f.onDisk ? '已落盘' : '未生成' }}</UiPill>
      <UiPill v-if="f.redacted" tone="muted">令牌已隐藏</UiPill>
    </div>
    <pre class="file-body">{{ f.content }}</pre>
  </div>
</template>
