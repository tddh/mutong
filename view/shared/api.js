const API_BASE = '';
async function request(url, options = {}) {
    const defaultHeaders = { 'Content-Type': 'application/json' };
    const config = { ...options, headers: { ...defaultHeaders, ...options.headers } };
    try {
        const response = await fetch(`${API_BASE}${url}`, config);
        if (!response.ok) throw new Error(`API Error: ${response.status} ${response.statusText}`);
        return await response.json();
    } catch (error) {
        console.error(`[API] ${url} failed:`, error);
        throw error;
    }
}
const API = {
    stats: { overview: () => request('/api/v1/stats/overview').catch(() => null) },
    resources: { nodes: () => request('/k8s/resources/graph/nodes'), edges: () => request('/k8s/resources/graph/edges') },
    alerts: {
        list: (params) => { const q = new URLSearchParams(params).toString(); return request(`/api/v1/alerts${q ? '?' + q : ''}`); },
        health: () => request('/api/v1/alerts/health')
    },
    inspection: {
        execute: () => request('/api/v1/inspection/execute', { method: 'POST' }),
        report: () => request('/api/v1/inspection/report')
    }
};
