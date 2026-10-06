<script setup>
// 模组页。
//
// 模组实体的存放是跨房间共享的（-ugc_directory 指向同一份），
// 因此「更新模组」是全局动作：把所有房间启用模组的并集补齐到本地，
// 任意一个房间点效果都一样。服务器启动时带 -skip_update_server_mods，
// 只加载不下载，所以启动前必须先把模组更新到位。

import { computed, onMounted, onUnmounted, ref, watch } from 'vue';

import Icon from '@/components/ui/Icon.vue';
import UiCard from '@/components/ui/UiCard.vue';
import UiEmpty from '@/components/ui/UiEmpty.vue';
import UiPill from '@/components/ui/UiPill.vue';
import UiSkeleton from '@/components/ui/UiSkeleton.vue';
import ModItem from '@/components/mods/ModItem.vue';
import WorkshopPanel from '@/components/mods/WorkshopPanel.vue';
import { useRoomsStore } from '@/stores/rooms.js';
import { modApi, modUpdateApi } from '@/lib/api.js';
import { guard, toast } from '@/lib/toast.js';

const rooms = useRoomsStore();

/** 服务器运行中冻结：模组改动重启后才生效 */
const frozen = computed(() => rooms.running);

const mods = ref([]);
const loading = ref(false);
const loadError = ref('');

/** 已启用但本地还没有的模组数量 */
const missingCount = computed(() =>
  mods.value.filter(m => m.enabled && !m.installed).length);

async function load() {
  const room = rooms.current;
  if (!room) {
    mods.value = [];
    loadError.value = '';
    return;
  }
  loading.value = true;
  loadError.value = '';
  try {
    mods.value = (await modApi.list(room.id)) || [];
  } catch (err) {
    loadError.value = err.message;
  } finally {
    loading.value = false;
  }
}

/* ---------- 模组预更新 ---------- */

const updating = ref(false);
/** 创意工坊面板的展开状态 */
const workshopOpen = ref(false);
const progress = ref('');
const updateError = ref('');
const output = ref([]);
let pollTimer = null;

function stopPoll() {
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

async function pollUpdate() {
  let st;
  try {
    st = await modUpdateApi.status();
  } catch (_) {
    return // 轮询失败不打断，下一轮再试
  }
  updating.value = !!st.running;
  progress.value = st.progress || '';
  updateError.value = st.error || '';
  output.value = st.output || [];

  if (!st.running) {
    stopPoll();
    // 更新结束后按实际结果汇报，绝不把「没下载到东西」说成成功——
    // 引擎可能在任何阶段静默退出且退出码为 0，只有清点结果才算数
    await load();
    const missing = mods.value.filter(m => m.enabled && !m.installed).length;
    // 输出只有「开始…」和结论两行 = 会话没跑引擎，本来就一个都不缺
    const nothingTodo = !st.error && st.output && st.output.length <= 2;
    if (st.error) toast('模组更新失败：' + st.error, 'err');
    else if (nothingTodo) toast('全部模组均已在本地，无需更新');
    else if (missing > 0) {
      toast(`更新结束，但仍有 ${missing} 个模组未下载，请查看下方输出后重试`, 'warn');
    } else {
      toast('模组更新完成');
    }
  }
}

function startPoll() {
  stopPoll();
  pollTimer = setInterval(pollUpdate, 2000);
}

async function startUpdate() {
  const room = rooms.current;
  if (!room || updating.value) return;
  const { ok } = await guard(() => modUpdateApi.start(room.id), { success: '模组更新已开始' });
  if (!ok) return;
  updating.value = true;
  startPoll();
  pollUpdate();
}

/** 单模组更新：ModItem 已做过确认，这里只负责发起会话 */
async function startOne(mod) {
  const room = rooms.current;
  if (!room || updating.value) return;
  const { ok } = await guard(
    () => modUpdateApi.start(room.id, null, mod.id),
    { success: `已开始更新模组 ${mod.name || mod.id}` },
  );
  if (!ok) return;
  updating.value = true;
  startPoll();
  pollUpdate();
}

async function stopUpdate() {
  await guard(() => modUpdateApi.stop(), { success: '已请求中止更新' });
}

onMounted(async () => {
  await load();
  // 面板重启后若上次更新还没结束，恢复轮询而不是丢掉进度
  await pollUpdate();
  if (updating.value) startPoll();
});
onUnmounted(stopPoll);
// 用 current 而不是 currentId 做触发：刷新页面时 currentId 从 localStorage
// 恢复、值不变，watch 不会触发，列表就一直空着；而 current 要等房间列表
// 加载完才从 null 变成房间对象，正好在数据就绪时拉一次模组列表
watch(() => rooms.current, load);
</script>

<template>
  <UiCard>
    <template #title>模组管理</template>
    <template #hint>启用/禁用后需重启服务器才生效；模组实体跨房间共享</template>
    <template #actions>
      <button
        class="btn btn-sm btn-primary"
        :disabled="frozen || !rooms.current || updating"
        @click="startUpdate"
      >
        <Icon name="download" size="14" />{{ updating ? '更新中…' : '更新模组' }}
      </button>
      <button
        class="btn btn-sm"
        :class="{ 'btn-primary': workshopOpen }"
        :disabled="!rooms.current"
        @click="workshopOpen = !workshopOpen"
      >
        <Icon name="globe" size="14" />创意工坊
      </button>
      <button class="btn btn-sm" @click="load">刷新</button>
    </template>

    <UiSkeleton v-if="!rooms.loaded" :rows="4" />
    <UiEmpty v-else-if="!rooms.current" icon="package" text="请先在「房间」页新建一个房间" />
    <UiSkeleton v-else-if="loading" :rows="4" />
    <UiEmpty v-else-if="loadError" icon="alert" :text="'读取模组失败：' + loadError" />

    <template v-else>
      <div v-if="frozen" class="notice" style="margin-bottom: 14px">
        <Icon name="info" size="16" />
        <span>
          服务器运行中，模组修改已冻结。请先在「总览」页
          <strong>关闭服务器</strong>后再启用/禁用模组、修改配置或更新模组。
        </span>
      </div>
      <p v-else-if="missingCount" class="field-tip" style="margin-bottom: 14px">
        有 {{ missingCount }} 个已启用的模组尚未下载，
        点右上角<strong>「更新模组」</strong>补齐后再启动服务器（启动时会做检查）。
      </p>

      <!-- 创意工坊：搜索 / ID 查询，下载走统一更新会话 -->
      <WorkshopPanel
        v-if="workshopOpen"
        :updating="updating"
        @download="startOne"
      />

      <div v-if="updating || output.length" class="mod-update">
        <div class="mod-update-head">
          <b>模组更新</b>
          <UiPill :tone="updating ? 'info' : (updateError ? 'danger' : 'ok')">
            {{ updating ? (progress || '进行中') : (updateError ? '失败' : '完成') }}
          </UiPill>
          <span class="spacer" />
          <button v-if="updating" class="btn btn-sm" @click="stopUpdate">中止</button>
        </div>
        <pre class="mod-update-log">{{ output.join('\n') }}</pre>
      </div>

      <template v-if="mods.length">
        <ModItem
          v-for="m in mods"
          :key="m.id"
          :mod="m"
          :updating="updating"
          @changed="load"
          @update="startOne"
        />
      </template>
      <UiEmpty
        v-else-if="!updating"
        icon="package"
        text="还没有模组。展开上方「创意工坊」搜索或粘贴 ID 下载，下载完成后会出现在这里。"
      />
    </template>
  </UiCard>
</template>
