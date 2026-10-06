<script setup>
// 指标卡：一个大数字 + 说明 + 图标。
import Icon from '@/components/ui/Icon.vue';

defineProps({
  label: { type: String, required: true },
  value: { type: [String, Number], required: true },
  sub: { type: String, default: '' },
  icon: { type: String, default: 'activity' },
  /** '' | ok | warn | danger | info，只影响图标底色 */
  tone: { type: String, default: '' },
});
</script>

<template>
  <div class="stat" :class="tone ? 'is-' + tone : ''">
    <span class="stat-icon"><Icon :name="icon" /></span>
    <div class="stat-body">
      <div class="stat-label">{{ label }}</div>
      <div class="stat-value">{{ value }}</div>
      <div v-if="sub" class="stat-sub">{{ sub }}</div>
    </div>
  </div>
</template>
