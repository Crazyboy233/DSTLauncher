// 实时日志流（SSE）。
//
// 每个分片一条独立连接、独立缓冲区：Master（地表）与 Caves（洞穴）是两个
// 互不相干的进程，日志混在一条列表里既看不清，也没法单独清屏或单独发命令。
//
// 做成模块级单例：日志要跨标签页存活，切到「房间」再切回来不能丢历史，
// 因此连接与缓冲区都不挂在某个组件上。

import { computed, reactive, ref } from 'vue';
import { logApi } from '@/lib/api.js';

// 每个分片各自的上限：超过就丢最早的，避免长时间挂机把内存吃满
const MAX_LINES = 4000;
// 建立连接时回补的历史行数
const HISTORY_LINES = 200;
// 批量落账间隔（毫秒）。
//
// 启动阶段 DST 每秒能吐几百行日志。若每行都触发一次响应式更新加一次滚动
// 定位，主线程会被整页重排打满，表现就是整个面板近乎卡死。
// 收敛成定时批量提交后，渲染频率与日志量彻底解耦，恒定在每秒十次以内。
const FLUSH_MS = 120;

/** 分片名 -> { lines: [], connected: bool } */
const channels = reactive({});
/** 当前订阅中的分片名，顺序即展示顺序 */
const shards = ref([]);

/** 分片名 -> EventSource */
const connections = new Map();

/**
 * 分片名 -> 尚未落账的行。
 * 刻意用普通 Map 而不是 reactive：中途累积的数据绝不能触发渲染，
 * 一旦触发就退化回"每行一渲染"的老问题。
 */
const pending = new Map();
let flushTimer = null;

let seq = 0;
let lastRoomId = null;

function ensure(shard) {
  if (!channels[shard]) channels[shard] = { lines: [], connected: false };
  return channels[shard];
}

/** 全局连接状态：任一通道在线即视为已连接。侧边栏用。 */
const connected = computed(() =>
  shards.value.some(s => channels[s] && channels[s].connected));

function push(shard, kind, text) {
  let batch = pending.get(shard);
  if (!batch) {
    batch = [];
    pending.set(shard, batch);
  }
  batch.push({ id: ++seq, kind, text });
  scheduleFlush();
}

function scheduleFlush() {
  if (flushTimer !== null) return;
  flushTimer = setTimeout(flushPending, FLUSH_MS);
}

function flushPending() {
  flushTimer = null;
  for (const [shard, batch] of pending) {
    pending.delete(shard);
    if (!batch.length) continue;
    const ch = ensure(shard);
    // 整段拼接而不是逐行 push：一批数据只触发一次响应式更新
    ch.lines = ch.lines.concat(batch);
    if (ch.lines.length > MAX_LINES) {
      ch.lines.splice(0, ch.lines.length - MAX_LINES);
    }
  }
}

/** 关掉某个分片的连接。缓冲区保留，重连后历史还在。 */
function closeShard(shard) {
  const es = connections.get(shard);
  if (es) {
    es.close();
    connections.delete(shard);
  }
  const ch = channels[shard];
  if (ch) ch.connected = false;
}

function disconnect() {
  for (const shard of [...connections.keys()]) closeShard(shard);
  shards.value = [];
}

/** 为单个分片建立连接。已有旧连接时先关掉。 */
function openShard(roomId, shard) {
  closeShard(shard);
  const ch = ensure(shard);

  const source = new EventSource(logApi.streamUrl(roomId, shard, HISTORY_LINES));
  connections.set(shard, source);

  source.onopen = () => { ch.connected = true; };
  source.onerror = () => { ch.connected = false; };

  source.onmessage = ev => {
    let data;
    try {
      data = JSON.parse(ev.data);
    } catch (_) {
      return;
    }
    // 订阅的是具体分片，后端只会推该分片；仍以消息自带的分片名为准更稳
    push(data.shard || shard, data.type === 'log' ? 'log' : data.type, data.message);
  };
}

/**
 * 按房间打开日志流：为房间的每个启用分片各建一条连接。
 *
 * - 换房间：URL 里的 room 变了，原有连接全部重建
 * - 分片增删（如用户开了洞穴）：只处理差异，已在连的分片不打断
 *
 * @param {number} roomId 房间 ID
 * @param {string[]} shardNames 该房间启用中的分片
 */
function openRoom(roomId, shardNames = []) {
  if (!roomId) {
    disconnect();
    return;
  }

  const roomChanged = roomId !== lastRoomId;
  lastRoomId = roomId;

  if (roomChanged) {
    for (const shard of [...connections.keys()]) closeShard(shard);
  } else {
    for (const shard of [...connections.keys()]) {
      if (!shardNames.includes(shard)) closeShard(shard);
    }
  }

  shards.value = [...shardNames];
  for (const shard of shardNames) {
    if (!connections.has(shard)) openShard(roomId, shard);
  }
}

/** 清空某个分片的日志。 */
function clear(shard) {
  pending.delete(shard); // 已缓冲未落账的行一并丢弃，否则清屏后又会冒出来
  const ch = channels[shard];
  if (ch) ch.lines = [];
}

/** 清空所有分片的日志。换房间时用。 */
function clearAll() {
  pending.clear();
  for (const shard of Object.keys(channels)) channels[shard].lines = [];
}

/** 用户敲的控制台命令。用独立类型以便着色，和服务器输出区分开。 */
function appendCmd(shard, text) {
  push(shard, 'cmd', text);
}

export function useLogs() {
  return {
    channels, shards, connected,
    openRoom, disconnect, clear, clearAll, appendCmd,
  };
}
