<script setup>
// 创意工坊面板：按关键词搜索 / 按 ID 链接查询，结果可直接下载。
//
// 网络现实：创意工坊网页在境内不可直连，搜索依赖 Steam Web API（可达）。
// 其中按 ID/链接查详情免 key，关键词搜索需要免费的 Web API Key——
// 所以查询入口合并成一个输入框：内容像 ID/链接就走详情，否则走搜索；
// 搜索没配 key 时给出明确指引并就地提供 key 设置。
//
// 下载不在本组件：emit('download', mod) 交给父组件走统一的
// 单模组更新会话（复用轮询与输出面板）。

import { computed, ref } from 'vue';

import Icon from '@/components/ui/Icon.vue';
import UiPill from '@/components/ui/UiPill.vue';
import { workshopApi } from '@/lib/api.js';
import { guard, toast } from '@/lib/toast.js';

const props = defineProps({
  /** 全局更新会话进行中：下载按钮禁用 */
  updating: { type: Boolean, default: false },
});

const emit = defineEmits(['download']);

const keyword = ref('');
const searching = ref(false);
const results = ref([]);
const searched = ref(false);

/** 判断输入是不是「ID / 链接」形态（纯数字、workshop-数字、含 id= 的链接） */
const looksLikeId = computed(() => {
  const v = keyword.value.trim();
  return v !== '' && (/^\d{4,15}$/.test(v) || /^workshop-\d+$/.test(v) || /id=\d+/.test(v));
});

/* ---------- API Key 设置 ---------- */

const showKeyForm = ref(false);
const apiKeyInput = ref('');
const keyLoaded = ref(false);
const savingKey = ref(false);

async function toggleKeyForm() {
  showKeyForm.value = !showKeyForm.value;
  if (showKeyForm.value && !keyLoaded.value) {
    const { ok, data } = await guard(() => workshopApi.getKey());
    if (ok) {
      apiKeyInput.value = data.steamApiKey || '';
      keyLoaded.value = true;
    }
  }
}

async function saveKey() {
  savingKey.value = true;
  const { ok } = await guard(
    () => workshopApi.saveKey(apiKeyInput.value.trim()),
    { success: apiKeyInput.value.trim() ? 'API Key 已保存' : 'API Key 已清空' },
  );
  savingKey.value = false;
  if (ok) showKeyForm.value = false;
}

/* ---------- 查询 ---------- */

function formatSize(item) {
  if (!item.fileSize) return '';
  const mb = item.fileSize / 1048576;
  return mb >= 1 ? mb.toFixed(1) + ' MB' : Math.max(1, Math.round(item.fileSize / 1024)) + ' KB';
}

async function query() {
  const v = keyword.value.trim();
  if (!v || searching.value) return;
  searching.value = true;
  try {
    if (looksLikeId.value) {
      // ID / 链接模式
      results.value = await workshopApi.details(v);
    } else {
      results.value = await workshopApi.search(v);
    }
    searched.value = true;
  } catch (err) {
    toast(err.message, 'err');
  } finally {
    searching.value = false;
  }
}

/** 提取纯 id（下载接口要 workshop-<数字> 格式，交给父组件拼装） */
function download(item) {
  if (props.updating) return;
  emit('download', { id: item.id, name: item.title, installed: item.installed });
}
</script>

<template>
  <div class="workshop">
    <div class="workshop-input">
      <input
        v-model="keyword"
        class="input"
        placeholder="搜索关键词，或粘贴模组 ID / 创意工坊链接"
        @keyup.enter="query"
      >
      <button class="btn btn-sm btn-primary" :disabled="searching || !keyword.trim()" @click="query">
        <Icon name="globe" size="14" />{{ searching ? '查询中…' : (looksLikeId ? '查询' : '搜索') }}
      </button>
    </div>

    <p v-if="!looksLikeId" class="field-tip">
      关键词搜索需要免费的
      <button class="link-btn" type="button" @click="toggleKeyForm">Steam Web API Key</button>
      （已配置则忽略本提示）；粘贴 ID 或链接查询无需任何配置。
    </p>

    <div v-if="showKeyForm" class="workshop-key">
      <input v-model="apiKeyInput" class="input" type="text"
             placeholder="Steam Web API Key（32 位十六进制，可留空清除）">
      <button class="btn btn-sm" :disabled="savingKey" @click="saveKey">
        {{ savingKey ? '保存中…' : '保存' }}
      </button>
    </div>

    <UiPill v-if="searching" tone="info">查询中…</UiPill>

    <div v-else-if="results.length" class="workshop-list">
      <div v-for="item in results" :key="item.id" class="workshop-item">
        <img
          v-if="item.previewUrl"
          class="workshop-thumb"
          :src="item.previewUrl"
          loading="lazy"
          alt=""
          @error="$event.target.style.display = 'none'"
        >
        <div class="workshop-info">
          <div class="workshop-title">
            {{ item.title || item.id }}
            <UiPill v-if="item.installed" tone="ok">已下载</UiPill>
          </div>
          <p v-if="item.shortDesc" class="workshop-desc">{{ item.shortDesc }}</p>
          <div class="workshop-meta">
            <span class="mod-id">workshop-{{ item.id }}</span>
            <span v-if="formatSize(item)">{{ formatSize(item) }}</span>
            <span v-for="t in (item.tags || []).filter(t => !t.startsWith('all_clients') && !t.startsWith('version'))"
                  :key="t" class="workshop-tag">{{ t }}</span>
          </div>
        </div>
        <button class="btn btn-sm" :disabled="updating" :title="updating ? '有更新会话进行中' : ''"
                @click="download(item)">
          <Icon name="download" size="14" />{{ item.installed ? '重新下载' : '下载' }}
        </button>
      </div>
    </div>

    <p v-else-if="searched && !results.length" class="field-tip">没有查到结果，换个关键词试试</p>
  </div>
</template>
