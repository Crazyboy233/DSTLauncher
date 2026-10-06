import { fileURLToPath, URL } from 'node:url';

import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

export default defineConfig({
  plugins: [vue()],

  resolve: {
    // 全项目统一用 @/ 绝对导入。
    // 相对路径很容易在挪动文件层级时写错（本项目就写错过三次），
    // @ 开头就不存在「要退几层」的问题。
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },

  // 相对路径：产物放在 web/dist，由 Go 面板挂在根路径下提供，
  // 用相对路径可避免以后改挂载点（或反向代理到子路径）时资源 404。
  base: './',

  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },

  server: {
    port: 5173,
    // 开发时前端跑在 Vite，接口转发给本机面板进程。
    // 两个进程要同时开着：先 `./dst-windows.exe`，再 `npm run dev`。
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8899',
        changeOrigin: true,
        // SSE 长连接不能被代理缓冲
        ws: false,
      },
    },
  },
});
