<script setup>
// 单个房间卡片：概览信息 + 操作按钮。

import { computed } from 'vue';

import UiPill from '@/components/ui/UiPill.vue';
import Icon from '@/components/ui/Icon.vue';
import { GAME_MODE_LABEL } from '@/lib/format.js';

const props = defineProps({
  room: { type: Object, required: true },
  selected: { type: Boolean, default: false },
});

defineEmits(['select', 'edit', 'remove', 'upload']);

const shardOf = name => (props.room.shards || []).find(s => s.name === name) || {};

const master = computed(() => shardOf('Master'));
const caves = computed(() => shardOf('Caves'));
const running = computed(() => (props.room.statuses || []).some(s => s.state === 'running'));
const modeLabel = computed(() => GAME_MODE_LABEL[props.room.gameMode] || props.room.gameMode);

/** 端口按「游戏端口 / Steam 主端口 / Steam 认证端口」顺序展示 */
function ports(s) {
  if (!s.serverPort) return '—';
  return `${s.serverPort} / ${s.masterServerPort} / ${s.authenticationPort}`;
}
</script>

<template>
  <div
    class="room-card"
    :class="{
      'is-selected': selected,
      'is-running': running,
    }"
  >
    <div>
      <div class="room-title">{{ room.name }}</div>
      <div class="room-sub">
        {{ room.key }}{{ room.hasSave ? ' · 已有存档' : '' }}
        <template v-if="room.description"> · {{ room.description }}</template>
      </div>
    </div>

    <div class="room-badges">
      <UiPill :tone="running ? 'info' : 'muted'">
        {{ running ? '运行中' : '已停止' }}
      </UiPill>
      <UiPill tone="muted">{{ modeLabel }}</UiPill>
      <UiPill v-if="!room.caves" tone="muted">仅地表</UiPill>
      <UiPill v-if="room.needToken" tone="warn">
        <Icon name="key" size="12" />缺令牌
      </UiPill>
    </div>

    <div class="port-chips">
      <span class="chip">
        <b>地表</b><span>{{ ports(master) }}</span>
      </span>
      <span class="chip" :class="{ 'is-off': !room.caves }">
        <b>洞穴</b><span>{{ room.caves ? ports(caves) : '未启用' }}</span>
      </span>
      <span class="chip">
        <b>tick</b><span>{{ room.tickRate }}</span>
      </span>
      <span class="chip">
        <b>回档</b><span>{{ room.maxSnapshots }}</span>
      </span>
    </div>

    <div class="room-actions">
      <button class="btn btn-sm" @click="$emit('select', room.id)">选中</button>
      <button class="btn btn-sm" @click="$emit('edit', room.id)">
        <Icon name="pencil" size="14" />编辑
      </button>
      <button class="btn btn-sm" @click="$emit('upload', room.id)">
        <Icon name="upload" size="14" />上传存档
      </button>
      <button class="btn btn-sm btn-danger" @click="$emit('remove', room.id)">
        <Icon name="trash" size="14" />删除
      </button>
    </div>
  </div>
</template>
