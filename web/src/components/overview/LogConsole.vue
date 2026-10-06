<script setup>
// 实时日志容器：为每个启用中的分片渲染一个独立面板。
//
// 分片数量由房间决定（未开启洞穴时只有 Master），因此这里是动态列表，
// 而不是写死两个框。

import UiCard from '@/components/ui/UiCard.vue';
import UiEmpty from '@/components/ui/UiEmpty.vue';
import LogPane from '@/components/overview/LogPane.vue';
import { useRoomsStore } from '@/stores/rooms.js';

const rooms = useRoomsStore();
</script>

<template>
  <UiCard flush>
    <template #title>实时日志</template>
    <template #hint>地表与洞穴是独立进程，日志分屏查看</template>

    <div v-if="!rooms.shards.length" class="log-empty">
      <UiEmpty icon="inbox" text="还没有可用的房间。先在「房间」页新建一个，再回来看日志。" />
    </div>

    <div v-else class="log-grid">
      <LogPane v-for="s in rooms.shards" :key="s" :shard="s" />
    </div>
  </UiCard>
</template>
