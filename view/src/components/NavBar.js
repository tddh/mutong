import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useAuth } from '../stores/auth.js'
import { useTheme } from '../composables/useTheme.js'
import { Icon } from './icons.js'
import { NAV_MORE, NAV_PRIMARY } from './nav.config.js'

/* 由 nav.config.js 驱动，链接不再硬编码在模板里。
   12 项平铺在 1280px 会溢出，故 8 项直显、4 项收进"更多"。
   下拉开合走 JS 状态而非纯 :hover：面板与按钮之间必然留有视觉间距，
   纯 CSS 下鼠标穿过那段间距时 :hover 当场断裂，菜单还没碰到就消失；
   触屏没有 hover，点击也必须能用。 */
export const NavBar = {
  name: 'NavBar',
  components: { Icon },
  template: `
    <nav class="navbar">
      <div class="nav-inner">
        <div class="nav-brand" @click="home">牧童 · 重明</div>
        <div class="nav-links">
          <a v-for="it in primary" :key="it.href" :href="it.href" :class="{ active: isActive(it.href) }">
            <Icon :name="it.icon" :size="16" /><span>{{ it.label }}</span>
          </a>
          <div
            v-if="more.length"
            ref="moreEl"
            class="nav-more"
            :class="{ 'has-active': moreActive, 'is-open': moreOpen }"
            @mouseenter="openMore"
            @mouseleave="closeSoon"
          >
            <button
              class="nav-more-btn"
              type="button"
              aria-haspopup="true"
              :aria-expanded="moreOpen"
              @click="toggleMore"
            >
              <span>更多</span><Icon name="chevron" :size="14" />
            </button>
            <div class="nav-pop">
              <a v-for="it in more" :key="it.href" :href="it.href" :class="{ active: isActive(it.href) }">
                <Icon :name="it.icon" :size="16" /><span>{{ it.label }}</span>
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

    const moreEl = ref(null)
    const moreOpen = ref(false)
    let closeTimer = 0
    const openMore = () => {
      clearTimeout(closeTimer)
      moreOpen.value = true
    }
    // 斜向移动时短暂越过边缘不该立刻收起，留 180ms 余量
    const closeSoon = () => {
      clearTimeout(closeTimer)
      closeTimer = setTimeout(() => {
        moreOpen.value = false
      }, 180)
    }
    const toggleMore = () => {
      clearTimeout(closeTimer)
      moreOpen.value = !moreOpen.value
    }
    const onDocClick = (e) => {
      if (moreEl.value && !moreEl.value.contains(e.target)) {
        clearTimeout(closeTimer)
        moreOpen.value = false
      }
    }
    const onKey = (e) => {
      if (e.key !== 'Escape') return
      clearTimeout(closeTimer)
      moreOpen.value = false
    }

    onMounted(async () => {
      document.addEventListener('click', onDocClick)
      window.addEventListener('keydown', onKey)
      await auth.init()
    })
    onUnmounted(() => {
      clearTimeout(closeTimer)
      document.removeEventListener('click', onDocClick)
      window.removeEventListener('keydown', onKey)
    })

    return {
      home,
      isActive,
      auth,
      logout,
      isDark,
      toggleTheme,
      primary,
      more,
      moreActive,
      moreEl,
      moreOpen,
      openMore,
      closeSoon,
      toggleMore,
    }
  },
}
