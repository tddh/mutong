/* 导航单一真源。此前 12 条链接硬编码在 NavBar 模板里，加/藏一个入口要动模板字符串，
   且"暂时隐藏"只能靠包注释实现——注释在开发模式保留、生产构建被丢弃，行为不一致。
   改为数据驱动后，隐藏项显式落在 NAV_HIDDEN，带原因与代码位置，不再依赖注释语义。 */

export const NAV_ITEMS = [
  { href: '/view/index.html', label: '仪表盘', icon: 'dashboard', group: 'primary' },
  { href: '/view/topology/index.html', label: '拓扑', icon: 'topology', group: 'primary' },
  { href: '/view/alerts/index.html', label: '告警', icon: 'alert', group: 'primary' },
  { href: '/view/diagnosis/index.html', label: 'AI 诊断', icon: 'ai', group: 'primary' },
  { href: '/view/inspection/index.html', label: '巡检', icon: 'inspect', group: 'primary' },
  { href: '/view/status/index.html', label: '状态', icon: 'status', group: 'primary' },
  { href: '/view/terminal/index.html', label: '终端', icon: 'terminal', group: 'primary' },
  { href: '/view/clusters/index.html', label: '集群', icon: 'cluster', group: 'primary' },
  // 次级入口：12 项全平铺在 1280px 会溢出，这 4 项收进"更多"
  { href: '/view/inspection-history/index.html', label: '巡检历史', icon: 'history', group: 'more' },
  { href: '/view/retrospective/index.html', label: '复盘分析', icon: 'report', group: 'more' },
  { href: '/view/retrospective-history/index.html', label: '历史复盘', icon: 'archive', group: 'more' },
  { href: '/view/resource-table.html', label: '资源表格', icon: 'table', group: 'more' },
]

/* 入口已收起，页面代码完整保留于 view/src/ 对应目录。
   恢复方式：把该项移回 NAV_ITEMS 并指定 group，无需改任何模板。
   勿删——删除会让 src/ 下的页面成为无入口的孤儿代码，且丢失收起原因。 */
export const NAV_HIDDEN = [
  {
    href: '/view/logs/index.html',
    label: '日志',
    icon: 'inspect',
    source: 'src/logs/',
    reason: '依赖 Elasticsearch，测试环境配置的 localhost:9200 无监听；客户端不探测连通性，故路由仍在但查询必失败',
  },
  {
    href: '/view/monitoring/index.html',
    label: '监控',
    icon: 'status',
    source: 'src/monitoring/',
    reason: '页面过于粗糙，待重建为 Grafana 内嵌（方案见 docs/TODO.md「前端菜单收敛与监控重建」）',
  },
  {
    href: '/view/trace/index.html',
    label: '追踪',
    icon: 'topology',
    source: 'src/trace/',
    reason: 'config.infra.yaml 中 opentelemetry.enabled=false，TraceController 未挂载，/api/v1/trace/* 不存在',
  },
]

export const NAV_PRIMARY = NAV_ITEMS.filter((i) => i.group === 'primary')
export const NAV_MORE = NAV_ITEMS.filter((i) => i.group === 'more')
