// 源码结构自检。
//
// 这里查两类 Vite 构建**不会报错**、但运行时会出问题的事情：
//
//   1. @/ 导入指向不存在的文件
//      ESLint 不做模块解析，写错层级只有 vite build 才会炸（本项目犯过三次）。
//
//   2. 视图组件出现多个根节点
//      App.vue 用 <Transition> 包裹视图，而 Transition 只接受「恰好一个」根元素。
//      多根（fragment）不会报错，但会让切换动画失效——表现为来回点导航后右侧内容空白。
//
// 用法：npm run lint（先跑本脚本，再跑 eslint）

import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const src = path.join(root, 'src');

const files = [];
(function walk(dir) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) walk(p);
    else if (/\.(vue|js)$/.test(e.name)) files.push(p);
  }
})(src);

const rel = p => path.relative(root, p).replace(/\\/g, '/');
const problems = [];

/* ---------- 1. @/ 导入是否可解析 ---------- */

/** 支持省略扩展名的解析：@/x/y 可能是 y.js / y.vue / y/index.js */
function resolvable(spec) {
  const base = path.join(src, spec.replace(/^@\//, ''));
  const candidates = [
    base,
    base + '.js',
    base + '.vue',
    base + '.json',
    path.join(base, 'index.js'),
    path.join(base, 'index.vue'),
  ];
  return candidates.some(p => {
    try {
      return fs.statSync(p).isFile();
    } catch (_) {
      return false;
    }
  });
}

for (const file of files) {
  const text = fs.readFileSync(file, 'utf8');
  for (const m of text.matchAll(/(?:\bfrom\s+|import\s+)'(@\/[^']*)'/g)) {
    if (!resolvable(m[1])) {
      problems.push(`${rel(file)}  ->  @/ 导入找不到文件：${m[1]}`);
    }
  }
}

/* ---------- 2. 视图组件是否单一根节点 ---------- */

const VOID_TAGS = new Set([
  'area', 'base', 'br', 'col', 'embed', 'hr', 'img',
  'input', 'link', 'meta', 'param', 'source', 'track', 'wbr',
]);

/** 找出模板里的顶层元素名 */
function topLevelTags(template) {
  const cleaned = template.replace(/<!--[\s\S]*?-->/g, '');

  // 属性部分显式允许引号包裹的任意内容，这样 attr 里的 ">" / ">=" 不会把标签截断
  const TAG = /<(\/?)([A-Za-z][\w.-]*)((?:[^>"']|"[^"]*"|'[^']*')*)>/g;

  const tags = [];
  let depth = 0;

  for (const m of cleaned.matchAll(TAG)) {
    const closing = m[1] === '/';
    const name = m[2];
    const attrs = m[3] || '';
    // 自闭合标签不会改变层级。注意 `/` 可能被属性正则吞掉，要单独判断结尾
    const selfClosing = /\/\s*$/.test(attrs) || VOID_TAGS.has(name.toLowerCase());

    if (closing) {
      depth = Math.max(0, depth - 1);
      continue;
    }
    if (depth === 0) tags.push(name);
    if (!selfClosing) depth++;
  }
  return tags;
}

for (const file of files) {
  if (!file.endsWith('.vue') || !/[\\/]views[\\/]/.test(file)) continue;

  const text = fs.readFileSync(file, 'utf8');
  const m = /<template>([\s\S]*)<\/template>/.exec(text);
  if (!m) {
    problems.push(`${rel(file)}  ->  没有 <template> 块`);
    continue;
  }
  const roots = topLevelTags(m[1]);
  if (roots.length !== 1) {
    problems.push(
      `${rel(file)}  ->  视图必须有且只有一个根节点，实际 ${roots.length} 个：${roots.join(', ')}`,
    );
  }
}

if (problems.length) {
  console.error('源码自检未通过：');
  problems.forEach(p => console.error('  [x] ' + p));
  process.exit(1);
}
console.log(`源码自检通过（${files.length} 个文件；@/ 导入可解析、视图均为单一根节点）`);
