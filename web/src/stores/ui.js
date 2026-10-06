// 界面外壳状态：当前标签页。
//
// 标签清单也放在这里，以后加「世界设置」「玩家管理」只要往数组里加一项，
// 侧边栏导航与面板切换都会自动跟上。icon 对应 components/ui/Icon.vue 里的名字。

import { defineStore } from 'pinia';
import { ref, watch } from 'vue';

/** activeTab 的本地记忆 key，与房间选择的记忆方式保持一致。 */
const TAB_KEY = 'dst-tab';

function readStoredTab(validKeys) {
  try {
    const v = localStorage.getItem(TAB_KEY);
    return validKeys.includes(v) ? v : '';
  } catch (_) {
    return '';
  }
}

function storeTab(key) {
  try {
    if (key) localStorage.setItem(TAB_KEY, key);
    else localStorage.removeItem(TAB_KEY);
  } catch (_) {
    // 写不了只是刷新后记不住选择，不影响功能
  }
}

export const useUiStore = defineStore('ui', () => {
  /** 数组顺序即导航顺序。key 同时决定组件映射（见 App.vue）。 */
  const tabs = [
    { key: 'overview', label: '概览', icon: 'dashboard' },
    { key: 'rooms', label: '房间', icon: 'server' },
    { key: 'world', label: '世界配置', icon: 'globe' },
    { key: 'mods', label: '模组', icon: 'package' },
    { key: 'backups', label: '备份', icon: 'archive' },
    { key: 'config', label: '配置预览', icon: 'fileCode' },
  ];

  // 初值取上次的选择；存了不认识的旧 key（版本迭代删过页面）时回落到概览
  const activeTab = ref(readStoredTab(tabs.map(t => t.key)) || 'overview');

  // 与 rooms.js 的房间记忆同理：切换就落盘，刷新后停在原页面
  watch(activeTab, storeTab);

  function select(key) {
    if (tabs.some(t => t.key === key)) activeTab.value = key;
  }

  return { tabs, activeTab, select };
});
