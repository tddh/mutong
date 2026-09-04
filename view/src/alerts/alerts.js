import { createApp, ref, onMounted, h } from 'vue'
import { NavBar } from '../components/SharedComponents.js'
import { StatusBadge } from '../components/primitives.js'
import { API } from '../utils/api.js'
import '../styles/common.css'

const ACTION_NAMES_CN = {
  restart_pod: '重启 Pod',
  rollout_restart: '滚动重启',
  scale_deployment: '扩缩容',
  update_deployment_image: '修改镜像',
  adjust_resource_limits: '调整资源限制',
  update_resource_limits: '调整资源限制',
  delete_pod: '删除 Pod',
  rollout_undo: '回滚版本',
}

function actionText(action) {
  return ACTION_NAMES_CN[action] || (action || '').replace(/_/g, ' ')
}

function formatTime(ts) {
  if (!ts) return '-'
  return new Date(ts).toLocaleString('zh-CN')
}

function getAlertFingerprint(alert) {
  if (alert?.type === 'aggregated_alert') return alert.fingerprint || ''
  return alert?.fingerprint || ''
}

function getAlertSummary(alert) {
  if (alert?.type === 'aggregated_alert') return alert.summary || '聚合告警'
  return alert?.labels?.alertname || alert?.annotations?.summary || '-'
}

function getAlertSeverity(alert) {
  if (alert?.type === 'aggregated_alert') {
    const subs = alert.alerts || alert.children || alert.SubAlerts || []
    const severities = subs.map((s) => s.labels?.severity).filter(Boolean)
    if (severities.includes('critical')) return 'critical'
    if (severities.includes('error')) return 'error'
    if (severities.includes('warning')) return 'warning'
    return severities[0] || '-'
  }
  return alert?.labels?.severity || '-'
}

function getAlertCount(alert) {
  if (alert?.type === 'aggregated_alert') {
    return alert.alert_count != null ? alert.alert_count : alert.Alerts ? alert.Alerts.length : '-'
  }
  return 1
}

function getAlertStartsAt(alert) {
  if (alert?.type === 'aggregated_alert') return alert.starts_at || alert.startsAt || ''
  return alert?.startsAt || ''
}

function getBusinessApp(alert) {
  return (
    alert?.businessContext?.appName ||
    alert?.businessImpact?.primaryBusinessApp ||
    alert?.businessImpact?.affectedBusinessApps?.[0]?.appName ||
    '-'
  )
}

function getBusinessTeam(alert) {
  return alert?.businessContext?.team || '-'
}

function getBusinessCriticality(alert) {
  return alert?.businessContext?.criticality || '-'
}

/** 受影响业务应用卡片：外壳与说明块走语义面，暗色下不再是白底灰块 */
function businessAppCard(item, extra) {
  return h('div', { class: 'm-app-card' }, [
    h('div', { class: 'm-app-card-head' }, [
      h('span', { class: 'm-app-card-name' }, item.appName),
      extra || null,
    ]),
    h('div', { class: 'm-app-card-tags' }, [
      item.criticality ? criticalityBadge(item.criticality) : null,
      item.businessUnit
        ? h(StatusBadge, { sev: 'default', text: '业务线 ' + item.businessUnit })
        : null,
      item.team ? h(StatusBadge, { sev: 'info', text: '团队 ' + item.team }) : null,
    ]),
    item.reasoning ? h('div', { class: 'm-app-card-reason' }, item.reasoning) : null,
  ])
}

/** 严重度→语义档位；颜色由 CSS 的 [data-sev] 决定，不再有色表 */
export function severityBadge(severity) {
  const s = (severity || '').toLowerCase()
  const sev = s === 'critical' || s === 'error' || s === 'warning' || s === 'info' ? s : 'pending'
  return h(StatusBadge, { sev, text: severity || '—', dot: true })
}

const CRIT_LABEL = { critical: '核心', high: '重要', medium: '一般', low: '低' }
const CRIT_SEV = { critical: 'critical', high: 'warning', medium: 'pass', low: 'pending' }

export function criticalityBadge(criticality) {
  const c = (criticality || '').toLowerCase()
  return h(StatusBadge, { sev: CRIT_SEV[c] || 'pending', text: CRIT_LABEL[c] || criticality || '—' })
}

const RISK_LABEL = { critical: '严重', high: '高风险', medium: '中风险', low: '低风险' }
const RISK_SEV = { critical: 'critical', high: 'warning', medium: 'pending', low: 'pass' }

export function riskBadge(level) {
  const l = (level || '').toLowerCase()
  return h(StatusBadge, { sev: RISK_SEV[l] || 'pending', text: RISK_LABEL[l] || level || '—', dot: true })
}

function infoRow(label, value) {
  return h('tr', { class: 'm-info-row' }, [
    h('th', { class: 'm-info-k', scope: 'row' }, label),
    h('td', { class: 'm-info-v' }, value != null ? value : '—'),
  ])
}

/** infoRow 产出行，宽度与分隔线由这个表格壳承载 */
function infoTable(rows) {
  return h('table', { class: 'm-info-tbl' }, h('tbody', null, rows))
}

function sectionBlock(title, content) {
  return h('section', { class: 'm-detail-section' }, [
    h('h4', { class: 'm-detail-title' }, title),
    content,
  ])
}

createApp({
  components: { NavBar },
  setup() {
    const alerts = ref([])
    const loading = ref(true)
    const error = ref(null)
    const expanded = ref({})
    const selectedAlert = ref(null)
    const activeTab = ref('basic')
    const diagnosisResult = ref(null)
    const diagnosisLoading = ref(false)
    const diagnosisError = ref(null)
    const rerunningDiagnosis = ref(false)
    const executionRecords = ref(null)

    // 业务过滤器状态
    const businessFilterApp = ref('')
    const showResolved = ref(false)
    const businessFilterTeam = ref('')
    const businessFilterCriticality = ref('')

    const toggleAgg = (id) => {
      expanded.value[id] = !expanded.value[id]
    }

    const selectAlert = (alert) => {
      selectedAlert.value = alert
      activeTab.value = 'basic'
      diagnosisResult.value = null
      diagnosisLoading.value = false
      diagnosisError.value = null
      executionRecords.value = null
    }

    const closeDrawer = () => {
      selectedAlert.value = null
      diagnosisResult.value = null
      diagnosisLoading.value = false
      diagnosisError.value = null
      executionRecords.value = null
    }

    const switchTab = (tab) => {
      activeTab.value = tab
      if (tab === 'diagnosis' && !diagnosisResult.value && !diagnosisLoading.value) {
        runDiagnosis()
      }
      if (tab === 'diagnosis') {
        loadExecutionRecords()
      }
    }

    const runDiagnosis = async () => {
      const fp = getAlertFingerprint(selectedAlert.value)
      if (!fp) {
        diagnosisError.value = '无法获取告警指纹，无法执行诊断'
        return
      }
      diagnosisLoading.value = true
      diagnosisError.value = null
      diagnosisResult.value = null
      try {
        diagnosisResult.value = await API.diagnosis.run(fp)
      } catch (e) {
        diagnosisError.value = e.message || '诊断执行失败'
      } finally {
        diagnosisLoading.value = false
      }
    }

    const rerunDiagnosis = async () => {
      const fp = getAlertFingerprint(selectedAlert.value)
      if (!fp) {
        diagnosisError.value = '无法获取告警指纹，无法执行诊断'
        return
      }
      rerunningDiagnosis.value = true
      diagnosisError.value = null
      try {
        diagnosisResult.value = await API.diagnosis.rerun(fp)
      } catch (e) {
        diagnosisError.value = e.message || '重新诊断执行失败'
      } finally {
        rerunningDiagnosis.value = false
      }
    }

    const loadExecutionRecords = async () => {
      const fp = getAlertFingerprint(selectedAlert.value)
      if (!fp) {
        executionRecords.value = []
        return
      }
      try {
        executionRecords.value = await API.executor.audit({ fingerprint: fp })
      } catch (e) {
        executionRecords.value = []
      }
    }

    const approvingId = ref('')
    const approveExecution = async (rec) => {
      const p = rec.plan || {}
      const target = p.target || (p.namespace ? p.namespace + '/' + p.resourceName : p.resourceName)
      const actText = actionText(p.action)
      const detailLines = []
      if (p.image) detailLines.push('镜像: ' + p.image)
      if (p.containerName) detailLines.push('容器: ' + p.containerName)
      if (p.configData && Object.keys(p.configData).length) {
        detailLines.push('资源: ' + Object.entries(p.configData).map(([k, v]) => k + '=' + v).join(', '))
      }
      if (p.action === 'scale_deployment' && (p.replicas || p.replicas === 0)) detailLines.push('副本数: ' + p.replicas)
      if (p.action === 'rollout_undo') detailLines.push(p.revision ? '回滚到版本: ' + p.revision : '回滚到上一版本')
      const detail = detailLines.length ? '\n' + detailLines.join('\n') : ''
      if (!window.confirm(`确认批准并执行？\n动作: ${actText}\n目标: ${target}\n风险: ${p.risk || '-'}${detail}`)) {
        return
      }
      approvingId.value = rec.id
      try {
        const res = await API.executor.approve(rec.id)
        if (res && res.success) {
          window.alert('执行成功: ' + (res.message || ''))
        } else {
          window.alert('执行失败: ' + ((res && res.message) || '未知错误'))
        }
      } catch (e) {
        window.alert('审批请求失败: ' + e.message)
      } finally {
        approvingId.value = ''
        await loadExecutionRecords()
      }
    }

    const loadAlerts = async () => {
      loading.value = true
      try {
        const r = await API.alerts.list({ status: showResolved.value ? 'all' : 'firing' })
        alerts.value = Array.isArray(r) ? r : r.alerts || []
        filteredAlerts.value = alerts.value
      } catch (e) {
        error.value = e.message
      } finally {
        loading.value = false
      }
    }

    onMounted(loadAlerts)

    const filteredAlerts = ref([])
    const applyBusinessFilters = () => {
      const appFilter = businessFilterApp.value.toLowerCase().trim()
      const teamFilter = businessFilterTeam.value.toLowerCase().trim()
      const critFilter = businessFilterCriticality.value.toLowerCase().trim()

      if (!appFilter && !teamFilter && !critFilter) {
        filteredAlerts.value = alerts.value
        return
      }

      filteredAlerts.value = alerts.value.filter((a) => {
        const bc = a.businessContext || {}
        const bi = a.businessImpact || {}
        const app = (
          bc.appName ||
          bi.primaryBusinessApp ||
          bi.affectedBusinessApps?.[0]?.appName ||
          ''
        ).toLowerCase()
        const team = (bc.team || '').toLowerCase()
        const crit = (bc.criticality || '').toLowerCase()

        const matchApp = !appFilter || app.includes(appFilter)
        const matchTeam = !teamFilter || team.includes(teamFilter)
        const matchCrit = !critFilter || crit.includes(critFilter)

        return matchApp && matchTeam && matchCrit
      })
    }

    return {
      alerts,
      loading,
      error,
      expanded,
      toggleAgg,
      selectedAlert,
      activeTab,
      diagnosisResult,
      diagnosisLoading,
      diagnosisError,
      rerunningDiagnosis,
      executionRecords,
      approveExecution,
      approvingId,
      selectAlert,
      closeDrawer,
      switchTab,
      runDiagnosis,
      rerunDiagnosis,
      businessFilterApp,
      showResolved,
      loadAlerts,
      businessFilterTeam,
      businessFilterCriticality,
      filteredAlerts,
      applyBusinessFilters,
    }
  },
  methods: {
    buildBusinessFilterBar() {
      const filterStyle = {
        display: 'flex',
        gap: '12px',
        padding: '10px 20px',
        borderBottom: '1px solid var(--border-color)',
        background: '#fafbfc',
        alignItems: 'center',
        flexWrap: 'wrap',
      }
      const labelStyle = {
        fontSize: '12px',
        color: 'var(--text-secondary)',
        fontWeight: 500,
        whiteSpace: 'nowrap',
      }
      const inputStyle = {
        padding: '4px 8px',
        border: '1px solid var(--border-color)',
        borderRadius: '4px',
        fontSize: '13px',
        outline: 'none',
        background: '#fff',
        minWidth: '120px',
      }
      const selectStyle = { ...inputStyle, cursor: 'pointer' }
      const clearBtnStyle = {
        padding: '4px 12px',
        background: 'none',
        border: '1px solid var(--border-color)',
        borderRadius: '4px',
        fontSize: '12px',
        color: 'var(--text-secondary)',
        cursor: 'pointer',
      }

      return h('div', { style: filterStyle }, [
        h(
          'span',
          {
            style: {
              fontSize: '12px',
              fontWeight: 600,
              color: 'var(--text-primary)',
              marginRight: '4px',
            },
          },
          '🔍 业务筛选:',
        ),
        h('span', { style: labelStyle }, '应用:'),
        h('input', {
          type: 'text',
          placeholder: '输入业务应用名称',
          value: this.businessFilterApp,
          onInput: (e) => {
            this.businessFilterApp = e.target.value
            this.applyBusinessFilters()
          },
          style: inputStyle,
        }),
        h('span', { style: labelStyle }, '团队:'),
        h('input', {
          type: 'text',
          placeholder: '输入团队名称',
          value: this.businessFilterTeam,
          onInput: (e) => {
            this.businessFilterTeam = e.target.value
            this.applyBusinessFilters()
          },
          style: inputStyle,
        }),
        h('span', { style: labelStyle }, '重要等级:'),
        h(
          'select',
          {
            value: this.businessFilterCriticality,
            onChange: (e) => {
              this.businessFilterCriticality = e.target.value
              this.applyBusinessFilters()
            },
            style: selectStyle,
          },
          [
            h('option', { value: '' }, '全部'),
            h('option', { value: 'critical' }, '核心'),
            h('option', { value: 'high' }, '重要'),
            h('option', { value: 'medium' }, '一般'),
            h('option', { value: 'low' }, '低'),
          ],
        ),
        h('label', { style: { ...labelStyle, display: 'inline-flex', alignItems: 'center', gap: '4px', cursor: 'pointer' } }, [
          h('input', {
            type: 'checkbox',
            checked: this.showResolved,
            onChange: (e) => {
              this.showResolved = e.target.checked
              this.loadAlerts()
            },
          }),
          '显示已恢复',
        ]),
        this.businessFilterApp || this.businessFilterTeam || this.businessFilterCriticality
          ? h(
              'button',
              {
                onClick: () => {
                  this.businessFilterApp = ''
                  this.businessFilterTeam = ''
                  this.businessFilterCriticality = ''
                  this.applyBusinessFilters()
                },
                style: clearBtnStyle,
              },
              '清除筛选',
            )
          : null,
      ])
    },
    criticalityBadge(criticality) {
      if (!criticality || criticality === '-') {
        return h(
          'span',
          {
            style: {
              display: 'inline-block',
              padding: '2px 8px',
              borderRadius: '4px',
              fontSize: '12px',
              color: 'var(--text-secondary)',
              background: '#f5f5f5',
            },
          },
          '-',
        )
      }
      return criticalityBadge(criticality)
    },
    groupResourcesByKind(resources) {
      const groups = {}
      for (const ref of resources) {
        const kind = ref.kind || ref.Kind || 'Unknown'
        if (!groups[kind]) groups[kind] = []
        groups[kind].push(ref)
      }
      return Object.entries(groups)
        .sort((a, b) => a[0].localeCompare(b[0]))
        .map(([kind, refs]) => ({
          kind,
          refs: refs.slice().sort((a, b) => {
            const aNs = a.namespace || a.Namespace || ''
            const bNs = b.namespace || b.Namespace || ''
            const aName = a.name || a.Name || ''
            const bName = b.name || b.Name || ''
            return `${aNs}/${aName}`.localeCompare(`${bNs}/${bName}`)
          }),
        }))
    },
    buildImpactGroup(title, grouped) {
      const rows = []
      for (const group of grouped) {
        const kindHeader = h(
          'div',
          {
            style: {
              fontSize: '12px',
              fontWeight: 600,
              color: 'var(--text-secondary)',
              padding: '8px 0 4px',
              borderTop: rows.length > 0 ? '1px solid #f0f0f0' : 'none',
              letterSpacing: '0.5px',
            },
          },
          group.kind + ' (' + group.refs.length + ')',
        )
        rows.push(kindHeader)
        for (const ref of group.refs) {
          const namespace = ref.namespace || ref.Namespace || ''
          const name = ref.name || ref.Name || '-'
          const displayName = namespace ? namespace + '/' + name : name
          rows.push(
            h(
              'div',
              {
                style: {
                  fontSize: '13px',
                  padding: '4px 0 4px 12px',
                  color: 'var(--text-primary)',
                  lineHeight: '1.5',
                  fontFamily: 'monospace',
                },
              },
              '• ' + displayName,
            ),
          )
        }
      }
      return h(
        'div',
        {
          style: {
            background: '#fff',
            border: '1px solid var(--border-color)',
            borderRadius: '6px',
            padding: '12px',
            marginBottom: '8px',
          },
        },
        [
          h(
            'div',
            {
              style: {
                fontSize: '13px',
                fontWeight: 600,
                marginBottom: '8px',
                color: 'var(--text-primary)',
              },
            },
            title,
          ),
        ].concat(rows),
      )
    },
    buildDrawerContent() {
      if (this.activeTab === 'basic') return this.buildBasicTab()
      return this.buildDiagnosisTab()
    },
    buildBasicTab() {
      const a = this.selectedAlert
      if (!a) return null
      const sections = []
      const basicRows = []
      const fp = getAlertFingerprint(a)
      if (fp) basicRows.push(infoRow('指纹', fp))
      basicRows.push(infoRow('告警名称', getAlertSummary(a)))
      basicRows.push(infoRow('严重级别', getAlertSeverity(a)))
      basicRows.push(infoRow('开始时间', formatTime(getAlertStartsAt(a))))
      basicRows.push(infoRow('状态', a.status === 'resolved' ? '✅ 已恢复' : '🔥 触发中'))
      if (a.status === 'resolved' && a.endsAt) basicRows.push(infoRow('恢复时间', formatTime(a.endsAt)))
      if (a?.type === 'aggregated_alert') basicRows.push(infoRow('告警数量', getAlertCount(a)))
      if (a.labels && Object.keys(a.labels).length > 0)
        basicRows.push(
          infoRow(
            '标签',
            Object.entries(a.labels)
              .map(([k, v]) => k + '=' + v)
              .join(', '),
          ),
        )
      if (a.annotations && Object.keys(a.annotations).length > 0) {
        for (const [key, value] of Object.entries(a.annotations))
          basicRows.push(infoRow(key, String(value)))
      }
      if (a.status) basicRows.push(infoRow('状态', a.status))
      sections.push(
        sectionBlock(
          '📋 基本信息',
          infoTable(basicRows),
        ),
      )

      // 业务上下文区块
      const bc = a.businessContext
      if (
        bc &&
        (bc.appName || bc.team || bc.businessUnit || bc.criticality || bc.environment || bc.source)
      ) {
        const bcRows = []
        if (bc.appName) bcRows.push(infoRow('业务应用', bc.appName))
        if (bc.businessUnit) bcRows.push(infoRow('业务单元', bc.businessUnit))
        if (bc.team) bcRows.push(infoRow('负责团队', bc.team))
        if (bc.criticality) {
          bcRows.push(
            infoRow(
              '重要等级',
              criticalityBadge(bc.criticality),
            ),
          )
        }
        if (bc.environment) bcRows.push(infoRow('环境', bc.environment))
        if (bc.source) bcRows.push(infoRow('富化来源', bc.source))
        sections.push(
          sectionBlock(
            '🏢 业务上下文',
            infoTable(bcRows),
          ),
        )
      } else {
        sections.push(
          sectionBlock(
            '🏢 业务上下文',
            h(
              'div',
              { style: { fontSize: '13px', color: 'var(--text-secondary)', padding: '8px 0' } },
              '暂无业务上下文信息',
            ),
          ),
        )
      }

      // 业务调用链区块
      const bcCalls = a.businessCalls
      if (bcCalls && (bcCalls.upstreams?.length || bcCalls.downstreams?.length)) {
        const depChildren = []
        if (bcCalls.upstreams?.length) {
          depChildren.push(
            h(
              'div',
              null,
              [
                h(
                  'div',
                  { style: { fontWeight: 600, marginBottom: '4px', color: 'var(--text-primary)' } },
                  '上游调用方 (' + bcCalls.upstreams.length + ')',
                ),
              ].concat(
                bcCalls.upstreams.map((u, i) =>
                  h('div', { key: i, class: 'm-chain-item' }, [
                    h('span', { class: 'm-chain-name' }, u.appName || '—'),
                    u.team ? h('span', { class: 'm-chain-team' }, u.team) : null,
                    u.criticality ? criticalityBadge(u.criticality) : null,
                  ]),
                ),
              ),
            ),
          )
        }
        if (bcCalls.downstreams?.length) {
          depChildren.push(
            h(
              'div',
              null,
              [
                h(
                  'div',
                  {
                    style: {
                      fontWeight: 600,
                      marginBottom: '4px',
                      color: 'var(--text-primary)',
                      marginTop: bcCalls.upstreams?.length ? '8px' : '0',
                    },
                  },
                  '下游依赖方 (' + bcCalls.downstreams.length + ')',
                ),
              ].concat(
                bcCalls.downstreams.map((d, i) =>
                  h('div', { key: i, class: 'm-chain-item' }, [
                    h('span', { class: 'm-chain-name' }, d.appName || '—'),
                    d.team ? h('span', { class: 'm-chain-team' }, d.team) : null,
                    d.criticality ? criticalityBadge(d.criticality) : null,
                  ]),
                ),
              ),
            ),
          )
        }
        if (depChildren.length > 0) {
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
                depChildren,
              ),
            ),
          )
        }
      }

      // Node 告警业务影响区块
      const bi = a.businessImpact
      if (
        bi &&
        (bi.affectedBusinessApps ||
          bi.affectedAppCount != null ||
          bi.containsCriticalApps ||
          bi.primaryBusinessApp)
      ) {
        const biRows = []
        if (bi.primaryBusinessApp) biRows.push(infoRow('主要影响业务', bi.primaryBusinessApp))
        if (bi.affectedAppCount != null)
          biRows.push(infoRow('影响业务数量', bi.affectedAppCount + ' 个'))
        if (bi.containsCriticalApps)
          biRows.push(
            infoRow(
              '含核心应用',
              h('span', { style: { color: 'var(--error-color)', fontWeight: 600 } }, '⚠️ 是'),
            ),
          )
        if (bi.affectedBusinessApps && bi.affectedBusinessApps.length > 0) {
          biRows.push(
            infoRow(
              '受影响业务列表',
              bi.affectedBusinessApps
                .map((app) => {
                  if (!app || typeof app !== 'object') return String(app || '-')
                  return app.criticality ? `${app.appName} (${app.criticality})` : app.appName
                })
                .join(', '),
            ),
          )
        }
        sections.push(
          sectionBlock(
            '📊 业务影响',
            infoTable(biRows),
          ),
        )
      }
      if (a?.type === 'aggregated_alert') {
        const subs = a.alerts || a.children || a.SubAlerts || []
        if (subs.length > 0) {
          const childRows = subs.map((c, idx) =>
            h('tr', { key: idx, style: { borderBottom: '1px solid #f0f0f0' } }, [
              h('td', { style: { padding: '6px 0', fontSize: '12px' } }, [
                severityBadge(c.labels?.severity || '-'),
                ' ' + (c.labels?.alertname || '-'),
              ]),
              h(
                'td',
                {
                  style: {
                    padding: '6px 0',
                    fontSize: '12px',
                    color: 'var(--text-secondary)',
                    textAlign: 'right',
                  },
                },
                formatTime(c.startsAt),
              ),
            ]),
          )
          sections.push(
            sectionBlock(
              '📦 子告警 (' + subs.length + ')',
              infoTable(childRows),
            ),
          )
        }
      }
      return h('div', null, sections)
    },
    buildDiagnosisTab() {
      if (this.diagnosisLoading) {
        return h(
          'div',
          { style: { textAlign: 'center', padding: '40px 0', color: 'var(--text-secondary)' } },
          [
            h('div', { style: { fontSize: '28px', marginBottom: '12px' } }, '⏳'),
            h('div', { style: { fontSize: '14px' } }, 'AI 诊断执行中...'),
            h(
              'div',
              { style: { fontSize: '12px', marginTop: '4px', color: '#bbb' } },
              '这可能需要几秒钟',
            ),
          ],
        )
      }
      if (this.diagnosisError) {
        return h(
          'div',
          {
            style: {
              background: '#fff2f0',
              border: '1px solid #ffccc7',
              borderRadius: 'var(--card-radius)',
              padding: '16px',
              color: 'var(--error-color)',
              fontSize: '13px',
            },
          },
          [
            h('div', { style: { fontWeight: 600, marginBottom: '4px' } }, '⚠️ 诊断失败'),
            h('div', null, this.diagnosisError),
            h(
              'button',
              {
                onClick: this.runDiagnosis,
                style: {
                  marginTop: '12px',
                  padding: '6px 16px',
                  background: 'var(--primary-color)',
                  color: '#fff',
                  border: 'none',
                  borderRadius: '4px',
                  cursor: 'pointer',
                  fontSize: '13px',
                },
              },
              '🔄 重试',
            ),
          ],
        )
      }
      if (!this.diagnosisResult) {
        return h(
          'div',
          { style: { textAlign: 'center', padding: '40px 0', color: 'var(--text-secondary)' } },
          [
            h('div', { style: { fontSize: '32px', marginBottom: '12px' } }, '🤖'),
            h(
              'div',
              { style: { fontSize: '14px', marginBottom: '16px' } },
              '进入 AI 诊断对话页面进行深度排查',
            ),
            h(
              'button',
              {
                onClick: () => {
                  const fp = this.selectedAlert?.fingerprint
                  if (fp) {
                    window.location.href = `/view/diagnosis/index.html?alert_fingerprint=${encodeURIComponent(fp)}`
                  }
                },
                style: {
                  padding: '8px 24px',
                  background: 'var(--primary-color)',
                  color: '#fff',
                  border: 'none',
                  borderRadius: '6px',
                  cursor: 'pointer',
                  fontSize: '14px',
                  fontWeight: 500,
                  transition: 'background 0.2s',
                },
                onMouseenter: (e) => {
                  e.currentTarget.style.background = '#40a9ff'
                },
                onMouseleave: (e) => {
                  e.currentTarget.style.background = 'var(--primary-color)'
                },
              },
              '🚀 进入 AI 诊断',
            ),
          ],
        )
      }
      const r = this.diagnosisResult
      const sections = []
      sections.push(
        h('div', { style: { display: 'flex', justifyContent: 'flex-end', marginBottom: '12px' } }, [
          h(
            'button',
            {
              onClick: this.rerunDiagnosis,
              disabled: this.rerunningDiagnosis,
              style: {
                padding: '4px 12px',
                background: 'none',
                border: '1px solid var(--primary-color)',
                borderRadius: '4px',
                cursor: this.rerunningDiagnosis ? 'not-allowed' : 'pointer',
                fontSize: '12px',
                color: 'var(--primary-color)',
                opacity: this.rerunningDiagnosis ? 0.5 : 1,
                transition: 'all 0.2s',
              },
              onMouseenter: (e) => {
                if (!this.rerunningDiagnosis) {
                  e.currentTarget.style.background = 'var(--primary-color)'
                  e.currentTarget.style.color = '#fff'
                }
              },
              onMouseleave: (e) => {
                e.currentTarget.style.background = 'none'
                e.currentTarget.style.color = 'var(--primary-color)'
              },
            },
            this.rerunningDiagnosis ? '⏳ 重新诊断中...' : '🔄 重新诊断',
          ),
        ]),
      )
      if (r.summary) {
        sections.push(
          sectionBlock(
            '📝 诊断摘要',
            h(
              'div',
              {
                style: {
                  fontSize: '13px',
                  lineHeight: '1.6',
                  color: 'var(--text-primary)',
                  background: '#fff',
                  padding: '12px',
                  borderRadius: '6px',
                  border: '1px solid var(--border-color)',
                },
              },
              r.summary,
            ),
          ),
        )
      }
      // 业务影响分析区块（始终显示）
      {
        const bizImpact = r.impact?.businessImpact
        const hasDirect = bizImpact?.directImpacts?.length > 0
        const hasIndirect = bizImpact?.indirectImpacts?.length > 0

        const bizChildren = []

        if (hasDirect || hasIndirect) {
          bizChildren.push(h('div', { class: 'm-risk-line' }, riskBadge(bizImpact.riskLevel)))
          if (bizImpact.summary)
            bizChildren.push(
              h(
                'div',
                {
                  style: {
                    fontSize: '13px',
                    color: 'var(--text-secondary)',
                    lineHeight: '1.5',
                    marginBottom: '12px',
                  },
                },
                bizImpact.summary,
              ),
            )

          if (hasDirect) {
            bizChildren.push(
              h(
                'div',
                {
                  style: {
                    fontWeight: 600,
                    fontSize: '12px',
                    color: 'var(--text-primary)',
                    marginBottom: '8px',
                  },
                },
                '🔴 直接影响的业务应用',
              ),
            )
            for (const item of bizImpact.directImpacts) {
              bizChildren.push(businessAppCard(item))
            }
          }
          if (hasIndirect) {
            bizChildren.push(
              h(
                'div',
                {
                  style: {
                    fontWeight: 600,
                    fontSize: '12px',
                    color: 'var(--text-primary)',
                    marginBottom: '8px',
                    marginTop: '8px',
                  },
                },
                '🟡 间接影响的业务应用 (下游链路)',
              ),
            )
            for (const item of bizImpact.indirectImpacts) {
              bizChildren.push(
                businessAppCard(
                  item,
                  item.direction
                    ? h(StatusBadge, { sev: 'pending', text: item.direction })
                    : null,
                ),
              )
            }
          }
        } else {
          bizChildren.push(
            h(
              'div',
              {
                style: {
                  padding: '8px 0',
                  color: 'var(--text-secondary)',
                  fontSize: '12px',
                  textAlign: 'center',
                },
              },
              '暂无业务影响分析数据',
            ),
          )
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
              bizChildren,
            ),
          ),
        )
      }
      if (
        r.business_app_calls &&
        (r.business_app_calls.upstreams?.length || r.business_app_calls.downstreams?.length)
      ) {
        const c = r.business_app_calls
        const depChildren = []
        if (c.upstreams?.length) {
          depChildren.push(
            h(
              'div',
              null,
              [
                h(
                  'div',
                  { style: { fontWeight: 600, marginBottom: '4px', color: 'var(--text-primary)' } },
                  '上游调用方 (' + c.upstreams.length + ')',
                ),
              ].concat(
                c.upstreams.map((u, i) =>
                  h('div', { key: i, class: 'm-chain-item' }, [
                    h('span', { class: 'm-chain-name' }, u.appName || '—'),
                    u.team ? h('span', { class: 'm-chain-team' }, u.team) : null,
                    u.criticality ? criticalityBadge(u.criticality) : null,
                  ]),
                ),
              ),
            ),
          )
        }
        if (c.downstreams?.length) {
          depChildren.push(
            h(
              'div',
              null,
              [
                h(
                  'div',
                  {
                    style: {
                      fontWeight: 600,
                      marginBottom: '4px',
                      color: 'var(--text-primary)',
                      marginTop: c.upstreams?.length ? '8px' : '0',
                    },
                  },
                  '下游依赖方 (' + c.downstreams.length + ')',
                ),
              ].concat(
                c.downstreams.map((d, i) =>
                  h('div', { key: i, class: 'm-chain-item' }, [
                    h('span', { class: 'm-chain-name' }, d.appName || '—'),
                    d.team ? h('span', { class: 'm-chain-team' }, d.team) : null,
                    d.criticality ? criticalityBadge(d.criticality) : null,
                  ]),
                ),
              ),
            ),
          )
        }
        if (depChildren.length > 0) {
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
                depChildren,
              ),
            ),
          )
        }
      }
      if (r.root_causes && r.root_causes.length > 0) {
        const causeCards = r.root_causes.map((cause, idx) => {
          const confColor =
            cause.confidence >= 0.8
              ? 'var(--success-color)'
              : cause.confidence >= 0.5
                ? 'var(--warning-color)'
                : 'var(--error-color)'
          const confBg =
            cause.confidence >= 0.8 ? '#e6f7ee' : cause.confidence >= 0.5 ? '#fff7e6' : '#fff1f0'
          const children = [
            h(
              'div',
              {
                style: {
                  display: 'flex',
                  justifyContent: 'space-between',
                  alignItems: 'center',
                  marginBottom: '6px',
                },
              },
              [
                h(
                  'span',
                  { style: { fontWeight: 600, fontSize: '13px' } },
                  (cause.resource_type || '') + ': ' + (cause.resource_name || ''),
                ),
                h(
                  'span',
                  {
                    style: {
                      fontSize: '11px',
                      padding: '2px 8px',
                      borderRadius: '10px',
                      background: confBg,
                      color: confColor,
                      fontWeight: 500,
                    },
                  },
                  '置信度 ' + (cause.confidence * 100).toFixed(0) + '%',
                ),
              ],
            ),
          ]
          if (cause.namespace)
            children.push(
              h(
                'div',
                {
                  style: { fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '6px' },
                },
                '命名空间: ' + cause.namespace,
              ),
            )
          if (cause.evidence && cause.evidence.length > 0) {
            children.push(
              h(
                'div',
                { style: { fontSize: '12px', color: 'var(--text-secondary)' } },
                [h('div', { style: { fontWeight: 500, marginBottom: '4px' } }, '证据:')].concat(
                  cause.evidence.map((ev, i) =>
                    h(
                      'div',
                      {
                        key: i,
                        style: {
                          padding: '4px 8px',
                          background: 'var(--bg-color)',
                          borderRadius: '4px',
                          marginBottom: i < cause.evidence.length - 1 ? '4px' : 0,
                          lineHeight: '1.4',
                        },
                      },
                      '• ' + ev,
                    ),
                  ),
                ),
              ),
            )
          }
          return h(
            'div',
            {
              key: idx,
              style: {
                background: '#fff',
                border: '1px solid var(--border-color)',
                borderRadius: '6px',
                padding: '12px',
                marginBottom: idx < r.root_causes.length - 1 ? '8px' : 0,
              },
            },
            children,
          )
        })
        sections.push(sectionBlock('🔍 根因分析', h('div', null, causeCards)))
      }
      if (r.impact) {
        const impact = r.impact
        const impactChildren = []
        if (impact.severity) impactChildren.push(infoRow('影响严重程度', impact.severity))
        if (impact.blast_radius != null)
          impactChildren.push(infoRow('影响范围', impact.blast_radius + ' 个资源'))
        if (impact.user_facing_impact)
          impactChildren.push(
            infoRow(
              '用户影响',
              h(
                'span',
                { style: { color: 'var(--error-color)', fontWeight: 600 } },
                '⚠️ 存在用户侧影响',
              ),
            ),
          )
        if (impact.affected_services && impact.affected_services.length > 0)
          impactChildren.push(infoRow('受影响服务', impact.affected_services.join(', ')))

        const directGrouped = this.groupResourcesByKind(impact.direct_impact || [])
        const indirectGrouped = this.groupResourcesByKind(impact.indirect_impact || [])
        const impactSections = []

        if (impactChildren.length > 0) {
          impactSections.push(
            h(
              'table',
              { class: ['m-info-tbl', { 'has-more': directGrouped.length > 0 || indirectGrouped.length > 0 }] },
              h('tbody', null, impactChildren),
            ),
          )
        }
        if (directGrouped.length > 0) {
          impactSections.push(this.buildImpactGroup('直接影响', directGrouped))
        }
        if (indirectGrouped.length > 0) {
          impactSections.push(this.buildImpactGroup('间接影响', indirectGrouped))
        }
        if (impactSections.length > 0) {
          sections.push(sectionBlock('📊 影响评估', h('div', null, impactSections)))
        }
      }
      if (r.remediations && r.remediations.length > 0) {
        const remediationCards = r.remediations.map((rem, idx) => {
          const riskColor =
            rem.risk_level === 'low'
              ? 'var(--success-color)'
              : rem.risk_level === 'medium'
                ? 'var(--warning-color)'
                : 'var(--error-color)'
          const riskBg =
            rem.risk_level === 'low'
              ? '#e6f7ee'
              : rem.risk_level === 'medium'
                ? '#fff7e6'
                : '#fff1f0'
          const children = [
            h(
              'div',
              {
                style: {
                  display: 'flex',
                  justifyContent: 'space-between',
                  alignItems: 'center',
                  marginBottom: '6px',
                },
              },
              [
                h(
                  'span',
                  { style: { fontWeight: 600, fontSize: '13px' } },
                  rem.action || '修复操作',
                ),
                h(
                  'span',
                  {
                    style: {
                      fontSize: '11px',
                      padding: '2px 8px',
                      borderRadius: '10px',
                      background: riskBg,
                      color: riskColor,
                      fontWeight: 500,
                    },
                  },
                  '风险: ' + (rem.risk_level || 'unknown'),
                ),
              ],
            ),
          ]
          if (rem.description)
            children.push(
              h(
                'div',
                {
                  style: {
                    fontSize: '12px',
                    color: 'var(--text-secondary)',
                    marginBottom: '8px',
                    lineHeight: '1.5',
                  },
                },
                rem.description,
              ),
            )
          if (rem.steps && rem.steps.length > 0) {
            children.push(
              h(
                'div',
                null,
                [
                  h(
                    'div',
                    {
                      style: {
                        fontSize: '12px',
                        fontWeight: 500,
                        marginBottom: '4px',
                        color: 'var(--text-primary)',
                      },
                    },
                    '执行步骤:',
                  ),
                ].concat(
                  rem.steps.map((step, i) =>
                    h(
                      'div',
                      {
                        key: i,
                        style: {
                          fontSize: '12px',
                          padding: '4px 8px',
                          background: 'var(--bg-color)',
                          borderRadius: '4px',
                          marginBottom: i < rem.steps.length - 1 ? '4px' : 0,
                          lineHeight: '1.4',
                          fontFamily: 'monospace',
                        },
                      },
                      i + 1 + '. ' + step,
                    ),
                  ),
                ),
              ),
            )
          }
          if (rem.auto_fixable)
            children.push(
              h(
                'div',
                {
                  style: {
                    marginTop: '8px',
                    fontSize: '11px',
                    color: 'var(--success-color)',
                    fontWeight: 500,
                  },
                },
                '✅ 支持自动修复',
              ),
            )
          return h(
            'div',
            {
              key: idx,
              style: {
                background: '#fff',
                border: '1px solid var(--border-color)',
                borderRadius: '6px',
                padding: '12px',
                marginBottom: idx < r.remediations.length - 1 ? '8px' : 0,
              },
            },
            children,
          )
        })
        sections.push(sectionBlock('🔧 修复建议', h('div', null, remediationCards)))
      }
      if (r.metrics && r.metrics.length > 0) {
        const metricItems = r.metrics.map((m, idx) =>
          h(
            'div',
            {
              key: idx,
              style: {
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                padding: '8px 12px',
                background: '#fff',
                borderRadius: '4px',
                marginBottom: idx < r.metrics.length - 1 ? '6px' : 0,
                border: '1px solid var(--border-color)',
                fontSize: '12px',
              },
            },
            [
              h(
                'span',
                { style: { color: 'var(--text-secondary)' } },
                m.resourceKind + '/' + m.resourceName + ': ' + m.metricName,
              ),
              h(
                'span',
                {
                  style: {
                    fontWeight: 600,
                    color:
                      m.status === 'critical'
                        ? 'var(--error-color)'
                        : m.status === 'warning'
                          ? 'var(--warning-color)'
                          : 'var(--text-primary)',
                  },
                },
                m.value,
              ),
            ],
          ),
        )
        sections.push(sectionBlock('📈 相关指标', h('div', null, metricItems)))
      }
      if (r.recentLogs && r.recentLogs.length > 0) {
        const logItems = r.recentLogs.slice(0, 10).map((log, idx) => {
          const levelColor =
            log.level === 'error'
              ? 'var(--error-color)'
              : log.level === 'warn'
                ? 'var(--warning-color)'
                : 'var(--text-secondary)'
          return h(
            'div',
            {
              key: idx,
              style: {
                padding: '6px 10px',
                background: '#fff',
                borderRadius: '4px',
                marginBottom: idx < Math.min(r.recentLogs.length, 10) - 1 ? '4px' : 0,
                border: '1px solid var(--border-color)',
                fontSize: '12px',
                fontFamily: 'monospace',
              },
            },
            [
              h(
                'span',
                { style: { color: 'var(--text-secondary)', marginRight: '8px' } },
                formatTime(log.timestamp),
              ),
              h(
                'span',
                {
                  style: {
                    color: levelColor,
                    fontWeight: 600,
                    marginRight: '8px',
                    textTransform: 'uppercase',
                  },
                },
                log.level,
              ),
              h(
                'span',
                { style: { color: 'var(--text-secondary)', marginRight: '8px' } },
                '[' + log.pod + ']',
              ),
              h('span', { style: { color: 'var(--text-primary)' } }, log.message),
            ],
          )
        })
        sections.push(sectionBlock('📜 近期日志', h('div', null, logItems)))
      }
      if (r.timestamp) {
        sections.push(
          h(
            'div',
            {
              style: {
                textAlign: 'center',
                padding: '12px 0',
                fontSize: '11px',
                color: '#bbb',
                borderTop: '1px solid var(--border-color)',
                marginTop: '8px',
              },
            },
            '诊断时间: ' + formatTime(r.timestamp) + ' | ID: ' + (r.id || '-'),
          ),
        )
      }
      sections.push(
        h(
          'div',
          {
            style: {
              textAlign: 'center',
              padding: '16px 0',
              borderTop: '1px solid var(--border-color)',
              marginTop: '8px',
            },
          },
          [
            h(
              'button',
              {
                onClick: () => {
                  const fp = this.selectedAlert?.fingerprint
                  if (fp) {
                    window.location.href = `/view/diagnosis/index.html?alert_fingerprint=${encodeURIComponent(fp)}`
                  }
                },
                style: {
                  padding: '10px 28px',
                  background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
                  color: '#fff',
                  border: 'none',
                  borderRadius: '8px',
                  cursor: 'pointer',
                  fontSize: '14px',
                  fontWeight: 600,
                  transition: 'transform 0.2s, box-shadow 0.2s',
                  boxShadow: '0 2px 8px rgba(102,126,234,0.3)',
                },
                onMouseenter: (e) => {
                  e.currentTarget.style.transform = 'translateY(-1px)'
                  e.currentTarget.style.boxShadow = '0 4px 12px rgba(102,126,234,0.4)'
                },
                onMouseleave: (e) => {
                  e.currentTarget.style.transform = 'translateY(0)'
                  e.currentTarget.style.boxShadow = '0 2px 8px rgba(102,126,234,0.3)'
                },
              },
              '🤖 进入 AI 对话进行深度排查',
            ),
          ],
        ),
      )
      if (this.executionRecords && this.executionRecords.length > 0) {
        const execCards = this.executionRecords.map((rec, idx) => {
          const p = rec.plan || {}
          const r = rec.result || {}
          const rOk = r.success
          const rAuto = rec.autoExecuted
          const rStatus = rAuto ? (rOk ? '已自动执行成功' : '已自动执行失败') : (rOk ? '已执行' : '待审批/未执行')
          const rColor = rOk ? '#52c41a' : (rAuto ? '#ff4d4f' : '#faad14')
          const target = p.target || (p.namespace ? p.namespace + '/' + p.resourceName : p.resourceName)
          const actText = actionText(p.action)
          const riskText = p.risk || ''
          const opText = rAuto ? '自动执行' : (rec.approvedBy ? '审批人: ' + rec.approvedBy : '未自动执行')
          // 执行后验证徽标（后台异步验证，结果追加在结果消息里）
          const rMsg = r.message || ''
          const pending = rMsg.includes('pending approval') || rMsg.includes('approval required')
          let verifyBadge = null
          if (!pending && rMsg.includes('执行验证失败已自动回滚')) {
            verifyBadge = { text: '⛔ 验证失败·已自动回滚', color: '#ff4d4f' }
          } else if (!pending && rMsg.includes('执行验证失败')) {
            verifyBadge = { text: '❌ 验证失败·需人工', color: '#ff4d4f' }
          } else if (!pending && rMsg.includes('执行验证通过')) {
            verifyBadge = { text: '🟢 验证通过', color: '#52c41a' }
          } else if (rOk) {
            verifyBadge = { text: '⏳ 后台验证中', color: '#1677ff' }
          }
          const detailParts = []
          if (p.image) detailParts.push('镜像: ' + p.image)
          if (p.containerName) detailParts.push('容器: ' + p.containerName)
          if (p.configData && Object.keys(p.configData).length) {
            detailParts.push('资源: ' + Object.entries(p.configData).map(([k, v]) => k + '=' + v).join(', '))
          }
          if (p.action === 'scale_deployment' && (p.replicas || p.replicas === 0)) detailParts.push('副本数: ' + p.replicas)
          if (p.action === 'rollout_undo') detailParts.push(p.revision ? '回滚到版本: ' + p.revision : '回滚到上一版本')
          return h('div', {
            key: idx,
            style: {
              padding: '12px',
              background: '#fff',
              border: '1px solid var(--border-color)',
              borderRadius: '6px',
              marginBottom: idx < this.executionRecords.length - 1 ? '8px' : 0,
              opacity: idx === 0 ? 1 : 0.75,
            },
          }, [
            h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' } }, [
              h('span', { style: { fontWeight: 600, fontSize: '13px' } }, actText),
              h('span', { style: { display: 'flex', gap: '8px', alignItems: 'center' } }, [
                verifyBadge
                  ? h('span', { style: { fontSize: '11px', color: verifyBadge.color, border: '1px solid ' + verifyBadge.color, borderRadius: '10px', padding: '1px 8px' } }, verifyBadge.text)
                  : null,
                h('span', { style: { fontSize: '12px', color: rColor, fontWeight: 600 } }, rStatus),
              ]),
            ]),
            h('div', { style: { fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '4px' } },
              '目标: ' + target + (riskText ? ' · 风险: ' + riskText : '')),
            detailParts.length
              ? h('div', { style: { fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '4px', wordBreak: 'break-all' } }, detailParts.join(' · '))
              : null,
            h('div', { style: { fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '4px' } }, opText),
            r.message ? h('div', { style: { fontSize: '12px', color: 'var(--text-secondary)' } }, '结果: ' + r.message) : null,
            (!rOk && !rAuto && !rec.approvedBy)
              ? h('div', { style: { marginTop: '8px' } }, [
                  h('button', {
                    onClick: () => this.approveExecution(rec),
                    disabled: this.approvingId === rec.id,
                    style: {
                      padding: '4px 14px',
                      fontSize: '12px',
                      fontWeight: 600,
                      background: this.approvingId === rec.id ? '#d9d9d9' : '#faad14',
                      color: '#fff',
                      border: 'none',
                      borderRadius: '4px',
                      cursor: this.approvingId === rec.id ? 'not-allowed' : 'pointer',
                    },
                  }, this.approvingId === rec.id ? '执行中…' : '✅ 批准执行'),
                ])
              : null,
            h('div', { style: { fontSize: '11px', color: '#bbb', marginTop: '4px' } }, rec.timestamp ? new Date(rec.timestamp).toLocaleString() : ''),
          ])
        })
        sections.push(sectionBlock('⚙️ 执行记录', h('div', null, execCards)))
      }
      return h('div', null, sections)
    },
  },
  render() {
    const hasBusinessFilters = !!(
      this.businessFilterApp ||
      this.businessFilterTeam ||
      this.businessFilterCriticality
    )
    const rawAlerts = hasBusinessFilters ? this.filteredAlerts : this.alerts
    // 按开始时间倒序（最新告警在最前）
    const sourceAlerts = [...rawAlerts].sort(
      (x, y) => new Date(getAlertStartsAt(y) || 0).getTime() - new Date(getAlertStartsAt(x) || 0).getTime(),
    )
    const rows = []
    let rowNo = 0
    for (const a of sourceAlerts) {
      rowNo++
      const fp = getAlertFingerprint(a)
      const isSelected = this.selectedAlert && getAlertFingerprint(this.selectedAlert) === fp
      const rowStyle = {
        borderBottom: '1px solid var(--border-color)',
        cursor: 'pointer',
        background: isSelected ? '#e6f7ff' : 'transparent',
        transition: 'background 0.2s',
      }
      const hoverIn = (e) => {
        if (!isSelected) e.currentTarget.style.background = '#fafafa'
      }
      const hoverOut = (e) => {
        if (!isSelected) e.currentTarget.style.background = 'transparent'
      }

      if (a?.type === 'aggregated_alert') {
        const id = fp || Math.random().toString(36).slice(2)
        const show = !!this.expanded[id]
        const subs = a.alerts || a.children || a.SubAlerts || []
        rows.push(
          h(
            'tr',
            {
              key: id,
              style: rowStyle,
              onClick: () => this.selectAlert(a),
              onMouseenter: hoverIn,
              onMouseleave: hoverOut,
            },
            [
              h(
                'td',
                { style: { padding: '10px 12px', fontSize: '12px', color: '#999', whiteSpace: 'nowrap' } },
                String(rowNo),
              ),
              h(
                'td',
                { style: { padding: '10px 12px', fontWeight: 600, fontSize: '14px' } },
                getAlertSummary(a),
              ),
              h('td', { style: { padding: '10px 12px' } }, severityBadge(getAlertSeverity(a))),
              h(
                'td',
                {
                  style: { padding: '10px 12px', fontSize: '13px', color: 'var(--text-secondary)' },
                },
                getAlertCount(a),
              ),
              h(
                'td',
                {
                  style: { padding: '10px 12px', fontSize: '13px', color: 'var(--text-secondary)' },
                },
                formatTime(getAlertStartsAt(a)),
              ),
              h('td', { style: { padding: '10px 12px', fontSize: '13px' } }, getBusinessApp(a)),
              h(
                'td',
                {
                  style: { padding: '10px 12px', fontSize: '13px', color: 'var(--text-secondary)' },
                },
                getBusinessTeam(a),
              ),
              h(
                'td',
                { style: { padding: '10px 12px' } },
                this.criticalityBadge(getBusinessCriticality(a)),
              ),
            ],
          ),
        )
        if (subs.length && show) {
          for (const [idx, c] of subs.entries()) {
            rows.push(
              h(
                'tr',
                {
                  key: c.fingerprint ?? idx,
                  style: { borderBottom: '1px solid var(--border-color)', background: '#fafbfc' },
                },
                [
                  h('td', { style: { padding: '8px 12px' } }, ''),
                  h(
                    'td',
                    { style: { padding: '8px 12px 8px 32px', fontSize: '13px' } },
                    c.labels?.alertname || '-',
                  ),
                  h(
                    'td',
                    { style: { padding: '8px 12px' } },
                    severityBadge(c.labels?.severity || '-'),
                  ),
                  h(
                    'td',
                    {
                      style: {
                        padding: '8px 12px',
                        fontSize: '13px',
                        color: 'var(--text-secondary)',
                      },
                    },
                    '-',
                  ),
                  h(
                    'td',
                    {
                      style: {
                        padding: '8px 12px',
                        fontSize: '13px',
                        color: 'var(--text-secondary)',
                      },
                    },
                    formatTime(c.startsAt),
                  ),
                  h(
                    'td',
                    {
                      style: {
                        padding: '8px 12px',
                        fontSize: '13px',
                        color: 'var(--text-secondary)',
                      },
                    },
                    '-',
                  ),
                  h(
                    'td',
                    {
                      style: {
                        padding: '8px 12px',
                        fontSize: '13px',
                        color: 'var(--text-secondary)',
                      },
                    },
                    '-',
                  ),
                  h('td', { style: { padding: '8px 12px' } }, this.criticalityBadge('-')),
                ],
              ),
            )
          }
        }
      } else {
        rows.push(
          h(
            'tr',
            {
              key: a.fingerprint || a.labels?.alertname || Math.random().toString(36).slice(2),
              style: rowStyle,
              onClick: () => this.selectAlert(a),
              onMouseenter: hoverIn,
              onMouseleave: hoverOut,
            },
            [
              h(
                'td',
                { style: { padding: '10px 12px', fontSize: '12px', color: '#999', whiteSpace: 'nowrap' } },
                String(rowNo),
              ),
              h(
                'td',
                { style: { padding: '10px 12px', fontWeight: 500, fontSize: '14px' } },
                [
                  a.labels?.alertname || '-',
                  h('span', {
                    style: {
                      marginLeft: '8px',
                      fontSize: '11px',
                      fontWeight: 600,
                      padding: '1px 8px',
                      borderRadius: '8px',
                      background: a.status === 'resolved' ? '#f6ffed' : '#fff1f0',
                      color: a.status === 'resolved' ? '#52c41a' : '#ff4d4f',
                      border: `1px solid ${a.status === 'resolved' ? '#b7eb8f' : '#ffa39e'}`,
                    },
                  }, a.status === 'resolved' ? '已恢复' : '触发中'),
                ],
              ),
              h(
                'td',
                { style: { padding: '10px 12px' } },
                severityBadge(a.labels?.severity || '-'),
              ),
              h(
                'td',
                {
                  style: { padding: '10px 12px', fontSize: '13px', color: 'var(--text-secondary)' },
                },
                '1',
              ),
              h(
                'td',
                {
                  style: { padding: '10px 12px', fontSize: '13px', color: 'var(--text-secondary)' },
                },
                [
                  formatTime(a.startsAt),
                  a.status === 'resolved' && a.endsAt
                    ? h('div', { style: { fontSize: '12px', color: '#52c41a' } }, '恢复: ' + formatTime(a.endsAt))
                    : null,
                ],
              ),
              h('td', { style: { padding: '10px 12px', fontSize: '13px' } }, getBusinessApp(a)),
              h(
                'td',
                {
                  style: { padding: '10px 12px', fontSize: '13px', color: 'var(--text-secondary)' },
                },
                getBusinessTeam(a),
              ),
              h(
                'td',
                { style: { padding: '10px 12px' } },
                this.criticalityBadge(getBusinessCriticality(a)),
              ),
            ],
          ),
        )
      }
    }

    if (rows.length === 0 && !this.loading) {
      const hasFilters =
        this.businessFilterApp || this.businessFilterTeam || this.businessFilterCriticality
      const msg = hasFilters ? '没有匹配的告警，请调整筛选条件' : '暂无活跃告警'
      rows.push(
        h('tr', [
          h(
            'td',
            {
              colspan: 7,
              style: {
                padding: '32px',
                textAlign: 'center',
                color: 'var(--text-secondary)',
                fontSize: '14px',
              },
            },
            msg,
          ),
        ]),
      )
    }

    const drawerContent = this.selectedAlert ? this.buildDrawerContent() : null
    const closeBtnStyle = {
      position: 'absolute',
      top: '12px',
      right: '12px',
      fontSize: '20px',
      background: 'none',
      border: 'none',
      cursor: 'pointer',
      color: '#999',
      width: '28px',
      height: '28px',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      borderRadius: '4px',
      transition: 'background 0.2s',
    }

    return h('div', { class: 'alerts-page' }, [
      h(NavBar),
      h('div', { class: 'container' }, [
        h('div', { class: 'dashboard-header' }, '告警管理'),
        this.loading
          ? h(
              'div',
              {
                class: 'card',
                style: { textAlign: 'center', padding: '32px', color: 'var(--text-secondary)' },
              },
              '加载中...',
            )
          : this.error
            ? h(
                'div',
                { class: 'card', style: { color: 'var(--error-color)' } },
                '获取失败: ' + this.error,
              )
            : h('div', { class: 'card', style: { padding: 0, overflow: 'hidden' } }, [
                h(
                  'div',
                  {
                    style: {
                      padding: '12px 20px',
                      borderBottom: '1px solid var(--border-color)',
                      fontSize: '13px',
                      color: 'var(--text-secondary)',
                    },
                  },
                  [
                    '共 ' + this.alerts.length + ' 条活跃告警',
                    this.businessFilterApp ||
                    this.businessFilterTeam ||
                    this.businessFilterCriticality
                      ? '（筛选显示 ' + (this.filteredAlerts?.length || 0) + ' 条）'
                      : '',
                  ],
                ),
                this.buildBusinessFilterBar(),
                h(
                  'table',
                  { class: 'm-info-tbl' },
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
                                textTransform: 'uppercase',
                                letterSpacing: '0.5px',
                              },
                            },
                            '#',
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
                                textTransform: 'uppercase',
                                letterSpacing: '0.5px',
                              },
                            },
                            '告警名称',
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
                                textTransform: 'uppercase',
                                letterSpacing: '0.5px',
                                width: '120px',
                              },
                            },
                            '严重级别',
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
                                textTransform: 'uppercase',
                                letterSpacing: '0.5px',
                                width: '80px',
                              },
                            },
                            '数量',
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
                                textTransform: 'uppercase',
                                letterSpacing: '0.5px',
                                width: '180px',
                              },
                            },
                            '开始时间',
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
                                textTransform: 'uppercase',
                                letterSpacing: '0.5px',
                                width: '140px',
                              },
                            },
                            '业务应用',
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
                                textTransform: 'uppercase',
                                letterSpacing: '0.5px',
                                width: '120px',
                              },
                            },
                            '团队',
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
                                textTransform: 'uppercase',
                                letterSpacing: '0.5px',
                                width: '100px',
                              },
                            },
                            '重要等级',
                          ),
                        ],
                      ),
                    ]),
                    h('tbody', null, rows),
                  ],
                ),
              ]),
      ]),
      this.selectedAlert
        ? h('div', {
            class: 'alert-drawer-overlay',
            onClick: this.closeDrawer,
            style: {
              position: 'fixed',
              top: 0,
              left: 0,
              right: 0,
              bottom: 0,
              background: 'rgba(0,0,0,0.35)',
              zIndex: 999,
              transition: 'opacity 0.3s',
            },
          })
        : null,
      this.selectedAlert
        ? h(
            'div',
            {
              class: 'alert-drawer-panel',
              style: {
                position: 'fixed',
                top: 0,
                right: 0,
                bottom: 0,
                width: '520px',
                maxWidth: 'min(520px, 50vw)',
                background: '#fff',
                boxShadow: '-10px 0 30px rgba(0,0,0,0.18)',
                zIndex: 1000,
                display: 'flex',
                flexDirection: 'column',
                animation: 'slideInRight 0.3s ease-out',
              },
            },
            [
              h(
                'div',
                {
                  style: {
                    padding: '16px 20px 12px',
                    borderBottom: '2px solid var(--border-color)',
                    position: 'relative',
                    flexShrink: 0,
                    background: 'linear-gradient(180deg, #ffffff 0%, #f7fbff 100%)',
                  },
                },
                [
                  h(
                    'h2',
                    {
                      style: {
                        margin: '0 0 4px 0',
                        fontSize: '16px',
                        fontWeight: 600,
                        maxWidth: 'calc(100% - 28px)',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      },
                    },
                    getAlertSummary(this.selectedAlert),
                  ),
                  h(
                    'div',
                    {
                      style: {
                        display: 'flex',
                        alignItems: 'center',
                        gap: '8px',
                        marginTop: '4px',
                      },
                    },
                    [
                      severityBadge(getAlertSeverity(this.selectedAlert)),
                      h(
                        'span',
                        { style: { fontSize: '12px', color: 'var(--text-secondary)' } },
                        formatTime(getAlertStartsAt(this.selectedAlert)),
                      ),
                    ],
                  ),
                  h(
                    'button',
                    {
                      onClick: this.closeDrawer,
                      style: closeBtnStyle,
                      onMouseenter: (e) => {
                        e.currentTarget.style.background = '#f0f0f0'
                      },
                      onMouseleave: (e) => {
                        e.currentTarget.style.background = 'none'
                      },
                    },
                    '\u00d7',
                  ),
                ],
              ),
              h(
                'div',
                {
                  style: {
                    display: 'flex',
                    borderBottom: '1px solid var(--border-color)',
                    flexShrink: 0,
                    background: '#fff',
                  },
                },
                [
                  h(
                    'button',
                    {
                      onClick: () => this.switchTab('basic'),
                      style: {
                        flex: 1,
                        padding: '12px 0',
                        border: 'none',
                        background: 'none',
                        cursor: 'pointer',
                        fontSize: '14px',
                        fontWeight: this.activeTab === 'basic' ? 600 : 400,
                        color:
                          this.activeTab === 'basic'
                            ? 'var(--primary-color)'
                            : 'var(--text-secondary)',
                        borderBottom:
                          this.activeTab === 'basic'
                            ? '2px solid var(--primary-color)'
                            : '2px solid transparent',
                        transition: 'all 0.2s',
                      },
                    },
                    '\u{1F4CB} 基本信息',
                  ),
                  h(
                    'button',
                    {
                      onClick: () => this.switchTab('diagnosis'),
                      style: {
                        flex: 1,
                        padding: '12px 0',
                        border: 'none',
                        background: 'none',
                        cursor: 'pointer',
                        fontSize: '14px',
                        fontWeight: this.activeTab === 'diagnosis' ? 600 : 400,
                        color:
                          this.activeTab === 'diagnosis'
                            ? 'var(--primary-color)'
                            : 'var(--text-secondary)',
                        borderBottom:
                          this.activeTab === 'diagnosis'
                            ? '2px solid var(--primary-color)'
                            : '2px solid transparent',
                        transition: 'all 0.2s',
                      },
                    },
                    '\u{1F916} AI 诊断',
                  ),
                ],
              ),
              h(
                'div',
                {
                  style: {
                    flex: 1,
                    overflowY: 'auto',
                    padding: '16px 20px',
                    background: '#fafafa',
                  },
                },
                drawerContent,
              ),
            ],
          )
        : null,
    ])
  },
}).mount('#app')
