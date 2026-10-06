<script setup>
// 左侧边栏：品牌、导航、底部状态摘要。
//
// 相比横向标签栏，侧边栏能给后面要加的「世界设置」「玩家管理」留出扩展空间，
// 也不会随着标签变多把顶部挤满。

import { computed } from 'vue';
import Icon from '@/components/ui/Icon.vue';
import { useUiStore } from '@/stores/ui.js';
import { useSystemStore } from '@/stores/system.js';
import { useRoomsStore } from '@/stores/rooms.js';
import { useLogs } from '@/composables/useLogs.js';

const ui = useUiStore();
const system = useSystemStore();
const rooms = useRoomsStore();
const { connected } = useLogs();

// 面板版本号：重建 exe 后看一眼这里，就能确认「跑的是哪个构建」，
// 免得再出现「后端明明更新了、页面行为还是老的」这种排查黑洞。
const panelVersion = computed(() => rooms.meta?.panelVersion || '');

const serverText = computed(() => {
  if (system.installing) return '正在安装…';
  if (!system.installed) return '未安装服务器';
  return '版本 ' + (system.version || '未知');
});
</script>

<template>
  <aside class="sidebar">
    <div class="sidebar-brand">
      <span class="brand-mark" aria-hidden="true">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"
             stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 3l7 9-7 9-7-9z" />
          <path d="M12 8v8" />
        </svg>
      </span>
      <div class="brand-text">
        <div class="brand-title">DST 开服控制台</div>
        <div class="brand-sub">Windows 原生</div>
      </div>
    </div>

    <nav class="nav">
      <div class="nav-label">管理</div>
      <button
        v-for="t in ui.tabs"
        :key="t.key"
        class="nav-item"
        :class="{ 'is-active': ui.activeTab === t.key }"
        @click="ui.select(t.key)"
      >
        <Icon class="nav-icon" :name="t.icon" />
        <span>{{ t.label }}</span>
      </button>
    </nav>

    <div class="sidebar-foot">
      <span>日志 {{ connected ? '已连接' : '未连接' }}</span>
      <span v-if="panelVersion" title="面板版本号，重建后可据此确认跑的是新构建">面板 {{ panelVersion }}</span>
      <span>{{ serverText }}</span>
    </div>
  </aside>
</template>
