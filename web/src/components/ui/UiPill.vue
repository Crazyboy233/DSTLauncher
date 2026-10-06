<script setup>
// 状态徽标。
defineProps({
  /** ok | warn | danger | info | muted */
  tone: { type: String, default: 'muted' },
});
</script>

<template>
  <span class="pill" :class="'pill-' + tone"><slot /></span>
</template>
