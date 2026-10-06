<script setup>
// 房间管理员：维护 adminlist.txt（每行一个 Klei ID）。
//
// 名单在服务器启动时读取——运行中修改不拦着，但如实提示需重启生效。
// 组件挂房间页，操作对象是「当前选中」的房间（与顶部当前房间标识一致）。

import { onMounted, ref, watch } from 'vue';

import Icon from '@/components/ui/Icon.vue';
import UiCard from '@/components/ui/UiCard.vue';
import UiEmpty from '@/components/ui/UiEmpty.vue';
import { adminApi } from '@/lib/api.js';
import { useRoomsStore } from '@/stores/rooms.js';
import { guard, toast } from '@/lib/toast.js';

const rooms = useRoomsStore();

const admins = ref([]);
const file = ref('');
const needRestart = ref(false);
const loading = ref(false);
const loadError = ref('');
const input = ref('');
const busy = ref(false);

async function load() {
  const room = rooms.current;
  if (!room) {
    admins.value = [];
    loadError.value = '';
    return;
  }
  loading.value = true;
  loadError.value = '';
  try {
    const data = await adminApi.list(room.id);
    admins.value = data.admins || [];
    file.value = data.file || '';
    needRestart.value = !!data.needRestart;
  } catch (err) {
    loadError.value = err.message;
  } finally {
    loading.value = false;
  }
}

onMounted(load);
watch(() => rooms.currentId, load);

async function add() {
  const room = rooms.current;
  const id = input.value.trim();
  if (!room || !id) return;
  if (!/^KU_[A-Za-z0-9]{1,32}$/i.test(id)) {
    toast('Klei ID 不合法：应为 KU_ 开头、后接字母数字', 'err');
    return;
  }

  busy.value = true;
  const { ok, data } = await guard(() => adminApi.add(room.id, id));
  busy.value = false;
  if (!ok) return;
  admins.value = data.admins;
  needRestart.value = !!data.needRestart;
  input.value = '';
  toast(`已添加管理员 ${id.toUpperCase().slice(0, 8)}…（重启后生效）`);
}

async function remove(id) {
  const room = rooms.current;
  if (!room) return;
  if (!confirm(`移除管理员 ${id}？`)) return;

  const { ok, data } = await guard(() => adminApi.remove(room.id, id));
  if (!ok) return;
  admins.value = data.admins;
  needRestart.value = !!data.needRestart;
  toast('已移除（重启后生效）');
}
</script>

<template>
  <UiCard>
    <template #title>房间管理员</template>
    <template #hint>写入 adminlist.txt；名单在服务器启动时读取，修改后需重启房间生效</template>

    <p v-if="file" class="path-line" style="margin-bottom: 12px">{{ file }}</p>

    <UiEmpty v-if="!rooms.current" icon="users" text="请先在「房间」页新建一个房间" />
    <p v-else-if="loadError" class="field-tip">读取失败：{{ loadError }}</p>

    <template v-else>
      <div class="inline-row" style="max-width: 560px">
        <input
          v-model="input"
          class="input mono"
          placeholder="输入 Klei ID（KU_ 开头）"
          autocomplete="off"
          spellcheck="false"
          @keydown.enter="add"
        >
        <button class="btn btn-primary" :disabled="busy || !input.trim()" @click="add">
          <Icon name="plus" size="14" />添加
        </button>
      </div>
      <p class="field-tip" style="margin-top: 6px">
        游戏内按 ` 反引号打开控制台，输入 TheNet:GetUserID() 可查看自己的 Klei ID。
      </p>

      <div v-if="needRestart" class="notice" style="margin-top: 12px">
        <Icon name="info" size="16" />
        <span>房间正在运行：名单的增删将在<strong>下次重启房间</strong>后生效。</span>
      </div>

      <div v-if="admins.length" class="admin-list">
        <div v-for="id in admins" :key="id" class="admin-row">
          <Icon class="admin-row-icon" name="users" size="14" />
          <span class="mono">{{ id }}</span>
          <button class="btn btn-sm btn-danger" @click="remove(id)">移除</button>
        </div>
      </div>
      <UiEmpty v-else icon="users" text="暂无管理员。房主（服务器持有者）默认就是管理员" />
    </template>
  </UiCard>
</template>
