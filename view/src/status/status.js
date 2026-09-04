import { createApp, ref, onMounted, onUnmounted, h } from 'vue'
import { NavBar } from '../components/SharedComponents.js'
import { PageHeader, Panel, StatusBadge, Skeleton } from '../components/primitives.js'
import { API } from '../utils/api.js'
import '../styles/common.css'

const kv = (k, v, valClass) =>
  h('div', { class: 'm-kv-row' }, [
    h('span', { class: 'm-kv-k' }, k),
    h('span', { class: ['m-kv-v', valClass] }, v ?? '—'),
  ])

const kvList = (rows) => h('div', { class: 'm-kv' }, rows)

createApp({
  components: { NavBar },
  setup() {
    const diagnosis = ref(null)
    const suppression = ref(null)
    const systemStatus = ref(null)
    const executor = ref(null)
    const loading = ref(true)
    const lastUpdated = ref(null)
    const errors = ref([])
    let timer = null

    const fetchData = async () => {
      errors.value = []
      const [diag, supp, sys, exec] = await Promise.all([
        API.diagnosis.status(),
        API.alerts.suppressionStatus(),
        API.system.status(),
        API.executor.status(),
      ])
      if (diag) diagnosis.value = diag
      if (supp) suppression.value = supp
      if (sys) systemStatus.value = sys
      if (exec) executor.value = exec
      if (!diag) errors.value.push('诊断服务')
      if (!supp) errors.value.push('告警抑制')
      if (!sys) errors.value.push('系统状态')
      // Executor may not be configured — don't show error if it returns null/404
      loading.value = false
      lastUpdated.value = new Date().toLocaleTimeString()
    }

    onMounted(() => {
      fetchData()
      timer = setInterval(fetchData, 30000)
    })
    onUnmounted(() => clearInterval(timer))

    return { diagnosis, suppression, systemStatus, executor, loading, lastUpdated, errors }
  },
  render() {
    const diag = this.diagnosis?.llm || {}
    const diagStats = this.diagnosis?.diagnosis || {}
    const re = this.suppression?.ruleEngine || this.suppression || {}
    const metrics = this.systemStatus?.alerts?.metrics || {}
    const stats = this.systemStatus?.diagnosis?.diagnosis || {}
    const exec = this.executor || {}

    const diagSev = !diag.configured ? 'pending' : diag.available ? 'pass' : 'critical'
    const diagText = !diag.configured ? '未配置' : diag.available ? '可用' : '不可用'
    const hasWindow = re.timeWindowSeconds != null
    const suppSev = !hasWindow ? 'pending' : re.timeWindowSeconds > 0 ? 'pass' : 'pending'
    const suppText = !hasWindow ? '未知' : re.timeWindowSeconds > 0 ? '运行中' : '未启用'

    const received = metrics.receivedTotal ?? 0
    const suppressed = metrics.suppressedTotal ?? 0
    const notified = metrics.notifiedTotal ?? 0
    const pct = (n) => (received > 0 ? `${((n / received) * 100).toFixed(1)}%` : '—')
    const suppTone =
      received === 0 ? undefined : suppressed / received < 0.3 ? 'success' : suppressed / received < 0.6 ? 'warning' : 'danger'
    const notifTone = received === 0 ? undefined : notified / received >= 0.5 ? 'success' : 'warning'

    const total = stats.totalDiagnoses ?? 0
    const llmPct = total > 0 ? ((stats.llmCalls ?? 0) / total) * 100 : 0
    const rulePct = total > 0 ? ((stats.ruleOnlyCalls ?? 0) / total) * 100 : 0

    const cards = [
      h(Panel, { title: 'LLM 诊断服务' }, {
        actions: () => h(StatusBadge, { sev: diagSev, text: diagText, dot: true }),
        default: () => [
          kvList([
            kv('提供商', diag.provider),
            kv('模型', diag.model, 'is-accent'),
            kv('地址', diag.endpoint ? truncate(diag.endpoint) : undefined, 'is-mono'),
          ]),
          h('div', { class: 'm-sub-label' }, '调用统计'),
          kvList([
            kv('总诊断次数', diagStats.totalDiagnoses),
            kv('LLM 调用次数', diagStats.llmCalls),
            kv('规则快速路径', diagStats.ruleOnlyCalls),
          ]),
        ],
      }),
      h(Panel, { title: '告警抑制' }, {
        actions: () => h(StatusBadge, { sev: suppSev, text: suppText, dot: true }),
        default: () =>
          kvList([
            kv('时间窗口', hasWindow ? `${re.timeWindowSeconds} 秒` : undefined),
            kv('最大抑制深度', re.maxDepth),
            kv('活跃告警数', re.activeAlertsCount),
            kv('例外严重级别', joinList(re.severityExceptions)),
            kv('例外关键级别', joinList(re.criticalityExceptions)),
          ]),
      }),
      h(Panel, { title: '告警处理指标' }, {
        default: () => [
          h(
            'div',
            { class: 'm-metric-grid' },
            [
              { k: '收到总数', v: metrics.receivedTotal, tone: 'accent' },
              { k: '抑制总数', v: metrics.suppressedTotal, tone: 'warning' },
              { k: '通知总数', v: metrics.notifiedTotal, tone: 'alt' },
              { k: '活跃告警', v: metrics.activeFiring, tone: 'danger' },
              { k: '已解决', v: metrics.activeResolved, tone: 'success' },
            ].map((m) =>
              h('div', { class: 'm-metric', 'data-tone': m.tone }, [
                h('div', { class: 'm-metric-v' }, m.v ?? '—'),
                h('div', { class: 'm-metric-k' }, m.k),
              ]),
            ),
          ),
          h('div', { class: 'm-stat-row' }, [
            h('div', { class: 'm-stat' }, [
              h('div', { class: 'm-stat-k' }, '抑制率'),
              h('div', { class: 'm-stat-v', 'data-tone': suppTone }, pct(suppressed)),
            ]),
            h('div', { class: 'm-stat' }, [
              h('div', { class: 'm-stat-k' }, '通知率'),
              h('div', { class: 'm-stat-v', 'data-tone': notifTone }, pct(notified)),
            ]),
          ]),
        ],
      }),
      h(Panel, { title: '诊断统计' }, {
        default: () => [
          kvList([
            kv('总诊断数', total),
            kv('LLM 调用', stats.llmCalls),
            kv('规则快速路径', stats.ruleOnlyCalls),
          ]),
          total > 0
            ? h('div', { class: 'm-bar-block' }, [
                h('div', { class: 'm-sub-label' }, 'LLM 与规则占比'),
                h('div', { class: 'm-bar' }, [
                  h('div', { class: 'm-bar-seg', style: `width:${llmPct}%` }),
                  h('div', {
                    class: 'm-bar-seg',
                    'data-tone': 'success',
                    style: `width:${rulePct}%`,
                  }),
                ]),
                h('div', { class: 'm-legend' }, [
                  h('span', { class: 'm-legend-item' }, [
                    h('i', { class: 'm-legend-key' }),
                    `LLM ${llmPct.toFixed(1)}%`,
                  ]),
                  h('span', { class: 'm-legend-item' }, [
                    h('i', { class: 'm-legend-key', 'data-tone': 'success' }),
                    `规则 ${rulePct.toFixed(1)}%`,
                  ]),
                ]),
              ])
            : null,
        ],
      }),
      h(Panel, { title: '执行器状态' }, {
        actions: () =>
          h(StatusBadge, {
            sev: exec.enabled ? 'pass' : 'pending',
            text: exec.enabled ? '启用' : '未启用',
            dot: true,
          }),
        default: () =>
          kvList([
            kv(
              '执行模式',
              h(StatusBadge, {
                sev: exec.autoMode ? 'pass' : 'warning',
                text: exec.autoMode ? '自主执行' : '审批模式',
              }),
            ),
            kv('审计日志数', exec.auditLogCount),
          ]),
      }),
    ]

    return h('div', [
      h(NavBar),
      h('div', { class: 'container' }, [
        h(PageHeader, {
          title: '系统状态',
          desc: this.lastUpdated ? `每 30 秒自动刷新 · 更新于 ${this.lastUpdated}` : '正在加载…',
        }),
        this.errors.length > 0
          ? h('div', { class: 'error-banner' }, `以下服务数据获取失败：${this.errors.join('、')}`)
          : null,
        this.loading
          ? h(Panel, { title: '系统状态', loading: true }, { default: () => null })
          : h('div', { class: 'm-grid-2' }, cards),
      ]),
    ])
  },
}).mount('#app')

function truncate(url) {
  return url.length > 40 ? url.slice(0, 37) + '…' : url
}

function joinList(arr) {
  return arr && arr.length > 0 ? arr.join('、') : undefined
}
