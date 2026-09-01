const API_BASE = ''

async function request(url, options = {}) {
  const defaultHeaders = { 'Content-Type': 'application/json' }
  const config = { ...options, headers: { ...defaultHeaders, ...options.headers } }
  try {
    const response = await fetch(`${API_BASE}${url}`, config)
    if (!response.ok) throw new Error(`API Error: ${response.status} ${response.statusText}`)
    if (options.responseType === 'text') return await response.text()
    return await response.json()
  } catch (error) {
    console.error(`[API] ${url} failed:`, error)
    throw error
  }
}

export const API = {
  stats: {
    overview: () => request('/api/v1/stats/overview').catch(() => null),
    resourceTotal: () => request('/api/v1/stats/resource-total').catch(() => null),
  },
  resources: {
    nodes: () => request('/k8s/resources/graph/nodes'),
    edges: () => request('/k8s/resources/graph/edges'),
    metadata: () => request('/k8s/resources/graph/metadata'),
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
      request(
        `/api/v1/metrics/pod?namespace=${encodeURIComponent(namespace)}&name=${encodeURIComponent(name)}`,
      ).catch(() => null),
    node: (name) =>
      request(`/api/v1/metrics/node?name=${encodeURIComponent(name)}`).catch(() => null),
    deployment: (namespace, name) =>
      request(
        `/api/v1/metrics/deployment?namespace=${encodeURIComponent(namespace)}&name=${encodeURIComponent(name)}`,
      ).catch(() => null),
  },
  diagnosis: {
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
    list: () => request('/api/v1/clusters').catch(() => ({ clusters: [] })),
    health: (name) =>
      request(`/api/v1/clusters/${encodeURIComponent(name)}/health`).catch(() => null),
    stats: (name) =>
      request(`/api/v1/clusters/${encodeURIComponent(name)}/stats`).catch(() => null),
  },
  executor: {
    status: () => request('/api/v1/executor/status').catch(() => null),
    audit: (params) => {
      const q = new URLSearchParams(params).toString()
      return request(`/api/v1/executor/audit${q ? '?' + q : ''}`).catch(() => [])
    },
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
      return request(`/api/v1/logs/pod${q ? '?' + q : ''}`).catch(() => null)
    },
    searchLogs: (namespace, query, since, max) => {
      const params = new URLSearchParams()
      if (namespace) params.set('namespace', namespace)
      if (query) params.set('query', query)
      if (since) params.set('since', since)
      if (max) params.set('max', max)
      const q = params.toString()
      return request(`/api/v1/logs/search${q ? '?' + q : ''}`).catch(() => null)
    },
    errorLogs: (namespace, since) => {
      const params = new URLSearchParams()
      if (namespace) params.set('namespace', namespace)
      if (since) params.set('since', since)
      const q = params.toString()
      return request(`/api/v1/logs/errors${q ? '?' + q : ''}`).catch(() => null)
    },
    warnLogs: (namespace, since) => {
      const params = new URLSearchParams()
      if (namespace) params.set('namespace', namespace)
      if (since) params.set('since', since)
      const q = params.toString()
      return request(`/api/v1/logs/warn${q ? '?' + q : ''}`).catch(() => null)
    },
    infoLogs: (namespace, since) => {
      const params = new URLSearchParams()
      if (namespace) params.set('namespace', namespace)
      if (since) params.set('since', since)
      const q = params.toString()
      return request(`/api/v1/logs/info${q ? '?' + q : ''}`).catch(() => null)
    },
  },
  trace: {
    services: () => request('/api/v1/trace/services').catch(() => []),
    query: (serviceName, operationName, traceId, limit) => {
      const params = new URLSearchParams()
      if (serviceName) params.set('serviceName', serviceName)
      if (operationName) params.set('operationName', operationName)
      if (traceId) params.set('traceId', traceId)
      if (limit) params.set('limit', limit)
      const q = params.toString()
      return request(`/api/v1/trace/query${q ? '?' + q : ''}`).catch(() => [])
    },
    spans: (traceId) =>
      request(`/api/v1/trace/spans?traceId=${encodeURIComponent(traceId)}`).catch(() => []),
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
