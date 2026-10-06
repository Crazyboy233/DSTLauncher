<script setup>
// 服务器文件：安装状态 / 复用来源 / 位置配置 / 一键安装与更新。
//
// 两种来源的差别只体现在「能不能在这里更新」：
//   - 面板自带安装：由本面板用 SteamCMD 安装，可在面板里检查更新
//   - 复用已有安装（Steam 工具里装的那份）：同一套文件，更新走 Steam；
//     面板强行走 SteamCMD 会跟 Steam 的文件管理打架

import { computed, onMounted, ref, watch } from 'vue';

import UiPill from '@/components/ui/UiPill.vue';
import UiProgress from '@/components/ui/UiProgress.vue';
import { useRoomsStore } from '@/stores/rooms.js';
import { useSystemStore } from '@/stores/system.js';
import { guard, toast } from '@/lib/toast.js';

const system = useSystemStore();
const rooms = useRoomsStore();

/**
 * 是否有分片正在运行。
 *
 * 有的话不能安装/更新：Windows 下正在使用的 exe 与 dll 被锁定，
 * SteamCMD 会替换失败，最坏是留下一个更新了一半的安装。
 * 后端也会拒绝，这里只是让按钮提前禁用、把原因说清楚。
 */
const busy = computed(() => rooms.list.some(r => r.running));

// 后端给的进度是「下载服务器文件 12.34%」这类文本，这里取出百分比喂给进度条
const percent = computed(() => {
  const m = /([\d.]+)\s*%/.exec(system.progress || '');
  if (!m) return -1;
  const n = Number(m[1]);
  return Number.isFinite(n) ? n : -1;
});

/* ---------- 位置配置 ---------- */

const server = computed(() => system.server);
const locked = computed(() => !!server.value?.locked || system.installing);

const draftDir = ref('');
const draftArch = ref('auto');

// 外部状态变化时同步草稿（例如自动探测到了新路径）
watch(server, s => {
  if (!s) return;
  draftArch.value = s.archPref || 'auto';
  draftDir.value = s.pinned ? s.root : '';
}, { immediate: true });

const ARCH_LABEL = { auto: '自动（优先 64 位）', 64: '64 位', 32: '32 位' };

async function apply(dir) {
  const { ok } = await guard(
    () => system.saveServer(dir, draftArch.value),
    { success: '服务器文件位置已更新' },
  );
  if (ok) toast('新设置会在下次启动分片时生效');
}

/** 改架构：路径按当前输入框的值一起提交，避免把自动探测固定成死路径 */
function applyArch() {
  return apply(draftDir.value);
}

function useCandidate(root) {
  draftDir.value = root;
  return apply(root);
}

function resetAuto() {
  draftDir.value = '';
  return apply('');
}

onMounted(async () => {
  await guard(() => system.refresh());
  await guard(() => system.loadServer());
});

function install() {
  if (!confirm('开始下载安装 DST 专用服务器？约 2GB，耗时较长。')) return;
  guard(() => system.startInstall(), { success: '安装任务已启动' });
}

function update() {
  guard(() => system.update(), { success: '更新任务已启动' });
}
</script>

<template>
  <!-- 安装 / 更新进行中 -->
  <template v-if="system.installing">
    <div class="shard-row">
      <span class="dot starting" />
      <span>正在安装 / 更新</span>
      <span v-if="percent >= 0" class="shard-meta">{{ percent }}%</span>
    </div>
    <div class="shard-row shard-row-block">
      <UiProgress :value="percent" />
      <p class="field-tip">{{ system.progress || '准备中…' }}</p>
    </div>
    <div class="shard-row">
      <span class="shard-meta">完成后会自动刷新，无需手动操作</span>
    </div>
  </template>

  <!-- 已就绪 -->
  <template v-else-if="system.installed">
    <div class="shard-row">
      <span class="dot running" />
      <span>{{ system.external ? '已复用已安装的服务器' : '服务器已安装' }}</span>
      <span class="shard-actions">
        <UiPill :tone="system.external ? 'info' : 'ok'">{{ system.arch }} 位</UiPill>
      </span>
    </div>

    <div class="shard-row">
      <span class="shard-meta">来源</span>
      <span class="shard-meta">{{ system.source || '未知' }}</span>
    </div>
    <div class="shard-row">
      <span class="shard-meta">游戏版本</span>
      <span class="shard-meta">{{ system.version || '未知' }}</span>
    </div>
    <div class="shard-row shard-row-block">
      <p class="path-line">{{ system.root }}</p>
    </div>

    <div class="shard-row">
      <span v-if="system.external" class="shard-meta">
        更新请在 Steam 中更新「Don't Starve Together Dedicated Server」工具，面板会自动使用最新版本。
      </span>
      <span v-else class="shard-actions">
        <button class="btn btn-sm" :disabled="busy" @click="update">检查更新</button>
      </span>
    </div>

    <!-- 更新前必须先停服，否则文件被占用会导致更新失败或只更新一半 -->
    <div v-if="busy" class="shard-row">
      <span class="shard-meta">
        有分片正在运行。{{ system.external ? '在 Steam 中更新前' : '更新前' }}请先全部停止，
        否则正在使用的服务器文件被锁定，更新会失败或只更新一半。
      </span>
    </div>
  </template>

  <!-- 未安装 -->
  <template v-else>
    <div class="shard-row">
      <span class="dot crashed" />
      <span>未找到专用服务器</span>
    </div>
    <div class="shard-row shard-row-block">
      <p class="field-tip">
        若你已在 Steam 中安装「Don't Starve Together Dedicated Server」工具，
        可在下方直接指定它的目录，或重启面板让它自动探测。
      </p>
    </div>
    <div class="shard-row">
      <span class="shard-actions">
        <button class="btn btn-sm btn-primary" @click="install">一键安装（约 2GB）</button>
      </span>
    </div>
  </template>

  <!-- 位置配置 -->
  <details v-if="server" class="details" style="margin-top: 14px">
    <summary>更改服务器文件位置</summary>

    <p v-if="locked" class="field-tip" style="margin-bottom: 10px">
      有分片正在运行，需先全部停止才能更改位置。
    </p>

    <div class="form-grid form-grid-top">
      <div class="field field-block">
        <label class="field-label" for="srv_dir">安装目录</label>
        <div class="inline-row">
          <input
            id="srv_dir"
            v-model="draftDir"
            class="input mono"
            :disabled="locked"
            placeholder="留空 = 自动探测 Steam 库"
          >
          <button class="btn btn-sm btn-primary" :disabled="locked" @click="apply(draftDir)">
            应用
          </button>
        </div>
        <p class="field-tip">
          填「Don't Starve Together Dedicated Server」那一层，也可以直接粘贴里面的 bin 目录。
        </p>
      </div>

      <div class="field">
        <label class="field-label" for="srv_arch">架构</label>
        <select
          id="srv_arch"
          v-model="draftArch"
          class="select"
          :disabled="locked"
          @change="applyArch"
        >
          <option v-for="a in server.archOptions" :key="a" :value="a">
            {{ ARCH_LABEL[a] || a }}
          </option>
        </select>
      </div>
    </div>

    <p class="field-tip" style="margin-top: 10px">
      64 位可突破 32 位进程约 2GB 的内存上限，模组较多时更稳。
    </p>

    <template v-if="server.candidates.length">
      <p class="field-tip" style="margin-top: 14px">自动探测到的安装：</p>
      <div class="candidate-list">
        <div v-for="c in server.candidates" :key="c.root" class="candidate">
          <span class="mono">{{ c.root }}</span>
          <UiPill tone="muted">{{ c.source }}</UiPill>
          <button
            class="btn btn-sm"
            :disabled="locked || c.root === server.root"
            @click="useCandidate(c.root)"
          >
            {{ c.root === server.root ? '使用中' : '使用' }}
          </button>
        </div>
      </div>
    </template>

    <div class="form-actions" style="margin-top: 14px">
      <button
        class="btn btn-sm"
        :disabled="locked || !server.pinned"
        @click="resetAuto"
      >
        恢复自动探测
      </button>
      <span class="field-tip" style="align-self: center">
        {{ server.pinned ? '当前为手动指定' : '当前为自动探测' }}
      </span>
    </div>

    <p class="field-tip" style="margin-top: 12px">
      配置保存在 <code>{{ server.configPath }}</code>，重启面板后依然有效。
    </p>
  </details>
</template>
