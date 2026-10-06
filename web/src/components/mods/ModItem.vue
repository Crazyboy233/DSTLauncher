<script setup>
// 单个模组条目：启用开关、删除、展开配置表单。

import { computed, ref, watch } from 'vue';

import UiPill from '@/components/ui/UiPill.vue';
import Icon from '@/components/ui/Icon.vue';
import ModConfigForm from '@/components/mods/ModConfigForm.vue';
import { useRoomsStore } from '@/stores/rooms.js';
import { modApi } from '@/lib/api.js';
import { guard, toast } from '@/lib/toast.js';

const props = defineProps({
  mod: { type: Object, required: true },
  /** 全局更新会话进行中：单模组按钮一并禁用（会话全局唯一） */
  updating: { type: Boolean, default: false },
});

const emit = defineEmits(['changed', 'update']);

const rooms = useRoomsStore();
const expanded = ref(false);

/** 服务器运行中冻结：模组开关与配置重启后才生效，改了也不生效 */
const frozen = computed(() => rooms.running);

// 开关状态用本地副本而不是直接改 props.mod：
// 一是避免修改父组件数据，二是接口失败时可以回滚显示。
const enabled = ref(props.mod.enabled);
watch(() => props.mod.enabled, v => { enabled.value = v; });

async function onToggle(next) {
  if (frozen.value) {
    toast('服务器运行中，请先关闭服务器再修改模组', 'warn');
    return;
  }
  const room = rooms.current;
  if (!room) return;

  enabled.value = next; // 先按用户的意图切换，失败再回滚
  const { ok } = await guard(() => modApi.toggle(room.id, props.mod.id, next));
  if (ok) {
    toast(`${props.mod.name || props.mod.id} ${next ? '已启用' : '已禁用'}`);
    return;
  }
  enabled.value = props.mod.enabled;
}

async function remove() {
  if (frozen.value) {
    toast('服务器运行中，请先关闭服务器再删除模组', 'warn');
    return;
  }
  const room = rooms.current;
  if (!room) return;
  const name = props.mod.name || props.mod.id;
  if (!confirm(
    `删除模组「${name}」？\n\n` +
    `模组实体是所有房间共享的，删除本地文件后其它房间也无法使用；` +
    `只会从当前房间的 modoverrides 中移除启用记录。`,
  )) return;

  const { ok } = await guard(() => modApi.remove(room.id, props.mod.id));
  if (ok) {
    toast('已删除');
    emit('changed');
  }
}

/**
 * 单模组更新：未下载的直接下载；已下载的先删旧文件再重下（强制刷新）。
 * 创意工坊模组才支持，本地模组没有「下载」一说。
 * 启动交给父组件（轮询归它管），这里只做确认。
 */
function updateOne() {
  if (frozen.value || props.updating) return;
  if (!props.mod.id.startsWith('workshop-')) {
    toast('本地模组不支持在线更新', 'warn');
    return;
  }
  const name = props.mod.name || props.mod.id;
  if (props.mod.installed && !confirm(
    `重新下载模组「${name}」？\n\n` +
    `将删除本地文件并从创意工坊重新获取，用于修复损坏或拉取更新；\n` +
    `模组实体所有房间共享，重下期间其它房间也暂时不可用。`,
  )) return;

  emit('update', props.mod);
}
</script>

<template>
  <div class="mod">
    <div class="mod-head">
      <label class="switch" :class="{ 'is-locked': frozen }">
        <input
          type="checkbox"
          :checked="enabled"
          :disabled="frozen"
          @change="onToggle($event.target.checked)"
        >
        <span>启用</span>
      </label>

      <span class="mod-name">{{ mod.name || mod.id }}</span>

      <UiPill :tone="enabled ? 'ok' : 'muted'">
        {{ enabled ? '已启用' : '已禁用' }}
      </UiPill>
      <UiPill :tone="mod.installed ? 'muted' : 'warn'">
        {{ mod.installed ? '已下载' : '未下载' }}
      </UiPill>

      <span class="mod-actions">
        <span class="mod-id">{{ mod.id }}</span>
        <button
          v-if="mod.id.startsWith('workshop-')"
          class="btn btn-sm"
          :disabled="frozen || updating"
          :title="mod.installed ? '删除本地文件并重新下载（强制刷新）' : '从创意工坊下载该模组'"
          @click="updateOne"
        >
          <Icon name="download" size="14" />{{ mod.installed ? '更新' : '下载' }}
        </button>
        <button
          v-if="mod.installed"
          class="btn btn-sm"
          :disabled="frozen && !expanded"
          :title="frozen ? '服务器运行中，配置已冻结' : ''"
          @click="expanded = !expanded"
        >
          {{ expanded ? '收起' : '配置' }}
        </button>
        <button class="btn btn-sm btn-danger" :disabled="frozen" @click="remove">删除</button>
      </span>
    </div>

    <div v-if="expanded" class="mod-body">
      <ModConfigForm :room-id="rooms.currentId" :mod="mod" :disabled="frozen" />
    </div>
  </div>
</template>
