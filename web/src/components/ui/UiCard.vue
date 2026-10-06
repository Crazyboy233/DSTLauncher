<script setup>
// 卡片外壳。标题 / 说明 / 右上角按钮都用插槽，内容放默认插槽。
defineProps({
  /** 内容区不留内边距，适合控制台这类需要贴边的区域 */
  flush: { type: Boolean, default: false },
});
</script>

<template>
  <article class="card">
    <header v-if="$slots.title || $slots.actions" class="card-head">
      <h2>
        <slot name="title" />
        <small v-if="$slots.hint"><slot name="hint" /></small>
      </h2>
      <div v-if="$slots.actions" class="card-actions"><slot name="actions" /></div>
    </header>
    <div class="card-body" :class="{ 'card-body-flush': flush }"><slot /></div>
  </article>
</template>
