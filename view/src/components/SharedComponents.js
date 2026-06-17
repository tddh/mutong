import { h, onMounted } from 'vue'
import { useAuth } from '../stores/auth.js'

export const NavBar = {
  template: `
        <nav class="navbar">
            <div class="nav-brand" @click="home">牧童 · 重明</div>
            <div class="nav-links">
                <a href="/view/index.html" :class="{ active: isActive('/view/index.html') }">仪表盘</a>
                <a href="/view/topology/index.html" :class="{ active: isActive('/view/topology/index.html') }">拓扑</a>
                <a href="/view/alerts/index.html" :class="{ active: isActive('/view/alerts/index.html') }">告警</a>
                <a href="/view/diagnosis/index.html" :class="{ active: isActive('/view/diagnosis/index.html') }">🤖 AI 诊断</a>
<a href="/view/inspection/index.html" :class="{ active: isActive('/view/inspection/index.html') }">巡检</a>
<a href="/view/inspection-history/index.html" :class="{ active: isActive('/view/inspection-history/index.html') }">巡检历史</a>
<a href="/view/retrospective/index.html" :class="{ active: isActive('/view/retrospective/index.html') }">复盘分析</a>
<a href="/view/retrospective-history/index.html" :class="{ active: isActive('/view/retrospective-history/index.html') }">历史复盘</a>
<a href="/view/resource-table.html" :class="{ active: isActive('/view/resource-table.html') }">资源表格</a>
                <a href="/view/status/index.html" :class="{ active: isActive('/view/status/index.html') }">状态</a>
                <a href="/view/logs/index.html" :class="{ active: isActive('/view/logs/index.html') }">📋 日志</a>
                <a href="/view/monitoring/index.html" :class="{ active: isActive('/view/monitoring/index.html') }">📊 监控</a>
                <a href="/view/terminal/index.html" :class="{ active: isActive('/view/terminal/index.html') }">💻 终端</a>
                <a href="/view/trace/index.html" :class="{ active: isActive('/view/trace/index.html') }">🔗 追踪</a>
                <a href="/view/clusters/index.html" :class="{ active: isActive('/view/clusters/index.html') }">🖥️ 集群</a>
            </div>
            <div class="nav-actions">
                <a v-if="auth.isAuthenticated" href="#" @click.prevent="logout">{{ auth.user?.username || '登出' }}</a>
                <a v-else href="/view/login.html">登录</a>
            </div>
        </nav>
    `,
  setup() {
    const auth = useAuth()
    const currentPath = window.location.pathname
    const home = () => {
      window.location.href = '/view/index.html'
    }
    const isActive = (href) => currentPath === href
    const logout = () => {
      auth.logout()
    }

    onMounted(async () => {
      await auth.init()
    })

    return { home, isActive, auth, logout }
  },
}

export const StatCard = {
  props: ['title', 'value', 'unit', 'color', 'icon'],
  template: `
        <div class="stat-card">
            <div class="stat-content">
                <span class="stat-title">{{ title }}</span>
                <span class="stat-value" :style="{ color: color }">{{ value }}<small class="unit">{{ unit }}</small></span>
            </div>
            <div class="stat-icon" v-if="icon">{{ icon }}</div>
        </div>
    `,
}
