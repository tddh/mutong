import { createApp, ref, h, onMounted, computed } from 'vue';
import { NavBar } from '../components/SharedComponents.js';
import { API } from '../utils/api.js';
import '../styles/common.css';

const SEVERITY_COLORS = {
    critical: '#cf1322', error: '#cf1322', warning: '#faad14', info: '#1890ff', pass: '#52c41a',
};
const SEVERITY_BG = {
    critical: '#fff1f0', error: '#fff1f0', warning: '#fff7e6', info: '#e6f7ff', pass: '#f6ffed',
};

function formatTime(ts) {
    if (!ts) return '-';
    const d = new Date(ts);
    if (isNaN(d.getTime())) return '-';
    return d.toLocaleString('zh-CN');
}

function severityBadge(severity, text) {
    const s = (severity || '').toLowerCase();
    return h('span', {
        class: 'severity-badge',
        style: { color: SEVERITY_COLORS[s] || '#666', background: SEVERITY_BG[s] || '#f5f5f5' },
    }, text || severity);
}

function infoCard(label, value) {
    return h('div', { class: 'info-card' }, [
        h('div', { class: 'info-label' }, label),
        h('div', { class: 'info-value' }, value || '-'),
    ]);
}

createApp({
    components: { NavBar },
    setup() {
        const viewMode = ref('list');

        const items = ref([]);
        const total = ref(0);
        const page = ref(1);
        const pageSize = ref(20);
        const loading = ref(false);
        const error = ref(null);

        const filters = ref({
            start: '', end: '', severity: '', keyword: '',
        });

        const toDatetimeLocal = (d) => {
            try { return d.toISOString().slice(0, 16); } catch { return ''; }
        };
        (() => {
            const now = new Date();
            const start = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
            start.setHours(0, 0, 0, 0);
            filters.value.start = toDatetimeLocal(start);
            filters.value.end = toDatetimeLocal(now);
        })();

        const selectedId = ref(null);
        const detailLoading = ref(false);
        const detail = ref(null);
        const expandedIssues = ref({});

        const compareId1 = ref('');
        const compareId2 = ref('');
        const compareLoading = ref(false);
        const compareData = ref(null);
        const allReportOptions = ref([]);

        const trendDays = ref(7);
        const trendLoading = ref(false);
        const trendData = ref(null);

        const totalPages = () => Math.max(1, Math.ceil(total.value / pageSize.value));

        const fetchList = async (p) => {
            const pg = p || 1;
            loading.value = true;
            error.value = null;
            try {
                const now = new Date();
                const thirtyDaysAgo = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
                const toRFC3339 = (d) => {
                    if (!d) return null;
                    if (d.endsWith('Z') || d.includes('+') || d.includes('T') && d.split('T')[1].length >= 8) return d;
                    if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(d)) return d + ':00Z';
                    if (/^\d{4}-\d{2}-\d{2}$/.test(d)) return d + 'T00:00:00Z';
                    return d;
                };
                const params = {
                    limit: pageSize.value,
                    offset: (pg - 1) * pageSize.value,
                    start: toRFC3339(filters.value.start) || now.toISOString().replace(/T.*/, 'T00:00:00.000Z'),
                    end: toRFC3339(filters.value.end) || now.toISOString(),
                };
                const res = await API.inspection.list(params);
                items.value = res.reports || [];
                total.value = res.count || 0;
                page.value = pg;
            } catch (e) {
                error.value = '加载列表失败: ' + (e.message || '未知错误');
                items.value = [];
                total.value = 0;
            } finally {
                loading.value = false;
            }
        };

        const search = () => { fetchList(1); };
        const resetFilters = () => {
            const now = new Date();
            const start = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
            start.setHours(0, 0, 0, 0);
            filters.value = {
                start: toDatetimeLocal(start),
                end: toDatetimeLocal(now),
                severity: '', keyword: '',
            };
            fetchList(1);
        };
        const goPage = (p) => { if (p >= 1 && p <= totalPages()) fetchList(p); };

        const switchView = (mode) => {
            viewMode.value = mode;
            error.value = null;
            if (mode === 'compare') {
                compareData.value = null;
                compareId1.value = '';
                compareId2.value = '';
                loadAllReportOptions();
            }
            if (mode === 'trend') {
                loadTrend();
            }
            if (mode === 'list') {
                fetchList(page.value);
            }
        };

        const viewDetail = async (id) => {
            selectedId.value = id;
            detailLoading.value = true;
            detail.value = null;
            expandedIssues.value = {};
            viewMode.value = 'detail';
            try {
                detail.value = await API.inspection.detail(id);
            } catch (e) {
                error.value = '加载报告详情失败: ' + (e.message || '未知错误');
            } finally {
                detailLoading.value = false;
            }
        };

        const backToList = () => {
            viewMode.value = 'list';
            selectedId.value = null;
            detail.value = null;
            expandedIssues.value = {};
        };

        const toggleIssueExpand = (idx) => {
            expandedIssues.value[idx] = !expandedIssues.value[idx];
        };

        const loadAllReportOptions = async () => {
            try {
                const now = new Date();
                const start = new Date(now.getTime() - 90 * 24 * 60 * 60 * 1000);
                const res = await API.inspection.list({ limit: 200, offset: 0, start: start.toISOString(), end: now.toISOString() });
                allReportOptions.value = (res.reports || []).map(r => ({
                    id: r.id,
                    label: (r.id ? r.id.slice(0, 8) + '...' : '-') + ' - ' + (formatTime(r.generatedAt)),
                }));
            } catch (e) {
                // silent
            }
        };

        const runCompare = async () => {
            if (!compareId1.value || !compareId2.value) {
                error.value = '请选择两份报告进行对比';
                return;
            }
            compareLoading.value = true;
            compareData.value = null;
            error.value = null;
            try {
                compareData.value = await API.inspection.compare(compareId1.value, compareId2.value);
            } catch (e) {
                error.value = '对比失败: ' + (e.message || '未知错误');
            } finally {
                compareLoading.value = false;
            }
        };

        const loadTrend = async () => {
            trendLoading.value = true;
            trendData.value = null;
            error.value = null;
            try {
                trendData.value = await API.inspection.trend(trendDays.value);
            } catch (e) {
                error.value = '加载趋势数据失败: ' + (e.message || '未知错误');
            } finally {
                trendLoading.value = false;
            }
        };

        const refreshTrend = () => { loadTrend(); };

        const compareFromDetail = () => {
            if (!selectedId.value) return;
            compareId1.value = selectedId.value;
            viewMode.value = 'compare';
            loadAllReportOptions();
            compareData.value = null;
            compareId2.value = '';
        };

        fetchList(1);

        return {
            viewMode, items, total, page, pageSize, loading, error, filters,
            totalPages, search, resetFilters, goPage, switchView,
            selectedId, detailLoading, detail, expandedIssues, viewDetail, backToList, toggleIssueExpand,
            compareId1, compareId2, compareLoading, compareData, allReportOptions, runCompare, compareFromDetail, loadAllReportOptions,
            trendDays, trendLoading, trendData, loadTrend, refreshTrend,
            formatTime, severityBadge, infoCard,
        };
    },
    render() {
        return h('div', { class: 'inspection-history-page' }, [
            h(NavBar),
            h('div', { class: 'container' }, [
                this.renderHeader(),
                this.error ? h('div', { class: 'error-msg' }, [
                    h('span', { style: { fontWeight: 600, marginRight: '8px' } }, '[!]'),
                    this.error,
                ]) : null,
                this.viewMode === 'list' ? this.renderList() :
                this.viewMode === 'detail' ? this.renderDetail() :
                this.viewMode === 'compare' ? this.renderCompare() :
                this.viewMode === 'trend' ? this.renderTrend() : null,
            ]),
        ]);
    },
    methods: {
        renderHeader() {
            const modes = [
                { key: 'list', label: '报告列表' },
                { key: 'trend', label: '趋势分析' },
                { key: 'compare', label: '报告对比' },
            ];
            return h('div', { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '12px' } }, [
                h('div', { class: 'dashboard-header', style: { marginBottom: 0 } }, '巡检历史'),
                h('div', { class: 'view-mode-bar' },
                    modes.map(m =>
                        h('button', {
                            class: 'view-mode-btn' + (this.viewMode === m.key ? ' active' : ''),
                            onClick: () => this.switchView(m.key),
                        }, m.label)
                    )
                ),
            ]);
        },

        renderList() {
            return [
                this.renderFilters(),
                this.loading ? h('div', { class: 'loading' }, '加载中...') :
                this.items.length === 0 ? h('div', { class: 'card empty-state' }, [
                    h('div', { class: 'icon' }, '[ ]'),
                    h('div', null, '暂无巡检报告'),
                ]) :
                h('div', { class: 'card-list', style: { background: 'var(--card-bg)', borderRadius: 'var(--card-radius)', boxShadow: 'var(--shadow-sm)' } }, [
                    ...this.items.map(item => this.renderCard(item)),
                    this.totalPages() > 1 ? h('div', { class: 'pagination' }, [
                        h('button', { disabled: this.page <= 1, onClick: () => this.goPage(this.page - 1) }, '[<] 上一页'),
                        h('span', null, '第 ' + this.page + '/' + this.totalPages() + ' 页'),
                        h('button', { disabled: this.page >= this.totalPages(), onClick: () => this.goPage(this.page + 1) }, '下一页 [>]'),
                    ]) : null,
                ]),
            ];
        },

        renderFilters() {
            return h('div', { class: 'card filters-bar' }, [
                h('input', { type: 'datetime-local', placeholder: '开始时间', value: this.filters.start, onInput: e => this.filters.start = e.target.value, style: { minWidth: '180px' } }),
                h('input', { type: 'datetime-local', placeholder: '结束时间', value: this.filters.end, onInput: e => this.filters.end = e.target.value, style: { minWidth: '180px' } }),
                h('button', { class: 'btn btn-primary btn-sm', onClick: this.search }, '搜索'),
                h('button', { class: 'btn btn-secondary btn-sm', onClick: this.resetFilters }, '重置'),
            ]);
        },

        renderCard(item) {
            const summary = item.summary || {};
            const critical = summary.critical || 0;
            const warning = summary.warning || 0;
            const info = summary.info || 0;
            const total = summary.total || 0;
            const hasCritical = critical > 0;
            const hasWarning = warning > 0;
            const stripColor = hasCritical ? SEVERITY_COLORS.error : hasWarning ? SEVERITY_COLORS.warning : SEVERITY_COLORS.pass;

            const statusBadge = hasCritical
                ? this.severityBadge('error', '存在问题')
                : hasWarning
                    ? this.severityBadge('warning', '有警告')
                    : this.severityBadge('pass', '全部通过');

            const reportLabel = '报告 #' + (item.id || '').slice(0, 12);

            const metaItems = [];
            if (item.generatedAt) {
                metaItems.push(h('span', null, '生成时间: ' + this.formatTime(item.generatedAt)));
            }

            const statItems = [];
            if (critical > 0) statItems.push(h('span', { class: 'summary-stat' }, [h('span', { class: 'ss-dot', style: { background: SEVERITY_COLORS.error } }), 'Critical ' + critical]));
            if (warning > 0) statItems.push(h('span', { class: 'summary-stat' }, [h('span', { class: 'ss-dot', style: { background: SEVERITY_COLORS.warning } }), 'Warning ' + warning]));
            if (info > 0) statItems.push(h('span', { class: 'summary-stat' }, [h('span', { class: 'ss-dot', style: { background: SEVERITY_COLORS.info } }), 'Info ' + info]));
            statItems.push(h('span', { class: 'summary-stat' }, [h('span', { class: 'ss-dot', style: { background: '#d9d9d9' } }), 'Total ' + total]));

            const severityMetaItems = [];
            if (item.severity_level) severityMetaItems.push(h('span', null, '严重级别: ' + item.severity_level));
            if (item.rules_checked) severityMetaItems.push(h('span', null, '检查规则: ' + item.rules_checked));

            return h('div', { class: 'report-card', onClick: () => this.viewDetail(item.id) }, [
                h('div', { class: 'severity-strip', style: { background: stripColor } }),
                h('div', { class: 'card-body' }, [
                    h('div', { class: 'card-title' }, [statusBadge, h('span', null, reportLabel)]),
                    metaItems.length ? h('div', { class: 'card-meta' }, metaItems) : null,
                    h('div', { class: 'summary-stats' }, statItems),
                    h('div', { class: 'card-actions' }, [
                        h('button', { class: 'btn btn-primary btn-sm', onClick: e => { e.stopPropagation(); this.viewDetail(item.id); } }, '查看详情'),
                        h('button', { class: 'btn btn-secondary btn-sm', onClick: e => { e.stopPropagation(); this.compareFromList(item.id); } }, '对比'),
                    ]),
                ]),
            ]);
        },

        compareFromList(id) {
            this.compareId1 = id;
            this.viewMode = 'compare';
            this.loadAllReportOptions();
            this.compareData = null;
            this.compareId2 = '';
        },

        renderDetail() {
            const loading = this.detailLoading;
            const d = this.detail;
            if (loading) return h('div', { class: 'loading' }, '加载中...');
            if (!d) return null;

            const summary = d.summary || {};
            const critical = summary.critical || 0;
            const warning = summary.warning || 0;
            const info = summary.info || 0;
            const total = summary.total || (critical + warning + info);

            return h('div', { class: 'card detail-panel' }, [
                h('div', { class: 'detail-header' }, [
                    h('span', { class: 'back-link', onClick: this.backToList }, '[<-] 返回列表'),
                    h('div', { class: 'detail-actions' }, [
                        h('button', { class: 'btn btn-primary btn-sm', onClick: this.compareFromDetail }, '对比其他报告'),
                    ]),
                ]),

                h('div', { class: 'info-grid' }, [
                    this.infoCard('报告 ID', (d.id || '').slice(0, 16) + '...'),
                    this.infoCard('生成时间', this.formatTime(d.generatedAt)),
                    this.infoCard('Critical', h('span', { style: { color: SEVERITY_COLORS.error, fontWeight: 600 } }, String(critical))),
                    this.infoCard('Warning', h('span', { style: { color: SEVERITY_COLORS.warning, fontWeight: 600 } }, String(warning))),
                    this.infoCard('Info', h('span', { style: { color: SEVERITY_COLORS.info, fontWeight: 600 } }, String(info))),
                    this.infoCard('总计', String(total)),
                ].filter(Boolean)),

                d.issues?.length ? this.renderSection('问题详情 (' + d.issues.length + ')', this.renderIssuesList(d.issues)) : null,
            ]);
        },

        renderIssuesList(issues) {
            return issues.map((issue, idx) => {
                const sev = (issue.severity || 'warning').toLowerCase();
                const isExpanded = !!this.expandedIssues[idx];
                return h('div', { class: 'issue-card', key: idx }, [
                    h('div', {
                        class: 'issue-header' + (isExpanded ? ' expanded' : ''),
                        onClick: () => this.toggleIssueExpand(idx),
                    }, [
                        this.severityBadge(sev, sev.toUpperCase()),
                        h('span', { class: 'issue-rule-name' }, issue.ruleName || '未命名规则'),
                        h('span', { style: { color: 'var(--text-secondary)', fontSize: '12px' } }, isExpanded ? '\u25b2' : '\u25bc'),
                    ]),
                    isExpanded ? h('div', { class: 'issue-body' }, [
                        issue.message ? h('div', { class: 'issue-body-row' }, [
                            h('div', { class: 'issue-body-label' }, '问题描述'),
                            h('div', null, issue.message),
                        ]) : null,
                        issue.suggestion ? h('div', { class: 'issue-body-row' }, [
                            h('div', { class: 'issue-body-label' }, '建议'),
                            h('div', { style: { color: '#389e0d' } }, issue.suggestion),
                        ]) : null,
                        issue.resources?.length ? h('div', { class: 'issue-body-row' }, [
                            h('div', { class: 'issue-body-label' }, '关联资源'),
                            h('div', { style: { fontFamily: 'monospace', fontSize: '12px' } },
                                issue.resources.join(', ')
                            ),
                        ]) : null,
                    ].filter(Boolean)) : null,
                ]);
            });
        },

        renderPassedRules(rules) {
            return h('div', { class: 'passed-rules' },
                rules.map((r, idx) => h('span', { class: 'passed-rule-tag', key: idx },
                    r.ruleName || '-'
                ))
            );
        },

        renderCompare() {
            const opts = this.allReportOptions.map(o =>
                h('option', { value: o.id }, o.label)
            );
            return [
                h('div', { class: 'card', style: { marginTop: '20px' } }, [
                    h('div', { style: { fontWeight: 600, marginBottom: '12px' } }, '选择两份巡检报告进行对比'),
                    h('div', { class: 'compare-pick-row' }, [
                        h('span', { style: { fontSize: '13px', color: 'var(--text-secondary)', minWidth: '60px' } }, '报告 1:'),
                        h('select', { value: this.compareId1, onChange: e => this.compareId1 = e.target.value }, [
                            h('option', { value: '' }, '-- 请选择 --'),
                            ...opts,
                        ]),
                    ]),
                    h('div', { class: 'compare-pick-row' }, [
                        h('span', { style: { fontSize: '13px', color: 'var(--text-secondary)', minWidth: '60px' } }, '报告 2:'),
                        h('select', { value: this.compareId2, onChange: e => this.compareId2 = e.target.value }, [
                            h('option', { value: '' }, '-- 请选择 --'),
                            ...opts,
                        ]),
                    ]),
                    h('button', { class: 'btn btn-primary btn-sm', onClick: this.runCompare, disabled: this.compareLoading, style: { marginTop: '8px' } },
                        this.compareLoading ? '对比中...' : '开始对比'),
                ]),

                this.compareLoading ? h('div', { class: 'loading' }, '对比中...') :
                this.compareData ? this.renderCompareResult() : null,
            ];
        },

        renderCompareResult() {
            const cd = this.compareData;
            const newIssues = cd.newIssues || [];
            const resolvedIssues = cd.resolvedIssues || [];
            const persistentIssues = cd.persistentIssues || [];
            const trend = cd.trend || 'stable';

            return h('div', { style: { marginTop: '20px' } }, [
                h('div', { class: 'card', style: { marginBottom: '16px' } }, [
                    h('div', { style: { fontWeight: 600 } }, '对比结果'),
                    h('div', { style: { fontSize: '13px', color: 'var(--text-secondary)', marginTop: '4px' } },
                        '趋势: ' + (trend === 'improving' ? '改善' : trend === 'worsening' ? '恶化' : '稳定') +
                        ' | 新增: ' + newIssues.length +
                        ' | 已解决: ' + resolvedIssues.length +
                        ' | 持续: ' + persistentIssues.length
                    ),
                ]),

                newIssues.length ? this.renderSection('新增问题 (' + newIssues.length + ')', this.renderDiffIssues(newIssues, 'added')) : null,
                resolvedIssues.length ? this.renderSection('已解决问题 (' + resolvedIssues.length + ')', this.renderDiffIssues(resolvedIssues, 'removed')) : null,
                persistentIssues.length ? this.renderSection('持续问题 (' + persistentIssues.length + ')', this.renderDiffIssues(persistentIssues, 'unchanged')) : null,
            ]);
        },

        renderDiffIssues(issues, diffType) {
            const diffClassMap = {
                added: 'compare-diff-added', removed: 'compare-diff-removed',
                worsened: 'compare-diff-worsened', improved: 'compare-diff-improved',
                unchanged: '',
            };
            const diffClass = diffClassMap[diffType] || '';
            return issues.map((issue, idx) => {
                const sev = (issue.severity || 'warning').toLowerCase();
                const bodyItems = [];
                if (issue.message) bodyItems.push(h('div', { class: 'issue-body-row' }, issue.message));
                if (issue.suggestion) bodyItems.push(h('div', { class: 'issue-body-row', style: { color: '#389e0d' } }, issue.suggestion));
                return h('div', { class: 'issue-card ' + diffClass, key: idx }, [
                    h('div', { class: 'issue-header' }, [
                        this.severityBadge(sev, sev.toUpperCase()),
                        h('span', { class: 'issue-rule-name' }, issue.ruleName || '未命名规则'),
                    ]),
                    bodyItems.length ? h('div', { class: 'issue-body' }, bodyItems) : null,
                ]);
            });
        },

        renderTrend() {
            return [
                h('div', { class: 'card', style: { marginTop: '20px' } }, [
                    h('div', { class: 'trend-controls' }, [
                        h('span', { style: { fontSize: '13px', color: 'var(--text-secondary)' } }, '最近'),
                        h('select', { value: this.trendDays, onChange: e => { this.trendDays = Number(e.target.value); } }, [
                            h('option', { value: 7 }, '7 天'),
                            h('option', { value: 14 }, '14 天'),
                            h('option', { value: 30 }, '30 天'),
                            h('option', { value: 60 }, '60 天'),
                            h('option', { value: 90 }, '90 天'),
                        ]),
                        h('button', { class: 'btn btn-primary btn-sm', onClick: this.refreshTrend, disabled: this.trendLoading }, this.trendLoading ? '加载中...' : '刷新'),
                    ]),
                ]),

                this.trendLoading ? h('div', { class: 'loading' }, '加载中...') :
                this.trendData?.trend?.length ? this.renderTrendChart(this.trendData.trend) :
                h('div', { class: 'card empty-state' }, [h('div', { class: 'icon' }, '[ ]'), h('div', null, '暂无趋势数据')]),
            ];
        },

        renderTrendChart(points) {
            const maxTotal = Math.max(...points.map(p => {
                return p.count || 0;
            }), 1);

            return h('div', { class: 'card trend-section' }, [
                h('div', { class: 'section-title' }, '巡检通过率趋势'),
                h('div', { class: 'trend-chart' },
                    points.map((p, idx) => {
                        const count = p.count || 0;
                        const barPct = maxTotal > 0 ? (count / maxTotal) * 100 : 0;
                        return h('div', { class: 'trend-bar-row', key: idx }, [
                            h('span', { class: 'trend-date' }, p.date || '-'),
                            h('div', { class: 'trend-bars' }, [
                                h('div', { class: 'trend-bar-seg trend-bar-seg-pass', style: { width: barPct + '%' } }),
                            ]),
                            h('span', { class: 'trend-stats' }, String(count)),
                        ]);
                    })
                ),
                h('div', { style: { display: 'flex', gap: '16px', marginTop: '12px', justifyContent: 'center', fontSize: '12px', color: 'var(--text-secondary)' } }, [
                    h('span', null, [h('span', { style: { display: 'inline-block', width: '12px', height: '12px', borderRadius: '3px', background: SEVERITY_COLORS.pass, marginRight: '4px', verticalAlign: 'middle' } }), 'Pass']),
                    h('span', null, [h('span', { style: { display: 'inline-block', width: '12px', height: '12px', borderRadius: '3px', background: SEVERITY_COLORS.warning, marginRight: '4px', verticalAlign: 'middle' } }), 'Warn']),
                    h('span', null, [h('span', { style: { display: 'inline-block', width: '12px', height: '12px', borderRadius: '3px', background: SEVERITY_COLORS.error, marginRight: '4px', verticalAlign: 'middle' } }), 'Fail']),
                ]),
            ]);
        },

        renderSection(title, content) {
            return h('div', { class: 'section' }, [
                h('div', { class: 'section-title' }, title),
                content,
            ]);
        },
    },
}).mount('#app');
