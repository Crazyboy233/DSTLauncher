<script setup>
// 房间页：列表 + 新建 / 编辑表单。

import { computed, onMounted, ref } from 'vue';

import UiCard from '@/components/ui/UiCard.vue';
import Icon from '@/components/ui/Icon.vue';
import RoomGrid from '@/components/rooms/RoomGrid.vue';
import RoomEditor from '@/components/rooms/RoomEditor.vue';
import RoomAdmins from '@/components/rooms/RoomAdmins.vue';
import SaveUploadPanel from '@/components/rooms/SaveUploadPanel.vue';
import { useRoomsStore } from '@/stores/rooms.js';
import { guard, toast } from '@/lib/toast.js';

const rooms = useRoomsStore();

/**
 * 编辑器的打开状态。
 * key 用时间戳：切换编辑对象时强制重建 RoomEditor，省掉一堆 watch 同步逻辑。
 */
const editor = ref({ open: false, roomId: null, key: 0 });

/**
 * 上传存档面板。key 同理用于强制重建：
 * 每次打开都是一次全新的导入，旧的进度与摘要不该残留。
 */
const uploader = ref({ open: false, roomId: null, key: 0 });

const editingRoom = computed(() => {
  if (!editor.value.roomId) return null;
  return rooms.list.find(r => r.id === editor.value.roomId) || null;
});

onMounted(async () => {
  await guard(() => rooms.load());
  if (!rooms.meta) await guard(() => rooms.loadMeta());
});

function reload() {
  guard(() => rooms.load());
}

function openNew() {
  editor.value = { open: true, roomId: null, key: Date.now() };
}

function openEdit(id) {
  editor.value = { open: true, roomId: Number(id), key: Date.now() };
}

function close() {
  editor.value.open = false;
}

function onSelect(id) {
  rooms.select(id);
}

/** 上传存档：roomId 为空表示「新建房间」，有值表示覆盖该房间 */
function openUpload(roomId = null) {
  uploader.value = { open: true, roomId: roomId ? Number(roomId) : null, key: Date.now() };
}

function closeUpload() {
  uploader.value.open = false;
}

/** 导入成功后房间配置已被改写，重新拉一次列表让卡片与表单跟上 */
async function onUploaded() {
  await guard(() => rooms.load());
}

async function onRemove(id) {
  const room = rooms.list.find(r => r.id === Number(id));
  const name = room ? room.name : '房间';
  if (!confirm(`删除「${name}」？\n\n只会从面板移除，磁盘上的存档目录会保留。`)) return;

  const { ok, data } = await guard(() => rooms.remove(id));
  if (!ok) return;
  close();
  toast((data && data.message) || '房间已删除');
}
</script>

<template>
  <div class="stack">
    <UiCard>
      <template #title>房间列表</template>
      <template #hint>一个房间 = 一套存档（Cluster_N），可各自启停与备份</template>
      <template #actions>
        <button class="btn btn-sm btn-primary" @click="openNew">
          <Icon name="plus" size="14" />新建房间
        </button>
        <button class="btn btn-sm" @click="openUpload()">
          <Icon name="upload" size="14" />上传存档
        </button>
        <button class="btn btn-sm" @click="reload">
          <Icon name="refresh" size="14" />刷新
        </button>
      </template>

      <RoomGrid
        :rooms="rooms.list"
        :current-id="rooms.currentId"
        @select="onSelect"
        @edit="openEdit"
        @remove="onRemove"
        @upload="openUpload"
      />
    </UiCard>

    <RoomAdmins />

    <SaveUploadPanel
      v-if="uploader.open"
      :key="uploader.key"
      :rooms="rooms.list"
      :preset-room-id="uploader.roomId"
      :current-room-id="rooms.currentId"
      @close="closeUpload"
      @done="onUploaded"
    />

    <RoomEditor
      v-if="editor.open"
      :key="editor.key"
      :room="editingRoom"
      :meta="rooms.meta"
      @close="close"
      @saved="close"
    />
  </div>
</template>
