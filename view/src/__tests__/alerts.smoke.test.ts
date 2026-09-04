import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { h, nextTick } from 'vue'

/* 告警页是全站最大的单文件（2797 行），本轮把它的色表与徽标实现换成了语义档位。
   占位测试自造一个 template 组件，永远测不到这些代码，故整体替换掉。 */
describe('severityBadge / criticalityBadge / riskBadge', () => {
  let mod

  beforeEach(async () => {
    vi.resetModules()
    document.body.innerHTML = '<div id="app"></div>'
    globalThis.fetch = async () => new Response('{}', { headers: { 'Content-Type': 'application/json' } })
    mod = await import('../alerts/alerts.js')
  })

  const sevOf = (vnode) => vnode.props.sev
  const textOf = (vnode) => vnode.props.text

  it('已知严重度映射到同名语义档', () => {
    expect(sevOf(mod.severityBadge('critical'))).toBe('critical')
    expect(sevOf(mod.severityBadge('warning'))).toBe('warning')
    expect(textOf(mod.severityBadge('error'))).toBe('error')
  })

  it('未知或缺失的严重度降级 pending，不产出空样式徽标', () => {
    expect(sevOf(mod.severityBadge('某种没见过的'))).toBe('pending')
    expect(sevOf(mod.severityBadge(''))).toBe('pending')
    expect(textOf(mod.severityBadge(undefined))).toBe('—')
  })

  it('业务重要等级用中文标签且映射到不同档', () => {
    expect(textOf(mod.criticalityBadge('critical'))).toBe('核心')
    expect(sevOf(mod.criticalityBadge('high'))).toBe('warning')
    expect(sevOf(mod.criticalityBadge('medium'))).toBe('pass')
    expect(sevOf(mod.criticalityBadge('low'))).toBe('pending')
  })

  it('风险等级中文标签齐备，缺失值不出空徽标', () => {
    expect(textOf(mod.riskBadge('critical'))).toBe('严重')
    expect(sevOf(mod.riskBadge('medium'))).toBe('pending')
    expect(textOf(mod.riskBadge(undefined))).toBe('—')
  })

  it('渲染出的徽标只带 data-sev，不带内联色', () => {
    const w = mount({
      render: () => h('div', null, [mod.severityBadge('critical'), mod.criticalityBadge('high')]),
    })
    const badges = w.findAll('.m-badge')
    expect(badges).toHaveLength(2)
    for (const b of badges) {
      expect(b.attributes('data-sev')).toBeTruthy()
      expect(b.attributes('style')).toBeFalsy()
    }
  })
})

describe('alerts page 挂载', () => {
  it('接口返回空数据时页面仍能挂载、渲染导航与业务区块', async () => {
    vi.resetModules()
    document.body.innerHTML = '<div id="app"></div>'
    globalThis.fetch = async () =>
      new Response(JSON.stringify({ alerts: [], data: [], items: [] }), {
        headers: { 'Content-Type': 'application/json' },
      })
    await import('../alerts/alerts.js')
    await vi.waitFor(() => expect(document.querySelector('.navbar')).toBeTruthy(), { timeout: 3000 })
    const app = document.querySelector('#app')
    // 空数据下告警页仍渲染出业务容器（列表区在其内），而非白屏
    expect(app.querySelector('.alerts-page')).toBeTruthy()
  })
})
