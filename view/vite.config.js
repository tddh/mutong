import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

/* 主题引导：必须早于任何样式执行，否则暗色下刷新会闪一帧浅色。
   head-prepend 使本脚本排在 vite 注入的 stylesheet/modulepreload 之前；
   17 个入口共用这一份真源，dev 与 build 行为一致，不引额外依赖。 */
const THEME_BOOTSTRAP = `!function(){try{var d=document.documentElement;d.classList.add('theming');if(localStorage.getItem('mutong.theme')==='dark')d.setAttribute('data-theme','dark')}catch(e){}}()`

const themeBootstrap = {
  name: 'theme-bootstrap',
  transformIndexHtml() {
    return [{ tag: 'script', children: THEME_BOOTSTRAP, injectTo: 'head-prepend' }]
  },
}

// https://vitejs.dev/config/
export default defineConfig(({ command, mode }) => {
  // Allow OUT_DIR env var to override output directory
  const outDir = process.env.OUT_DIR ? resolve(__dirname, process.env.OUT_DIR) : resolve(__dirname, 'dist')
  
  return {
    root: resolve(__dirname, 'src'),
    plugins: [themeBootstrap, vue()],
    resolve: {
      alias: {
        '@': resolve(__dirname, 'src'),
        // 使用 Vue runtime + compiler 版本，支持字符串模板编译
        'vue': 'vue/dist/vue.esm-bundler.js',
      },
    },
    base: '/view/',
    build: {
      outDir: outDir,
      emptyOutDir: true,
      rollupOptions: {
        output: {
          manualChunks(id) {
            // G6 is only used by the retrospective pages; keep it out of the
            // shared entry chunk so other pages do not pay 1.4 MB.
            if (id.includes('@antv/g6') || id.includes('node_modules/@antv')) return 'g6'
            return undefined
          },
        },
        input: {
          index: resolve(__dirname, 'src/index.html'),
          topology: resolve(__dirname, 'src/topology/index.html'),
          alerts: resolve(__dirname, 'src/alerts/index.html'),
          inspection: resolve(__dirname, 'src/inspection/index.html'),
          'resource-table': resolve(__dirname, 'src/resource-table.html'),
          status: resolve(__dirname, 'src/status/index.html'),
          logs: resolve(__dirname, 'src/logs/index.html'),
          terminal: resolve(__dirname, 'src/terminal/index.html'),
          trace: resolve(__dirname, 'src/trace/index.html'),
          monitoring: resolve(__dirname, 'src/monitoring/index.html'),
          diagnosis: resolve(__dirname, 'src/diagnosis/index.html'),
          retrospective: resolve(__dirname, 'src/retrospective/index.html'),
          'retrospective-history': resolve(__dirname, 'src/retrospective-history/index.html'),
          'inspection-history': resolve(__dirname, 'src/inspection-history/index.html'),
          clusters: resolve(__dirname, 'src/clusters/index.html'),
          login: resolve(__dirname, 'src/login.html'),
          consent: resolve(__dirname, 'src/consent.html'),
        },
      },
    },
    server: {
      port: 3000,
      proxy: {
        '/api': 'http://localhost:8888',
        '/k8s': 'http://localhost:8888',
        '/metrics': 'http://localhost:8888',
      },
    },
  }
})
