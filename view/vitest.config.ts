import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      // 与 vite.config.js:17 一致。缺了它，测试里的 vue 是 runtime-only：
      // mount({template}) 只 warn 不抛错、渲染空节点，断言照样"通过"。
      vue: 'vue/dist/vue.esm-bundler.js',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.ts'],
  },
})
