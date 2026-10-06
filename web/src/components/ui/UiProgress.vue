<script setup>
// 进度条。value 为 0-100；传负数表示进度未知（显示为满格的浅色条）。
import { computed } from 'vue';

const props = defineProps({
  value: { type: Number, default: 0 },
});

const width = computed(() => {
  const v = props.value < 0 ? 100 : Math.min(100, Math.max(0, props.value));
  return v + '%';
});
</script>

<template>
  <div class="progress">
    <div class="progress-bar" :style="{ width }" />
  </div>
</template>
