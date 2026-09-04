import { ref, computed } from 'vue'

/* 全站有 19 个独立的 createApp().mount（17 个入口 + topology + terminal），
   没有共享的状态容器，模块级单例 ref 是唯一能让它们看到同一份主题的方式。
   真源是 <html> 的 data-theme 属性——由 MutationObserver 反向驱动 ref，
   于是本页切换、跨标签页 storage 同步、引导脚本抢先设置，三者都能收敛到同一状态。 */

const KEY = 'mutong.theme'
const el = () => document.documentElement

function readAttr() {
  try {
    return el().getAttribute('data-theme') === 'dark' ? 'dark' : 'light'
  } catch {
    return 'light'
  }
}

const theme = ref(readAttr())

try {
  new MutationObserver(() => {
    const next = readAttr()
    if (theme.value !== next) theme.value = next
  }).observe(el(), { attributes: true, attributeFilter: ['data-theme'] })
} catch {
  /* 非浏览器环境（vitest 之外的构建期求值）忽略 */
}

function apply(next) {
  try {
    el().classList.add('theming')
    if (next === 'dark') el().setAttribute('data-theme', 'dark')
    else el().removeAttribute('data-theme')
    localStorage.setItem(KEY, next)
  } catch {
    /* 隐私模式下 localStorage 会抛，主题仍应可切换，只是不持久 */
  }
  theme.value = next
  window.dispatchEvent(new CustomEvent('mutong:theme', { detail: { theme: next } }))
}

/* 引导脚本给 <html> 挂了 theming 以关掉全站过渡；首帧画完即摘，
   双 rAF 确保样式已应用、过渡已抑制，之后的切换才有正常动效。 */
function releaseTheming() {
  requestAnimationFrame(() => requestAnimationFrame(() => el().classList.remove('theming')))
}
try {
  if (document.readyState === 'complete') releaseTheming()
  else window.addEventListener('load', releaseTheming, { once: true })
} catch {
  /* 非浏览器环境忽略 */
}

export function useTheme() {
  return {
    theme,
    isDark: computed(() => theme.value === 'dark'),
    setTheme: apply,
    toggleTheme: () => apply(theme.value === 'dark' ? 'light' : 'dark'),
  }
}
