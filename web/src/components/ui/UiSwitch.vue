<script setup>
// 带文字的开关（复选框包成胶囊样式）。
// 用 v-model 双向绑定：<UiSwitch v-model="form.pvp" label="PVP" />
defineProps({
  modelValue: { type: Boolean, default: false },
  label: { type: String, default: '' },
  /** 服务器运行中等场景下冻结开关 */
  disabled: { type: Boolean, default: false },
});
const emit = defineEmits(['update:modelValue']);
</script>

<template>
  <label class="switch">
    <input
      type="checkbox"
      :checked="modelValue"
      :disabled="disabled"
      @change="emit('update:modelValue', $event.target.checked)"
    >
    <span>{{ label }}</span>
  </label>
</template>
