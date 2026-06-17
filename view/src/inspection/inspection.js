import { createApp, ref, onMounted, h, resolveComponent } from 'vue'
import { NavBar } from '../components/SharedComponents.js'
import { API } from '../utils/api.js'
import '../styles/common.css'

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
    const issueList = (this.report?.issues || []).map((issue) =>
      h('li', { key: issue.ruleName, style: 'color:var(--text-secondary)' }, [
        h('b', null, issue.ruleName),
        `: ${issue.message}`,
      ]),
    )
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
                  this.report.issues?.length
                    ? h('ul', { style: 'margin-top:12px;padding-left:20px;' }, issueList)
                    : null,
                ],
              )
            : h('div', { class: 'card', style: 'text-align:center;color:#666;' }, '暂无巡检报告'),
      ]),
    ])
  },
}).mount('#app')
