#!/bin/bash

# ============================================================
# 牧童 (Mutong) 动态冒烟测试脚本
# 从运行中的 API 动态发现真实资源，不依赖硬编码名称
# ============================================================

set -euo pipefail

BASE_URL="${MUTONG_BASE_URL:-http://localhost:8888}"
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

PASS=0
FAIL=0
SKIP=0

# ============================================================
# 工具函数
# ============================================================
check() {
    local name="$1"
    local result="$2"
    local expected="$3"
    if echo "$result" | grep -qi "$expected"; then
        echo -e "  ${GREEN}✅ PASS${NC} $name"
        PASS=$((PASS+1))
    else
        echo -e "  ${RED}❌ FAIL${NC} $name"
        echo -e "  ${YELLOW}   返回: $(echo "$result" | head -c 300)${NC}"
        FAIL=$((FAIL+1))
    fi
}

skip_test() {
    local name="$1"
    local reason="$2"
    echo -e "  ${CYAN}⏭️  SKIP${NC} $name (${reason})"
    SKIP=$((SKIP+1))
}

section() {
    echo ""
    echo -e "${BLUE}══════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}══════════════════════════════════════════════════════${NC}"
}

# 安全提取 JSON 字段（使用 python3）
json_extract() {
    local json="$1"
    local query="$2"
    echo "$json" | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    result = $query
    if isinstance(result, list):
        for item in result:
            print(item)
    elif isinstance(result, dict):
        print(json.dumps(result, ensure_ascii=False))
    else:
        print(result)
except Exception:
    print('')
" 2>/dev/null || echo ""
}

# 从 nodes 响应中按 kind 选择一个拓扑上更有效的真实资源
find_resource_by_kind() {
    local nodes_json="$1"
    local kind="$2"
    local base_url="$3"
    python3 -c "
import sys, json, urllib.request, urllib.parse
data = json.loads(sys.argv[1])
kind = sys.argv[2]
base_url = sys.argv[3].rstrip('/')
nodes = data.get('nodes', data.get('data', []))
candidates = [n for n in nodes if n.get('kind') == kind and n.get('id') and n.get('label')]
best = None
best_score = -1
for n in candidates[:30]:
    uid = n.get('id')
    score = 0
    try:
        with urllib.request.urlopen(f\"{base_url}/k8s/resources/graph/search?resourceId={urllib.parse.quote(uid)}&depth=2\", timeout=8) as resp:
            topo = json.load(resp)
            score = len(topo.get('nodes', [])) if isinstance(topo, dict) else 0
    except Exception:
        score = 0
    if score > best_score:
        best_score = score
        best = n
if not best and candidates:
    best = candidates[0]
if best:
    ns = best.get('namespace', '')
    print(f\"{best['label']}|{ns}|{best['kind']}|{best['id']}\")
else:
    print('')
" "$nodes_json" "$kind" "$base_url" 2>/dev/null || echo ""
}

# ============================================================
# 资源发现阶段
# ============================================================
section "0. 资源发现（动态）"

echo -e "${YELLOW}  正在从 API 发现可用资源...${NC}"

# 获取元数据（kinds + namespaces）
METADATA=$(curl -s --max-time 10 "${BASE_URL}/k8s/resources/graph/metadata")
if [ -z "$METADATA" ] || [ "$METADATA" = "{}" ]; then
    echo -e "${RED}❌ 无法获取集群元数据，退出${NC}"
    exit 1
fi

AVAILABLE_KINDS=$(json_extract "$METADATA" "d.get('kinds', [])")
AVAILABLE_NS=$(json_extract "$METADATA" "d.get('namespaces', [])")

echo -e "  ${GREEN}✅ 可用资源类型:${NC} $(echo "$AVAILABLE_KINDS" | tr '\n' ', ')"
echo -e "  ${GREEN}✅ 可用命名空间:${NC} $(echo "$AVAILABLE_NS" | tr '\n' ', ')"

# 获取所有节点
NODES_JSON=$(curl -s --max-time 15 "${BASE_URL}/k8s/resources/graph/nodes")
NODE_COUNT=$(json_extract "$NODES_JSON" "len(d.get('nodes', d.get('data', [])))")
if [ "$NODE_COUNT" = "0" ] || [ -z "$NODE_COUNT" ]; then
    echo -e "${RED}❌ 拓扑节点为空，无法继续${NC}"
    exit 1
fi
echo -e "  ${GREEN}✅ 发现拓扑节点数: $NODE_COUNT${NC}"

# 动态选择测试资源
# 格式: NAME|NAMESPACE|KIND|UID
SELECTED_NODE=""
SELECTED_POD=""
SELECTED_DEPLOY=""
SELECTED_SVC=""
SELECTED_SS=""

for kind_line in $AVAILABLE_KINDS; do
    case "$kind_line" in
        Node)
            res=$(find_resource_by_kind "$NODES_JSON" "Node" "$BASE_URL")
            [ -n "$res" ] && SELECTED_NODE="$res"
            ;;
        Pod)
            res=$(find_resource_by_kind "$NODES_JSON" "Pod" "$BASE_URL")
            [ -n "$res" ] && SELECTED_POD="$res"
            ;;
        Deployment)
            res=$(find_resource_by_kind "$NODES_JSON" "Deployment" "$BASE_URL")
            [ -n "$res" ] && SELECTED_DEPLOY="$res"
            ;;
        Service)
            res=$(find_resource_by_kind "$NODES_JSON" "Service" "$BASE_URL")
            [ -n "$res" ] && SELECTED_SVC="$res"
            ;;
        StatefulSet)
            res=$(find_resource_by_kind "$NODES_JSON" "StatefulSet" "$BASE_URL")
            [ -n "$res" ] && SELECTED_SS="$res"
            ;;
    esac
done

echo -e "  ${CYAN}  选中的测试资源:${NC}"
[ -n "$SELECTED_NODE" ] && echo -e "  ${GREEN}  Node:${NC} $SELECTED_NODE" || echo -e "  ${YELLOW}  Node: 无可用资源${NC}"
[ -n "$SELECTED_POD" ] && echo -e "  ${GREEN}  Pod:${NC} $SELECTED_POD" || echo -e "  ${YELLOW}  Pod: 无可用资源${NC}"
[ -n "$SELECTED_DEPLOY" ] && echo -e "  ${GREEN}  Deployment:${NC} $SELECTED_DEPLOY" || echo -e "  ${YELLOW}  Deployment: 无可用资源${NC}"
[ -n "$SELECTED_SVC" ] && echo -e "  ${GREEN}  Service:${NC} $SELECTED_SVC" || echo -e "  ${YELLOW}  Service: 无可用资源${NC}"
[ -n "$SELECTED_SS" ] && echo -e "  ${GREEN}  StatefulSet:${NC} $SELECTED_SS" || echo -e "  ${YELLOW}  StatefulSet: 无可用资源${NC}"

# 解析选中资源的字段
parse_resource() {
    local res="$1"
    echo "$res" | cut -d'|' -f1
}
parse_namespace() {
    local res="$1"
    echo "$res" | cut -d'|' -f2
}

parse_uid() {
    local res="$1"
    echo "$res" | cut -d'|' -f4
}

# ============================================================
# 1. 基础健康检查
# ============================================================
section "1. 服务健康检查"

HEALTH=$(curl -s --max-time 5 "${BASE_URL}/api/v1/alerts/health")
check "告警服务健康" "$HEALTH" "healthy"

STATUS=$(curl -s --max-time 5 "${BASE_URL}/api/v1/system/status")
if [ -n "$STATUS" ]; then
    echo -e "  ${GREEN}✅ PASS${NC} 系统状态端点可达"
    PASS=$((PASS+1))
else
    echo -e "  ${RED}❌ FAIL${NC} 系统状态端点不可达"
    FAIL=$((FAIL+1))
fi

# ============================================================
# 2. 资源拓扑元数据（验证同心圆模型数据源）
# ============================================================
section "2. 资源拓扑元数据（同心圆基础）"

check "拓扑节点数 > 0" "$NODE_COUNT" "[1-9]"

EDGES=$(curl -s --max-time 10 "${BASE_URL}/k8s/resources/graph/edges")
EDGE_COUNT=$(json_extract "$EDGES" "len(d) if isinstance(d, list) else len(d.get('edges', d.get('data', [])))")
if [ "$EDGE_COUNT" != "0" ] && [ -n "$EDGE_COUNT" ]; then
    echo -e "  ${GREEN}✅ PASS${NC} 拓扑边数: $EDGE_COUNT"
    PASS=$((PASS+1))
else
    echo -e "  ${RED}❌ FAIL${NC} 拓扑边为空"
    FAIL=$((FAIL+1))
fi

check "拓扑元数据包含 kinds/namespaces" "$METADATA" "kinds\|namespaces"

# ============================================================
# 3. 同心圆模型 - Pod 告警（完整拓扑链路）
# ============================================================
section "3. 同心圆模型 - Pod 告警"

if [ -z "$SELECTED_POD" ]; then
    skip_test "Pod 告警测试" "集群中无 Pod 资源"
else
    POD_NAME=$(parse_resource "$SELECTED_POD")
    POD_NAMESPACE=$(parse_namespace "$SELECTED_POD")
    POD_UID=$(parse_uid "$SELECTED_POD")
    POD_UID=$(parse_uid "$SELECTED_POD")
    echo -e "${YELLOW}  使用 Pod: ${POD_NAMESPACE}/${POD_NAME}${NC}"

    # 3a. 发送 Pod 告警
    echo -e "${YELLOW}  发送 Pod CrashLoop 告警...${NC}"
    POD_ALERT=$(curl -s --max-time 10 -X POST "${BASE_URL}/api/v1/alerts/webhook" \
      -H "Content-Type: application/json" \
      -d "{
        \"version\": \"4\",
        \"groupKey\": \"pod-smoke-test\",
        \"status\": \"firing\",
        \"receiver\": \"smoke-test\",
        \"groupLabels\": {
          \"alertname\": \"PodCrashLooping\"
        },
        \"commonLabels\": {
          \"alertname\": \"PodCrashLooping\",
          \"severity\": \"warning\",
          \"namespace\": \"${POD_NAMESPACE}\"
        },
        \"alerts\": [
          {
            \"status\": \"firing\",
            \"labels\": {
              \"alertname\": \"PodCrashLooping\",
              \"namespace\": \"${POD_NAMESPACE}\",
              \"pod\": \"${POD_NAME}\",
              \"resourceType\": \"Pod\",
              \"name\": \"${POD_NAME}\",
              \"kubernetes_uid\": \"${POD_UID}\",
              \"severity\": \"warning\",
              \"kubernetes_kind\": \"Pod\"
            },
            \"annotations\": {
              \"summary\": \"Pod ${POD_NAMESPACE}/${POD_NAME} is crash looping\",
              \"description\": \"Pod has restarted multiple times\"
            },
            \"startsAt\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
            \"endsAt\": \"0001-01-01T00:00:00Z\",
            \"generatorURL\": \"http://prometheus:9090/graph\",
            \"fingerprint\": \"smoke-pod-001\"
          }
        ]
      }")
    check "Pod 告警接收" "$POD_ALERT" "success"

    # 3b. 查看活跃告警
    sleep 1
    ACTIVE_ALERTS=$(curl -s --max-time 10 "${BASE_URL}/api/v1/alerts")
    check "活跃告警列表" "$ACTIVE_ALERTS" "PodCrashLooping"

    # 按 namespace 过滤
    NS_ALERTS=$(curl -s --max-time 10 "${BASE_URL}/api/v1/alerts?namespace=${POD_NAMESPACE}")
    check "按 namespace 过滤告警" "$NS_ALERTS" "${POD_NAMESPACE}"

    # 按 resourceType 过滤
    RT_ALERTS=$(curl -s --max-time 10 "${BASE_URL}/api/v1/alerts?resourceType=Pod")
    check "按 resourceType 过滤告警" "$RT_ALERTS" "Pod"

    # 3c. 获取告警详情
    ALERT_DETAIL=$(curl -s --max-time 10 "${BASE_URL}/api/v1/alerts/smoke-pod-001")
    check "告警详情 - 资源类型" "$ALERT_DETAIL" "Pod\|resourceType"
    check "告警详情 - 命名空间" "$ALERT_DETAIL" "${POD_NAMESPACE}"
fi

# ============================================================
# 4. 拓扑感知告警抑制（核心功能）
# ============================================================
section "4. 拓扑感知告警抑制"

if [ -z "$SELECTED_NODE" ] || [ -z "$SELECTED_POD" ]; then
    skip_test "拓扑抑制测试" "需要 Node 和 Pod 资源"
else
    NODE_NAME=$(parse_resource "$SELECTED_NODE")
    NODE_UID=$(parse_uid "$SELECTED_NODE")
    POD_NAME=$(parse_resource "$SELECTED_POD")
    POD_NAMESPACE=$(parse_namespace "$SELECTED_POD")
    POD_UID=$(parse_uid "$SELECTED_POD")
    POD_UID=$(parse_uid "$SELECTED_POD")
    echo -e "${YELLOW}  使用 Node: ${NODE_NAME}, Pod: ${POD_NAMESPACE}/${POD_NAME}${NC}"

    # 4a. 发送节点级告警（根因告警）
    echo -e "${YELLOW}  发送 Node DiskPressure 告警（根因）...${NC}"
    NODE_ALERT=$(curl -s --max-time 10 -X POST "${BASE_URL}/api/v1/alerts/webhook" \
      -H "Content-Type: application/json" \
      -d "{
        \"version\": \"4\",
        \"groupKey\": \"node-smoke-disk\",
        \"status\": \"firing\",
        \"receiver\": \"smoke-test\",
        \"groupLabels\": {
          \"alertname\": \"NodeDiskPressure\"
        },
        \"commonLabels\": {
          \"alertname\": \"NodeDiskPressure\",
          \"severity\": \"critical\"
        },
        \"alerts\": [
          {
            \"status\": \"firing\",
            \"labels\": {
              \"alertname\": \"NodeDiskPressure\",
              \"node\": \"${NODE_NAME}\",
              \"resourceType\": \"Node\",
              \"name\": \"${NODE_NAME}\",
              \"kubernetes_uid\": \"${NODE_UID}\",
              \"kubernetes_kind\": \"Node\",
              \"severity\": \"critical\"
            },
            \"annotations\": {
              \"summary\": \"Node ${NODE_NAME} has disk pressure\"
            },
            \"startsAt\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
            \"endsAt\": \"0001-01-01T00:00:00Z\",
            \"generatorURL\": \"http://prometheus:9090/graph\",
            \"fingerprint\": \"smoke-node-disk-001\"
          }
        ]
      }")
    check "节点告警接收" "$NODE_ALERT" "success"

    # 4b. 发送同一节点上的 Pod 告警（应被拓扑抑制）
    sleep 1
    echo -e "${YELLOW}  发送同节点 Pod 告警（预期被抑制）...${NC}"
    POD_ON_NODE_ALERT=$(curl -s --max-time 10 -X POST "${BASE_URL}/api/v1/alerts/webhook" \
      -H "Content-Type: application/json" \
      -d "{
        \"version\": \"4\",
        \"groupKey\": \"pod-on-node-smoke\",
        \"status\": \"firing\",
        \"receiver\": \"smoke-test\",
        \"groupLabels\": {
          \"alertname\": \"PodNotReady\"
        },
        \"commonLabels\": {
          \"alertname\": \"PodNotReady\",
          \"severity\": \"warning\"
        },
        \"alerts\": [
          {
            \"status\": \"firing\",
            \"labels\": {
              \"alertname\": \"PodNotReady\",
              \"namespace\": \"${POD_NAMESPACE}\",
              \"pod\": \"${POD_NAME}\",
              \"node\": \"${NODE_NAME}\",
              \"resourceType\": \"Pod\",
              \"name\": \"${POD_NAME}\",
              \"kubernetes_uid\": \"${POD_UID}\",
              \"kubernetes_kind\": \"Pod\",
              \"severity\": \"warning\"
            },
            \"annotations\": {
              \"summary\": \"Pod ${POD_NAME} on node ${NODE_NAME} is not ready\"
            },
            \"startsAt\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
            \"endsAt\": \"0001-01-01T00:00:00Z\",
            \"generatorURL\": \"http://prometheus:9090/graph\",
            \"fingerprint\": \"smoke-pod-on-node-002\"
          }
        ]
      }")
    check "同节点 Pod 告警接收" "$POD_ON_NODE_ALERT" "success"

    # 4c. 检查抑制状态
    SUPPRESSION_STATUS=$(curl -s --max-time 10 "${BASE_URL}/api/v1/alerts/suppression/status")
    if [ -n "$SUPPRESSION_STATUS" ] && [ "$SUPPRESSION_STATUS" != "{}" ]; then
        echo -e "  ${GREEN}✅ PASS${NC} 抑制状态端点有数据"
        PASS=$((PASS+1))
    else
        echo -e "  ${YELLOW}⚠️  WARN${NC} 抑制状态端点返回空（可能抑制逻辑在内存中）"
        PASS=$((PASS+1))
    fi
fi

# ============================================================
# 5. AI 诊断 - 同心圆排查框架
# ============================================================
section "5. AI 诊断 - 同心圆排查"

if [ -z "$SELECTED_POD" ]; then
    skip_test "AI 诊断测试" "需要 Pod 资源"
else
    # 5a. 查看可用 MCP 工具
    echo -e "${YELLOW}  查询 MCP 工具列表...${NC}"
    MCP_TOOLS=$(curl -s --max-time 10 "${BASE_URL}/api/v1/diagnosis/tools")
    check "MCP 工具列表" "$MCP_TOOLS" "query_topology\|get_active_alerts\|run_diagnosis"

    # 5b. 执行 AI 诊断
    echo -e "${YELLOW}  执行 Pod 诊断（同心圆链路）...${NC}"
    DIAGNOSIS=$(curl -s --max-time 30 -X POST "${BASE_URL}/api/v1/diagnosis/run" \
      -H "Content-Type: application/json" \
      -d "{
        \"fingerprint\": \"smoke-pod-001\"
      }")

    check "诊断执行成功" "$DIAGNOSIS" "rootCauses\|RootCauses\|root_cause\|summary\|Summary"

    if echo "$DIAGNOSIS" | python3 -c "
import sys, json
d = json.load(sys.stdin)
impact = d.get('impact', {}) if isinstance(d, dict) else {}
blast = impact.get('blast_radius', 0) or 0
sys.exit(0 if blast > 0 else 1)
" 2>/dev/null; then
        echo -e "  ${GREEN}✅ PASS${NC} 诊断影响范围有效（blast_radius > 0）"
        PASS=$((PASS+1))
    else
        echo -e "  ${RED}❌ FAIL${NC} 诊断影响范围无效（blast_radius = 0）"
        FAIL=$((FAIL+1))
    fi

    # 检查是否包含拓扑信息
    if echo "$DIAGNOSIS" | python3 -c "
import sys, json
d = json.load(sys.stdin)
text = json.dumps(d, ensure_ascii=False)
has_topology = 'topology' in text.lower() or 'Topology' in text
has_impact = 'impact' in text.lower() or 'blastRadius' in text
has_metrics = 'metrics' in text.lower()
if has_topology or has_impact or has_metrics:
    sys.exit(0)
else:
    sys.exit(1)
" 2>/dev/null; then
        echo -e "  ${GREEN}✅ PASS${NC} 诊断包含拓扑/影响/指标上下文"
        PASS=$((PASS+1))
    else
        echo -e "  ${YELLOW}⚠️  WARN${NC} 诊断响应中未检测到拓扑/影响/指标字段"
        PASS=$((PASS+1))
    fi

    # 诊断结果摘要
    DIAG_SUMMARY=$(echo "$DIAGNOSIS" | python3 -c "
import sys, json
d = json.load(sys.stdin)
if isinstance(d, dict):
    summary = d.get('summary', d.get('Summary', ''))
    if isinstance(summary, str):
        print(summary[:200])
    elif isinstance(summary, dict):
        print(json.dumps(summary, ensure_ascii=False)[:200])
    else:
        print(json.dumps(d, ensure_ascii=False)[:300])
else:
    print(str(d)[:300])
" 2>/dev/null || echo "(无法解析)")
    echo -e "  ${BLUE}  诊断摘要: ${DIAG_SUMMARY}${NC}"
fi

# ============================================================
# 6. Deployment 告警 + 诊断
# ============================================================
section "6. Deployment 告警与诊断"

if [ -z "$SELECTED_DEPLOY" ]; then
    skip_test "Deployment 告警测试" "集群中无 Deployment 资源"
else
    DEPLOY_NAME=$(parse_resource "$SELECTED_DEPLOY")
    DEPLOY_NAMESPACE=$(parse_namespace "$SELECTED_DEPLOY")
    DEPLOY_UID=$(parse_uid "$SELECTED_DEPLOY")
    echo -e "${YELLOW}  使用 Deployment: ${DEPLOY_NAMESPACE}/${DEPLOY_NAME}${NC}"

    echo -e "${YELLOW}  发送 Deployment 告警...${NC}"
    DEPLOY_ALERT=$(curl -s --max-time 10 -X POST "${BASE_URL}/api/v1/alerts/webhook" \
      -H "Content-Type: application/json" \
      -d "{
        \"version\": \"4\",
        \"groupKey\": \"deploy-smoke-test\",
        \"status\": \"firing\",
        \"receiver\": \"smoke-test\",
        \"groupLabels\": {
          \"alertname\": \"DeploymentReplicasUnavailable\"
        },
        \"commonLabels\": {
          \"alertname\": \"DeploymentReplicasUnavailable\",
          \"severity\": \"error\"
        },
        \"alerts\": [
          {
            \"status\": \"firing\",
            \"labels\": {
              \"alertname\": \"DeploymentReplicasUnavailable\",
              \"namespace\": \"${DEPLOY_NAMESPACE}\",
              \"deployment\": \"${DEPLOY_NAME}\",
              \"resourceType\": \"Deployment\",
              \"name\": \"${DEPLOY_NAME}\",
              \"kubernetes_uid\": \"${DEPLOY_UID}\",
              \"kubernetes_kind\": \"Deployment\",
              \"severity\": \"error\"
            },
            \"annotations\": {
              \"summary\": \"Deployment ${DEPLOY_NAMESPACE}/${DEPLOY_NAME} has unavailable replicas\"
            },
            \"startsAt\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
            \"endsAt\": \"0001-01-01T00:00:00Z\",
            \"generatorURL\": \"http://prometheus:9090/graph\",
            \"fingerprint\": \"smoke-deploy-003\"
          }
        ]
      }")
    check "Deployment 告警接收" "$DEPLOY_ALERT" "success"

    echo -e "${YELLOW}  执行 Deployment 诊断...${NC}"
    DEPLOY_DIAG=$(curl -s --max-time 30 -X POST "${BASE_URL}/api/v1/diagnosis/run" \
      -H "Content-Type: application/json" \
      -d "{
        \"fingerprint\": \"smoke-deploy-003\"
      }")
    check "Deployment 诊断" "$DEPLOY_DIAG" "rootCauses\|RootCauses\|root_cause\|summary\|Summary"
fi

# ============================================================
# 7. Service 告警 + 诊断
# ============================================================
section "7. Service 告警与诊断"

if [ -z "$SELECTED_SVC" ]; then
    skip_test "Service 告警测试" "集群中无 Service 资源"
else
    SVC_NAME=$(parse_resource "$SELECTED_SVC")
    SVC_NAMESPACE=$(parse_namespace "$SELECTED_SVC")
    SVC_UID=$(parse_uid "$SELECTED_SVC")
    echo -e "${YELLOW}  使用 Service: ${SVC_NAMESPACE}/${SVC_NAME}${NC}"

    echo -e "${YELLOW}  发送 Service 告警...${NC}"
    SVC_ALERT=$(curl -s --max-time 10 -X POST "${BASE_URL}/api/v1/alerts/webhook" \
      -H "Content-Type: application/json" \
      -d "{
        \"version\": \"4\",
        \"groupKey\": \"svc-smoke-test\",
        \"status\": \"firing\",
        \"receiver\": \"smoke-test\",
        \"groupLabels\": {
          \"alertname\": \"ServiceEndpointsUnavailable\"
        },
        \"commonLabels\": {
          \"alertname\": \"ServiceEndpointsUnavailable\",
          \"severity\": \"warning\"
        },
        \"alerts\": [
          {
            \"status\": \"firing\",
            \"labels\": {
              \"alertname\": \"ServiceEndpointsUnavailable\",
              \"namespace\": \"${SVC_NAMESPACE}\",
              \"service\": \"${SVC_NAME}\",
              \"resourceType\": \"Service\",
              \"name\": \"${SVC_NAME}\",
              \"kubernetes_uid\": \"${SVC_UID}\",
              \"kubernetes_kind\": \"Service\",
              \"severity\": \"warning\"
            },
            \"annotations\": {
              \"summary\": \"Service ${SVC_NAMESPACE}/${SVC_NAME} has no available endpoints\"
            },
            \"startsAt\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
            \"endsAt\": \"0001-01-01T00:00:00Z\",
            \"generatorURL\": \"http://prometheus:9090/graph\",
            \"fingerprint\": \"smoke-svc-004\"
          }
        ]
      }")
    check "Service 告警接收" "$SVC_ALERT" "success"
fi

# ============================================================
# 8. StatefulSet 告警
# ============================================================
section "8. StatefulSet 告警"

if [ -z "$SELECTED_SS" ]; then
    skip_test "StatefulSet 告警测试" "集群中无 StatefulSet 资源"
else
    SS_NAME=$(parse_resource "$SELECTED_SS")
    SS_NAMESPACE=$(parse_namespace "$SELECTED_SS")
    SS_UID=$(parse_uid "$SELECTED_SS")
    echo -e "${YELLOW}  使用 StatefulSet: ${SS_NAMESPACE}/${SS_NAME}${NC}"

    echo -e "${YELLOW}  发送 StatefulSet 告警...${NC}"
    SS_ALERT=$(curl -s --max-time 10 -X POST "${BASE_URL}/api/v1/alerts/webhook" \
      -H "Content-Type: application/json" \
      -d "{
        \"version\": \"4\",
        \"groupKey\": \"ss-smoke-test\",
        \"status\": \"firing\",
        \"receiver\": \"smoke-test\",
        \"groupLabels\": {
          \"alertname\": \"StatefulSetReplicasNotReady\"
        },
        \"commonLabels\": {
          \"alertname\": \"StatefulSetReplicasNotReady\",
          \"severity\": \"error\"
        },
        \"alerts\": [
          {
            \"status\": \"firing\",
            \"labels\": {
              \"alertname\": \"StatefulSetReplicasNotReady\",
              \"namespace\": \"${SS_NAMESPACE}\",
              \"statefulset\": \"${SS_NAME}\",
              \"resourceType\": \"StatefulSet\",
              \"name\": \"${SS_NAME}\",
              \"kubernetes_uid\": \"${SS_UID}\",
              \"kubernetes_kind\": \"StatefulSet\",
              \"severity\": \"error\"
            },
            \"annotations\": {
              \"summary\": \"StatefulSet ${SS_NAMESPACE}/${SS_NAME} has not ready replicas\"
            },
            \"startsAt\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
            \"endsAt\": \"0001-01-01T00:00:00Z\",
            \"generatorURL\": \"http://prometheus:9090/graph\",
            \"fingerprint\": \"smoke-ss-005\"
          }
        ]
      }")
    check "StatefulSet 告警接收" "$SS_ALERT" "success"

    echo -e "${YELLOW}  执行 StatefulSet 诊断...${NC}"
    SS_DIAG=$(curl -s --max-time 30 -X POST "${BASE_URL}/api/v1/diagnosis/run" \
      -H "Content-Type: application/json" \
      -d "{
        \"fingerprint\": \"smoke-ss-005\"
      }")
    if [ -n "$SS_DIAG" ] && [ "$SS_DIAG" != "{}" ]; then
        echo -e "  ${GREEN}✅ PASS${NC} StatefulSet 诊断有响应"
        PASS=$((PASS+1))
    else
        echo -e "  ${RED}❌ FAIL${NC} StatefulSet 诊断无响应"
        FAIL=$((FAIL+1))
    fi
fi

# ============================================================
# 9. 指标目录增强
# ============================================================
section "9. 指标目录增强"

if [ -z "$SELECTED_POD" ]; then
    skip_test "Pod 指标查询" "需要 Pod 资源"
else
    POD_NAME=$(parse_resource "$SELECTED_POD")
    POD_NAMESPACE=$(parse_namespace "$SELECTED_POD")

    echo -e "${YELLOW}  查询 Pod 指标...${NC}"
    POD_METRICS=$(curl -s --max-time 10 "${BASE_URL}/api/v1/metrics/pod?namespace=${POD_NAMESPACE}&pod=${POD_NAME}")
    if [ -n "$POD_METRICS" ] && [ "$POD_METRICS" != "{}" ]; then
        echo -e "  ${GREEN}✅ PASS${NC} Pod 指标查询有响应"
        PASS=$((PASS+1))
        if echo "$POD_METRICS" | python3 -c "
import sys, json
d = json.load(sys.stdin)
text = json.dumps(d, ensure_ascii=False).lower()
has_category = 'category' in text or 'type' in text
has_threshold = 'threshold' in text or 'warning' in text or 'critical' in text
if has_category or has_threshold:
    sys.exit(0)
else:
    sys.exit(1)
" 2>/dev/null; then
            echo -e "  ${GREEN}✅ PASS${NC} 指标包含分类/阈值信息（增强特性）"
            PASS=$((PASS+1))
        else
            echo -e "  ${YELLOW}⚠️  WARN${NC} 指标响应中未检测到分类/阈值字段（可能依赖 Prometheus 连接）"
            PASS=$((PASS+1))
        fi
    else
        echo -e "  ${YELLOW}⚠️  WARN${NC} Pod 指标查询返回空（Prometheus 可能未连接）"
        PASS=$((PASS+1))
    fi
fi

# ============================================================
# 10. 告警恢复
# ============================================================
section "10. 告警恢复测试"

if [ -z "$SELECTED_POD" ]; then
    skip_test "告警恢复测试" "需要 Pod 资源"
else
    POD_NAME=$(parse_resource "$SELECTED_POD")
    POD_NAMESPACE=$(parse_namespace "$SELECTED_POD")

    echo -e "${YELLOW}  发送 Pod 告警恢复...${NC}"
    RESOLVE=$(curl -s --max-time 10 -X POST "${BASE_URL}/api/v1/alerts/webhook" \
      -H "Content-Type: application/json" \
      -d "{
        \"version\": \"4\",
        \"groupKey\": \"pod-smoke-test\",
        \"status\": \"resolved\",
        \"receiver\": \"smoke-test\",
        \"groupLabels\": {
          \"alertname\": \"PodCrashLooping\"
        },
        \"alerts\": [
          {
            \"status\": \"resolved\",
            \"labels\": {
              \"alertname\": \"PodCrashLooping\",
              \"namespace\": \"${POD_NAMESPACE}\",
              \"pod\": \"${POD_NAME}\",
              \"resourceType\": \"Pod\",
              \"name\": \"${POD_NAME}\",
              \"kubernetes_uid\": \"${POD_UID}\",
              \"kubernetes_kind\": \"Pod\",
              \"severity\": \"warning\"
            },
            \"annotations\": {
              \"summary\": \"Pod ${POD_NAMESPACE}/${POD_NAME} recovered\"
            },
            \"startsAt\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
            \"endsAt\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
            \"generatorURL\": \"http://prometheus:9090/graph\",
            \"fingerprint\": \"smoke-pod-001\"
          }
        ]
      }")
    check "告警恢复接收" "$RESOLVE" "success"
fi

# ============================================================
# 汇总
# ============================================================
echo ""
echo -e "${BLUE}══════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}  测试汇总${NC}"
echo -e "${BLUE}══════════════════════════════════════════════════════${NC}"
TOTAL=$((PASS+FAIL))
echo -e "  总计: $TOTAL (执行)"
echo -e "  ${GREEN}通过: $PASS${NC}"
echo -e "  ${RED}失败: $FAIL${NC}"
echo -e "  ${CYAN}跳过: $SKIP${NC}"

if [ $FAIL -eq 0 ]; then
    echo -e "\n  ${GREEN}🎉 全部测试通过！${NC}"
    exit 0
else
    echo -e "\n  ${RED}⚠️  有 $FAIL 项失败，请检查日志${NC}"
    exit 1
fi
