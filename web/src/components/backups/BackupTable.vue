<script setup>
// 备份列表表格。纯展示，动作交给父组件。

import { fmtSize, fmtTime } from '@/lib/format.js';

defineProps({
  items: { type: Array, required: true },
});

defineEmits(['restore', 'remove', 'export']);
</script>

<template>
  <div class="table-wrap">
    <table class="table">
      <thead>
        <tr>
          <th>时间</th>
          <th>大小</th>
          <th class="right">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="b in items" :key="b.name">
          <td>{{ fmtTime(b.createdAt) }}</td>
          <td class="num">{{ fmtSize(b.size) }}</td>
          <td class="right">
            <button class="btn btn-sm" @click="$emit('restore', b.name)">回滚</button>
            <button class="btn btn-sm" @click="$emit('export', b.name)">导出</button>
            <button class="btn btn-sm btn-danger" @click="$emit('remove', b.name)">删除</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
