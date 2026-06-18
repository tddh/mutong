import { createApp, ref, onMounted, h, resolveComponent } from 'vue'
import { NavBar } from '../components/SharedComponents.js'
import { API } from '../utils/api.js'
import '../styles/common.css'

const SEVERITY_COLORS = { critical: '#cf1322', warning: '#faad14', info: '#1890ff' }
const SEVERITY_BG = { critical: '#fff1f0', warning: '#fff7e6', info: '#e6f7ff' }

function severityBadge(severity) {
  const s = (severity || '').toLowerCase()
  return h('span', {
    style: {
      display: 'inline-block', padding: '2px 8px', borderRadius: '4px',
      fontSize: '12px', fontWeight: 600,
      color: SEVERITY_COLORS[s] || '#666', background: SEVERITY_BG[s] || '#f5f5f5',
    },
  }, (severity || '').toUpperCase())
}

const thStyle = {
  textAlign: 'left', padding: '10px 12px', fontWeight: 600, fontSize: '12px',
  color: 'var(--text-secondary)', borderBottom: '2px solid var(--border-color)', whiteSpace: 'nowrap',
}
const tdStyle = { padding: '8px 12px', verticalAlign: 'top', fontSize: '13px' }

createApp({
  components: { NavBar },
  setup() {
    const report = ref(null)
    const loading = ref(true)
    const running = ref(false)

    const loadReport = async () => {
      loading.value = true
      try {
        report.value = await API.inspection.report()
      } catch {
        report.value = null
      } finally {
        loading.value = false
      }
    }

    const runInspection = async () => {
      running.value = true
      try {
        await API.inspection.execute()
        setTimeout(loadReport, 2000)
      } finally {
        running.value = false
      }
    }

    onMounted(loadReport)
    return { report, loading, running, runInspection }
  },
  render() {
    const issues = this.report?.issues || []
    const issueTable = issues.length
      ? h('div', { style: 'overflow-x:auto;margin-top:12px;' }, [
          h('table', { style: 'width:100%;border-collapse:collapse;font-size:13px;' }, [
            h('thead', null, [
              h('tr', { style: 'background:var(--bg-color);' }, [
                h('th', thStyle, '级别'),
                h('th', thStyle, '规则'),
                h('th', thStyle, '资源'),
                h('th', thStyle, '建议'),
              ]),
            ]),
            h('tbody', null, issues.map((issue, idx) =>
              h('tr', {
                key: `${issue.ruleName}-${idx}`,
                style: { borderBottom: '1px solid #f0f0f0' },
              }, [
                h('td', tdStyle, severityBadge(issue.severity)),
                h('td', tdStyle, issue.ruleName),
                h('td', tdStyle, (issue.resources || []).join(', ') || '-'),
                h('td', { ...tdStyle, maxWidth: '400px', color: 'var(--text-secondary)' }, issue.suggestion || '-'),
              ])
            )),
          ]),
        ])
      : null

    return h('div', [
      h(NavBar),
      h('div', { class: 'container' }, [
        h(
          'div',
          {
            style:
              'display:flex; justify-content:space-between; align-items:center; margin-bottom:24px;',
          },
          [
            h('h1', { style: 'font-size:24px;font-weight:600;margin:0;' }, '巡检中心'),
            h(
              'button',
              {
                class: 'btn',
                style:
                  'padding:8px 16px;background:var(--primary-color);color:#fff;border:none;border-radius:4px;cursor:pointer;font-size:14px;',
                onClick: this.runInspection,
                disabled: this.running,
              },
              this.running ? '执行中...' : '立即执行巡检',
            ),
          ],
        ),
        this.loading
          ? h('div', { class: 'card' }, '加载报告...')
          : this.report
            ? h(
                'div',
                {
                  class: 'card',
                  style: 'margin-top:20px;padding:20px;border-left:4px solid var(--primary-color);',
                },
                [
                  h('h3', null, '最近巡检报告'),
                  h('p', null, `报告 ID: ${this.report.id}`),
                  h('p', null, `生成时间: ${new Date(this.report.generatedAt).toLocaleString()}`),
                  h('div', { style: 'margin-top:12px;' }, [
                    h(
                      'span',
                      { style: 'color: var(--error-color)' },
                      `Critical: ${this.report.summary?.critical || 0}`,
                    ),
                    h(
                      'span',
                      { style: 'color: var(--warning-color); margin-left: 16px;' },
                      `Warning: ${this.report.summary?.warning || 0}`,
                    ),
                  ]),
                  issueTable,
                ],
              )
            : h('div', { class: 'card', style: 'text-align:center;color:#666;' }, '暂无巡检报告'),
      ]),
    ])
  },
}).mount('#app')
