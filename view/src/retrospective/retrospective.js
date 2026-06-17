import { createApp, ref, h, onMounted, onUnmounted, nextTick } from 'vue'
import { NavBar } from '../components/SharedComponents.js'
import { API } from '../utils/api.js'
import '../styles/common.css'
import G6 from '@antv/g6'

const SEVERITY_COLORS = {
  critical: '#cf1322',
  error: '#cf1322',
  warning: '#faad14',
  info: '#1890ff',
}

const SEVERITY_BG = {
  critical: '#fff1f0',
  error: '#fff1f0',
  warning: '#fff7e6',
  info: '#e6f7ff',
}

function formatTime(ts) {
  if (!ts) return '-'
  return new Date(ts).toLocaleString('zh-CN')
}

function severityBadge(severity, text) {
  const s = (severity || '').toLowerCase()
  return h(
    'span',
    {
      style: {
        display: 'inline-block',
        padding: '2px 8px',
        borderRadius: '4px',
        fontSize: '12px',
        fontWeight: 500,
        color: SEVERITY_COLORS[s] || '#666',
        background: SEVERITY_BG[s] || '#f5f5f5',
      },
    },
    text || severity,
  )
}

function sectionBlock(title, content) {
  return h('div', { style: { marginBottom: '20px' } }, [
    h(
      'h3',
      {
        style: {
          fontSize: '15px',
          fontWeight: 600,
          color: 'var(--text-primary)',
          margin: '0 0 12px 0',
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
        },
      },
      title,
    ),
    content,
  ])
}

function infoCard(label, value) {
  return h(
    'div',
    {
      style: {
        background: '#fff',
        border: '1px solid var(--border-color)',
        borderRadius: '6px',
        padding: '12px',
      },
    },
    [
      h(
        'div',
        { style: { fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '4px' } },
        label,
      ),
      h(
        'div',
        {
          style: {
            fontSize: '14px',
            fontWeight: 500,
            color: 'var(--text-primary)',
            wordBreak: 'break-all',
          },
        },
        value || '-',
      ),
    ],
  )
}

function timelineItem(event, idx, isLast) {
  const severity = event.severity || 'info'
  const timeColor = SEVERITY_COLORS[severity.toLowerCase()] || '#1890ff'
  const borderColor = SEVERITY_BG[severity.toLowerCase()] || '#e6f7ff'

  return h(
    'div',
    {
      style: {
        display: 'flex',
        gap: '16px',
        position: 'relative',
        paddingBottom: isLast ? '0' : '20px',
      },
    },
    [
      h(
        'div',
        {
          style: {
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            flexShrink: 0,
            width: '24px',
          },
        },
        [
          h('div', {
            style: {
              width: '12px',
              height: '12px',
              borderRadius: '50%',
              background: timeColor,
              border: '2px solid #fff',
              boxShadow: '0 0 0 2px ' + timeColor,
              flexShrink: 0,
            },
          }),
          !isLast
            ? h('div', {
                style: { flex: 1, width: '2px', background: '#e8e8e8', marginTop: '4px' },
              })
            : null,
        ],
      ),
      h(
        'div',
        {
          style: {
            flex: 1,
            background: '#fff',
            border: '1px solid var(--border-color)',
            borderRadius: '8px',
            padding: '12px 16px',
            borderLeft: '4px solid ' + timeColor,
          },
        },
        [
          h(
            'div',
            {
              style: {
                display: 'flex',
                alignItems: 'center',
                gap: '8px',
                marginBottom: '6px',
                flexWrap: 'wrap',
              },
            },
            [
              h(
                'span',
                {
                  style: {
                    fontSize: '12px',
                    color: 'var(--text-secondary)',
                    fontFamily: 'monospace',
                  },
                },
                formatTime(event.timestamp),
              ),
              severityBadge(severity, (event.event_type || event.eventType || '').toUpperCase()),
              event.source
                ? h(
                    'span',
                    {
                      style: {
                        fontSize: '11px',
                        padding: '1px 6px',
                        borderRadius: '3px',
                        background: '#f0f0f0',
                        color: '#666',
                      },
                    },
                    '来源: ' + event.source,
                  )
                : null,
            ],
          ),
          h(
            'div',
            { style: { fontSize: '13px', color: 'var(--text-primary)', lineHeight: '1.6' } },
            event.description || '-',
          ),
          event.resource
            ? h(
                'div',
                {
                  style: {
                    fontSize: '12px',
                    color: 'var(--text-secondary)',
                    marginTop: '6px',
                    fontFamily: 'monospace',
                    background: '#fafafa',
                    padding: '4px 8px',
                    borderRadius: '4px',
                    display: 'inline-block',
                  },
                },
                '资源: ' + event.resource,
              )
            : null,
        ],
      ),
    ],
  )
}

function causalLinkItem(link, idx) {
  const confColor =
    link.confidence >= 0.8 ? '#52c41a' : link.confidence >= 0.5 ? '#faad14' : '#ff4d4f'
  const confBg = link.confidence >= 0.8 ? '#f6ffed' : link.confidence >= 0.5 ? '#fff7e6' : '#fff1f0'

  return h(
    'div',
    {
      style: {
        background: '#fff',
        border: '1px solid var(--border-color)',
        borderRadius: '8px',
        padding: '14px',
        marginBottom: idx > 0 ? '12px' : 0,
      },
    },
    [
      h(
        'div',
        {
          style: {
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            marginBottom: '10px',
            flexWrap: 'wrap',
          },
        },
        [
          h(
            'span',
            { style: { fontSize: '14px', fontWeight: 600, color: 'var(--error-color)' } },
            link.cause || '-',
          ),
          h('span', { style: { fontSize: '16px', color: '#999' } }, '→'),
          h(
            'span',
            { style: { fontSize: '14px', fontWeight: 600, color: 'var(--primary-color)' } },
            link.effect || '-',
          ),
          h(
            'span',
            {
              style: {
                fontSize: '11px',
                padding: '1px 8px',
                borderRadius: '10px',
                background: confBg,
                color: confColor,
                fontWeight: 500,
                marginLeft: 'auto',
              },
            },
            '置信度 ' + ((link.confidence || 0) * 100).toFixed(0) + '%',
          ),
        ],
      ),
      link.evidence
        ? h(
            'div',
            {
              style: {
                fontSize: '12px',
                color: 'var(--text-secondary)',
                background: '#fafafa',
                padding: '8px 12px',
                borderRadius: '4px',
                lineHeight: '1.5',
              },
            },
            '证据: ' + link.evidence,
          )
        : null,
    ],
  )
}

function actionItemRow(item, idx) {
  const priorityColors = { high: '#ff4d4f', medium: '#faad14', low: '#52c41a' }
  const priorityLabels = { high: '高', medium: '中', low: '低' }
  const priority = (item.priority || 'medium').toLowerCase()

  return h('tr', { style: { borderBottom: '1px solid #f0f0f0' } }, [
    h(
      'td',
      { style: { padding: '10px 12px', fontSize: '13px', color: 'var(--text-primary)' } },
      idx + 1 + '. ' + (item.description || '-'),
    ),
    h(
      'td',
      { style: { padding: '10px 12px' } },
      severityBadge(priority, priorityLabels[priority] || priority),
    ),
    h(
      'td',
      { style: { padding: '10px 12px', fontSize: '13px', color: 'var(--text-secondary)' } },
      item.owner || '-',
    ),
    h(
      'td',
      { style: { padding: '10px 12px', fontSize: '13px', color: 'var(--text-secondary)' } },
      item.due_date || '-',
    ),
  ])
}

// G6 图实例存储（模块级，供 setup 和 methods 共享）
const graphStore = {}

// 根据 causal_chain.links 推断拓扑节点角色
function inferNodeRoles(topologyNodes, causalLinks) {
  const roles = {}
  if (!topologyNodes || !topologyNodes.length || !causalLinks || !causalLinks.length) {
    ;(topologyNodes || []).forEach((n) => {
      roles[n.uid] = 'normal'
    })
    return roles
  }
  const nodeNameSet = new Set(topologyNodes.map((n) => n.name))
  const uidByName = {}
  topologyNodes.forEach((n) => {
    uidByName[n.name] = n.uid
  })
  // root_cause: 第一个 cause 对应的拓扑节点
  const rootCauseName = causalLinks[0].cause
  const rootCauseNode = topologyNodes.find((n) => rootCauseName.indexOf(n.name) >= 0)
  const affectedNames = new Set()
  const indirectNames = new Set()
  causalLinks.forEach((link, idx) => {
    if (idx === 0) affectedNames.add(link.cause)
    affectedNames.add(link.effect)
  })
  // 判断某个拓扑节点名是否匹配因果链中的 cause/effect（模糊匹配）
  function matches(name, causalText) {
    return causalText.indexOf(name) >= 0
  }
  topologyNodes.forEach((n) => {
    if (
      n.name === rootCauseName ||
      n.uid === rootCauseName ||
      (rootCauseNode && n.uid === rootCauseNode.uid)
    ) {
      roles[n.uid] = 'root_cause'
    } else if (
      affectedNames.has(n.name) ||
      affectedNames.has(n.uid) ||
      [...affectedNames].some((t) => matches(n.name, t))
    ) {
      roles[n.uid] = 'affected'
    } else {
      roles[n.uid] = 'normal'
    }
  })
  return roles
}

createApp({
  components: { NavBar },
  setup() {
    const fingerprint = ref('')
    const loading = ref(false)
    const error = ref(null)
    const timeline = ref([])
    const causalChain = ref(null)
    const postmortem = ref(null)
    const generating = ref(false)
    const markdownText = ref('')
    const activeSection = ref('')
    const logShowAll = ref(false)
    const actionKanbanFilter = ref('all')
    const fullscreenGraph = ref(null)

    const toggleFullscreen = (name) => {
      const wasFull = fullscreenGraph.value === name
      fullscreenGraph.value = wasFull ? null : name
      if (!wasFull) {
        requestAnimationFrame(() => {
          requestAnimationFrame(() => {
            const inst =
              name === 'topo' ? graphStore._topoGraphInstance : graphStore._causalGraphInstance
            if (inst) {
              const el = inst.get('container')
              inst.changeSize(el.clientWidth, el.clientHeight)
              inst.fitView(20)
            }
          })
        })
      }
    }

    const loadAnalysis = async () => {
      if (!fingerprint.value.trim()) {
        error.value = '请输入告警指纹'
        return
      }
      loading.value = true
      error.value = null
      postmortem.value = null
      markdownText.value = ''
      try {
        const [tl, cc] = await Promise.all([
          API.retrospective.timeline(fingerprint.value.trim()),
          API.retrospective.causalChain(fingerprint.value.trim()),
        ])
        timeline.value = tl || []
        causalChain.value = cc || null
      } catch (e) {
        error.value = '加载分析数据失败: ' + (e.message || '未知错误')
        timeline.value = []
        causalChain.value = null
      } finally {
        loading.value = false
      }
    }

    const generatePostmortem = async () => {
      if (!fingerprint.value.trim()) {
        error.value = '请输入告警指纹'
        return
      }
      generating.value = true
      error.value = null
      try {
        postmortem.value = await API.retrospective.postmortem(fingerprint.value.trim())
        markdownText.value = ''
      } catch (e) {
        error.value = '生成复盘报告失败: ' + (e.message || '未知错误')
        postmortem.value = null
      } finally {
        generating.value = false
      }
    }

    const exportMarkdown = async () => {
      if (!fingerprint.value.trim()) return
      try {
        const text = await API.retrospective.postmortemText(fingerprint.value.trim())
        if (text) {
          markdownText.value = text
          const blob = new Blob([text], { type: 'text/markdown;charset=utf-8' })
          const url = URL.createObjectURL(blob)
          const a = document.createElement('a')
          a.href = url
          a.download = `postmortem_${fingerprint.value.trim().slice(0, 16)}.md`
          a.click()
          URL.revokeObjectURL(url)
        }
      } catch (e) {
        error.value = '导出 Markdown 失败: ' + (e.message || '未知错误')
      }
    }

    const handleKeydown = (e) => {
      if (e.key === 'Escape' && fullscreenGraph.value) {
        fullscreenGraph.value = null
      }
    }

    onMounted(() => {
      document.addEventListener('keydown', handleKeydown)
    })

    onUnmounted(() => {
      document.removeEventListener('keydown', handleKeydown)
      if (graphStore._topoGraphInstance) {
        graphStore._topoGraphInstance.destroy()
        graphStore._topoGraphInstance = null
      }
      if (graphStore._causalGraphInstance) {
        graphStore._causalGraphInstance.destroy()
        graphStore._causalGraphInstance = null
      }
    })

    return {
      fingerprint,
      loading,
      error,
      timeline,
      causalChain,
      postmortem,
      generating,
      markdownText,
      activeSection,
      logShowAll,
      actionKanbanFilter,
      fullscreenGraph,
      toggleFullscreen,
      loadAnalysis,
      generatePostmortem,
      exportMarkdown,
    }
  },
  methods: {
    _destroyTopoGraph() {
      if (graphStore._topoGraphInstance) {
        graphStore._topoGraphInstance.destroy()
        graphStore._topoGraphInstance = null
      }
    },
    _destroyCausalGraph() {
      if (graphStore._causalGraphInstance) {
        graphStore._causalGraphInstance.destroy()
        graphStore._causalGraphInstance = null
      }
    },
    _initTopoGraph() {
      this._destroyTopoGraph()
      const r = this.postmortem
      if (!r) return
      const container = this.$refs.topoGraphContainer
      if (!container) return

      const bc = r.business_calls
      if (!bc) return

      const centerName = bc.app_name || (r.business_context && r.business_context.app_name) || ''
      if (!centerName) return

      const nodes = []
      const edges = []
      const centerId = 'center'
      nodes.push({
        id: centerId,
        label: centerName,
        role: 'root_cause',
        criticality: (r.business_context && r.business_context.criticality) || 'high',
      })
      ;(bc.upstreams || []).forEach((u, i) => {
        const uid = 'up_' + i
        nodes.push({
          id: uid,
          label: u.app_name || '',
          role: 'upstream',
          criticality: u.criticality || 'medium',
        })
        edges.push({ source: uid, target: centerId, label: '调用' })
      })
      ;(bc.downstreams || []).forEach((d, i) => {
        const did = 'dn_' + i
        nodes.push({
          id: did,
          label: d.app_name || '',
          role: 'downstream',
          criticality: d.criticality || 'medium',
        })
        edges.push({ source: centerId, target: did, label: '调用' })
      })

      if (nodes.length <= 1) return

      const critColors = { high: '#cf1322', medium: '#faad14', low: '#52c41a' }
      const roleLabels = {
        root_cause: '故障业务',
        upstream: '上游调用方',
        downstream: '下游依赖方',
      }
      const roleColors = { root_cause: '#cf1322', upstream: '#fa8c16', downstream: '#1890ff' }

      const g6Nodes = nodes.map((n) => ({
        id: n.id,
        label: n.label,
        role: n.role,
        color: roleColors[n.role] || '#d9d9d9',
        criticality: n.criticality,
      }))
      const g6Edges = edges.map((e) => ({ source: e.source, target: e.target, label: e.label }))

      const width = container.clientWidth || 700
      const height = 400
      const graph = new G6.Graph({
        container,
        width,
        height,
        layout: { type: 'dagre', rankdir: 'LR', nodesep: 60, ranksep: 100 },
        modes: { default: ['drag-canvas', 'zoom-canvas', 'drag-node'] },
        defaultNode: {
          type: 'rect',
          size: [140, 40],
          style: { radius: 8, fill: '#fafbfc', stroke: '#d9d9d9', lineWidth: 1 },
          labelCfg: { style: { fontSize: 12, fill: '#333' } },
        },
        defaultEdge: {
          type: 'polyline',
          style: { stroke: '#aaa', lineWidth: 1.5, endArrow: true },
        },
      })
      graph.node((node) => {
        const color = node.color || '#18900ff'
        const critColor = critColors[node.criticality] || '#faad14'
        return {
          style: { fill: color + '10', stroke: color, lineWidth: 2 },
          labelCfg: { style: { fill: '#333', fontSize: 12 } },
        }
      })
      graph.edge(() => ({ labelCfg: { autoRotate: true, style: { fontSize: 10, fill: '#999' } } }))
      const tooltipEl = document.createElement('div')
      tooltipEl.style.cssText =
        'position:absolute;display:none;background:rgba(0,0,0,0.8);color:#fff;padding:6px 10px;border-radius:4px;font-size:12px;pointer-events:none;z-index:100;max-width:250px;line-height:1.5;'
      container.appendChild(tooltipEl)
      graph.on('node:mouseenter', (e) => {
        const model = e.item.getModel()
        const items = [model.label]
        if (model.role) items.push('角色: ' + (roleLabels[model.role] || model.role))
        if (model.criticality) items.push('关键度: ' + model.criticality)
        tooltipEl.innerHTML = items.join('<br>')
        tooltipEl.style.display = 'block'
        tooltipEl.style.left = e.canvasX + 15 + 'px'
        tooltipEl.style.top = e.canvasY - 10 + 'px'
      })
      graph.on('node:mouseleave', () => {
        tooltipEl.style.display = 'none'
      })
      graph.data({ nodes: g6Nodes, edges: g6Edges })
      graph.render()
      graphStore._topoGraphInstance = graph
    },
    _initCausalGraph() {
      this._destroyCausalGraph()
      const r = this.postmortem
      if (!r) return
      const cc = r.causal_chain
      if (!cc || !cc.links || cc.links.length === 0) return
      const container = this.$refs.causalGraphContainer
      if (!container) return

      const linkList = cc.links || []
      const dedupNameMap = {}
      linkList.forEach((link, idx) => {
        const causeName = link.cause || ''
        const effectName = link.effect || ''
        const causeId = 'n_' + causeName.replace(/[^a-zA-Z0-9]/g, '_')
        const effectId = 'n_' + effectName.replace(/[^a-zA-Z0-9]/g, '_')
        const isFirst = idx === 0
        const isLast = idx === linkList.length - 1
        if (!dedupNameMap[causeName]) {
          dedupNameMap[causeName] = {
            id: causeId,
            label: causeName || '?',
            role: isFirst ? 'root_cause' : 'intermediate',
          }
        } else if (isFirst) {
          dedupNameMap[causeName].role = 'root_cause'
        }
        if (!dedupNameMap[effectName]) {
          dedupNameMap[effectName] = {
            id: effectId,
            label: effectName || '?',
            role: isLast ? 'effect' : 'intermediate',
          }
        } else if (isLast) {
          dedupNameMap[effectName].role = 'effect'
        }
      })

      const roleColors = { root_cause: '#cf1322', intermediate: '#fa8c16', effect: '#1890ff' }
      const g6Nodes = Object.values(dedupNameMap).map((n) => ({
        id: n.id,
        label: n.label,
        role: n.role,
        color: roleColors[n.role] || '#fa8c16',
      }))

      const g6Edges = linkList.map((link) => {
        const conf = link.confidence || 0
        let confColor = '#ff4d4f'
        if (conf >= 0.8) confColor = '#52c41a'
        else if (conf >= 0.5) confColor = '#faad14'
        const causeId = 'n_' + (link.cause || '').replace(/[^a-zA-Z0-9]/g, '_')
        const effectId = 'n_' + (link.effect || '').replace(/[^a-zA-Z0-9]/g, '_')
        return {
          source: causeId,
          target: effectId,
          label: (conf * 100).toFixed(0) + '%',
          confidence: conf,
          confColor,
          evidence: link.evidence,
        }
      })

      const width = container.clientWidth || 700
      const height = 300

      if (!G6._flowLineRegistered) {
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
                attrs: {
                  x: 0,
                  y: 0,
                  r: 4,
                  fill: cfg.confColor || '#faad14',
                  opacity: 0.8,
                },
                name: 'flow-circle',
              })
              const step = () => {
                if (!circle || circle.destroyed) return
                start += 0.02
                if (start > 1) start = 0
                const point = shape.getPoint(start)
                if (point) {
                  circle.attr({ x: point.x, y: point.y })
                }
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
        G6._flowLineRegistered = true
      }

      const graph = new G6.Graph({
        container,
        width,
        height,
        layout: { type: 'dagre', rankdir: 'LR', nodesep: 50, ranksep: 100 },
        modes: { default: ['drag-canvas', 'zoom-canvas', 'drag-node'] },
        defaultNode: {
          type: 'rect',
          size: [120, 40],
          style: { fill: '#fff', stroke: '#ccc', radius: 6 },
          labelCfg: { style: { fontSize: 12, fill: '#333', fontWeight: 500 } },
        },
        defaultEdge: {
          type: 'flow-line',
          style: {
            stroke: '#aaa',
            lineWidth: 1.5,
            lineDash: [4, 2],
            endArrow: { path: G6.Arrow.triangle(6, 8, 0), fill: '#aaa' },
          },
          labelCfg: { autoRotate: true, style: { fontSize: 10, fontWeight: 600 } },
        },
      })

      graph.node((node) => {
        const base = { fill: '#fff' }
        if (node.role === 'root_cause') base.stroke = '#cf1322'
        else if (node.role === 'effect') base.stroke = '#1890ff'
        else base.stroke = '#fa8c16'
        base.lineWidth = 2
        return { style: base }
      })

      graph.edge((edge) => ({
        style: { stroke: edge.confColor || '#faad14' },
        labelCfg: { style: { fill: edge.confColor || '#faad14', fontSize: 10, fontWeight: 600 } },
      }))

      graph.on('node:mouseenter', (e) => {
        const model = e.item.getModel()
        const tooltipEl = document.createElement('div')
        tooltipEl.style.cssText =
          'position:absolute;background:rgba(0,0,0,0.85);color:#fff;padding:8px 12px;border-radius:6px;font-size:12px;pointer-events:none;z-index:100;max-width:280px;line-height:1.5;'
        tooltipEl.innerHTML = '<strong>' + model.label + '</strong>'
        if (model.role) {
          const roleLabels = { root_cause: '根因', intermediate: '中间节点', effect: '最终影响' }
          tooltipEl.innerHTML +=
            '<br><span style="color:#aaa">角色: ' +
            (roleLabels[model.role] || model.role) +
            '</span>'
        }
        container.appendChild(tooltipEl)
        tooltipEl.style.left = e.canvasX + 15 + 'px'
        tooltipEl.style.top = e.canvasY - 10 + 'px'
        e.item._tooltipEl = tooltipEl
      })
      graph.on('node:mouseleave', (e) => {
        if (e.item._tooltipEl) {
          e.item._tooltipEl.remove()
          e.item._tooltipEl = null
        }
      })
      graph.on('node:mousemove', (e) => {
        if (e.item._tooltipEl) {
          e.item._tooltipEl.style.left = e.canvasX + 15 + 'px'
          e.item._tooltipEl.style.top = e.canvasY - 10 + 'px'
        }
      })

      graph.data({ nodes: g6Nodes, edges: g6Edges })
      graph.render()
      graphStore._causalGraphInstance = graph
    },
    renderPostmortemReport() {
      const r = this.postmortem
      if (!r) return null

      const sections = []

      sections.push(
        sectionBlock(
          '📊 基本信息',
          h(
            'div',
            {
              style: {
                display: 'grid',
                gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
                gap: '12px',
              },
            },
            [
              infoCard('事故标题', r.incident_title),
              infoCard('开始时间', formatTime(r.start_time)),
              infoCard('结束时间', formatTime(r.end_time)),
              infoCard('持续时间', r.duration || '-'),
              infoCard('严重级别', r.severity ? severityBadge(r.severity) : '-'),
              infoCard('生成时间', formatTime(r.generated_at)),
              r.detection_method ? infoCard('检测方式', r.detection_method) : null,
              r.mttd ? infoCard('MTTD', r.mttd) : null,
            ].filter(Boolean),
          ),
        ),
      )

      if (r.business_context) {
        const bc = r.business_context
        sections.push(
          sectionBlock(
            '🏢 业务上下文',
            h(
              'div',
              {
                style: {
                  display: 'grid',
                  gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
                  gap: '12px',
                },
              },
              [
                infoCard('业务应用', bc.app_name || '-'),
                infoCard('所属团队', bc.team || '-'),
                infoCard(
                  '关键度',
                  bc.criticality ? severityBadge(bc.criticality, bc.criticality) : '-',
                ),
                bc.business_unit ? infoCard('业务线', bc.business_unit) : null,
                bc.environment ? infoCard('环境', bc.environment) : null,
              ].filter(Boolean),
            ),
          ),
        )
      }

      if (
        r.business_calls &&
        (r.business_calls.upstreams?.length || r.business_calls.downstreams?.length)
      ) {
        const callChildren = []
        if (r.business_calls.upstreams?.length) {
          callChildren.push(
            h('div', null, [
              h(
                'div',
                { style: { fontWeight: 600, marginBottom: '4px', color: 'var(--text-primary)' } },
                '上游调用方 (' + r.business_calls.upstreams.length + ')',
              ),
              ...r.business_calls.upstreams.map((u, i) =>
                h(
                  'div',
                  {
                    key: 'u' + i,
                    style: {
                      padding: '4px 0',
                      fontSize: '13px',
                      display: 'flex',
                      gap: '8px',
                      alignItems: 'center',
                    },
                  },
                  [
                    h('span', { style: { color: 'var(--text-primary)' } }, u.app_name),
                    u.criticality
                      ? h(
                          'span',
                          {
                            style: {
                              fontSize: '11px',
                              padding: '1px 6px',
                              borderRadius: '3px',
                              background: '#fff1f0',
                              color: '#cf1322',
                            },
                          },
                          u.criticality,
                        )
                      : null,
                    u.team
                      ? h(
                          'span',
                          {
                            style: {
                              fontSize: '11px',
                              padding: '1px 6px',
                              borderRadius: '3px',
                              background: '#e6f7ff',
                              color: '#096dd9',
                            },
                          },
                          u.team,
                        )
                      : null,
                  ],
                ),
              ),
            ]),
          )
        }
        if (r.business_calls.downstreams?.length) {
          callChildren.push(
            h('div', null, [
              h(
                'div',
                {
                  style: {
                    fontWeight: 600,
                    marginBottom: '4px',
                    marginTop: r.business_calls.upstreams?.length ? '8px' : '0',
                    color: 'var(--text-primary)',
                  },
                },
                '下游依赖方 (' + r.business_calls.downstreams.length + ')',
              ),
              ...r.business_calls.downstreams.map((d, i) =>
                h(
                  'div',
                  {
                    key: 'd' + i,
                    style: {
                      padding: '4px 0',
                      fontSize: '13px',
                      display: 'flex',
                      gap: '8px',
                      alignItems: 'center',
                    },
                  },
                  [
                    h('span', { style: { color: 'var(--text-primary)' } }, d.app_name),
                    d.criticality
                      ? h(
                          'span',
                          {
                            style: {
                              fontSize: '11px',
                              padding: '1px 6px',
                              borderRadius: '3px',
                              background: '#fff1f0',
                              color: '#cf1322',
                            },
                          },
                          d.criticality,
                        )
                      : null,
                    d.team
                      ? h(
                          'span',
                          {
                            style: {
                              fontSize: '11px',
                              padding: '1px 6px',
                              borderRadius: '3px',
                              background: '#e6f7ff',
                              color: '#096dd9',
                            },
                          },
                          d.team,
                        )
                      : null,
                  ],
                ),
              ),
            ]),
          )
        }
        sections.push(
          sectionBlock(
            '🔗 业务调用链',
            h(
              'div',
              {
                style: {
                  background: '#fff',
                  border: '1px solid var(--border-color)',
                  borderRadius: '6px',
                  padding: '12px',
                },
              },
              callChildren,
            ),
          ),
        )
      }

      if (
        r.business_impact &&
        (r.business_impact.direct_impacts?.length || r.business_impact.indirect_impacts?.length)
      ) {
        const biChildren = []
        if (r.business_impact.risk_level) {
          biChildren.push(
            h(
              'div',
              { style: { marginBottom: '8px' } },
              severityBadge(r.business_impact.risk_level),
            ),
          )
        }
        if (r.business_impact.direct_impacts?.length) {
          biChildren.push(
            h(
              'div',
              {
                style: {
                  fontWeight: 600,
                  fontSize: '12px',
                  color: 'var(--text-primary)',
                  marginBottom: '6px',
                },
              },
              '🔴 直接影响的业务应用',
            ),
          )
          r.business_impact.direct_impacts.forEach((item, idx) => {
            biChildren.push(
              h(
                'div',
                {
                  key: 'di' + idx,
                  style: {
                    background: '#fafafa',
                    border: '1px solid #f0f0f0',
                    borderRadius: '6px',
                    padding: '8px 10px',
                    marginBottom: '6px',
                    fontSize: '13px',
                  },
                },
                [
                  h(
                    'div',
                    {
                      style: { fontWeight: 600, color: 'var(--text-primary)', marginBottom: '2px' },
                    },
                    item.app_name,
                  ),
                  h(
                    'div',
                    { style: { display: 'flex', gap: '6px', flexWrap: 'wrap' } },
                    [
                      item.criticality
                        ? h(
                            'span',
                            {
                              style: {
                                fontSize: '11px',
                                padding: '1px 6px',
                                borderRadius: '3px',
                                background: '#fff1f0',
                                color: '#cf1322',
                              },
                            },
                            item.criticality,
                          )
                        : null,
                      item.team
                        ? h(
                            'span',
                            {
                              style: {
                                fontSize: '11px',
                                padding: '1px 6px',
                                borderRadius: '3px',
                                background: '#e6f7ff',
                                color: '#096dd9',
                              },
                            },
                            item.team,
                          )
                        : null,
                    ].filter(Boolean),
                  ),
                  item.reasoning
                    ? h(
                        'div',
                        { style: { color: '#999', fontSize: '12px', marginTop: '4px' } },
                        item.reasoning,
                      )
                    : null,
                ],
              ),
            )
          })
        }
        if (r.business_impact.indirect_impacts?.length) {
          biChildren.push(
            h(
              'div',
              {
                style: {
                  fontWeight: 600,
                  fontSize: '12px',
                  color: 'var(--text-primary)',
                  marginBottom: '6px',
                  marginTop: '8px',
                },
              },
              '🟡 间接影响的业务应用 (下游)',
            ),
          )
          r.business_impact.indirect_impacts.forEach((item, idx) => {
            biChildren.push(
              h(
                'div',
                {
                  key: 'ii' + idx,
                  style: {
                    background: '#fafafa',
                    border: '1px solid #f0f0f0',
                    borderRadius: '6px',
                    padding: '8px 10px',
                    marginBottom: '6px',
                    fontSize: '13px',
                  },
                },
                [
                  h(
                    'div',
                    {
                      style: { fontWeight: 600, color: 'var(--text-primary)', marginBottom: '2px' },
                    },
                    item.app_name,
                  ),
                  h(
                    'div',
                    { style: { display: 'flex', gap: '6px', flexWrap: 'wrap' } },
                    [
                      item.criticality
                        ? h(
                            'span',
                            {
                              style: {
                                fontSize: '11px',
                                padding: '1px 6px',
                                borderRadius: '3px',
                                background: '#fff1f0',
                                color: '#cf1322',
                              },
                            },
                            item.criticality,
                          )
                        : null,
                      item.team
                        ? h(
                            'span',
                            {
                              style: {
                                fontSize: '11px',
                                padding: '1px 6px',
                                borderRadius: '3px',
                                background: '#e6f7ff',
                                color: '#096dd9',
                              },
                            },
                            item.team,
                          )
                        : null,
                    ].filter(Boolean),
                  ),
                  item.impact_path
                    ? h(
                        'div',
                        { style: { color: '#888', fontSize: '12px', fontFamily: 'monospace' } },
                        item.impact_path,
                      )
                    : null,
                  item.reasoning
                    ? h(
                        'div',
                        { style: { color: '#999', fontSize: '12px', marginTop: '4px' } },
                        item.reasoning,
                      )
                    : null,
                ],
              ),
            )
          })
        }
        sections.push(
          sectionBlock(
            '📊 业务影响分析',
            h(
              'div',
              {
                style: {
                  background: '#fafbfc',
                  border: '1px solid var(--border-color)',
                  borderRadius: '8px',
                  padding: '14px',
                },
              },
              biChildren,
            ),
          ),
        )
      }

      if (r.ai_diagnosis) {
        const ad = r.ai_diagnosis
        const adChildren = []
        adChildren.push(
          h(
            'div',
            {
              style: {
                background: '#fff7e6',
                border: '1px solid #ffd591',
                borderRadius: '8px',
                padding: '14px',
                marginBottom: '12px',
              },
            },
            [
              h(
                'div',
                {
                  style: {
                    fontSize: '14px',
                    color: '#d46b08',
                    lineHeight: '1.6',
                    marginBottom: '8px',
                  },
                },
                ad.summary || ad.root_cause,
              ),
              h(
                'div',
                { style: { fontSize: '12px', color: '#999' } },
                '置信度: ' + Math.round(ad.confidence * 100) + '%',
              ),
            ],
          ),
        )
        if (ad.evidence?.length) {
          adChildren.push(
            h('div', { style: { marginTop: '8px' } }, [
              h(
                'div',
                {
                  style: {
                    fontWeight: 600,
                    fontSize: '12px',
                    color: 'var(--text-secondary)',
                    marginBottom: '6px',
                  },
                },
                '证据链',
              ),
              ...ad.evidence.map((ev, idx) =>
                h(
                  'div',
                  {
                    key: 'ev' + idx,
                    style: { padding: '4px 0', fontSize: '13px', color: 'var(--text-primary)' },
                  },
                  '• ' + ev,
                ),
              ),
            ]),
          )
        }
        if (ad.remediation) {
          adChildren.push(
            h(
              'div',
              {
                style: {
                  marginTop: '8px',
                  background: '#f6ffed',
                  border: '1px solid #b7eb8f',
                  borderRadius: '6px',
                  padding: '10px 14px',
                  fontSize: '13px',
                  color: '#389e0d',
                },
              },
              '建议: ' + ad.remediation,
            ),
          )
        }
        sections.push(sectionBlock('🤖 AI 诊断结论', h('div', null, adChildren)))
      }

      if (r.workload_context) {
        const wc = r.workload_context
        const podItems =
          wc.pods?.map((p) =>
            h(
              'div',
              {
                key: p.name,
                style: {
                  padding: '6px 10px',
                  borderRadius: '4px',
                  fontSize: '13px',
                  background: p.is_alerted ? '#fff1f0' : '#f6ffed',
                  border: p.is_alerted ? '1px solid #ffa39e' : '1px solid #b7eb8f',
                  color: p.is_alerted ? '#cf1322' : '#389e0d',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '6px',
                  marginBottom: '4px',
                },
              },
              [
                h('span', null, p.is_alerted ? '🔴' : '🟢'),
                h('span', { style: { fontWeight: 500 } }, p.name),
                p.is_alerted ? h('span', { style: { fontSize: '11px' } }, '(当前告警)') : null,
              ],
            ),
          ) || []
        sections.push(
          sectionBlock(
            '📦 工作负载上下文',
            h('div', null, [
              h(
                'div',
                { style: { marginBottom: '10px', fontSize: '14px', color: 'var(--text-primary)' } },
                [
                  h(
                    'span',
                    { style: { fontWeight: 600 } },
                    wc.controller_kind + ': ' + wc.controller_name,
                  ),
                  h(
                    'span',
                    { style: { marginLeft: '12px', fontSize: '13px', color: '#666' } },
                    '副本: ' + wc.healthy_pods + '/' + wc.total_pods + ' 健康',
                  ),
                ],
              ),
              h(
                'div',
                { style: { display: 'flex', flexDirection: 'column', gap: '4px' } },
                podItems,
              ),
            ]),
          ),
        )
      }

      if (r.impacted_services && r.impacted_services.length > 0) {
        sections.push(
          sectionBlock(
            '🏢 受影响服务',
            h(
              'div',
              {
                style: { display: 'flex', flexWrap: 'wrap', gap: '8px' },
              },
              r.impacted_services.map((svc, idx) =>
                h(
                  'span',
                  {
                    key: idx,
                    style: {
                      padding: '4px 12px',
                      borderRadius: '4px',
                      background: '#f0f5ff',
                      color: '#2f54eb',
                      fontSize: '13px',
                      border: '1px solid #adc6ff',
                    },
                  },
                  svc,
                ),
              ),
            ),
          ),
        )
      }

      if (r.root_cause) {
        const rcText =
          typeof r.root_cause === 'string'
            ? r.root_cause
            : r.root_cause.final || r.root_cause.ai_generated || ''
        sections.push(
          sectionBlock(
            '📝 诊断结论',
            h(
              'div',
              {
                style: {
                  background: '#fff7e6',
                  border: '1px solid #ffd591',
                  borderRadius: '8px',
                  padding: '16px',
                  color: '#8c4b0a',
                  lineHeight: '1.6',
                  fontSize: '14px',
                  whiteSpace: 'pre-wrap',
                },
              },
              rcText,
            ),
          ),
        )
      }

      if (r.resolution) {
        const resText =
          typeof r.resolution === 'string'
            ? r.resolution
            : r.resolution.final || r.resolution.ai_generated || ''
        sections.push(
          sectionBlock(
            '✅ 解决措施',
            h(
              'div',
              {
                style: {
                  background: '#f6ffed',
                  border: '1px solid #b7eb8f',
                  borderRadius: '8px',
                  padding: '16px',
                  color: '#389e0d',
                  lineHeight: '1.6',
                  fontSize: '14px',
                  whiteSpace: 'pre-wrap',
                },
              },
              resText,
            ),
          ),
        )
      }

      // 经验教训扩展：做得好的 / 可以改进的 / 促成因素
      if (r.what_went_well && r.what_went_well.length > 0) {
        sections.push(
          sectionBlock(
            '✅ 做得好的',
            h(
              'div',
              null,
              r.what_went_well.map((item, idx) =>
                h(
                  'div',
                  {
                    key: 'www' + idx,
                    style: {
                      background: '#f6ffed',
                      border: '1px solid #b7eb8f',
                      borderRadius: '6px',
                      padding: '10px 14px',
                      marginBottom: '6px',
                      fontSize: '13px',
                      color: '#389e0d',
                      lineHeight: '1.5',
                    },
                  },
                  '✓ ' + item,
                ),
              ),
            ),
          ),
        )
      }
      if (r.what_went_wrong && r.what_went_wrong.length > 0) {
        sections.push(
          sectionBlock(
            '❌ 可以改进的',
            h(
              'div',
              null,
              r.what_went_wrong.map((item, idx) =>
                h(
                  'div',
                  {
                    key: 'wwwr' + idx,
                    style: {
                      background: '#fff1f0',
                      border: '1px solid #ffa39e',
                      borderRadius: '6px',
                      padding: '10px 14px',
                      marginBottom: '6px',
                      fontSize: '13px',
                      color: '#cf1322',
                      lineHeight: '1.5',
                    },
                  },
                  '✗ ' + item,
                ),
              ),
            ),
          ),
        )
      }
      if (r.contributing_factors && r.contributing_factors.length > 0) {
        sections.push(
          sectionBlock(
            '🔍 促成因素',
            h(
              'div',
              null,
              r.contributing_factors.map((item, idx) =>
                h(
                  'div',
                  {
                    key: 'cf' + idx,
                    style: {
                      background: '#fff7e6',
                      border: '1px solid #ffd591',
                      borderRadius: '6px',
                      padding: '10px 14px',
                      marginBottom: '6px',
                      fontSize: '13px',
                      color: '#8c4b0a',
                      lineHeight: '1.5',
                    },
                  },
                  '• ' + item,
                ),
              ),
            ),
          ),
        )
      }

      if (r.lessons_learned && r.lessons_learned.length > 0) {
        const lessonsItems = r.lessons_learned.map((lesson, idx) =>
          h(
            'div',
            {
              key: idx,
              style: {
                padding: '8px 0',
                borderBottom: idx < r.lessons_learned.length - 1 ? '1px solid #f0f0f0' : 'none',
                fontSize: '13px',
                color: 'var(--text-primary)',
                lineHeight: '1.6',
              },
            },
            [
              h(
                'span',
                { style: { fontWeight: 600, marginRight: '8px', color: '#1890ff' } },
                idx + 1 + '.',
              ),
              lesson,
            ],
          ),
        )
        sections.push(sectionBlock('💡 经验教训', h('div', null, lessonsItems)))
      }

      if (r.action_items && r.action_items.length > 0) {
        const actionRows = r.action_items.map((item, idx) => actionItemRow(item, idx))
        sections.push(
          sectionBlock(
            '🎯 改进项',
            h(
              'div',
              {
                style: {
                  background: '#fff',
                  border: '1px solid var(--border-color)',
                  borderRadius: '8px',
                  overflow: 'hidden',
                },
              },
              [
                h(
                  'table',
                  { width: '100%', style: { borderCollapse: 'collapse', fontSize: '14px' } },
                  [
                    h('thead', null, [
                      h(
                        'tr',
                        {
                          style: {
                            borderBottom: '2px solid var(--border-color)',
                            background: 'var(--bg-color)',
                          },
                        },
                        [
                          h(
                            'th',
                            {
                              style: {
                                textAlign: 'left',
                                padding: '10px 12px',
                                fontWeight: 600,
                                fontSize: '12px',
                                color: 'var(--text-secondary)',
                              },
                            },
                            '改进项',
                          ),
                          h(
                            'th',
                            {
                              style: {
                                textAlign: 'left',
                                padding: '10px 12px',
                                fontWeight: 600,
                                fontSize: '12px',
                                color: 'var(--text-secondary)',
                                width: '80px',
                              },
                            },
                            '优先级',
                          ),
                          h(
                            'th',
                            {
                              style: {
                                textAlign: 'left',
                                padding: '10px 12px',
                                fontWeight: 600,
                                fontSize: '12px',
                                color: 'var(--text-secondary)',
                                width: '120px',
                              },
                            },
                            '负责人',
                          ),
                          h(
                            'th',
                            {
                              style: {
                                textAlign: 'left',
                                padding: '10px 12px',
                                fontWeight: 600,
                                fontSize: '12px',
                                color: 'var(--text-secondary)',
                                width: '120px',
                              },
                            },
                            '截止日期',
                          ),
                        ],
                      ),
                    ]),
                    h('tbody', null, actionRows),
                  ],
                ),
              ],
            ),
          ),
        )
      }

      if (r.timeline && r.timeline.length > 0) {
        sections.push(
          sectionBlock(
            '📅 报告时间线',
            h(
              'div',
              {
                style: {
                  background: '#fafbfc',
                  border: '1px solid var(--border-color)',
                  borderRadius: '8px',
                  padding: '20px',
                },
              },
              r.timeline.map((ev, idx) => timelineItem(ev, idx, idx === r.timeline.length - 1)),
            ),
          ),
        )
      }

      if (r.impact_assessment) {
        const ia = r.impact_assessment
        const iaChildren = []
        iaChildren.push(
          h(
            'div',
            {
              style: {
                display: 'grid',
                gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
                gap: '12px',
                marginBottom: '16px',
              },
            },
            [
              infoCard('严重级别', ia.severity ? severityBadge(ia.severity, ia.severity) : '-'),
              infoCard('影响半径', ia.blast_radius != null ? ia.blast_radius + ' 跳' : '-'),
              infoCard('直接受影响资源', ia.direct_impact ? ia.direct_impact.length : '0'),
              infoCard('间接影响资源', ia.indirect_impact ? ia.indirect_impact.length : '0'),
            ],
          ),
        )
        if (ia.user_facing_impact) {
          iaChildren.push(
            h(
              'div',
              {
                style: {
                  background: '#fff1f0',
                  border: '1px solid #ffa39e',
                  borderRadius: '6px',
                  padding: '8px 14px',
                  marginBottom: '12px',
                  fontSize: '13px',
                  color: '#cf1322',
                  fontWeight: 600,
                },
              },
              '⚠️ 用户面受影响',
            ),
          )
        }
        if (ia.affected_services && ia.affected_services.length > 0) {
          iaChildren.push(
            h('div', { style: { marginBottom: '12px' } }, [
              h(
                'div',
                {
                  style: {
                    fontSize: '13px',
                    fontWeight: 600,
                    color: 'var(--text-primary)',
                    marginBottom: '6px',
                  },
                },
                '受影响 K8s 服务 (' + ia.affected_services.length + ')',
              ),
              h(
                'div',
                { style: { display: 'flex', flexWrap: 'wrap', gap: '6px' } },
                ia.affected_services.map((s, i) =>
                  h(
                    'span',
                    {
                      key: 'as' + i,
                      style: {
                        padding: '2px 10px',
                        borderRadius: '4px',
                        background: '#f0f5ff',
                        color: '#2f54eb',
                        fontSize: '12px',
                        border: '1px solid #adc6ff',
                      },
                    },
                    s,
                  ),
                ),
              ),
            ]),
          )
        }
        if (ia.direct_impact && ia.direct_impact.length > 0) {
          const diRows = []
          let diLastKind = ''
          ia.direct_impact.forEach((item, idx) => {
            if (item.kind && item.kind !== diLastKind) {
              diLastKind = item.kind
              diRows.push(
                h('tr', { key: 'dih' + idx, style: { background: '#fafafa' } }, [
                  h(
                    'td',
                    {
                      colspan: 3,
                      style: {
                        padding: '6px 12px',
                        fontSize: '12px',
                        fontWeight: 600,
                        color: '#666',
                      },
                    },
                    item.kind,
                  ),
                ]),
              )
            }
            diRows.push(
              h('tr', { key: 'di' + idx, style: { borderBottom: '1px solid #f0f0f0' } }, [
                h('td', { style: { padding: '6px 12px', fontSize: '13px' } }, item.kind || '-'),
                h(
                  'td',
                  { style: { padding: '6px 12px', fontSize: '13px', fontWeight: 500 } },
                  item.name || '-',
                ),
                h(
                  'td',
                  { style: { padding: '6px 12px', fontSize: '13px', color: '#666' } },
                  item.namespace || '-',
                ),
              ]),
            )
          })
          iaChildren.push(
            h('div', { style: { marginBottom: '16px' } }, [
              h(
                'div',
                {
                  style: {
                    fontSize: '13px',
                    fontWeight: 600,
                    color: 'var(--text-primary)',
                    marginBottom: '8px',
                  },
                },
                '🔴 直接影响资源 (' + ia.direct_impact.length + ')',
              ),
              h(
                'div',
                {
                  style: {
                    background: '#fff',
                    border: '1px solid var(--border-color)',
                    borderRadius: '6px',
                    overflow: 'hidden',
                  },
                },
                [
                  h(
                    'table',
                    { width: '100%', style: { borderCollapse: 'collapse', fontSize: '13px' } },
                    [
                      h('thead', null, [
                        h('tr', { style: { borderBottom: '2px solid var(--border-color)' } }, [
                          h(
                            'th',
                            {
                              style: {
                                textAlign: 'left',
                                padding: '8px 12px',
                                fontSize: '12px',
                                color: 'var(--text-secondary)',
                              },
                            },
                            '类型',
                          ),
                          h(
                            'th',
                            {
                              style: {
                                textAlign: 'left',
                                padding: '8px 12px',
                                fontSize: '12px',
                                color: 'var(--text-secondary)',
                              },
                            },
                            '名称',
                          ),
                          h(
                            'th',
                            {
                              style: {
                                textAlign: 'left',
                                padding: '8px 12px',
                                fontSize: '12px',
                                color: 'var(--text-secondary)',
                              },
                            },
                            '命名空间',
                          ),
                        ]),
                      ]),
                      h('tbody', null, diRows),
                    ],
                  ),
                ],
              ),
            ]),
          )
        }
        if (ia.indirect_impact && ia.indirect_impact.length > 0) {
          const iiRows = []
          let iiLastKind = ''
          ia.indirect_impact.forEach((item, idx) => {
            if (item.kind && item.kind !== iiLastKind) {
              iiLastKind = item.kind
              iiRows.push(
                h('tr', { key: 'iih' + idx, style: { background: '#fafafa' } }, [
                  h(
                    'td',
                    {
                      colspan: 3,
                      style: {
                        padding: '6px 12px',
                        fontSize: '12px',
                        fontWeight: 600,
                        color: '#666',
                      },
                    },
                    item.kind,
                  ),
                ]),
              )
            }
            iiRows.push(
              h('tr', { key: 'ii' + idx, style: { borderBottom: '1px solid #f0f0f0' } }, [
                h('td', { style: { padding: '6px 12px', fontSize: '13px' } }, item.kind || '-'),
                h(
                  'td',
                  { style: { padding: '6px 12px', fontSize: '13px', fontWeight: 500 } },
                  item.name || '-',
                ),
                h(
                  'td',
                  { style: { padding: '6px 12px', fontSize: '13px', color: '#666' } },
                  item.namespace || '-',
                ),
              ]),
            )
          })
          iaChildren.push(
            h('div', null, [
              h(
                'div',
                {
                  style: {
                    fontSize: '13px',
                    fontWeight: 600,
                    color: 'var(--text-primary)',
                    marginBottom: '8px',
                  },
                },
                '🟡 间接影响资源 (' + ia.indirect_impact.length + ')',
              ),
              h(
                'div',
                {
                  style: {
                    background: '#fff',
                    border: '1px solid var(--border-color)',
                    borderRadius: '6px',
                    overflow: 'hidden',
                  },
                },
                [
                  h(
                    'table',
                    { width: '100%', style: { borderCollapse: 'collapse', fontSize: '13px' } },
                    [
                      h('thead', null, [
                        h('tr', { style: { borderBottom: '2px solid var(--border-color)' } }, [
                          h(
                            'th',
                            {
                              style: {
                                textAlign: 'left',
                                padding: '8px 12px',
                                fontSize: '12px',
                                color: 'var(--text-secondary)',
                              },
                            },
                            '类型',
                          ),
                          h(
                            'th',
                            {
                              style: {
                                textAlign: 'left',
                                padding: '8px 12px',
                                fontSize: '12px',
                                color: 'var(--text-secondary)',
                              },
                            },
                            '名称',
                          ),
                          h(
                            'th',
                            {
                              style: {
                                textAlign: 'left',
                                padding: '8px 12px',
                                fontSize: '12px',
                                color: 'var(--text-secondary)',
                              },
                            },
                            '命名空间',
                          ),
                        ]),
                      ]),
                      h('tbody', null, iiRows),
                    ],
                  ),
                ],
              ),
            ]),
          )
        }
        sections.push(sectionBlock('📊 影响评估详情', h('div', null, iaChildren)))
      }

      if (r.diagnosis_metrics && r.diagnosis_metrics.length > 0) {
        const metrics = r.diagnosis_metrics
        const maxVal = Math.max(...metrics.map((m) => m.value || 0), 1)
        const barColors = { critical: '#cf1322', warning: '#faad14', normal: '#52c41a' }
        const barItems = metrics.map((m, idx) => {
          const val = m.value || 0
          const pct = Math.round((val / maxVal) * 100)
          return h(
            'div',
            {
              key: 'metric' + idx,
              style: {
                display: 'flex',
                alignItems: 'center',
                gap: '10px',
                marginBottom: '8px',
                fontSize: '13px',
              },
            },
            [
              h(
                'span',
                {
                  style: {
                    minWidth: '120px',
                    color: 'var(--text-primary)',
                    whiteSpace: 'nowrap',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                  },
                },
                (m.metricName || '') + ' / ' + (m.resourceName || '-'),
              ),
              h(
                'span',
                {
                  style: {
                    minWidth: '60px',
                    textAlign: 'right',
                    fontWeight: 500,
                    color: 'var(--text-primary)',
                    fontFamily: 'monospace',
                  },
                },
                typeof val === 'number' ? val.toFixed(1) : val,
              ),
              h(
                'div',
                {
                  style: {
                    flex: 1,
                    height: '20px',
                    background: '#f0f0f0',
                    borderRadius: '4px',
                    overflow: 'hidden',
                    position: 'relative',
                  },
                },
                [
                  h('div', {
                    style: {
                      height: '100%',
                      width: pct + '%',
                      background: barColors[m.status] || '#52c41a',
                      borderRadius: '4px',
                      minWidth: pct > 0 ? '4px' : '0',
                      transition: 'width 0.4s ease',
                    },
                  }),
                ],
              ),
              h(
                'span',
                {
                  style: {
                    minWidth: '52px',
                    fontSize: '11px',
                    color: barColors[m.status] || '#666',
                    fontWeight: 500,
                  },
                },
                (m.status || 'normal').toUpperCase(),
              ),
            ],
          )
        })
        sections.push(
          sectionBlock(
            '📈 指标快照',
            h(
              'div',
              {
                style: {
                  background: '#fafbfc',
                  border: '1px solid var(--border-color)',
                  borderRadius: '8px',
                  padding: '14px',
                },
              },
              barItems,
            ),
          ),
        )
      }

      if (
        r.business_calls &&
        (r.business_calls.upstreams?.length || r.business_calls.downstreams?.length)
      ) {
        const topoChildren = []
        const isTopoFull = this.fullscreenGraph === 'topo'
        topoChildren.push(
          h('div', {
            ref: 'topoGraphContainer',
            style: isTopoFull
              ? {
                  position: 'fixed',
                  top: 0,
                  left: 0,
                  width: '100vw',
                  height: '100vh',
                  zIndex: 9999,
                  background: '#fff',
                }
              : {
                  width: '100%',
                  height: '400px',
                  border: '1px solid var(--border-color)',
                  borderRadius: '6px',
                  background: '#fafbfc',
                  position: 'relative',
                },
          }),
        )
        topoChildren.push(
          h(
            'div',
            {
              style: isTopoFull
                ? {
                    position: 'fixed',
                    top: '12px',
                    right: '12px',
                    zIndex: 10001,
                    display: 'flex',
                    gap: '8px',
                    fontSize: '11px',
                    color: '#666',
                    background: 'rgba(255,255,255,0.95)',
                    padding: '6px 12px',
                    borderRadius: '6px',
                    border: '1px solid #e8e8e8',
                  }
                : {
                    position: 'absolute',
                    top: '8px',
                    right: '12px',
                    display: 'flex',
                    gap: '12px',
                    fontSize: '11px',
                    color: '#666',
                    background: 'rgba(255,255,255,0.9)',
                    padding: '4px 10px',
                    borderRadius: '4px',
                    border: '1px solid #e8e8e8',
                    zIndex: 10,
                  },
            },
            [
              h('span', { style: { display: 'flex', alignItems: 'center', gap: '4px' } }, [
                h('span', {
                  style: {
                    width: '10px',
                    height: '10px',
                    borderRadius: '50%',
                    background: '#cf1322',
                    display: 'inline-block',
                  },
                }),
                '故障业务',
              ]),
              h('span', { style: { display: 'flex', alignItems: 'center', gap: '4px' } }, [
                h('span', {
                  style: {
                    width: '10px',
                    height: '10px',
                    borderRadius: '50%',
                    background: '#fa8c16',
                    display: 'inline-block',
                  },
                }),
                '上游调用方',
              ]),
              h('span', { style: { display: 'flex', alignItems: 'center', gap: '4px' } }, [
                h('span', {
                  style: {
                    width: '10px',
                    height: '10px',
                    borderRadius: '50%',
                    background: '#1890ff',
                    display: 'inline-block',
                  },
                }),
                '下游依赖方',
              ]),
              h(
                'span',
                {
                  style: {
                    cursor: 'pointer',
                    fontWeight: 600,
                    color: '#1890ff',
                    userSelect: 'none',
                  },
                  onClick: () => this.toggleFullscreen('topo'),
                },
                isTopoFull ? '⧉ 退出全屏' : '⛶ 全屏',
              ),
            ],
          ),
        )
        sections.push(
          sectionBlock('🔗 业务拓扑', h('div', { style: { position: 'relative' } }, topoChildren)),
        )
      } else {
        sections.push(
          sectionBlock(
            '🔗 业务拓扑',
            h(
              'div',
              {
                style: {
                  textAlign: 'center',
                  padding: '40px',
                  color: 'var(--text-secondary)',
                  fontSize: '13px',
                },
              },
              '暂无业务调用链数据',
            ),
          ),
        )
      }

      if (r.causal_chain && r.causal_chain.links && r.causal_chain.links.length > 0) {
        const isCausalFull = this.fullscreenGraph === 'causal'
        const causalChildren = [
          h('div', {
            ref: 'causalGraphContainer',
            style: isCausalFull
              ? {
                  position: 'fixed',
                  top: 0,
                  left: 0,
                  width: '100vw',
                  height: '100vh',
                  zIndex: 9999,
                  background: '#fff',
                }
              : {
                  width: '100%',
                  height: '300px',
                  border: '1px solid var(--border-color)',
                  borderRadius: '6px',
                  background: '#fafbfc',
                },
          }),
          h(
            'div',
            {
              style: isCausalFull
                ? {
                    position: 'fixed',
                    top: '12px',
                    right: '12px',
                    zIndex: 10001,
                    fontSize: '11px',
                    color: '#666',
                    background: 'rgba(255,255,255,0.95)',
                    padding: '6px 12px',
                    borderRadius: '6px',
                    border: '1px solid #e8e8e8',
                  }
                : {
                    position: 'absolute',
                    top: '8px',
                    right: '12px',
                    fontSize: '11px',
                    color: '#666',
                    background: 'rgba(255,255,255,0.9)',
                    padding: '4px 10px',
                    borderRadius: '4px',
                    border: '1px solid #e8e8e8',
                    zIndex: 10,
                  },
            },
            [
              h(
                'span',
                {
                  style: {
                    cursor: 'pointer',
                    fontWeight: 600,
                    color: '#1890ff',
                    userSelect: 'none',
                  },
                  onClick: () => this.toggleFullscreen('causal'),
                },
                isCausalFull ? '⧉ 退出全屏' : '⛶ 全屏',
              ),
            ],
          ),
        ]
        sections.push(
          sectionBlock(
            '🔗 因果链 DAG',
            h('div', { style: { position: 'relative' } }, causalChildren),
          ),
        )
      } else {
        sections.push(
          sectionBlock(
            '🔗 因果链 DAG',
            h(
              'div',
              {
                style: {
                  textAlign: 'center',
                  padding: '40px',
                  color: 'var(--text-secondary)',
                  fontSize: '13px',
                },
              },
              '暂无因果链数据',
            ),
          ),
        )
      }

      if (r.diagnosis_logs && r.diagnosis_logs.length > 0) {
        const logs = r.diagnosis_logs
        const showAll = this.logShowAll
        const displayLogs = showAll ? logs : logs.slice(0, 10)
        const logItems = displayLogs.map((log, idx) => {
          const sev = (log.level || 'INFO').toUpperCase()
          const sevColor =
            { ERROR: '#cf1322', WARN: '#faad14', INFO: '#1890ff', DEBUG: '#666' }[sev] || '#666'
          const sevBg =
            { ERROR: '#fff1f0', WARN: '#fff7e6', INFO: '#e6f7ff', DEBUG: '#f5f5f5' }[sev] ||
            '#f5f5f5'
          return h(
            'div',
            {
              key: 'log' + idx,
              style: {
                background: '#fff',
                border: '1px solid var(--border-color)',
                borderRadius: '6px',
                marginBottom: '8px',
                overflow: 'hidden',
              },
            },
            [
              h(
                'div',
                {
                  style: {
                    display: 'flex',
                    alignItems: 'center',
                    gap: '10px',
                    padding: '8px 14px',
                    borderBottom: '1px solid #f0f0f0',
                    background: '#fafafa',
                  },
                },
                [
                  h(
                    'span',
                    {
                      style: {
                        fontSize: '12px',
                        color: '#999',
                        fontFamily: 'monospace',
                        minWidth: '130px',
                      },
                    },
                    formatTime(log.timestamp),
                  ),
                  h(
                    'span',
                    {
                      style: {
                        display: 'inline-block',
                        padding: '1px 8px',
                        borderRadius: '3px',
                        fontSize: '11px',
                        fontWeight: 600,
                        color: sevColor,
                        background: sevBg,
                      },
                    },
                    sev,
                  ),
                  h(
                    'span',
                    {
                      style: {
                        fontSize: '11px',
                        padding: '1px 8px',
                        borderRadius: '3px',
                        background: '#f0f0f0',
                        color: '#666',
                      },
                    },
                    (log.pod || '') + (log.container ? '/' + log.container : ''),
                  ),
                  log.namespace
                    ? h('span', { style: { fontSize: '11px', color: '#999' } }, log.namespace)
                    : null,
                ].filter(Boolean),
              ),
              h(
                'div',
                {
                  style: {
                    padding: '10px 14px',
                    background: '#1e1e1e',
                    color: '#d4d4d4',
                    fontFamily: 'monospace',
                    fontSize: '12px',
                    lineHeight: '1.6',
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-all',
                    maxHeight: '200px',
                    overflowY: 'auto',
                  },
                },
                log.message || '-',
              ),
            ],
          )
        })
        const logChildren = [h('div', null, logItems)]
        if (logs.length > 10) {
          logChildren.push(
            h(
              'button',
              {
                onClick: () => {
                  this.logShowAll = !this.logShowAll
                },
                style: {
                  display: 'block',
                  margin: '10px auto 0',
                  padding: '6px 20px',
                  background: 'none',
                  color: 'var(--primary-color)',
                  border: '1px solid var(--primary-color)',
                  borderRadius: '6px',
                  cursor: 'pointer',
                  fontSize: '13px',
                  fontWeight: 500,
                },
              },
              showAll ? '收起' : '展开全部 ' + logs.length + ' 条',
            ),
          )
        }
        sections.push(
          sectionBlock('📜 关键日志 (' + logs.length + ' 条)', h('div', null, logChildren)),
        )
      }

      if (r.action_items && r.action_items.length > 0) {
        const categories = {
          prevent: { label: '🛡️ 预防 (Prevent)', color: '#1890ff', bg: '#e6f7ff', items: [] },
          detect: { label: '🔍 检测 (Detect)', color: '#52c41a', bg: '#f6ffed', items: [] },
          mitigate: { label: '🛟 缓解 (Mitigate)', color: '#fa8c16', bg: '#fff7e6', items: [] },
          other: { label: '📋 其他', color: '#666', bg: '#fafafa', items: [] },
        }
        r.action_items.forEach((item) => {
          const cat = item.category || 'other'
          if (categories[cat]) categories[cat].items.push(item)
          else categories.other.items.push(item)
        })
        const catKeys = ['prevent', 'detect', 'mitigate', 'other'].filter(
          (k) => categories[k].items.length > 0,
        )
        if (catKeys.length > 0) {
          const priorityColors = { high: '#ff4d4f', medium: '#faad14', low: '#52c41a' }
          const kanbanCols = catKeys.map((catKey) => {
            const cat = categories[catKey]
            return h(
              'div',
              {
                key: catKey,
                style: {
                  background: cat.bg,
                  border: '1px solid ' + cat.color + '30',
                  borderRadius: '8px',
                  padding: '12px',
                  minWidth: '220px',
                },
              },
              [
                h(
                  'div',
                  {
                    style: {
                      fontSize: '13px',
                      fontWeight: 600,
                      color: cat.color,
                      marginBottom: '10px',
                      paddingBottom: '8px',
                      borderBottom: '1px solid ' + cat.color + '30',
                    },
                  },
                  cat.label + ' (' + cat.items.length + ')',
                ),
                h(
                  'div',
                  { style: { display: 'flex', flexDirection: 'column', gap: '8px' } },
                  cat.items.map((item, idx) =>
                    h(
                      'div',
                      {
                        key: 'ai' + idx,
                        style: {
                          background: '#fff',
                          border: '1px solid var(--border-color)',
                          borderRadius: '6px',
                          padding: '10px 12px',
                          fontSize: '13px',
                        },
                      },
                      [
                        h(
                          'div',
                          {
                            style: {
                              fontWeight: 600,
                              color: 'var(--text-primary)',
                              marginBottom: '6px',
                              lineHeight: '1.4',
                            },
                          },
                          item.description || '-',
                        ),
                        h(
                          'div',
                          {
                            style: {
                              display: 'flex',
                              alignItems: 'center',
                              gap: '8px',
                              flexWrap: 'wrap',
                              fontSize: '12px',
                            },
                          },
                          [
                            item.owner
                              ? h(
                                  'span',
                                  {
                                    style: {
                                      padding: '1px 8px',
                                      borderRadius: '3px',
                                      background: '#f0f0f0',
                                      color: '#666',
                                    },
                                  },
                                  item.owner,
                                )
                              : null,
                            item.priority
                              ? severityBadge(
                                  item.priority,
                                  { high: '高', medium: '中', low: '低' }[
                                    item.priority.toLowerCase()
                                  ] || item.priority,
                                )
                              : null,
                            item.due_date
                              ? h('span', { style: { color: '#999' } }, '截止: ' + item.due_date)
                              : null,
                            item.category
                              ? h(
                                  'span',
                                  {
                                    style: {
                                      fontSize: '10px',
                                      padding: '1px 6px',
                                      borderRadius: '3px',
                                      background: '#f5f5f5',
                                      color: '#999',
                                    },
                                  },
                                  item.category,
                                )
                              : null,
                          ].filter(Boolean),
                        ),
                        item.exit_criteria
                          ? h(
                              'div',
                              { style: { fontSize: '11px', color: '#999', marginTop: '4px' } },
                              '退出条件: ' + item.exit_criteria,
                            )
                          : null,
                      ],
                    ),
                  ),
                ),
              ],
            )
          })
          sections.push(
            sectionBlock(
              '🎯 改进项看板',
              h(
                'div',
                {
                  style: {
                    display: 'grid',
                    gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))',
                    gap: '12px',
                  },
                },
                kanbanCols,
              ),
            ),
          )
        }
      } else {
        sections.push(
          sectionBlock(
            '🎯 改进项看板',
            h(
              'div',
              {
                style: {
                  textAlign: 'center',
                  padding: '40px',
                  color: 'var(--text-secondary)',
                  fontSize: '13px',
                },
              },
              '暂无改进项',
            ),
          ),
        )
      }

      this.$nextTick(() => {
        if (
          this.postmortem &&
          this.postmortem.business_calls &&
          (this.postmortem.business_calls.upstreams?.length ||
            this.postmortem.business_calls.downstreams?.length)
        ) {
          this._initTopoGraph()
        }
        if (
          this.postmortem &&
          this.postmortem.causal_chain &&
          this.postmortem.causal_chain.links &&
          this.postmortem.causal_chain.links.length > 0
        ) {
          this._initCausalGraph()
        }
      })

      return h(
        'div',
        {
          style: {
            background: '#fff',
            border: '1px solid var(--border-color)',
            borderRadius: '12px',
            padding: '24px',
            marginTop: '12px',
          },
        },
        sections,
      )
    },
  },
  render() {
    return h('div', { class: 'retrospective-page' }, [
      h(NavBar),
      h('div', { class: 'container' }, [
        h('div', { class: 'dashboard-header' }, '复盘分析'),

        // 指纹输入区
        h('div', { class: 'card', style: { padding: '20px', marginBottom: '20px' } }, [
          h(
            'div',
            { style: { display: 'flex', gap: '12px', alignItems: 'stretch', flexWrap: 'wrap' } },
            [
              h('input', {
                type: 'text',
                placeholder: '输入告警指纹 (fingerprint) 开始分析...',
                value: this.fingerprint,
                onInput: (e) => {
                  this.fingerprint = e.target.value
                },
                onKeydown: (e) => {
                  if (e.key === 'Enter') this.loadAnalysis()
                },
                style: {
                  flex: 1,
                  minWidth: '300px',
                  padding: '10px 16px',
                  border: '1px solid var(--border-color)',
                  borderRadius: '6px',
                  fontSize: '14px',
                  outline: 'none',
                  background: '#fff',
                  transition: 'border-color 0.2s',
                },
              }),
              h(
                'button',
                {
                  onClick: this.loadAnalysis,
                  disabled: this.loading,
                  style: {
                    padding: '10px 24px',
                    background: 'var(--primary-color)',
                    color: '#fff',
                    border: 'none',
                    borderRadius: '6px',
                    cursor: this.loading ? 'not-allowed' : 'pointer',
                    fontSize: '14px',
                    fontWeight: 500,
                    opacity: this.loading ? 0.6 : 1,
                    transition: 'all 0.2s',
                    whiteSpace: 'nowrap',
                  },
                  onMouseenter: (e) => {
                    if (!this.loading) e.currentTarget.style.background = '#40a9ff'
                  },
                  onMouseleave: (e) => {
                    e.currentTarget.style.background = 'var(--primary-color)'
                  },
                },
                this.loading ? '分析中...' : '开始分析',
              ),
              h(
                'button',
                {
                  onClick: this.generatePostmortem,
                  disabled: this.generating || !this.fingerprint.trim(),
                  style: {
                    padding: '10px 24px',
                    background: '#722ed1',
                    color: '#fff',
                    border: 'none',
                    borderRadius: '6px',
                    cursor: this.generating || !this.fingerprint.trim() ? 'not-allowed' : 'pointer',
                    fontSize: '14px',
                    fontWeight: 500,
                    opacity: this.generating || !this.fingerprint.trim() ? 0.6 : 1,
                    transition: 'all 0.2s',
                    whiteSpace: 'nowrap',
                  },
                  onMouseenter: (e) => {
                    if (!this.generating && this.fingerprint.trim())
                      e.currentTarget.style.background = '#9254de'
                  },
                  onMouseleave: (e) => {
                    e.currentTarget.style.background = '#722ed1'
                  },
                },
                this.generating ? '生成中...' : '生成复盘报告',
              ),
              h(
                'button',
                {
                  onClick: this.exportMarkdown,
                  disabled: !this.fingerprint.trim(),
                  style: {
                    padding: '10px 24px',
                    background: 'none',
                    color: 'var(--primary-color)',
                    border: '1px solid var(--primary-color)',
                    borderRadius: '6px',
                    cursor: !this.fingerprint.trim() ? 'not-allowed' : 'pointer',
                    fontSize: '14px',
                    fontWeight: 500,
                    opacity: !this.fingerprint.trim() ? 0.6 : 1,
                    transition: 'all 0.2s',
                    whiteSpace: 'nowrap',
                  },
                  onMouseenter: (e) => {
                    if (this.fingerprint.trim()) {
                      e.currentTarget.style.background = 'var(--primary-color)'
                      e.currentTarget.style.color = '#fff'
                    }
                  },
                  onMouseleave: (e) => {
                    e.currentTarget.style.background = 'none'
                    e.currentTarget.style.color = 'var(--primary-color)'
                  },
                },
                '导出 Markdown',
              ),
            ],
          ),
        ]),

        // 错误提示
        this.error
          ? h(
              'div',
              {
                class: 'card',
                style: {
                  padding: '16px 20px',
                  marginBottom: '20px',
                  background: '#fff2f0',
                  border: '1px solid #ffccc7',
                  borderRadius: '8px',
                  color: 'var(--error-color)',
                  fontSize: '14px',
                },
              },
              [h('span', { style: { fontWeight: 600, marginRight: '8px' } }, '⚠️'), this.error],
            )
          : null,

        // 加载状态
        this.loading
          ? h(
              'div',
              {
                class: 'card',
                style: { textAlign: 'center', padding: '40px', color: 'var(--text-secondary)' },
              },
              [
                h('div', { style: { fontSize: '28px', marginBottom: '8px' } }, '⏳'),
                h('div', { style: { fontSize: '14px' } }, '正在加载分析数据...'),
              ],
            )
          : null,

        // 分析结果
        !this.loading && this.fingerprint && (this.timeline.length > 0 || this.causalChain)
          ? h('div', null, [
              // 事件时间线
              this.timeline.length > 0
                ? sectionBlock(
                    '📅 事件时间线 (' + this.timeline.length + ' 个事件)',
                    h(
                      'div',
                      {
                        style: {
                          background: '#fafbfc',
                          border: '1px solid var(--border-color)',
                          borderRadius: '8px',
                          padding: '20px',
                        },
                      },
                      this.timeline.map((ev, idx) =>
                        timelineItem(ev, idx, idx === this.timeline.length - 1),
                      ),
                    ),
                  )
                : null,

              // 因果链分析
              this.causalChain
                ? sectionBlock(
                    '🔗 因果链分析',
                    h('div', null, [
                      this.causalChain.root_cause
                        ? h(
                            'div',
                            {
                              style: {
                                background: '#fff7e6',
                                border: '1px solid #ffd591',
                                borderRadius: '8px',
                                padding: '14px',
                                marginBottom: '16px',
                              },
                            },
                            [
                              h(
                                'div',
                                {
                                  style: {
                                    fontSize: '13px',
                                    fontWeight: 600,
                                    color: '#d46b08',
                                    marginBottom: '6px',
                                  },
                                },
                                '根因',
                              ),
                              h(
                                'div',
                                {
                                  style: { fontSize: '14px', color: '#8c4b0a', lineHeight: '1.6' },
                                },
                                this.causalChain.root_cause,
                              ),
                            ],
                          )
                        : null,
                      this.causalChain.links && this.causalChain.links.length > 0
                        ? h(
                            'div',
                            null,
                            this.causalChain.links.map((link, idx) => causalLinkItem(link, idx)),
                          )
                        : null,
                      this.causalChain.impact
                        ? h(
                            'div',
                            {
                              style: {
                                marginTop: '12px',
                                background: '#fff',
                                border: '1px solid var(--border-color)',
                                borderRadius: '8px',
                                padding: '14px',
                              },
                            },
                            [
                              h(
                                'div',
                                {
                                  style: {
                                    fontSize: '13px',
                                    fontWeight: 600,
                                    color: 'var(--text-primary)',
                                    marginBottom: '8px',
                                  },
                                },
                                '影响范围',
                              ),
                              h(
                                'div',
                                {
                                  style: {
                                    fontSize: '13px',
                                    color: 'var(--text-primary)',
                                    lineHeight: '1.6',
                                  },
                                },
                                this.causalChain.impact,
                              ),
                            ],
                          )
                        : null,
                    ]),
                  )
                : null,
            ])
          : null,

        // Postmortem 报告
        this.generating
          ? h(
              'div',
              {
                class: 'card',
                style: { textAlign: 'center', padding: '40px', color: 'var(--text-secondary)' },
              },
              [
                h('div', { style: { fontSize: '28px', marginBottom: '8px' } }, '📝'),
                h('div', { style: { fontSize: '14px' } }, '正在生成复盘报告...'),
              ],
            )
          : null,

        this.postmortem ? sectionBlock('📋 复盘报告', this.renderPostmortemReport()) : null,
      ]),
    ])
  },
}).mount('#app')
