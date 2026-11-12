import { createApp, ref, computed, onMounted } from 'vue'
import { NavBar } from './components/SharedComponents.js'
import { PageHeader, StatCard, Skeleton, Icon } from './components/primitives.js'
import { toast } from './components/toast.js'
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
    const error = ref('')
    const updatedAt = ref('')
    const desc = computed(() => {
      if (loading.value) return '正在加载概览数据…'
      if (error.value) return '概览数据加载失败'
      return updatedAt.value ? `数据更新于 ${updatedAt.value}` : ''
    })

    const load = async () => {
      loading.value = true
      error.value = ''
      try {
        stats.value = await fetchOverviewStats()
        // 只有真实拿到数据才标注更新时间，失败时不得制造"数据新鲜"的假象
        updatedAt.value = new Date().toLocaleTimeString()
      } catch (e) {
        error.value = e.message || '未知错误'
        toast.error('概览数据加载失败: ' + error.value)
      } finally {
        loading.value = false
      }
    }

    onMounted(load)

    return { stats, loading, error, desc, load }
  },
}).mount('#app')