<script setup>
// 模组页。
//
// 模组实体的存放是跨房间共享的（-ugc_directory 指向同一份），
// 因此「更新模组」是全局动作：把所有房间启用模组的并集补齐到本地，
// 任意一个房间点效果都一样。服务器启动时带 -skip_update_server_mods，
// 只加载不下载，所以启动前必须先把模组更新到位。

import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue';

import Icon from '@/components/ui/Icon.vue';
import UiCard from '@/components/ui/UiCard.vue';
import UiEmpty from '@/components/ui/UiEmpty.vue';
import UiPill from '@/components/ui/UiPill.vue';
import UiSkeleton from '@/components/ui/UiSkeleton.vue';
import ModItem from '@/components/mods/ModItem.vue';
import WorkshopPanel from '@/components/mods/WorkshopPanel.vue';
import { useRoomsStore } from '@/stores/rooms.js';
import { modApi, modUpdateApi } from '@/lib/api.js';
import { guard, toast } from '@/lib/toast.js';

const rooms = useRoomsStore();

/** 服务器运行中冻结：模组改动重启后才生效 */
const frozen = computed(() => rooms.running);

const mods = ref([]);
const loading = ref(false);
const loadError = ref('');

/* ---------- 本地（自制）mod 目录 ---------- */

/**
 * 自制 mod 的唯一正确位置是「服务器安装目录」下的 mods/。
 *
 * 面板自己有个 ugc_mods（只存创意工坊 mod，还要 acf 登记），程序目录里又留着
 * 早期开发用的 mods/——这两处都容易被当成「放自己 mod 的地方」，但引擎是按
 * MODS_ROOT（固定相对 bin 的 ../mods）拼路径加载本地 mod 的，放错了既不加载
 * 也不报错。所以底部这条提示要常驻，列表空着的时候尤其需要。
 */
const modsDir = ref('');
const modsDirInstalled = ref(true);

async function loadModsDir() {
  try {
    const d = await modApi.dir();
    modsDir.value = (d && d.modsDir) || '';
    modsDirInstalled.value = !d || d.installed !== false;
  } catch (_) {
    // 取不到就退回通用文案（硬编码 Steam 默认路径），不打扰用户
    modsDir.value = '';
  }
}

/**
 * 已启用的模组置顶显示，组内按名称排序。
 *
 * 排序放在页面侧：每个房间的启用集合不同，切换房间时拿到的是一份新列表；
 * 而启用/禁用是就地切换的，重新请求反而慢。用计算属性，数据一变就重算。
 *
 * 组内必须自己排：后端只保证「overrides 里的顺序」和「其余按 ID」，
 * 名称顺序对用户没有意义。Intl.Collator 的中文排序按拼音，
 * numeric 让「v2」「v10」按数值而不是字符排。
 */
const collator = new Intl.Collator('zh-Hans-CN', { numeric: true, sensitivity: 'base' });

const sortedMods = computed(() =>
  [...mods.value].sort((a, b) => {
    if (a.enabled !== b.enabled) return a.enabled ? -1 : 1;
    const byName = collator.compare(a.name || a.id, b.name || b.id);
    // 同名（比如多个 mod 都没解析出名字）时按 ID 兜底，保证顺序稳定
    return byName !== 0 ? byName : collator.compare(a.id, b.id);
  }));

/** 已启用但本地还没有的模组数量（客户端模组服务器不加载，不计入） */
const missingCount = computed(() =>
  mods.value.filter(m => m.enabled && !m.clientOnly && !m.installed).length);

/**
 * 已启用、本地有文件、但 Steam 没登记的模组数量。
 * 这类目录引擎看不见（静默忽略），表现就是「启用了却不生效」，
 * 手工拷进 ugc 目录的模组全属于这一类。
 */
const unregisteredCount = computed(() =>
  mods.value.filter(m => m.enabled && m.installed && !m.clientOnly && !m.registered).length);

/** 上一次成功加载的房间 ID，用于识别「切房间」并清掉上一个房间的列表 */
let loadedRoomId = null;

async function load() {
  const room = rooms.current;
  if (!room) {
    mods.value = [];
    loadedRoomId = null;
    loadError.value = '';
    return;
  }
  // 切房间时先清空：各房间的启用集合不同，留着旧列表会在新数据到达前
  // 把上一个房间的模组（含它的置顶顺序）先渲染出来，看起来像串了
  if (loadedRoomId !== room.id) {
    mods.value = [];
    loadedRoomId = room.id;
  }
  loading.value = true;
  loadError.value = '';
  try {
    mods.value = (await modApi.list(room.id)) || [];
  } catch (err) {
    loadError.value = err.message;
  } finally {
    loading.value = false;
  }
}

/* ---------- 更新检查 ---------- */

const checking = ref(false);
/** { checked, updates, missing }；null 表示还没查过 */
const checkResult = ref(null);

/** 有更新的模组，按 id 索引，供列表项打标 */
const updateMap = computed(() => {
  const map = {};
  for (const u of (checkResult.value && checkResult.value.updates) || []) {
    map[u.id] = u;
  }
  return map;
});

const updateNames = computed(() =>
  ((checkResult.value && checkResult.value.updates) || [])
    .map(u => u.name || u.id)
    .join('、'));

/* ---------- 模组预更新 ---------- */

const updating = ref(false);
/** 创意工坊面板的展开状态 */
const workshopOpen = ref(false);
const progress = ref('');
const updateError = ref('');
const output = ref([]);
/** 排队等待下载的模组数量（后端串行消化，不并行） */
const queuedCount = ref(0);
/** 当前正在下载的创意工坊模组 ID（供结果列表做内联提示） */
const downloadingId = ref('');
/** 进度输出块的 DOM 引用，发起更新后滚动过去 */
const updateBlock = ref(null);
let pollTimer = null;

function stopPoll() {
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

/** 把进度块滚进视野：它渲染在页面顶部，搜索结果一长就看不见了 */
function scrollToUpdate() {
  nextTick(() => updateBlock.value?.scrollIntoView({ behavior: 'smooth', block: 'start' }));
}

async function pollUpdate() {
  let st;
  try {
    st = await modUpdateApi.status();
  } catch (_) {
    return // 轮询失败不打断，下一轮再试
  }
  updating.value = !!st.running;
  progress.value = st.progress || '';
  updateError.value = st.error || '';
  output.value = st.output || [];
  queuedCount.value = st.queued || 0;

  if (!st.running) {
    stopPoll();
    downloadingId.value = '';
    // 更新结束后按实际结果汇报，绝不把「没下载到东西」说成成功——
    // 引擎可能在任何阶段静默退出且退出码为 0，只有清点结果才算数
    await load();
    // 未下载 + 未登记 都算「没就绪」：两者都只能靠重新下载解决
    const missing = missingCount.value + unregisteredCount.value;
    // 输出只有「开始…」和结论两行 = 会话没跑引擎，本来就一个都不缺
    const nothingTodo = !st.error && st.output && st.output.length <= 2;
    if (st.error) toast('模组更新失败：' + st.error, 'err');
    else if (nothingTodo) toast('全部模组均已在本地，无需更新');
    else if (missing > 0) {
      toast(`更新结束，但仍有 ${missing} 个模组未下载，请查看下方输出后重试`, 'warn');
    } else {
      toast('模组更新完成');
    }
  }
}

function startPoll() {
  stopPoll();
  pollTimer = setInterval(pollUpdate, 2000);
}

/**
 * 一键更新：先检查已启用的模组，再把需要处理的加入下载队列。
 *
 * 需要处理的有两类：
 *   - 本地有旧版（acf 记录的版本时间落后于工坊）→ 删旧文件重下
 *   - 已启用但本地根本没有 → 直接下载
 * 逐个发单模组请求，后端会串行排队，不会多个引擎进程抢写共享目录。
 */
async function updateAll() {
  const room = rooms.current;
  if (!room || updating.value || checking.value) return;

  checking.value = true;
  let result;
  try {
    result = await modUpdateApi.check(room.id);
    checkResult.value = result;
  } catch (err) {
    toast('检查更新失败：' + err.message, 'err');
    return;
  } finally {
    checking.value = false;
  }

  const todo = new Set();
  for (const u of result.updates || []) todo.add(u.id);
  for (const id of result.notInstalled || []) todo.add(id);

  if (!todo.size) {
    toast('已启用的模组都是最新版，无需更新');
    return;
  }

  let queued = 0;
  for (const id of todo) {
    try {
      await modUpdateApi.start(room.id, null, id);
      queued += 1;
    } catch (err) {
      toast(`${id} 加入更新队列失败：${err.message}`, 'err');
    }
  }
  if (!queued) return;

  toast(`已开始更新 ${queued} 个模组`);
  updating.value = true;
  downloadingId.value = '';
  startPoll();
  pollUpdate();
  scrollToUpdate();
}

/** 单模组更新：下载中有会话在跑时后端会排队，这里不做 updating 拦截 */
async function startOne(mod) {
  const room = rooms.current;
  if (!room) return;
  const { ok, data } = await guard(() => modUpdateApi.start(room.id, null, mod.id));
  if (!ok) return;
  // POST 返回当前会话状态：queued > 0 说明这次点击进入了队列而不是立即开跑
  if (data && data.queued > 0) {
    toast(`${mod.name || mod.id} 已加入下载队列（第 ${data.queued} 位），完成当前任务后自动开始`);
  } else {
    toast(`已开始下载 ${mod.name || mod.id}`);
  }
  updating.value = true;
  downloadingId.value = mod.id;
  startPoll();
  pollUpdate();
  scrollToUpdate();
}

async function stopUpdate() {
  await guard(() => modUpdateApi.stop(), { success: '已请求中止更新' });
}

/**
 * 模组启用状态就地切换成功后同步本地副本。
 *
 * 不重新拉取列表：排序与各种计数都由计算属性驱动，改这一个字段就够了，
 * 列表会立刻重排（刚启用的跳到已启用区），也不会出现整页闪一下。
 */
function onToggled({ id, enabled }) {
  const mod = mods.value.find(m => m.id === id);
  if (mod) mod.enabled = enabled;
}

onMounted(async () => {
  await load();
  await loadModsDir();
  // 面板重启后若上次更新还没结束，恢复轮询而不是丢掉进度
  await pollUpdate();
  if (updating.value) startPoll();
});
onUnmounted(stopPoll);
// 用 current 而不是 currentId 做触发：刷新页面时 currentId 从 localStorage
// 恢复、值不变，watch 不会触发，列表就一直空着；而 current 要等房间列表
// 加载完才从 null 变成房间对象，正好在数据就绪时拉一次模组列表
watch(() => rooms.current, () => {
  checkResult.value = null;
  load();
});
</script>

<template>
  <UiCard>
    <template #title>模组管理</template>
    <template #hint>启用/禁用后需重启服务器才生效；模组实体跨房间共享</template>
    <template #actions>
      <button
        class="btn btn-sm btn-primary"
        :disabled="frozen || !rooms.current || updating || checking"
        title="先检查已启用的模组，再把有新版本、未下载的模组下载下来"
        @click="updateAll"
      >
        <Icon name="download" size="14" />{{ updating ? '更新中…' : (checking ? '检查中…' : '更新模组') }}
      </button>
      <button
        class="btn btn-sm"
        :class="{ 'btn-primary': workshopOpen }"
        :disabled="!rooms.current"
        @click="workshopOpen = !workshopOpen"
      >
        <Icon name="globe" size="14" />创意工坊
      </button>
      <button class="btn btn-sm" @click="load">刷新</button>
    </template>

    <UiSkeleton v-if="!rooms.loaded" :rows="4" />
    <UiEmpty v-else-if="!rooms.current" icon="package" text="请先在「房间」页新建一个房间" />
    <!-- 骨架屏只给首屏：后台刷新（每 15 秒的房间轮询触发的 load）不能
         把整个内容区（含创意工坊搜索结果、更新进度块）拆掉重建，
         否则表现为整页周期性闪烁、搜索结果周期性消失 -->
    <UiSkeleton v-else-if="loading && !mods.length" :rows="4" />
    <UiEmpty v-else-if="loadError" icon="alert" :text="'读取模组失败：' + loadError" />

    <template v-else>
      <div v-if="frozen" class="notice" style="margin-bottom: 14px">
        <Icon name="info" size="16" />
        <span>
          服务器运行中，模组修改已冻结。请先在「总览」页
          <strong>关闭服务器</strong>后再启用/禁用模组、修改配置或更新模组。
        </span>
      </div>
      <template v-else>
        <p v-if="missingCount" class="field-tip" style="margin-bottom: 14px">
          有 {{ missingCount }} 个已启用的模组尚未下载，
          点右上角<strong>「更新模组」</strong>补齐后再启动服务器（启动时会做检查）。
        </p>
        <p v-if="unregisteredCount" class="field-tip" style="margin-bottom: 14px">
          有 {{ unregisteredCount }} 个已启用的模组<strong>未在 Steam 登记</strong>（多为手工拷入
          ugc 目录的），引擎会静默忽略、日志里也没有报错。点这些模组的<strong>「更新」</strong>
          重新下载一次即可补上登记。
        </p>
      </template>

      <!-- 检查结果：明确指出是哪个模组旧了，客户端只会笼统提示「有旧版本 mod」 -->
      <div v-if="checkResult" class="notice" style="margin-bottom: 14px">
        <Icon name="info" size="16" />
        <span v-if="checkResult.updates.length">
          已检查 {{ checkResult.checked }} 个工坊模组，
          <strong>{{ checkResult.updates.length }} 个有新版本</strong>：{{ updateNames }}。
          已加入更新队列，结果见下方进度。
        </span>
        <span v-else>
          已检查 {{ checkResult.checked }} 个工坊模组，全部是最新版。
        </span>
      </div>
      <p
        v-if="checkResult && (checkResult.notInstalled || []).length"
        class="field-tip"
        style="margin-bottom: 14px"
      >
        另有 {{ (checkResult.notInstalled || []).length }} 个已启用但本地不可用的模组，已一并加入队列。
      </p>
      <p
        v-if="checkResult && (checkResult.unregistered || []).length"
        class="field-tip"
        style="margin-bottom: 14px"
      >
        其中 {{ (checkResult.unregistered || []).length }} 个是<strong>手工放入、未在 Steam 登记</strong>的
        （{{ (checkResult.unregistered || []).join('、') }}），重新下载后才会被引擎识别。
      </p>
      <p
        v-if="checkResult && (checkResult.missing || []).length"
        class="field-tip"
        style="margin-bottom: 14px"
      >
        工坊查不到以下模组（可能已下架或 ID 有误）：{{ (checkResult.missing || []).join('、') }}
      </p>

      <!-- 进度块放在创意工坊面板上方：搜索结果一长，放下面就会滚出视野 -->
      <div v-if="updating || output.length" ref="updateBlock" class="mod-update">
        <div class="mod-update-head">
          <b>模组更新</b>
          <UiPill :tone="updating ? 'info' : (updateError ? 'danger' : 'ok')">
            {{ updating ? (progress || '进行中') : (updateError ? '失败' : '完成') }}
          </UiPill>
          <span v-if="queuedCount" class="field-tip">队列中还有 {{ queuedCount }} 个待下载</span>
          <span class="spacer" />
          <button v-if="updating" class="btn btn-sm" @click="stopUpdate">中止</button>
        </div>
        <pre class="mod-update-log">{{ output.join('\n') }}</pre>
      </div>

      <!-- 创意工坊：搜索 / ID 查询，下载走统一更新会话 -->
      <WorkshopPanel
        v-if="workshopOpen"
        :downloading-id="downloadingId"
        :progress="progress"
        @download="startOne"
      />

      <template v-if="mods.length">
        <ModItem
          v-for="m in sortedMods"
          :key="m.id"
          :mod="m"
          :update-info="updateMap[m.id] || null"
          @changed="load"
          @toggled="onToggled"
          @update="startOne"
        />
      </template>
      <UiEmpty
        v-else-if="!updating"
        icon="package"
        text="还没有模组。展开上方「创意工坊」搜索或粘贴 ID 下载，下载完成后会出现在这里。"
      />

      <!-- 本地 mod 目录提示：常在底部，一条模组都没有时最需要它 -->
      <div class="mods-dir">
        <Icon name="folder" size="15" />
        <span>
          自己的 mod 放这里：把整个 mod 文件夹（里面要有 <code>modinfo.lua</code>）拷到服务器安装目录下的
          <strong>mods</strong> 文件夹
          <code v-if="modsDir">{{ modsDir }}</code>
          <code v-else>steam\steamapps\common\Don't Starve Together Dedicated Server\mods</code><template
            v-if="modsDir && !modsDirInstalled"
          >（服务器文件尚未装好，这是装好后的位置）</template>，
          再点右上角「刷新」就会以目录名出现在上面，勾选启用即可。
          放进面板程序目录或 ugc_mods 都不会被加载——ugc_mods 只存创意工坊 mod。
        </span>
      </div>
    </template>
  </UiCard>
</template>
