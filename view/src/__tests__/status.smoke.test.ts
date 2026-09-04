import { describe, it, expect, beforeAll } from 'vitest'
import { nextTick } from 'vue'

/* 状态页是 Phase 1 首个接入组件层的页面。这里走的是它真实的挂载路径：
   status.js 直接 createApp().mount('#app')，所以先备好 #app 再 import 模块，
   让页面在"后端全部不可达"这一最坏输入下渲染——各 API 方法自带 catch 返回 null，
   因此页面应当出五张面板加错误横幅，而不是白屏或抛错。 */
describe('status page', () => {
  beforeAll(async () => {
    document.body.innerHTML = '<div id="app"></div>'
    globalThis.fetch = async () => {
      throw new Error('network offline')
    }
    await import('../status/status.js')
    await nextTick()
    await nextTick()
  })

  it('挂载出导航与页头', () => {
    const app = document.querySelector('#app')
    expect(app.querySelector('.navbar')).toBeTruthy()
    expect(app.querySelector('.m-page-title')?.textContent).toBe('系统状态')
  })

  it('五个面板都在，且用的是组件层的 Panel 而非页面自拼卡片', () => {
    const panels = document.querySelectorAll('#app .m-panel')
    expect(panels).toHaveLength(5)
    const titles = [...panels].map((p) => p.querySelector('.m-panel-title')?.textContent)
    expect(titles).toEqual([
      'LLM 诊断服务',
      '告警抑制',
      '告警处理指标',
      '诊断统计',
      '执行器状态',
    ])
  })

  it('接口全失败时徽标降级为未配置/未知，而非留空', () => {
    const badges = [...document.querySelectorAll('#app .m-badge')].map((b) => ({
      text: b.textContent.trim(),
      sev: b.getAttribute('data-sev'),
    }))
    expect(badges).toEqual(
      expect.arrayContaining([
        { text: '未配置', sev: 'pending' },
        { text: '未知', sev: 'pending' },
        { text: '未启用', sev: 'pending' },
      ]),
    )
  })

  it('列出获取失败的服务', () => {
    const banner = document.querySelector('#app .error-banner')
    expect(banner?.textContent).toContain('诊断服务')
    expect(banner?.textContent).toContain('系统状态')
  })

  it('缺失的字段显示占位破折号而不是 undefined', () => {
    const values = [...document.querySelectorAll('#app .m-kv-v')].map((v) => v.textContent.trim())
    expect(values.length).toBeGreaterThan(0)
    expect(values.every((v) => v !== 'undefined' && v !== 'null')).toBe(true)
    expect(values).toContain('—')
  })
})
