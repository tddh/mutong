import { createApp, ref, h, onMounted, onUnmounted } from 'vue';
import { NavBar } from '../components/SharedComponents.js';
import { API } from '../utils/api.js';
import '../styles/common.css';

createApp({
    components: { NavBar },
    setup() {
        const clusters = ref([]);
        const loading = ref(true);
        const error = ref('');
        const lastUpdated = ref(null);
        const toast = ref(null);
        const statsModal = ref({ visible: false, data: null, loading: false });
        let timer = null;

        const fetchClusters = async () => {
            error.value = '';
            try {
                const data = await API.clusters.list();
                clusters.value = data.clusters || [];
            } catch (e) {
                error.value = '获取集群列表失败，请重试';
                clusters.value = [];
            }
            loading.value = false;
            lastUpdated.value = new Date().toLocaleTimeString();
        };

        const showToast = (msg, color = '#333') => {
            toast.value = { msg, color, visible: true };
            setTimeout(() => { toast.value = null; }, 3000);
        };

        const runHealthCheck = async (name) => {
            try {
                const result = await API.clusters.health(name);
                if (result) {
                    const color = result.status === 'healthy' ? 'var(--success-color)' : 'var(--error-color)';
                    const icon = result.status === 'healthy' ? '✅' : '❌';
                    const msg = result.error ? `${icon} ${name}: ${result.error}` : `${icon} ${name}: ${result.status}`;
                    showToast(msg, color);
                    fetchClusters();
                } else {
                    showToast(`⚠️ ${name}: 健康检查无响应`, 'var(--warning-color)');
                }
            } catch (e) {
                showToast(`❌ ${name}: 健康检查失败`, 'var(--error-color)');
            }
        };

        const viewStats = async (name) => {
            statsModal.value = { visible: true, data: null, loading: true };
            try {
                const result = await API.clusters.stats(name);
                statsModal.value = { visible: true, data: result, loading: false };
            } catch (e) {
                statsModal.value = { visible: true, data: null, loading: false };
            }
        };

        const closeModal = () => {
            statsModal.value = { visible: false, data: null, loading: false };
        };

        onMounted(async () => {
            await fetchClusters();
            clusters.value.forEach(c => runHealthCheck(c.name));
            timer = setInterval(fetchClusters, 30000);
        });
        onUnmounted(() => clearInterval(timer));

        return { clusters, loading, error, lastUpdated, toast, statsModal, runHealthCheck, viewStats, closeModal };
    },
    render() {
        const badge = (text, color) => h('span', {
            style: 'display:inline-block;padding:3px 10px;border-radius:12px;font-size:12px;font-weight:600;color:#fff;background:' + color,
        }, text);

        const statusBadge = (status) => {
            if (status === 'healthy') return badge('健康', 'var(--success-color)');
            if (status === 'unhealthy') return badge('异常', 'var(--error-color)');
            return badge('未知', '#999');
        };

        const toggleEnabled = (c, checked) => {
            const idx = this.clusters.findIndex(x => x.name === c.name);
            if (idx !== -1) this.clusters[idx].enable = checked;
        };

        const formatTime = (t) => {
            if (!t) return '-';
            const d = new Date(t);
            return d.toLocaleString('zh-CN');
        };

        const statsCards = () => {
            const all = this.clusters;
            const total = all.length;
            const healthy = all.filter(c => c.status === 'healthy').length;
            const enabled = all.filter(c => c.enable).length;
            const items = [
                { title: '集群总数', value: total, color: 'var(--primary-color)', icon: '🏗️' },
                { title: '健康集群', value: healthy, color: 'var(--success-color)', icon: '✅' },
                { title: '异常集群', value: all.length - healthy, color: 'var(--error-color)', icon: '⚠️' },
                { title: '已启用', value: enabled, color: '#722ed1', icon: '🔌' },
            ];
            return h('div', { class: 'stat-card-grid' }, items.map(m =>
                h('div', { class: 'stat-card' }, [
                    h('div', { style: 'display:flex;align-items:center;gap:12px;' }, [
                        h('span', { style: 'font-size:28px;' }, m.icon),
                        h('div', [
                            h('span', { class: 'stat-title' }, m.title),
                            h('span', { class: 'stat-value', style: 'color:' + m.color }, m.value),
                        ]),
                    ]),
                ])
            ));
        };

        const clusterTable = () => {
            if (this.clusters.length === 0) {
                return h('div', { class: 'card', style: 'text-align:center;padding:40px;color:var(--text-secondary);' }, '暂无集群数据');
            }
            return h('table', { class: 'log-table', style: 'margin-top:16px;' }, [
                h('thead', null, [
                    h('tr', null, [
                        h('th', { class: 'log-th' }, '集群名称'),
                        h('th', { class: 'log-th' }, '区域'),
                        h('th', { class: 'log-th' }, 'Endpoint'),
                        h('th', { class: 'log-th' }, '状态'),
                        h('th', { class: 'log-th', style: 'text-align:center;' }, '已启用'),
                        h('th', { class: 'log-th' }, '最近检查'),
                        h('th', { class: 'log-th', style: 'text-align:center;' }, '操作'),
                    ]),
                ]),
                h('tbody', null, this.clusters.map(c =>
                    h('tr', null, [
                        h('td', { class: 'log-cell', style: 'font-weight:600;' }, c.name),
                        h('td', { class: 'log-cell' }, c.region || '-'),
                        h('td', { class: 'log-cell', style: 'max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;' }, c.endpoint || '-'),
                        h('td', { class: 'log-cell' }, statusBadge(c.status)),
                        h('td', { class: 'log-cell', style: 'text-align:center;' }, [
                            h('input', {
                                type: 'checkbox',
                                checked: !!c.enable,
                                onChange: (e) => toggleEnabled(c, e.target.checked),
                                style: 'width:18px;height:18px;cursor:pointer;accent-color:var(--primary-color);',
                            }),
                        ]),
                        h('td', { class: 'log-cell' }, formatTime(c.last_health_check)),
                        h('td', { class: 'log-cell', style: 'text-align:center;' }, [
                            h('button', {
                                class: 'btn btn-sm',
                                style: 'margin-right:6px;padding:4px 12px;font-size:12px;cursor:pointer;background:var(--primary-color);color:#fff;border:none;border-radius:4px;',
                                onClick: () => this.runHealthCheck(c.name),
                            }, '健康检查'),
                            h('button', {
                                class: 'btn btn-sm',
                                style: 'padding:4px 12px;font-size:12px;cursor:pointer;background:var(--card-bg);color:var(--primary-color);border:1px solid var(--primary-color);border-radius:4px;',
                                onClick: () => this.viewStats(c.name),
                            }, '查看统计'),
                        ]),
                    ])
                )),
            ]);
        };

        const errorBanner = () => {
            if (!this.error) return null;
            return h('div', { class: 'error-banner', style: 'margin-bottom:16px;' }, [
                h('span', null, '⚠️ ' + this.error),
                h('button', {
                    style: 'margin-left:12px;padding:4px 16px;background:var(--primary-color);color:#fff;border:none;border-radius:4px;cursor:pointer;font-size:13px;',
                    onClick: () => { this.loading = true; this.fetchClusters(); },
                }, '重试'),
            ]);
        };

        const toastEl = () => {
            if (!this.toast) return null;
            const t = this.toast;
            return h('div', {
                style: 'position:fixed;top:80px;right:20px;padding:12px 20px;border-radius:8px;background:' + t.color + ';color:#fff;font-size:14px;font-weight:500;box-shadow:0 4px 12px rgba(0,0,0,0.15);z-index:1000;animation:slideInRight 0.3s ease;',
            }, t.msg);
        };

        const statsModalEl = () => {
            if (!this.statsModal || !this.statsModal.visible) return null;
            const sm = this.statsModal;
            return h('div', {
                style: 'position:fixed;inset:0;background:rgba(0,0,0,0.4);z-index:999;display:flex;align-items:center;justify-content:center;',
                onClick: () => this.closeModal(),
            }, [
                h('div', {
                    class: 'card',
                    style: 'max-width:600px;width:90%;max-height:80vh;overflow-y:auto;animation:slideInRight 0.2s ease;',
                    onClick: (e) => e.stopPropagation(),
                }, [
                    h('div', { style: 'display:flex;justify-content:space-between;align-items:center;margin-bottom:20px;padding-bottom:12px;border-bottom:1px solid var(--border-color);' }, [
                        h('h3', { style: 'font-size:18px;font-weight:600;' }, '📊 集群统计信息'),
                        h('button', {
                            style: 'background:none;border:none;font-size:20px;cursor:pointer;color:var(--text-secondary);',
                            onClick: () => this.closeModal(),
                        }, '✕'),
                    ]),
                    sm.loading
                        ? h('div', { style: 'text-align:center;padding:40px;color:var(--text-secondary);' }, '加载中...')
                        : sm.data
                            ? [
                                sm.data.cluster ? h('p', { style: 'margin-bottom:12px;font-weight:600;color:var(--primary-color);' }, '集群: ' + sm.data.cluster) : null,
                                sm.data.stats ? h('div', { class: 'metric-grid', style: 'grid-template-columns:repeat(2,1fr);' }, [
                                    { label: 'Pods', value: sm.data.stats.pods, icon: '📦' },
                                    { label: 'Nodes', value: sm.data.stats.nodes, icon: '🖥️' },
                                    { label: 'Services', value: sm.data.stats.services, icon: '🔗' },
                                    { label: 'Deployments', value: sm.data.stats.deployments, icon: '🚀' },
                                ].map(m =>
                                    h('div', { class: 'metric-item' }, [
                                        h('div', { class: 'metric-value', style: 'color:var(--primary-color);font-size:28px;' }, m.value ?? '-'),
                                        h('div', { class: 'metric-label', style: 'font-size:13px;' }, m.icon + ' ' + m.label),
                                    ])
                                )) : null,
                            ]
                            : h('div', { style: 'text-align:center;padding:40px;color:var(--error-color);' }, '获取统计信息失败'),
                ]),
            ]);
        };

        return h('div', [
            h(NavBar),
            h('div', { class: 'container' }, [
                h('div', { class: 'dashboard-header' }, '集群管理'),
                this.loading
                    ? h('div', { class: 'card', style: 'text-align:center;padding:40px;' }, '加载中...')
                    : [
                        statsCards(),
                        errorBanner(),
                        clusterTable(),
                        h('div', { class: 'refresh-info' }, '最近更新: ' + (this.lastUpdated || '-')),
                    ],
                toastEl(),
                statsModalEl(),
            ]),
        ]);
    },
}).mount('#app');
