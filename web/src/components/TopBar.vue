<script setup>
// 顶栏：当前页面标题、房间选择器、运行状态、主题切换。

import { computed } from 'vue';

import Icon from '@/components/ui/Icon.vue';
import UiPill from '@/components/ui/UiPill.vue';
import { useRoomsStore } from '@/stores/rooms.js';
import { useUiStore } from '@/stores/ui.js';
import { useTheme } from '@/composables/useTheme.js';
import { toast } from '@/lib/toast.js';
import { GAME_MODE_LABEL } from '@/lib/format.js';

const rooms = useRoomsStore();
const ui = useUiStore();
const theme = useTheme();

const pageTitle = computed(() => {
  const t = ui.tabs.find(x => x.key === ui.activeTab);
  return t ? t.label : '控制台';
});

const pageSub = computed(() => {
  const r = rooms.current;
  if (!r) return '还没有房间';
  const mode = GAME_MODE_LABEL[r.gameMode] || r.gameMode;
  return `${r.key} · ${mode} · ${r.maxPlayers} 人`;
});

const stateTone = computed(() => (rooms.running ? 'ok' : 'muted'));

const themeTitle = computed(() => {
  const shown = theme.resolved.value === 'dark' ? '深色' : '浅色';
  return `当前：${theme.label.value}（显示为${shown}）\n点击切换为：${theme.nextLabel.value}`;
});

function onRoomChange(e) {
  rooms.select(e.target.value);
}

function onTheme() {
  toast(`主题：${theme.cycle()}`);
}
</script>

<template>
  <header class="topbar">
    <div class="topbar-title">
      <h1>{{ pageTitle }}</h1>
      <span>{{ pageSub }}</span>
    </div>

    <div class="spacer" />

    <UiPill :tone="stateTone">
      <Icon :name="rooms.running ? 'activity' : 'stop'" size="12" />
      {{ rooms.running ? '运行中' : '已停止' }}
    </UiPill>

    <div class="topbar-field">
      <label for="roomSelect">房间</label>
      <select id="roomSelect" class="select" :value="rooms.currentId ?? ''" @change="onRoomChange">
        <option v-if="!rooms.list.length" value="">（暂无房间）</option>
        <option v-for="r in rooms.list" :key="r.id" :value="r.id">
          {{ r.name }}
        </option>
      </select>
    </div>

    <button class="btn btn-icon" :title="themeTitle" aria-label="切换主题" @click="onTheme">
      <Icon class="icon-sun" name="sun" size="18" />
      <Icon class="icon-moon" name="moon" size="18" />
    </button>
  </header>
</template>
