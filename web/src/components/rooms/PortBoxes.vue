<script setup>
// 分片端口编辑区。
//
// 直接对 props 里的分片对象做 v-model：props 是浅只读，改嵌套字段是允许的，
// 且这些对象就是 RoomEditor 表单状态本身，不需要再绕一层 emit。

import UiField from '@/components/ui/UiField.vue';

defineProps({
  shards: { type: Array, required: true },
  cavesEnabled: { type: Boolean, default: true },
});

const TITLE = { Master: '地表 Master', Caves: '洞穴 Caves' };

const FIELDS = [
  { key: 'serverPort', label: '游戏端口' },
  { key: 'masterServerPort', label: 'Steam 主端口' },
  { key: 'authenticationPort', label: 'Steam 认证口' },
];
</script>

<template>
  <div class="ports">
    <div v-for="s in shards" :key="s.name" class="port-box">
      <h4>
        {{ TITLE[s.name] || s.name }}
        <span v-if="s.name === 'Caves' && !cavesEnabled" class="pill pill-muted">未启用</span>
      </h4>
      <UiField
        v-for="f in FIELDS"
        :key="f.key"
        :label="f.label"
        :for-id="`p_${s.name}_${f.key}`"
      >
        <input
          :id="`p_${s.name}_${f.key}`"
          v-model.number="s[f.key]"
          class="input"
          type="number"
          min="1024"
          max="65535"
        >
      </UiField>
    </div>
  </div>
</template>
