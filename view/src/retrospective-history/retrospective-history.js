import { createApp, ref, h, onMounted, onUnmounted, getCurrentInstance } from 'vue'
import { NavBar } from '../components/SharedComponents.js'
import { StatusBadge } from '../components/primitives.js'
import { API } from '../utils/api.js'
import { onThemeChange } from '../styles/theme.js'
import { renderTopoGraph, renderCausalGraph, destroyGraph } from '../utils/topo-graph.js'
import '../styles/common.css'

// G6 图实例存储（模块级，供 setup 和 methods 共享）
const graphStore = {}

/* 严重度 → 语义档位，颜色由组件层 data-sev 决定，两主题自动适配 */
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
  const d = new Date(ts)
  if (isNaN(d.getTime())) return '-'
  return d.toLocaleString('zh-CN')
}

function severityBadge(severity, text) {
  const s = SEV_TONES[(severity || '').toLowerCase()] || 'pending'
  return h(StatusBadge, { sev: s, text: text || severity || '—' })
}

function infoCard(label, value) {
  return h('div', { class: 'info-card' }, [
    h('div', { class: 'info-label' }, label),
    h('div', { class: 'info-value' }, value || '-'),
  ])
}

createApp({
  components: { NavBar },
  setup() {
    const items = ref([])
    const total = ref(0)
    const page = ref(1)
    const pageSize = ref(20)
    const loading = ref(false)
    const error = ref(null)

    const filters = ref({
      severity: '',
      keyword: '',
      start_time: '',
      end_time: '',
    })

    const toDatetimeLocal = (d) => {
      try {
        return d.toISOString().slice(0, 16)
      } catch {
        return ''
      }
    }

    ;(() => {
      const now = new Date()
      const start = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000)
      start.setHours(0, 0, 0, 0)
      filters.value.start_time = toDatetimeLocal(start)
      filters.value.end_time = toDatetimeLocal(now)
    })()

    const selectedFp = ref(null)
    const detailLoading = ref(false)
    const postmortem = ref(null)
    const editing = ref(false)
    const saving = ref(false)
    const editForm = ref({
      root_cause: '',
      resolution: '',
      lessons_learned: [],
      action_items: [],
    })

    const fetchList = async (p) => {
      const pg = p || 1
      loading.value = true
      error.value = null
      try {
        const params = { page: pg, page_size: pageSize.value }
        if (filters.value.severity) params.severity = filters.value.severity
        if (filters.value.keyword) params.keyword = filters.value.keyword
        if (filters.value.start_time) params.start_time = filters.value.start_time
        if (filters.value.end_time) params.end_time = filters.value.end_time
        const res = await API.retrospective.list(params)
        items.value = res.items || []
        total.value = res.total || 0
        page.value = res.page || pg
      } catch (e) {
        error.value = '加载列表失败: ' + (e.message || '未知错误')
        items.value = []
      } finally {
        loading.value = false
      }
    }

    const search = () => {
      fetchList(1)
    }
    const resetFilters = () => {
      const now = new Date()
      const start = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000)
      start.setHours(0, 0, 0, 0)
      filters.value = {
        severity: '',
        keyword: '',
        start_time: toDatetimeLocal(start),
        end_time: toDatetimeLocal(now),
      }
      fetchList(1)
    }
    const goPage = (p) => {
      if (p >= 1 && p <= totalPages.value) fetchList(p)
    }

    const totalPages = () => Math.max(1, Math.ceil(total.value / pageSize.value))

    const viewDetail = async (fingerprint) => {
      selectedFp.value = fingerprint
      detailLoading.value = true
      postmortem.value = null
      editing.value = false
      try {
        const stored = await API.retrospective.history(fingerprint)
        if (stored?.report_json) {
          try {
            postmortem.value = JSON.parse(stored.report_json)
          } catch (parseErr) {
            error.value = '数据解析失败: ' + parseErr.message
          }
        }
      } catch (e) {
        error.value = '加载复盘详情失败: ' + (e.message || '未知错误')
      } finally {
        detailLoading.value = false
      }
    }

    const backToList = () => {
      selectedFp.value = null
      postmortem.value = null
      editing.value = false
    }

    const startEdit = () => {
      if (!postmortem.value) return
      const r = postmortem.value
      editForm.value = {
        root_cause: typeof r.root_cause === 'string' ? r.root_cause : r.root_cause?.final || '',
        resolution: typeof r.resolution === 'string' ? r.resolution : r.resolution?.final || '',
        lessons_learned: [...(r.lessons_learned || [])],
        action_items: (r.action_items || []).map((a) => ({ ...a })),
      }
      editing.value = true
    }

    const cancelEdit = () => {
      editing.value = false
    }

    const saveEdit = async () => {
      if (!selectedFp.value || saving.value) return
      saving.value = true
      try {
        const payload = {
          root_cause: editForm.value.root_cause,
          resolution: editForm.value.resolution,
          lessons_learned: editForm.value.lessons_learned,
          action_items: editForm.value.action_items,
        }
        const updated = await API.retrospective.update(selectedFp.value, payload)
        postmortem.value = updated
        editing.value = false
      } catch (e) {
        error.value = '保存失败: ' + (e.message || '未知错误')
      } finally {
        saving.value = false
      }
    }

    const addLesson = () => {
      editForm.value.lessons_learned.push('')
    }
    const removeLesson = (i) => {
      editForm.value.lessons_learned.splice(i, 1)
    }
    const addActionItem = () => {
      editForm.value.action_items.push({
        description: '',
        priority: 'medium',
        owner: '',
        due_date: '',
        status: 'open',
      })
    }
    const removeActionItem = (i) => {
      editForm.value.action_items.splice(i, 1)
    }

    const exportMd = async (fingerprint) => {
      try {
        const text = await API.retrospective.postmortemText(fingerprint)
        if (!text) return
        const blob = new Blob([text], { type: 'text/markdown;charset=utf-8' })
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = `postmortem_${fingerprint.slice(0, 16)}.md`
        a.click()
        URL.revokeObjectURL(url)
      } catch (e) {
        error.value = '导出失败: ' + (e.message || '未知错误')
      }
    }

    fetchList(1)

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
    const handleKeydown = (e) => {
      if (e.key === 'Escape' && fullscreenGraph.value) fullscreenGraph.value = null
    }
    // 主题切换后按新 token 重建 G6（canvas 吃不了 CSS 变量，只能重绘）
    const instance = getCurrentInstance()
    const offTheme = onThemeChange(() => {
      const proxy = instance && instance.proxy
      if (proxy && proxy.postmortem) proxy._scheduleGraphInit()
    })
    onMounted(() => document.addEventListener('keydown', handleKeydown))
    onUnmounted(() => {
      document.removeEventListener('keydown', handleKeydown)
      offTheme()
      destroyGraph(graphStore._topoGraphInstance)
      graphStore._topoGraphInstance = null
      destroyGraph(graphStore._causalGraphInstance)
      graphStore._causalGraphInstance = null
    })

    return {
      items,
      total,
      page,
      pageSize,
      loading,
      error,
      filters,
      selectedFp,
      detailLoading,
      postmortem,
      editing,
      saving,
      editForm,
      fullscreenGraph,
      toggleFullscreen,
      totalPages,
      search,
      resetFilters,
      goPage,
      viewDetail,
      backToList,
      startEdit,
      cancelEdit,
      saveEdit,
      addLesson,
      removeLesson,
      addActionItem,
      removeActionItem,
      exportMd,
      formatTime,
      severityBadge,
      infoCard,
    }
  },
  methods: {
    renderList() {
      return [
        h('div', { class: 'card filters-bar' }, [
          h('input', {
            type: 'datetime-local',
            placeholder: '开始时间',
            value: this.filters.start_time,
            onInput: (e) => (this.filters.start_time = e.target.value),
            style: { minWidth: '180px' },
          }),
          h('input', {
            type: 'datetime-local',
            placeholder: '结束时间',
            value: this.filters.end_time,
            onInput: (e) => (this.filters.end_time = e.target.value),
            style: { minWidth: '180px' },
          }),
          h(
            'select',
            {
              value: this.filters.severity,
              onChange: (e) => (this.filters.severity = e.target.value),
            },
            [
              h('option', { value: '' }, '全部级别'),
              h('option', { value: 'critical' }, 'Critical'),
              h('option', { value: 'error' }, 'Error'),
              h('option', { value: 'warning' }, 'Warning'),
              h('option', { value: 'info' }, 'Info'),
            ],
          ),
          h('input', {
            type: 'text',
            placeholder: '搜索关键词...',
            value: this.filters.keyword,
            onInput: (e) => (this.filters.keyword = e.target.value),
            onKeydown: (e) => {
              if (e.key === 'Enter') this.search()
            },
          }),
          h('button', { class: 'btn btn-primary btn-sm', onClick: this.search }, '搜索'),
          h('button', { class: 'btn btn-secondary btn-sm', onClick: this.resetFilters }, '重置'),
        ]),

        this.loading
          ? h('div', { class: 'loading' }, '加载中...')
          : this.items.length === 0
            ? h('div', { class: 'card empty-state' }, [
                h('div', { class: 'icon' }, '[ ]'),
                h('div', null, '暂无复盘报告'),
              ])
            : h(
                'div',
                {
                  class: 'card-list',
                  style: {
                    background: 'var(--surface-card)',
                    borderRadius: 'var(--card-radius)',
                    boxShadow: 'var(--shadow-sm)',
                  },
                },
                [
                  ...this.items.map((item) => this.renderCard(item)),
                  this.totalPages() > 1
                    ? h('div', { class: 'pagination' }, [
                        h(
                          'button',
                          { disabled: this.page <= 1, onClick: () => this.goPage(this.page - 1) },
                          '[<] 上一页',
                        ),
                        h('span', null, `第 ${this.page}/${this.totalPages()} 页`),
                        h(
                          'button',
                          {
                            disabled: this.page >= this.totalPages(),
                            onClick: () => this.goPage(this.page + 1),
                          },
                          '下一页 [>]',
                        ),
                      ])
                    : null,
                ],
              ),
      ]
    },

    renderCard(item) {
      const sev = (item.severity || '').toLowerCase()
      const stripColor = `var(--sev-${SEV_TONES[sev] || 'pending'}-fg)`
      return h('div', { class: 'report-card', onClick: () => this.viewDetail(item.fingerprint) }, [
        h('div', { class: 'severity-strip', style: { background: stripColor } }),
        h('div', { class: 'card-body' }, [
          h('div', { class: 'card-title' }, [
            this.severityBadge(item.severity),
            h('span', null, item.incident_title || '未命名报告'),
          ]),
          h(
            'div',
            { class: 'card-meta' },
            [
              item.resource_name
                ? h('span', null, `资源: ${item.resource_kind || '-'}/${item.resource_name}`)
                : null,
              item.namespace ? h('span', null, `命名空间: ${item.namespace}`) : null,
              item.start_time
                ? h(
                    'span',
                    null,
                    `${this.formatTime(item.start_time)} ~ ${this.formatTime(item.end_time)} (${item.duration || '-'})`,
                  )
                : null,
              item.updated_at && item.created_at &&
              new Date(item.updated_at).getTime() - new Date(item.created_at).getTime() > 60000
                ? h('span', { title: '该报告被重新生成过' }, `更新于 ${this.formatTime(item.updated_at)}`)
                : item.created_at
                  ? h('span', null, `生成于 ${this.formatTime(item.created_at)}`)
                  : null,
            ].filter(Boolean),
          ),
          item.root_cause_summary
            ? h('div', { class: 'card-summary' }, `根因: ${item.root_cause_summary}`)
            : null,
          h('div', { class: 'card-actions' }, [
            h(
              'button',
              {
                class: 'btn btn-primary btn-sm',
                onClick: (e) => {
                  e.stopPropagation()
                  this.viewDetail(item.fingerprint)
                },
              },
              '查看详情',
            ),
            h(
              'button',
              {
                class: 'btn btn-secondary btn-sm',
                onClick: (e) => {
                  e.stopPropagation()
                  this.exportMd(item.fingerprint)
                },
              },
              '导出 Markdown',
            ),
          ]),
        ]),
      ])
    },

    renderDetail() {
      const loading = this.detailLoading
      const pm = this.postmortem
      if (loading) return h('div', { class: 'loading' }, '加载中...')
      if (!pm) return null

      this.$nextTick(() => this._scheduleGraphInit())
      return h('div', { class: 'card detail-panel' }, [
        h('div', { class: 'detail-header' }, [
          h('span', { class: 'back-link', onClick: this.backToList }, '[<-] 返回列表'),
          h('div', { class: 'detail-actions' }, [
            this.editing
              ? null
              : h('button', { class: 'btn btn-primary btn-sm', onClick: this.startEdit }, '编辑'),
            this.editing
              ? h(
                  'button',
                  {
                    class: 'btn btn-primary btn-sm',
                    onClick: this.saveEdit,
                    disabled: this.saving,
                  },
                  this.saving ? '保存中...' : '保存',
                )
              : null,
            this.editing
              ? h('button', { class: 'btn btn-secondary btn-sm', onClick: this.cancelEdit }, '取消')
              : null,
            h(
              'button',
              { class: 'btn btn-secondary btn-sm', onClick: () => this.exportMd(this.selectedFp) },
              '导出 Markdown',
            ),
          ]),
        ]),

        h(
          'div',
          { class: 'info-grid' },
          [
            this.infoCard('事故标题', pm.incident_title),
            this.infoCard('开始时间', this.formatTime(pm.start_time)),
            this.infoCard('结束时间', this.formatTime(pm.end_time)),
            this.infoCard('持续时间', pm.duration),
            this.infoCard('严重级别', this.severityBadge(pm.severity)),
            this.infoCard('生成时间', this.formatTime(pm.generated_at)),
            pm.detection_method ? this.infoCard('检测方式', pm.detection_method) : null,
            pm.mttd ? this.infoCard('MTTD', pm.mttd) : null,
          ].filter(Boolean),
        ),

        pm.business_context ? this.renderBusinessContext(pm.business_context) : null,
        pm.business_calls &&
        (pm.business_calls.upstreams?.length || pm.business_calls.downstreams?.length)
          ? this.renderSection('业务调用链', this.renderBusinessCalls(pm.business_calls))
          : null,
        pm.business_impact &&
        (pm.business_impact.direct_impacts?.length || pm.business_impact.indirect_impacts?.length)
          ? this.renderSection('业务影响分析', this.renderBusinessImpact(pm.business_impact))
          : null,
        pm.workload_context ? this.renderWorkloadContext(pm.workload_context) : null,
        pm.impact_assessment
          ? this.renderSection('影响评估详情', this.renderImpactAssessment(pm.impact_assessment))
          : null,
        pm.diagnosis_metrics?.length
          ? this.renderSection('指标快照', this.renderMetricsBar(pm.diagnosis_metrics))
          : null,
        pm.impacted_services?.length
          ? this.renderSection('受影响服务', this.renderImpactedSvc(pm.impacted_services))
          : null,

        this.renderTopoSection(pm),
        this.renderCausalDagSection(pm),

        pm.timeline?.length ? this.renderTimeline(pm.timeline) : null,
        pm.causal_chain ? this.renderCausalChain(pm.causal_chain) : null,

        this.editing
          ? this.renderEditableField(
              '📝 诊断结论',
              'root_cause',
              this.editForm.root_cause,
              pm.root_cause,
            )
          : this.renderTextField('📝 诊断结论', pm.root_cause, 'warning'),
        this.editing
          ? this.renderEditableField(
              '解决措施',
              'resolution',
              this.editForm.resolution,
              pm.resolution,
            )
          : this.renderTextField('解决措施', pm.resolution, 'success'),
        this.renderLessonsLearned(),
        pm.what_went_well?.length
          ? this.renderSection('✅ 做得好的', this.renderCheckList(pm.what_went_well, 'pass'))
          : null,
        pm.what_went_wrong?.length
          ? this.renderSection('❌ 可以改进的', this.renderCheckList(pm.what_went_wrong, 'error'))
          : null,
        pm.contributing_factors?.length
          ? this.renderSection(
              '🔍 促成因素',
              this.renderCheckList(pm.contributing_factors, 'pending'),
            )
          : null,
        this.renderActionItems(),
        pm.ai_diagnosis ? this.renderAiDiagnosis(pm.ai_diagnosis) : null,
      ])
    },

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
        this.$refs.topoGraphContainerHist,
        this.postmortem,
        { height: this.fullscreenGraph === 'topo' ? window.innerHeight : 400 },
      )
    },
    _initCausalGraph() {
      this._destroyCausalGraph()
      graphStore._causalGraphInstance = renderCausalGraph(
        this.$refs.causalGraphContainerHist,
        this.postmortem,
        { height: this.fullscreenGraph === 'causal' ? window.innerHeight : 300 },
      )
    },
    _scheduleGraphInit() {
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          this._initTopoGraph()
          this._initCausalGraph()
        })
      })
    },
    renderTopoSection(pm) {
      if (
        !pm.business_calls ||
        (!pm.business_calls.upstreams?.length && !pm.business_calls.downstreams?.length)
      ) {
        return this.renderSection(
          '🔗 业务拓扑',
          h(
            'div',
            { style: { textAlign: 'center', padding: '40px', color: 'var(--text-muted)', fontSize: '13px' } },
            '暂无业务调用链数据',
          ),
        )
      }
      const isFull = this.fullscreenGraph === 'topo'
      const legend = h(
        'div',
        {
          style: isFull
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
                gap: '8px',
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
              style: { cursor: 'pointer', fontWeight: 600, color: 'var(--accent)', userSelect: 'none' },
              onClick: () => this.toggleFullscreen('topo'),
            },
            isFull ? '⧉ 退出' : '⛶ 全屏',
          ),
        ],
      )
      return this.renderSection(
        '🔗 业务拓扑',
        h('div', { style: { position: 'relative' } }, [
          h('div', {
            ref: 'topoGraphContainerHist',
            style: isFull
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
          legend,
        ]),
      )
    },
    renderCausalDagSection(pm) {
      if (!pm.causal_chain || !pm.causal_chain.links || pm.causal_chain.links.length === 0) {
        return this.renderSection(
          '🔗 因果链 DAG',
          h(
            'div',
            { style: { textAlign: 'center', padding: '40px', color: 'var(--text-muted)', fontSize: '13px' } },
            '暂无因果链数据',
          ),
        )
      }
      const isFull = this.fullscreenGraph === 'causal'
      const btn = h(
        'div',
        {
          style: isFull
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
              style: { cursor: 'pointer', fontWeight: 600, color: 'var(--accent)', userSelect: 'none' },
              onClick: () => this.toggleFullscreen('causal'),
            },
            isFull ? '⧉ 退出' : '⛶ 全屏',
          ),
        ],
      )
      return this.renderSection(
        '🔗 因果链 DAG',
        h('div', { style: { position: 'relative' } }, [
          h('div', {
            ref: 'causalGraphContainerHist',
            style: isFull
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
          btn,
        ]),
      )
    },

    renderSection(title, content) {
      return h('div', { class: 'section' }, [h('div', { class: 'section-title' }, title), content])
    },

    renderCheckList(items, tone) {
      return h(
        'div',
        { style: { display: 'flex', flexDirection: 'column', gap: '6px' } },
        items.map((item, idx) =>
          h(
            'div',
            {
              key: idx,
              style: {
                display: 'flex',
                gap: '8px',
                alignItems: 'flex-start',
                padding: '8px 12px',
                background: `var(--sev-${tone}-bg)`,
                borderLeft: `3px solid var(--sev-${tone}-fg)`,
                borderRadius: '4px',
                fontSize: '13px',
                color: 'var(--text-primary)',
                lineHeight: '1.6',
              },
            },
            [
              h(
                'span',
                { style: { flexShrink: 0, fontWeight: 600, color: `var(--sev-${tone}-fg)` } },
                '✓',
              ),
              h('span', null, item),
            ],
          ),
        ),
      )
    },

    renderImpactedSvc(svcs) {
      return h(
        'div',
        { style: { display: 'flex', flexWrap: 'wrap', gap: '8px' } },
        svcs.map((svc, idx) =>
          h(
            'span',
            {
              key: idx,
              style: {
                padding: '4px 12px',
                background: 'var(--surface-sunken)',
                borderRadius: '12px',
                fontSize: '12px',
                color: 'var(--text-secondary)',
                fontFamily: 'monospace',
              },
            },
            svc,
          ),
        ),
      )
    },

    renderBusinessCalls(bc) {
      return h(
        'div',
        { style: { display: 'flex', gap: '16px', flexWrap: 'wrap' } },
        [
          bc.upstreams?.length
            ? h('div', { style: { flex: 1, minWidth: '200px' } }, [
                h(
                  'div',
                  {
                    style: {
                      fontWeight: 600,
                      fontSize: '12px',
                      color: 'var(--text-muted)',
                      marginBottom: '8px',
                    },
                  },
                  '⬆ 上游调用方',
                ),
                ...bc.upstreams.map((u, i) =>
                  h(
                    'div',
                    {
                      key: i,
                      style: {
                        padding: '6px 10px',
                        background: 'var(--surface-raised)',
                        border: '1px solid var(--border-default)',
                        borderRadius: '4px',
                        marginBottom: '4px',
                        fontSize: '13px',
                      },
                    },
                    [
                      h('span', { style: { fontWeight: 500 } }, u.appName || u.app_name || '-'),
                      u.team
                        ? h(
                            'span',
                            { style: { marginLeft: '8px', color: 'var(--text-muted)', fontSize: '11px' } },
                            u.team,
                          )
                        : null,
                      u.criticality
                        ? h(
                            'span',
                            {
                              style: {
                                marginLeft: '8px',
                                padding: '1px 6px',
                                borderRadius: '3px',
                                fontSize: '10px',
                                background: u.criticality === 'high' ? 'var(--sev-critical-bg)' : 'var(--sev-pass-bg)',
                                color: u.criticality === 'high' ? 'var(--sev-critical-fg)' : 'var(--sev-pass-fg)',
                              },
                            },
                            u.criticality,
                          )
                        : null,
                    ],
                  ),
                ),
              ])
            : null,
          bc.downstreams?.length
            ? h('div', { style: { flex: 1, minWidth: '200px' } }, [
                h(
                  'div',
                  {
                    style: {
                      fontWeight: 600,
                      fontSize: '12px',
                      color: 'var(--text-muted)',
                      marginBottom: '8px',
                    },
                  },
                  '⬇ 下游依赖方',
                ),
                ...bc.downstreams.map((d, i) =>
                  h(
                    'div',
                    {
                      key: i,
                      style: {
                        padding: '6px 10px',
                        background: 'var(--surface-raised)',
                        border: '1px solid var(--border-default)',
                        borderRadius: '4px',
                        marginBottom: '4px',
                        fontSize: '13px',
                      },
                    },
                    [
                      h('span', { style: { fontWeight: 500 } }, d.appName || d.app_name || '-'),
                      d.team
                        ? h(
                            'span',
                            { style: { marginLeft: '8px', color: 'var(--text-muted)', fontSize: '11px' } },
                            d.team,
                          )
                        : null,
                      d.criticality
                        ? h(
                            'span',
                            {
                              style: {
                                marginLeft: '8px',
                                padding: '1px 6px',
                                borderRadius: '3px',
                                fontSize: '10px',
                                background: d.criticality === 'high' ? 'var(--sev-critical-bg)' : 'var(--sev-pass-bg)',
                                color: d.criticality === 'high' ? 'var(--sev-critical-fg)' : 'var(--sev-pass-fg)',
                              },
                            },
                            d.criticality,
                          )
                        : null,
                    ],
                  ),
                ),
              ])
            : null,
        ].filter(Boolean),
      )
    },

    renderBusinessImpact(bi) {
      const items = []
      if (bi.risk_level)
        items.push(
          this.infoCard(
            '风险等级',
            h(
              'span',
              {
                style: {
                  fontWeight: 600,
                  color: bi.risk_level === 'critical' ? 'var(--sev-critical-fg)' : 'var(--sev-warning-fg)',
                },
              },
              bi.risk_level,
            ),
          ),
        )
      if (bi.direct_impacts?.length)
        items.push(
          h('div', { style: { gridColumn: '1 / -1' } }, [
            h(
              'div',
              {
                style: { fontWeight: 600, fontSize: '12px', color: 'var(--sev-critical-fg)', marginBottom: '8px' },
              },
              `直接影响 (${bi.direct_impacts.length})`,
            ),
            ...bi.direct_impacts.map((d, i) =>
              h(
                'div',
                {
                  key: i,
                  style: {
                    padding: '6px 10px',
                    background: 'var(--sev-critical-bg)',
                    borderLeft: '3px solid var(--sev-critical-fg)',
                    borderRadius: '4px',
                    marginBottom: '4px',
                    fontSize: '13px',
                  },
                },
                [
                  h('span', { style: { fontWeight: 500 } }, d.app_name || d.appName || '-'),
                  d.criticality
                    ? h(
                        'span',
                        {
                          style: {
                            marginLeft: '8px',
                            padding: '1px 6px',
                            borderRadius: '3px',
                            fontSize: '10px',
                            background: 'var(--sev-critical-bg)',
                            color: 'var(--sev-critical-fg)',
                          },
                        },
                        d.criticality,
                      )
                    : null,
                  d.reasoning
                    ? h(
                        'div',
                        { style: { marginTop: '4px', fontSize: '11px', color: 'var(--text-muted)' } },
                        d.reasoning,
                      )
                    : null,
                ],
              ),
            ),
          ]),
        )
      if (bi.indirect_impacts?.length)
        items.push(
          h('div', { style: { gridColumn: '1 / -1' } }, [
            h(
              'div',
              {
                style: { fontWeight: 600, fontSize: '12px', color: 'var(--sev-warning-fg)', marginBottom: '8px' },
              },
              `间接影响 (${bi.indirect_impacts.length})`,
            ),
            ...bi.indirect_impacts.map((d, i) =>
              h(
                'div',
                {
                  key: i,
                  style: {
                    padding: '6px 10px',
                    background: 'var(--sev-warning-bg)',
                    borderLeft: '3px solid var(--sev-warning-fg)',
                    borderRadius: '4px',
                    marginBottom: '4px',
                    fontSize: '13px',
                  },
                },
                [
                  h('span', { style: { fontWeight: 500 } }, d.app_name || d.appName || '-'),
                  d.hopDistance != null
                    ? h(
                        'span',
                        { style: { marginLeft: '8px', fontSize: '11px', color: 'var(--text-muted)' } },
                        `${d.hopDistance}跳`,
                      )
                    : null,
                  d.impactPath
                    ? h(
                        'div',
                        {
                          style: {
                            marginTop: '4px',
                            fontSize: '11px',
                            color: 'var(--text-muted)',
                            fontFamily: 'monospace',
                          },
                        },
                        d.impactPath,
                      )
                    : null,
                  d.reasoning
                    ? h(
                        'div',
                        { style: { marginTop: '2px', fontSize: '11px', color: 'var(--text-muted)' } },
                        d.reasoning,
                      )
                    : null,
                ],
              ),
            ),
          ]),
        )
      return h(
        'div',
        {
          class: 'info-grid',
          style: { gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))' },
        },
        items.filter(Boolean),
      )
    },

    renderWorkloadContext(wc) {
      const items = [
        this.infoCard('控制器', `${wc.controller_kind || '-'}/${wc.controller_name || '-'}`),
        this.infoCard('副本健康', `${wc.healthy_pods || 0}/${wc.total_pods || 0}`),
      ]
      if (wc.pods?.length) {
        items.push(
          h('div', { style: { gridColumn: '1 / -1' } }, [
            h(
              'div',
              { style: { fontWeight: 600, fontSize: '12px', color: 'var(--text-muted)', marginBottom: '8px' } },
              'Pod 列表',
            ),
            h(
              'div',
              { style: { display: 'flex', flexWrap: 'wrap', gap: '6px' } },
              wc.pods.map((p, i) =>
                h(
                  'span',
                  {
                    key: i,
                    style: {
                      padding: '3px 10px',
                      borderRadius: '12px',
                      fontSize: '11px',
                      fontFamily: 'monospace',
                      background: p.is_alerted ? 'var(--sev-critical-bg)' : 'var(--sev-pass-bg)',
                      color: p.is_alerted ? 'var(--sev-critical-fg)' : 'var(--sev-pass-fg)',
                      border: '1px solid ' + (p.is_alerted ? 'var(--sev-critical-bd)' : 'var(--sev-pass-bd)'),
                    },
                  },
                  p.name,
                ),
              ),
            ),
          ]),
        )
      }
      return h(
        'div',
        {
          class: 'info-grid',
          style: { gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))' },
        },
        items.filter(Boolean),
      )
    },

    renderImpactAssessment(ia) {
      return h('div', null, [
        h(
          'div',
          {
            class: 'info-grid',
            style: {
              gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))',
              marginBottom: '16px',
            },
          },
          [
            this.infoCard('严重级别', this.severityBadge(ia.severity || '-')),
            this.infoCard('影响半径', (ia.blast_radius || 0) + ' 跳'),
            this.infoCard('直接受影响', (ia.direct_impact?.length || 0) + ' 个'),
            this.infoCard('间接影响', (ia.indirect_impact?.length || 0) + ' 个'),
          ].filter(Boolean),
        ),
        ia.user_facing_impact
          ? h(
              'div',
              {
                style: {
                  padding: '8px 14px',
                  background: 'var(--sev-critical-bg)',
                  border: '1px solid var(--sev-critical-bd)',
                  borderRadius: '6px',
                  marginBottom: '12px',
                  fontSize: '13px',
                  color: 'var(--sev-critical-fg)',
                  fontWeight: 500,
                },
              },
              '⚠️ 用户面受影响',
            )
          : null,
        ia.affected_services?.length
          ? h('div', { style: { marginBottom: '12px' } }, [
              h(
                'div',
                {
                  style: { fontWeight: 600, fontSize: '12px', color: 'var(--text-muted)', marginBottom: '6px' },
                },
                `受影响 K8s 服务 (${ia.affected_services.length})`,
              ),
              h(
                'div',
                { style: { display: 'flex', flexWrap: 'wrap', gap: '6px' } },
                ia.affected_services.map((s, i) =>
                  h(
                    'span',
                    {
                      key: i,
                      style: {
                        padding: '3px 10px',
                        background: 'var(--sev-warning-bg)',
                        border: '1px solid var(--sev-warning-bd)',
                        borderRadius: '12px',
                        fontSize: '11px',
                        fontFamily: 'monospace',
                      },
                    },
                    s,
                  ),
                ),
              ),
            ])
          : null,
        ia.direct_impact?.length
          ? h('div', { style: { marginBottom: '12px' } }, [
              h(
                'div',
                {
                  style: {
                    fontWeight: 600,
                    fontSize: '12px',
                    color: 'var(--sev-critical-fg)',
                    marginBottom: '6px',
                  },
                },
                `🔴 直接影响 (${ia.direct_impact.length})`,
              ),
              h('table', { class: 'rh-table' }, [
                h(
                  'thead',
                  null,
                  h('tr', null, [
                    h('th', null, '类型'),
                    h('th', null, '名称'),
                    h('th', null, '命名空间'),
                  ]),
                ),
                h(
                  'tbody',
                  null,
                  ia.direct_impact.map((r, i) =>
                    h('tr', { key: i }, [
                      h('td', null, r.kind || '-'),
                      h('td', { style: { fontFamily: 'monospace' } }, r.name || '-'),
                      h('td', null, r.namespace || '-'),
                    ]),
                  ),
                ),
              ]),
            ])
          : null,
        ia.indirect_impact?.length
          ? h('div', null, [
              h(
                'div',
                {
                  style: {
                    fontWeight: 600,
                    fontSize: '12px',
                    color: 'var(--sev-warning-fg)',
                    marginBottom: '6px',
                  },
                },
                `🟡 间接影响 (${ia.indirect_impact.length})`,
              ),
              h('table', { class: 'rh-table' }, [
                h(
                  'thead',
                  null,
                  h('tr', null, [
                    h('th', null, '类型'),
                    h('th', null, '名称'),
                    h('th', null, '命名空间'),
                  ]),
                ),
                h(
                  'tbody',
                  null,
                  ia.indirect_impact
                    .slice(0, 20)
                    .map((r, i) =>
                      h('tr', { key: i }, [
                        h('td', null, r.kind || '-'),
                        h('td', { style: { fontFamily: 'monospace' } }, r.name || '-'),
                        h('td', null, r.namespace || '-'),
                      ]),
                    ),
                ),
                ia.indirect_impact.length > 20
                  ? h(
                      'tr',
                      null,
                      h(
                        'td',
                        { colSpan: 3, style: { textAlign: 'center', color: 'var(--text-muted)' } },
                        `... 还有 ${ia.indirect_impact.length - 20} 条`,
                      ),
                    )
                  : null,
              ]),
            ])
          : null,
      ])
    },

    renderMetricsBar(metrics) {
      const maxVal = Math.max(...metrics.map((m) => m.value || 0), 1)
      return h(
        'div',
        { style: { display: 'flex', flexDirection: 'column', gap: '8px' } },
        metrics.map((m, idx) => {
          const pct = Math.min((m.value / maxVal) * 100, 100)
          const color =
            m.status === 'critical' ? 'var(--sev-critical-fg)' : m.status === 'warning' ? 'var(--sev-warning-fg)' : 'var(--sev-pass-fg)'
          return h(
            'div',
            { key: idx, style: { display: 'flex', alignItems: 'center', gap: '10px' } },
            [
              h(
                'span',
                {
                  style: {
                    fontSize: '12px',
                    fontFamily: 'monospace',
                    minWidth: '160px',
                    color: 'var(--text-primary)',
                  },
                },
                m.metricName + ' / ' + m.resourceName,
              ),
              h(
                'div',
                {
                  style: {
                    flex: 1,
                    height: '20px',
                    background: 'var(--st-neutral-bg)',
                    borderRadius: '4px',
                    overflow: 'hidden',
                  },
                },
                h('div', {
                  style: {
                    width: pct + '%',
                    height: '100%',
                    background: color,
                    borderRadius: '4px',
                    transition: 'width 0.5s',
                  },
                }),
              ),
              h(
                'span',
                {
                  style: {
                    fontSize: '12px',
                    fontWeight: 600,
                    minWidth: '50px',
                    textAlign: 'right',
                    color: 'var(--text-primary)',
                  },
                },
                (m.value || 0).toFixed(1),
              ),
            ],
          )
        }),
      )
    },

    renderBusinessContext(bc) {
      return h('div', { class: 'section' }, [
        h('div', { class: 'section-title' }, '业务上下文'),
        h(
          'div',
          { class: 'info-grid' },
          [
            bc.app_name ? this.infoCard('业务应用', bc.app_name) : null,
            bc.team ? this.infoCard('团队', bc.team) : null,
            bc.criticality ? this.infoCard('关键度', bc.criticality) : null,
            bc.business_unit ? this.infoCard('业务线', bc.business_unit) : null,
            bc.environment ? this.infoCard('环境', bc.environment) : null,
          ].filter(Boolean),
        ),
      ])
    },

    renderTimeline(events) {
      return h('div', { class: 'section' }, [
        h('div', { class: 'section-title' }, `事件时间线 (${events.length} 个事件)`),
        h(
          'div',
          { class: 'timeline' },
          events.map((ev, idx) => {
            const sev = (ev.severity || 'info').toLowerCase()
            const color = `var(--sev-${SEV_TONES[sev] || 'info'}-fg)`
            return h('div', { class: 'timeline-item', key: idx }, [
              h('div', { style: { position: 'relative', width: '16px', flexShrink: 0 } }, [
                h('div', { class: 'tl-dot', style: { color: color } }),
                idx < events.length - 1 ? h('div', { class: 'tl-line' }) : null,
              ]),
              h('div', { class: 'tl-content', style: { borderLeft: `4px solid ${color}` } }, [
                h('div', { class: 'tl-header' }, [
                  h('span', { class: 'tl-time' }, this.formatTime(ev.timestamp)),
                  this.severityBadge(sev, (ev.event_type || ev.eventType || '').toUpperCase()),
                  ev.source
                    ? h(
                        'span',
                        {
                          style: {
                            fontSize: '11px',
                            padding: '1px 6px',
                            borderRadius: '3px',
                            background: 'var(--st-neutral-bg)',
                            color: 'var(--text-secondary)',
                          },
                        },
                        ev.source,
                      )
                    : null,
                ]),
                h('div', { class: 'tl-desc' }, ev.description || '-'),
                ev.resource
                  ? h(
                      'div',
                      {
                        style: {
                          fontSize: '12px',
                          color: 'var(--text-muted)',
                          marginTop: '6px',
                          fontFamily: 'monospace',
                        },
                      },
                      ev.resource,
                    )
                  : null,
              ]),
            ])
          }),
        ),
      ])
    },

    renderCausalChain(chain) {
      return h('div', { class: 'section' }, [
        h('div', { class: 'section-title' }, '因果链分析'),
        chain.root_cause
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
                  { style: { fontSize: '14px', color: 'var(--sev-warning-fg)', lineHeight: '1.6' } },
                  chain.root_cause,
                ),
              ],
            )
          : null,
        ...(chain.links || []).map((link, idx) => {
          const evidenceList = Array.isArray(link.evidence)
            ? link.evidence.filter(Boolean)
            : link.evidence
              ? [link.evidence]
              : []
          const confPct = ((link.confidence || 0) * 100).toFixed(0)
          const confTone =
            link.confidence >= 0.8 ? 'pass' : link.confidence >= 0.5 ? 'warning' : 'error'
          return h('div', { class: 'causal-link', key: idx }, [
            h(
              'div',
              {
                style: {
                  display: 'flex',
                  alignItems: 'center',
                  gap: '8px',
                  marginBottom: '8px',
                  flexWrap: 'wrap',
                },
              },
              [
                h('span', { style: { fontWeight: 600, color: 'var(--sev-critical-fg)' } }, link.cause || '-'),
                h('span', { style: { color: 'var(--text-muted)' } }, '->'),
                h('span', { style: { fontWeight: 600, color: 'var(--accent)' } }, link.effect || '-'),
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
                  confPct + '%',
                ),
              ],
            ),
            evidenceList.length
              ? h(
                  'div',
                  {
                    style: {
                      fontSize: '12px',
                      color: 'var(--text-muted)',
                      background: 'var(--surface-inset)',
                      padding: '8px 12px',
                      borderRadius: '4px',
                    },
                  },
                  evidenceList.map((evidence, evidenceIdx) =>
                    h('div', { key: evidenceIdx }, evidence),
                  ),
                )
              : null,
          ])
        }),
      ])
    },

    renderTextField(title, value, variant) {
      const text = typeof value === 'string' ? value : value?.final || value?.ai_generated || ''
      if (!text) return null
      const bgColors = {
        warning: 'var(--sev-warning-bg)',
        success: 'var(--sev-pass-bg)',
        default: 'var(--surface-raised)',
      }
      const borderColors = {
        warning: 'var(--sev-warning-bd)',
        success: 'var(--sev-pass-bd)',
        default: 'var(--border-default)',
      }
      const bg = bgColors[variant] || bgColors.default
      const bc = borderColors[variant] || borderColors.default
      return h('div', { class: 'section' }, [
        h('div', { class: 'section-title' }, title),
        h(
          'div',
          {
            style: {
              background: bg,
              border: `1px solid ${bc}`,
              borderRadius: '8px',
              padding: '16px',
              lineHeight: '1.6',
              fontSize: '14px',
              color: 'var(--text-primary)',
              whiteSpace: 'pre-wrap',
            },
          },
          text,
        ),
      ])
    },

    renderEditableField(title, key, value, original) {
      const origText =
        typeof original === 'string' ? original : original?.final || original?.ai_generated || ''
      return h('div', { class: 'section edit-field' }, [
        h('div', { class: 'section-title' }, title),
        origText
          ? h(
              'div',
              { class: 'ai-original' },
              `AI 原文: ${origText.slice(0, 200)}${origText.length > 200 ? '...' : ''}`,
            )
          : null,
        h('textarea', {
          value: value,
          onInput: (e) => {
            this.editForm[key] = e.target.value
          },
        }),
      ])
    },

    renderLessonsLearned() {
      const list = this.editing
        ? this.editForm.lessons_learned
        : this.postmortem?.lessons_learned || []
      if (!list.length && !this.editing) return null
      return h('div', { class: 'section' }, [
        h('div', { class: 'section-title' }, '经验教训'),
        h(
          'div',
          {
            style: {
              background: 'var(--surface-raised)',
              border: '1px solid var(--border-default)',
              borderRadius: '8px',
              padding: '16px',
            },
          },
          list.map((item, idx) =>
            h(
              'div',
              {
                key: idx,
                style: {
                  display: 'flex',
                  gap: '8px',
                  padding: '8px 0',
                  borderBottom: idx < list.length - 1 ? '1px solid var(--border-subtle)' : 'none',
                  alignItems: 'flex-start',
                },
              },
              [
                h(
                  'span',
                  { style: { fontWeight: 600, color: 'var(--accent)', flexShrink: 0 } },
                  idx + 1 + '.',
                ),
                this.editing
                  ? h('input', {
                      value: item,
                      onInput: (e) => {
                        this.editForm.lessons_learned[idx] = e.target.value
                      },
                      style: {
                        flex: 1,
                        padding: '6px 10px',
                        border: '1px solid var(--border-default)',
                        borderRadius: '4px',
                        fontSize: '13px',
                      },
                    })
                  : h(
                      'span',
                      {
                        style: {
                          flex: 1,
                          fontSize: '13px',
                          color: 'var(--text-primary)',
                          lineHeight: '1.6',
                        },
                      },
                      item,
                    ),
                this.editing
                  ? h(
                      'button',
                      {
                        class: 'btn btn-danger btn-sm',
                        onClick: () => this.removeLesson(idx),
                        style: { flexShrink: 0 },
                      },
                      'X',
                    )
                  : null,
              ],
            ),
          ),
        ),
        this.editing
          ? h(
              'button',
              {
                class: 'btn btn-secondary btn-sm',
                onClick: this.addLesson,
                style: { marginTop: '8px' },
              },
              '+ 添加',
            )
          : null,
      ])
    },

    renderActionItems() {
      const list = this.editing ? this.editForm.action_items : this.postmortem?.action_items || []
      if (!list.length && !this.editing) return null
      return h('div', { class: 'section' }, [
        h('div', { class: 'section-title' }, '改进项'),
        h('table', { class: 'rh-table' }, [
          h(
            'thead',
            null,
            h('tr', null, [
              h('th', null, '描述'),
              h('th', { style: { width: '80px' } }, '优先级'),
              h('th', { style: { width: '100px' } }, '负责人'),
              h('th', { style: { width: '100px' } }, '截止日期'),
              this.editing ? h('th', { style: { width: '40px' } }, '') : null,
            ]),
          ),
          h(
            'tbody',
            null,
            list.map((item, idx) =>
              h('tr', { key: idx }, [
                h(
                  'td',
                  null,
                  this.editing
                    ? h('input', {
                        value: item.description || '',
                        onInput: (e) => {
                          this.editForm.action_items[idx].description = e.target.value
                        },
                        placeholder: '描述',
                      })
                    : item.description || '-',
                ),
                h(
                  'td',
                  null,
                  this.editing
                    ? h(
                        'select',
                        {
                          value: item.priority || 'medium',
                          onChange: (e) => {
                            this.editForm.action_items[idx].priority = e.target.value
                          },
                        },
                        [
                          h('option', { value: 'high' }, '高'),
                          h('option', { value: 'medium' }, '中'),
                          h('option', { value: 'low' }, '低'),
                        ],
                      )
                    : item.priority || '-',
                ),
                h(
                  'td',
                  null,
                  this.editing
                    ? h('input', {
                        value: item.owner || '',
                        onInput: (e) => {
                          this.editForm.action_items[idx].owner = e.target.value
                        },
                        placeholder: '负责人',
                      })
                    : item.owner || '-',
                ),
                h(
                  'td',
                  null,
                  this.editing
                    ? h('input', {
                        value: item.due_date || '',
                        onInput: (e) => {
                          this.editForm.action_items[idx].due_date = e.target.value
                        },
                        placeholder: 'YYYY-MM-DD',
                      })
                    : item.due_date || '-',
                ),
                this.editing
                  ? h(
                      'td',
                      null,
                      h(
                        'button',
                        {
                          class: 'btn btn-danger btn-sm',
                          onClick: () => this.removeActionItem(idx),
                        },
                        'X',
                      ),
                    )
                  : null,
              ]),
            ),
          ),
        ]),
        this.editing
          ? h(
              'button',
              {
                class: 'btn btn-secondary btn-sm',
                onClick: this.addActionItem,
                style: { marginTop: '8px' },
              },
              '+ 添加改进项',
            )
          : null,
      ])
    },

    renderAiDiagnosis(ad) {
      return h('div', { class: 'section' }, [
        h('div', { class: 'section-title' }, 'AI 诊断结论'),
        h(
          'div',
          {
            style: {
              background: 'var(--sev-warning-bg)',
              border: '1px solid var(--sev-warning-bd)',
              borderRadius: '8px',
              padding: '14px',
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
            ad.confidence
              ? h(
                  'div',
                  { style: { fontSize: '12px', color: 'var(--text-muted)' } },
                  '置信度: ' + Math.round(ad.confidence * 100) + '%',
                )
              : null,
            ad.evidence?.length
              ? h('div', { style: { marginTop: '8px' } }, [
                  h(
                    'div',
                    {
                      style: {
                        fontWeight: 600,
                        fontSize: '12px',
                        color: 'var(--text-muted)',
                        marginBottom: '6px',
                      },
                    },
                    '证据链',
                  ),
                  ...ad.evidence.map((ev, i) =>
                    h(
                      'div',
                      { key: i, style: { padding: '4px 0', fontSize: '13px', color: 'var(--text-secondary)' } },
                      '- ' + ev,
                    ),
                  ),
                ])
              : null,
            ad.remediation
              ? h(
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
                )
              : null,
          ],
        ),
      ])
    },
  },
  render() {
    return h('div', { class: 'retrospective-history-page' }, [
      h(NavBar),
      h('div', { class: 'container' }, [
        h('div', { class: 'dashboard-header' }, this.selectedFp ? '复盘报告详情' : '历史复盘报告'),

        this.error
          ? h('div', { class: 'error-msg' }, [
              h('span', { style: { fontWeight: 600, marginRight: '8px' } }, '[!]'),
              this.error,
            ])
          : null,

        this.selectedFp ? this.renderDetail() : this.renderList(),
      ]),
    ])
  },
}).mount('#app')
