import { createApp, ref, computed, onMounted } from 'vue'
import { NavBar } from './components/SharedComponents.js'
import { PageHeader, StatCard, Skeleton, Icon } from './components/primitives.js'
import { fetchOverviewStats } from './utils/stats.js'
import './styles/common.css'

createApp({
  // in-DOM 模板不分大小写，注册名必须是 kebab-case 对应的 PascalCase
  components: { NavBar, PageHeader, StatCard, MSkeleton: Skeleton, MIcon: Icon },
  setup() {
    const stats = ref({
      resources: { total: 0, nodes: 0, pods: 0 },
      alerts: { active: 0, suppressed: 0 },
      inspection: { issues: 0 },
    })
    const loading = ref(true)
    const updatedAt = ref('')
    const desc = computed(() =>
      loading.value ? '正在加载概览数据…' : updatedAt.value ? `数据更新于 ${updatedAt.value}` : '',
    )

    onMounted(async () => {
      try {
        stats.value = await fetchOverviewStats()
      } finally {
        loading.value = false
        updatedAt.value = new Date().toLocaleTimeString()
      }
    })

    return { stats, loading, desc }
  },
}).mount('#app')
