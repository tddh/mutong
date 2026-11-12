import { toast } from '../components/toast.js'

const API_BASE = ''

async function request(url, options = {}) {
  const defaultHeaders = { 'Content-Type': 'application/json' }
  const config = { ...options, headers: { ...defaultHeaders, ...options.headers } }
  const response = await fetch(`${API_BASE}${url}`, config)
  if (!response.ok) {
    // 尽量带出服务端的错误详情，而不是只显示 HTTP 状态
    let detail = ''
    try {
      const body = await response.json()
      detail = body.error || body.message || ''
    } catch {
      /* 非 JSON 响应忽略 */
    }
    throw new Error(detail ? `${detail}` : `API Error: ${response.status} ${response.statusText}`)
  }
  if (options.responseType === 'text') return await response.text()
  return await response.json()
}

/* 软失败：保持返回形状兼容（页面不崩），但故障必须可感知——toast 报错而不是
   静默返回 null/[] 把"后端挂了"伪装成"没有数据"。只用于调用方没有错误态的查询路径；
   调用方自带错误横幅的（status/clusters）继续静默降级，避免双重报错。 */
const soft = (promise, fallback, label) =>
  promise.catch((e) => {
    toast.error(`${label}失败: ${e.message || '未知错误'}`)
    return fallback
  })

export const API = {
  stats: {
    // 不吞错：仪表盘依赖抛错进入错误态（FUNC-03），不得回退假数据
    overview: () => request('/api/v1/stats/overview'),
    resourceTotal: () => request('/api/v1/stats/resource-total'),
  },
  resources: {
    nodes: () => request('/api/v1/k8s/resources/graph/nodes'),
    edges: () => request('/api/v1/k8s/resources/graph/edges'),
    metadata: () => request('/api/v1/k8s/resources/graph/metadata'),
  },
  alerts: {
    list: (params) => {
      const q = new URLSearchParams(params).toString()
      return request(`/api/v1/alerts${q ? '?' + q : ''}`)
    },
    health: () => request('/api/v1/alerts/health'),
    suppressionStatus: () => request('/api/v1/alerts/suppression/status').catch(() => null),
  },
  metrics: {
    pod: (namespace, name) =>
      soft(
        request(
          `/api/v1/metrics/pod?namespace=${encodeURIComponent(namespace)}&name=${encodeURIComponent(name)}`,
        ),
        null,
        '获取 Pod 指标',
      ),
    node: (name) =>
      soft(
        request(`/api/v1/metrics/node?name=${encodeURIComponent(name)}`),
        null,
        '获取节点指标',
      ),
    deployment: (namespace, name) =>
      soft(
        request(
          `/api/v1/metrics/deployment?namespace=${encodeURIComponent(namespace)}&name=${encodeURIComponent(name)}`,
        ),
        null,
        '获取 Deployment 指标',
      ),
  },
  diagnosis: {
    // 静默降级仅限调用方自带错误态的探测类接口（status 页按 null 列出失败服务）
    status: () => request('/api/v1/diagnosis/status').catch(() => null),
    run: (fingerprint) =>
      request('/api/v1/diagnosis/run', { method: 'POST', body: JSON.stringify({ fingerprint }) }),
    rerun: (fingerprint) =>
      request('/api/v1/diagnosis/rerun', { method: 'POST', body: JSON.stringify({ fingerprint }) }),
    chat: {
      context: (data) =>
        request('/api/v1/diagnosis/chat/context', { method: 'POST', body: JSON.stringify(data) }),
      ask: (data) =>
        request('/api/v1/diagnosis/chat/ask', { method: 'POST', body: JSON.stringify(data) }),
    },
  },
  system: {
    status: () => request('/api/v1/system/status').catch(() => null),
  },
  clusters: {
    // 不在这里吞错：原先 catch 返回 { clusters: [] }，会把后端故障伪装成"一个集群都没配"，
    // 使调用方的错误分支成为死代码。错误交给页面处理（clusters.js 有 try/catch 与重试）。
    list: () => request('/api/v1/clusters'),
    health: (name) =>
      request(`/api/v1/clusters/${encodeURIComponent(name)}/health`).catch(() => null),
    stats: (name) =>
      request(`/api/v1/clusters/${encodeURIComponent(name)}/stats`).catch(() => null),
  },
  executor: {
    status: () => request('/api/v1/executor/status').catch(() => null),
    audit: (params) => {
      const q = new URLSearchParams(params).toString()
      return soft(
        request(`/api/v1/executor/audit${q ? '?' + q : ''}`),
        [],
        '获取执行审计',
      )
    },
    approve: (id) =>
      request(`/api/v1/executor/audit/${encodeURIComponent(id)}/approve`, { method: 'POST' }),
  },
  inspection: {
    execute: () => request('/api/v1/inspection/execute', { method: 'POST' }),
    report: () => request('/api/v1/inspection/report'),
    list: (params) => {
      const q = new URLSearchParams(params).toString()
      return request(`/api/v1/inspection/reports${q ? '?' + q : ''}`)
    },
    detail: (id) => request(`/api/v1/inspection/reports/${encodeURIComponent(id)}`),
    compare: (id, otherId) =>
      request(
        `/api/v1/inspection/reports/${encodeURIComponent(id)}/compare/${encodeURIComponent(otherId)}`,
      ),
    trend: (days) => request(`/api/v1/inspection/trend?days=${days || 30}`),
  },
  logs: {
    podLogs: (namespace, podName, container, since, tail) => {
      const params = new URLSearchParams()
      if (namespace) params.set('namespace', namespace)
      if (podName) params.set('name', podName)
      if (container) params.set('container', container)
      if (since) params.set('since', since)
      if (tail) params.set('tail', tail)
      const q = params.toString()
      return soft(request(`/api/v1/logs/pod${q ? '?' + q : ''}`), null, '获取 Pod 日志')
    },
    searchLogs: (namespace, query, since, max) => {
      const params = new URLSearchParams()
      if (namespace) params.set('namespace', namespace)
      if (query) params.set('query', query)
      if (since) params.set('since', since)
      if (max) params.set('max', max)
      const q = params.toString()
      return soft(request(`/api/v1/logs/search${q ? '?' + q : ''}`), null, '搜索日志')
    },
    errorLogs: (namespace, since) => {
      const params = new URLSearchParams()
      if (namespace) params.set('namespace', namespace)
      if (since) params.set('since', since)
      const q = params.toString()
      return soft(request(`/api/v1/logs/errors${q ? '?' + q : ''}`), null, '获取错误日志')
    },
    warnLogs: (namespace, since) => {
      const params = new URLSearchParams()
      if (namespace) params.set('namespace', namespace)
      if (since) params.set('since', since)
      const q = params.toString()
      return soft(request(`/api/v1/logs/warn${q ? '?' + q : ''}`), null, '获取警告日志')
    },
    infoLogs: (namespace, since) => {
      const params = new URLSearchParams()
      if (namespace) params.set('namespace', namespace)
      if (since) params.set('since', since)
      const q = params.toString()
      return soft(request(`/api/v1/logs/info${q ? '?' + q : ''}`), null, '获取信息日志')
    },
  },
  trace: {
    services: () => soft(request('/api/v1/trace/services'), [], '获取追踪服务列表'),
    query: (serviceName, operationName, traceId, limit) => {
      const params = new URLSearchParams()
      if (serviceName) params.set('serviceName', serviceName)
      if (operationName) params.set('operationName', operationName)
      if (traceId) params.set('traceId', traceId)
      if (limit) params.set('limit', limit)
      const q = params.toString()
      return soft(request(`/api/v1/trace/query${q ? '?' + q : ''}`), [], '查询 Trace')
    },
    spans: (traceId) =>
      soft(
        request(`/api/v1/trace/spans?traceId=${encodeURIComponent(traceId)}`),
        [],
        '获取 Span 列表',
      ),
  },
  retrospective: {
    timeline: (fingerprint) =>
      request(`/api/v1/retrospective/timeline/${encodeURIComponent(fingerprint)}`),
    causalChain: (fingerprint) =>
      request(`/api/v1/retrospective/causal-chain/${encodeURIComponent(fingerprint)}`),
    postmortem: (fingerprint) =>
      request(`/api/v1/retrospective/postmortem/${encodeURIComponent(fingerprint)}`, {
        method: 'POST',
      }),
    postmortemText: (fingerprint) =>
      request(`/api/v1/retrospective/postmortem/${encodeURIComponent(fingerprint)}/text`, {
        method: 'GET',
        responseType: 'text',
      }),
    update: (fingerprint, data) =>
      request(`/api/v1/retrospective/postmortem/${encodeURIComponent(fingerprint)}`, {
        method: 'PUT',
        body: JSON.stringify(data),
      }),
    history: (fingerprint) =>
      request(`/api/v1/retrospective/history/${encodeURIComponent(fingerprint)}`),
    list: (params) => {
      const q = new URLSearchParams(params).toString()
      return request(`/api/v1/retrospective/list${q ? '?' + q : ''}`)
    },
    searchKnowledge: (params) => {
      const q = new URLSearchParams(params).toString()
      return request(`/api/v1/retrospective/knowledge/search${q ? '?' + q : ''}`)
    },
  },
}
