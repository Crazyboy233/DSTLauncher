<script setup>
// 上传存档面板：把一个外部存档整包（Klei 下载 / 面板导出）导入到房间。
//
// 语义是「存档占用房间」：存档里的可覆盖配置写回房间表单，世界数据拷进房间目录，
// 随后 ini 由面板重新生成，所以导入完可以直接在房间页二次编辑。
//
// 不做 modal：与其它页面保持一致，直接把面板插到房间列表下方，省掉一套弹窗层级。

import { computed, ref, watch } from 'vue';

import UiCard from '@/components/ui/UiCard.vue';
import UiField from '@/components/ui/UiField.vue';
import UiPill from '@/components/ui/UiPill.vue';
import UiProgress from '@/components/ui/UiProgress.vue';
import Icon from '@/components/ui/Icon.vue';
import { saveApi } from '@/lib/api.js';
import { toast } from '@/lib/toast.js';
import { fmtSize } from '@/lib/format.js';

const props = defineProps({
  rooms: { type: Array, default: () => [] },
  /** 打开时预选的房间（从房间卡片的「上传存档」进来） */
  presetRoomId: { type: Number, default: null },
  currentRoomId: { type: Number, default: null },
});

const emit = defineEmits(['close', 'done']);

const MODE_OVERWRITE = 'overwrite';
const MODE_NEW = 'new';

const mode = ref(props.rooms.length ? MODE_OVERWRITE : MODE_NEW);
const roomId = ref(
  props.presetRoomId || props.currentRoomId || (props.rooms[0] && props.rooms[0].id) || null,
);

const file = ref(null);
const dragging = ref(false);
const busy = ref(false);
const percent = ref(0);
const error = ref('');
/** 导入成功后的摘要 */
const result = ref(null);
const fileInput = ref(null);

const selectedRoom = computed(() => props.rooms.find(r => r.id === roomId.value) || null);

/** 运行中的房间不能覆盖：Windows 下存档文件被占用，且退服会把新存档盖回去 */
const running = computed(() => {
  const s = selectedRoom.value;
  return !!s && (s.statuses || []).some(
    x => x.state === 'running' || x.state === 'starting' || x.state === 'stopping',
  );
});

const busyText = computed(() => (
  percent.value >= 100 ? '上传完成，服务器正在解压并导入…' : `正在上传 ${percent.value}%`
));

const submitLabel = computed(() => {
  if (mode.value === MODE_NEW) return '上传并新建房间';
  return selectedRoom.value ? `覆盖「${selectedRoom.value.name}」` : '上传';
});

const canSubmit = computed(() => (
  !busy.value && !!file.value && (mode.value === MODE_NEW || !!roomId.value)
));

// 房间一个都没有时只能新建
watch(() => props.rooms.length, n => {
  if (!n) {
    mode.value = MODE_NEW;
    roomId.value = null;
  }
}, { immediate: true });

function chooseFile(f) {
  error.value = '';
  if (!f) return;
  if (!/\.zip$/i.test(f.name)) {
    error.value = '请选择 .zip 压缩包（Klei 下载的整包，或面板导出的备份）';
    return;
  }
  file.value = f;
  result.value = null;
}

function onPick(e) {
  chooseFile(e.target.files && e.target.files[0]);
  // 清空 value，允许连续两次选择同一个文件
  e.target.value = '';
}

function onDrop(e) {
  dragging.value = false;
  chooseFile(e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0]);
}

function clearFile() {
  file.value = null;
  result.value = null;
  error.value = '';
}

async function submit() {
  if (!canSubmit.value) return;
  if (mode.value === MODE_OVERWRITE && running.value) {
    error.value = '该房间正在运行，请先停止再上传';
    return;
  }

  error.value = '';
  result.value = null;
  busy.value = true;
  percent.value = 0;

  try {
    const data = await saveApi.upload(
      file.value,
      { mode: mode.value, roomId: mode.value === MODE_OVERWRITE ? roomId.value : undefined },
      p => { percent.value = p; },
    );
    result.value = (data && data.summary) || null;
    file.value = null;
    toast(mode.value === MODE_NEW ? '存档已导入并新建房间' : '存档已导入，房间配置已覆盖');
    emit('done');
  } catch (err) {
    error.value = err.message;
  } finally {
    busy.value = false;
    percent.value = 0;
  }
}
</script>

<template>
  <UiCard>
    <template #title>上传存档</template>
    <template #hint>存档占用目标房间：玩法配置写回房间，世界数据拷进房间目录</template>
    <template #actions>
      <button class="btn btn-sm" :disabled="busy" @click="emit('close')">
        <Icon name="x" size="14" />关闭
      </button>
    </template>

    <div class="notice" style="margin-bottom: 18px">
      <Icon name="info" size="16" />
      <span>
        支持 Klei 下载或面板导出的整包 <b>.zip</b>。
        模组、世界设置、玩家名单会一并带入；端口、集群密钥、主分片 IP
        属于本机配置，不会被存档覆盖。
      </span>
    </div>

    <section class="form-section">
      <h3>导入到</h3>

      <div class="switches" style="margin-bottom: 14px">
        <label class="switch">
          <input v-model="mode" type="radio" :value="MODE_OVERWRITE" :disabled="!rooms.length || busy">
          覆盖已有房间
        </label>
        <label class="switch">
          <input v-model="mode" type="radio" :value="MODE_NEW" :disabled="busy">
          新建房间
        </label>
      </div>

      <div v-if="mode === MODE_OVERWRITE" class="form-grid">
        <UiField label="目标房间" for-id="s_target">
          <select id="s_target" v-model.number="roomId" class="select" :disabled="busy">
            <option v-for="r in rooms" :key="r.id" :value="r.id">
              {{ r.name }}（{{ r.key }}）
            </option>
          </select>
        </UiField>
      </div>

      <p v-if="mode === MODE_OVERWRITE && !rooms.length" class="field-tip">
        还没有任何房间，请改用「新建房间」。
      </p>
      <p v-else-if="running" class="field-tip" style="color: var(--warn)">
        该房间正在运行，需先停止服务器才能覆盖存档。
      </p>
      <p v-else-if="mode === MODE_OVERWRITE" class="field-tip">
        房间现有的世界存档与模组配置会被存档包里的内容替换；端口保持不变。
      </p>
      <p v-else class="field-tip">
        端口由系统自动分配，房间创建后可直接在房间页继续编辑。
      </p>
    </section>

    <section class="form-section">
      <h3>存档压缩包</h3>

      <div
        class="dropzone"
        :class="{ 'is-over': dragging, 'is-filled': !!file }"
        @click="!file && fileInput && fileInput.click()"
        @dragover.prevent="dragging = true"
        @dragleave.prevent="dragging = false"
        @drop.prevent="onDrop"
      >
        <Icon :name="file ? 'archive' : 'upload'" size="22" />
        <div class="dropzone-text">
          <b v-if="file">{{ file.name }}</b>
          <b v-else>点击选择，或把 .zip 拖到这里</b>
          <span v-if="file">{{ fmtSize(file.size) }}</span>
          <span v-else>含 Cluster_* 目录的存档整包</span>
        </div>
        <button v-if="file" class="btn btn-sm" :disabled="busy" @click.stop="clearFile">
          重新选择
        </button>
      </div>
      <input
        ref="fileInput"
        class="dropzone-input"
        type="file"
        accept=".zip,application/zip"
        @change="onPick"
      >
    </section>

    <div v-if="busy" style="margin-bottom: 14px">
      <UiProgress :value="percent" />
      <p class="field-tip">{{ busyText }}</p>
    </div>

    <div v-if="error" class="notice notice-danger" style="margin-bottom: 14px">
      <Icon name="alert" size="16" />
      <span>{{ error }}</span>
    </div>

    <section v-if="result" class="form-section">
      <h3>导入结果</h3>
      <div class="import-summary">
        <div class="import-row">
          <span>房间</span>
          <b>{{ result.roomName }}</b>
          <UiPill :tone="result.newRoom ? 'ok' : 'info'">
            {{ result.newRoom ? '新建' : '覆盖' }}
          </UiPill>
        </div>
        <div class="import-row">
          <span>分片</span>
          <b>{{ (result.shards || []).join(' + ') || '—' }}</b>
        </div>
        <div class="import-row">
          <span>世界数据</span>
          <b>{{ result.hasSave ? '已导入' : '存档包里没有世界数据' }}</b>
        </div>
        <div class="import-row">
          <span>模组</span>
          <b v-if="result.modsTotal">
            已导入 {{ result.modsTotal }} 个<template v-if="(result.modsMissing || []).length">
              ，其中 {{ result.modsMissing.length }} 个未下载，需到「模组」页点「更新模组」</template>
          </b>
          <b v-else>存档里没有模组配置，房间原有模组保持不变</b>
        </div>
        <div class="import-row">
          <span>令牌</span>
          <b>{{ result.tokenSource }}</b>
        </div>
        <div v-if="(result.worldFiles || []).length" class="import-row">
          <span>世界设置</span>
          <div class="import-list">
            <b v-for="(w, i) in result.worldFiles" :key="'w' + i">{{ w }}</b>
          </div>
        </div>
        <div v-if="(result.listFiles || []).length" class="import-row">
          <span>玩家名单</span>
          <b>{{ result.listFiles.join('、') }}</b>
        </div>
        <div v-if="(result.notes || []).length" class="import-row">
          <span>说明</span>
          <div class="import-list">
            <b v-for="(n, i) in result.notes" :key="'n' + i">{{ n }}</b>
          </div>
        </div>
        <div v-if="(result.warnings || []).length" class="import-row">
          <span>警告</span>
          <div class="import-list import-list-warn">
            <b v-for="(w, i) in result.warnings" :key="'x' + i">{{ w }}</b>
          </div>
        </div>
      </div>
    </section>

    <div class="form-actions">
      <button class="btn btn-primary" :disabled="!canSubmit" @click="submit">
        <Icon name="upload" size="14" />{{ busy ? '处理中…' : submitLabel }}
      </button>
      <button class="btn" :disabled="busy" @click="emit('close')">关闭</button>
    </div>
  </UiCard>
</template>
