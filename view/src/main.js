import { createApp, ref, onMounted } from 'vue';
import { NavBar, StatCard } from './components/SharedComponents.js';
import { fetchOverviewStats } from './utils/stats.js';
import './styles/common.css';

createApp({
    components: { NavBar, StatCard },
    setup() {
        const stats = ref({
            resources: { total: 0, nodes: 0, pods: 0 },
            alerts: { active: 0, suppressed: 0 },
            inspection: { issues: 0 }
        });

        onMounted(async () => {
            stats.value = await fetchOverviewStats();
        });

        return { stats };
    }
}).mount('#app');
