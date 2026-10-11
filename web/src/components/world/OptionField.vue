<script setup>
// 单个世界设置项。
//
// 有取值列表的用下拉框；取值由游戏运行时生成的（task_set、start_location 等）
// 只能用文本框让用户自己填。改过的项在标签旁点一个小圆点，
// 方便在两百多项里一眼看出自己改了什么。
//
// 行首显示游戏内「世界设置」界面的同款图标，
// 未收录图标的选项自动隐藏图片位。

import { computed, ref } from 'vue';

import { iconOf } from '@/lib/worldIcons.js';

const props = defineProps({
  option: { type: Object, required: true },
  value: { type: String, default: '' },
  changed: { type: Boolean, default: false },
  /** 服务器运行中：控件只读 */
  disabled: { type: Boolean, default: false },
});

const emit = defineEmits(['change']);

const inputId = computed(() => 'opt-' + props.option.name);
const icon = computed(() => iconOf(props.option.name));
const iconFailed = ref(false);

/** 当前值不在游戏给出的取值列表里（游戏更新改名后会出现），兜底显示出来 */
const unknownValue = computed(() => {
  const vals = props.option.values;
  if (!props.value || !vals || !vals.length) return false;
  return !vals.some(v => v.data === props.value);
});

function onChange(e) {
  if (props.disabled) return;
  emit('change', e.target.value);
}
</script>

<template>
  <div class="opt-row" :class="{ 'is-changed': changed }">
    <!-- 图标位常驻：即使某张图缺失，单元格布局也不会变形 -->
    <span class="opt-row-icon">
      <img
        v-if="icon && !iconFailed"
        :src="icon"
        :alt="''"
        loading="lazy"
        @error="iconFailed = true"
      >
    </span>

    <div class="opt-row-main">
      <label class="opt-row-label" :for="inputId">
        {{ option.label || option.name }}
        <span v-if="changed" class="opt-row-dot" title="已修改" />
      </label>

      <select
        v-if="option.values && option.values.length"
        :id="inputId"
        class="select"
        :value="value"
        :disabled="disabled"
        @change="onChange"
      >
        <option v-if="unknownValue" :value="value">{{ value }}（游戏未提供此项）</option>
        <option v-for="v in option.values" :key="v.data" :value="v.data">
          {{ v.label || v.data }}
        </option>
      </select>

      <input
        v-else
        :id="inputId"
        class="input mono"
        :value="value"
        :disabled="disabled"
        autocomplete="off"
        placeholder="按游戏内的名称填写"
        @change="onChange"
      >

      <p v-if="option.dynamic" class="opt-row-tip">取值由游戏运行时决定，需自行填写</p>
    </div>
  </div>
</template>
