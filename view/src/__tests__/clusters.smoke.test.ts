import { describe, it, expect, beforeEach, vi } from 'vitest'
import { nextTick } from 'vue'

/* 集群页是 Phase 1 第三个接入组件层的页面，也是自己手写 toast/遮罩/徽标/表格最重的一页。
   这里锁两件事：一是页面在接口全失败时的降级形态，二是"重试"按钮——
   原实现 setup 未导出 fetchClusters 却调 this.fetchClusters()，点击必抛 TypeError。 */
describe('clusters page', () => {
  let calls

  const loadPage = async (fetchImpl) => {
    vi.resetModules()
    document.body.innerHTML = '<div id="app"></div>'
    calls = []
    globalThis.fetch = async (url, opts) => {
      calls.push(String(url))
      return fetchImpl(url)
    }
    await import('../clusters/clusters.js')
    await nextTick()
    await nextTick()
    await nextTick()
  }

  const json = (obj) =>
    new Response(JSON.stringify(obj), { headers: { 'Content-Type': 'application/json' } })

  beforeEach(() => {
    localStorage.clear()
  })

  it('渲染四张统计卡与组件层表格，徽标走 data-sev', async () => {
    await loadPage(() =>
      json({
        clusters: [
          { name: 'tf001', region: 'cn-beijing', endpoint: 'http://10.0.0.1', status: 'healthy', enable: true },
          { name: 'k8s-m1', endpoint: 'http://10.0.0.2', status: 'unknown' },
        ],
      }),
    )
    const app = document.querySelector('#app')
    expect(app.querySelectorAll('.m-stat-card')).toHaveLength(4)
    expect(app.querySelectorAll('.m-tbl tbody tr')).toHaveLength(2)
    const badges = [...app.querySelectorAll('.m-badge')]
    expect(badges.map((b) => b.getAttribute('data-sev'))).toContain('pass')
    expect(badges.map((b) => b.getAttribute('data-sev'))).toContain('pending')
  })

  it('列表接口失败时出错误横幅，点重试会真的重新拉取而不是抛错', async () => {
    await loadPage((url) => {
      if (String(url).includes('/api/v1/clusters')) return Promise.reject(new Error('network offline'))
      return Promise.resolve(json({ clusters: [] }))
    })
    // 异步链有数层 await，用 waitFor 轮询而不是猜 nextTick 次数
    await vi.waitFor(() => {
      const banner = document.querySelector('.error-banner')
      expect(banner).toBeTruthy()
      expect(banner.textContent).toContain('获取集群列表失败')
    })

    const before = calls.length
    const retry = [...document.querySelectorAll('.error-banner button')].find((b) =>
      b.textContent.includes('重试'),
    )
    expect(retry).toBeTruthy()
    retry.click()
    await vi.waitFor(() => expect(calls.length).toBeGreaterThan(before))
  })

  it('集群为空时表格出空态提示而非空白', async () => {
    await loadPage(() => json({ clusters: [] }))
    expect(document.querySelector('.m-empty')?.textContent).toContain('暂无集群数据')
  })
})
