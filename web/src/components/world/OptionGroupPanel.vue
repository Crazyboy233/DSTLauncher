<script setup>
// 一个官方分组（折叠面板），默认展开。
//
// 分组名与顺序都来自游戏文件，不手工维护；看到「已改 N」就知道
// 这个分组里有几项被改过，不用逐个展开找。

import { computed, ref } from 'vue';

import Icon from '@/components/ui/Icon.vue';
import UiPill from '@/components/ui/UiPill.vue';
import OptionField from '@/components/world/OptionField.vue';

const props = defineProps({
  group: { type: Object, required: true },
  /** (option) => 当前值 */
  valueOf: { type: Function, required: true },
  /** (option) => 是否被改动过 */
  isChanged: { type: Function, required: true },
  /** 服务器运行中：整个分组冻结 */
  disabled: { type: Boolean, default: false },
});

const emit = defineEmits(['change']);

const open = ref(true);

const changedCount = computed(() => props.group.options.filter(o => props.isChanged(o)).length);
</script>

<template>
  <section class="opt-group" :class="{ 'is-open': open }">
    <button class="opt-group-head" type="button" @click="open = !open">
      <Icon class="opt-group-arrow" name="chevron" size="14" />
      <span class="opt-group-label">{{ group.label || group.id }}</span>
      <UiPill v-if="changedCount" tone="info">已改 {{ changedCount }}</UiPill>
      <span class="opt-group-count">{{ group.options.length }} 项</span>
    </button>

    <div v-show="open" class="opt-group-body">
      <OptionField
        v-for="o in group.options"
        :key="o.name"
        :option="o"
        :value="valueOf(o)"
        :changed="isChanged(o)"
        :disabled="disabled"
        @change="v => emit('change', o, v)"
      />
    </div>
  </section>
</template>
