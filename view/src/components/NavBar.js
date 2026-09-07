import { computed, onMounted, ref } from 'vue'
import { useAuth } from '../stores/auth.js'
import { useTheme } from '../composables/useTheme.js'
import { Icon } from './icons.js'
import { NAV_MORE, NAV_PRIMARY } from './nav.config.js'

/* 由 nav.config.js 驱动，链接不再硬编码在模板里。
   12 项平铺在 1280px 会溢出，故 8 项直显、4 项收进"更多"。
   下拉用 CSS :hover + :focus-within 展开而非 JS 状态：键盘 Tab 也能打开，
   且不必为关闭时机写监听。 */
export const NavBar = {
  name: 'NavBar',
  components: { Icon },
  template: `
    <nav class="navbar">
      <div class="nav-inner">
        <div class="nav-brand" @click="home">牧童 · 重明</div>
        <div class="nav-links">
          <a v-for="it in primary" :key="it.href" :href="it.href" :class="{ active: isActive(it.href) }">
            <Icon :name="it.icon" :size="15" /><span>{{ it.label }}</span>
          </a>
          <div v-if="more.length" class="nav-more" :class="{ 'has-active': moreActive }">
            <button class="nav-more-btn" type="button" aria-haspopup="true">
              <span>更多</span><Icon name="chevron" :size="13" />
            </button>
            <div class="nav-pop">
              <a v-for="it in more" :key="it.href" :href="it.href" :class="{ active: isActive(it.href) }">
                <Icon :name="it.icon" :size="15" /><span>{{ it.label }}</span>
              </a>
            </div>
          </div>
        </div>
        <div class="nav-actions">
          <button class="theme-toggle" type="button" @click="toggleTheme"
                  :title="isDark ? '切换到浅色' : '切换到暗色'"
                  :aria-label="isDark ? '切换到浅色' : '切换到暗色'">
            <Icon :name="isDark ? 'moon' : 'sun'" :size="18" :stroke="1.8" />
          </button>
          <a v-if="auth.isAuthenticated" href="#" @click.prevent="logout">{{ auth.user?.username || '登出' }}</a>
          <a v-else href="/view/login.html">登录</a>
        </div>
      </div>
    </nav>
  `,
  setup() {
    const auth = useAuth()
    const { isDark, toggleTheme } = useTheme()
    const currentPath = ref(typeof window === 'undefined' ? '' : window.location.pathname)
    const primary = NAV_PRIMARY
    const more = NAV_MORE
    const moreActive = computed(() => more.some((it) => it.href === currentPath.value))
    const home = () => {
      window.location.href = '/view/index.html'
    }
    const isActive = (href) => currentPath.value === href
    const logout = () => {
      auth.logout()
    }

    onMounted(async () => {
      await auth.init()
    })

    return { home, isActive, auth, logout, isDark, toggleTheme, primary, more, moreActive }
  },
}
