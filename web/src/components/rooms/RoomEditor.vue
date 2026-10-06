<script setup>
// 房间编辑表单。
//
// 房间配置是全部 ini 的唯一事实来源，所以这里覆盖了 cluster.ini 与 server.ini
// 的所有可配置项。表单状态是本地副本，只有点保存才写回后端。
// 切换编辑对象时由父组件用 :key 重建本组件，因此不需要 watch 同步。

import { computed, reactive } from 'vue';

import UiCard from '@/components/ui/UiCard.vue';
import UiField from '@/components/ui/UiField.vue';
import UiSwitch from '@/components/ui/UiSwitch.vue';
import Icon from '@/components/ui/Icon.vue';
import PortBoxes from '@/components/rooms/PortBoxes.vue';
import { useRoomsStore } from '@/stores/rooms.js';
import { guard, toast } from '@/lib/toast.js';
import { GAME_MODE_LABEL, TICK_RATE_LABEL, LANGUAGE_LABEL } from '@/lib/format.js';

const props = defineProps({
  /** null 表示新建 */
  room: { type: Object, default: null },
  meta: { type: Object, default: null },
});

const emit = defineEmits(['close', 'saved']);

const rooms = useRoomsStore();
const isNew = computed(() => !props.room);

function blankShard(name, isMaster, gameId) {
  return { name, isMaster, gameId, serverPort: null, masterServerPort: null, authenticationPort: null };
}

function buildForm() {
  const d = (props.meta && props.meta.defaults) || {};
  const src = props.room || {};
  const pick = name => (src.shards || []).find(s => s.name === name) || {};

  return {
    id: src.id || 0,
    name: src.name ?? d.name ?? '我的饥荒服务器',
    description: src.description ?? d.description ?? '',
    gameMode: src.gameMode || d.gameMode || 'endless',
    maxPlayers: src.maxPlayers || d.maxPlayers || 12,
    tickRate: src.tickRate || d.tickRate || 30,
    language: src.language || d.language || 'zh',
    password: src.password || '',
    token: src.token || '',
    maxSnapshots: src.maxSnapshots || d.maxSnapshots || 10,
    masterIp: src.masterIp || d.masterIp || '127.0.0.1',
    masterPort: src.masterPort || null,
    clusterKey: src.clusterKey || '',

    // 布尔值用 ?? 而不是 ||，否则用户主动关掉的开关会被默认值顶回来
    pvp: src.pvp ?? true,
    pauseEmpty: src.pauseEmpty ?? true,
    voteEnabled: src.voteEnabled ?? true,
    voteKick: src.voteKick ?? true,
    caves: src.caves ?? true,
    lanOnly: src.lanOnly ?? false,
    offline: src.offline ?? false,

    // 两个分片都要保留：洞穴停用时端口也不会被回收，
    // 之后再启用不需要重新分配
    shards: [
      { ...blankShard('Master', true, 1), ...pick('Master') },
      { ...blankShard('Caves', false, 2), ...pick('Caves') },
    ],
  };
}

const form = reactive(buildForm());

const gameModes = computed(() => (props.meta && props.meta.gameModes) || Object.keys(GAME_MODE_LABEL));
const tickRates = computed(() => (props.meta && props.meta.tickRates) || [15, 30, 60]);
const languages = computed(() => (props.meta && props.meta.languages) || Object.keys(LANGUAGE_LABEL));

/** 数字框清空后会是空字符串，必须转成后端能接的整数。 */
function toNum(value, fallback) {
  const n = Number(value);
  return Number.isFinite(n) && n > 0 ? Math.round(n) : fallback;
}

async function save() {
  const payload = {
    id: form.id,
    name: form.name.trim(),
    description: form.description.trim(),
    gameMode: form.gameMode,
    maxPlayers: toNum(form.maxPlayers, 12),
    tickRate: toNum(form.tickRate, 30),
    language: form.language,
    password: form.password,
    token: form.token.trim(),
    maxSnapshots: toNum(form.maxSnapshots, 10),
    masterIp: form.masterIp.trim(),
    masterPort: toNum(form.masterPort, 0),
    clusterKey: form.clusterKey.trim(),
    pvp: form.pvp,
    pauseEmpty: form.pauseEmpty,
    voteEnabled: form.voteEnabled,
    voteKick: form.voteKick,
    caves: form.caves,
    lanOnly: form.lanOnly,
    offline: form.offline,
  };

  // 新建时不提交端口，交给后端统一分配，避免与已有房间撞号
  if (!isNew.value) {
    payload.shards = form.shards.map((s, i) => ({
      name: s.name,
      gameId: i + 1,
      isMaster: s.name === 'Master',
      serverPort: toNum(s.serverPort, 0),
      masterServerPort: toNum(s.masterServerPort, 0),
      authenticationPort: toNum(s.authenticationPort, 0),
    }));
  }

  const { ok, data } = await guard(() =>
    isNew.value ? rooms.create(payload) : rooms.update(payload));
  if (!ok) return;

  // 房间运行中改配置不会立即生效，后端会带一条提示
  const hint = data && data.hint;
  if (hint) toast(hint, 'warn');
  else toast(isNew.value ? '房间已创建' : '房间已保存');

  emit('saved');
}
</script>

<template>
  <UiCard>
    <template #title>{{ isNew ? '新建房间' : '编辑房间 · ' + form.name }}</template>
    <template v-if="!isNew" #hint>Cluster_{{ form.id }}</template>

    <div v-if="isNew" class="notice" style="margin-bottom: 18px">
      <Icon name="info" size="16" />
      <span>
        端口（游戏端口 / Steam 主端口 / Steam 认证端口 / 分片协调端口）由系统自动分配，
        避免与已有房间冲突；创建后可在编辑页调整。
      </span>
    </div>

    <section class="form-section">
      <h3>基本信息</h3>
      <div class="form-grid">
        <UiField label="房间名" for-id="f_name">
          <input id="f_name" v-model="form.name" class="input" type="text"
                 placeholder="显示在服务器列表里的名字">
        </UiField>
        <UiField label="描述" for-id="f_description">
          <input id="f_description" v-model="form.description" class="input" type="text"
                 placeholder="一句话介绍">
        </UiField>
        <UiField label="游戏模式" for-id="f_gameMode">
          <select id="f_gameMode" v-model="form.gameMode" class="select">
            <option v-for="m in gameModes" :key="m" :value="m">
              {{ GAME_MODE_LABEL[m] || m }}
            </option>
          </select>
        </UiField>
        <UiField label="最大人数" for-id="f_maxPlayers">
          <input id="f_maxPlayers" v-model.number="form.maxPlayers" class="input"
                 type="number" min="1" max="64">
        </UiField>
        <UiField label="tick 频率" for-id="f_tickRate">
          <select id="f_tickRate" v-model.number="form.tickRate" class="select">
            <option v-for="t in tickRates" :key="t" :value="t">
              {{ TICK_RATE_LABEL[t] || t }}
            </option>
          </select>
        </UiField>
        <UiField label="语言" for-id="f_language">
          <select id="f_language" v-model="form.language" class="select">
            <option v-for="l in languages" :key="l" :value="l">
              {{ LANGUAGE_LABEL[l] || l }}
            </option>
          </select>
        </UiField>
        <UiField label="房间密码" for-id="f_password">
          <input id="f_password" v-model="form.password" class="input" type="text"
                 placeholder="留空表示无密码">
        </UiField>
        <UiField label="回档点数" for-id="f_maxSnapshots">
          <input id="f_maxSnapshots" v-model.number="form.maxSnapshots" class="input"
                 type="number" min="1" max="100">
        </UiField>
      </div>
    </section>

    <section class="form-section">
      <h3>Klei 服务器令牌</h3>
      <UiField
        for-id="f_token"
        block
        tip="在线开服必需，会写入存档目录下的 cluster_token.txt；勾选「仅局域网」或「离线模式」后可以留空。"
      >
        <textarea
          id="f_token"
          v-model="form.token"
          class="textarea"
          rows="2"
          spellcheck="false"
          placeholder="pds-g^... 从 Klei 账号页复制"
        />
      </UiField>
    </section>

    <section class="form-section">
      <h3>行为开关</h3>
      <div class="switches">
        <UiSwitch v-model="form.pvp" label="PVP" />
        <UiSwitch v-model="form.pauseEmpty" label="无人时暂停" />
        <UiSwitch v-model="form.voteEnabled" label="投票" />
        <UiSwitch v-model="form.voteKick" label="投票踢人" />
        <UiSwitch v-model="form.caves" label="启用洞穴" />
        <UiSwitch v-model="form.lanOnly" label="仅局域网" />
        <UiSwitch v-model="form.offline" label="离线模式" />
      </div>
    </section>

    <section class="form-section">
      <h3>分片与端口</h3>
      <PortBoxes :shards="form.shards" :caves-enabled="form.caves" />
      <p class="field-tip">
        {{ isNew
          ? '新建时留空即可，保存后由系统填好实际端口。'
          : '三个端口都必须全局唯一；改动后需重启该房间才生效。' }}
      </p>
    </section>

    <details class="form-section details">
      <summary>高级设置</summary>
      <div class="form-grid form-grid-top">
        <UiField label="主分片 IP" for-id="f_masterIp">
          <input id="f_masterIp" v-model="form.masterIp" class="input" type="text">
        </UiField>
        <UiField label="分片协调端口" for-id="f_masterPort">
          <input id="f_masterPort" v-model.number="form.masterPort" class="input"
                 type="number" min="1024" max="65535">
        </UiField>
        <UiField
          label="通信密钥"
          for-id="f_clusterKey"
          wide
          tip="地表与洞穴之间的认证密钥，两边必须一致；改动后需重启全部已启用的分片。"
        >
          <input id="f_clusterKey" v-model="form.clusterKey" class="input" type="text">
        </UiField>
      </div>
    </details>

    <div class="form-actions">
      <button class="btn btn-primary" @click="save">
        <Icon name="check" size="14" />保存
      </button>
      <button class="btn" @click="emit('close')">取消</button>
    </div>
  </UiCard>
</template>
