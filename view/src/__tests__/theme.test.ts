import { describe, it, expect, beforeEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'
import { mount } from '@vue/test-utils'
import { useTheme } from '../composables/useTheme.js'
import { NavBar } from '../components/SharedComponents.js'

const here = dirname(fileURLToPath(import.meta.url))
const read = (p) => readFileSync(resolve(here, '..', 'styles', p), 'utf-8')

/** 提取形如 `--name: value;` 的自定义属性声明（跳过注释行） */
function decls(css) {
  const out = new Map()
  for (const line of css.split('\n')) {
    if (line.trimStart().startsWith('*') || line.trimStart().startsWith('/*')) continue
    const m = line.match(/^\s*(--[a-z0-9-]+)\s*:\s*(.+?);\s*$/)
    if (m) out.set(m[1], m[2].trim())
  }
  return out
}

const light = decls(read('theme.light.css'))
const dark = decls(read('theme.dark.css'))

describe('主题语义层', () => {
  it('暗色里的每个变量名在浅色里都存在（防拼写错误导致暗色静默失效）', () => {
    const orphan = [...dark.keys()].filter((k) => !light.has(k))
    expect(orphan).toEqual([])
  })

  it('暗色确实覆盖了背景、文本、边框、状态、严重度五类语义色', () => {
    const required = [
      '--surface-page',
      '--surface-card',
      '--text-primary',
      '--text-secondary',
      '--border-default',
      '--st-danger-fg',
      '--st-success-fg',
      '--sev-critical-bg',
    ]
    for (const k of required) {
      expect(dark.has(k), `暗色缺少 ${k}`).toBe(true)
      expect(dark.get(k), `暗色 ${k} 与浅色同值`).not.toBe(light.get(k))
    }
  })

  it('图表与图形结构色两主题都是 6 位 hex（retrospective.js:619 会拼 color + "10"）', () => {
    const keys = [
      ...Array.from({ length: 12 }, (_, i) => `--chart-${i + 1}`),
      '--graph-canvas-bg',
      '--graph-node-fill',
      '--graph-node-stroke',
      '--graph-label',
      '--graph-edge',
    ]
    for (const layer of [['light', light], ['dark', dark]]) {
      for (const k of keys) {
        const v = layer[1].get(k)
        expect(v, `${layer[0]} 缺少 ${k}`).toBeTruthy()
        expect(/^#[0-9a-f]{6}$/.test(v), `${layer[0]} ${k}=${v} 不是 6 位 hex`).toBe(true)
      }
    }
  })

  it('K8s 品牌色不随主题变化', () => {
    const tokens = decls(read('tokens.css'))
    expect(tokens.get('--pal-k8s-blue')).toBe('#326ce5')
    expect(dark.has('--pal-k8s-blue')).toBe(false)
  })
})

describe('useTheme', () => {
  beforeEach(() => {
    document.documentElement.removeAttribute('data-theme')
    localStorage.clear()
  })

  it('切换写入 data-theme、localStorage，并广播 mutong:theme', () => {
    let seen = null
    window.addEventListener('mutong:theme', (e) => (seen = e.detail.theme), { once: true })

    const { toggleTheme, theme, isDark } = useTheme()
    expect(theme.value).toBe('light')

    toggleTheme()
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    expect(localStorage.getItem('mutong.theme')).toBe('dark')
    expect(theme.value).toBe('dark')
    expect(isDark.value).toBe(true)
    expect(seen).toBe('dark')

    toggleTheme()
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
    expect(localStorage.getItem('mutong.theme')).toBe('light')
    expect(theme.value).toBe('light')
  })

  it('多处调用返回同一份状态（全站 19 个独立挂载点必须共享）', () => {
    const a = useTheme()
    const b = useTheme()
    a.toggleTheme()
    expect(b.theme.value).toBe('dark')
    expect(a.theme).toBe(b.theme)
  })
})

describe('NavBar 主题按钮', () => {
  beforeEach(() => {
    document.documentElement.removeAttribute('data-theme')
    localStorage.clear()
  })

  it('渲染出切换按钮，点击后翻到暗色', async () => {
    const wrapper = mount(NavBar)
    const btn = wrapper.find('.theme-toggle')
    expect(btn.exists()).toBe(true)

    await btn.trigger('click')
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    await btn.trigger('click')
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
    wrapper.unmount()
  })

  it('隐藏项不产生可点击锚点，其余 12 个入口在位', () => {
    // 注意层次：开发模式下 Vue 保留注释节点，被收起的 href 仍会以注释文本
    // 出现在 html() 里（生产构建才丢弃）。用户能否点到，取决于是否渲染成 a 元素，
    // 因此断言锚点集合，而不是断言输出字符串。
    const wrapper = mount(NavBar)
    const hrefs = wrapper.findAll('.nav-links a').map((n) => n.attributes('href'))
    for (const hidden of ['/view/logs/', '/view/monitoring/', '/view/trace/']) {
      expect(hrefs.some((h) => h && h.includes(hidden)), `隐藏项仍可点击：${hidden}`).toBe(false)
    }
    for (const kept of [
      '/view/status/',
      '/view/terminal/',
      '/view/clusters/',
      '/view/topology/',
      '/view/alerts/',
      '/view/diagnosis/',
    ]) {
      expect(hrefs.some((h) => h && h.includes(kept)), `保留项丢失：${kept}`).toBe(true)
    }
    expect(hrefs).toHaveLength(12)
    wrapper.unmount()
  })
})
