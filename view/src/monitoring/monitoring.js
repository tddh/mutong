import { createApp, ref, computed, watch, onMounted } from 'vue';
import { NavBar } from '../components/SharedComponents.js';
import '../styles/common.css';
import './monitoring.css';

createApp({
    components: { NavBar },
    setup() {
        const selectedNs = ref('default');
        const selectedType = ref('Pod');
        const selectedResource = ref(null);
        const metrics = ref([]);
        const loading = ref(false);
        const error = ref(null);
        const allResources = ref([]);

        const namespaces = computed(() => [...new Set(allResources.value.map(r => r.namespace).filter(Boolean))].sort());

        const filteredResources = computed(() => {
            const ns = selectedNs.value;
            const type = selectedType.value;
            return allResources.value.filter(r => {
                if (r.kind !== type) return false;
                if (ns !== 'all' && r.namespace !== ns) return false;
                return true;
            });
        });

        watch(selectedType, () => {
            selectedResource.value = null;
            metrics.value = [];
        });

        watch(selectedNs, () => {
            selectedResource.value = null;
            metrics.value = [];
        });

        const loadResources = async () => {
            try {
                const res = await fetch('/k8s/resources/graph/nodes?page=1&pageSize=5000');
                if (!res.ok) throw new Error(`获取资源列表失败 (${res.status})`);
                const data = await res.json();
                allResources.value = (data.nodes || []).map(n => ({
                    id: n.id,
                    label: n.label,
                    kind: n.kind,
                    namespace: n.namespace,
                }));
            } catch (err) {
                console.error('Failed to load resources:', err);
            }
        };

        const loadMetrics = async () => {
            if (!selectedResource.value) return;

            loading.value = true;
            error.value = null;
            metrics.value = [];

            try {
                const { kind, label, namespace } = selectedResource.value;
                const ns = kind === 'Node' ? '' : (namespace || 'default');
                const url = `/api/v1/monitoring/metrics/${kind}?name=${encodeURIComponent(label)}${ns ? `&namespace=${encodeURIComponent(ns)}` : ''}`;

                const res = await fetch(url);
                if (!res.ok) {
                    const errData = await res.json().catch(() => ({}));
                    throw new Error(errData.error || `获取指标失败 (${res.status})`);
                }

                const data = await res.json();
                metrics.value = data || [];
            } catch (err) {
                error.value = err.message || '加载指标失败';
                console.error('Failed to load metrics:', err);
            } finally {
                loading.value = false;
            }
        };

        const getMetricDisplayName = (name) => {
            const displayNames = {
                'pod_cpu_usage_cores': 'CPU 使用核数',
                'pod_memory_working_set_bytes': '工作集内存',
                'pod_memory_usage_percent': '内存使用率',
                'pod_network_receive_bytes_per_sec': '网络接收速率',
                'pod_network_transmit_bytes_per_sec': '网络发送速率',
                'kube_pod_container_status_restarts_total': '容器重启次数',
                'node_cpu_usage_percent': 'CPU 使用率',
                'node_memory_usage_percent': '内存使用率',
                'node_filesystem_usage_percent': '根磁盘使用率',
                'node_network_receive_bytes_per_sec': '网络接收速率',
                'node_network_transmit_bytes_per_sec': '网络发送速率',
                'kube_deployment_spec_replicas': '期望副本数',
                'kube_deployment_status_replicas_available': '可用副本数',
                'kube_deployment_status_replicas_unavailable': '不可用副本数',
                'kube_statefulset_status_replicas_ready': '就绪副本数',
                'kube_statefulset_status_replicas_current': '当前副本数',
                'kube_statefulset_replicas': '期望副本数',
                'kube_daemonset_status_number_ready': '就绪节点数',
                'kube_daemonset_status_number_available': '可用节点数',
                'kube_daemonset_status_desired_number_scheduled': '期望调度数',
                'kube_endpoint_address_available': '可用端点地址数',
                'kube_endpoint_address_not_ready': '未就绪端点地址数',
            };
            return displayNames[name] || name;
        };

        const formatMetricValue = (name, value) => {
            const v = Number(value);
            if (isNaN(v)) return value;

            // 1. 百分比类指标
            if (name.includes('percent')) return v.toFixed(1) + '%';
            
            // 2. CPU 核数指标
            if (name.includes('cores')) return v.toFixed(3) + ' 核';
            
            // 3. 字节类指标（排除速率）
            if (name.includes('bytes') && !name.includes('per_sec')) {
                if (v >= 1073741824) return (v / 1073741824).toFixed(2) + ' GB';
                if (v >= 1048576) return (v / 1048576).toFixed(2) + ' MB';
                if (v >= 1024) return (v / 1024).toFixed(2) + ' KB';
                return v.toFixed(0) + ' B';
            }
            
            // 4. 速率类指标
            if (name.includes('per_sec')) {
                if (v >= 1048576) return (v / 1048576).toFixed(2) + ' MB/s';
                if (v >= 1024) return (v / 1024).toFixed(2) + ' KB/s';
                return v.toFixed(1) + ' B/s';
            }
            
            // 5. 计数类指标（副本、重启次数）
            if (name.includes('replicas') || name.includes('restarts')) return v.toFixed(0);
            
            // 6. 兜底：纯数字，绝不加 %
            return v.toFixed(2);
        };

        onMounted(async () => {
            await loadResources();
        });

        return {
            selectedNs,
            selectedType,
            selectedResource,
            metrics,
            loading,
            error,
            namespaces,
            filteredResources,
            loadMetrics,
            getMetricDisplayName,
            formatMetricValue,
        };
    }
}).mount('#app');

// 独立挂载 NavBar 到 #vue-nav
createApp({ components: { NavBar } }).mount('#vue-nav');
