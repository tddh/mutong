import { createApp, ref, h, onMounted, onUnmounted, getCurrentInstance } from 'vue'
import { NavBar } from '../components/SharedComponents.js'
import { StatusBadge } from '../components/primitives.js'
import { API } from '../utils/api.js'
import { onThemeChange } from '../styles/theme.js'
import { renderTopoGraph, renderCausalGraph, destroyGraph } from '../utils/topo-graph.js'
import '../styles/common.css'

/* 严重度/优先级 → 语义档位，颜色由组件层 data-sev 决定，两主题自动适配 */
const SEV_TONES = {
  critical: 'critical',
  error: 'error',
  high: 'critical',
  warning: 'warning',
  medium: 'warning',
  info: 'info',
  low: 'pass',
  pass: 'pass',
}

function formatTime(ts) {
  if (!ts) return '-'
  return new Date(ts).toLocaleString('zh-CN')
}

function severityBadge(severity, text) {
  const s = SEV_TONES[(severity || '').toLowerCase()] || 'pending'
  return h(StatusBadge, { sev: s, text: text || severity || '—' })
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
        background: 'var(--surface-card)',
        border: '1px solid var(--border-default)',
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
  const tone = SEV_TONES[severity.toLowerCase()] || 'info'
  const timeColor = `var(--sev-${tone}-fg)`

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
              border: '2px solid var(--surface-card)',
              boxShadow: '0 0 0 2px ' + timeColor,
              flexShrink: 0,
            },
          }),
          !isLast
            ? h('div', {
                style: {
                  flex: 1,
                  width: '2px',
                  background: 'var(--border-default)',
                  marginTop: '4px',
                },
              })
            : null,
        ],
      ),
      h(
        'div',
        {
          style: {
            flex: 1,
            background: 'var(--surface-card)',
            border: '1px solid var(--border-default)',
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
                        background: 'var(--st-neutral-bg)',
                        color: 'var(--st-neutral-fg)',
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
                    background: 'var(--surface-inset)',
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
  const confTone =
    link.confidence >= 0.8 ? 'pass' : link.confidence >= 0.5 ? 'warning' : 'critical'

  return h(
    'div',
    {
      style: {
        background: 'var(--surface-card)',
        border: '1px solid var(--border-default)',
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
            { style: { fontSize: '14px', fontWeight: 600, color: 'var(--st-danger-fg)' } },
            link.cause || '-',
          ),
          h('span', { style: { fontSize: '16px', color: 'var(--text-muted)' } }, '→'),
          h(
            'span',
            { style: { fontSize: '14px', fontWeight: 600, color: 'var(--accent)' } },
            link.effect || '-',
          ),
          h(
            'span',
            {
              style: {
                fontSize: '11px',
                padding: '1px 8px',
                borderRadius: '10px',
                background: `var(--sev-${confTone}-bg)`,
                color: `var(--sev-${confTone}-fg)`,
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
                background: 'var(--surface-inset)',
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
  const priorityLabels = { high: '高', medium: '中', low: '低' }
  const priority = (item.priority || 'medium').toLowerCase()

  return h('tr', { style: { borderBottom: '1px solid var(--border-subtle)' } }, [
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

    // 主题切换后按新 token 重建 G6（canvas 吃不了 CSS 变量，只能重绘）
    const instance = getCurrentInstance()
    const offTheme = onThemeChange(() => {
      const proxy = instance && instance.proxy
      if (proxy && proxy.postmortem) proxy.reinitGraphs()
    })

    onMounted(() => {
      document.addEventListener('keydown', handleKeydown)
    })

    onUnmounted(() => {
      document.removeEventListener('keydown', handleKeydown)
      offTheme()
      destroyGraph(graphStore._topoGraphInstance)
      graphStore._topoGraphInstance = null
      destroyGraph(graphStore._causalGraphInstance)
      graphStore._causalGraphInstance = null
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
      destroyGraph(graphStore._topoGraphInstance)
      graphStore._topoGraphInstance = null
    },
    _destroyCausalGraph() {
      destroyGraph(graphStore._causalGraphInstance)
      graphStore._causalGraphInstance = null
    },
    _initTopoGraph() {
      this._destroyTopoGraph()
      graphStore._topoGraphInstance = renderTopoGraph(
        this.$refs.topoGraphContainer,
        this.postmortem,
        { height: this.fullscreenGraph === 'topo' ? window.innerHeight : 400 },
      )
    },
    _initCausalGraph() {
      this._destroyCausalGraph()
      graphStore._causalGraphInstance = renderCausalGraph(
        this.$refs.causalGraphContainer,
        this.postmortem,
        { height: this.fullscreenGraph === 'causal' ? window.innerHeight : 300 },
      )
    },
    reinitGraphs() {
      this._initTopoGraph()
      this._initCausalGraph()
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
                              background: 'var(--sev-critical-bg)',
                              color: 'var(--sev-critical-fg)',
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
                              background: 'var(--st-info-bg)',
                              color: 'var(--st-info-fg)',
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
                              background: 'var(--sev-critical-bg)',
                              color: 'var(--sev-critical-fg)',
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
                              background: 'var(--st-info-bg)',
                              color: 'var(--st-info-fg)',
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
                  background: 'var(--surface-card)',
                  border: '1px solid var(--border-default)',
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
                    background: 'var(--surface-inset)',
                    border: '1px solid var(--border-subtle)',
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
                                background: 'var(--sev-critical-bg)',
                                color: 'var(--sev-critical-fg)',
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
                                background: 'var(--st-info-bg)',
                                color: 'var(--st-info-fg)',
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
                        { style: { color: 'var(--text-muted)', fontSize: '12px', marginTop: '4px' } },
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
                    background: 'var(--surface-inset)',
                    border: '1px solid var(--border-subtle)',
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
                                background: 'var(--sev-critical-bg)',
                                color: 'var(--sev-critical-fg)',
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
                                background: 'var(--st-info-bg)',
                                color: 'var(--st-info-fg)',
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
                        { style: { color: 'var(--text-muted)', fontSize: '12px', fontFamily: 'monospace' } },
                        item.impact_path,
                      )
                    : null,
                  item.reasoning
                    ? h(
                        'div',
                        { style: { color: 'var(--text-muted)', fontSize: '12px', marginTop: '4px' } },
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
                  background: 'var(--surface-raised)',
                  border: '1px solid var(--border-default)',
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
                background: 'var(--sev-warning-bg)',
                border: '1px solid var(--sev-warning-bd)',
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
                    color: 'var(--sev-warning-fg)',
                    lineHeight: '1.6',
                    marginBottom: '8px',
                  },
                },
                ad.summary || ad.root_cause,
              ),
              h(
                'div',
                { style: { fontSize: '12px', color: 'var(--text-muted)' } },
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
                  background: 'var(--sev-pass-bg)',
                  border: '1px solid var(--sev-pass-bd)',
                  borderRadius: '6px',
                  padding: '10px 14px',
                  fontSize: '13px',
                  color: 'var(--sev-pass-fg)',
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
                  background: p.is_alerted ? 'var(--sev-critical-bg)' : 'var(--sev-pass-bg)',
                  border: p.is_alerted ? '1px solid var(--sev-critical-bd)' : '1px solid var(--sev-pass-bd)',
                  color: p.is_alerted ? 'var(--sev-critical-fg)' : 'var(--sev-pass-fg)',
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
                    { style: { marginLeft: '12px', fontSize: '13px', color: 'var(--text-secondary)' } },
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
                      background: 'var(--st-info-bg)',
                      color: 'var(--st-info-fg)',
                      fontSize: '13px',
                      border: '1px solid var(--st-info-bd)',
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
                  background: 'var(--sev-warning-bg)',
                  border: '1px solid var(--sev-warning-bd)',
                  borderRadius: '8px',
                  padding: '16px',
                  color: 'var(--sev-warning-fg)',
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
                  background: 'var(--sev-pass-bg)',
                  border: '1px solid var(--sev-pass-bd)',
                  borderRadius: '8px',
                  padding: '16px',
                  color: 'var(--sev-pass-fg)',
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
                      background: 'var(--sev-pass-bg)',
                      border: '1px solid var(--sev-pass-bd)',
                      borderRadius: '6px',
                      padding: '10px 14px',
                      marginBottom: '6px',
                      fontSize: '13px',
                      color: 'var(--sev-pass-fg)',
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
                      background: 'var(--sev-critical-bg)',
                      border: '1px solid var(--sev-critical-bd)',
                      borderRadius: '6px',
                      padding: '10px 14px',
                      marginBottom: '6px',
                      fontSize: '13px',
                      color: 'var(--sev-critical-fg)',
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
                      background: 'var(--sev-warning-bg)',
                      border: '1px solid var(--sev-warning-bd)',
                      borderRadius: '6px',
                      padding: '10px 14px',
                      marginBottom: '6px',
                      fontSize: '13px',
                      color: 'var(--sev-warning-fg)',
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
                borderBottom: idx < r.lessons_learned.length - 1 ? '1px solid var(--border-subtle)' : 'none',
                fontSize: '13px',
                color: 'var(--text-primary)',
                lineHeight: '1.6',
              },
            },
            [
              h(
                'span',
                { style: { fontWeight: 600, marginRight: '8px', color: 'var(--accent)' } },
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
                  background: 'var(--surface-card)',
                  border: '1px solid var(--border-default)',
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
                            borderBottom: '2px solid var(--border-default)',
                            background: 'var(--surface-page)',
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
                  background: 'var(--surface-raised)',
                  border: '1px solid var(--border-default)',
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
                  background: 'var(--sev-critical-bg)',
                  border: '1px solid var(--sev-critical-bd)',
                  borderRadius: '6px',
                  padding: '8px 14px',
                  marginBottom: '12px',
                  fontSize: '13px',
                  color: 'var(--sev-critical-fg)',
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
                        background: 'var(--st-info-bg)',
                        color: 'var(--st-info-fg)',
                        fontSize: '12px',
                        border: '1px solid var(--st-info-bd)',
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
                h('tr', { key: 'dih' + idx, style: { background: 'var(--surface-inset)' } }, [
                  h(
                    'td',
                    {
                      colspan: 3,
                      style: {
                        padding: '6px 12px',
                        fontSize: '12px',
                        fontWeight: 600,
                        color: 'var(--text-secondary)',
                      },
                    },
                    item.kind,
                  ),
                ]),
              )
            }
            diRows.push(
              h('tr', { key: 'di' + idx, style: { borderBottom: '1px solid var(--border-subtle)' } }, [
                h('td', { style: { padding: '6px 12px', fontSize: '13px' } }, item.kind || '-'),
                h(
                  'td',
                  { style: { padding: '6px 12px', fontSize: '13px', fontWeight: 500 } },
                  item.name || '-',
                ),
                h(
                  'td',
                  { style: { padding: '6px 12px', fontSize: '13px', color: 'var(--text-secondary)' } },
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
                    background: 'var(--surface-card)',
                    border: '1px solid var(--border-default)',
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
                        h('tr', { style: { borderBottom: '2px solid var(--border-default)' } }, [
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
                h('tr', { key: 'iih' + idx, style: { background: 'var(--surface-inset)' } }, [
                  h(
                    'td',
                    {
                      colspan: 3,
                      style: {
                        padding: '6px 12px',
                        fontSize: '12px',
                        fontWeight: 600,
                        color: 'var(--text-secondary)',
                      },
                    },
                    item.kind,
                  ),
                ]),
              )
            }
            iiRows.push(
              h('tr', { key: 'ii' + idx, style: { borderBottom: '1px solid var(--border-subtle)' } }, [
                h('td', { style: { padding: '6px 12px', fontSize: '13px' } }, item.kind || '-'),
                h(
                  'td',
                  { style: { padding: '6px 12px', fontSize: '13px', fontWeight: 500 } },
                  item.name || '-',
                ),
                h(
                  'td',
                  { style: { padding: '6px 12px', fontSize: '13px', color: 'var(--text-secondary)' } },
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
                    background: 'var(--surface-card)',
                    border: '1px solid var(--border-default)',
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
                        h('tr', { style: { borderBottom: '2px solid var(--border-default)' } }, [
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
        const barColors = {
          critical: 'var(--sev-critical-fg)',
          warning: 'var(--sev-warning-fg)',
          normal: 'var(--sev-pass-fg)',
        }
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
                    background: 'var(--surface-sunken)',
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
                      background: barColors[m.status] || 'var(--sev-pass-fg)',
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
                    color: barColors[m.status] || 'var(--text-secondary)',
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
                  background: 'var(--surface-raised)',
                  border: '1px solid var(--border-default)',
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
                  background: 'var(--surface-card)',
                }
              : {
                  width: '100%',
                  height: '400px',
                  border: '1px solid var(--border-default)',
                  borderRadius: '6px',
                  background: 'var(--surface-raised)',
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
                    color: 'var(--text-secondary)',
                    background: 'color-mix(in srgb, var(--surface-card) 95%, transparent)',
                    padding: '6px 12px',
                    borderRadius: '6px',
                    border: '1px solid var(--border-default)',
                  }
                : {
                    position: 'absolute',
                    top: '8px',
                    right: '12px',
                    display: 'flex',
                    gap: '12px',
                    fontSize: '11px',
                    color: 'var(--text-secondary)',
                    background: 'color-mix(in srgb, var(--surface-card) 92%, transparent)',
                    padding: '4px 10px',
                    borderRadius: '4px',
                    border: '1px solid var(--border-default)',
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
                    background: 'var(--sev-critical-fg)',
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
                    background: 'var(--chart-7)',
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
                    background: 'var(--chart-1)',
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
                    color: 'var(--accent)',
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
                  background: 'var(--surface-card)',
                }
              : {
                  width: '100%',
                  height: '300px',
                  border: '1px solid var(--border-default)',
                  borderRadius: '6px',
                  background: 'var(--surface-raised)',
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
                    color: 'var(--text-secondary)',
                    background: 'color-mix(in srgb, var(--surface-card) 95%, transparent)',
                    padding: '6px 12px',
                    borderRadius: '6px',
                    border: '1px solid var(--border-default)',
                  }
                : {
                    position: 'absolute',
                    top: '8px',
                    right: '12px',
                    fontSize: '11px',
                    color: 'var(--text-secondary)',
                    background: 'color-mix(in srgb, var(--surface-card) 92%, transparent)',
                    padding: '4px 10px',
                    borderRadius: '4px',
                    border: '1px solid var(--border-default)',
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
                    color: 'var(--accent)',
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
          const sevTone =
            { ERROR: 'critical', WARN: 'warning', INFO: 'info', DEBUG: 'pending' }[sev] || 'pending'
          const sevColor = `var(--sev-${sevTone}-fg)`
          const sevBg = `var(--sev-${sevTone}-bg)`
          return h(
            'div',
            {
              key: 'log' + idx,
              style: {
                background: 'var(--surface-card)',
                border: '1px solid var(--border-default)',
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
                    borderBottom: '1px solid var(--border-subtle)',
                    background: 'var(--surface-inset)',
                  },
                },
                [
                  h(
                    'span',
                    {
                      style: {
                        fontSize: '12px',
                        color: 'var(--text-muted)',
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
                        background: 'var(--st-neutral-bg)',
                        color: 'var(--st-neutral-fg)',
                      },
                    },
                    (log.pod || '') + (log.container ? '/' + log.container : ''),
                  ),
                  log.namespace
                    ? h('span', { style: { fontSize: '11px', color: 'var(--text-muted)' } }, log.namespace)
                    : null,
                ].filter(Boolean),
              ),
              h(
                'div',
                {
                  style: {
                    padding: '10px 14px',
                    background: 'var(--terminal-bg)',
                    color: 'var(--terminal-fg)',
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
                  color: 'var(--accent)',
                  border: '1px solid var(--accent)',
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
          prevent: { label: '🛡️ 预防 (Prevent)', tone: 'info', items: [] },
          detect: { label: '🔍 检测 (Detect)', tone: 'pass', items: [] },
          mitigate: { label: '🛟 缓解 (Mitigate)', tone: 'warning', items: [] },
          other: { label: '📋 其他', tone: 'pending', items: [] },
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
          const kanbanCols = catKeys.map((catKey) => {
            const cat = categories[catKey]
            return h(
              'div',
              {
                key: catKey,
                style: {
                  background: `var(--sev-${cat.tone}-bg)`,
                  border: `1px solid var(--sev-${cat.tone}-bd)`,
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
                      color: `var(--sev-${cat.tone}-fg)`,
                      marginBottom: '10px',
                      paddingBottom: '8px',
                      borderBottom: `1px solid var(--sev-${cat.tone}-bd)`,
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
                          background: 'var(--surface-card)',
                          border: '1px solid var(--border-default)',
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
                                      background: 'var(--surface-sunken)',
                                      color: 'var(--text-secondary)',
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
                              ? h('span', { style: { color: 'var(--text-muted)' } }, '截止: ' + item.due_date)
                              : null,
                            item.category
                              ? h(
                                  'span',
                                  {
                                    style: {
                                      fontSize: '10px',
                                      padding: '1px 6px',
                                      borderRadius: '3px',
                                      background: 'var(--surface-sunken)',
                                      color: 'var(--text-muted)',
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
                              { style: { fontSize: '11px', color: 'var(--text-muted)', marginTop: '4px' } },
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

      this.$nextTick(() => this.reinitGraphs())

      return h(
        'div',
        {
          style: {
            background: 'var(--surface-card)',
            border: '1px solid var(--border-default)',
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
                  border: '1px solid var(--border-default)',
                  borderRadius: '6px',
                  fontSize: '14px',
                  outline: 'none',
                  background: 'var(--surface-card)',
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
                    background: 'var(--accent)',
                    color: 'var(--text-on-accent)',
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
                    if (!this.loading) e.currentTarget.style.background = 'var(--accent-hover)'
                  },
                  onMouseleave: (e) => {
                    e.currentTarget.style.background = 'var(--accent)'
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
                    background: 'var(--chart-5)',
                    color: 'var(--text-on-accent)',
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
                      e.currentTarget.style.background = 'color-mix(in srgb, var(--chart-5), white 15%)'
                  },
                  onMouseleave: (e) => {
                    e.currentTarget.style.background = 'var(--chart-5)'
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
                    color: 'var(--accent)',
                    border: '1px solid var(--accent)',
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
                      e.currentTarget.style.background = 'var(--accent)'
                      e.currentTarget.style.color = 'var(--text-on-accent)'
                    }
                  },
                  onMouseleave: (e) => {
                    e.currentTarget.style.background = 'none'
                    e.currentTarget.style.color = 'var(--accent)'
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
                  background: 'var(--st-danger-bg)',
                  border: '1px solid var(--st-danger-bd)',
                  borderRadius: '8px',
                  color: 'var(--st-danger-fg)',
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
                          background: 'var(--surface-raised)',
                          border: '1px solid var(--border-default)',
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
                                background: 'var(--sev-warning-bg)',
                                border: '1px solid var(--sev-warning-bd)',
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
                                    color: 'var(--sev-warning-fg)',
                                    marginBottom: '6px',
                                  },
                                },
                                '根因',
                              ),
                              h(
                                'div',
                                {
                                  style: { fontSize: '14px', color: 'var(--sev-warning-fg)', lineHeight: '1.6' },
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
                                background: 'var(--surface-card)',
                                border: '1px solid var(--border-default)',
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
