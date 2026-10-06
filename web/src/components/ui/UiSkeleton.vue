<script setup>
// 骨架屏：比转圈的 spinner 更能表达「这里将要出现几行内容」。

const props = defineProps({
  rows: { type: Number, default: 3 },
});

const WIDTHS = ['64%', '90%', '48%', '76%', '56%'];
const widthOf = i => WIDTHS[(i - 1) % WIDTHS.length];
void props;
</script>

<template>
  <div>
    <div v-for="i in rows" :key="i" class="skeleton-row">
      <span class="skeleton" style="width: 8px; height: 8px; border-radius: 50%" />
      <span class="skeleton" :style="{ height: '12px', width: widthOf(i) }" />
    </div>
  </div>
</template>
