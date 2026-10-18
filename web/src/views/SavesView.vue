<script setup>
// 存档管理：列出磁盘上的全部存档目录（Cluster_*）。
//
// 删除房间只删定义、不删存档，所以这里会看到一批「房间已删除」的孤儿存档。
// 只有孤儿能在这里删——房间还在的存档必须先到「房间」页删房间，
// 否则就太容易把正在玩的存档误删了。

import { computed, onMounted, ref } from 'vue';

import UiCard from '@/components/ui/UiCard.vue';
import UiEmpty from '@/components/ui/UiEmpty.vue';
import UiSkeleton from '@/components/ui/UiSkeleton.vue';
import { savesApi } from '@/lib/api.js';
import { guard, toast } from '@/lib/toast.js';
import { fmtSize, fmtTime } from '@/lib/format.js';

const items = ref([]);
const root = ref('');
const loading = ref(false);
const loadError = ref('');

const totalSize = computed(() => items.value.reduce((n, x) => n + (x.size || 0), 0));
const orphans = computed(() => items.value.filter(x => !x.roomExists));
const orphanSize = computed(() => orphans.value.reduce((n, x) => n + (x.size || 0), 0));

async function load() {
  loading.value = true;
  loadError.value = '';
  try {
    const data = (await savesApi.list()) || {};
    items.value = data.items || [];
    root.value = data.root || '';
  } catch (err) {
    loadError.value = err.message;
    items.value = [];
  } finally {
    loading.value = false;
  }
}

onMounted(load);

function shardNames(item) {
  return (item.shards || []).map(s => s.name).join('、') || '—';
}

async function remove(item) {
  if (item.roomExists) {
    toast('该房间还在面板里，请先在「房间」页删除房间', 'warn');
    return;
  }
  if (item.running) {
    toast('该存档仍被运行中的进程占用，请先停服', 'warn');
    return;
  }

  const ok = confirm(
    `永久删除 ${item.cluster}？\n\n`
    + `分片：${shardNames(item)}\n`
    + `占用：${fmtSize(item.size)}\n`
    + `最后修改：${fmtTime(item.modTime)}\n\n`
    + '删除后无法恢复。想保留的话请先取消，把上面显示的目录整个复制到别处。',
  );
  if (!ok) return;

  const { ok: done } = await guard(
    () => savesApi.remove(item.cluster),
    { success: '存档已删除' },
  );
  if (done) load();
}
</script>

<template>
  <UiCard>
    <template #title>存档管理</template>
    <template #hint>磁盘上的全部存档目录</template>
    <template #actions>
      <button class="btn btn-sm" @click="load">刷新</button>
    </template>

    <div class="save-summary">
      <span>共 <b>{{ items.length }}</b> 个存档</span>
      <span>占用 <b>{{ fmtSize(totalSize) }}</b></span>
      <span v-if="orphans.length">
        孤儿存档 <b>{{ orphans.length }}</b> 个 · <b>{{ fmtSize(orphanSize) }}</b>
      </span>
    </div>

    <p v-if="root" class="field-tip" style="margin-bottom: 14px">
      存档根目录：<code>{{ root }}</code>。删除房间不会删这里的文件，
      没有房间认领的存档只能在本页清理。
    </p>

    <UiSkeleton v-if="loading" :rows="3" />
    <UiEmpty v-else-if="loadError" icon="alert" :text="'读取失败：' + loadError" />
    <UiEmpty v-else-if="!items.length" icon="folder" text="磁盘上还没有任何存档" />

    <div v-else class="table-wrap">
      <table class="table">
        <thead>
          <tr>
            <th>存档目录</th>
            <th>房间</th>
            <th>分片</th>
            <th>大小</th>
            <th>最后修改</th>
            <th class="right">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in items" :key="s.cluster">
            <td>
              <code>{{ s.cluster }}</code>
              <span v-if="s.running" class="pill pill-info" style="margin-left: 6px">运行中</span>
            </td>
            <td>
              <template v-if="s.roomExists">{{ s.roomName }}</template>
              <span v-else class="save-orphan">房间已删除</span>
            </td>
            <td>
              <span v-if="!s.hasSave" class="field-tip">未生成世界</span>
              <template v-else>{{ shardNames(s) }}</template>
            </td>
            <td class="num">{{ fmtSize(s.size) }}</td>
            <td>{{ fmtTime(s.modTime) }}</td>
            <td class="right">
              <button
                class="btn btn-sm btn-danger"
                :disabled="s.roomExists || s.running"
                :title="s.roomExists ? '房间还在，请先在房间页删除房间'
                  : (s.running ? '正在运行，请先停服' : '永久删除这份存档')"
                @click="remove(s)"
              >
                删除
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </UiCard>
</template>
