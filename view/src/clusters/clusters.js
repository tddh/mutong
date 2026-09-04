import { createApp, ref, h, onMounted, onUnmounted } from 'vue'
import { NavBar } from '../components/SharedComponents.js'
import { PageHeader, Panel, StatCard, StatusBadge, MBtn } from '../components/primitives.js'
import { DataTable } from '../components/DataTable.js'
import { MModal } from '../components/overlays.js'
import { toast } from '../components/toast.js'
import { API } from '../utils/api.js'
import '../styles/common.css'

const SEV = { healthy: 'pass', unhealthy: 'critical' }
const LABEL = { healthy: '健康', unhealthy: '异常' }

createApp({
  components: { NavBar },
  setup() {
    const clusters = ref([])
    const loading = ref(true)
    const error = ref('')
    const lastUpdated = ref(null)
    const statsModal = ref({ visible: false, data: null, loading: false, name: '' })
    let timer = null

    const fetchClusters = async () => {
      error.value = ''
      try {
        const data = await API.clusters.list()
        clusters.value = data?.clusters || []
      } catch (e) {
        error.value = '获取集群列表失败，请重试'
        clusters.value = []
      }
      loading.value = false
      lastUpdated.value = new Date().toLocaleTimeString()
    }

    const runHealthCheck = async (name) => {
      try {
        const result = await API.clusters.health(name)
        if (result) {
          const ok = result.status === 'healthy'
          toast(result.error ? `${name}：${result.error}` : `${name}：${result.status}`, ok ? 'pass' : 'critical')
          fetchClusters()
        } else {
          toast(`${name}：健康检查无响应`, 'warning')
        }
      } catch (e) {
        toast(`${name}：健康检查失败`, 'critical')
      }
    }

    const viewStats = async (name) => {
      statsModal.value = { visible: true, data: null, loading: true, name }
      try {
        const result = await API.clusters.stats(name)
        statsModal.value = { visible: true, data: result, loading: false, name }
      } catch (e) {
        statsModal.value = { visible: true, data: null, loading: false, name }
      }
    }

    const closeModal = () => {
      statsModal.value = { visible: false, data: null, loading: false, name: '' }
    }

    const toggleEnabled = (c, checked) => {
      const idx = clusters.value.findIndex((x) => x.name === c.name)
      if (idx !== -1) clusters.value[idx].enable = checked
    }

    onMounted(async () => {
      await fetchClusters()
      clusters.value.forEach((c) => runHealthCheck(c.name))
      timer = setInterval(fetchClusters, 30000)
    })
    onUnmounted(() => clearInterval(timer))

    const columns = [
      {
        key: 'name',
        title: '集群名称',
        render: (c) => h('span', { class: 'is-strong' }, c.name),
      },
      { key: 'region', title: '区域', render: (c) => c.region || '—' },
      {
        key: 'endpoint',
        title: 'Endpoint',
        render: (c) =>
          h('span', { class: 'm-tbl-ellipsis is-mono', title: c.endpoint || '' }, c.endpoint || '—'),
      },
      {
        key: 'status',
        title: '状态',
        nowrap: true,
        render: (c) =>
          h(StatusBadge, {
            sev: SEV[c.status] || 'pending',
            text: LABEL[c.status] || '未知',
            dot: true,
          }),
      },
      {
        key: 'enable',
        title: '已启用',
        render: (c) =>
          h('input', {
            type: 'checkbox',
            checked: !!c.enable,
            'aria-label': `启用 ${c.name}`,
            onChange: (e) => toggleEnabled(c, e.target.checked),
          }),
      },
      {
        key: 'last_health_check',
        title: '最近检查',
        nowrap: true,
        muted: true,
        render: (c) => formatTime(c.last_health_check),
      },
      {
        key: '_ops',
        title: '操作',
        nowrap: true,
        render: (c) =>
          h('div', { class: 'm-tbl-ops' }, [
            h(MBtn, { size: 'sm', icon: 'refresh', title: '健康检查', onClick: () => runHealthCheck(c.name) }),
            h(MBtn, { size: 'sm', variant: 'text', onClick: () => viewStats(c.name) }, () => '查看统计'),
          ]),
      },
    ]

    return { clusters, loading, error, lastUpdated, statsModal, columns, fetchClusters, runHealthCheck, viewStats, closeModal }
  },
  render() {
    const all = this.clusters
    const healthy = all.filter((c) => c.status === 'healthy').length
    const enabled = all.filter((c) => c.enable).length
    const sm = this.statsModal

    return h('div', [
      h(NavBar),
      h('div', { class: 'container' }, [
        h(PageHeader, {
          title: '集群管理',
          desc: this.lastUpdated ? `每 30 秒自动刷新 · 更新于 ${this.lastUpdated}` : '正在加载集群列表…',
        }),
        h(
          'div',
          { class: 'm-stat-grid' },
          [
            { title: '集群总数', value: all.length, tone: 'accent', icon: 'cluster' },
            { title: '健康集群', value: healthy, tone: 'success', icon: 'check' },
            { title: '异常集群', value: all.length - healthy, tone: 'danger', icon: 'warning' },
            { title: '已启用', value: enabled, tone: 'alt', icon: 'status' },
          ].map((m) => h(StatCard, m)),
        ),
        this.error
          ? h('div', { class: 'error-banner' }, [
              h('span', null, this.error),
              h(
                MBtn,
                {
                  size: 'sm',
                  onClick: () => {
                    this.loading = true
                    this.fetchClusters()
                  },
                },
                () => '重试',
              ),
            ])
          : null,
        h(Panel, { flush: true }, {
          default: () =>
            h(DataTable, {
              columns: this.columns,
              rows: all,
              keyOf: 'name',
              loading: this.loading,
              emptyTitle: '暂无集群数据',
              emptyDesc: '在 config.core.yaml 的 clusters[] 中声明集群后重启服务',
            }),
        }),
        h(MModal, {
          open: sm.visible,
          title: `集群统计 · ${sm.name}`,
          width: 600,
          onClose: this.closeModal,
        }, {
          default: () =>
            sm.loading
              ? h(Panel, { loading: true }, { default: () => null })
              : sm.data
                ? [
                    sm.data.cluster
                      ? h('p', { class: 'm-modal-context' }, `集群：${sm.data.cluster}`)
                      : null,
                    sm.data.stats
                      ? h(
                          'div',
                          { class: 'm-metric-grid' },
                          [
                            { k: 'Pods', v: sm.data.stats.pods, icon: 'topology' },
                            { k: 'Nodes', v: sm.data.stats.nodes, icon: 'cluster' },
                            { k: 'Services', v: sm.data.stats.services, icon: 'table' },
                            { k: 'Deployments', v: sm.data.stats.deployments, icon: 'archive' },
                          ].map((m) =>
                            h('div', { class: 'm-metric', 'data-tone': 'accent' }, [
                              h('div', { class: 'm-metric-v' }, m.v ?? '—'),
                              h('div', { class: 'm-metric-k' }, m.k),
                            ]),
                          ),
                        )
                      : null,
                  ]
                : h('p', { class: 'm-modal-error' }, '获取统计信息失败'),
        }),
      ]),
    ])
  },
}).mount('#app')

function formatTime(t) {
  if (!t) return '—'
  return new Date(t).toLocaleString('zh-CN')
}
