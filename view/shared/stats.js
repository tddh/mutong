const MockStats = {
    resources: { total: 1200, nodes: 24, pods: 156, services: 89 },
    alerts: { active: 12, suppressed: 34, notified: 12 },
    inspection: { lastRun: '2小时前', issues: 3, health: '良好' }
};
async function fetchOverviewStats() {
    try { const data = await API.stats.overview(); if (!data) return MockStats; return data; }
    catch { return MockStats; }
}
