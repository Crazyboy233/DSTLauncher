// 后端接口的唯一定义处。
//
// 所有端点都收敛在这里，视图组件不直接拼 URL——
// 以后接口调整只改这个文件，不用全项目搜索字符串。

const TIMEOUT_MS = 60000;

async function request(path, { method = 'GET', params, body } = {}) {
  const url = new URL(path, location.origin);
  if (params) {
    Object.entries(params).forEach(([k, v]) => {
      if (v !== undefined && v !== null && v !== '') url.searchParams.set(k, v);
    });
  }

  const init = { method };
  if (body !== undefined) {
    init.headers = { 'Content-Type': 'application/json' };
    init.body = JSON.stringify(body);
  }

  // 停止服务器要等 c_save + c_shutdown，可能几十秒，不能按默认超时掐断
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), TIMEOUT_MS);
  init.signal = controller.signal;

  let res;
  try {
    res = await fetch(url, init);
  } catch (err) {
    clearTimeout(timer);
    // 保留原始错误，方便在控制台里追根因
    if (err.name === 'AbortError') {
      throw new Error('请求超时，服务器可能仍在处理中', { cause: err });
    }
    throw new Error('无法连接面板服务，请确认 dst-windows.exe 正在运行', { cause: err });
  }
  clearTimeout(timer);

  let json;
  try {
    json = await res.json();
  } catch (err) {
    throw new Error(`接口返回异常（HTTP ${res.status}）`, { cause: err });
  }
  if (json.code !== 200) throw new Error(json.message || '请求失败');
  return json.data;
}

/* ---------- 房间 ---------- */

export const roomApi = {
  list: () => request('/api/rooms'),
  create: body => request('/api/rooms', { method: 'POST', body }),
  update: body => request('/api/rooms', { method: 'PUT', body }),
  remove: id => request('/api/rooms', { method: 'DELETE', params: { room: id } }),
  meta: () => request('/api/rooms/meta'),
};

/* ---------- 房间管理员（adminlist.txt） ---------- */

export const adminApi = {
  list: roomId => request('/api/admins', { params: { room: roomId } }),
  add: (roomId, kleiId) => request('/api/admins', { method: 'POST', params: { room: roomId }, body: { kleiId } }),
  remove: (roomId, id) => request('/api/admins', { method: 'DELETE', params: { room: roomId, id } }),
};

/* ---------- 进程控制 ---------- */

export const procApi = {
  status: roomId => request('/api/status', { params: { room: roomId } }),
  start: (roomId, shard) => request('/api/start', { params: { room: roomId, shard } }),
  stop: (roomId, shard, timeout) => request('/api/stop', {
    params: { room: roomId, shard, timeout },
  }),
  restart: (roomId, shard) => request('/api/restart', { params: { room: roomId, shard } }),
  cmd: (roomId, shard, cmd) => request('/api/cmd', { params: { room: roomId, shard, cmd } }),
};

/* ---------- 日志 ---------- */

export const logApi = {
  tail: (roomId, shard, lines) => request('/api/logs', { params: { room: roomId, shard, lines } }),
  /** SSE 只能用 EventSource，因此这里返回地址而不是发起请求。 */
  streamUrl(roomId, shard, lines = 200) {
    const url = new URL('/api/logs/stream', location.origin);
    if (roomId) url.searchParams.set('room', roomId);
    if (shard) url.searchParams.set('shard', shard);
    url.searchParams.set('lines', lines);
    return url.toString();
  },
};

/* ---------- 世界配置 ---------- */

export const worldApi = {
  get: (roomId, shard) => request('/api/worldconfig', { params: { room: roomId, shard } }),
  save: payload => request('/api/worldconfig', { method: 'PUT', body: payload }),
};

/* ---------- 服务器文件安装 ---------- */

export const installApi = {
  status: () => request('/api/install'),
  start: () => request('/api/install', { params: { action: 'start' } }),
  update: () => request('/api/install/update'),
};

/* ---------- 服务器文件位置（可切换安装目录与架构） ---------- */

export const serverApi = {
  get: () => request('/api/server'),
  // serverDir 传空字符串表示「保持/恢复自动探测」
  update: (serverDir, arch) => request('/api/server', {
    method: 'PUT',
    body: { serverDir, arch },
  }),};

/* ---------- 模组 ---------- */

export const modApi = {
  list: roomId => request('/api/mods', { params: { room: roomId } }),
  toggle: (roomId, id, enabled) => request('/api/mods/toggle', {
    params: { room: roomId, id, enabled },
  }),
  setConfig: (roomId, id, key, value) => request('/api/mods/config', {
    params: { room: roomId, id, key, value },
  }),
  schema: (roomId, id) => request('/api/mods/schema', { params: { room: roomId, id } }),
  remove: (roomId, id) => request('/api/mods/delete', { params: { room: roomId, id } }),
};

/* ---------- 模组预更新（下载创意工坊模组） ---------- */

export const modUpdateApi = {
  /** 查询进度：{ running, progress, error, output } */
  status: () => request('/api/mods/update'),
  /**
   * 触发一次预更新，立即返回，之后轮询 status。
   * id 传工坊模组 ID 时为单模组模式（已安装的会先删旧文件再重下），
   * 不传则全量补齐所有缺失模组。
   */
  start: (roomId, shard, id) => {
    const params = { room: roomId, shard };
    if (id) params.id = id;
    return request('/api/mods/update', { params });
  },
  stop: () => request('/api/mods/update', { method: 'DELETE' }),
};

/* ---------- 创意工坊 ---------- */

export const workshopApi = {
  /** 按 ID / 链接查详情（免 key）。input 支持纯数字、workshop-数字、链接、逗号分隔 */
  details: input => request('/api/workshop/details', { params: { input } }),
  /** 关键词搜索（需要 Steam Web API Key）。page 从 1 开始 */
  search: (q, page = 1) => request('/api/workshop/search', { params: { q, page } }),
  getKey: () => request('/api/workshop/key'),
  saveKey: steamApiKey => request('/api/workshop/key', { method: 'PUT', body: { steamApiKey } }),
};

/* ---------- 备份 ---------- */

export const backupApi = {
  list: roomId => request('/api/backups', { params: { room: roomId } }),
  create: roomId => request('/api/backups/create', { params: { room: roomId } }),
  remove: (roomId, name) => request('/api/backups/delete', { params: { room: roomId, name } }),
  restore: (roomId, name) => request('/api/backups/restore', { params: { room: roomId, name } }),
  /**
   * 导出备份到浏览器。走原生 <a download> 而不是 request()：
   * 响应体是 zip 文件流而非 JSON，且浏览器自带的下载管理比手动拼 blob 更省事。
   */
  download(roomId, name) {
    const url = new URL('/api/backups/download', location.origin);
    url.searchParams.set('room', roomId);
    url.searchParams.set('name', name);
    const a = document.createElement('a');
    a.href = url.toString();
    a.download = name;
    document.body.appendChild(a);
    a.click();
    a.remove();
  },
};

/* ---------- 存档上传 ---------- */

export const saveApi = {
  /**
   * 上传存档整包（Klei 下载或别的面板导出的 Cluster_* 压缩包）。
   *
   * 用 XHR 而不是 request()：存档动辄几百 MB，必须能报告上传进度，
   * 而 fetch 没有上传进度事件；XHR 的 upload.onprogress 是现成手段。
   * 另外上传 + 服务端解压拷贝可能耗时数分钟，也不能套用 request() 的 60s 超时。
   *
   * @param {File} file
   * @param {{mode: 'new'|'overwrite', roomId?: number}} options
   * @param {(percent: number) => void} [onProgress] 上传进度回调
   * @returns {Promise<{room: object, summary: object}>}
   */
  upload(file, { mode, roomId }, onProgress) {
    return new Promise((resolve, reject) => {
      const form = new FormData();
      form.append('file', file);
      form.append('mode', mode);
      if (roomId) form.append('room', String(roomId));

      const xhr = new XMLHttpRequest();
      xhr.open('POST', '/api/saves/upload');
      // 传输 + 解压 + 拷贝整条链路都可能很慢，给足 30 分钟
      xhr.timeout = 30 * 60 * 1000;

      xhr.upload.onprogress = e => {
        if (onProgress && e.lengthComputable) {
          onProgress(Math.round((e.loaded / e.total) * 100));
        }
      };
      xhr.onload = () => {
        let json;
        try {
          json = JSON.parse(xhr.responseText);
        } catch (_) {
          reject(new Error(`接口返回异常（HTTP ${xhr.status}）`));
          return;
        }
        if (json.code !== 200) {
          reject(new Error(json.message || '上传失败'));
          return;
        }
        resolve(json.data);
      };
      xhr.onerror = () => reject(new Error('无法连接面板服务，请确认 dst-windows.exe 正在运行'));
      xhr.ontimeout = () => reject(new Error('上传超时，请检查网络或改用更小的存档包'));
      xhr.send(form);
    });
  },
};

/* ---------- 配置预览 ---------- */

export const configApi = {
  preview: roomId => request('/api/config', { params: { room: roomId } }),
};

export { request };
