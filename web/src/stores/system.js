// 服务器文件的安装 / 更新状态。

import { defineStore } from 'pinia';
import { ref } from 'vue';

import { installApi, serverApi } from '@/lib/api.js';
import { toast } from '@/lib/toast.js';

export const useSystemStore = defineStore('system', () => {
  const installed = ref(false);
  const installing = ref(false);
  const progress = ref('');
  const error = ref('');
  const output = ref([]);
  const version = ref('');

  // 服务器来源。external 为 true 表示复用了 Steam 里已装好的那份，
  // 此时安装/更新都应该走 Steam，面板只做展示。
  const external = ref(false);
  const root = ref('');
  const executable = ref('');
  const source = ref('');
  const arch = ref('');

  // 服务器文件位置的可配置状态（来自 /api/server）：
  // candidates 是可选的安装，locked 表示运行中不允许改。
  const server = ref(null);

  let timer = null;
  let wasInstalling = false;

  function apply(d) {
    installed.value = !!d.installed;
    installing.value = !!d.installing;
    progress.value = d.progress || '';
    // 后端把 Go 的 nil error 序列化成了 "<nil>"，过滤掉
    error.value = d.error === '<nil>' ? '' : (d.error || '');
    output.value = d.output || [];
    version.value = d.version || '';
    external.value = !!d.external;
    root.value = d.root || '';
    executable.value = d.executable || '';
    source.value = d.source || '';
    arch.value = d.arch || '';
  }

  async function refresh() {
    const d = await installApi.status();
    apply(d);

    if (installing.value && !timer) startPoll();
    // 一轮安装刚结束：给个结论，并停掉轮询
    if (!installing.value && wasInstalling) {
      toast(error.value ? '安装失败：' + error.value : '服务器文件已就绪', error.value ? 'err' : '');
    }
    wasInstalling = installing.value;
    return d;
  }

  function startPoll() {
    timer = setInterval(async () => {
      try {
        await refresh();
      } catch (_) {
        stopPoll();
      }
    }, 3000);
  }

  function stopPoll() {
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
  }

  async function startInstall() {
    await installApi.start();
    installing.value = true;
    wasInstalling = true;
    startPoll();
  }

  async function update() {
    await installApi.update();
    installing.value = true;
    wasInstalling = true;
    startPoll();
  }

  /* ---------- 服务器文件位置 ---------- */

  /** 拉取可配置状态（候选安装、是否运行中锁定、架构可选项等）。 */
  async function loadServer() {
    server.value = await serverApi.get();
    return server.value;
  }

  /**
   * 应用新的位置 / 架构设置。
   * serverDir 传空字符串表示「保持或恢复自动探测」。
   * 成功后同时刷新安装状态——它里面的路径与架构也变了。
   */
  async function saveServer(serverDir, archValue) {
    server.value = await serverApi.update(serverDir, archValue);
    await refresh();
    return server.value;
  }

  return {
    installed, installing, progress, error, output, version,
    external, root, executable, source, arch,
    server, loadServer, saveServer,
    refresh, startInstall, update,
  };
});
