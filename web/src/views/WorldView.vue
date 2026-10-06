<script setup>
// 世界配置：按分片（Master / Caves）编辑 worldgenoverride.lua。
//
// 只写 worldgenoverride.lua —— 同目录下的 leveldataoverride.lua 优先级更高，
// 且按 Klei 在服务器源码里的注释会「完全覆盖已保存的世界数据」，
// 那是游戏内部文件，面板不碰。
//
// 保存时只提交与默认值不同的项：官方文档说等于默认值的项可以直接省略，
// 省略还能避免游戏以后新增选项时被这里保存的旧默认值覆盖。
//
// 世界规则（settings）与世界生成（worldgen）分开成两个标签页展示；
// 服务器运行中整个页面冻结——游戏只在启动时读这份文件，改了也不生效。

import { computed, onMounted, reactive, ref, watch } from 'vue';

import Icon from '@/components/ui/Icon.vue';
import UiCard from '@/components/ui/UiCard.vue';
import UiEmpty from '@/components/ui/UiEmpty.vue';
import UiField from '@/components/ui/UiField.vue';
import UiSkeleton from '@/components/ui/UiSkeleton.vue';
import UiSwitch from '@/components/ui/UiSwitch.vue';
import OptionGroupPanel from '@/components/world/OptionGroupPanel.vue';
import { worldApi } from '@/lib/api.js';
import { useRoomsStore } from '@/stores/rooms.js';
import { guard, toast } from '@/lib/toast.js';

const rooms = useRoomsStore();

const SHARD_LABEL = { Master: '地表世界', Caves: '洞穴世界' };

const shard = ref('Master');
const view = ref(null);
const loading = ref(false);
const saving = ref(false);
const loadError = ref('');

const enabled = ref(true);
const worldgenPreset = ref('');
const settingsPreset = ref('');

/** 编辑中的改动：只放与默认值不同的项 */
const draft = reactive({});
/** 载入时的原始值，用于判断有没有未保存的改动 */
const original = reactive({ enabled: true, worldgenPreset: '', settingsPreset: '' });

const keyword = ref('');
const onlyChanged = ref(false);

/** 选项标签页：世界规则 / 世界生成 */
const category = ref('settings');

/* ---------- 派生状态 ---------- */

const shardHint = computed(() => SHARD_LABEL[shard.value] || shard.value);

/** 服务器运行中冻结全部修改：游戏只在启动时读配置，改了也不生效 */
const frozen = computed(() => rooms.running);

/**
 * 世界已生成的房间冻结「世界生成」类配置：
 * 这些选项只在生成世界时被读取，已有地图不会重建，改了纯属误导。
 * 世界规则类不受此限制。
 */
const worldGenerated = computed(() => !!view.value?.hasSave);
const worldgenFrozen = computed(() => frozen.value || worldGenerated.value);

const totalOptions = computed(() =>
  (view.value?.groups || []).reduce((n, g) => n + g.options.length, 0));

const changedCount = computed(() => Object.keys(draft).length);

const settingsChanged = computed(() =>
  enabled.value !== original.enabled
  || worldgenPreset.value !== original.worldgenPreset
  || settingsPreset.value !== original.settingsPreset);

const dirty = computed(() => changedCount.value > 0 || settingsChanged.value);

const worldgenPresets = computed(() => view.value?.presets?.worldgen || []);
const settingsPresets = computed(() => view.value?.presets?.settings || []);

/** 按类别拆分分组 */
const settingsGroups = computed(() =>
  (view.value?.groups || []).filter(g => g.category !== 'worldgen'));
const worldgenGroups = computed(() =>
  (view.value?.groups || []).filter(g => g.category === 'worldgen'));

const activeGroups = computed(() =>
  category.value === 'worldgen' ? worldgenGroups.value : settingsGroups.value);

const categoryCounts = computed(() => ({
  settings: settingsGroups.value.reduce((n, g) => n + g.options.length, 0),
  worldgen: worldgenGroups.value.reduce((n, g) => n + g.options.length, 0),
}));

const visibleGroups = computed(() => {
  const kw = keyword.value.trim().toLowerCase();
  if (!kw && !onlyChanged.value) return activeGroups.value;

  return activeGroups.value
    .map(g => ({
      ...g,
      options: g.options.filter(o => {
        if (onlyChanged.value && !isChanged(o)) return false;
        if (!kw) return true;
        return o.name.toLowerCase().includes(kw)
          || (o.label || '').toLowerCase().includes(kw)
          || (o.values || []).some(v => (v.label || '').toLowerCase().includes(kw));
      }),
    }))
    .filter(g => g.options.length > 0);
});

/* ---------- 取值读写 ---------- */

function isChanged(o) {
  return Object.prototype.hasOwnProperty.call(draft, o.name);
}

function valueOf(o) {
  return isChanged(o) ? draft[o.name] : o.default;
}

function onOptionChange(o, value) {
  if (frozen.value) return;
  // 世界已生成时，世界生成类选项锁死（与控件 disabled 双保险）
  if (worldgenFrozen.value && o.category === 'worldgen') return;
  // 改回默认值等于没改，从草稿里删掉——避免写出一份全是默认值的文件
  if (value === o.default) delete draft[o.name];
  else draft[o.name] = value;
}

/* ---------- 加载与保存 ---------- */

function applyView(data) {
  view.value = data;
  enabled.value = data.config.overrideEnabled;
  worldgenPreset.value = data.config.worldgenPreset;
  settingsPreset.value = data.config.settingsPreset;
  original.enabled = data.config.overrideEnabled;
  original.worldgenPreset = data.config.worldgenPreset;
  original.settingsPreset = data.config.settingsPreset;

  Object.keys(draft).forEach(k => delete draft[k]);
  Object.assign(draft, data.config.overrides || {});
}

async function load() {
  if (!rooms.currentId) {
    view.value = null;
    return;
  }
  loading.value = true;
  loadError.value = '';
  try {
    applyView(await worldApi.get(rooms.currentId, shard.value));
  } catch (err) {
    loadError.value = err.message;
    view.value = null;
  } finally {
    loading.value = false;
  }
}

async function save() {
  if (frozen.value) {
    toast('服务器运行中，请先关闭服务器再保存', 'warn');
    return;
  }
  saving.value = true;
  const { ok, data } = await guard(
    () => worldApi.save({
      room: rooms.currentId,
      shard: shard.value,
      overrideEnabled: enabled.value,
      worldgenPreset: worldgenPreset.value,
      settingsPreset: settingsPreset.value,
      overrides: { ...draft },
    }),
    { success: '世界配置已保存' },
  );
  saving.value = false;
  if (!ok) return;
  // 游戏只在启动时读这个文件，运行中的分片感知不到改动
  if (data && data.needRestart) toast('有分片正在运行，重启后才会生效');
  await load();
}

function resetAll() {
  Object.keys(draft).forEach(k => delete draft[k]);
  enabled.value = original.enabled;
  worldgenPreset.value = original.worldgenPreset;
  settingsPreset.value = original.settingsPreset;
}

/* ---------- 生命周期 ---------- */

watch(shard, load);
watch(() => rooms.currentId, () => {
  shard.value = 'Master';
  load();
});
// 洞穴被关掉后，当前选中的 Caves 会失效
watch(() => rooms.shards.join(','), () => {
  if (!rooms.shards.includes(shard.value)) shard.value = rooms.shards[0] || 'Master';
});

onMounted(load);
</script>

<template>
  <!-- 必须保持单一根节点：App.vue 用 <Transition> 包裹视图组件 -->
  <div>
    <UiCard>
      <template #title>世界配置</template>
      <template #hint>{{ rooms.current ? rooms.current.name + ' · ' + shardHint : '' }}</template>
      <template #actions>
        <button
          v-for="s in rooms.shards"
          :key="s"
          class="btn btn-sm"
          :class="{ 'btn-primary': s === shard }"
          @click="shard = s"
        >
          {{ s }}
        </button>
      </template>

      <UiSkeleton v-if="!rooms.loaded" :rows="4" />
      <UiEmpty v-else-if="!rooms.current" icon="globe" text="请先在「房间」页新建一个房间" />
      <UiSkeleton v-else-if="loading" :rows="4" />
      <p v-else-if="loadError" class="field-tip">读取失败：{{ loadError }}</p>

      <template v-else-if="view">
        <!-- 游戏只在启动时读配置，运行中修改不会生效 -->
        <div v-if="frozen" class="notice">
          <Icon name="info" size="16" />
          <span>
            房间正在运行中，配置已冻结。现在修改不会生效，
            请先在「总览」页<strong>关闭服务器</strong>后再修改配置。
          </span>
        </div>

        <div class="form-grid">
          <UiField label="世界生成预设" for-id="w-worldgen" tip="决定地图长什么样">
            <select id="w-worldgen" v-model="worldgenPreset" class="select" :disabled="worldgenFrozen">
              <option v-for="p in worldgenPresets" :key="p.data" :value="p.data">
                {{ p.label ? p.label + '（' + p.data + '）' : p.data }}
              </option>
            </select>
          </UiField>

          <UiField label="世界规则预设" for-id="w-settings" tip="决定游戏规则">
            <select id="w-settings" v-model="settingsPreset" class="select" :disabled="frozen">
              <option v-for="p in settingsPresets" :key="p.data" :value="p.data">
                {{ p.label ? p.label + '（' + p.data + '）' : p.data }}
              </option>
            </select>
          </UiField>
        </div>

        <div class="switches" style="margin-top: 14px">
          <UiSwitch v-model="enabled" label="启用本文件" :disabled="frozen" />
        </div>

        <div v-if="!enabled" class="notice">
          已关闭 <code>override_enabled</code>：游戏仍会读取本文件，但不会应用里面的设置。
        </div>
        <div v-if="worldGenerated" class="notice">
          <Icon name="info" size="16" />
          <span>
            该房间<strong>已生成过世界</strong>：世界生成类配置已冻结——
            这些选项只在生成世界时读取，改了也不会重建已有地图。
            如需调整地形、资源分布等，请新建房间重新生成，或另行导入存档。
          </span>
        </div>

        <p class="path-line" style="margin-top: 12px">{{ view.file }}</p>
      </template>
    </UiCard>

    <UiCard v-if="view && !loading && !loadError" class="world-options-card">
      <template #title>选项</template>
      <template #hint>共 {{ totalOptions }} 项</template>

      <div class="world-toolbar">
        <div class="seg">
          <button
            class="seg-btn"
            :class="{ 'is-active': category === 'settings' }"
            type="button"
            @click="category = 'settings'"
          >
            世界规则 <span class="seg-count">{{ categoryCounts.settings }}</span>
          </button>
          <button
            class="seg-btn"
            :class="{ 'is-active': category === 'worldgen' }"
            type="button"
            @click="category = 'worldgen'"
          >
            世界生成 <span class="seg-count">{{ categoryCounts.worldgen }}</span>
          </button>
        </div>
      </div>

      <div class="world-toolbar world-toolbar-sub">
        <input v-model="keyword" class="input" placeholder="搜索选项（中英文均可）">
        <label class="switch">
          <input v-model="onlyChanged" type="checkbox">
          <span>只看已改动</span>
        </label>
        <span class="world-count">
          {{ changedCount ? '已改 ' + changedCount + ' 项' : '未做改动' }}
        </span>
        <button class="btn btn-sm" :disabled="!dirty || frozen" @click="resetAll">重置</button>
        <button class="btn btn-sm btn-primary" :disabled="!dirty || frozen || saving" @click="save">
          {{ saving ? '保存中…' : '保存' }}
        </button>
      </div>

      <UiEmpty
        v-if="!visibleGroups.length"
        icon="inbox"
        :text="onlyChanged ? '还没有改动过任何选项' : '没有匹配的选项'"
      />

      <template v-else>
        <!-- 服务器运行中冻结：改了也不生效，引导用户先关服 -->
        <div v-if="frozen" class="notice" style="margin-bottom: 12px">
          <Icon name="info" size="16" />
          <span>
            服务器运行中，选项已冻结。请先<strong>关闭服务器</strong>再修改。
          </span>
        </div>
        <!-- 世界已生成：世界生成类选项只在生成世界时读取，锁死防止无效修改 -->
        <div
          v-else-if="worldGenerated && category === 'worldgen'"
          class="notice"
          style="margin-bottom: 12px"
        >
          <Icon name="info" size="16" />
          <span>
            世界已生成，世界生成类选项已冻结——它们只在生成世界时读取。
          </span>
        </div>

        <OptionGroupPanel
          v-for="g in visibleGroups"
          :key="g.category + '/' + g.id"
          :group="g"
          :value-of="valueOf"
          :is-changed="isChanged"
          :disabled="frozen || (worldGenerated && g.category === 'worldgen')"
          @change="onOptionChange"
        />
      </template>
    </UiCard>
  </div>
</template>
