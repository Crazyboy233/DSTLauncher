<script setup>
// 表单一行：左侧标签 + 右侧控件，控件放默认插槽。
// wide 让该行横跨整个表单网格；block 让标签独占一行（适合多行输入框）。
//
// 注意：属性 id 对应的 prop 必须叫 forId 而不是 for——
// for 是 JS 保留字，写成 :for="for" 模板表达式无法解析。
defineProps({
  label: { type: String, default: '' },
  forId: { type: String, default: '' },
  tip: { type: String, default: '' },
  wide: { type: Boolean, default: false },
  block: { type: Boolean, default: false },
});
</script>

<template>
  <div class="field" :class="{ 'field-wide': wide, 'field-block': block }">
    <label v-if="label" class="field-label" :for="forId">{{ label }}</label>
    <slot />
    <p v-if="tip" class="field-tip">{{ tip }}</p>
  </div>
</template>
