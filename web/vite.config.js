import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 前端工程根目录即 web/，构建产物输出到 dist/。
// 产物由 Server 从磁盘目录托管（默认 /etc/monitor-server/web，见 internal/server/api/spa.go 与 server.yaml 的 webDir），不内嵌进二进制。
export default defineConfig({
  root: '.',
  plugins: [vue()],
  base: '/',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsDir: 'assets',
  },
  server: {
    port: 5173,
    proxy: {
      // 开发态将 API 与 WebSocket 代理到本地 Server（默认 8080）
      '/api': 'http://localhost:8080',
      '/ws': { target: 'ws://localhost:8080', ws: true, changeOrigin: true },
    },
  },
  // 单元测试配置（Vitest 与构建共用同一份配置）。
  // Vitest 2.x 与 Vite 5 兼容；Vitest 3+ 要求 Vite 6+，升级前请同步升 Vite。
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.js'],
    // element-plus 必须 inline（交给 Vite 转换），不能走 Node 原生 ESM 外置加载：
    // EP 的 ESM 里 `import AsyncValidator from 'async-validator'` 在 Node 原生 CJS 互操作下
    // 拿到的是 `{ default: Schema }` 而不是构造函数，`new AsyncValidator(...)` 同步抛错，
    // 而该错误被 EP 的 .catch 吞掉 → 表单 validate() 恒 resolve(true)。
    // 症状：所有 `:rules` 校验在测试里静默失效（界面上看着能拦，测试里拦不住）。
    server: { deps: { inline: ['element-plus'] } },
  },
})
