import js from '@eslint/js';
import pluginVue from 'eslint-plugin-vue';

// 前端代码检查。
//
// Vite 构建只能发现语法与模块解析问题，发现不了「模板里引用了不存在的变量」
// 「import 了但没用」这类问题，所以单独跑 ESLint。
// 用法：npm run lint

export default [
  { ignores: ['dist/**', 'node_modules/**'] },

  js.configs.recommended,
  ...pluginVue.configs['flat/recommended'],

  {
    files: ['**/*.{js,mjs,vue}'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: {
        // 浏览器环境
        window: 'readonly',
        document: 'readonly',
        localStorage: 'readonly',
        location: 'readonly',
        history: 'readonly',
        fetch: 'readonly',
        URL: 'readonly',
        EventSource: 'readonly',
        AbortController: 'readonly',
        FormData: 'readonly',
        // 上传存档要用 XHR：fetch 报告不了上传进度
        XMLHttpRequest: 'readonly',
        // 日志自动滚动用 rAF 合并，避免高频日志把布局打满
        requestAnimationFrame: 'readonly',
        confirm: 'readonly',
        alert: 'readonly',
        console: 'readonly',
        setTimeout: 'readonly',
        clearTimeout: 'readonly',
        setInterval: 'readonly',
        clearInterval: 'readonly',
      },
    },
    rules: {
      // 允许用 _ 前缀显式标记「故意不用」的变量（含 catch 的绑定）
      'no-unused-vars': ['error', {
        argsIgnorePattern: '^_',
        varsIgnorePattern: '^_',
        caughtErrorsIgnorePattern: '^_',
      }],
      // catch 块留空是常见写法（例如「失败也不影响主流程」）
      'no-empty': ['error', { allowEmptyCatch: true }],

      // 组件名允许单词（UiCard / TabNav 这类已经很清晰）
      'vue/multi-word-component-names': 'off',

      // Icon.vue 用 v-html 渲染内联 SVG 路径。内容是文件内的常量映射表，
      // 不含任何外部输入，不存在 XSS 面，因此关掉这条规则。
      'vue/no-v-html': 'off',

      // 以下都是纯排版规则，交给人工排版，避免 lint 结果被噪声淹没
      'vue/max-attributes-per-line': 'off',
      'vue/first-attribute-linebreak': 'off',
      'vue/html-closing-bracket-newline': 'off',
      'vue/singleline-html-element-content-newline': 'off',
      'vue/html-self-closing': 'off',
      'vue/attributes-order': 'off',
    },
  },

  {
    // 构建期脚本跑在 Node 里，需要额外放开 Node 全局
    files: ['scripts/**/*.mjs', 'vite.config.js', 'eslint.config.js'],
    languageOptions: {
      globals: {
        process: 'readonly',
        Buffer: 'readonly',
        __dirname: 'readonly',
      },
    },
  },
];
