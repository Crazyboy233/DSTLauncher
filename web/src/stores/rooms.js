// 房间状态：列表、当前房间、分片状态与进程控制。
//
// 约定：这些 action 出错时**抛出异常**，由调用方决定怎么提示——
// 表单保存失败要留在编辑器里，而启停失败只需要弹个提示，两者的处理不同。

import { defineStore } from 'pinia';
import { ref, computed, watch } from 'vue';

import { roomApi, procApi } from '@/lib/api.js';

/**
 * 当前房间的本地记忆。
 *
 * 没有它的话，刷新页面后 currentId 归零，load() 会把选择落在列表第一个房间上——
 * 表现为「明明选着第二个房间，刷新一下就跳回第一个」。
 */
const ROOM_KEY = 'dst-room';

function readStoredRoomId() {
  try {
    const n = Number(localStorage.getItem(ROOM_KEY));
    return Number.isInteger(n) && n > 0 ? n : null;
  } catch (_) {
    return null;
  }
}

function storeRoomId(id) {
  try {
    if (id) localStorage.setItem(ROOM_KEY, String(id));
    else localStorage.removeItem(ROOM_KEY);
  } catch (_) {
    // 隐私模式等场景下写不了：只是刷新后记不住选择，不影响功能
  }
}

export const useRoomsStore = defineStore('rooms', () => {
  const list = ref([]);
  // 初值直接取上次的选择，首屏就能停在同一个房间
  const currentId = ref(readStoredRoomId());
  const meta = ref(null);
  /** 仅当前房间的分片状态，刷新频率比列表高 */
  const statuses = ref([]);
  const loaded = ref(false);

  /** 当前选中的房间。列表为空时返回 null。 */
  const current = computed(() => {
    if (!list.value.length) return null;
    return list.value.find(r => r.id === currentId.value) || list.value[0];
  });

  /** 当前房间启用中的分片。 */
  const shards = computed(() => {
    const r = current.value;
    if (!r) return [];
    return r.caves ? ['Master', 'Caves'] : ['Master'];
  });

  const running = computed(() =>
    statuses.value.some(s => s.state === 'running' || s.state === 'starting'));

  const hasToken = computed(() => !!current.value && !current.value.needToken);

  // 选中变化就落盘。集中在这里写，避免 create / update / remove / load
  // 各处的赋值漏掉持久化。
  watch(currentId, storeRoomId);

  /* ---------- 加载 ---------- */

  async function load() {
    list.value = (await roomApi.list()) || [];
    loaded.value = true;
    // 当前房间被删除或首次加载时，回落到第一个房间
    if (!list.value.some(r => r.id === currentId.value)) {
      currentId.value = list.value.length ? list.value[0].id : null;
    }
    return list.value;
  }

  async function loadMeta() {
    meta.value = await roomApi.meta();
    return meta.value;
  }

  async function refreshStatus() {
    const room = current.value;
    if (!room) {
      statuses.value = [];
      return;
    }
    statuses.value = (await procApi.status(room.id)) || [];
    // 同步给列表中的同一房间，保证房间卡片与顶部状态标签一致
    const inList = list.value.find(r => r.id === room.id);
    if (inList) inList.statuses = statuses.value;
  }

  function select(id) {
    const num = Number(id);
    if (!num || num === currentId.value) return false;
    currentId.value = num;
    return true;
  }

  /* ---------- 增删改 ---------- */

  async function create(payload) {
    const room = await roomApi.create(payload);
    currentId.value = room.id;
    await load();
    return room;
  }

  /**
   * 保存修改。返回 { room, hint }：
   * 运行中的房间改完配置不会立即生效，后端会给一条 hint 提示需重启。
   */
  async function update(payload) {
    const data = await roomApi.update(payload);
    const result = data && data.room ? data : { room: data, hint: '' };
    currentId.value = result.room.id;
    await load();
    return result;
  }

  async function remove(id) {
    const data = await roomApi.remove(id);
    if (currentId.value === Number(id)) currentId.value = null;
    await load();
    return data;
  }

  /* ---------- 进程控制 ---------- */

  async function start(shard) {
    const room = current.value;
    if (!room) throw new Error('请先选择房间');
    await procApi.start(room.id, shard);
  }

  async function stop(shard, timeout) {
    const room = current.value;
    if (!room) throw new Error('请先选择房间');
    await procApi.stop(room.id, shard, timeout);
  }

  async function restart(shard) {
    const room = current.value;
    if (!room) throw new Error('请先选择房间');
    await procApi.restart(room.id, shard);
  }

  async function sendCmd(shard, cmd) {
    const room = current.value;
    if (!room) throw new Error('请先选择房间');
    await procApi.cmd(room.id, shard, cmd);
  }

  return {
    list, currentId, meta, statuses, loaded,
    current, shards, running, hasToken,
    load, loadMeta, refreshStatus, select,
    create, update, remove,
    start, stop, restart, sendCmd,
  };
});
