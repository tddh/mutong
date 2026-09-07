import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

/* 这一组不是行为测试而是"对齐契约"测试：布局比例是静态 CSS 事实，
   没有浏览器可目视，唯一能防回退的手段就是把契约钉在源码上。 */

const here = dirname(fileURLToPath(import.meta.url))
const root = resolve(here, '..')
const readStyle = (p) => readFileSync(resolve(root, 'styles', p), 'utf-8')
const readSrc = (p) => readFileSync(resolve(root, p), 'utf-8')
const stripComments = (css) => css.replace(/\/\*[\s\S]*?\*\//g, '')

/** 取出某个 :where(...) 选择器之后的第一条规则体（源序，首个命中） */
function firstRule(css, selector) {
  const i = stripComments(css).indexOf(selector)
  expect(i, `找不到选择器 ${selector}`).toBeGreaterThanOrEqual(0)
  const body = stripComments(css).slice(i)
  const m = body.match(/\{([^}]*)\}/)
  expect(m, `${selector} 没有规则体`).toBeTruthy()
  return m[1]
}

const tokens = readStyle('tokens.css')
const common = readStyle('common.css')
const comp = readStyle('components.css')

describe('流式 shell 与对齐契约', () => {
  it('shell 尺寸走流式插值，不再是固定像素内缩', () => {
    expect(tokens).toMatch(/--shell-pad:\s*clamp\(/)
    expect(tokens).toMatch(/--shell-max:\s*\d+px/)
  })

  it('页面容器与导航内层引用同一组 shell 变量——两侧对齐全靠这个', () => {
    const shell = firstRule(common, '.container')
    const nav = firstRule(comp, ':where(.nav-inner)')
    for (const [name, body] of [
      ['shell', shell],
      ['nav-inner', nav],
    ]) {
      expect(body, `${name} 缺少 --shell-max`).toContain('--shell-max')
      expect(body, `${name} 缺少 --shell-pad`).toContain('--shell-pad')
    }
  })

  it('全站不得再出现页面级死宽度上限（1200/1400 那一类）', () => {
    const files = [
      ...readdirSync(resolve(root, 'styles')).filter((f) => f.endsWith('.css')).map((f) => `styles/${f}`),
      'resource-table.css',
      ...['monitoring', 'topology', 'diagnosis', 'terminal', 'inspection-history', 'retrospective-history']
        .map((d) => readdirSync(resolve(root, d)).filter((f) => f.endsWith('.css')).map((f) => `${d}/${f}`))
        .flat(),
    ]
    const bad = []
    for (const f of files) {
      for (const [n, line] of readSrc(f).split('\n').entries()) {
        // @media (max-width: 1200px) 是响应式断点，合法
        if (/@media/.test(line)) continue
        if (/max-width:\s*(?:1200|1400|1600)px/.test(line)) bad.push(`${f}:${n + 1} ${line.trim()}`)
      }
    }
    expect(bad).toEqual([])
  })

  it('sticky 导航不得靠 margin-bottom 撑场距（滚动后间隙会被内容穿过）', () => {
    const nav = firstRule(comp, ':where(.navbar)')
    expect(nav).toMatch(/position:\s*sticky/)
    expect(nav).not.toMatch(/margin/)
  })
})

describe('网格整除分档', () => {
  const all = stripComments(comp).replace(/\s+/g, ' ')
  /** 某选择器作为选择器列表开头时，其后第一条规则里的 repeat 列数 */
  const colsAfter = (sel) =>
    [...all.matchAll(new RegExp(`${sel.replace(/[[\]\\().]/g, '\\$&')}[^{]*\\{[^}]*?repeat\\((\\d+|[a-z-]+)`, 'g'))].map((m) => m[1])

  it('统计卡与快捷入口必须成对写在同一选择器列表——拆开即各自漂移', () => {
    const pairs = (all.match(/:where\(\.m-stat-grid\), :where\(\.m-quick-grid\) \{/g) || []).length
    expect(pairs, '两网格未共用档表').toBeGreaterThanOrEqual(3)
  })

  it('共用档只走能整除 6 的列数，不得残留 auto-fit', () => {
    const cols = colsAfter(':where(.m-stat-grid),')
    expect(cols.length).toBeGreaterThanOrEqual(3)
    expect(new Set(cols)).toEqual(new Set(['2', '3', '6']))
  })

  it('4 项网格走 2/4 档，不复用 6 列档；页面需声明 data-n', () => {
    const four = colsAfter(":where(.m-stat-grid[data-n='4'])")
    expect(new Set(four)).toEqual(new Set(['2', '4']))
    expect(readSrc('clusters/clusters.js')).toContain("'data-n': '4'")
  })

  it('双列面板不得用 auto-fit——shell 放宽后会被撑成 4 列，5 张面板变 4+1', () => {
    expect(firstRule(comp, ':where(.m-grid-2)')).not.toContain('auto-fit')
  })

  it('统计卡的数值与图标随列宽流式缩放，锁死值会在窄列下溢出', () => {
    const value = firstRule(comp, ':where(.m-stat-card-value)')
    const icon = firstRule(comp, ':where(.m-stat-card-icon)')
    expect(value).toMatch(/font-size:\s*clamp\(/)
    expect(value).not.toContain('--fs-800')
    expect(icon).toMatch(/width:\s*clamp\(/)
    expect(icon).not.toMatch(/width:\s*38px/)
  })
})
