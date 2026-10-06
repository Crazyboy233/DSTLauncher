// 展示层的格式化与字典。
//
// Vue 的插值默认就是转义的，所以这里不需要 esc()。

/** 字节数转可读体积。 */
export function fmtSize(bytes) {
  const n = Number(bytes) || 0;
  if (n < 1024) return n + ' B';
  if (n < 1048576) return (n / 1024).toFixed(1) + ' KB';
  if (n < 1073741824) return (n / 1048576).toFixed(1) + ' MB';
  return (n / 1073741824).toFixed(2) + ' GB';
}

/** ISO 时间转本地可读格式。 */
export function fmtTime(iso) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return String(iso || '');
  return d.toLocaleString('zh-CN', { hour12: false });
}

/** 分片运行状态的中文名。 */
export const STATE_LABEL = {
  running: '运行中',
  starting: '启动中',
  stopping: '停止中',
  stopped: '已停止',
  crashed: '已崩溃',
};

/** 游戏模式的中文名（英文值同时保留，方便对照 ini）。 */
export const GAME_MODE_LABEL = {
  survival: '生存 survival',
  endless: '无尽 endless',
  wilderness: '荒野 wilderness',
};

export const TICK_RATE_LABEL = {
  15: '15（省 CPU）',
  30: '30（标准）',
  60: '60（流畅）',
};

export const LANGUAGE_LABEL = {
  zh: '中文 zh',
  en: 'English',
};

/** 进程事件类型 -> 展示用前缀，undefined 表示按普通日志处理。 */
export const EVENT_LABEL = {
  start: '启动',
  stop: '停止',
  crash: '崩溃',
  restart: '重启',
  ready: '就绪',
  warn: '警告',
};
