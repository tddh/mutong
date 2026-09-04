import { h, onMounted } from 'vue'
import { useAuth } from '../stores/auth.js'
import { useTheme } from '../composables/useTheme.js'

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
                <!-- [暂时隐藏-勿删] 日志菜单入口已收起，页面代码完整保留于 src/logs/；恢复：删除本行与末行注释标记，保留中间的链接
                <a href="/view/logs/index.html" :class="{ active: isActive('/view/logs/index.html') }">📋 日志</a>
                [暂时隐藏-勿删] -->
                <!-- [暂时隐藏-勿删] 监控菜单入口已收起（该页将重建为 Grafana 内嵌），页面代码完整保留于 src/monitoring/；恢复：删除本行与末行注释标记，保留中间的链接
                <a href="/view/monitoring/index.html" :class="{ active: isActive('/view/monitoring/index.html') }">📊 监控</a>
                [暂时隐藏-勿删] -->
                <a href="/view/terminal/index.html" :class="{ active: isActive('/view/terminal/index.html') }">💻 终端</a>
                <!-- [暂时隐藏-勿删] 链路追踪菜单入口已收起，页面代码完整保留于 src/trace/；恢复：删除本行与末行注释标记，保留中间的链接
                <a href="/view/trace/index.html" :class="{ active: isActive('/view/trace/index.html') }">🔗 追踪</a>
                [暂时隐藏-勿删] -->
                <a href="/view/clusters/index.html" :class="{ active: isActive('/view/clusters/index.html') }">🖥️ 集群</a>
            </div>
            <div class="nav-actions">
                <button class="theme-toggle" type="button" @click="toggleTheme"
                        :title="isDark ? '切换到浅色' : '切换到暗色'"
                        :aria-label="isDark ? '切换到浅色' : '切换到暗色'">
                    <svg v-if="isDark" viewBox="0 0 24 24" width="18" height="18" aria-hidden="true"
                         fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/>
                    </svg>
                    <svg v-else viewBox="0 0 24 24" width="18" height="18" aria-hidden="true"
                         fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                        <circle cx="12" cy="12" r="4"/>
                        <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>
                    </svg>
                </button>
                <a v-if="auth.isAuthenticated" href="#" @click.prevent="logout">{{ auth.user?.username || '登出' }}</a>
                <a v-else href="/view/login.html">登录</a>
            </div>
        </nav>
    `,
  setup() {
    const auth = useAuth()
    const { isDark, toggleTheme } = useTheme()
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

    return { home, isActive, auth, logout, isDark, toggleTheme }
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
