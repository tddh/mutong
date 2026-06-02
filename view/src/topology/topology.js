import { createApp, ref, shallowRef, computed, watch, onMounted, onUnmounted, nextTick } from 'vue';
import ForceGraph from 'force-graph';
import { forceManyBody, forceLink, forceCenter, forceCollide } from 'd3-force';
import { NavBar } from '../components/SharedComponents.js';
import { API } from '../utils/api.js';
import '../styles/common.css';
import './topology.css';

const COLORS = {
    Pod: '#1890ff', Node: '#52c41a', Service: '#fa8c16',
    Deployment: '#722ed1', StatefulSet: '#8e44ad', Result: '#eb2f96',
    ReplicaSet: '#9254de', ConfigMap: '#52c41a', Secret: '#eb2f96',
    PersistentVolumeClaim: '#13c2c2', PersistentVolume: '#08979c', Ingress: '#fa541c',
    Endpoints: '#faad14', EndpointSlice: '#ffc53d', Namespace: '#f5222d',
    StorageClass: '#2f54eb', ServiceAccount: '#a0d911', Job: '#fadb14',
    CronJob: '#d3f261', DaemonSet: '#6c5ce7',
    ClusterRole: '#30336b', ClusterRoleBinding: '#5352ed',
    Role: '#2e86de', RoleBinding: '#1dd1a1', Lease: '#c44569',
    Event: '#f39c12', ControllerRevision: '#576574', CSINode: '#3742fa',
    CSIDriver: '#487eb0', Certificate: '#ff6348', CertificateRequest: '#ff7979',
    ClusterIssuer: '#8c7ae6', Issuer: '#a29bfe', Order: '#dfe6e9',
    BusinessApp: '#e74c3c'
};

const BUSINESS_COLORS = {
    BusinessApp: '#e74c3c',
    critical: '#c0392b',
    high: '#e74c3c',
    medium: '#f39c12',
    low: '#27ae60'
};

function getKindColor(kind, mode) {
    if (mode === 'business') {
        return BUSINESS_COLORS[kind] || BUSINESS_COLORS.BusinessApp;
    }
    return COLORS[kind] || '#666';
}

let particlePhase = 0;
let rafId = null;
function animLoop() {
    particlePhase = (Date.now() % 4000) / 4000;
    rafId = requestAnimationFrame(animLoop);
}
function startAnimLoop() {
    if (rafId === null) animLoop();
}
function stopAnimLoop() {
    if (rafId !== null) { cancelAnimationFrame(rafId); rafId = null; }
}

createApp({
    components: { NavBar },
    setup() {
        const loading = ref(true);
        const allNodes = shallowRef([]);
        const allEdges = shallowRef([]);
        const graphRef = ref(null);
        const selectedNs = ref('default');
        const selectedResourceUid = ref('');
        const selectedNode = ref(null);
        const detailLoading = ref(false);
        const depth = ref(2);
        const kindFilter = ref('');
        const suggestions = ref([]);
        const suggestVisible = ref(false);
        const suggestActiveIdx = ref(-1);
        let suggestTimer = null;
        const graphDataCount = ref(0);
        const graphEdgeCount = ref(0);
        const currentPage = ref(1);
        const totalPages = ref(1);
        const metrics = ref(null);
        const metricsLoading = ref(false);
        const error = ref(null);
        const animMode = ref('float');
        const showParticles = ref(false);
        const showCurves = ref(true);
        const selectedKinds = ref([]);
        const showAllKinds = ref(false);
        const viewMode = ref(localStorage.getItem('topology-viewMode') || 'physical');
        const businessUnitFilter = ref('');
        const businessUnits = computed(() => [...new Set(allNodes.value.filter(n => n.businessUnit).map(n => n.businessUnit))].sort());

        let fgInstance = null;
        let hoveredLink = null;
        let hoveredNode = null;
        let driftMode = 'float';
        let driftTick = 0;
        let nodeIdMap = {};
        let nodeLinkMap = {};
        let isPointerOverGraph = false;

        const animModes = [
            { key: 'float', label: '漂动', icon: '🌊' },
            { key: 'wave', label: '波纹', icon: '💫' },
            { key: 'static', label: '静止', icon: '⏸' }
        ];

        const layoutModes = [
            { key: 'force', label: '力导向', icon: '⚡' }
        ];

        const namespaceList = ref([]);

        const namespaces = computed(() => {
            if (namespaceList.value.length > 0) return [...namespaceList.value].sort();
            return [...new Set(allNodes.value.map(n => n.namespace).filter(Boolean))].sort();
        });
        const kinds = computed(() => [...new Set(allNodes.value.map(n => n.kind))].filter(k => k !== 'Label'));

        const visibleKinds = computed(() => {
            const all = kinds.value;
            if (showAllKinds.value || all.length <= 10) return all;
            return all.slice(0, 10);
        });
        const hasHiddenKinds = computed(() => kinds.value.length > 10);

        const kindPriority = {
            Pod: 1,
            Deployment: 2,
            StatefulSet: 3,
            DaemonSet: 4,
            ReplicaSet: 5,
            Service: 6,
            Node: 7,
            PersistentVolumeClaim: 8,
            ConfigMap: 9,
            Secret: 10,
            CiliumEndpoint: 30,
            CiliumIdentity: 31,
        };

        const nodesInNs = computed(() => {
            const ns = selectedNs.value;
            const q = kindFilter.value.toLowerCase();
            const result = [];
            for (let i = 0; i < allNodes.value.length; i++) {
                const n = allNodes.value[i];
                if (n.kind === 'Label') continue;
                if (ns !== 'all' && n.namespace !== ns) continue;
                if (q && !n.label.toLowerCase().includes(q) && !n.id.toLowerCase().includes(q)) continue;
                result.push(n);
            }
            result.sort((a, b) => {
                if (a.label !== b.label) return a.label.localeCompare(b.label);
                const pa = kindPriority[a.kind] || 100;
                const pb = kindPriority[b.kind] || 100;
                if (pa !== pb) return pa - pb;
                return (a.namespace || '').localeCompare(b.namespace || '');
            });
            return result;
        });

        const relatedResources = computed(() => {
            if (!selectedNode.value) return { upstream: [], downstream: [] };
            const uid = selectedNode.value.id;
            const upstream = [];
            const downstream = [];
            const nodeMap = {};
            for (const n of allNodes.value) nodeMap[n.id] = n;
            for (const e of allEdges.value) {
                if (e.target === uid && nodeMap[e.source]) {
                    upstream.push({ kind: nodeMap[e.source].kind, label: nodeMap[e.source].label });
                }
                if (e.source === uid && nodeMap[e.target]) {
                    downstream.push({ kind: nodeMap[e.target].kind, label: nodeMap[e.target].label });
                }
            }
            return { upstream, downstream };
        });

        function buildBusinessSelectedNode(node) {
            return {
                id: node.id,
                label: node.appName || node.label,
                kind: 'BusinessApp',
                namespace: node.namespace || '-',
                appName: node.appName,
                team: node.team,
                businessUnit: node.businessUnit,
                criticality: node.criticality,
                environment: node.environment,
            };
        }

        function buildBusinessSubGraph(seedId) {
            if (!seedId) return { nodes: [], edges: [] };
            const nodeMap = new Map(allNodes.value.map(n => [n.id, n]));
            if (!nodeMap.has(seedId)) return { nodes: [], edges: [] };

            const relevantEdges = allEdges.value.filter(e => e.source === seedId || e.target === seedId);
            const nodeIds = new Set([seedId]);
            for (const e of relevantEdges) {
                nodeIds.add(e.source);
                nodeIds.add(e.target);
            }

            const nodes = Array.from(nodeIds)
                .map(id => nodeMap.get(id))
                .filter(Boolean)
                .map(n => ({
                    ...n,
                    _isSeed: n.id === seedId,
                }));

            const edges = relevantEdges.map(e => ({
                source: e.source,
                target: e.target,
                relation: e.relation || 'calls',
            }));

            return { nodes, edges };
        }

        // 从 K8s labels 中提取业务上下文
        function extractBusinessContext(labels) {
            if (!labels || typeof labels !== 'object') return {};
            const ctx = {};
            // 应用名称
            ctx.appName = labels['app.kubernetes.io/name'] || labels['app'] || labels['application'] || '';
            // 业务单元
            ctx.businessUnit = labels['business-unit'] || labels['mutong/business-unit'] || labels['app.kubernetes.io/part-of'] || labels['biz-domain'] || '';
            // 团队
            ctx.team = labels['team'] || labels['owner'] || labels['mutong/team'] || labels['managed-by'] || '';
            // 关键等级
            ctx.criticality = labels['criticality'] || labels['mutong/criticality'] || labels['priority'] || labels['severity-level'] || '';
            // 环境
            ctx.environment = labels['environment'] || labels['env'] || labels['mutong/environment'] || labels['stage'] || '';
            // 来源
            ctx.source = labels['source'] || labels['mutong/source'] || labels['provisioner'] || labels['helm.sh/chart'] || '';
            // 清理空值
            for (const k of Object.keys(ctx)) { if (!ctx[k]) delete ctx[k]; }
            return ctx;
        }

        function parseResourceDefine(resource) {
            const result = {};
            if (!resource.resource_define) return result;
            try {
                let define = resource.resource_define;
                if (typeof define === 'string') {
                    define = JSON.parse(define);
                    if (typeof define === 'string') define = JSON.parse(define);
                }
                const metadata = define.metadata || {};
                const status = define.status || {};
                if (define.apiVersion) result.apiVersion = define.apiVersion;
                if (resource.group) result.group = resource.group;
                if (metadata.creationTimestamp) {
                    result.created = new Date(metadata.creationTimestamp).toLocaleString('zh-CN');
                }
                if (metadata.annotations && Object.keys(metadata.annotations).length > 0) {
                    result.annotationsCount = Object.keys(metadata.annotations).length;
                }
                if (Array.isArray(metadata.ownerReferences) && metadata.ownerReferences.length > 0) {
                    result.owners = metadata.ownerReferences.map(o => `${o.kind || 'Unknown'}:${o.name || '-'}`).join(', ');
                }

                // 从 labels 提取业务上下文
                const bizCtx = extractBusinessContext(metadata.labels);
                Object.assign(result, bizCtx);
                if (resource.kind === 'Pod') {
                    if (status.phase) result.status = status.phase;
                    if (status.podIP) result.podIP = status.podIP;
                    if (Array.isArray(status.containerStatuses) && status.containerStatuses.length > 0) {
                        result.restartCount = status.containerStatuses.reduce((sum, c) => sum + (c.restartCount || 0), 0);
                        result.images = status.containerStatuses.map(c => c.image).join(', ');
                        result.containers = status.containerStatuses.map(c => c.name).join(', ');
                    }
                    if (define.spec && define.spec.nodeName) result.nodeName = define.spec.nodeName;
                }
                if (['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet'].includes(resource.kind)) {
                    const spec = define.spec || {};
                    if (spec.replicas !== undefined) result.replicas = spec.replicas;
                    if (status.readyReplicas !== undefined) result.readyReplicas = status.readyReplicas;
                    if (status.availableReplicas !== undefined) result.availableReplicas = status.availableReplicas;
                    if (spec.replicas !== undefined && status.readyReplicas !== undefined) {
                        result.status = `${status.readyReplicas}/${spec.replicas} 就绪`;
                    }
                }
                if (resource.kind === 'Node') {
                    if (Array.isArray(status.conditions)) {
                        const ready = status.conditions.find(c => c.type === 'Ready');
                        if (ready) result.status = ready.status === 'True' ? 'Ready' : 'NotReady';
                    }
                    if (Array.isArray(status.addresses)) {
                        const internalIP = status.addresses.find(a => a.type === 'InternalIP');
                        if (internalIP) result.nodeIP = internalIP.address;
                    }
                    if (status.capacity) {
                        if (status.capacity.cpu) result.cpuCapacity = status.capacity.cpu;
                        if (status.capacity.memory) result.memoryCapacity = status.capacity.memory;
                    }
                }
                if (resource.kind === 'Service') {
                    const spec = define.spec || {};
                    if (spec.type) result.serviceType = spec.type;
                    if (spec.clusterIP) result.clusterIP = spec.clusterIP;
                    if (Array.isArray(spec.ports) && spec.ports.length > 0) {
                        result.ports = spec.ports.map(p => `${p.port}/${p.protocol || 'TCP'}`).join(', ');
                    }
                    if (spec.selector) {
                        result.selector = Object.entries(spec.selector).map(([k, v]) => `${k}=${v}`).join(', ');
                    }
                }
                if (['ConfigMap', 'Secret'].includes(resource.kind)) {
                    const data = define.data || {};
                    result.dataKeys = Object.keys(data).length;
                }
                if (resource.kind === 'PersistentVolumeClaim') {
                    if (status.phase) result.status = status.phase;
                    const spec = define.spec || {};
                    if (spec.resources && spec.resources.requests && spec.resources.requests.storage) {
                        result.storageRequest = spec.resources.requests.storage;
                    }
                }
                if (resource.kind === 'CiliumEndpoint') {
                    if (status.state) result.status = status.state;
                    if (status.networking && Array.isArray(status.networking.addressing) && status.networking.addressing.length > 0) {
                        const address = status.networking.addressing[0];
                        if (address.ipv4) result.endpointIPv4 = address.ipv4;
                        if (address.ipv6) result.endpointIPv6 = address.ipv6;
                    }
                    if (status.networking && status.networking.node) {
                        result.endpointNode = status.networking.node;
                    }
                    if (status.identity && status.identity.id !== undefined) {
                        result.identityId = status.identity.id;
                    }
                    if (Array.isArray(status['named-ports']) && status['named-ports'].length > 0) {
                        result.namedPorts = status['named-ports']
                            .map(p => `${p.name || 'port'}:${p.port}/${p.protocol || 'TCP'}`)
                            .join(', ');
                    }
                    if (status['external-identifiers']) {
                        const ext = status['external-identifiers'];
                        if (ext['pod-name']) result.externalPodName = ext['pod-name'];
                        else if (ext['k8s-pod-name']) result.externalPodName = ext['k8s-pod-name'];
                    }
                }
                if (metadata.labels && Object.keys(metadata.labels).length > 0) {
                    const labels = Object.entries(metadata.labels);
                    result.labels = labels.slice(0, 5).map(([k, v]) => `${k}=${v}`).join(', ');
                    if (labels.length > 5) result.labels += ` (+${labels.length - 5})`;
                }
                if (!result.status) {
                    if (typeof status.phase === 'string' && status.phase) result.status = status.phase;
                    else if (typeof status.state === 'string' && status.state) result.status = status.state;
                    else if (typeof status.currentStatus === 'string' && status.currentStatus) result.status = status.currentStatus;
                }
                if (Array.isArray(status.conditions) && status.conditions.length > 0) {
                    result.conditionsSummary = status.conditions.slice(0, 3)
                        .map(c => `${c.type || 'Condition'}:${c.status || 'Unknown'}`)
                        .join(', ');
                    if (!result.status) {
                        const healthy = status.conditions.find(c => ['Ready', 'Available', 'Succeeded', 'Completed'].includes(c.type) && c.status === 'True');
                        const unhealthy = status.conditions.find(c => c.status === 'False');
                        if (healthy) result.status = healthy.type;
                        else if (unhealthy) result.status = unhealthy.type;
                    }
                }
            } catch (e) {
                console.warn('解析 resource_define 失败:', e);
            }
            if (result.status) {
                const s = result.status.toLowerCase();
                if (['running', 'ready', 'bound', 'available'].includes(s)) result.statusColor = '#52c41a';
                else if (s === 'pending') result.statusColor = '#faad14';
                else if (s.includes('notready') || ['failed', 'error', 'lost'].includes(s)) result.statusColor = '#ff4d4f';
                else if (s.includes('就绪')) result.statusColor = '#52c41a';
                else result.statusColor = '#8c8c8c';
            }
            return result;
        }

        const NODE_REL_SIZE = 7;

        function nodeRadius(node) {
            const isSeed = node._isSeed;
            return isSeed ? 18 : 14;
        }

        function nodeHitRadius(node) {
            return nodeRadius(node) + 5;
        }

        function drawK8sNode(node, ctx) {
            const kind = node.kind || 'Unknown';
            const color = getKindColor(kind, viewMode.value);
            const isSeed = node._isSeed;
            const highlighted = node.__highlighted || false;
            const r = nodeRadius(node);

            ctx.save();

            if (highlighted || isSeed) {
                ctx.shadowColor = color;
                ctx.shadowBlur = highlighted ? 20 : 12;
            }

            const bgColor = isSeed ? '#fffbe6' : color;
            const borderColor = highlighted ? '#1890ff' : (isSeed ? '#faad14' : color);
            const borderWidth = highlighted ? 2.5 : (isSeed ? 2.5 : 1.5);

            ctx.beginPath();
            ctx.arc(node.x, node.y, r, 0, Math.PI * 2);
            ctx.fillStyle = bgColor;
            ctx.fill();
            ctx.strokeStyle = borderColor;
            ctx.lineWidth = borderWidth;
            ctx.stroke();

            ctx.shadowBlur = 0;
            ctx.shadowColor = 'transparent';

            const label = node.label || '';
            const maxLen = 16;
            const displayLabel = label.length > maxLen ? label.substring(0, maxLen - 1) + '\u2026' : label;
            ctx.font = `${highlighted ? '600' : '400'} 11px -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif`;
            ctx.textAlign = 'center';
            ctx.textBaseline = 'top';
            ctx.fillStyle = highlighted ? '#1890ff' : (isSeed ? '#333' : '#555');
            ctx.fillText(displayLabel, node.x, node.y + r + 4);

            ctx.font = '500 9px -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif';
            ctx.fillStyle = highlighted ? '#1890ff' : '#888';
            ctx.globalAlpha = 0.75;
            ctx.fillText(kind, node.x, node.y + r + 18);
            ctx.globalAlpha = 1;

            if (isSeed) {
                ctx.font = '12px sans-serif';
                ctx.fillStyle = '#faad14';
                ctx.fillText('\u2605', node.x, node.y - r - 6);
            }

            ctx.restore();
        }

        function drawK8sLink(link, ctx) {
            const src = link.source;
            const tgt = link.target;
            if (src.x == null || tgt.x == null) return;

            ctx.save();
            ctx.strokeStyle = link.__highlighted ? 'rgba(24,144,255,0.6)' : 'rgba(160,174,192,0.35)';
            ctx.lineWidth = link.__highlighted ? 2 : 1;

            if (showCurves.value) {
                const dx = tgt.x - src.x;
                const dy = tgt.y - src.y;
                const cx = (src.x + tgt.x) / 2 - dy * 0.15;
                const cy = (src.y + tgt.y) / 2 + dx * 0.15;
                ctx.beginPath();
                ctx.moveTo(src.x, src.y);
                ctx.quadraticCurveTo(cx, cy, tgt.x, tgt.y);
                ctx.stroke();

                if (showParticles.value) {
                    for (let i = 0; i < 2; i++) {
                        const t = ((particlePhase + i / 2) % 1);
                        const px = (1 - t) * (1 - t) * src.x + 2 * (1 - t) * t * cx + t * t * tgt.x;
                        const py = (1 - t) * (1 - t) * src.y + 2 * (1 - t) * t * cy + t * t * tgt.y;
                        ctx.beginPath();
                        ctx.arc(px, py, 1.5, 0, Math.PI * 2);
                        ctx.fillStyle = getKindColor(src.kind, viewMode.value) || '#1890ff';
                        ctx.globalAlpha = 0.5 * (1 - Math.abs(t - 0.5) * 2);
                        ctx.fill();
                        ctx.globalAlpha = 1;
                    }
                }
            } else {
                ctx.beginPath();
                ctx.moveTo(src.x, src.y);
                ctx.lineTo(tgt.x, tgt.y);
                ctx.stroke();

                if (showParticles.value) {
                    for (let i = 0; i < 2; i++) {
                        const t = ((particlePhase + i / 2) % 1);
                        ctx.beginPath();
                        ctx.arc(src.x + (tgt.x - src.x) * t, src.y + (tgt.y - src.y) * t, 1.5, 0, Math.PI * 2);
                        ctx.fillStyle = getKindColor(src.kind, viewMode.value) || '#1890ff';
                        ctx.globalAlpha = 0.5;
                        ctx.fill();
                        ctx.globalAlpha = 1;
                    }
                }
            }
            ctx.restore();
        }

        function buildForceGraph(nodes, edges) {
            const container = document.getElementById('graph-container');
            if (!container) return;
            if (fgInstance) { fgInstance._destructor && fgInstance._destructor(); }

            const fgNodes = nodes.map(n => ({
                ...n,
                x: (Math.random() - 0.5) * 1000,
                y: (Math.random() - 0.5) * 700,
                val: n.kind === 'Node' ? 6.6 : n.kind === 'Deployment' || n.kind === 'StatefulSet' ? 4.5 : n.kind === 'Service' ? 4 : n.kind === 'Pod' ? 4 : 4
            }));

            const hitRadiusFn = (node) => nodeRadius(node) + 5;

            fgNodes.sort((a, b) => {
                if (a._isSeed && !b._isSeed) return 1;
                if (!a._isSeed && b._isSeed) return -1;
                return 0;
            });

            // 去重：A→B 和 B→A 只保留一条
            const seen = new Set();
            const fgLinks = [];
            for (const e of edges) {
                const key = [e.source, e.target].sort().join('|||');
                if (!seen.has(key)) {
                    seen.add(key);
                    fgLinks.push({ source: e.source, target: e.target, relation: e.relation || '' });
                }
            }

            fgInstance = ForceGraph()(container)
                .graphData({ nodes: fgNodes, links: fgLinks })
                .backgroundColor('#f8f9fa')
                .nodeRelSize(NODE_REL_SIZE)
                .nodeVal(n => n.val || 2)
                .nodeCanvasObject((node, ctx) => drawK8sNode(node, ctx))
                .nodePointerAreaPaint((node, color, ctx) => {
                    ctx.fillStyle = color;
                    ctx.beginPath();
                    ctx.arc(node.x, node.y, hitRadiusFn(node), 0, Math.PI * 2);
                    ctx.fill();
                })
                .linkCanvasObjectMode(() => 'replace')
                .linkCanvasObject((link, ctx) => drawK8sLink(link, ctx))
                .linkWidth(1.5)
                .enablePointerInteraction(true)
                .autoPauseRedraw(false)
                .onNodeHover(node => {
                    const prev = hoveredNode;
                    if (prev && !prev.__highlighted_click) { prev.__highlighted = false; prev.__highlighted_hover = false; }
                    for (let i = 0; i < fgLinks.length; i++) {
                        if (fgLinks[i].__highlighted_hover && !fgLinks[i].__highlighted_click) { fgLinks[i].__highlighted = false; fgLinks[i].__highlighted_hover = false; }
                    }
                    for (let i = 0; i < fgNodes.length; i++) {
                        if (fgNodes[i].__highlighted_hover && !fgNodes[i].__highlighted_click) { fgNodes[i].__highlighted = false; fgNodes[i].__highlighted_hover = false; }
                    }
                    hoveredNode = node;
                    if (node) {
                        node.__highlighted = true;
                        node.__highlighted_hover = true;
                        const nbrs = nodeLinkMap[node.id];
                        if (nbrs) {
                            for (let i = 0; i < nbrs.l.length; i++) { nbrs.l[i].__highlighted = true; nbrs.l[i].__highlighted_hover = true; }
                            for (let i = 0; i < nbrs.n.length; i++) { nbrs.n[i].__highlighted = true; nbrs.n[i].__highlighted_hover = true; }
                        }
                    }
                })
                .onLinkHover(link => {
                    if (hoveredLink) {
                        hoveredLink.__highlighted = false;
                        hoveredLink.__highlighted_hover = false;
                        const srcN = nodeIdMap[typeof hoveredLink.source === 'object' ? hoveredLink.source.id : hoveredLink.source];
                        const tgtN = nodeIdMap[typeof hoveredLink.target === 'object' ? hoveredLink.target.id : hoveredLink.target];
                        if (srcN && !srcN.__highlighted_click) { srcN.__highlighted = false; srcN.__highlighted_hover = false; }
                        if (tgtN && !tgtN.__highlighted_click) { tgtN.__highlighted = false; tgtN.__highlighted_hover = false; }
                    }
                    hoveredLink = link;
                    if (link) {
                        link.__highlighted = true;
                        link.__highlighted_hover = true;
                        const srcN = nodeIdMap[typeof link.source === 'object' ? link.source.id : link.source];
                        const tgtN = nodeIdMap[typeof link.target === 'object' ? link.target.id : link.target];
                        if (srcN) { srcN.__highlighted = true; srcN.__highlighted_hover = true; }
                        if (tgtN) { tgtN.__highlighted = true; tgtN.__highlighted_hover = true; }
                    }
                })
                .onNodeClick(node => {
                    for (let i = 0; i < fgNodes.length; i++) { const n = fgNodes[i]; n.__highlighted = false; n.__highlighted_click = false; n.__highlighted_hover = false; }
                    for (let i = 0; i < fgLinks.length; i++) { fgLinks[i].__highlighted = false; fgLinks[i].__highlighted_hover = false; }
                    node.__highlighted = true;
                    node.__highlighted_click = true;
                    const nbrs = nodeLinkMap[node.id];
                    if (nbrs) {
                        for (let i = 0; i < nbrs.l.length; i++) { nbrs.l[i].__highlighted = true; }
                        for (let i = 0; i < nbrs.n.length; i++) { nbrs.n[i].__highlighted = true; nbrs.n[i].__highlighted_click = true; }
                    }
                    const clickedNodeId = node.id;
                    setTimeout(() => {
                        if (viewMode.value === 'business') {
                            selectedNode.value = {
                                id: node.id,
                                label: node.appName || node.label,
                                kind: 'BusinessApp',
                                namespace: node.namespace || '-',
                                appName: node.appName,
                                team: node.team,
                                businessUnit: node.businessUnit,
                                criticality: node.criticality,
                                environment: node.environment,
                            };
                            detailLoading.value = false;
                        } else {
                            selectedNode.value = {
                                id: node.id,
                                label: node.label,
                                kind: node.kind,
                                namespace: node.namespace,
                                apiVersion: node.apiVersion,
                                group: node.group,
                                appName: node.appName || '',
                                team: node.team || '',
                                businessUnit: node.businessUnit || '',
                                criticality: node.criticality || '',
                                environment: node.environment || '',
                                source: node.source || '',
                            };
                            detailLoading.value = true;
                            fetch(`/k8s/resources/graph/resource-define?resourceId=${encodeURIComponent(node.id)}`)
                                .then(res => res.ok ? res.json() : null)
                                .then(data => {
                                    if (selectedNode.value && selectedNode.value.id === clickedNodeId && data && data.resource_define) {
                                        const parsed = parseResourceDefine({ ...node, resource_define: data.resource_define });
                                        selectedNode.value = {
                                            id: node.id,
                                            label: node.label,
                                            kind: node.kind,
                                            namespace: node.namespace,
                                            apiVersion: node.apiVersion,
                                            group: node.group,
                                            appName: node.appName || parsed.appName || '',
                                            team: node.team || parsed.team || '',
                                            businessUnit: node.businessUnit || parsed.businessUnit || '',
                                            criticality: node.criticality || parsed.criticality || '',
                                            environment: node.environment || parsed.environment || '',
                                            source: node.source || parsed.source || '',
                                            ...parsed,
                                        };
                                    }
                                })
                                .catch(() => {})
                                .finally(() => {
                                    if (selectedNode.value && selectedNode.value.id === clickedNodeId) {
                                        detailLoading.value = false;
                                    }
                                });
                        }
                    }, 0);
                })
                .onBackgroundClick(() => {
                    for (let i = 0; i < fgNodes.length; i++) { const n = fgNodes[i]; n.__highlighted = false; n.__highlighted_click = false; n.__highlighted_hover = false; }
                    for (let i = 0; i < fgLinks.length; i++) { fgLinks[i].__highlighted = false; fgLinks[i].__highlighted_hover = false; }
                    selectedNode.value = null;
                })
                .onNodeDragEnd(node => {
                    node.fx = node.x;
                    node.fy = node.y;
                })
                .d3AlphaDecay(0.005)
                .d3VelocityDecay(0.4)
                .d3Force('charge', forceManyBody()
                    .strength(d => {
                        if (d.kind === 'Node') return -600;
                        if (d.kind === 'Deployment' || d.kind === 'StatefulSet') return -500;
                        if (d.kind === 'Service') return -400;
                        if (d.kind === 'Ingress') return -350;
                        if (d.kind === 'ReplicaSet') return -300;
                        return -250;
                    })
                    .distanceMax(500)
                )
                .d3Force('link', forceLink().id(d => d.id).distance(d => {
                    const rel = d.relation || '';
                    if (rel === 'runs') return 250;
                    if (rel === 'manages') return 160;
                    if (rel === 'exposes' || rel === 'serves') return 180;
                    if (rel === 'uses') return 200;
                    if (rel === 'routes') return 180;
                    return 170;
                }).strength(0.4))
                .d3Force('center', forceCenter())
                .d3Force('collision', forceCollide().radius(d => (d._isSeed ? 18 : 14) + 35).strength(0.85).iterations(2))
                .cooldownTicks(Infinity)
                .cooldownTime(Infinity)
                .onEngineTick(() => onEngineDrift());

            fgNodes.forEach(n => { n.__driftSeed = n.__driftSeed || Math.random() * 6283; });
            nodeIdMap = {};
            nodeLinkMap = {};
            for (let i = 0; i < fgNodes.length; i++) {
                nodeIdMap[fgNodes[i].id] = fgNodes[i];
                nodeLinkMap[fgNodes[i].id] = { l: [], n: [] };
            }
            for (let i = 0; i < fgLinks.length; i++) {
                const l = fgLinks[i];
                const sid = typeof l.source === 'object' ? l.source.id : l.source;
                const tid = typeof l.target === 'object' ? l.target.id : l.target;
                const sn = nodeLinkMap[sid];
                const tn = nodeLinkMap[tid];
                if (sn) { sn.l.push(l); if (nodeIdMap[tid]) sn.n.push(nodeIdMap[tid]); }
                if (tn) { tn.l.push(l); if (nodeIdMap[sid]) tn.n.push(nodeIdMap[sid]); }
            }

            try { applyAnimMode(animMode.value); } catch (e) { console.warn('[topology] applyAnimMode failed:', e); }

            container.addEventListener('mouseenter', () => {
                isPointerOverGraph = true;
                fgInstance && fgInstance.flushShadowCanvas && fgInstance.flushShadowCanvas();
            });
            container.addEventListener('mouseleave', () => { isPointerOverGraph = false; });

            graphDataCount.value = fgNodes.length;
            graphEdgeCount.value = fgLinks.length;
            graphRef.value = fgInstance;
        }

function applyAnimMode(mode) {
            if (!fgInstance) return;
            driftMode = mode;
            driftTick = 0;
            fgInstance.graphData().nodes.forEach(n => n.__driftSeed = n.__driftSeed || Math.random() * 6283);
            if (mode === 'float') {
                fgInstance.cooldownTicks(Infinity).d3AlphaDecay(0.005).d3VelocityDecay(0.4);
                fgInstance.graphData().nodes.forEach(n => { n.fx = undefined; n.fy = undefined; });
                fgInstance.d3ReheatSimulation();
            } else if (mode === 'static') {
                fgInstance.cooldownTicks(300).d3AlphaDecay(0.06).d3VelocityDecay(0.5);
            } else if (mode === 'wave') {
                fgInstance.cooldownTicks(Infinity).d3AlphaDecay(0.005).d3VelocityDecay(0.4);
                fgInstance.graphData().nodes.forEach(n => { n.fx = undefined; n.fy = undefined; });
                fgInstance.d3ReheatSimulation();
            }
        }

        function onEngineDrift() {
            if (!fgInstance) return;
            if (driftMode === 'static') return;
            if (isPointerOverGraph) return;
            driftTick++;
            const nodes = fgInstance.graphData().nodes;
            const len = nodes.length;
            const container = document.getElementById('graph-container');
            const W = container ? container.clientWidth : 1200;
            const H = container ? container.clientHeight : 800;
            const softR = Math.min(W, H) * 0.35;
            let t, seed, dx, dy;
            if (driftMode === 'float') {
                t = driftTick * 0.0008;
                for (let i = 0; i < len; i++) {
                    const n = nodes[i];
                    if (n.fx != null) continue;
                    seed = n.__driftSeed || 0;
                    dx = Math.sin(t + seed) * 0.35 + Math.sin(t * 0.7 + seed * 1.7) * 0.25;
                    dy = Math.cos(t * 0.8 + seed * 1.3) * 0.35 + Math.cos(t * 0.5 + seed * 0.9) * 0.25;
                    const dist = Math.sqrt(n.x * n.x + n.y * n.y);
                    if (dist > softR) {
                        const pull = (dist - softR) * 0.008;
                        dx -= (n.x / dist) * pull;
                        dy -= (n.y / dist) * pull;
                    }
                    n.x += dx;
                    n.y += dy;
                }
            } else if (driftMode === 'wave') {
                t = driftTick * 0.0006;
                for (let i = 0; i < len; i++) {
                    const n = nodes[i];
                    if (n.fx != null) continue;
                    seed = n.__driftSeed || 0;
                    const phase = ((n.x || 0) + (n.y || 0)) * 0.003;
                    dx = Math.sin(t + phase + seed * 0.2) * 0.5;
                    dy = Math.cos(t * 0.7 + phase * 0.5 + seed * 0.3) * 0.5;
                    const dist = Math.sqrt(n.x * n.x + n.y * n.y);
                    if (dist > softR) {
                        const pull = (dist - softR) * 0.008;
                        dx -= (n.x / dist) * pull;
                        dy -= (n.y / dist) * pull;
                    }
                    n.x += dx;
                    n.y += dy;
                }
            }
        }

        const fetchTopology = async (uid, layers) => {
            loading.value = true;
            try {
                const res = await fetch(`/k8s/resources/graph/search?resourceId=${encodeURIComponent(uid)}&depth=${layers}`);
                if (!res.ok) throw new Error(`获取拓扑数据失败 (${res.status})`);
                const data = await res.json();
                const topoNodes = (data.nodes || []).filter(n => n.id).map(n => ({
                    ...n, kind: n.kind || 'Unknown', namespace: n.namespace || n.name_space || '-', _isSeed: n.id === uid
                }));
                const topoEdges = (data.edges || []).map(e => ({
                    source: e.source, target: e.target, relation: e.relation || ''
                }));

                const topoNodeIds = new Set(topoNodes.map(n => n.id));
                const existingNodes = allNodes.value.filter(n => !topoNodeIds.has(n.id));
                allNodes.value = [...existingNodes, ...topoNodes];
                allEdges.value = topoEdges;

                buildForceGraph(topoNodes, topoEdges);
                loading.value = false;
            } catch (err) {
                console.error('Fetch topology error:', err);
                error.value = err.message || '加载拓扑数据失败';
                loading.value = false;
            }
        };

        const fetchNamespaceList = async () => {
            try {
                const data = await API.resources.metadata();
                if (data && data.namespaces) {
                    namespaceList.value = data.namespaces;
                }
            } catch (e) {
                console.warn('获取命名空间列表失败:', e);
            }
        };

        const fetchResourceList = async () => {
            loading.value = true;
            try {
                const params = new URLSearchParams({ page: currentPage.value, pageSize: 20 });
                if (selectedNs.value !== 'all') params.set('namespace', selectedNs.value);
                const res = await fetch('/k8s/resources/graph/nodes?' + params.toString());
                if (!res.ok) throw new Error(`获取资源列表失败 (${res.status})`);
                const data = await res.json();
                allNodes.value = (data.nodes || []).map(n => ({
                    id: n.id, label: n.label, kind: n.kind, namespace: n.namespace,
                    apiVersion: n.apiVersion, group: n.group,
                    appName: n.appName || '', team: n.team || '',
                    businessUnit: n.businessUnit || '', criticality: n.criticality || '',
                    environment: n.environment || '', source: n.source || '',
                }));
                allEdges.value = [];
                const tc = data.totalCount || allNodes.value.length;
                totalPages.value = Math.max(1, Math.ceil(tc / 20));
            } catch (err) {
                console.error('Fetch resource list error:', err);
                error.value = err.message || '加载资源列表失败';
                allNodes.value = [];
                totalPages.value = 1;
            } finally {
                loading.value = false;
            }
        };

        const fetchSuggest = async (q) => {
            if (!q || !q.trim()) {
                suggestions.value = [];
                suggestVisible.value = false;
                return;
            }
            try {
                const params = new URLSearchParams({ q: q.trim() });
                if (selectedNs.value !== 'all') params.set('namespace', selectedNs.value);
                const res = await fetch('/k8s/resources/graph/suggest?' + params.toString());
                if (!res.ok) return;
                const data = await res.json();
                suggestions.value = Array.isArray(data) ? data : (data.suggestions || []);
                suggestVisible.value = suggestions.value.length > 0;
                suggestActiveIdx.value = -1;
            } catch (e) {
                console.warn('Suggest error:', e);
                suggestions.value = [];
                suggestVisible.value = false;
            }
        };

        const handleSuggestionSelect = (node) => {
            kindFilter.value = '';
            suggestions.value = [];
            suggestVisible.value = false;
            suggestActiveIdx.value = -1;
            handleResourceSelect(node);
        };

        const handleSuggestKeydown = (e) => {
            if (!suggestVisible.value || suggestions.value.length === 0) return;
            if (e.key === 'ArrowDown') {
                e.preventDefault();
                suggestActiveIdx.value = Math.min(suggestActiveIdx.value + 1, suggestions.value.length - 1);
            } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                suggestActiveIdx.value = Math.max(suggestActiveIdx.value - 1, -1);
            } else if (e.key === 'Enter' && suggestActiveIdx.value >= 0) {
                e.preventDefault();
                handleSuggestionSelect(suggestions.value[suggestActiveIdx.value]);
            } else if (e.key === 'Escape') {
                suggestVisible.value = false;
                suggestActiveIdx.value = -1;
            }
        };

        const fetchBusinessTopology = async () => {
            loading.value = true;
            try {
                const res = await fetch('/api/v1/business-topology/graph');
                if (!res.ok) throw new Error(`获取业务拓扑数据失败 (${res.status})`);
                const data = await res.json();
                
                let nodes = (data.nodes || []).map(n => ({
                    id: n.id || n.appName,
                    label: n.appName,
                    kind: 'BusinessApp',
                    namespace: n.namespace || '-',
                    appName: n.appName,
                    team: n.team || '-',
                    businessUnit: n.businessUnit || '-',
                    criticality: n.criticality || 'medium',
                    environment: n.environment || '-',
                    _isSeed: false
                }));

                if (businessUnitFilter.value && businessUnitFilter.value !== 'all') {
                    nodes = nodes.filter(n => n.businessUnit === businessUnitFilter.value);
                    const allowedIds = new Set(nodes.map(n => n.id));
                    const edges = (data.edges || [])
                        .filter(e => allowedIds.has(e.source) && allowedIds.has(e.target))
                        .map(e => ({
                            source: e.source,
                            target: e.target,
                            relation: 'calls'
                        }));
                    allNodes.value = nodes;
                    allEdges.value = edges;
                    buildForceGraph(nodes, edges);
                } else {
                    const edges = (data.edges || []).map(e => ({
                        source: e.source,
                        target: e.target,
                        relation: 'calls'
                    }));
                    allNodes.value = nodes;
                    allEdges.value = edges;
                    buildForceGraph(nodes, edges);
                }
                
                loading.value = false;
            } catch (err) {
                console.error('Fetch business topology error:', err);
                error.value = err.message || '加载业务拓扑数据失败';
                loading.value = false;
            }
        };

        watch(selectedNs, () => {
            selectedResourceUid.value = '';
            selectedNode.value = null;
            graphDataCount.value = 0;
            graphEdgeCount.value = 0;
            currentPage.value = 1;
            suggestions.value = [];
            suggestVisible.value = false;
            fetchResourceList();
        });

        watch(selectedResourceUid, (newVal) => {
            if (newVal && viewMode.value === 'physical') fetchTopology(newVal, depth.value);
        });

        watch(depth, () => {
            if (selectedResourceUid.value) fetchTopology(selectedResourceUid.value, depth.value);
        });

        watch(selectedNode, (newVal) => { fetchMetrics(newVal); });

        watch(selectedKinds, () => {
            if (selectedResourceUid.value) fetchTopology(selectedResourceUid.value, depth.value);
        }, { deep: true });

        watch(viewMode, (newMode) => {
            if (newMode === 'business') {
                fetchBusinessTopology();
            } else {
                if (selectedResourceUid.value) {
                    fetchTopology(selectedResourceUid.value, depth.value);
                } else {
                    fetchResourceList();
                }
            }
        });

        watch(businessUnitFilter, () => {
            if (viewMode.value === 'business') {
                fetchBusinessTopology();
            } else if (viewMode.value === 'physical') {
                // 物理模式下按业务单元过滤当前图节点
                const unit = businessUnitFilter.value;
                if (!unit) {
                    // 清空筛选，恢复当前资源拓扑
                    if (selectedResourceUid.value) fetchTopology(selectedResourceUid.value, depth.value);
                    else fetchResourceList();
                    return;
                }
                const filteredNodes = allNodes.value.filter(n =>
                    n.businessUnit === unit || n.labels?.includes(unit)
                );
                if (filteredNodes.length === 0) {
                    allEdges.value = [];
                    buildForceGraph([], []);
                    return;
                }
                const filteredIds = new Set(filteredNodes.map(n => n.id));
                const filteredEdges = allEdges.value.filter(e =>
                    filteredIds.has(e.source) && filteredIds.has(e.target)
                );
                buildForceGraph(filteredNodes, filteredEdges);
            }
        });

        watch(kindFilter, (val) => {
            clearTimeout(suggestTimer);
            if (!val || !val.trim()) {
                suggestions.value = [];
                suggestVisible.value = false;
                return;
            }
            suggestTimer = setTimeout(() => fetchSuggest(val), 300);
        });

        const fetchMetrics = async (node) => {
            if (!node) { metrics.value = null; return; }
            metricsLoading.value = true;
            metrics.value = null;
            try {
                const kind = node.kind;
                let data = null;
                if (kind === 'Pod') {
                    data = await API.metrics.pod(node.namespace || 'default', node.label);
                } else if (kind === 'Node') {
                    data = await API.metrics.node(node.label);
                } else if (kind === 'Deployment') {
                    data = await API.metrics.deployment(node.namespace || 'default', node.label);
                }
                if (data && data.error) metrics.value = null;
                else metrics.value = data;
            } catch (e) {
                console.error('[metrics] fetch error:', e);
                metrics.value = null;
            } finally {
                metricsLoading.value = false;
            }
        };

        const getMetricStatusColor = (status) => {
            if (status === 'healthy' || status === 'normal') return 'var(--success-color)';
            if (status === 'warning') return 'var(--warning-color)';
            if (status === 'critical' || status === 'error') return 'var(--error-color)';
            return 'var(--text-secondary)';
        };

        const getMetricDisplayValue = (metric) => {
            if (metric.value === null || metric.value === undefined) return '-';
            const v = Number(metric.value);
            if (isNaN(v)) return metric.value;
            const name = (metric.metricName || '').toLowerCase();

            if (name.includes('percent')) return v.toFixed(1) + '%';
            if (name.includes('cores')) return v.toFixed(3) + ' 核';
            if (name.includes('bytes') && !name.includes('per_sec')) {
                if (v >= 1073741824) return (v / 1073741824).toFixed(2) + ' GB';
                if (v >= 1048576) return (v / 1048576).toFixed(2) + ' MB';
                if (v >= 1024) return (v / 1024).toFixed(2) + ' KB';
                return v.toFixed(0) + ' B';
            }
            if (name.includes('per_sec')) {
                if (v >= 1048576) return (v / 1048576).toFixed(2) + ' MB/s';
                if (v >= 1024) return (v / 1024).toFixed(2) + ' KB/s';
                return v.toFixed(1) + ' B/s';
            }
            if (name.includes('restart')) return v.toFixed(0);
            if (name.includes('replica')) return v.toFixed(0);
            return v.toFixed(2);
        };

        const getMetricLabel = (metric) => {
            const name = metric.metricName || '';
            const labelMap = {
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
                'cpu_usage': 'CPU 使用率', 'cpu_usage_percent': 'CPU 使用率',
                'memory_usage': '内存使用率', 'memory_usage_percent': '内存使用率',
                'disk_usage': '磁盘使用率', 'disk_usage_percent': '磁盘使用率',
                'restarts': '重启次数', 'restart_count': '重启次数',
                'ready_replicas': '就绪副本', 'available_replicas': '可用副本', 'total_replicas': '总副本',
            };
            return labelMap[name] || name;
        };

        const toggleKind = (k) => {
            const idx = selectedKinds.value.indexOf(k);
            if (idx > -1) selectedKinds.value.splice(idx, 1);
            else selectedKinds.value.push(k);
        };

        const handleRefresh = async () => {
            currentPage.value = 1;
            await fetchNamespaceList();
            if (viewMode.value === 'business') {
                const savedNodeId = selectedNode.value?.id;
                await fetchBusinessTopology();
                if (savedNodeId) {
                    const node = allNodes.value.find(n => n.id === savedNodeId);
                    if (node) {
                        selectedNode.value = buildBusinessSelectedNode(node);
                    }
                    const { nodes, edges } = buildBusinessSubGraph(savedNodeId);
                    buildForceGraph(nodes, edges);
                }
            } else if (selectedResourceUid.value) {
                await fetchTopology(selectedResourceUid.value, depth.value);
            } else {
                await fetchResourceList();
            }
        };
        const handlePageChange = (p) => {
            if (p < 1 || p > totalPages.value) return;
            currentPage.value = p;
            fetchResourceList();
        };
        const handleResourceSelect = (node) => {
            if (!node || !node.id) return;
            if (viewMode.value === 'business') {
                selectedResourceUid.value = '';
                selectedNode.value = buildBusinessSelectedNode(node);
                const { nodes, edges } = buildBusinessSubGraph(node.id);
                buildForceGraph(nodes, edges);
                detailLoading.value = false;
                return;
            }
            selectedResourceUid.value = node.id;
            selectedNode.value = null;
            detailLoading.value = false;
        };
        const handleReset = () => {
            kindFilter.value = '';
            selectedNs.value = 'default';
            selectedResourceUid.value = '';
            depth.value = 2;
            selectedKinds.value = [];
            selectedNode.value = null;
            detailLoading.value = false;
            graphDataCount.value = 0;
            graphEdgeCount.value = 0;
            currentPage.value = 1;
            driftMode = 'float';
            driftTick = 0;
            viewMode.value = 'physical';
            businessUnitFilter.value = '';
            localStorage.removeItem('topology-viewMode');
        };

        const handleViewModeChange = (mode) => {
            viewMode.value = mode;
            localStorage.setItem('topology-viewMode', mode);
        };

        const handleBusinessUnitChange = (unit) => {
            businessUnitFilter.value = unit;
        };
        const handleDepthChange = (d) => {
            depth.value = d;
            if (selectedResourceUid.value) fetchTopology(selectedResourceUid.value, depth.value);
        };
        const handleAnimModeChange = (mode) => {
            animMode.value = mode;
            applyAnimMode(mode);
        };
        const handleZoomIn = () => { fgInstance && fgInstance.zoom(1.5, 300); };
        const handleZoomOut = () => { fgInstance && fgInstance.zoom(0.67, 300); };
        const handleFitView = () => { fgInstance && fgInstance.zoomToFit(400, 40); };

        const handleResize = () => {
            const container = document.getElementById('graph-container');
            if (fgInstance && container) {
                fgInstance.width(container.clientWidth).height(container.clientHeight);
            }
        };

        onMounted(async () => {
            createApp({ components: { NavBar } }).mount('#vue-nav');
            startAnimLoop();
            await fetchNamespaceList();
            if (viewMode.value === 'business') {
                await fetchBusinessTopology();
            } else {
                await fetchResourceList();
            }
            window.addEventListener('resize', handleResize);
        });

        onUnmounted(() => {
            stopAnimLoop();
            if (fgInstance) { fgInstance._destructor && fgInstance._destructor(); fgInstance = null; }
            window.removeEventListener('resize', handleResize);
        });

        return {
            loading, error, selectedNs, selectedResourceUid, selectedNode, detailLoading, depth, kindFilter,
            suggestions, suggestVisible, suggestActiveIdx,
            graphDataCount, graphEdgeCount, currentPage, totalPages,
            animMode, showParticles, showCurves,
            selectedKinds, showAllKinds,
            viewMode, businessUnitFilter, businessUnits,
            namespaces, kinds, visibleKinds, hasHiddenKinds, nodesInNs, relatedResources,
            metrics, metricsLoading,
            getKindColor, getMetricStatusColor, getMetricDisplayValue, getMetricLabel,
            handleRefresh, handleReset, handleDepthChange, handleAnimModeChange, handlePageChange,
            handleZoomIn, handleZoomOut, handleFitView,
            handleResourceSelect, handleSuggestionSelect, handleSuggestKeydown,
            handleViewModeChange, handleBusinessUnitChange,
            toggleKind,
            animModes, layoutModes,
            graph: graphRef
        };
    }
}).mount('#app');
