<script setup>
// 内联 SVG 图标集（线性风格，24x24 网格，跟随 currentColor）。
//
// 用集中定义而不是散落的 <svg>：既能统一线宽与视觉风格，
// 也避免同一图标在多个组件里各写一份路径。

import { computed } from 'vue';

const ICONS = {
  // 品牌
  tent: '<path d="M12 3l7.5 9.5L12 21.5 4.5 12z"/><path d="M12 8.5v8"/>',

  // 导航
  dashboard: '<rect x="3.5" y="3.5" width="7.2" height="7.2" rx="2"/><rect x="13.3" y="3.5" width="7.2" height="7.2" rx="2"/><rect x="3.5" y="13.3" width="7.2" height="7.2" rx="2"/><rect x="13.3" y="13.3" width="7.2" height="7.2" rx="2"/>',
  server: '<rect x="3" y="4" width="18" height="7" rx="2"/><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 7.5h.01M7 16.5h.01"/>',
  package: '<path d="M12 3l8 4.5v9L12 21l-8-4.5v-9z"/><path d="M4 7.5l8 4.5 8-4.5"/><path d="M12 12v9"/>',
  archive: '<rect x="3" y="4" width="18" height="4.5" rx="1.5"/><path d="M5 8.5V19a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8.5"/><path d="M10 12.5h4"/>',
  fileCode: '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/><path d="M10 12.5l-2 2 2 2M14 12.5l2 2-2 2"/>',

  // 主题
  sun: '<circle cx="12" cy="12" r="4.2"/><path d="M12 2.5v2M12 19.5v2M2.5 12h2M19.5 12h2M5.2 5.2l1.4 1.4M17.4 17.4l1.4 1.4M18.8 5.2l-1.4 1.4M6.6 17.4l-1.4 1.4"/>',
  moon: '<path d="M20 14.5A8.5 8.5 0 1 1 9.5 4a6.8 6.8 0 0 0 10.5 10.5z"/>',

  // 操作
  play: '<path d="M7.5 5l11.5 7-11.5 7z"/>',
  stop: '<rect x="6.5" y="6.5" width="11" height="11" rx="2"/>',
  restart: '<path d="M20 12a8 8 0 1 1-2.4-5.7"/><path d="M20 3.8v4.7h-4.7"/>',
  refresh: '<path d="M21 12a9 9 0 1 1-2.6-6.4"/><path d="M21 3.5V9h-5.5"/>',
  plus: '<path d="M12 5.5v13M5.5 12h13"/>',
  pencil: '<path d="M4 20h4L19 9a2.1 2.1 0 0 0-3-3L5 17v3z"/><path d="M14.5 6.5l3 3"/>',
  trash: '<path d="M4 7h16"/><path d="M9.5 7V4.5h5V7"/><path d="M6.5 7l.9 12.5a1 1 0 0 0 1 .95h7.2a1 1 0 0 0 1-.95L17.5 7"/>',
  check: '<path d="M4.5 12.5l5 5 10-11"/>',
  download: '<path d="M12 4v12.5"/><path d="M7.5 12l4.5 4.5L16.5 12"/><path d="M4.5 19.5h15"/>',
  upload: '<path d="M12 16.5V4"/><path d="M7.5 8.5L12 4l4.5 4.5"/><path d="M4.5 15v3.5a1.5 1.5 0 0 0 1.5 1.5h12a1.5 1.5 0 0 0 1.5-1.5V15"/>',
  chevron: '<path d="M9.5 6l6 6-6 6"/>',
  x: '<path d="M6.5 6.5l11 11M17.5 6.5l-11 11"/>',
  alert: '<path d="M12 4l8.5 15H3.5z"/><path d="M12 10v4M12 17h.01"/>',
  info: '<circle cx="12" cy="12" r="8.5"/><path d="M12 11v5M12 8h.01"/>',

  // 数据
  terminal: '<rect x="3" y="4.5" width="18" height="15" rx="2"/><path d="M7.5 10l2.5 2.5-2.5 2.5M13 15h4"/>',
  activity: '<path d="M3 12h3.5l2.5-6.5 3 13 2.5-6.5H21"/>',
  cpu: '<rect x="4.5" y="4.5" width="15" height="15" rx="3"/><rect x="9.5" y="9.5" width="5" height="5" rx="1"/><path d="M9.5 2.5v2M14.5 2.5v2M9.5 19.5v2M14.5 19.5v2M2.5 9.5h2M2.5 14.5h2M19.5 9.5h2M19.5 14.5h2"/>',
  key: '<circle cx="8" cy="12" r="3.5"/><path d="M11.5 12H21"/><path d="M17 12v3.5M14 12v2.5"/>',
  globe: '<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17"/><path d="M12 3.5c2.2 2.4 3.3 5.3 3.3 8.5S14.2 18.1 12 20.5c-2.2-2.4-3.3-5.3-3.3-8.5S9.8 5.9 12 3.5z"/>',
  users: '<circle cx="9" cy="8" r="3.2"/><path d="M3.2 20c0-3.2 2.6-5.8 5.8-5.8s5.8 2.6 5.8 5.8"/><path d="M16 5.4a3.2 3.2 0 0 1 0 5.2M17.6 14.4A5.8 5.8 0 0 1 20.8 20"/>',
  clock: '<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3.2 2"/>',
  inbox: '<path d="M3.2 13h4.6l1 2.6h6.4l1-2.6h4.6"/><path d="M5.6 4.8h12.8l2.4 8.2V19H3.2v-6z"/>',
};

const props = defineProps({
  name: { type: String, required: true },
  /** 像素尺寸，传数字即可 */
  size: { type: [Number, String], default: 18 },
});

// 图标内容是文件内的常量，不含任何外部输入，v-html 在这里是安全的
const inner = computed(() => ICONS[props.name] || '');
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html -- 常量图标路径，无外部输入 -->
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.7"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    v-html="inner"
  />
</template>
