<script setup>
// 应用外壳：布局、视图切换、轮询与日志连接的编排。
//
// 各视图自己负责加载「进入时」的数据；这里只管与时间相关的部分：
// 状态轮询、房间列表刷新、日志 SSE 连接。

import { computed, onMounted, onUnmounted, watch } from 'vue';

import AppSidebar from '@/components/AppSidebar.vue';
import TopBar from '@/components/TopBar.vue';
import CurrentRoomBanner from '@/components/CurrentRoomBanner.vue';
import ToastHost from '@/components/ToastHost.vue';

import OverviewView from '@/views/OverviewView.vue';
import RoomsView from '@/views/RoomsView.vue';
import WorldView from '@/views/WorldView.vue';
import ModsView from '@/views/ModsView.vue';
import BackupsView from '@/views/BackupsView.vue';
import ConfigView from '@/views/ConfigView.vue';

import { useUiStore } from '@/stores/ui.js';
import { useRoomsStore } from '@/stores/rooms.js';
import { useLogs } from '@/composables/useLogs.js';
import { guard } from '@/lib/toast.js';

/** 标签 key -> 视图组件。加新页面时在这里登记一行。 */
const VIEWS = {
  overview: OverviewView,
  rooms: RoomsView,
  world: WorldView,
  mods: ModsView,
  backups: BackupsView,
  config: ConfigView,
};

/** 兜底：万一 activeTab 与 VIEWS 不一致，也不会渲染出空白页。 */
const currentView = computed(() => VIEWS[ui.activeTab] || OverviewView);

const ui = useUiStore();
const rooms = useRoomsStore();
const logs = useLogs();

let statusTimer = null;
let roomsTimer = null;
let lastRoomId = undefined;

// 换房间要重连日志；启停洞穴会改变分片列表，也要跟着增删对应分片的连接
watch(
  [() => rooms.currentId, () => rooms.shards.join(',')],
  () => {
    // 只有真正换了房间才清空日志，否则切换洞穴开关会把当前房间的日志也清掉
    if (rooms.currentId !== lastRoomId) {
      logs.clearAll();
      lastRoomId = rooms.currentId;
    }
    logs.openRoom(rooms.currentId, rooms.shards);
    rooms.refreshStatus().catch(() => {});
  },
);

onMounted(async () => {
  // 首次加载会设置 currentId，从而触发上面的 watcher 完成状态刷新与日志连接
  await guard(() => rooms.load());

  // 表单枚举值失败不影响使用，不阻塞进入面板。
  // 安装状态由概览页的 InstallPanel 自己拉取，这里不重复请求。
  guard(() => rooms.loadMeta());

  statusTimer = setInterval(() => rooms.refreshStatus().catch(() => {}), 3000);
  roomsTimer = setInterval(() => rooms.load().catch(() => {}), 15000);
});

onUnmounted(() => {
  clearInterval(statusTimer);
  clearInterval(roomsTimer);
  logs.disconnect();
});
</script>

<template>
  <div class="shell">
    <AppSidebar />

    <div class="main">
      <TopBar />
      <!-- 全局当前房间标识：每个页面都能看到正在操作的是哪个房间 -->
      <CurrentRoomBanner />

      <main class="page">
        <!-- 只用入场淡入，不加 mode="out-in"：
             连续快速切换时 out-in 需要等待离场动画结束，中断会让新旧元素都不在 DOM 里，
             表现为右侧内容消失。入场动画被重复触发是安全的。
             另外视图组件必须是单一根节点（见 OverviewView 顶部注释）。 -->
        <Transition name="fade">
          <component :is="currentView" :key="ui.activeTab" />
        </Transition>
      </main>
    </div>
  </div>

  <ToastHost />
</template>
