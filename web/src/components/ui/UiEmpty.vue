<script setup>
// 空态：图标 + 文案。默认文案走插槽，便于写多行说明。
import Icon from '@/components/ui/Icon.vue';

defineProps({
  icon: { type: String, default: 'inbox' },
  text: { type: String, default: '' },
});
</script>

<template>
  <div class="empty">
    <span class="empty-icon"><Icon :name="icon" size="20" /></span>
    <span><slot>{{ text }}</slot></span>
  </div>
</template>
