import G6 from '@antv/g6'
import { readTheme, tok } from '../styles/theme.js'

/* 复盘两页共用的 G6 拓扑/因果链渲染。此前两份 ~115 行实现完全相同且各自写死色号、
   tooltip 用 innerHTML 直拼 LLM 生成的 label（存储型 XSS）。
   约定：canvas 色一律经 styles/theme.js 读语义 token（6 位 hex，可安全拼透明度后缀）；
   tooltip 内容只走 textContent。 */

const ROLE_LABELS_TOPO = {
  root_cause: '故障业务',
  upstream: '上游调用方',
  downstream: '下游依赖方',
}
const ROLE_LABELS_CAUSAL = { root_cause: '根因', intermediate: '中间节点', effect: '最终影响' }

function graphColors() {
  const t = readTheme()
  return {
    rootCause: t.sev.critical,
    upstream: tok('--chart-7', '#fa8c16'),
    downstream: tok('--chart-1', '#1890ff'),
    intermediate: tok('--chart-7', '#fa8c16'),
    effect: tok('--chart-1', '#1890ff'),
    nodeFill: t.nodeFill,
    nodeStroke: t.nodeStroke,
    label: t.text,
    edge: t.edge,
    edgeLabel: t.textMuted,
    confHigh: t.sev.pass,
    confMid: t.sev.warning,
    confLow: t.sev.error,
  }
}

const confColor = (conf, c) => (conf >= 0.8 ? c.confHigh : conf >= 0.5 ? c.confMid : c.confLow)

/** 业务拓扑数据：中心应用 + 上下游调用方。无数据返回 null */
export function buildTopoData(pm) {
  const bc = pm && pm.business_calls
  if (!bc) return null
  const centerName = bc.app_name || (pm.business_context && pm.business_context.app_name) || ''
  if (!centerName) return null

  const nodes = [
    {
      id: 'center',
      label: centerName,
      role: 'root_cause',
      criticality: (pm.business_context && pm.business_context.criticality) || 'high',
    },
  ]
  const edges = []
  ;(bc.upstreams || []).forEach((u, i) => {
    const uid = 'up_' + i
    nodes.push({
      id: uid,
      label: u.app_name || '',
      role: 'upstream',
      criticality: u.criticality || 'medium',
    })
    edges.push({ source: uid, target: 'center', label: '调用' })
  })
  ;(bc.downstreams || []).forEach((d, i) => {
    const did = 'dn_' + i
    nodes.push({
      id: did,
      label: d.app_name || '',
      role: 'downstream',
      criticality: d.criticality || 'medium',
    })
    edges.push({ source: 'center', target: did, label: '调用' })
  })
  if (nodes.length <= 1) return null
  return { nodes, edges }
}

/** 因果链数据：按名称去重，首因标 root_cause、末果标 effect。无数据返回 null */
export function buildCausalData(pm) {
  const links = pm && pm.causal_chain && pm.causal_chain.links
  if (!links || links.length === 0) return null

  const idOf = (name) => 'n_' + name.replace(/[^a-zA-Z0-9]/g, '_')
  const dedup = {}
  links.forEach((link, idx) => {
    const causeName = link.cause || ''
    const effectName = link.effect || ''
    const isFirst = idx === 0
    const isLast = idx === links.length - 1
    if (!dedup[causeName]) {
      dedup[causeName] = {
        id: idOf(causeName),
        label: causeName || '?',
        role: isFirst ? 'root_cause' : 'intermediate',
      }
    } else if (isFirst) {
      dedup[causeName].role = 'root_cause'
    }
    if (!dedup[effectName]) {
      dedup[effectName] = {
        id: idOf(effectName),
        label: effectName || '?',
        role: isLast ? 'effect' : 'intermediate',
      }
    } else if (isLast) {
      dedup[effectName].role = 'effect'
    }
  })

  const nodes = Object.values(dedup).map((n) => ({
    id: n.id,
    label: n.label.length > 30 ? n.label.slice(0, 30) + '...' : n.label,
    fullLabel: n.label,
    role: n.role,
  }))
  const edges = links.map((link) => ({
    source: idOf(link.cause || ''),
    target: idOf(link.effect || ''),
    label: ((link.confidence || 0) * 100).toFixed(0) + '%',
    confidence: link.confidence || 0,
    evidence: link.evidence,
  }))
  return { nodes, edges }
}

/* tooltip 逐行 textContent 写入：model.label 来自 LLM/后端，禁止进 innerHTML */
function attachTooltip(graph, container, linesOf) {
  const t = readTheme()
  const el = document.createElement('div')
  el.style.cssText = `position:absolute;display:none;background:${t.tooltipBg};color:${t.tooltipFg};padding:6px 10px;border-radius:4px;font-size:12px;pointer-events:none;z-index:100;max-width:280px;line-height:1.5;`
  container.appendChild(el)
  const move = (e) => {
    el.style.left = e.canvasX + 15 + 'px'
    el.style.top = e.canvasY - 10 + 'px'
  }
  graph.on('node:mouseenter', (e) => {
    const model = e.item.getModel()
    el.replaceChildren()
    for (const line of linesOf(model)) {
      const row = document.createElement('div')
      row.textContent = line
      el.appendChild(row)
    }
    el.style.display = 'block'
    move(e)
  })
  graph.on('node:mousemove', move)
  graph.on('node:mouseleave', () => {
    el.style.display = 'none'
  })
}

let flowLineRegistered = false
function ensureFlowLine() {
  if (flowLineRegistered) return
  G6.registerEdge(
    'flow-line',
    {
      afterDraw(cfg, group) {
        const shape = group.get('children')[0]
        if (!shape) return
        const length = shape.getTotalLength ? shape.getTotalLength() : 0
        if (length === 0) return
        let start = 0
        const circle = group.addShape('circle', {
          attrs: { x: 0, y: 0, r: 4, fill: cfg.confColor || cfg.stroke, opacity: 0.8 },
          name: 'flow-circle',
        })
        const step = () => {
          if (!circle || circle.destroyed) return
          start += 0.02
          if (start > 1) start = 0
          const point = shape.getPoint(start)
          if (point) circle.attr({ x: point.x, y: point.y })
          cfg._rafId = requestAnimationFrame(step)
        }
        step()
      },
      destroy() {
        if (this.cfg && this.cfg._rafId) {
          cancelAnimationFrame(this.cfg._rafId)
          this.cfg._rafId = null
        }
      },
    },
    'polyline',
  )
  flowLineRegistered = true
}

/** 渲染业务拓扑图；无数据或无容器返回 null。调用方负责 destroy */
export function renderTopoGraph(container, pm, { height = 400 } = {}) {
  if (!container) return null
  const data = buildTopoData(pm)
  if (!data) return null
  const c = graphColors()
  const roleColor = { root_cause: c.rootCause, upstream: c.upstream, downstream: c.downstream }

  const graph = new G6.Graph({
    container,
    width: container.clientWidth || 700,
    height,
    layout: { type: 'dagre', rankdir: 'LR', nodesep: 60, ranksep: 100 },
    modes: { default: ['drag-canvas', 'zoom-canvas', 'drag-node'] },
    defaultNode: {
      type: 'rect',
      size: [140, 40],
      style: { radius: 8, fill: c.nodeFill, stroke: c.nodeStroke, lineWidth: 1 },
      labelCfg: { style: { fontSize: 12, fill: c.label } },
    },
    defaultEdge: {
      type: 'polyline',
      style: { stroke: c.edge, lineWidth: 1.5, endArrow: true },
    },
  })
  graph.node((node) => {
    const color = roleColor[node.role] || c.nodeStroke
    return {
      style: { fill: color + '1a', stroke: color, lineWidth: 2 },
      labelCfg: { style: { fill: c.label, fontSize: 12 } },
    }
  })
  graph.edge(() => ({
    labelCfg: { autoRotate: true, style: { fontSize: 10, fill: c.edgeLabel } },
  }))
  attachTooltip(graph, container, (model) => {
    const lines = [model.label]
    if (model.role) lines.push('角色: ' + (ROLE_LABELS_TOPO[model.role] || model.role))
    if (model.criticality) lines.push('关键度: ' + model.criticality)
    return lines
  })
  graph.data({
    nodes: data.nodes.map((n) => ({ ...n, color: roleColor[n.role] || c.nodeStroke })),
    edges: data.edges,
  })
  graph.render()
  return graph
}

/** 渲染因果链 DAG；无数据或无容器返回 null。调用方负责 destroy */
export function renderCausalGraph(container, pm, { height = 300, animate = true } = {}) {
  if (!container) return null
  const data = buildCausalData(pm)
  if (!data) return null
  const c = graphColors()
  const roleColor = { root_cause: c.rootCause, intermediate: c.intermediate, effect: c.effect }
  if (animate) ensureFlowLine()

  const graph = new G6.Graph({
    container,
    width: container.clientWidth || 700,
    height,
    layout: { type: 'dagre', rankdir: 'LR', nodesep: 50, ranksep: 100 },
    modes: { default: ['drag-canvas', 'zoom-canvas', 'drag-node'] },
    defaultNode: {
      type: 'rect',
      size: [140, 40],
      style: { radius: 6, fill: c.nodeFill, stroke: c.nodeStroke, lineWidth: 1 },
      labelCfg: { style: { fontSize: 12, fill: c.label, fontWeight: 500 } },
    },
    defaultEdge: {
      type: animate ? 'flow-line' : 'polyline',
      style: {
        stroke: c.edge,
        lineWidth: 1.5,
        lineDash: [4, 2],
        endArrow: { path: G6.Arrow.triangle(6, 8, 0), fill: c.edge },
      },
      labelCfg: { autoRotate: true, style: { fontSize: 10, fontWeight: 600 } },
    },
  })
  graph.node((node) => ({
    style: {
      fill: c.nodeFill,
      stroke: roleColor[node.role] || c.intermediate,
      lineWidth: 2,
    },
  }))
  graph.edge((edge) => {
    const color = confColor(edge.confidence || 0, c)
    return {
      confColor: color,
      style: { stroke: color },
      labelCfg: { style: { fill: color, fontSize: 10, fontWeight: 600 } },
    }
  })
  attachTooltip(graph, container, (model) => {
    const lines = [model.fullLabel || model.label]
    if (model.role) lines.push('角色: ' + (ROLE_LABELS_CAUSAL[model.role] || model.role))
    return lines
  })
  graph.data(data)
  graph.render()
  return graph
}

export function destroyGraph(graph) {
  if (graph) graph.destroy()
}