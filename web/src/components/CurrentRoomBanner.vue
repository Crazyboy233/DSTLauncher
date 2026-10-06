<script setup>
// 当前房间标识条：所有页面共用，明确「现在操作的是哪个房间」。
//
// 放在 App 外壳里（TopBar 下方），而不是每个视图各写一份，
// 避免漏页； Rooms 页也一样显示，保持全局一致。
import { computed } from 'vue';

import Icon from '@/components/ui/Icon.vue';
import UiPill from '@/components/ui/UiPill.vue';
import { useRoomsStore } from '@/stores/rooms.js';

const rooms = useRoomsStore();

const room = computed(() => rooms.current);
const mode = computed(() => room.value?.gameMode || '');
</script>

<template>
  <div v-if="room" class="room-banner">
    <Icon class="room-banner-icon" name="server" size="14" />
    <span class="room-banner-label">当前房间</span>
    <strong class="room-banner-name">{{ room.name }}</strong>
    <span class="room-banner-key">{{ room.key }}<template v-if="mode"> · {{ mode }}</template></span>

    <UiPill class="room-banner-state" :tone="rooms.running ? 'ok' : 'muted'">
      <span class="dot" :class="{ running: rooms.running }" />
      {{ rooms.running ? '运行中' : '已停止' }}
    </UiPill>
  </div>
</template>
