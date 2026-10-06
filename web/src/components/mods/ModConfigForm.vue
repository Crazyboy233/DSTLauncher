<script setup>
// 模组配置表单：按 modinfo.lua 声明的 schema 动态渲染。
//
// 每个配置项都是独立接口调用（后端一次只接受一个键），
// 因此保存是逐个提交；某项失败会立即中断并提示，避免部分写入却显示成功。

import { computed, onMounted, reactive, ref } from 'vue';

import UiSkeleton from '@/components/ui/UiSkeleton.vue';
import { modApi } from '@/lib/api.js';
import { toast } from '@/lib/toast.js';

const props = defineProps({
  roomId: { type: Number, required: true },
  mod: { type: Object, required: true },
  /** 服务器运行中冻结：控件只读、禁止保存 */
  disabled: { type: Boolean, default: false },
});

const schema = ref([]);
const values = reactive({});
const loading = ref(true);
const loadError = ref('');
const saving = ref(false);

/** 配置项的类型：有 options 就是下拉框，否则看 type */
function fieldType(option) {
  if (option.options && option.options.length) return 'select';
  if (option.type === 'boolean') return 'boolean';
  if (option.type === 'number') return 'number';
  return 'string';
}

/**
 * 把后端解析出的值归一成表单期望的类型。
 * Lua 里 true 可能被解析成字符串 "true"，直接丢给 <select> 会选不中任何选项。
 */
function normalize(option, raw) {
  const type = fieldType(option);
  if (type === 'boolean') {
    if (typeof raw === 'string') return raw.toLowerCase() === 'true';
    return !!raw;
  }
  if (type === 'number') {
    const n = Number(raw);
    return Number.isFinite(n) ? n : 0;
  }
  return raw == null ? '' : String(raw);
}

onMounted(async () => {
  try {
    const list = await modApi.schema(props.roomId, props.mod.id);
    schema.value = list || [];
    const current = props.mod.config || {};
    schema.value.forEach(o => {
      const raw = (o.name in current) ? current[o.name] : o.default;
      values[o.name] = normalize(o, raw);
    });
  } catch (err) {
    loadError.value = err.message;
  } finally {
    loading.value = false;
  }
});

const hasOptions = computed(() => schema.value.length > 0);

async function save() {
  if (props.disabled) {
    toast('服务器运行中，请先关闭服务器再修改配置', 'warn');
    return;
  }
  saving.value = true;
  try {
    for (const o of schema.value) {
      const type = fieldType(o);
      const raw = values[o.name];
      // 后端按 JSON 字面量解析 value 参数，数字/布尔要原样传
      let value;
      if (type === 'number') value = JSON.stringify(Number(raw) || 0);
      else if (type === 'boolean') value = raw === true || raw === 'true' ? 'true' : 'false';
      else value = JSON.stringify(raw == null ? '' : String(raw));

      await modApi.setConfig(props.roomId, props.mod.id, o.name, value);
    }
    toast('模组配置已保存，重启后生效');
  } catch (err) {
    toast(err.message, 'err');
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <UiSkeleton v-if="loading" :rows="2" />

  <p v-else-if="loadError" class="field-tip">读取配置项失败：{{ loadError }}</p>

  <p v-else-if="!hasOptions" class="field-tip">该模组没有可配置项。</p>

  <template v-else>
    <p class="field-tip" style="margin-bottom: 10px">修改后需重启服务器生效</p>

    <div v-for="o in schema" :key="o.name" class="opt">
      <label :for="`opt_${mod.id}_${o.name}`">{{ o.label || o.name }}</label>

      <select
        v-if="fieldType(o) === 'select'"
        :id="`opt_${mod.id}_${o.name}`"
        v-model="values[o.name]"
        class="select"
        :disabled="disabled"
      >
        <option v-for="op in o.options" :key="op" :value="op">{{ op }}</option>
      </select>

      <select
        v-else-if="fieldType(o) === 'boolean'"
        :id="`opt_${mod.id}_${o.name}`"
        v-model="values[o.name]"
        class="select"
        :disabled="disabled"
      >
        <option :value="true">true</option>
        <option :value="false">false</option>
      </select>

      <input
        v-else-if="fieldType(o) === 'number'"
        :id="`opt_${mod.id}_${o.name}`"
        v-model="values[o.name]"
        class="input"
        type="number"
        :disabled="disabled"
        :min="o.min !== undefined && o.min !== '' ? o.min : undefined"
        :max="o.max !== undefined && o.max !== '' ? o.max : undefined"
      >

      <input
        v-else
        :id="`opt_${mod.id}_${o.name}`"
        v-model="values[o.name]"
        class="input"
        type="text"
        :disabled="disabled"
      >

      <div v-if="o.description" class="opt-desc">{{ o.description }}</div>
    </div>

    <div style="margin-top: 12px">
      <button class="btn btn-sm btn-primary" :disabled="saving || disabled" @click="save">
        {{ saving ? '保存中…' : '保存配置' }}
      </button>
    </div>
  </template>
</template>
