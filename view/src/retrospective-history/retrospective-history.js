import { createApp, ref, h, onMounted, onUnmounted, nextTick } from 'vue';
import { NavBar } from '../components/SharedComponents.js';
import { API } from '../utils/api.js';
import G6 from '@antv/g6';
import '../styles/common.css';

// G6 图实例存储（模块级，供 setup 和 methods 共享）
const graphStore = {};

// 根据 causal_chain.links 推断拓扑节点角色
function inferNodeRoles(topologyNodes, causalLinks) {
    const roles = {};
    if (!topologyNodes || !topologyNodes.length || !causalLinks || !causalLinks.length) {
        (topologyNodes || []).forEach(n => { roles[n.uid] = 'normal'; });
        return roles;
    }
    const uidByName = {};
    topologyNodes.forEach(n => { uidByName[n.name] = n.uid; });
    const rootCauseName = causalLinks[0].cause;
    const rootCauseNode = topologyNodes.find(n => rootCauseName.indexOf(n.name) >= 0);
    const affectedNames = new Set();
    causalLinks.forEach((link, idx) => {
        if (idx === 0) affectedNames.add(link.cause);
        affectedNames.add(link.effect);
    });
    function matches(name, causalText) { return causalText.indexOf(name) >= 0; }
    topologyNodes.forEach(n => {
        if (n.name === rootCauseName || n.uid === rootCauseName || (rootCauseNode && n.uid === rootCauseNode.uid)) {
            roles[n.uid] = 'root_cause';
        } else if (affectedNames.has(n.name) || affectedNames.has(n.uid) || [...affectedNames].some(t => matches(n.name, t))) {
            roles[n.uid] = 'affected';
        } else {
            roles[n.uid] = 'normal';
        }
    });
    return roles;
}

const SEVERITY_COLORS = {
    critical: '#cf1322', error: '#cf1322', warning: '#faad14', info: '#1890ff',
};
const SEVERITY_BG = {
    critical: '#fff1f0', error: '#fff1f0', warning: '#fff7e6', info: '#e6f7ff',
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
        const items = ref([]);
        const total = ref(0);
        const page = ref(1);
        const pageSize = ref(20);
        const loading = ref(false);
        const error = ref(null);

        const filters = ref({
            severity: '', keyword: '', start_time: '', end_time: '',
        });

        const toDatetimeLocal = (d) => {
            try { return d.toISOString().slice(0, 16); } catch { return ''; }
        };

        (() => {
            const now = new Date();
            const start = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
            start.setHours(0, 0, 0, 0);
            filters.value.start_time = toDatetimeLocal(start);
            filters.value.end_time = toDatetimeLocal(now);
        })();

        const selectedFp = ref(null);
        const detailLoading = ref(false);
        const postmortem = ref(null);
        const editing = ref(false);
        const saving = ref(false);
        const editForm = ref({
            root_cause: '', resolution: '', lessons_learned: [], action_items: [],
        });

        const fetchList = async (p) => {
            const pg = p || 1;
            loading.value = true;
            error.value = null;
            try {
                const params = { page: pg, page_size: pageSize.value };
                if (filters.value.severity) params.severity = filters.value.severity;
                if (filters.value.keyword) params.keyword = filters.value.keyword;
                if (filters.value.start_time) params.start_time = filters.value.start_time;
                if (filters.value.end_time) params.end_time = filters.value.end_time;
                const res = await API.retrospective.list(params);
                items.value = res.items || [];
                total.value = res.total || 0;
                page.value = res.page || pg;
            } catch (e) {
                error.value = '加载列表失败: ' + (e.message || '未知错误');
                items.value = [];
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
                severity: '', keyword: '',
                start_time: toDatetimeLocal(start),
                end_time: toDatetimeLocal(now),
            };
            fetchList(1);
        };
        const goPage = (p) => { if (p >= 1 && p <= totalPages.value) fetchList(p); };

        const totalPages = () => Math.max(1, Math.ceil(total.value / pageSize.value));

        const viewDetail = async (fingerprint) => {
            selectedFp.value = fingerprint;
            detailLoading.value = true;
            postmortem.value = null;
            editing.value = false;
            try {
                const stored = await API.retrospective.history(fingerprint);
                if (stored?.report_json) {
                    try {
                        postmortem.value = JSON.parse(stored.report_json);
                    } catch (parseErr) {
                        console.error('JSON parse failed:', parseErr.message, 'raw:', stored.report_json.substring(0, 200));
                        error.value = '数据解析失败: ' + parseErr.message;
                    }
                }
            } catch (e) {
                error.value = '加载复盘详情失败: ' + (e.message || '未知错误');
            } finally {
                detailLoading.value = false;
            }
        };

        const backToList = () => {
            selectedFp.value = null;
            postmortem.value = null;
            editing.value = false;
        };

        const startEdit = () => {
            if (!postmortem.value) return;
            const r = postmortem.value;
            editForm.value = {
                root_cause: typeof r.root_cause === 'string' ? r.root_cause : (r.root_cause?.final || ''),
                resolution: typeof r.resolution === 'string' ? r.resolution : (r.resolution?.final || ''),
                lessons_learned: [...(r.lessons_learned || [])],
                action_items: (r.action_items || []).map(a => ({ ...a })),
            };
            editing.value = true;
        };

        const cancelEdit = () => { editing.value = false; };

        const saveEdit = async () => {
            if (!selectedFp.value || saving.value) return;
            saving.value = true;
            try {
                const payload = {
                    root_cause: editForm.value.root_cause,
                    resolution: editForm.value.resolution,
                    lessons_learned: editForm.value.lessons_learned,
                    action_items: editForm.value.action_items,
                };
                const updated = await API.retrospective.update(selectedFp.value, payload);
                postmortem.value = updated;
                editing.value = false;
            } catch (e) {
                error.value = '保存失败: ' + (e.message || '未知错误');
            } finally {
                saving.value = false;
            }
        };

        const addLesson = () => { editForm.value.lessons_learned.push(''); };
        const removeLesson = (i) => { editForm.value.lessons_learned.splice(i, 1); };
        const addActionItem = () => {
            editForm.value.action_items.push({ description: '', priority: 'medium', owner: '', due_date: '', status: 'open' });
        };
        const removeActionItem = (i) => { editForm.value.action_items.splice(i, 1); };

        const exportMd = async (fingerprint) => {
            try {
                const text = await API.retrospective.postmortemText(fingerprint);
                if (!text) return;
                const blob = new Blob([text], { type: 'text/markdown;charset=utf-8' });
                const url = URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url;
                a.download = `postmortem_${fingerprint.slice(0, 16)}.md`;
                a.click();
                URL.revokeObjectURL(url);
            } catch (e) {
                error.value = '导出失败: ' + (e.message || '未知错误');
            }
        };

        fetchList(1);

        const fullscreenGraph = ref(null);
        const toggleFullscreen = (name) => {
            const wasFull = fullscreenGraph.value === name;
            fullscreenGraph.value = wasFull ? null : name;
            if (!wasFull) {
                requestAnimationFrame(() => {
                    requestAnimationFrame(() => {
                        const inst = name === 'topo' ? graphStore._topoGraphInstance : graphStore._causalGraphInstance;
                        if (inst) {
                            const el = inst.get('container');
                            inst.changeSize(el.clientWidth, el.clientHeight);
                            inst.fitView(20);
                        }
                    });
                });
            }
        };
        const handleKeydown = (e) => {
            if (e.key === 'Escape' && fullscreenGraph.value) fullscreenGraph.value = null;
        };
        onMounted(() => document.addEventListener('keydown', handleKeydown));
        onUnmounted(() => {
            document.removeEventListener('keydown', handleKeydown);
            if (graphStore._topoGraphInstance) { graphStore._topoGraphInstance.destroy(); graphStore._topoGraphInstance = null; }
            if (graphStore._causalGraphInstance) { graphStore._causalGraphInstance.destroy(); graphStore._causalGraphInstance = null; }
        });

        return {
            items, total, page, pageSize, loading, error, filters, selectedFp,
            detailLoading, postmortem, editing, saving, editForm,
            fullscreenGraph, toggleFullscreen,
            totalPages, search, resetFilters, goPage, viewDetail, backToList,
            startEdit, cancelEdit, saveEdit, addLesson, removeLesson, addActionItem, removeActionItem,
            exportMd, formatTime, severityBadge, infoCard,
        };
    },
    render() {
        return h('div', { class: 'retrospective-history-page' }, [
            h(NavBar),
            h('div', { class: 'container' }, [
                h('div', { class: 'dashboard-header' }, this.selectedFp ? '复盘报告详情' : '历史复盘报告'),

                this.error ? h('div', { class: 'error-msg' }, [
                    h('span', { style: { fontWeight: 600, marginRight: '8px' } }, '[!]'),
                    this.error,
                ]) : null,

                this.selectedFp ? this.renderDetail() : this.renderList(),
            ]),
        ]);
    },
    methods: {
        renderList() {
            return [
                h('div', { class: 'card filters-bar' }, [
                    h('input', { type: 'datetime-local', placeholder: '开始时间', value: this.filters.start_time, onInput: e => this.filters.start_time = e.target.value, style: { minWidth: '180px' } }),
                    h('input', { type: 'datetime-local', placeholder: '结束时间', value: this.filters.end_time, onInput: e => this.filters.end_time = e.target.value, style: { minWidth: '180px' } }),
                    h('select', { value: this.filters.severity, onChange: e => this.filters.severity = e.target.value }, [
                        h('option', { value: '' }, '全部级别'),
                        h('option', { value: 'critical' }, 'Critical'),
                        h('option', { value: 'error' }, 'Error'),
                        h('option', { value: 'warning' }, 'Warning'),
                        h('option', { value: 'info' }, 'Info'),
                    ]),
                    h('input', { type: 'text', placeholder: '搜索关键词...', value: this.filters.keyword, onInput: e => this.filters.keyword = e.target.value, onKeydown: e => { if (e.key === 'Enter') this.search(); } }),
                    h('button', { class: 'btn btn-primary btn-sm', onClick: this.search }, '搜索'),
                    h('button', { class: 'btn btn-secondary btn-sm', onClick: this.resetFilters }, '重置'),
                ]),

                this.loading ? h('div', { class: 'loading' }, '加载中...') :
                this.items.length === 0 ? h('div', { class: 'card empty-state' }, [
                    h('div', { class: 'icon' }, '[ ]'),
                    h('div', null, '暂无复盘报告'),
                ]) :
                h('div', { class: 'card-list', style: { background: 'var(--card-bg)', borderRadius: 'var(--card-radius)', boxShadow: 'var(--shadow-sm)' } }, [
                    ...this.items.map(item => this.renderCard(item)),
                    this.totalPages() > 1 ? h('div', { class: 'pagination' }, [
                        h('button', { disabled: this.page <= 1, onClick: () => this.goPage(this.page - 1) }, '[<] 上一页'),
                        h('span', null, `第 ${this.page}/${this.totalPages()} 页`),
                        h('button', { disabled: this.page >= this.totalPages(), onClick: () => this.goPage(this.page + 1) }, '下一页 [>]'),
                    ]) : null,
                ]),
            ];
        },

        renderCard(item) {
            const sev = (item.severity || '').toLowerCase();
            const stripColor = SEVERITY_COLORS[sev] || '#d9d9d9';
            return h('div', { class: 'report-card', onClick: () => this.viewDetail(item.fingerprint) }, [
                h('div', { class: 'severity-strip', style: { background: stripColor } }),
                h('div', { class: 'card-body' }, [
                    h('div', { class: 'card-title' }, [
                        this.severityBadge(item.severity),
                        h('span', null, item.incident_title || '未命名报告'),
                    ]),
                    h('div', { class: 'card-meta' }, [
                        item.resource_name ? h('span', null, `资源: ${item.resource_kind || '-'}/${item.resource_name}`) : null,
                        item.namespace ? h('span', null, `命名空间: ${item.namespace}`) : null,
                        item.start_time ? h('span', null, `${this.formatTime(item.start_time)} ~ ${this.formatTime(item.end_time)} (${item.duration || '-'})`) : null,
                    ].filter(Boolean)),
                    item.root_cause_summary ? h('div', { class: 'card-summary' }, `根因: ${item.root_cause_summary}`) : null,
                    h('div', { class: 'card-actions' }, [
                        h('button', { class: 'btn btn-primary btn-sm', onClick: e => { e.stopPropagation(); this.viewDetail(item.fingerprint); } }, '查看详情'),
                        h('button', { class: 'btn btn-secondary btn-sm', onClick: e => { e.stopPropagation(); this.exportMd(item.fingerprint); } }, '导出 Markdown'),
                    ]),
                ]),
            ]);
        },

        renderDetail() {
            const loading = this.detailLoading;
            const pm = this.postmortem;
            if (loading) return h('div', { class: 'loading' }, '加载中...');
            if (!pm) return null;

            this.$nextTick(() => this._scheduleGraphInit());
            return h('div', { class: 'card detail-panel' }, [
                h('div', { class: 'detail-header' }, [
                    h('span', { class: 'back-link', onClick: this.backToList }, '[<-] 返回列表'),
                    h('div', { class: 'detail-actions' }, [
                        this.editing ? null : h('button', { class: 'btn btn-primary btn-sm', onClick: this.startEdit }, '编辑'),
                        this.editing ? h('button', { class: 'btn btn-primary btn-sm', onClick: this.saveEdit, disabled: this.saving }, this.saving ? '保存中...' : '保存') : null,
                        this.editing ? h('button', { class: 'btn btn-secondary btn-sm', onClick: this.cancelEdit }, '取消') : null,
                        h('button', { class: 'btn btn-secondary btn-sm', onClick: () => this.exportMd(this.selectedFp) }, '导出 Markdown'),
                    ]),
                ]),

                h('div', { class: 'info-grid' }, [
                    this.infoCard('事故标题', pm.incident_title),
                    this.infoCard('开始时间', this.formatTime(pm.start_time)),
                    this.infoCard('结束时间', this.formatTime(pm.end_time)),
                    this.infoCard('持续时间', pm.duration),
                    this.infoCard('严重级别', this.severityBadge(pm.severity)),
                    this.infoCard('生成时间', this.formatTime(pm.generated_at)),
                    pm.detection_method ? this.infoCard('检测方式', pm.detection_method) : null,
                    pm.mttd ? this.infoCard('MTTD', pm.mttd) : null,
                ].filter(Boolean)),

                pm.business_context ? this.renderBusinessContext(pm.business_context) : null,
                pm.business_calls && (pm.business_calls.upstreams?.length || pm.business_calls.downstreams?.length) ? this.renderSection('业务调用链', this.renderBusinessCalls(pm.business_calls)) : null,
                pm.business_impact && (pm.business_impact.direct_impacts?.length || pm.business_impact.indirect_impacts?.length) ? this.renderSection('业务影响分析', this.renderBusinessImpact(pm.business_impact)) : null,
                pm.workload_context ? this.renderWorkloadContext(pm.workload_context) : null,
                pm.impact_assessment ? this.renderSection('影响评估详情', this.renderImpactAssessment(pm.impact_assessment)) : null,
                pm.diagnosis_metrics?.length ? this.renderSection('指标快照', this.renderMetricsBar(pm.diagnosis_metrics)) : null,
                pm.impacted_services?.length ? this.renderSection('受影响服务', this.renderImpactedSvc(pm.impacted_services)) : null,

                this.renderTopoSection(pm),
                this.renderCausalDagSection(pm),

                pm.timeline?.length ? this.renderTimeline(pm.timeline) : null,
                pm.causal_chain ? this.renderCausalChain(pm.causal_chain) : null,

                this.editing ? this.renderEditableField('📝 诊断结论', 'root_cause', this.editForm.root_cause, pm.root_cause) : this.renderTextField('📝 诊断结论', pm.root_cause, 'warning'),
                this.editing ? this.renderEditableField('解决措施', 'resolution', this.editForm.resolution, pm.resolution) : this.renderTextField('解决措施', pm.resolution, 'success'),
                this.renderLessonsLearned(),
                pm.what_went_well?.length ? this.renderSection('✅ 做得好的', this.renderCheckList(pm.what_went_well, '#52c41a')) : null,
                pm.what_went_wrong?.length ? this.renderSection('❌ 可以改进的', this.renderCheckList(pm.what_went_wrong, '#ff4d4f')) : null,
                pm.contributing_factors?.length ? this.renderSection('🔍 促成因素', this.renderCheckList(pm.contributing_factors, '#8c8c8c')) : null,
                this.renderActionItems(),
                pm.ai_diagnosis ? this.renderAiDiagnosis(pm.ai_diagnosis) : null,
            ]);
        },

        _destroyTopoGraph() {
            if (graphStore._topoGraphInstance) { graphStore._topoGraphInstance.destroy(); graphStore._topoGraphInstance = null; }
        },
        _destroyCausalGraph() {
            if (graphStore._causalGraphInstance) { graphStore._causalGraphInstance.destroy(); graphStore._causalGraphInstance = null; }
        },
        _initTopoGraph() {
            this._destroyTopoGraph();
            const pm = this.postmortem;
            if (!pm) return;
            const container = this.$refs.topoGraphContainerHist;
            if (!container) return;

            const bc = pm.business_calls;
            if (!bc) return;

            const centerName = bc.app_name || (pm.business_context && pm.business_context.app_name) || '';
            if (!centerName) return;

            const nodes = [];
            const edges = [];
            const centerId = 'center';
            nodes.push({ id: centerId, label: centerName, role: 'root_cause', criticality: (pm.business_context && pm.business_context.criticality) || 'high' });

            (bc.upstreams || []).forEach((u, i) => {
                const uid = 'up_' + i;
                nodes.push({ id: uid, label: u.app_name || '', role: 'upstream', criticality: u.criticality || 'medium' });
                edges.push({ source: uid, target: centerId, label: '调用' });
            });
            (bc.downstreams || []).forEach((d, i) => {
                const did = 'dn_' + i;
                nodes.push({ id: did, label: d.app_name || '', role: 'downstream', criticality: d.criticality || 'medium' });
                edges.push({ source: centerId, target: did, label: '调用' });
            });

            if (nodes.length <= 1) return;

            const roleColors = { root_cause: '#cf1322', upstream: '#fa8c16', downstream: '#1890ff' };
            const roleLabels = { root_cause: '故障业务', upstream: '上游调用方', downstream: '下游依赖方' };

            const g6Nodes = nodes.map(n => ({
                id: n.id, label: n.label, role: n.role,
                color: roleColors[n.role] || '#d9d9d9', criticality: n.criticality,
            }));
            const g6Edges = edges.map(e => ({ source: e.source, target: e.target, label: e.label }));

            const width = container.clientWidth || 700;
            const isFull = this.fullscreenGraph === 'topo';
            const height = isFull ? window.innerHeight : 400;
            const graph = new G6.Graph({
                container, width, height,
                layout: { type: 'dagre', rankdir: 'LR', nodesep: 60, ranksep: 100 },
                modes: { default: ['drag-canvas', 'zoom-canvas', 'drag-node'] },
                defaultNode: { type: 'rect', size: [140, 40], style: { radius: 8, fill: '#fafbfc', stroke: '#d9d9d9', lineWidth: 1 }, labelCfg: { style: { fontSize: 12, fill: '#333' } } },
                defaultEdge: { type: 'polyline', style: { stroke: '#aaa', lineWidth: 1.5, endArrow: true } },
            });
            graph.node(node => ({ style: { fill: (node.color || '#1890ff') + '10', stroke: node.color || '#1890ff', lineWidth: 2 }, labelCfg: { style: { fill: '#333', fontSize: 12 } } }));
            graph.edge(() => ({ labelCfg: { autoRotate: true, style: { fontSize: 10, fill: '#999' } } }));
            const tooltipEl = document.createElement('div');
            tooltipEl.style.cssText = 'position:absolute;display:none;background:rgba(0,0,0,0.8);color:#fff;padding:6px 10px;border-radius:4px;font-size:12px;pointer-events:none;z-index:100;max-width:250px;line-height:1.5;';
            container.appendChild(tooltipEl);
            graph.on('node:mouseenter', (e) => {
                const model = e.item.getModel();
                const items = [model.label];
                if (model.role) items.push('角色: ' + (roleLabels[model.role] || model.role));
                if (model.criticality) items.push('关键度: ' + model.criticality);
                tooltipEl.innerHTML = items.join('<br>');
                tooltipEl.style.display = 'block';
                tooltipEl.style.left = (e.canvasX + 15) + 'px';
                tooltipEl.style.top = (e.canvasY - 10) + 'px';
            });
            graph.on('node:mouseleave', () => { tooltipEl.style.display = 'none'; });
            graph.data({ nodes: g6Nodes, edges: g6Edges });
            graph.render();
            graphStore._topoGraphInstance = graph;
        },
        _initCausalGraph() {
            this._destroyCausalGraph();
            const pm = this.postmortem;
            if (!pm || !pm.causal_chain || !pm.causal_chain.links || pm.causal_chain.links.length === 0) return;
            const container = this.$refs.causalGraphContainerHist;
            if (!container) return;
            const dedupNameMap = {};
            pm.causal_chain.links.forEach((link, idx) => {
                const causeName = link.cause || '';
                const effectName = link.effect || '';
                if (!dedupNameMap[causeName]) dedupNameMap[causeName] = { id: 'c' + idx, label: causeName, role: idx === 0 ? 'root_cause' : 'intermediate' };
                if (!dedupNameMap[effectName]) dedupNameMap[effectName] = { id: 'e' + idx, label: effectName, role: 'effect' };
                if (idx === 0) dedupNameMap[causeName].role = 'root_cause';
            });
            const roleColors = { root_cause: '#cf1322', intermediate: '#fa8c16', effect: '#1890ff' };
            const nodes = Object.values(dedupNameMap).map(d => ({
                id: d.id, label: d.label.length > 30 ? d.label.slice(0, 30) + '...' : d.label,
                role: d.role, color: roleColors[d.role],
            }));
            const edges = pm.causal_chain.links.map((link, idx) => {
                const cid = dedupNameMap[link.cause]?.id;
                const eid = dedupNameMap[link.effect]?.id;
                return { source: cid, target: eid, label: ((link.confidence || 0) * 100).toFixed(0) + '%', confidence: link.confidence || 0 };
            });
            const isFull = this.fullscreenGraph === 'causal';
            const width = container.clientWidth || 700;
            const height = isFull ? window.innerHeight : 300;
            const graph = new G6.Graph({
                container, width, height,
                layout: { type: 'dagre', rankdir: 'LR', nodesep: 50, ranksep: 100 },
                modes: { default: ['drag-canvas', 'zoom-canvas'] },
                defaultNode: { type: 'rect', size: [160, 36], style: { radius: 6, fill: '#fafbfc', stroke: '#d9d9d9', lineWidth: 1 } },
                defaultEdge: { type: 'polyline', style: { stroke: '#aaa', lineWidth: 1.5, endArrow: true } },
            });
            graph.node(node => {
                const color = node.color || '#1890ff';
                return { style: { fill: color + '15', stroke: color, lineWidth: 2 }, labelCfg: { style: { fill: '#333', fontSize: 11 } } };
            });
            graph.edge(edge => {
                const conf = edge.confidence || 0;
                const color = conf >= 0.8 ? '#52c41a' : conf >= 0.5 ? '#faad14' : '#ff4d4f';
                return { labelCfg: { autoRotate: true, style: { fill: color, fontSize: 10, fontWeight: 600 } }, style: { stroke: color } };
            });
            graph.data({ nodes, edges });
            graph.render();
            graphStore._causalGraphInstance = graph;
        },
        _scheduleGraphInit() {
            requestAnimationFrame(() => {
                requestAnimationFrame(() => {
                    this._initTopoGraph();
                    this._initCausalGraph();
                });
            });
        },
        renderTopoSection(pm) {
            if (!pm.business_calls || (!pm.business_calls.upstreams?.length && !pm.business_calls.downstreams?.length)) {
                return this.renderSection('🔗 业务拓扑', h('div', { style: { textAlign: 'center', padding: '40px', color: '#888', fontSize: '13px' } }, '暂无业务调用链数据'));
            }
            const isFull = this.fullscreenGraph === 'topo';
            const legend = h('div', { style: isFull
                ? { position: 'fixed', top: '12px', right: '12px', zIndex: 10001, display: 'flex', gap: '8px', fontSize: '11px', color: '#666', background: 'rgba(255,255,255,0.95)', padding: '6px 12px', borderRadius: '6px', border: '1px solid #e8e8e8' }
                : { position: 'absolute', top: '8px', right: '12px', display: 'flex', gap: '8px', fontSize: '11px', color: '#666', background: 'rgba(255,255,255,0.9)', padding: '4px 10px', borderRadius: '4px', border: '1px solid #e8e8e8', zIndex: 10 }
            }, [
                h('span', { style: { display: 'flex', alignItems: 'center', gap: '4px' } }, [h('span', { style: { width: '10px', height: '10px', borderRadius: '50%', background: '#cf1322', display: 'inline-block' } }), '故障业务']),
                h('span', { style: { display: 'flex', alignItems: 'center', gap: '4px' } }, [h('span', { style: { width: '10px', height: '10px', borderRadius: '50%', background: '#fa8c16', display: 'inline-block' } }), '上游调用方']),
                h('span', { style: { display: 'flex', alignItems: 'center', gap: '4px' } }, [h('span', { style: { width: '10px', height: '10px', borderRadius: '50%', background: '#1890ff', display: 'inline-block' } }), '下游依赖方']),
                h('span', { style: { cursor: 'pointer', fontWeight: 600, color: '#1890ff', userSelect: 'none' }, onClick: () => this.toggleFullscreen('topo') }, isFull ? '⧉ 退出' : '⛶ 全屏'),
            ]);
            return this.renderSection('🔗 业务拓扑', h('div', { style: { position: 'relative' } }, [
                h('div', { ref: 'topoGraphContainerHist', style: isFull ? { position: 'fixed', top: 0, left: 0, width: '100vw', height: '100vh', zIndex: 9999, background: '#fff' } : { width: '100%', height: '400px', border: '1px solid var(--border-color)', borderRadius: '6px', background: '#fafbfc', position: 'relative' } }),
                legend,
            ]));
        },
        renderCausalDagSection(pm) {
            if (!pm.causal_chain || !pm.causal_chain.links || pm.causal_chain.links.length === 0) {
                return this.renderSection('🔗 因果链 DAG', h('div', { style: { textAlign: 'center', padding: '40px', color: '#888', fontSize: '13px' } }, '暂无因果链数据'));
            }
            const isFull = this.fullscreenGraph === 'causal';
            const btn = h('div', { style: isFull
                ? { position: 'fixed', top: '12px', right: '12px', zIndex: 10001, fontSize: '11px', color: '#666', background: 'rgba(255,255,255,0.95)', padding: '6px 12px', borderRadius: '6px', border: '1px solid #e8e8e8' }
                : { position: 'absolute', top: '8px', right: '12px', fontSize: '11px', color: '#666', background: 'rgba(255,255,255,0.9)', padding: '4px 10px', borderRadius: '4px', border: '1px solid #e8e8e8', zIndex: 10 }
            }, [
                h('span', { style: { cursor: 'pointer', fontWeight: 600, color: '#1890ff', userSelect: 'none' }, onClick: () => this.toggleFullscreen('causal') }, isFull ? '⧉ 退出' : '⛶ 全屏'),
            ]);
            return this.renderSection('🔗 因果链 DAG', h('div', { style: { position: 'relative' } }, [
                h('div', { ref: 'causalGraphContainerHist', style: isFull ? { position: 'fixed', top: 0, left: 0, width: '100vw', height: '100vh', zIndex: 9999, background: '#fff' } : { width: '100%', height: '300px', border: '1px solid var(--border-color)', borderRadius: '6px', background: '#fafbfc' } }),
                btn,
            ]));
        },

        renderSection(title, content) {
            return h('div', { class: 'section' }, [
                h('div', { class: 'section-title' }, title),
                content,
            ]);
        },

        renderCheckList(items, color) {
            return h('div', { style: { display: 'flex', flexDirection: 'column', gap: '6px' } },
                items.map((item, idx) => h('div', { key: idx, style: { display: 'flex', gap: '8px', alignItems: 'flex-start', padding: '8px 12px', background: color + '10', borderLeft: '3px solid ' + color, borderRadius: '4px', fontSize: '13px', color: 'var(--text-primary)', lineHeight: '1.6' } }, [
                    h('span', { style: { flexShrink: 0, fontWeight: 600, color: color } }, '✓'),
                    h('span', null, item),
                ]))
            );
        },

        renderImpactedSvc(svcs) {
            return h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '8px' } },
                svcs.map((svc, idx) => h('span', { key: idx, style: { padding: '4px 12px', background: '#f5f5f5', borderRadius: '12px', fontSize: '12px', color: 'var(--text-secondary)', fontFamily: 'monospace' } }, svc))
            );
        },

        renderBusinessCalls(bc) {
            return h('div', { style: { display: 'flex', gap: '16px', flexWrap: 'wrap' } }, [
                bc.upstreams?.length ? h('div', { style: { flex: 1, minWidth: '200px' } }, [
                    h('div', { style: { fontWeight: 600, fontSize: '12px', color: '#888', marginBottom: '8px' } }, '⬆ 上游调用方'),
                    ...bc.upstreams.map((u, i) => h('div', { key: i, style: { padding: '6px 10px', background: '#fafbfc', border: '1px solid var(--border-color)', borderRadius: '4px', marginBottom: '4px', fontSize: '13px' } }, [
                        h('span', { style: { fontWeight: 500 } }, u.appName || u.app_name || '-'),
                        u.team ? h('span', { style: { marginLeft: '8px', color: '#888', fontSize: '11px' } }, u.team) : null,
                        u.criticality ? h('span', { style: { marginLeft: '8px', padding: '1px 6px', borderRadius: '3px', fontSize: '10px', background: u.criticality === 'high' ? '#fff1f0' : '#f6ffed', color: u.criticality === 'high' ? '#cf1322' : '#52c41a' } }, u.criticality) : null,
                    ])),
                ]) : null,
                bc.downstreams?.length ? h('div', { style: { flex: 1, minWidth: '200px' } }, [
                    h('div', { style: { fontWeight: 600, fontSize: '12px', color: '#888', marginBottom: '8px' } }, '⬇ 下游依赖方'),
                    ...bc.downstreams.map((d, i) => h('div', { key: i, style: { padding: '6px 10px', background: '#fafbfc', border: '1px solid var(--border-color)', borderRadius: '4px', marginBottom: '4px', fontSize: '13px' } }, [
                        h('span', { style: { fontWeight: 500 } }, d.appName || d.app_name || '-'),
                        d.team ? h('span', { style: { marginLeft: '8px', color: '#888', fontSize: '11px' } }, d.team) : null,
                        d.criticality ? h('span', { style: { marginLeft: '8px', padding: '1px 6px', borderRadius: '3px', fontSize: '10px', background: d.criticality === 'high' ? '#fff1f0' : '#f6ffed', color: d.criticality === 'high' ? '#cf1322' : '#52c41a' } }, d.criticality) : null,
                    ])),
                ]) : null,
            ].filter(Boolean));
        },

        renderBusinessImpact(bi) {
            const items = [];
            if (bi.risk_level) items.push(this.infoCard('风险等级', h('span', { style: { fontWeight: 600, color: bi.risk_level === 'critical' ? '#cf1322' : '#faad14' } }, bi.risk_level)));
            if (bi.direct_impacts?.length) items.push(h('div', { style: { gridColumn: '1 / -1' } }, [
                h('div', { style: { fontWeight: 600, fontSize: '12px', color: '#cf1322', marginBottom: '8px' } }, `直接影响 (${bi.direct_impacts.length})`),
                ...bi.direct_impacts.map((d, i) => h('div', { key: i, style: { padding: '6px 10px', background: '#fff1f0', borderLeft: '3px solid #cf1322', borderRadius: '4px', marginBottom: '4px', fontSize: '13px' } }, [
                    h('span', { style: { fontWeight: 500 } }, d.app_name || d.appName || '-'),
                    d.criticality ? h('span', { style: { marginLeft: '8px', padding: '1px 6px', borderRadius: '3px', fontSize: '10px', background: '#fff1f0', color: '#cf1322' } }, d.criticality) : null,
                    d.reasoning ? h('div', { style: { marginTop: '4px', fontSize: '11px', color: '#888' } }, d.reasoning) : null,
                ])),
            ]));
            if (bi.indirect_impacts?.length) items.push(h('div', { style: { gridColumn: '1 / -1' } }, [
                h('div', { style: { fontWeight: 600, fontSize: '12px', color: '#faad14', marginBottom: '8px' } }, `间接影响 (${bi.indirect_impacts.length})`),
                ...bi.indirect_impacts.map((d, i) => h('div', { key: i, style: { padding: '6px 10px', background: '#fff7e6', borderLeft: '3px solid #faad14', borderRadius: '4px', marginBottom: '4px', fontSize: '13px' } }, [
                    h('span', { style: { fontWeight: 500 } }, d.app_name || d.appName || '-'),
                    d.hopDistance != null ? h('span', { style: { marginLeft: '8px', fontSize: '11px', color: '#888' } }, `${d.hopDistance}跳`) : null,
                    d.impactPath ? h('div', { style: { marginTop: '4px', fontSize: '11px', color: '#888', fontFamily: 'monospace' } }, d.impactPath) : null,
                    d.reasoning ? h('div', { style: { marginTop: '2px', fontSize: '11px', color: '#888' } }, d.reasoning) : null,
                ])),
            ]));
            return h('div', { class: 'info-grid', style: { gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))' } }, items.filter(Boolean));
        },

        renderWorkloadContext(wc) {
            const items = [
                this.infoCard('控制器', `${wc.controller_kind || '-'}/${wc.controller_name || '-'}`),
                this.infoCard('副本健康', `${wc.healthy_pods || 0}/${wc.total_pods || 0}`),
            ];
            if (wc.pods?.length) {
                items.push(h('div', { style: { gridColumn: '1 / -1' } }, [
                    h('div', { style: { fontWeight: 600, fontSize: '12px', color: '#888', marginBottom: '8px' } }, 'Pod 列表'),
                    h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '6px' } },
                        wc.pods.map((p, i) => h('span', { key: i, style: { padding: '3px 10px', borderRadius: '12px', fontSize: '11px', fontFamily: 'monospace', background: p.is_alerted ? '#fff1f0' : '#f6ffed', color: p.is_alerted ? '#cf1322' : '#52c41a', border: '1px solid ' + (p.is_alerted ? '#ffa39e' : '#b7eb8f') } }, p.name))
                    ),
                ]));
            }
            return h('div', { class: 'info-grid', style: { gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))' } }, items.filter(Boolean));
        },

        renderImpactAssessment(ia) {
            return h('div', null, [
                h('div', { class: 'info-grid', style: { gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', marginBottom: '16px' } }, [
                    this.infoCard('严重级别', this.severityBadge(ia.severity || '-')),
                    this.infoCard('影响半径', (ia.blast_radius || 0) + ' 跳'),
                    this.infoCard('直接受影响', (ia.direct_impact?.length || 0) + ' 个'),
                    this.infoCard('间接影响', (ia.indirect_impact?.length || 0) + ' 个'),
                ].filter(Boolean)),
                ia.user_facing_impact ? h('div', { style: { padding: '8px 14px', background: '#fff1f0', border: '1px solid #ffa39e', borderRadius: '6px', marginBottom: '12px', fontSize: '13px', color: '#cf1322', fontWeight: 500 } }, '⚠️ 用户面受影响') : null,
                ia.affected_services?.length ? h('div', { style: { marginBottom: '12px' } }, [
                    h('div', { style: { fontWeight: 600, fontSize: '12px', color: '#888', marginBottom: '6px' } }, `受影响 K8s 服务 (${ia.affected_services.length})`),
                    h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '6px' } }, ia.affected_services.map((s, i) => h('span', { key: i, style: { padding: '3px 10px', background: '#fff7e6', border: '1px solid #ffd591', borderRadius: '12px', fontSize: '11px', fontFamily: 'monospace' } }, s))),
                ]) : null,
                ia.direct_impact?.length ? h('div', { style: { marginBottom: '12px' } }, [
                    h('div', { style: { fontWeight: 600, fontSize: '12px', color: '#cf1322', marginBottom: '6px' } }, `🔴 直接影响 (${ia.direct_impact.length})`),
                    h('table', { class: 'rh-table' }, [
                        h('thead', null, h('tr', null, [h('th', null, '类型'), h('th', null, '名称'), h('th', null, '命名空间')])),
                        h('tbody', null, ia.direct_impact.map((r, i) => h('tr', { key: i }, [
                            h('td', null, r.kind || '-'), h('td', { style: { fontFamily: 'monospace' } }, r.name || '-'), h('td', null, r.namespace || '-'),
                        ]))),
                    ]),
                ]) : null,
                ia.indirect_impact?.length ? h('div', null, [
                    h('div', { style: { fontWeight: 600, fontSize: '12px', color: '#faad14', marginBottom: '6px' } }, `🟡 间接影响 (${ia.indirect_impact.length})`),
                    h('table', { class: 'rh-table' }, [
                        h('thead', null, h('tr', null, [h('th', null, '类型'), h('th', null, '名称'), h('th', null, '命名空间')])),
                        h('tbody', null, ia.indirect_impact.slice(0, 20).map((r, i) => h('tr', { key: i }, [
                            h('td', null, r.kind || '-'), h('td', { style: { fontFamily: 'monospace' } }, r.name || '-'), h('td', null, r.namespace || '-'),
                        ]))),
                        ia.indirect_impact.length > 20 ? h('tr', null, h('td', { colSpan: 3, style: { textAlign: 'center', color: '#888' } }, `... 还有 ${ia.indirect_impact.length - 20} 条`)) : null,
                    ]),
                ]) : null,
            ]);
        },

        renderMetricsBar(metrics) {
            const maxVal = Math.max(...metrics.map(m => m.value || 0), 1);
            return h('div', { style: { display: 'flex', flexDirection: 'column', gap: '8px' } },
                metrics.map((m, idx) => {
                    const pct = Math.min((m.value / maxVal) * 100, 100);
                    const color = m.status === 'critical' ? '#cf1322' : m.status === 'warning' ? '#faad14' : '#52c41a';
                    return h('div', { key: idx, style: { display: 'flex', alignItems: 'center', gap: '10px' } }, [
                        h('span', { style: { fontSize: '12px', fontFamily: 'monospace', minWidth: '160px', color: 'var(--text-primary)' } }, m.metricName + ' / ' + m.resourceName),
                        h('div', { style: { flex: 1, height: '20px', background: '#f0f0f0', borderRadius: '4px', overflow: 'hidden' } },
                            h('div', { style: { width: pct + '%', height: '100%', background: color, borderRadius: '4px', transition: 'width 0.5s' } })
                        ),
                        h('span', { style: { fontSize: '12px', fontWeight: 600, minWidth: '50px', textAlign: 'right', color: 'var(--text-primary)' } }, (m.value || 0).toFixed(1)),
                    ]);
                })
            );
        },

        renderBusinessContext(bc) {
            return h('div', { class: 'section' }, [
                h('div', { class: 'section-title' }, '业务上下文'),
                h('div', { class: 'info-grid' }, [
                    bc.app_name ? this.infoCard('业务应用', bc.app_name) : null,
                    bc.team ? this.infoCard('团队', bc.team) : null,
                    bc.criticality ? this.infoCard('关键度', bc.criticality) : null,
                    bc.business_unit ? this.infoCard('业务线', bc.business_unit) : null,
                    bc.environment ? this.infoCard('环境', bc.environment) : null,
                ].filter(Boolean)),
            ]);
        },

        renderTimeline(events) {
            return h('div', { class: 'section' }, [
                h('div', { class: 'section-title' }, `事件时间线 (${events.length} 个事件)`),
                h('div', { class: 'timeline' }, events.map((ev, idx) => {
                    const sev = (ev.severity || 'info').toLowerCase();
                    const color = SEVERITY_COLORS[sev] || '#1890ff';
                    return h('div', { class: 'timeline-item', key: idx }, [
                        h('div', { style: { position: 'relative', width: '16px', flexShrink: 0 } }, [
                            h('div', { class: 'tl-dot', style: { color: color } }),
                            idx < events.length - 1 ? h('div', { class: 'tl-line' }) : null,
                        ]),
                        h('div', { class: 'tl-content', style: { borderLeft: `4px solid ${color}` } }, [
                            h('div', { class: 'tl-header' }, [
                                h('span', { class: 'tl-time' }, this.formatTime(ev.timestamp)),
                                this.severityBadge(sev, (ev.event_type || ev.eventType || '').toUpperCase()),
                                ev.source ? h('span', { style: { fontSize: '11px', padding: '1px 6px', borderRadius: '3px', background: '#f0f0f0', color: '#666' } }, ev.source) : null,
                            ]),
                            h('div', { class: 'tl-desc' }, ev.description || '-'),
                            ev.resource ? h('div', { style: { fontSize: '12px', color: '#888', marginTop: '6px', fontFamily: 'monospace' } }, ev.resource) : null,
                        ]),
                    ]);
                })),
            ]);
        },

		renderCausalChain(chain) {
			return h('div', { class: 'section' }, [
				h('div', { class: 'section-title' }, '因果链分析'),
				chain.root_cause ? h('div', { style: { background: '#fff7e6', border: '1px solid #ffd591', borderRadius: '8px', padding: '14px', marginBottom: '16px' } }, [
					h('div', { style: { fontSize: '13px', fontWeight: 600, color: '#d46b08', marginBottom: '6px' } }, '根因'),
					h('div', { style: { fontSize: '14px', color: '#8c4b0a', lineHeight: '1.6' } }, chain.root_cause),
				]) : null,
				...(chain.links || []).map((link, idx) => {
					const evidenceList = Array.isArray(link.evidence)
						? link.evidence.filter(Boolean)
						: (link.evidence ? [link.evidence] : []);
					const confPct = ((link.confidence || 0) * 100).toFixed(0);
					const confColor = link.confidence >= 0.8 ? '#52c41a' : link.confidence >= 0.5 ? '#faad14' : '#ff4d4f';
					return h('div', { class: 'causal-link', key: idx }, [
						h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '8px', flexWrap: 'wrap' } }, [
							h('span', { style: { fontWeight: 600, color: '#cf1322' } }, link.cause || '-'),
							h('span', { style: { color: '#999' } }, '->'),
							h('span', { style: { fontWeight: 600, color: '#1890ff' } }, link.effect || '-'),
							h('span', { style: { fontSize: '11px', padding: '1px 8px', borderRadius: '10px', background: confColor + '1a', color: confColor, fontWeight: 500, marginLeft: 'auto' } }, confPct + '%'),
						]),
						evidenceList.length ? h('div', { style: { fontSize: '12px', color: '#888', background: '#fafafa', padding: '8px 12px', borderRadius: '4px' } },
							evidenceList.map((evidence, evidenceIdx) => h('div', { key: evidenceIdx }, evidence))
						) : null,
					]);
				}),
			]);
		},

        renderTextField(title, value, variant) {
            const text = typeof value === 'string' ? value : (value?.final || value?.ai_generated || '');
            if (!text) return null;
            const bgColors = { warning: '#fff7e6', success: '#f6ffed', default: '#fafbfc' };
            const borderColors = { warning: '#ffd591', success: '#b7eb8f', default: 'var(--border-color)' };
            const bg = bgColors[variant] || bgColors.default;
            const bc = borderColors[variant] || borderColors.default;
            return h('div', { class: 'section' }, [
                h('div', { class: 'section-title' }, title),
                h('div', { style: { background: bg, border: `1px solid ${bc}`, borderRadius: '8px', padding: '16px', lineHeight: '1.6', fontSize: '14px', color: 'var(--text-primary)', whiteSpace: 'pre-wrap' } }, text),
            ]);
        },

        renderEditableField(title, key, value, original) {
            const origText = typeof original === 'string' ? original : (original?.final || original?.ai_generated || '');
            return h('div', { class: 'section edit-field' }, [
                h('div', { class: 'section-title' }, title),
                origText ? h('div', { class: 'ai-original' }, `AI 原文: ${origText.slice(0, 200)}${origText.length > 200 ? '...' : ''}`) : null,
                h('textarea', {
                    value: value,
                    onInput: e => { this.editForm[key] = e.target.value; },
                }),
            ]);
        },

        renderLessonsLearned() {
            const list = this.editing ? this.editForm.lessons_learned : (this.postmortem?.lessons_learned || []);
            if (!list.length && !this.editing) return null;
            return h('div', { class: 'section' }, [
                h('div', { class: 'section-title' }, '经验教训'),
                h('div', { style: { background: '#fafbfc', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '16px' } },
                    list.map((item, idx) => h('div', { key: idx, style: { display: 'flex', gap: '8px', padding: '8px 0', borderBottom: idx < list.length - 1 ? '1px solid #f0f0f0' : 'none', alignItems: 'flex-start' } }, [
                    h('span', { style: { fontWeight: 600, color: '#1890ff', flexShrink: 0 } }, (idx + 1) + '.'),
                    this.editing
                        ? h('input', { value: item, onInput: e => { this.editForm.lessons_learned[idx] = e.target.value; }, style: { flex: 1, padding: '6px 10px', border: '1px solid var(--border-color)', borderRadius: '4px', fontSize: '13px' } })
                        : h('span', { style: { flex: 1, fontSize: '13px', color: 'var(--text-primary)', lineHeight: '1.6' } }, item),
                    this.editing ? h('button', { class: 'btn btn-danger btn-sm', onClick: () => this.removeLesson(idx), style: { flexShrink: 0 } }, 'X') : null,
                ])),
                ),
                this.editing ? h('button', { class: 'btn btn-secondary btn-sm', onClick: this.addLesson, style: { marginTop: '8px' } }, '+ 添加') : null,
            ]);
        },

        renderActionItems() {
            const list = this.editing ? this.editForm.action_items : (this.postmortem?.action_items || []);
            if (!list.length && !this.editing) return null;
            return h('div', { class: 'section' }, [
                h('div', { class: 'section-title' }, '改进项'),
                h('table', { class: 'rh-table' }, [
                    h('thead', null, h('tr', null, [
                        h('th', null, '描述'), h('th', { style: { width: '80px' } }, '优先级'),
                        h('th', { style: { width: '100px' } }, '负责人'), h('th', { style: { width: '100px' } }, '截止日期'),
                        this.editing ? h('th', { style: { width: '40px' } }, '') : null,
                    ])),
                    h('tbody', null, list.map((item, idx) => h('tr', { key: idx }, [
                        h('td', null, this.editing
                            ? h('input', { value: item.description || '', onInput: e => { this.editForm.action_items[idx].description = e.target.value; }, placeholder: '描述' })
                            : (item.description || '-')),
                        h('td', null, this.editing
                            ? h('select', { value: item.priority || 'medium', onChange: e => { this.editForm.action_items[idx].priority = e.target.value; } }, [
                                h('option', { value: 'high' }, '高'), h('option', { value: 'medium' }, '中'), h('option', { value: 'low' }, '低'),
                            ])
                            : (item.priority || '-')),
                        h('td', null, this.editing
                            ? h('input', { value: item.owner || '', onInput: e => { this.editForm.action_items[idx].owner = e.target.value; }, placeholder: '负责人' })
                            : (item.owner || '-')),
                        h('td', null, this.editing
                            ? h('input', { value: item.due_date || '', onInput: e => { this.editForm.action_items[idx].due_date = e.target.value; }, placeholder: 'YYYY-MM-DD' })
                            : (item.due_date || '-')),
                        this.editing ? h('td', null, h('button', { class: 'btn btn-danger btn-sm', onClick: () => this.removeActionItem(idx) }, 'X')) : null,
                    ]))),
                ]),
                this.editing ? h('button', { class: 'btn btn-secondary btn-sm', onClick: this.addActionItem, style: { marginTop: '8px' } }, '+ 添加改进项') : null,
            ]);
        },

        renderAiDiagnosis(ad) {
            return h('div', { class: 'section' }, [
                h('div', { class: 'section-title' }, 'AI 诊断结论'),
                h('div', { style: { background: '#fff7e6', border: '1px solid #ffd591', borderRadius: '8px', padding: '14px' } }, [
                    h('div', { style: { fontSize: '14px', color: '#d46b08', lineHeight: '1.6', marginBottom: '8px' } }, ad.summary || ad.root_cause),
                    ad.confidence ? h('div', { style: { fontSize: '12px', color: '#999' } }, '置信度: ' + Math.round(ad.confidence * 100) + '%') : null,
                    ad.evidence?.length ? h('div', { style: { marginTop: '8px' } }, [
                        h('div', { style: { fontWeight: 600, fontSize: '12px', color: '#888', marginBottom: '6px' } }, '证据链'),
                        ...ad.evidence.map((ev, i) => h('div', { key: i, style: { padding: '4px 0', fontSize: '13px', color: '#555' } }, '- ' + ev)),
                    ]) : null,
                    ad.remediation ? h('div', { style: { marginTop: '8px', background: '#f6ffed', border: '1px solid #b7eb8f', borderRadius: '6px', padding: '10px 14px', fontSize: '13px', color: '#389e0d' } }, '建议: ' + ad.remediation) : null,
                ]),
            ]);
        },
    },
}).mount('#app');
