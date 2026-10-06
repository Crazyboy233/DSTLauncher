// 主题：浅色 / 深色 / 跟随系统。
//
// 首屏主题已在 index.html 的内联脚本里定好（防止闪屏），
// 这里只负责后续切换与响应系统偏好。

import { ref, computed } from 'vue';

const KEY = 'dst-theme';
const MODES = ['auto', 'light', 'dark'];

export const MODE_LABEL = { auto: '跟随系统', light: '浅色', dark: '深色' };

const mql = window.matchMedia('(prefers-color-scheme: dark)');

const mode = ref(readSaved());
const systemDark = ref(mql.matches);

/** 实际生效的黑白（auto 会跟随系统）。 */
export const resolved = computed(() =>
  mode.value === 'auto' ? (systemDark.value ? 'dark' : 'light') : mode.value);

function readSaved() {
  try {
    const v = localStorage.getItem(KEY);
    return MODES.includes(v) ? v : 'auto';
  } catch (_) {
    return 'auto';
  }
}

function apply() {
  document.documentElement.dataset.themeMode = mode.value;
  document.documentElement.dataset.theme = resolved.value;
}

/** 按「浅色 → 深色 → 跟随系统」循环。 */
function cycle() {
  mode.value = MODES[(MODES.indexOf(mode.value) + 1) % MODES.length];
  try {
    localStorage.setItem(KEY, mode.value);
  } catch (_) {}
  apply();
  return mode.value;
}

let inited = false;

export function initTheme() {
  if (inited) return;
  inited = true;
  apply();

  const onChange = () => {
    systemDark.value = mql.matches;
    apply();
  };
  // Safari 14 以下只有 addListener
  if (mql.addEventListener) mql.addEventListener('change', onChange);
  else if (mql.addListener) mql.addListener(onChange);
}

export function useTheme() {
  return {
    mode,
    resolved,
    label: computed(() => MODE_LABEL[mode.value]),
    nextLabel: computed(() => MODE_LABEL[MODES[(MODES.indexOf(mode.value) + 1) % MODES.length]]),
    cycle,
  };
}
