import { createApp, ref, onMounted, onUnmounted, h } from 'vue'
import { NavBar } from '../components/SharedComponents.js'
import { API } from '../utils/api.js'
import '../styles/common.css'

createApp({
  components: { NavBar },
  setup() {
    const logs = ref([])
    const namespace = ref('default')
    const podName = ref('')
    const container = ref('')
    const level = ref('all')
    const keyword = ref('')
    const since = ref(15)
    const tail = ref(50)
    const loading = ref(false)
    const error = ref(null)
    const lastUpdated = ref(null)
    const autoRefresh = ref(true)
    let timer = null

    const fetchLogs = async () => {
      loading.value = true
      error.value = null
      try {
        let result
        if (level.value === 'error') {
          result = await API.logs.errorLogs(namespace.value, since.value)
        } else if (level.value === 'warn' || level.value === 'info') {
          if (keyword.value.trim()) {
            result = await API.logs.searchLogs(
              namespace.value,
              keyword.value.trim(),
              since.value,
              tail.value,
            )
          } else {
            result = await API.logs.podLogs(
              namespace.value,
              podName.value.trim(),
              container.value.trim(),
              since.value,
              tail.value,
            )
          }
        } else if (keyword.value.trim()) {
          result = await API.logs.searchLogs(
            namespace.value,
            keyword.value.trim(),
            since.value,
            tail.value,
          )
        } else if (podName.value.trim()) {
          result = await API.logs.podLogs(
            namespace.value,
            podName.value.trim(),
            container.value.trim(),
            since.value,
            tail.value,
          )
        } else {
          error.value = '请输入 Pod 名称或关键词以查询日志'
          logs.value = []
          loading.value = false
          return
        }
        logs.value = result || []
      } catch (e) {
        error.value = e.message || '日志查询失败'
        logs.value = []
      }
      loading.value = false
      lastUpdated.value = new Date().toLocaleTimeString()
    }

    const loadMore = async () => {
      tail.value += 50
      await fetchLogs()
    }

    const onFilterSubmit = () => {
      tail.value = 50
      fetchLogs()
    }

    const toggleAutoRefresh = () => {
      autoRefresh.value = !autoRefresh.value
      if (autoRefresh.value) {
        timer = setInterval(fetchLogs, 30000)
      } else {
        clearInterval(timer)
        timer = null
      }
    }

    onMounted(() => {
      fetchLogs()
      if (autoRefresh.value) {
        timer = setInterval(fetchLogs, 30000)
      }
    })
    onUnmounted(() => {
      if (timer) clearInterval(timer)
    })

    return {
      logs,
      namespace,
      podName,
      container,
      level,
      keyword,
      since,
      tail,
      loading,
      error,
      lastUpdated,
      autoRefresh,
      fetchLogs,
      loadMore,
      onFilterSubmit,
      toggleAutoRefresh,
    }
  },
  render() {
    const levelBadge = (lvl) => {
      const upper = (lvl || '').toUpperCase()
      let color = 'var(--text-secondary)'
      if (upper === 'ERROR' || upper === 'FATAL' || upper === 'PANIC') color = 'var(--error-color)'
      else if (upper === 'WARN' || upper === 'WARNING') color = 'var(--warning-color)'
      else if (upper === 'INFO') color = 'var(--primary-color)'
      else if (upper === 'DEBUG') color = '#999'
      return h(
        'span',
        {
          style: `display:inline-block;padding:2px 8px;border-radius:10px;font-size:11px;font-weight:600;color:#fff;background:${color};min-width:48px;text-align:center;`,
        },
        upper || 'INFO',
      )
    }

    const formatTimestamp = (ts) => {
      if (!ts) return '-'
      try {
        const d = new Date(ts)
        return isNaN(d.getTime()) ? ts : d.toLocaleString()
      } catch {
        return ts
      }
    }

    const filterBar = () =>
      h('div', { class: 'card', style: 'margin-bottom:20px;' }, [
        h('div', { style: 'display:flex;flex-wrap:wrap;gap:12px;align-items:flex-end;' }, [
          h('div', { class: 'log-filter-group' }, [
            h('label', { class: 'log-filter-label' }, '命名空间'),
            h('input', {
              class: 'log-filter-input',
              value: this.namespace,
              onInput: (e) => {
                this.namespace = e.target.value
              },
              placeholder: 'default',
            }),
          ]),
          h('div', { class: 'log-filter-group' }, [
            h('label', { class: 'log-filter-label' }, 'Pod 名称'),
            h('input', {
              class: 'log-filter-input',
              value: this.podName,
              onInput: (e) => {
                this.podName = e.target.value
              },
              placeholder: '留空查全部',
            }),
          ]),
          h('div', { class: 'log-filter-group' }, [
            h('label', { class: 'log-filter-label' }, '容器'),
            h('input', {
              class: 'log-filter-input',
              value: this.container,
              onInput: (e) => {
                this.container = e.target.value
              },
              placeholder: '可选',
            }),
          ]),
          h('div', { class: 'log-filter-group' }, [
            h('label', { class: 'log-filter-label' }, '级别'),
            h(
              'select',
              {
                class: 'log-filter-select',
                value: this.level,
                onChange: (e) => {
                  this.level = e.target.value
                },
              },
              [
                h('option', { value: 'all' }, '全部'),
                h('option', { value: 'error' }, 'Error'),
                h('option', { value: 'warn' }, 'Warn'),
                h('option', { value: 'info' }, 'Info'),
              ],
            ),
          ]),
          h('div', { class: 'log-filter-group' }, [
            h('label', { class: 'log-filter-label' }, '关键词'),
            h('input', {
              class: 'log-filter-input',
              value: this.keyword,
              onInput: (e) => {
                this.keyword = e.target.value
              },
              placeholder: '搜索日志内容',
            }),
          ]),
          h('div', { class: 'log-filter-group' }, [
            h('label', { class: 'log-filter-label' }, '时间范围(分钟)'),
            h('input', {
              class: 'log-filter-input',
              type: 'number',
              value: this.since,
              onInput: (e) => {
                this.since = parseInt(e.target.value) || 15
              },
              min: '1',
              style: 'width:80px;',
            }),
          ]),
          h(
            'button',
            {
              class: 'log-filter-btn',
              onClick: this.onFilterSubmit,
            },
            '查询',
          ),
        ]),
      ])

    const logTable = () => {
      if (this.loading && this.logs.length === 0) {
        return h(
          'div',
          { class: 'card', style: 'text-align:center;padding:40px;color:var(--text-secondary);' },
          '加载中...',
        )
      }

      if (this.error) {
        return h('div', { class: 'error-banner' }, `⚠️ ${this.error}`)
      }

      if (this.logs.length === 0) {
        return h(
          'div',
          { class: 'card', style: 'text-align:center;padding:40px;color:var(--text-secondary);' },
          '暂无日志数据',
        )
      }

      const rows = this.logs.map((entry, idx) =>
        h(
          'tr',
          {
            style: `background:${idx % 2 === 0 ? 'var(--card-bg)' : 'var(--bg-color)'};`,
          },
          [
            h('td', { class: 'log-cell log-cell-ts' }, formatTimestamp(entry.timestamp)),
            h('td', { class: 'log-cell' }, levelBadge(entry.level)),
            h('td', { class: 'log-cell log-cell-pod', title: entry.pod || '-' }, entry.pod || '-'),
            h('td', { class: 'log-cell' }, entry.container || '-'),
            h('td', { class: 'log-cell log-cell-msg' }, entry.message || '-'),
          ],
        ),
      )

      return h('div', { class: 'card', style: 'padding:0;overflow:hidden;' }, [
        h('div', { style: 'overflow-x:auto;' }, [
          h('table', { class: 'log-table' }, [
            h('thead', null, [
              h('tr', null, [
                h('th', { class: 'log-th' }, '时间'),
                h('th', { class: 'log-th' }, '级别'),
                h('th', { class: 'log-th' }, 'Pod'),
                h('th', { class: 'log-th' }, '容器'),
                h('th', { class: 'log-th' }, '消息'),
              ]),
            ]),
            h('tbody', null, rows),
          ]),
        ]),
        h('div', { style: 'display:flex;justify-content:center;padding:12px;' }, [
          h(
            'button',
            {
              class: 'log-filter-btn',
              onClick: this.loadMore,
              disabled: this.loading,
              style: this.loading ? 'opacity:0.6;cursor:not-allowed;' : '',
            },
            this.loading ? '加载中...' : '加载更多',
          ),
        ]),
      ])
    }

    return h('div', [
      h(NavBar),
      h('div', { class: 'container' }, [
        h(
          'div',
          {
            class: 'dashboard-header',
            style: 'display:flex;justify-content:space-between;align-items:center;',
          },
          [
            h('span', null, '📋 日志查询'),
            h('div', { style: 'display:flex;align-items:center;gap:12px;' }, [
              h(
                'button',
                {
                  class: 'log-filter-btn',
                  onClick: this.toggleAutoRefresh,
                  style: `font-size:12px;padding:4px 12px;${this.autoRefresh ? 'background:var(--success-color);' : 'background:#999;'}`,
                },
                this.autoRefresh ? '● 自动刷新' : '○ 已暂停',
              ),
              this.lastUpdated
                ? h(
                    'span',
                    { style: 'font-size:12px;color:var(--text-secondary);' },
                    `最近更新: ${this.lastUpdated}`,
                  )
                : null,
            ]),
          ],
        ),
        filterBar(),
        logTable(),
      ]),
    ])
  },
}).mount('#app')
