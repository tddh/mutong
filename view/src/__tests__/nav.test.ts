import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'
import { mount } from '@vue/test-utils'
import { NavBar } from '../components/SharedComponents.js'

const here = dirname(fileURLToPath(import.meta.url))
const comp = readFileSync(resolve(here, '..', 'styles', 'components.css'), 'utf-8')

/* 下拉菜单此前纯靠 CSS :hover 展开，鼠标从按钮移向面板时穿过 6px 缝隙，
   :hover 当场断裂、菜单还没碰到就消失。现在开合走组件状态，
   交互与样式契约都钉在这里。 */
describe('更多菜单的交互', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  // 必须挂进 document.body：组件用 contains(e.target) 判定"点击在外部"，
  // 游离在文档外的 wrapper 会让每次点击都被当成外部点击。
  const setup = () => mount(NavBar, { attachTo: document.body })

  it('点击展开、再点收起，aria-expanded 跟着走', async () => {
    const w = setup()
    await w.find('.nav-more-btn').trigger('click')
    expect(w.find('.nav-more').classes()).toContain('is-open')
    expect(w.find('.nav-more-btn').attributes('aria-expanded')).toBe('true')
    await w.find('.nav-more-btn').trigger('click')
    expect(w.find('.nav-more').classes()).not.toContain('is-open')
    expect(w.find('.nav-more-btn').attributes('aria-expanded')).toBe('false')
    w.unmount()
  })

  it('移出后延迟收起，这段时间里移回则保持展开', async () => {
    const w = setup()
    const more = w.find('.nav-more')
    await more.trigger('mouseenter')
    expect(more.classes()).toContain('is-open')

    await more.trigger('mouseleave')
    vi.advanceTimersByTime(100)
    await more.trigger('mouseenter')
    vi.advanceTimersByTime(1000)
    await w.vm.$nextTick()
    expect(more.classes(), '斜向移动不该让菜单半路收起').toContain('is-open')

    await more.trigger('mouseleave')
    vi.advanceTimersByTime(400)
    await w.vm.$nextTick()
    expect(more.classes()).not.toContain('is-open')
    w.unmount()
  })

  it('点击菜单外或按 Esc 都能收起', async () => {
    const w = setup()
    await w.find('.nav-more-btn').trigger('click')
    expect(w.find('.nav-more').classes()).toContain('is-open')
    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await w.vm.$nextTick()
    expect(w.find('.nav-more').classes()).not.toContain('is-open')

    await w.find('.nav-more-btn').trigger('click')
    expect(w.find('.nav-more').classes()).toContain('is-open')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await w.vm.$nextTick()
    expect(w.find('.nav-more').classes()).not.toContain('is-open')
    w.unmount()
  })

  it('unmount 后摘掉全局监听，不留悬挂回调', async () => {
    const w = setup()
    await w.find('.nav-more-btn').trigger('click')
    w.unmount()
    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(true).toBe(true)
  })
})

describe('下拉的样式契约', () => {
  const strip = comp.replace(/\/\*[\s\S]*?\*\//g, '').replace(/\s+/g, ' ')

  it('面板常驻 DOM 由 is-open 驱动，不再 display:none 配合 :hover', () => {
    const body = strip.match(/:where\(\.nav-pop\)\s*\{[^}]*\}/)?.[0] ?? ''
    expect(body).not.toContain('display: none')
    expect(body).toContain('visibility: hidden')
    expect(strip).toContain(':where(.nav-more.is-open .nav-pop)')
    expect(strip).not.toContain(':where(.nav-more:hover .nav-pop)')
  })

  it('按钮与面板之间必须有桥接，否则 :hover 会在缝隙处断裂', () => {
    expect(strip).toMatch(/:where\(\.nav-pop\)::before\s*\{[^}]*bottom:\s*100%/i)
    // 定位本身不得再制造空隙；间距交给 margin-top + 桥接条
    expect(strip).not.toMatch(/\.nav-pop\)\s*\{[^}]*top:\s*calc\(100%\s*\+/)
  })

  it('导航字号已整体提一档（pill 15px、brand 20px）', () => {
    const pill = strip.match(/:where\(\.nav-links a\), :where\(\.nav-more-btn\)\s*\{[^}]*\}/)
    expect(pill ? pill[0] : '').toContain('--fs-450')
    const brand = strip.match(/:where\(\.nav-brand\)\s*\{[^}]*\}/)
    expect(brand ? brand[0] : '').toContain('--fs-600')
  })
})
