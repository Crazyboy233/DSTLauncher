<script setup>
// 分片（世界）编辑器。
//
// 每个分片是一个独立的服务器进程：自己的存档目录、server.ini 与三组端口。
// 分片名同时是**存档目录名**和 `-shard` 启动参数，所以已有分片不能改名——
// 改名等于换目录，原存档会留在旧目录里，看起来就像世界丢了。
//
// 主分片固定存在、不可停用也不可删除：房间至少要有一个世界。
// Master 与 Caves 两个名字更是引擎硬编码的通道锚点（落水洞→"Caves"、出口→"Master"），
// 名字或类型一动，地表↔洞穴的传送就全断，因此连新建房间时也锁死。

import { defineModel } from 'vue';

import Icon from '@/components/ui/Icon.vue';
import UiSwitch from '@/components/ui/UiSwitch.vue';
import { WORLD_TYPE_LABEL } from '@/lib/format.js';

const props = defineProps({
  /** 新建房间时还没落盘，分片可以改名（通道锚点 Master/Caves 除外） */
  roomIsNew: { type: Boolean, default: false },
});

const shards = defineModel('shards', { type: Array, required: true });

const WORLD_TYPES = Object.entries(WORLD_TYPE_LABEL).map(([value, label]) => ({ value, label }));

const PORTS = [
  { key: 'serverPort', label: '游戏端口' },
  { key: 'masterServerPort', label: 'Steam 主端口' },
  { key: 'authenticationPort', label: 'Steam 认证口' },
];

// Master 与 Caves 是引擎硬编码的通道锚点（cave_entrance.lua 写死 shard_name="Caves"、
// cave_exit.lua 写死 "Master"，且按**名字**匹配分片）。改这两个名字或类型会让
// 落水洞/出口全部失效，所以无论新建还是编辑都锁死，不允许修改。
const ANCHOR_WORLD_TYPE = { Master: 'forest', Caves: 'cave' };

function isAnchor(s) {
  return Object.prototype.hasOwnProperty.call(ANCHOR_WORLD_TYPE, s.name);
}

/** 已有分片的名字锁死；本次新增的、或新建房间时的分片可以改——但通道锚点永远锁死 */
function canRename(s) {
  return !isAnchor(s) && (props.roomIsNew || s.isNew);
}

/** 名字改成锚点名时联动类型：改名后类型会被锁死，不联动就会卡在错误的类型上 */
function onNameChange(s) {
  const t = ANCHOR_WORLD_TYPE[s.name];
  if (t) s.worldType = t;
}

function nameTitle(s) {
  if (isAnchor(s)) return '通道锚点：落水洞/出口按此名字匹配，不可修改';
  return canRename(s) ? '英文名，不含空格；保存后不可修改' : '存档目录名，保存后不可修改';
}

function typeTitle(s) {
  if (isAnchor(s)) return '通道锚点：世界类型绑定落水洞通道，不可修改';
  return '决定世界生成预设与可用设置项';
}

function typeLabel(value) {
  return WORLD_TYPE_LABEL[value] || value || WORLD_TYPE_LABEL.forest;
}

function addShard() {
  const used = new Set(shards.value.map(s => s.name));
  let n = shards.value.length + 1;
  while (used.has('World' + n)) n += 1;

  shards.value = [
    ...shards.value,
    {
      name: 'World' + n,
      worldType: 'forest',
      isMaster: false,
      disabled: false,
      gameId: 0,
      serverPort: null,
      masterServerPort: null,
      authenticationPort: null,
      isNew: true,
    },
  ];
}

function removeShard(index) {
  const s = shards.value[index];
  if (!s || s.isMaster) return;
  const ok = confirm(
    `从面板移除世界「${s.name}」？\n\n`
    + '存档目录会保留在磁盘上，但面板不再管理它。\n'
    + '以后用同名的分片重新添加，就能继续使用这份存档。',
  );
  if (!ok) return;
  shards.value = shards.value.filter((_, i) => i !== index);
}
</script>

<template>
  <div class="shard-list">
    <div
      v-for="(s, i) in shards"
      :key="i"
      class="shard-card"
      :class="{ 'is-off': s.disabled }"
    >
      <div class="shard-card-head">
        <input
          v-model.trim="s.name"
          class="input shard-name-input"
          :disabled="!canRename(s)"
          :title="nameTitle(s)"
          placeholder="世界名"
          @change="onNameChange(s)"
        >
        <select
          v-model="s.worldType"
          class="select shard-type-select"
          :disabled="!canRename(s)"
          :title="typeTitle(s)"
        >
          <option v-for="t in WORLD_TYPES" :key="t.value" :value="t.value">{{ t.label }}</option>
        </select>
        <span v-if="s.isMaster" class="pill pill-info">主世界</span>
        <UiSwitch
          :model-value="!s.disabled"
          :disabled="s.isMaster"
          label="启用"
          @update:model-value="v => (s.disabled = !v)"
        />
        <button
          v-if="!s.isMaster"
          class="btn btn-sm shard-del"
          title="从面板移除（存档目录保留）"
          @click="removeShard(i)"
        >
          <Icon name="trash" size="14" />
        </button>
      </div>

      <div class="shard-ports">
        <label v-for="p in PORTS" :key="p.key" class="shard-port">
          <span>{{ p.label }}</span>
          <input
            v-model.number="s[p.key]"
            class="input"
            type="number"
            min="1024"
            max="65535"
            :placeholder="s[p.key] ? '' : '自动分配'"
          >
        </label>
      </div>

      <p class="shard-foot">
        世界 ID <code>{{ s.gameId || '自动' }}</code>
        · {{ typeLabel(s.worldType) }}
        <template v-if="s.isMaster"> · 进服落点，不可停用</template>
        <template v-else-if="s.disabled"> · 已停用，不会启动</template>
      </p>
    </div>

    <button class="btn btn-sm shard-add" type="button" @click="addShard">
      <Icon name="plus" size="14" />添加世界
    </button>
  </div>
</template>
