/* 主题读取层：canvas 与 G6 拿不到 CSS 变量，必须由 JS 侧把自定义属性读成色值。
   约定：只有这里允许出现色值读取，业务代码禁止再写字面色号（模式 C）。 */

const read = (name) => {
  try {
    // getComputedStyle 取自定义属性会带前导空格，必须 trim
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  } catch {
    return ''
  }
}

/** 读单个 token，取不到时回退 */
export function tok(name, fallback = '') {
  return read(name) || fallback
}

/** 一次性读出图表/拓扑所需的全部配色，供 force-graph 与 G6 建模使用 */
export function readTheme() {
  return {
    name: document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'light',
    canvasBg: tok('--graph-canvas-bg', '#f8f9fa'),
    nodeFill: tok('--graph-node-fill', '#ffffff'),
    nodeStroke: tok('--graph-node-stroke', '#d9d9d9'),
    label: tok('--graph-label', '#8c8c8c'),
    edge: tok('--graph-edge', '#bfbfbf'),
    tooltipBg: tok('--graph-tooltip-bg', 'rgba(0,0,0,0.8)'),
    tooltipFg: tok('--graph-tooltip-fg', '#ffffff'),
    text: tok('--text-primary', '#262626'),
    textMuted: tok('--text-muted', '#8c8c8c'),
    surface: tok('--surface-card', '#ffffff'),
    border: tok('--border-default', '#e8e8e8'),
    accent: tok('--accent', '#1890ff'),
    /* 12 支分类色，索引从 1 开始，与 CSS 的 --chart-N 一一对应 */
    chart: Array.from({ length: 12 }, (_, i) => tok(`--chart-${i + 1}`, '#1890ff')),
    sev: {
      critical: tok('--sev-critical-fg', '#cf1322'),
      error: tok('--sev-error-fg', '#ff4d4f'),
      warning: tok('--sev-warning-fg', '#d46b08'),
      info: tok('--sev-info-fg', '#096dd9'),
      pass: tok('--sev-pass-fg', '#389e0d'),
      pending: tok('--sev-pending-fg', '#595959'),
    },
  }
}

/**
 * 订阅主题变化。双保险：自定义事件（本页主动切换）+ MutationObserver
 * （跨标签页 storage 同步、或外部直接改属性）。返回退订函数。
 */
export function onThemeChange(fn) {
  const emit = () => fn(readTheme())
  window.addEventListener('mutong:theme', emit)
  let mo
  if (typeof MutationObserver !== 'undefined') {
    mo = new MutationObserver(emit)
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
  }
  return () => {
    window.removeEventListener('mutong:theme', emit)
    if (mo) mo.disconnect()
  }
}
