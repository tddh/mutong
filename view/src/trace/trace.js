import { createApp, ref, onMounted, h } from 'vue';
import { NavBar } from '../components/SharedComponents.js';
import { API } from '../utils/api.js';
import '../styles/common.css';

const serviceColors = [
    '#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6',
    '#06b6d4', '#84cc16', '#f97316', '#6366f1', '#14b8a6',
];

const getServiceColor = (serviceName, index) => {
    const hash = serviceName.split('').reduce((acc, char) => acc + char.charCodeAt(0), 0);
    return serviceColors[hash % serviceColors.length] || serviceColors[index % serviceColors.length];
};

createApp({
    components: { NavBar },
    setup() {
        const services = ref([]);
        const serviceName = ref('');
        const operationName = ref('');
        const traceId = ref('');
        const limit = ref(20);
        const traces = ref([]);
        const selectedTrace = ref(null);
        const spans = ref([]);
        const loading = ref(false);
        const error = ref(null);

        const fetchServices = async () => {
            const result = await API.trace.services();
            services.value = result || [];
        };

        const fetchTraces = async () => {
            loading.value = true;
            error.value = null;
            try {
                const result = await API.trace.query(
                    serviceName.value,
                    operationName.value,
                    traceId.value,
                    limit.value
                );
                traces.value = result || [];
                if (traces.value.length > 0 && traceId.value) {
                    await fetchSpans(traces.value[0].traceId);
                } else {
                    selectedTrace.value = null;
                    spans.value = [];
                }
            } catch (e) {
                error.value = e.message || '查询失败';
                traces.value = [];
            }
            loading.value = false;
        };

        const fetchSpans = async (id) => {
            const result = await API.trace.spans(id);
            spans.value = result || [];
            selectedTrace.value = traces.value.find(t => t.traceId === id) || { traceId: id };
        };

        const formatDuration = (ms) => {
            if (ms < 1000) return `${ms}ms`;
            if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`;
            return `${(ms / 60000).toFixed(2)}m`;
        };

        const formatTime = (t) => {
            if (!t) return '-';
            try {
                const d = new Date(t);
                return d.toLocaleTimeString();
            } catch {
                return t;
            }
        };

        onMounted(() => {
            fetchServices();
        });

        return {
            services, serviceName, operationName, traceId, limit,
            traces, selectedTrace, spans, loading, error,
            fetchServices, fetchTraces, fetchSpans,
            formatDuration, formatTime,
        };
    },
    render() {
        const filterBar = () =>
            h('div', { class: 'card', style: 'margin-bottom:20px;' }, [
                h('div', { style: 'display:flex;flex-wrap:wrap;gap:12px;align-items:flex-end;' }, [
                    h('div', { class: 'log-filter-group' }, [
                        h('label', { class: 'log-filter-label' }, '服务'),
                        h('select', {
                            class: 'log-filter-select',
                            value: this.serviceName,
                            onChange: (e) => { this.serviceName = e.target.value; },
                        }, [
                            h('option', { value: '' }, '全部'),
                            ...this.services.map(s => h('option', { value: s }, s)),
                        ]),
                    ]),
                    h('div', { class: 'log-filter-group' }, [
                        h('label', { class: 'log-filter-label' }, '操作'),
                        h('input', {
                            class: 'log-filter-input',
                            value: this.operationName,
                            onInput: (e) => { this.operationName = e.target.value; },
                            placeholder: '操作名称',
                        }),
                    ]),
                    h('div', { class: 'log-filter-group' }, [
                        h('label', { class: 'log-filter-label' }, 'Trace ID'),
                        h('input', {
                            class: 'log-filter-input',
                            value: this.traceId,
                            onInput: (e) => { this.traceId = e.target.value; },
                            placeholder: '精确查询',
                        }),
                    ]),
                    h('div', { class: 'log-filter-group' }, [
                        h('label', { class: 'log-filter-label' }, '数量'),
                        h('input', {
                            class: 'log-filter-input',
                            type: 'number',
                            value: this.limit,
                            onInput: (e) => { this.limit = parseInt(e.target.value) || 20; },
                            min: '1',
                            max: '100',
                            style: 'width:80px;',
                        }),
                    ]),
                    h('button', {
                        class: 'log-filter-btn',
                        onClick: this.fetchTraces,
                    }, '查询'),
                ]),
            ]);

        const traceList = () => {
            if (this.loading && this.traces.length === 0) {
                return h('div', { class: 'card', style: 'text-align:center;padding:40px;color:var(--text-secondary);' }, '加载中...');
            }
            if (this.error) {
                return h('div', { class: 'error-banner' }, `⚠️ ${this.error}`);
            }
            if (this.traces.length === 0) {
                return h('div', { class: 'card', style: 'text-align:center;padding:40px;color:var(--text-secondary);' }, '暂无追踪数据');
            }

            return h('div', { class: 'card', style: 'max-height:300px;overflow-y:auto;' },
                this.traces.map(t =>
                    h('div', {
                        style: `padding:12px;border-bottom:1px solid var(--border-color);cursor:pointer;${this.selectedTrace?.traceId === t.traceId ? 'background:var(--primary-light);' : ''}`,
                        onClick: () => this.fetchSpans(t.traceId),
                    }, [
                        h('div', { style: 'display:flex;justify-content:space-between;' }, [
                            h('span', { style: 'font-weight:600;' }, t.traceId?.substring(0, 16) || '-'),
                            h('span', { style: 'color:var(--text-secondary);' }, this.formatDuration(t.duration || 0)),
                        ]),
                        h('div', { style: 'font-size:12px;color:var(--text-secondary);' }, `${t.spans?.length || 0} spans · ${this.formatTime(t.startTime)}`),
                    ])
                )
            );
        };

        const waterfallChart = () => {
            if (!this.selectedTrace || this.spans.length === 0) {
                return null;
            }

            const maxDuration = Math.max(...this.spans.map(s => s.duration || 0), 1);
            const minTime = Math.min(...this.spans.map(s => new Date(s.startTime).getTime()), Date.now());

            return h('div', { class: 'card' }, [
                h('div', { style: 'margin-bottom:12px;font-weight:600;' }, `Trace: ${this.selectedTrace.traceId}`),
                h('div', { style: 'overflow-x:auto;' },
                    this.spans.map((s, i) => {
                        const left = ((new Date(s.startTime).getTime() - minTime) / maxDuration) * 100;
                        const width = Math.max(((s.duration || 0) / maxDuration) * 100, 2);
                        const color = getServiceColor(s.serviceName || 'unknown', i);
                        const isError = s.status !== 'ok' && s.status !== 'OK';

                        return h('div', {
                            style: `display:flex;align-items:center;margin:4px 0;position:relative;`,
                        }, [
                            h('div', {
                                style: `width:200px;font-size:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;`,
                                title: `${s.serviceName || '-'} / ${s.operationName || '-'}`,
                            }, [
                                h('span', { style: `color:${color};font-weight:600;` }, s.serviceName || '-'),
                                h('span', { style: 'color:var(--text-secondary);' }, ` / ${s.operationName || '-'}`),
                            ]),
                            h('div', {
                                style: `flex:1;height:24px;background:var(--bg-color);border-radius:4px;position:relative;`,
                            }, [
                                h('div', {
                                    style: `position:absolute;left:${left}%;width:${width}%;height:100%;background:${isError ? '#ef4444' : color};border-radius:4px;opacity:0.8;`,
                                    title: `${s.operationName} (${this.formatDuration(s.duration || 0)})`,
                                }),
                            ]),
                            h('div', {
                                style: `width:80px;font-size:11px;color:var(--text-secondary);text-align:right;`,
                            }, this.formatDuration(s.duration || 0)),
                        ]);
                    })
                ),
            ]);
        };

        return h('div', [
            h(NavBar),
            h('div', { class: 'container' }, [
                h('div', { class: 'dashboard-header' }, '🔗 链路追踪'),
                filterBar(),
                h('div', { style: 'display:grid;grid-template-columns:1fr 2fr;gap:20px;' }, [
                    traceList(),
                    waterfallChart(),
                ]),
            ]),
        ]);
    },
}).mount('#app');