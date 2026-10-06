<script setup>
// 概览页：核心指标 + 房间状态 + 服务器文件 + 实时日志。

import { computed } from 'vue';

import UiCard from '@/components/ui/UiCard.vue';
import UiStat from '@/components/ui/UiStat.vue';
import ShardList from '@/components/overview/ShardList.vue';
import InstallPanel from '@/components/overview/InstallPanel.vue';
import LogConsole from '@/components/overview/LogConsole.vue';
import { useRoomsStore } from '@/stores/rooms.js';
import { useSystemStore } from '@/stores/system.js';
import { useProcessControl } from '@/composables/useProcessControl.js';
import { GAME_MODE_LABEL, fmtSize } from '@/lib/format.js';

const rooms = useRoomsStore();
const system = useSystemStore();
const proc = useProcessControl();

const shardTotal = computed(() => rooms.shards.length);
const shardRunning = computed(() => rooms.statuses.filter(s => s.state === 'running').length);

const shardTone = computed(() => {
  if (!shardTotal.value) return '';
  if (shardRunning.value === shardTotal.value) return 'ok';
  return shardRunning.value > 0 ? 'warn' : '';
});

/** 运行中分片的资源合计。CPU 为各分片之和，口径是「占整机」。 */
const res = computed(() => {
  const live = rooms.statuses.filter(s => s.state === 'running');
  return {
    count: live.length,
    cpu: live.reduce((n, s) => n + (s.cpuPct || 0), 0),
    mem: live.reduce((n, s) => n + (s.memBytes || 0), 0),
  };
});

const resValue = computed(() => {
  const r = res.value;
  if (!r.count) return '—';
  // 首次采样没有基准，后端给 0；此时显示「—」而不是「0.0%」
  if (!r.cpu) return '—';
  return r.cpu < 0.05 ? '< 0.1%' : r.cpu.toFixed(1) + '%';
});

const resSub = computed(() => {
  const r = res.value;
  if (!r.count) return '未运行';
  return `内存 ${fmtSize(r.mem)}`;
});

const resTone = computed(() => {
  if (!res.value.count) return '';
  return res.value.cpu > 50 ? 'warn' : 'info';
});

const roomSub = computed(() => {
  const r = rooms.current;
  if (!r) return '还没有房间';
  return GAME_MODE_LABEL[r.gameMode] || r.gameMode;
});

const serverValue = computed(() => {
  if (system.installing) return '安装中';
  if (!system.installed) return '未安装';
  return system.version || '已安装';
});

const serverSub = computed(() => {
  if (system.installing) return system.progress || '准备中…';
  return system.installed ? '可检查更新' : '需先安装服务器文件';
});

const serverTone = computed(() => {
  if (system.installing) return 'info';
  return system.installed ? 'ok' : 'warn';
});
</script>

<template>
  <!-- 必须保持单一根节点：App.vue 用 <Transition> 包裹视图组件，
       而 Transition 只接受「恰好一个」根元素。多根（fragment）会让切换动画失效，
       表现为来回点导航后右侧内容整个空白。 -->
  <div>
    <div class="stat-row">
      <UiStat
        label="运行分片"
        :value="`${shardRunning} / ${shardTotal}`"
        sub="地表与洞穴各自独立进程"
        icon="server"
        :tone="shardTone"
      />
      <UiStat
        label="当前房间"
        :value="rooms.current ? rooms.current.name : '—'"
        :sub="roomSub"
        icon="globe"
        tone="info"
      />
      <UiStat
        label="资源占用"
        :value="resValue"
        :sub="resSub"
        icon="cpu"
        :tone="resTone"
      />
      <UiStat
        label="服务器文件"
        :value="serverValue"
        :sub="serverSub"
        icon="package"
        :tone="serverTone"
      />
    </div>

    <!-- 状态区（主）与服务器文件区（辅）按 7:5 分栏：
         分片行要放下「状态 · 运行时长 · PID · 端口 · 重启次数」，
         给它更多宽度才不会挤成两行；服务器文件内容少，窄一点更紧凑。
         不用 auto-fit 等分——那样两个区各占一半，状态区会大片留白。 -->
    <div class="overview-split">
      <UiCard>
        <template #title>房间状态</template>
        <template #actions>
          <button class="btn btn-sm btn-primary" :disabled="!rooms.current" @click="proc.start('')">
            全部启动
          </button>
          <button class="btn btn-sm btn-danger" :disabled="!rooms.current" @click="proc.stop('')">
            全部停止
          </button>
        </template>
        <ShardList />
      </UiCard>

      <UiCard>
        <template #title>服务器文件</template>
        <InstallPanel />
      </UiCard>
    </div>

    <LogConsole />
  </div>
</template>
