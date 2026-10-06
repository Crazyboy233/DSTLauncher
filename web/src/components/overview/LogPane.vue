<script setup>
// 单个分片的日志面板：独立缓冲区、独立滚动位置、独立命令输入。
//
// 因为 Master 与 Caves 是两个独立进程，所以在哪个面板里敲命令，
// 就发给哪个分片——不需要再选一次目标。

import { computed, onMounted, ref, watch } from 'vue';

import { useLogs } from '@/composables/useLogs.js';
import { useRoomsStore } from '@/stores/rooms.js';
import { guard } from '@/lib/toast.js';

const props = defineProps({
  /** 分片名：Master / Caves */
  shard: { type: String, required: true },
});

const rooms = useRoomsStore();
const { channels, clear, appendCmd } = useLogs();

/** 通道尚未建立时的占位。必须是稳定引用，否则 computed 会反复触发。 */
const FALLBACK = { lines: [], connected: false };
const state = computed(() => channels[props.shard] || FALLBACK);
const lines = computed(() => state.value.lines);
const connected = computed(() => !!state.value.connected);

const SUBTITLE = { Master: '地表世界', Caves: '洞穴世界' };
const subtitle = computed(() => SUBTITLE[props.shard] || '');

/** 只有运行中的分片才接受控制台命令，其余情况禁用输入并说明原因。 */
const running = computed(() => {
  const st = rooms.statuses.find(s => s.shard === props.shard);
  return !!st && st.state === 'running';
});

const box = ref(null);
const input = ref('');
/** 是否自动跟随最新日志。关掉后可以安静地往上翻历史。 */
const autoscroll = ref(true);

function onScroll() {
  // 用户手动滚到底部时重新打开跟随，省得再去点开关
  const el = box.value;
  if (!el) return;
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 30 && !autoscroll.value) {
    autoscroll.value = true;
  }
}

/**
 * 滚动到底部，用 requestAnimationFrame 合并。
 *
 * 设置 scrollTop 会强制浏览器做一次完整布局，而日志区可能有几千个节点。
 * 高频日志下若每行都滚一次，布局计算会把主线程打满——这正是之前
 * "启动阶段整个页面近乎卡死"的根因之一。rAF 保证一帧最多定位一次，
 * 与 useLogs 的批量落账配合后，布局次数与日志量彻底解耦。
 */
let scrollQueued = false;

function scrollToEnd() {
  if (scrollQueued) return;
  scrollQueued = true;
  requestAnimationFrame(() => {
    scrollQueued = false;
    const el = box.value;
    if (el) el.scrollTop = el.scrollHeight;
  });
}

watch(() => lines.value.length, () => {
  if (autoscroll.value) scrollToEnd();
});

onMounted(scrollToEnd);

async function submit() {
  const cmd = input.value.trim();
  if (!cmd) return;
  const { ok } = await guard(() => rooms.sendCmd(props.shard, cmd));
  if (ok) {
    appendCmd(props.shard, cmd);
    input.value = '';
    autoscroll.value = true;
    scrollToEnd();
  }
}
</script>

<template>
  <section class="log-pane">
    <header class="log-pane-head">
      <span class="log-pane-name">{{ shard }}</span>
      <span v-if="subtitle" class="log-pane-sub">{{ subtitle }}</span>
      <span class="pill" :class="connected ? 'pill-ok' : 'pill-warn'">
        {{ connected ? '已连接' : '重连中' }}
      </span>
      <label class="switch" title="关闭后可以安静地往上翻历史，滚回底部会自动重新打开">
        <input v-model="autoscroll" type="checkbox">
        <span>自动滚动</span>
      </label>
      <button class="btn btn-sm" @click="clear(shard)">清屏</button>
    </header>

    <div ref="box" class="console" @scroll="onScroll">
      <div v-if="!lines.length" class="console-empty">
        暂无 {{ shard }} 的日志输出。启动该分片后，这里的输出会实时刷新。
      </div>
      <div v-for="l in lines" :key="l.id" class="log-line" :class="'k-' + l.kind">
        <span class="log-text">{{ l.text }}</span>
      </div>
    </div>

    <form class="console-form" @submit.prevent="submit">
      <input
        v-model="input"
        class="input"
        autocomplete="off"
        :disabled="!running"
        :placeholder="running ? '控制台命令，如 c_save() / c_listallplayers()' : shard + ' 未运行'"
      >
      <button class="btn btn-primary" type="submit" :disabled="!running">执行</button>
    </form>
  </section>
</template>
