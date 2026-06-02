import { createApp, ref, onMounted, onUnmounted, h } from 'vue';
import { NavBar } from '../components/SharedComponents.js';
import { API } from '../utils/api.js';
import '../styles/common.css';

createApp({
    components: { NavBar },
    setup() {
        const diagnosis = ref(null);
        const suppression = ref(null);
        const systemStatus = ref(null);
        const executor = ref(null);
        const loading = ref(true);
        const lastUpdated = ref(null);
        const errors = ref([]);
        let timer = null;

        const fetchData = async () => {
            errors.value = [];
            const [diag, supp, sys, exec] = await Promise.all([
                API.diagnosis.status(),
                API.alerts.suppressionStatus(),
                API.system.status(),
                API.executor.status(),
            ]);
            if (diag) diagnosis.value = diag;
            if (supp) suppression.value = supp;
            if (sys) systemStatus.value = sys;
            if (exec) executor.value = exec;
            if (!diag) errors.value.push('诊断服务');
            if (!supp) errors.value.push('告警抑制');
            if (!sys) errors.value.push('系统状态');
            // Executor may not be configured — don't show error if it returns null/404
            loading.value = false;
            lastUpdated.value = new Date().toLocaleTimeString();
        };

        onMounted(() => {
            fetchData();
            timer = setInterval(fetchData, 30000);
        });
        onUnmounted(() => clearInterval(timer));

        return { diagnosis, suppression, systemStatus, executor, loading, lastUpdated, errors };
    },
    render() {
        const badge = (text, color) =>
            h('span', {
                style: `display:inline-block;padding:3px 10px;border-radius:12px;font-size:12px;font-weight:600;color:#fff;background:${color};letter-spacing:0.5px;`
            }, text);

        const diagStatus = () => {
            const llm = this.diagnosis?.llm;
            if (!llm) return badge('未知', '#999');
            if (llm.configured && llm.available) return badge('可用', 'var(--success-color)');
            if (llm.configured && !llm.available) return badge('不可用', 'var(--error-color)');
            return badge('未配置', '#999');
        };

        const suppStatus = () => {
            const s = this.suppression || {};
            if (!s || s.activeAlertsCount == null) return badge('未知', '#999');
            return s.timeWindowSeconds > 0 ? badge('运行中', 'var(--success-color)') : badge('未启用', '#999');
        };

        const field = (label, value, fallback = '-') =>
            h('div', { style: 'display:flex;justify-content:space-between;padding:6px 0;border-bottom:1px solid var(--border-color);font-size:14px;' }, [
                h('span', { style: 'color:var(--text-secondary);' }, label),
                h('span', { style: 'color:var(--text-primary);font-weight:500;word-break:break-all;text-align:right;max-width:60%;' }, value ?? fallback),
            ]);

        const diagCard = () => {
            const llm = this.diagnosis?.llm || {};
            const stats = this.diagnosis?.diagnosis || {};
            const truncateEndpoint = (url) => {
                if (!url) return '-';
                return url.length > 40 ? url.substring(0, 37) + '...' : url;
            };
            return h('div', { class: 'status-card' }, [
                h('div', { class: 'status-card-header' }, [
                    h('span', { class: 'status-card-title' }, '🧠 LLM 诊断服务'),
                    diagStatus(),
                ]),
                h('div', { class: 'status-card-body' }, [
                    field('提供商', llm.provider),
                    h('div', { style: 'display:flex;justify-content:space-between;padding:6px 0;border-bottom:1px solid var(--border-color);font-size:14px;' }, [
                        h('span', { style: 'color:var(--text-secondary);' }, '模型'),
                        h('span', { style: 'color:var(--primary-color);font-weight:600;text-align:right;max-width:60%;word-break:break-all;' }, llm.model ?? '-'),
                    ]),
                    field('地址', truncateEndpoint(llm.endpoint)),
                    h('div', { style: 'margin-top:12px;padding-top:8px;border-top:1px dashed var(--border-color);' }, [
                        h('div', { style: 'font-size:13px;color:var(--text-secondary);margin-bottom:8px;font-weight:600;' }, '调用统计'),
                        field('总诊断次数', stats.totalDiagnoses ?? '-'),
                        field('LLM 调用次数', stats.llmCalls ?? '-'),
                        field('规则快速路径', stats.ruleOnlyCalls ?? '-'),
                    ]),
                ]),
            ]);
        };

        const suppCard = () => {
            const re = this.suppression || {};
            return h('div', { class: 'status-card' }, [
                h('div', { class: 'status-card-header' }, [
                    h('span', { class: 'status-card-title' }, '🛡️ 告警抑制'),
                    suppStatus(),
                ]),
                h('div', { class: 'status-card-body' }, [
                    field('时间窗口', re.timeWindowSeconds != null ? `${re.timeWindowSeconds} 秒` : '-'),
                    field('最大抑制深度', re.maxDepth ?? '-'),
                    field('活跃告警数', re.activeAlertsCount ?? '-'),
                    field('例外严重级别', re.severityExceptions && re.severityExceptions.length > 0 ? re.severityExceptions.join(', ') : '-'),
                    field('例外关键级别', re.criticalityExceptions && re.criticalityExceptions.length > 0 ? re.criticalityExceptions.join(', ') : '-'),
                ]),
            ]);
        };

        const alertCard = () => {
            const alertData = this.systemStatus?.alerts || {};
            const metrics = alertData.metrics || {};
            const received = metrics.receivedTotal ?? 0;
            const suppressed = metrics.suppressedTotal ?? 0;
            const notified = metrics.notifiedTotal ?? 0;
            const suppRate = received > 0 ? ((suppressed / received) * 100).toFixed(1) : '-';
            const notifRate = received > 0 ? ((notified / received) * 100).toFixed(1) : '-';
            const suppColor = received > 0 ? (suppressed / received < 0.3 ? 'var(--success-color)' : suppressed / received < 0.6 ? 'var(--warning-color)' : 'var(--error-color)') : 'var(--text-secondary)';
            const notifColor = received > 0 ? (notified / received >= 0.5 ? 'var(--success-color)' : 'var(--warning-color)') : 'var(--text-secondary)';
            const items = [
                { label: '收到总数', value: metrics.receivedTotal, color: 'var(--primary-color)' },
                { label: '抑制总数', value: metrics.suppressedTotal, color: 'var(--warning-color)' },
                { label: '通知总数', value: metrics.notifiedTotal, color: '#722ed1' },
                { label: '活跃告警', value: metrics.activeFiring, color: 'var(--error-color)' },
                { label: '已解决', value: metrics.activeResolved, color: 'var(--success-color)' },
            ];
            return h('div', { class: 'status-card' }, [
                h('div', { class: 'status-card-header' }, [
                    h('span', { class: 'status-card-title' }, '📊 告警处理指标'),
                ]),
                h('div', { class: 'status-card-body' }, [
                    h('div', { class: 'metric-grid' },
                        items.map(m =>
                            h('div', { class: 'metric-item' }, [
                                h('div', { class: 'metric-value', style: `color:${m.color};` }, m.value ?? '-'),
                                h('div', { class: 'metric-label' }, m.label),
                            ])
                        )
                    ),
                    h('div', { style: 'margin-top:16px;padding-top:12px;border-top:1px dashed var(--border-color);display:flex;gap:24px;' }, [
                        h('div', { style: 'flex:1;' }, [
                            h('div', { style: 'font-size:12px;color:var(--text-secondary);margin-bottom:4px;' }, '抑制率'),
                            h('div', { style: `font-size:20px;font-weight:700;color:${suppColor};` }, suppRate === '-' ? '-' : `${suppRate}%`),
                        ]),
                        h('div', { style: 'flex:1;' }, [
                            h('div', { style: 'font-size:12px;color:var(--text-secondary);margin-bottom:4px;' }, '通知率'),
                            h('div', { style: `font-size:20px;font-weight:700;color:${notifColor};` }, notifRate === '-' ? '-' : `${notifRate}%`),
                        ]),
                    ]),
                ]),
            ]);
        };

        const diagStatsCard = () => {
            const diagData = this.systemStatus?.diagnosis || {};
            const stats = diagData.diagnosis || {};
            const total = stats.totalDiagnoses ?? 0;
            const llm = stats.llmCalls ?? 0;
            const rule = stats.ruleOnlyCalls ?? 0;
            const llmPct = total > 0 ? ((llm / total) * 100).toFixed(1) : 0;
            const rulePct = total > 0 ? ((rule / total) * 100).toFixed(1) : 0;
            return h('div', { class: 'status-card' }, [
                h('div', { class: 'status-card-header' }, [
                    h('span', { class: 'status-card-title' }, '📈 诊断统计'),
                ]),
                h('div', { class: 'status-card-body' }, [
                    field('总诊断数', total),
                    field('LLM 调用', llm),
                    field('规则快速路径', rule),
                    total > 0 ? h('div', { style: 'margin-top:16px;' }, [
                        h('div', { style: 'font-size:13px;color:var(--text-secondary);margin-bottom:8px;' }, 'LLM vs 规则占比'),
                        h('div', { class: 'bar-track' }, [
                            h('div', { class: 'bar-fill bar-fill-llm', style: `width:${llmPct}%` }),
                            h('div', { class: 'bar-fill bar-fill-rule', style: `width:${rulePct}%` }),
                        ]),
                        h('div', { style: 'display:flex;justify-content:space-between;margin-top:6px;font-size:12px;color:var(--text-secondary);' }, [
                            h('span', null, `🤖 LLM ${llmPct}%`),
                            h('span', null, `⚡ 规则 ${rulePct}%`),
                        ]),
                    ]) : null,
                ]),
            ]);
        };

        const execCard = () => {
            const exec = this.executor || {};
            const enabled = exec.enabled ?? false;
            const autoMode = exec.autoMode ?? false;
            const auditLogCount = exec.auditLogCount ?? '-';
            const modeBadge = autoMode
                ? badge('自主执行', 'var(--success-color)')
                : badge('审批模式', 'var(--warning-color)');
            return h('div', { class: 'status-card' }, [
                h('div', { class: 'status-card-header' }, [
                    h('span', { class: 'status-card-title' }, '⚙️ 执行器状态'),
                    enabled ? badge('启用', 'var(--success-color)') : badge('未启用', '#999'),
                ]),
                h('div', { class: 'status-card-body' }, [
                    field('执行模式', modeBadge),
                    field('审计日志数', auditLogCount),
                ]),
            ]);
        };

        const errorBanner = () => {
            if (this.errors.length === 0) return null;
            return h('div', { class: 'error-banner' }, [
                h('span', null, `⚠️ 以下服务数据获取失败: ${this.errors.join('、')}`),
            ]);
        };

        return h('div', [
            h(NavBar),
            h('div', { class: 'container' }, [
                h('div', { class: 'dashboard-header' }, '系统状态'),
                this.loading
                    ? h('div', { class: 'card' }, '加载中...')
                    : [
                        errorBanner(),
                        h('div', { class: 'status-grid' }, [
                            diagCard(),
                            suppCard(),
                            alertCard(),
                            diagStatsCard(),
                            execCard(),
                        ]),
                        h('div', { class: 'refresh-info' }, `最近更新: ${this.lastUpdated || '-'}`),
                    ],
            ]),
        ]);
    },
}).mount('#app');