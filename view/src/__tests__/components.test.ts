import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { h, nextTick } from 'vue'
import {
  PageHeader,
  Panel,
  StatusBadge,
  MBtn,
  EmptyState,
  Skeleton,
  StatCard,
  Icon,
} from '../components/primitives.js'
import { DataTable } from '../components/DataTable.js'
import { ICON_NAMES } from '../components/icons.js'

describe('Icon', () => {
  it('已知图标渲染出 path，未知图标渲染空 svg 而不抛错', () => {
    expect(ICON_NAMES.length).toBeGreaterThan(20)
    const ok = mount(Icon, { props: { name: 'alert' } })
    expect(ok.find('svg').exists()).toBe(true)
    expect(ok.find('svg').attributes('stroke')).toBe('currentColor')
    const bad = mount(Icon, { props: { name: '不存在的图标' } })
    expect(bad.find('svg').exists()).toBe(true)
    expect(bad.find('svg').element.children.length).toBe(0)
  })
})

describe('StatusBadge', () => {
  it('未知严重度降级为 default，不抛错也不留空样式', () => {
    const w = mount(StatusBadge, { props: { sev: '某种没见过的值', text: 'x' } })
    expect(w.attributes('data-sev')).toBe('default')
  })
  it('颜色靠 data-sev 选择，自身不写内联色', () => {
    const w = mount(StatusBadge, { props: { sev: 'critical', text: 'P0' } })
    expect(w.attributes('style')).toBeFalsy()
    expect(w.attributes('data-sev')).toBe('critical')
  })
})

describe('Panel', () => {
  it('loading 时渲染骨架而非空白', () => {
    const w = mount(Panel, { props: { title: 'T', loading: true }, slots: { default: () => h('b', 'x') } })
    expect(w.find('.m-panel-title').text()).toBe('T')
    expect(w.find('.m-skeleton').exists()).toBe(true)
    expect(w.text()).not.toContain('x')
  })
  it('flush 去掉正文内边距，actions 插槽进头部', () => {
    const w = mount(Panel, {
      props: { title: 'T', flush: true },
      slots: { actions: () => h('button', 'a'), default: () => h('p', 'body') },
    })
    expect(w.find('.m-panel').classes()).toContain('is-flush')
    expect(w.find('.m-panel-actions').text()).toBe('a')
  })
})

describe('MBtn', () => {
  it('disabled 与 loading 都不触发 click，loading 显示 spinner', async () => {
    const a = mount(MBtn, { props: { disabled: true }, slots: { default: 'go' } })
    await a.trigger('click')
    expect(a.emitted('click')).toBeFalsy()
    const b = mount(MBtn, { props: { loading: true } })
    expect(b.find('.m-btn-spinner').exists()).toBe(true)
    await b.trigger('click')
    expect(b.emitted('click')).toBeFalsy()
    const c = mount(MBtn)
    await c.trigger('click')
    expect(c.emitted('click')).toHaveLength(1)
  })
})

describe('StatCard', () => {
  it('tone 落成 data-tone 属性，不再由调用方传色号', () => {
    const w = mount(StatCard, { props: { title: '告警', value: 3, tone: 'danger', icon: 'alert' } })
    expect(w.attributes('data-tone')).toBe('danger')
    expect(w.attributes('style')).toBeFalsy()
    expect(w.find('.m-stat-card-icon svg').exists()).toBe(true)
  })
})

describe('EmptyState / Skeleton / PageHeader', () => {
  it('EmptyState 的 desc 为空时不渲染多余段落', () => {
    const w = mount(EmptyState, { props: { title: '暂无数据' } })
    expect(w.find('.m-empty-desc').exists()).toBe(false)
  })
  it('Skeleton 按 rows 出条数且逐条收窄', () => {
    const w = mount(Skeleton, { props: { rows: 3 } })
    const bars = w.findAll('.m-skeleton-bar')
    expect(bars).toHaveLength(3)
    expect(bars[0].attributes('style')).toContain('width: 92%')
    expect(bars[2].attributes('style')).toContain('width: 76%')
  })
  it('PageHeader 无 desc 时不出空节点', () => {
    const w = mount(PageHeader, { props: { title: 'T' } })
    expect(w.find('.m-page-title').text()).toBe('T')
    expect(w.find('.m-page-desc').exists()).toBe(false)
  })
})

describe('DataTable', () => {
  const columns = [
    { key: 'name', title: '名称' },
    { key: 'n', title: '数量', align: 'right', sortable: true },
    { key: 'sev', title: '状态', render: (row) => h(StatusBadge, { sev: row.sev, text: row.sev }) },
  ]
  const rows = [
    { id: 'a', name: 'A', n: 2, sev: 'critical' },
    { id: 'b', name: 'B', n: 9, sev: 'warning' },
  ]

  it('render 列走组件而非文本，缺失值出破折号', () => {
    const w = mount(DataTable, { props: { columns, rows } })
    expect(w.findAll('tbody tr')).toHaveLength(2)
    expect(w.find('tbody .m-badge').attributes('data-sev')).toBe('critical')
    const withGap = mount(DataTable, {
      props: { columns: [{ key: 'x', title: 'X' }], rows: [{ id: 1, x: '' }] },
    })
    expect(withGap.text()).toContain('—')
  })

  it('空数据渲染空态而不是空 table', () => {
    const w = mount(DataTable, { props: { columns, rows: [], emptyDesc: '换个条件试试' } })
    expect(w.find('.m-empty').exists()).toBe(true)
    expect(w.find('.m-empty-desc').text()).toBe('换个条件试试')
  })

  it('loading 渲染骨架，行数归零', () => {
    const w = mount(DataTable, { props: { columns, rows, loading: true } })
    expect(w.findAll('tbody tr')).toHaveLength(1)
    expect(w.find('.m-skeleton').exists()).toBe(true)
  })

  it('排序点击上报 key 与方向，全选上报全部 rowKey', async () => {
    const w = mount(DataTable, { props: { columns, rows, selectable: true, selected: [] } })
    // 开启 selectable 后 th[0] 是复选框列，可排序的"数量"在 th[2]
    await w.findAll('th')[2].trigger('click')
    expect(w.emitted('sort')[0][0]).toEqual({ key: 'n', order: 'asc' })
    await w.find('thead input[type=checkbox]').trigger('change')
    expect(w.emitted('update:selected')[0][0]).toEqual(['a', 'b'])
  })

  it('分页在首页禁用上一页、按总数算页数', async () => {
    const w = mount(DataTable, {
      props: { columns, rows, page: 1, pageSize: 10, total: 35 },
    })
    const btns = w.findAll('.m-pager-btn')
    expect(btns[0].attributes('disabled')).toBeDefined()
    // 35/10 → 4 页，页码 1..4 加前后翻页共 6 个按钮
    expect(btns).toHaveLength(6)
    expect(w.find('.m-pager span').text()).toContain('35')
    await btns[2].trigger('click')
    expect(w.emitted('update:page')[0][0]).toBe(2)
  })
})

describe('toast', () => {
  it('挂到 body 的单例容器，按时长自动消失', async () => {
    vi.useFakeTimers()
    const { toast } = await import('../components/toast.js')
    toast('已重启 Pod', 'pass')
    await nextTick()
    const host = document.querySelector('#mutong-toasts')
    expect(host).toBeTruthy()
    expect(host.querySelectorAll('.m-toast')).toHaveLength(1)
    expect(host.querySelector('.m-toast').getAttribute('data-kind')).toBe('pass')
    vi.advanceTimersByTime(3000)
    await nextTick()
    expect(host.querySelectorAll('.m-toast')).toHaveLength(0)
    // 第二次调用复用同一容器
    toast('again', 'critical')
    await nextTick()
    expect(document.querySelectorAll('#mutong-toasts')).toHaveLength(1)
    vi.useRealTimers()
  })
})
