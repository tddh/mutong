import { API } from '../utils/api.js';

const MockStats = {
    resources: { total: 1200, nodes: 24, pods: 156, services: 89 },
    alerts: { active: 12, suppressed: 34, notified: 12 },
    inspection: { lastRun: '2小时前', issues: 3, health: '良好' }
};

export async function fetchOverviewStats() {
    try {
        const [data, totalData] = await Promise.all([
            API.stats.overview(),
            API.stats.resourceTotal()
        ]);
        if (!data) return MockStats;
        if (totalData && totalData.total !== undefined) {
            data.resources.total = totalData.total;
        }
        return data;
    } catch {
        return MockStats;
    }
}
