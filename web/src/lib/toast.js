// 右下角提示条。
//
// 做成模块级单例：任何模块都能直接 import { toast } 调用，
// 不需要层层传 props 或注入，ToastHost 组件只负责渲染。

import { ref } from 'vue';

const items = ref([]);
let seq = 0;

export function useToasts() {
  return items;
}

/**
 * 弹一条提示。
 * @param {string} message 文案
 * @param {''|'err'|'warn'} kind 类型，err 停留更久
 */
export function toast(message, kind = '') {
  const id = ++seq;
  items.value.push({ id, message, kind });
  setTimeout(() => dismiss(id), kind === 'err' ? 6000 : 3200);
}

export function dismiss(id) {
  const i = items.value.findIndex(t => t.id === id);
  if (i >= 0) items.value.splice(i, 1);
}

/**
 * 执行一个可能抛错的异步动作，失败时自动弹错误提示。
 *
 * 视图里大量「调用 -> 捕获 -> 提示」都是同一套写法，收敛到这里：
 *   const { ok } = await guard(() => rooms.start(shard), { success: '启动指令已发送' });
 *   // ok 为 false 时不要继续后续步骤（例如关闭编辑表单）
 */
export async function guard(fn, { success = '', kind = '' } = {}) {
  try {
    const data = await fn();
    if (success) toast(success, kind);
    return { ok: true, data };
  } catch (err) {
    toast(err.message, 'err');
    return { ok: false, error: err };
  }
}
