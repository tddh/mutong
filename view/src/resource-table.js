import { ref, onMounted, onUnmounted, computed, h, createApp } from 'vue';
import { NavBar } from './components/SharedComponents.js';
import './styles/common.css';
import './resource-table.css';

const COLORS = {
    'Pod': '#ff7875', 'Service': '#52c41a', 'Deployment': '#1890ff',
    'ReplicaSet': '#faad14', 'StatefulSet': '#722ed1', 'DaemonSet': '#13c2c2',
    'ConfigMap': '#eb2f96', 'Secret': '#73d13d', 'PersistentVolumeClaim': '#fa8c16',
    'Namespace': '#597ef7', 'Node': '#f759ab', 'Label': '#9254de',
    'Event': '#8c8c8c', 'Endpoints': '#36cfc9', 'EndpointSlice': '#36cfc9',
    'PersistentVolume': '#fa8c16', 'ServiceAccount': '#ff7a45',
    'Role': '#9254de', 'ClusterRole': '#9254de', 'Certificate': '#ff85c0',
    'CiliumEndpoint': '#2f54eb', 'ValidatingWebhookConfiguration': '#faad14',
    'Result': '#b7eb8f'
};

createApp({
    components: { NavBar },
    setup() {
        const allResources = ref([]);
        const filteredResources = ref([]);
        const metadataNamespaces = ref([]);
        const metadataKinds = ref([]);
        const currentPage = ref(1);
        const pageSize = 500;
const totalCount = ref(0);
        const loading = ref(false);
        const error = ref(null);
        const sortField = ref(null);
        const sortOrder = ref('asc');
        const namespaceFilter = ref('all');
        const kindFilter = ref('all');
        const searchTerm = ref('');

        const namespaces = computed(() => {
            if (metadataNamespaces.value.length > 0) return metadataNamespaces.value;
            const ns = new Set();
            allResources.value.forEach(r => r.namespace && ns.add(r.namespace));
            return [...ns].sort();
        });

        const kinds = computed(() => {
            if (metadataKinds.value.length > 0) return metadataKinds.value;
            const k = new Set();
            allResources.value.forEach(r => r.kind && k.add(r.kind));
            return [...k].sort();
        });

        const totalPages = computed(() => Math.ceil(totalCount.value / pageSize));

        const getKindColor = (kind) => COLORS[kind] || '#999';

        const applyFilter = () => {
            currentPage.value = 1;
            allResources.value = [];
            fetchResources();
        };

        let filterDebounceTimer = null;
        const debouncedApplyFilter = () => {
            if (filterDebounceTimer) clearTimeout(filterDebounceTimer);
            filterDebounceTimer = setTimeout(applyFilter, 300);
        };

        const sortBy = (field) => {
            if (sortField.value === field) {
                sortOrder.value = sortOrder.value === 'asc' ? 'desc' : 'asc';
            } else {
                sortField.value = field;
                sortOrder.value = 'asc';
            }
        };

        const pageData = computed(() => {
            const start = (currentPage.value - 1) * pageSize;
            return filteredResources.value.slice(start, start + pageSize);
        });

        const sortedPageData = computed(() => {
            if (!sortField.value) return pageData.value;
            return [...pageData.value].sort((a, b) => {
                let aVal = a[sortField.value];
                let bVal = b[sortField.value];
                if (typeof aVal === 'string' && typeof bVal === 'string') {
                    aVal = aVal.toLowerCase();
                    bVal = bVal.toLowerCase();
                }
                if (aVal < bVal) return sortOrder.value === 'asc' ? -1 : 1;
                if (aVal > bVal) return sortOrder.value === 'asc' ? 1 : -1;
                return 0;
            });
        });

        const fetchMetadata = async () => {
            try {
                const resp = await fetch('/k8s/resources/graph/metadata');
                if (resp.ok) {
                    const data = await resp.json();
                    if (data && data.namespaces) {
                        metadataNamespaces.value = (data.namespaces || []).sort();
                        metadataKinds.value = (data.kinds || []).sort();
                    }
                }
            } catch (e) {}
        };

        const fetchResources = async () => {
            loading.value = true;
            error.value = null;
            try {
                const params = new URLSearchParams({
                    page: currentPage.value,
                    pageSize: pageSize
                });
                const ns = namespaceFilter.value;
                const kind = kindFilter.value;
                const search = searchTerm.value;
                if (ns && ns !== 'all') params.set('namespace', ns);
                if (kind && kind !== 'all') params.set('kind', kind);
                if (search) params.set('search', search);

                const response = await fetch('/k8s/resources/graph/nodes?' + params.toString());
                if (!response.ok) throw new Error(`获取资源数据失败 (${response.status})`);
                const data = await response.json();
                const nodes = Array.isArray(data) ? data : (data.nodes || []);

                const newItems = nodes.map(node => ({
                    name: node.label, kind: node.kind, namespace: node.namespace || 'default',
                    apiVersion: node.apiVersion || '-', group: node.group || '-',
                    uid: node.id, labels: node.labels || {}
                }));

                if (currentPage.value === 1) {
                    allResources.value = newItems;
                } else {
                    const startIdx = (currentPage.value - 1) * pageSize;
                    const endIdx = startIdx + newItems.length;
                    if (allResources.value.length < endIdx) {
                        allResources.value.length = endIdx;
                    }
                    for (let i = 0; i < newItems.length; i++) {
                        allResources.value[startIdx + i] = newItems[i];
                    }
                }

                totalCount.value = data.totalCount || 0;
                filteredResources.value = allResources.value.filter(r => {
                    if (!searchTerm.value) return true;
                    const s = searchTerm.value.toLowerCase();
                    return (r.name && r.name.toLowerCase().includes(s)) ||
                           (r.kind && r.kind.toLowerCase().includes(s)) ||
                           (r.namespace && r.namespace.toLowerCase().includes(s));
                });
            } catch (err) {
                error.value = err.message || '加载失败';
                allResources.value = [];
                filteredResources.value = [];
            } finally {
                loading.value = false;
            }
        };

        const renderLabels = (labels) => {
            if (typeof labels === 'object' && labels !== null) return JSON.stringify(labels);
            if (typeof labels === 'string') return labels;
            return '{}';
        };

        const sortIcon = (field) => {
            if (sortField.value !== field) return '↕';
            return sortOrder.value === 'asc' ? '↑' : '↓';
        };

        onMounted(() => {
            fetchMetadata();
            fetchResources();
        });

        onUnmounted(() => {
            if (filterDebounceTimer) clearTimeout(filterDebounceTimer);
        });

        return {
                allResources, filteredResources, currentPage, pageSize, sortField, sortOrder,
                namespaceFilter, kindFilter, searchTerm, loading, error,
                namespaces, kinds, totalPages, pageData: sortedPageData,
                getKindColor, applyFilter, debouncedApplyFilter, sortBy, sortIcon, fetchResources, renderLabels,
                totalCount
            };
    },
    render() {
        const s = this;
        const pageBtns = [];
        const maxV = 5;
        let sp = Math.max(1, s.currentPage - Math.floor(maxV / 2));
        let ep = Math.min(s.totalPages, sp + maxV - 1);
        if (ep - sp + 1 < maxV) sp = Math.max(1, ep - maxV + 1);
        if (sp > 1) {
            pageBtns.push(h('button', { class: 'pagination-btn', onClick: () => { s.currentPage = 1; s.fetchResources(); } }, '1'));
            if (sp > 2) pageBtns.push(h('span', null, '...'));
        }
        for (let i = sp; i <= ep; i++) {
            pageBtns.push(h('button', { class: 'pagination-btn' + (i === s.currentPage ? ' active' : ''), onClick: () => { s.currentPage = i; s.fetchResources(); } }, i));
        }
        if (ep < s.totalPages) {
            if (ep < s.totalPages - 1) pageBtns.push(h('span', null, '...'));
            pageBtns.push(h('button', { class: 'pagination-btn', onClick: () => { s.currentPage = s.totalPages; s.fetchResources(); } }, s.totalPages));
        }

        const headerCells = [
            ['name', '资源名称'], ['kind', '资源类型'], ['namespace', '命名空间'],
            ['apiVersion', 'API版本'], ['group', 'API组']
        ].map(([f, l]) => h('th', {
            style: 'background:#fafafa;padding:12px;text-align:left;font-weight:600;color:#333;cursor:pointer;user-select:none;',
            onClick: () => s.sortBy(f)
        }, [l, h('span', { style: 'margin-left:4px;color:#999;' }, s.sortIcon(f))]));
        headerCells.push(h('th', null, '标签'));
        headerCells.push(h('th', null, 'UID'));
        headerCells.push(h('th', { style: 'width:80px;' }, '操作'));

        const rows = s.pageData.map(r => {
            const cells = [
                h('td', null, r.name),
                h('td', null, h('span', { class: 'resource-kind-badge', style: { backgroundColor: s.getKindColor(r.kind) } }, r.kind)),
                h('td', null, r.namespace),
                h('td', null, r.apiVersion),
                h('td', null, r.group),
                h('td', { class: 'expandable' }, h('div', { class: 'expandable-content' }, s.renderLabels(r.labels))),
                h('td', { style: 'font-size:12px;word-break:break-all;' }, r.uid)
            ];
            if (r.kind === 'Pod') {
                cells.push(h('td', { style: 'white-space:nowrap;' }, h('a', {
                    href: `/view/terminal/index.html?namespace=${encodeURIComponent(r.namespace)}&pod=${encodeURIComponent(r.name)}`,
                    style: 'display:inline-block;padding:4px 8px;background:#1890ff;color:white;border-radius:4px;font-size:12px;text-decoration:none;white-space:nowrap;',
                    title: '打开终端'
                }, '💻 Terminal')));
            } else {
                cells.push(h('td', null, ''));
            }
            return h('tr', { key: r.uid }, cells);
        });
        if (!rows.length && !s.loading) {
            rows.push(h('tr', [h('td', { colspan: 8, style: 'text-align:center;padding:60px;color:#999;' }, '没有找到匹配的资源')]));
        }

        const nsOpts = [h('option', { value: 'all' }, '所有命名空间')].concat(s.namespaces.map(ns => h('option', { value: ns }, ns)));
        const kindOpts = [h('option', { value: 'all' }, '所有类型')].concat(s.kinds.map(k => h('option', { value: k }, k)));

        const content = s.loading
            ? h('div', { class: 'card' }, '加载中...')
            : s.error
                ? h('div', { class: 'card', style: 'color:var(--error-color)' }, '错误: ' + s.error)
                : h('div', [
                    h('div', { style: 'overflow-x:auto;' }, [
                        h('table', { style: 'width:100%;border-collapse:collapse;font-size:14px;min-width:900px;' }, [
                            h('thead', null, [h('tr', { style: 'border-bottom:2px solid #e8e8e8;' }, headerCells)]),
                            h('tbody', null, rows)
                        ])
                    ]),
                    h('div', { style: 'display:flex;justify-content:flex-end;align-items:center;margin-top:20px;gap:8px;' }, [
                        h('span', { style: 'color:#666;font-size:14px;' }, '共 ' + s.totalCount + ' 条记录'),
                        h('button', { style: 'padding:6px 12px;border:1px solid #d9d9d9;background:white;border-radius:4px;cursor:pointer;font-size:14px;', disabled: s.currentPage === 1, onClick: () => { if (s.currentPage > 1) { s.currentPage--; s.fetchResources(); } } }, '上一页'),
                    ].concat(pageBtns).concat([
                        h('button', { style: 'padding:6px 12px;border:1px solid #d9d9d9;background:white;border-radius:4px;cursor:pointer;font-size:14px;', disabled: s.currentPage >= s.totalPages, onClick: () => { if (s.currentPage < s.totalPages) { s.currentPage++; s.fetchResources(); } } }, '下一页')
                    ]))
                ]);

        return h('div', null, [
            h(NavBar),
            h('div', { class: 'container' }, [
            h('div', { class: 'header', style: 'padding:20px;border-bottom:1px solid #e8e8e8;background:#fafafa;' }, [
                h('h1', { style: 'font-size:24px;color:#333;margin-bottom:16px;' }, 'Kubernetes资源表格'),
                h('div', { style: 'display:flex;flex-wrap:wrap;gap:12px;align-items:center;margin-bottom:16px;' }, [
                    h('div', { style: 'display:flex;flex-direction:column;gap:4px;' }, [
                        h('label', { style: 'font-size:14px;color:#666;font-weight:500;' }, '命名空间'),
                        h('select', { value: s.namespaceFilter, onChange: (e) => { s.namespaceFilter = e.target.value; s.debouncedApplyFilter(); }, style: 'padding:8px 12px;border:1px solid #d9d9d9;border-radius:4px;font-size:14px;min-width:150px;' }, nsOpts)
                    ]),
                    h('div', { style: 'display:flex;flex-direction:column;gap:4px;' }, [
                        h('label', { style: 'font-size:14px;color:#666;font-weight:500;' }, '资源类型'),
                        h('select', { value: s.kindFilter, onChange: (e) => { s.kindFilter = e.target.value; s.debouncedApplyFilter(); }, style: 'padding:8px 12px;border:1px solid #d9d9d9;border-radius:4px;font-size:14px;min-width:150px;' }, kindOpts)
                    ]),
                    h('div', { style: 'display:flex;flex-direction:column;gap:4px;' }, [
                        h('label', { style: 'font-size:14px;color:#666;font-weight:500;' }, '搜索'),
                        h('input', { type: 'text', value: s.searchTerm, onInput: (e) => { s.searchTerm = e.target.value; }, onKeydown: (e) => { if (e.key === 'Enter') s.applyFilter(); }, placeholder: '输入资源名称...', style: 'padding:8px 12px;border:1px solid #d9d9d9;border-radius:4px;font-size:14px;min-width:150px;' })
                    ]),
                    h('div', { style: 'display:flex;gap:8px;' }, [
                        h('button', { style: 'padding:8px 16px;border:none;border-radius:4px;font-size:14px;cursor:pointer;background:#1890ff;color:white;', onClick: s.applyFilter }, '应用过滤'),
                        h('button', { style: 'padding:8px 16px;border:none;border-radius:4px;font-size:14px;cursor:pointer;background:#52c41a;color:white;', onClick: s.fetchResources }, '刷新数据')
                    ])
                ])
            ]),
            h('div', { style: 'padding:20px;' }, [content])
            ]),
        ]);
    }
}).mount('#app');
