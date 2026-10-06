<script setup>
// 配置预览页。
//
// 这里刻意只读：房间表单是唯一事实来源，同时允许手改 ini
// 会让两边互相覆盖，用户无法判断哪份才是最新的。

import { onMounted, ref, watch } from 'vue';

import UiCard from '@/components/ui/UiCard.vue';
import UiEmpty from '@/components/ui/UiEmpty.vue';
import UiSkeleton from '@/components/ui/UiSkeleton.vue';
import ConfigFiles from '@/components/config/ConfigFiles.vue';
import { useRoomsStore } from '@/stores/rooms.js';
import { configApi } from '@/lib/api.js';

const rooms = useRoomsStore();

const files = ref([]);
const clusterDir = ref('');
const loading = ref(false);
const loadError = ref('');

async function load() {
  const room = rooms.current;
  if (!room) {
    files.value = [];
    clusterDir.value = '';
    return;
  }
  loading.value = true;
  loadError.value = '';
  try {
    const data = await configApi.preview(room.id);
    files.value = data.files || [];
    clusterDir.value = data.clusterDir || '';
  } catch (err) {
    loadError.value = err.message;
  } finally {
    loading.value = false;
  }
}

onMounted(load);
// 用 current 而不是 currentId 做触发（原因见 ModsView）：currentId 刷新后
// 值不变 watch 不到，current 要等房间列表加载完才从 null 变成房间对象
watch(() => rooms.current, load);
</script>

<template>
  <UiCard>
    <template #title>配置预览</template>
    <template #hint>cluster.ini / server.ini 由「房间」表单生成，这里只读</template>
    <template #actions>
      <button class="btn btn-sm" @click="load">刷新</button>
    </template>

    <p v-if="clusterDir" class="field-tip" style="margin-bottom: 14px">
      存档目录：{{ clusterDir }}
    </p>

    <UiSkeleton v-if="!rooms.loaded" :rows="5" />
    <UiEmpty v-else-if="!rooms.current" icon="fileCode" text="请先在「房间」页新建一个房间" />
    <UiSkeleton v-else-if="loading" :rows="5" />
    <UiEmpty v-else-if="loadError" icon="alert" :text="'读取失败：' + loadError" />
    <UiEmpty v-else-if="!files.length" icon="fileCode" text="尚未生成配置文件" />
    <ConfigFiles v-else :files="files" />
  </UiCard>
</template>
