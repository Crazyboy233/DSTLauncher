<script setup>
// 房间卡片网格。只负责布局与事件透传，业务动作由 RoomsView 处理。

import RoomCard from '@/components/rooms/RoomCard.vue';

defineProps({
  rooms: { type: Array, required: true },
  // 允许为 null：没有选中房间时传空
  currentId: { type: Number, default: null },
});

defineEmits(['select', 'edit', 'remove', 'upload']);
</script>

<template>
  <div v-if="!rooms.length" class="empty">还没有房间，点右上角「新建房间」开始</div>
  <div v-else class="room-grid">
    <RoomCard
      v-for="r in rooms"
      :key="r.id"
      :room="r"
      :selected="r.id === currentId"
      @select="$emit('select', $event)"
      @edit="$emit('edit', $event)"
      @remove="$emit('remove', $event)"
      @upload="$emit('upload', $event)"
    />
  </div>
</template>
