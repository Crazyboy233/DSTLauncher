<script setup>
// 右下角提示条渲染。点一下可以提前关掉。

import Icon from '@/components/ui/Icon.vue';
import { useToasts, dismiss } from '@/lib/toast.js';

const items = useToasts();

const ICON = { err: 'alert', warn: 'alert' };
const iconOf = kind => ICON[kind] ?? 'check';
</script>

<template>
  <div class="toast-host">
    <TransitionGroup name="toast">
      <div
        v-for="t in items"
        :key="t.id"
        class="toast"
        :class="t.kind ? 'is-' + t.kind : ''"
        @click="dismiss(t.id)"
      >
        <Icon :name="iconOf(t.kind)" size="16" />
        <span>{{ t.message }}</span>
      </div>
    </TransitionGroup>
  </div>
</template>
