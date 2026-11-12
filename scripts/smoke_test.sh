#!/bin/bash

# ============================================================
# 牧童 (Mutong) 冒烟测试脚本
# 从运行中的 API 动态发现真实资源，不依赖硬编码名称
#
# 需要活服务。环境变量：
#   MUTONG_BASE_URL      服务地址，默认 http://localhost:8888
#   MUTONG_USERNAME      登录用户名，默认 admin
#   MUTONG_PASSWORD      登录密码（与 MUTONG_TOKEN 二选一）
#   MUTONG_TOKEN         已有 PAT/SAT（mtp_/mts_），走 Bearer 鉴权
#   MUTONG_WEBHOOK_SECRET 告警 webhook HMAC 密钥，未设置则跳过 webhook 用例
#   HTTP_TIMEOUT         单请求超时秒数，默认 20
#
# 路由与鉴权对齐 cmd/main.go：/api/v1/* 需登录态，/healthz 与
# /api/v1/alerts/webhook 为公开路径；webhook 签名见
# controllers/webhook_verifier.go：HMAC-SHA256(secret, 时间戳 + "\n" + 原始 body)
# ============================================================

set -euo pipefail

BASE_URL="${MUTONG_BASE_URL:-http://localhost:8888}"
BASE_URL="${BASE_URL%/}"
USERNAME="${MUTONG_USERNAME:-admin}"
PASSWORD="${MUTONG_PASSWORD:-}"
TOKEN="${MUTONG_TOKEN:-}"
WEBHOOK_SECRET="${MUTONG_WEBHOOK_SECRET:-}"
HTTP_TIMEOUT="${HTTP_TIMEOUT:-20}"
RUN_ID="$(date +%s)-$$"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

PASS=0
FAIL=0
SKIP=0

COOKIE_JAR="$(mktemp)"
trap 'rm -f "$COOKIE_JAR"' EXIT

# 会话令牌只能走 Cookie：BearerTokenMiddleware 的 Bearer 分支仅接受
# PAT/OIDC/OAuth2 令牌，不接受登录返回的 session token。
AUTH_ARGS=(-H "X-Trace-ID: smoke-${RUN_ID}")

# ============================================================
# 工具函数
# ============================================================
section() {
    echo ""
    echo -e "${BLUE}══════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}══════════════════════════════════════════════════════${NC}"
}

pass() {
    echo -e "  ${GREEN}✅ PASS${NC} $1"
    PASS=$((PASS + 1))
}

fail() {
    echo -e "  ${RED}❌ FAIL${NC} $1"
    [ -n "${2:-}" ] && echo -e "  ${YELLOW}   返回: $(printf '%s' "$2" | head -c 300)${NC}"
    FAIL=$((FAIL + 1))
}

skip_test() {
    echo -e "  ${CYAN}⏭️  SKIP${NC} $1 (${2})"
    SKIP=$((SKIP + 1))
}

# 响应约定：body + "\n" + http_code（curl -w 追加，连接失败时也会输出 000）
code_of() { printf '%s' "${1##*$'\n'}"; }
body_of() { printf '%s' "${1%$'\n'*}"; }

# 仅取状态码，不跟随重定向
status_only() {
    local code
    code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time "$HTTP_TIMEOUT" "$@" 2>/dev/null || true)"
    printf '%s' "${code:-000}"
}

check_body() {
    local name="$1" resp="$2" expected="$3" body
    body="$(body_of "$resp")"
    if printf '%s' "$body" | grep -qi "$expected"; then
        pass "$name"
    else
        fail "$name" "$body"
    fi
}

check_code() {
    local name="$1" resp="$2" want="$3" code
    code="$(code_of "$resp")"
    if [ "$code" = "$want" ]; then
        pass "$name (HTTP $code)"
    else
        fail "$name (HTTP $code, 期望 $want)" "$(body_of "$resp")"
    fi
}

# 带登录态的请求；-L 跟随 /k8s/* → /api/v1/k8s/* 等重定向
request() {
    local method="$1" path="$2" body="${3:-}"
    local args=(-sS -L --max-time "$HTTP_TIMEOUT" -X "$method"
        -b "$COOKIE_JAR" -c "$COOKIE_JAR" -w '\n%{http_code}' "${AUTH_ARGS[@]}")
    if [ -n "$body" ]; then
        args+=(-H 'Content-Type: application/json' --data-binary "$body")
    fi
    curl "${args[@]}" "${BASE_URL}${path}" 2>/dev/null || true
}

# 安全提取 JSON 字段
json_extract() {
    local json="$1" query="$2"
    printf '%s' "$json" | python3 -c "
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

# webhook 签名：HMAC-SHA256(secret, timestamp + "\n" + raw body)
sign_payload() {
    python3 - "$1" "$2" "$WEBHOOK_SECRET" <<'PY' 2>/dev/null
import hashlib, hmac, sys
ts, body, secret = sys.argv[1], sys.argv[2], sys.argv[3]
msg = ts.encode() + b"\n" + body.encode()
print(hmac.new(secret.encode(), msg, hashlib.sha256).hexdigest())
PY
}

post_webhook() {
    local path="$1" body="$2" ts sig
    ts="$(date +%s)"
    sig="$(sign_payload "$ts" "$body")"
    if [ -z "$sig" ]; then
        printf '\n000'
        return 0
    fi
    curl -sS -L --max-time "$HTTP_TIMEOUT" -X POST \
        -H 'Content-Type: application/json' \
        -H "X-Alertmanager-Timestamp: ${ts}" \
        -H "X-Alertmanager-Signature: ${sig}" \
        --data-binary "$body" -w '\n%{http_code}' \
        "${BASE_URL}${path}" 2>/dev/null || true
}

# 生成 Alertmanager webhook 负载（python 保证 JSON 转义正确）
alert_payload() {
    python3 - "$@" <<'PY' 2>/dev/null
import datetime, json, sys
fp, alertname, kind, name, ns, severity, status, uid, node = sys.argv[1:10]
now = datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')
labels = {
    "alertname": alertname, "severity": severity, "resourceType": kind,
    "name": name, "kubernetes_kind": kind,
}
if ns:
    labels["namespace"] = ns
if uid:
    labels["kubernetes_uid"] = uid
if node:
    labels["node"] = node
key = {"Pod": "pod", "Deployment": "deployment", "Service": "service",
       "StatefulSet": "statefulset", "Node": "node"}.get(kind)
if key:
    labels[key] = name
print(json.dumps({
    "version": "4",
    "groupKey": "smoke-" + fp,
    "status": status,
    "receiver": "smoke-test",
    "groupLabels": {"alertname": alertname},
    "commonLabels": dict(labels, **({"namespace": ns} if ns else {})),
    "alerts": [{
        "status": status,
        "labels": labels,
        "annotations": {"summary": "%s %s/%s %s" % (kind, ns, name, alertname)},
        "startsAt": now,
        "endsAt": now if status == "resolved" else "0001-01-01T00:00:00Z",
        "generatorURL": "http://prometheus:9090/graph",
        "fingerprint": fp,
    }],
}, ensure_ascii=False))
PY
}

# 从 nodes 响应中为每种 kind 选一个真实资源，输出 KIND|NAME|NS|UID
select_resources() {
    python3 - "$1" <<'PY' 2>/dev/null
import json, sys
try:
    data = json.loads(sys.argv[1])
except Exception:
    sys.exit(0)
nodes = data.get('nodes') or data.get('data') or []
wanted = ["Node", "Pod", "Deployment", "Service", "StatefulSet"]
seen = {}
for n in nodes:
    k = n.get('kind')
    if k in wanted and k not in seen and n.get('id') and n.get('label'):
        seen[k] = "%s|%s|%s|%s" % (k, n['label'], n.get('namespace', ''), n['id'])
for k in wanted:
    if k in seen:
        print(seen[k])
PY
}

field_name() { printf '%s' "${1%%|*}"; }
field_ns() { local r="${1#*|}"; printf '%s' "${r%%|*}"; }
field_uid() { printf '%s' "${1##*|}"; }

# ============================================================
# 0. 依赖与公开端点
# ============================================================
section "0. 依赖与公开端点"

for dep in curl python3; do
    if ! command -v "$dep" >/dev/null 2>&1; then
        echo -e "${RED}❌ 缺少依赖: $dep${NC}"
        exit 1
    fi
done
pass "依赖检查 (curl/python3)"

HEALTHZ="$(request GET /healthz)"
check_code "容器健康探针 /healthz" "$HEALTHZ" "200"

# ============================================================
# 1. 登录取会话
# ============================================================
section "1. 鉴权"

if [ -n "$TOKEN" ]; then
    AUTH_ARGS+=(-H "Authorization: Bearer ${TOKEN}")
    echo -e "${YELLOW}  使用 MUTONG_TOKEN (Bearer)${NC}"
elif [ -n "$PASSWORD" ]; then
    LOGIN_BODY="$(python3 -c 'import json,sys; print(json.dumps({"username": sys.argv[1], "password": sys.argv[2]}))' "$USERNAME" "$PASSWORD")"
    LOGIN="$(request POST /api/auth/login "$LOGIN_BODY")"
    check_code "登录 ${USERNAME}" "$LOGIN" "200"
    if [ "$(code_of "$LOGIN")" != "200" ]; then
        echo -e "${RED}❌ 登录失败，后续用例无法执行${NC}"
        exit 1
    fi
    if grep -q 'mutong_session' "$COOKIE_JAR"; then
        pass "会话 Cookie 已写入"
    else
        fail "会话 Cookie 缺失" "$(body_of "$LOGIN")"
    fi
else
    echo -e "${RED}❌ 未提供凭据：设置 MUTONG_PASSWORD 或 MUTONG_TOKEN${NC}"
    echo -e "${YELLOW}   示例: MUTONG_PASSWORD=... MUTONG_WEBHOOK_SECRET=... $0${NC}"
    exit 2
fi

ME="$(request GET /api/auth/me)"
check_code "当前用户 /api/auth/me" "$ME" "200"

UNAUTH="$(status_only "${BASE_URL}/api/v1/k8s/resources/graph/metadata")"
if [ "$UNAUTH" = "401" ]; then
    pass "未登录访问受保护路由被拒 (401)"
else
    fail "未登录访问受保护路由应返回 401，实际 $UNAUTH"
fi

# ============================================================
# 2. 资源发现（动态）
# ============================================================
section "2. 资源发现（动态）"

METADATA_RESP="$(request GET /api/v1/k8s/resources/graph/metadata)"
check_code "拓扑元数据可达" "$METADATA_RESP" "200"
METADATA="$(body_of "$METADATA_RESP")"
AVAILABLE_KINDS="$(json_extract "$METADATA" "d.get('kinds', [])")"
AVAILABLE_NS="$(json_extract "$METADATA" "d.get('namespaces', [])")"
echo -e "  ${GREEN}可用资源类型:${NC} $(printf '%s' "$AVAILABLE_KINDS" | tr '\n' ',')"
echo -e "  ${GREEN}可用命名空间:${NC} $(printf '%s' "$AVAILABLE_NS" | tr '\n' ',')"
check_body "元数据包含 kinds/namespaces" "$METADATA_RESP" "kinds\|namespaces"

NODES_RESP="$(request GET '/api/v1/k8s/resources/graph/nodes?pageSize=500')"
check_code "拓扑节点可达" "$NODES_RESP" "200"
NODES_JSON="$(body_of "$NODES_RESP")"
NODE_COUNT="$(json_extract "$NODES_JSON" "len(d.get('nodes', d.get('data', [])))")"
if [ -z "$NODE_COUNT" ] || [ "$NODE_COUNT" = "0" ]; then
    echo -e "${YELLOW}  ⚠️  拓扑节点为空，资源相关用例将跳过${NC}"
    NODE_COUNT=0
fi
echo -e "  ${GREEN}发现拓扑节点数: $NODE_COUNT${NC}"

SELECTED_NODE=""
SELECTED_POD=""
SELECTED_DEPLOY=""
SELECTED_SVC=""
SELECTED_SS=""
while IFS='|' read -r kind name ns uid; do
    [ -z "$kind" ] && continue
    case "$kind" in
        Node) SELECTED_NODE="$name|$ns|$uid" ;;
        Pod) SELECTED_POD="$name|$ns|$uid" ;;
        Deployment) SELECTED_DEPLOY="$name|$ns|$uid" ;;
        Service) SELECTED_SVC="$name|$ns|$uid" ;;
        StatefulSet) SELECTED_SS="$name|$ns|$uid" ;;
    esac
done < <(select_resources "$NODES_JSON")

for pair in "Node:$SELECTED_NODE" "Pod:$SELECTED_POD" "Deployment:$SELECTED_DEPLOY" "Service:$SELECTED_SVC" "StatefulSet:$SELECTED_SS"; do
    label="${pair%%:*}"
    value="${pair#*:}"
    if [ -n "$value" ]; then
        echo -e "  ${GREEN}  ${label}:${NC} $value"
    else
        echo -e "  ${YELLOW}  ${label}: 无可用资源${NC}"
    fi
done

# 旧路径应 301 到 /api/v1（不跟随重定向，单独验证）
LEGACY_CODE="$(status_only -b "$COOKIE_JAR" "${AUTH_ARGS[@]}" \
    "${BASE_URL}/k8s/resources/graph/metadata")"
if [ "$LEGACY_CODE" = "301" ] || [ "$LEGACY_CODE" = "308" ]; then
    pass "旧路径 /k8s/* 重定向 ($LEGACY_CODE)"
else
    fail "旧路径 /k8s/* 应重定向，实际 $LEGACY_CODE"
fi

EDGES_RESP="$(request GET /api/v1/k8s/resources/graph/edges)"
check_code "拓扑边可达" "$EDGES_RESP" "200"
EDGE_COUNT="$(json_extract "$(body_of "$EDGES_RESP")" "len(d) if isinstance(d, list) else len(d.get('edges', d.get('data', [])))")"
echo -e "  ${GREEN}拓扑边数: ${EDGE_COUNT:-0}${NC}"

# ============================================================
# 3. 服务健康与系统状态
# ============================================================
section "3. 服务健康与系统状态"

check_body "告警服务健康" "$(request GET /api/v1/alerts/health)" "healthy"
check_code "系统状态端点" "$(request GET /api/v1/system/status)" "200"
check_code "监控健康端点" "$(request GET /api/v1/monitoring/health)" "200"
check_code "统计概览端点" "$(request GET /api/v1/stats/overview)" "200"

# ============================================================
# 4. 告警接入（签名 webhook）
# ============================================================
section "4. 告警接入（HMAC 签名 webhook）"

FP_POD="smoke-pod-${RUN_ID}"
FP_NODE="smoke-node-${RUN_ID}"
FP_POD_ON_NODE="smoke-pod-on-node-${RUN_ID}"
FP_DEPLOY="smoke-deploy-${RUN_ID}"
FP_SVC="smoke-svc-${RUN_ID}"
FP_SS="smoke-ss-${RUN_ID}"

if [ -z "$WEBHOOK_SECRET" ]; then
    skip_test "全部 webhook 用例" "未设置 MUTONG_WEBHOOK_SECRET"
elif [ -z "$SELECTED_POD" ]; then
    skip_test "Pod 告警测试" "集群中无 Pod 资源"
else
    POD_NAME="$(field_name "$SELECTED_POD")"
    POD_NS="$(field_ns "$SELECTED_POD")"
    POD_UID="$(field_uid "$SELECTED_POD")"
    echo -e "${YELLOW}  使用 Pod: ${POD_NS}/${POD_NAME}${NC}"

    POD_ALERT="$(post_webhook /api/v1/alerts/webhook \
        "$(alert_payload "$FP_POD" PodCrashLooping Pod "$POD_NAME" "$POD_NS" warning firing "$POD_UID" "")")"
    check_code "Pod 告警接收" "$POD_ALERT" "200"
    check_body "Pod 告警响应 status=success" "$POD_ALERT" "success"

    # 篡改签名必须被拒（重放/伪造防护）
    BAD_BODY="$(alert_payload "$FP_POD-bad" PodCrashLooping Pod "$POD_NAME" "$POD_NS" warning firing "$POD_UID" "")"
    BAD_TS="$(date +%s)"
    BAD_CODE="$(status_only -X POST \
        -H 'Content-Type: application/json' \
        -H "X-Alertmanager-Timestamp: ${BAD_TS}" \
        -H "X-Alertmanager-Signature: deadbeef" \
        --data-binary "$BAD_BODY" "${BASE_URL}/api/v1/alerts/webhook")"
    if [ "$BAD_CODE" = "401" ]; then
        pass "无效签名被拒 (401)"
    else
        fail "无效签名应返回 401，实际 $BAD_CODE"
    fi

    sleep 1
    check_body "活跃告警列表" "$(request GET /api/v1/alerts)" "PodCrashLooping"
    check_body "按 namespace 过滤告警" "$(request GET "/api/v1/alerts?namespace=${POD_NS}")" "$POD_NS"
    check_body "按 resourceType 过滤告警" "$(request GET '/api/v1/alerts?resourceType=Pod')" "Pod"
    check_body "告警详情" "$(request GET "/api/v1/alerts/${FP_POD}")" "$FP_POD"
fi

# ============================================================
# 5. 拓扑感知告警抑制
# ============================================================
section "5. 拓扑感知告警抑制"

if [ -z "$WEBHOOK_SECRET" ]; then
    skip_test "拓扑抑制测试" "未设置 MUTONG_WEBHOOK_SECRET"
elif [ -z "$SELECTED_NODE" ] || [ -z "$SELECTED_POD" ]; then
    skip_test "拓扑抑制测试" "需要 Node 和 Pod 资源"
else
    NODE_NAME="$(field_name "$SELECTED_NODE")"
    NODE_UID="$(field_uid "$SELECTED_NODE")"
    POD_NAME="$(field_name "$SELECTED_POD")"
    POD_NS="$(field_ns "$SELECTED_POD")"
    POD_UID="$(field_uid "$SELECTED_POD")"
    echo -e "${YELLOW}  使用 Node: ${NODE_NAME}, Pod: ${POD_NS}/${POD_NAME}${NC}"

    NODE_ALERT="$(post_webhook /api/v1/alerts/webhook \
        "$(alert_payload "$FP_NODE" NodeDiskPressure Node "$NODE_NAME" "" critical firing "$NODE_UID" "")")"
    check_body "节点告警接收" "$NODE_ALERT" "success"

    sleep 1
    POD_ON_NODE="$(post_webhook /api/v1/alerts/webhook \
        "$(alert_payload "$FP_POD_ON_NODE" PodNotReady Pod "$POD_NAME" "$POD_NS" warning firing "$POD_UID" "$NODE_NAME")")"
    check_body "同节点 Pod 告警接收" "$POD_ON_NODE" "success"

    check_code "抑制状态端点" "$(request GET /api/v1/alerts/suppression/status)" "200"
fi

# ============================================================
# 6. AI 诊断
# ============================================================
section "6. AI 诊断"

check_code "MCP 工具列表" "$(request GET /api/v1/diagnosis/tools)" "200"
check_body "MCP 工具内容" "$(request GET /api/v1/diagnosis/tools)" "query_topology\|get_active_alerts\|run_diagnosis"

if [ -z "$WEBHOOK_SECRET" ] || [ -z "$SELECTED_POD" ]; then
    skip_test "Pod 诊断" "需要 webhook 密钥与 Pod 资源"
else
    DIAG="$(request POST /api/v1/diagnosis/run "{\"fingerprint\":\"${FP_POD}\"}")"
    check_code "诊断执行" "$DIAG" "200"
    check_body "诊断结果字段" "$DIAG" "rootCauses\|root_cause\|summary"
    DIAG_SUMMARY="$(json_extract "$(body_of "$DIAG")" "d.get('summary', d.get('Summary', ''))")"
    echo -e "  ${BLUE}  诊断摘要: $(printf '%s' "$DIAG_SUMMARY" | head -c 200)${NC}"
fi

# ============================================================
# 7. 其他资源类型告警
# ============================================================
section "7. Deployment / Service / StatefulSet 告警"

if [ -z "$WEBHOOK_SECRET" ]; then
    skip_test "其他资源告警" "未设置 MUTONG_WEBHOOK_SECRET"
else
    if [ -n "$SELECTED_DEPLOY" ]; then
        D_NAME="$(field_name "$SELECTED_DEPLOY")"
        D_NS="$(field_ns "$SELECTED_DEPLOY")"
        D_UID="$(field_uid "$SELECTED_DEPLOY")"
        check_body "Deployment 告警接收" "$(post_webhook /api/v1/alerts/webhook \
            "$(alert_payload "$FP_DEPLOY" DeploymentReplicasUnavailable Deployment "$D_NAME" "$D_NS" error firing "$D_UID" "")")" "success"
        check_code "Deployment 诊断" "$(request POST /api/v1/diagnosis/run "{\"fingerprint\":\"${FP_DEPLOY}\"}")" "200"
    else
        skip_test "Deployment 告警测试" "集群中无 Deployment 资源"
    fi

    if [ -n "$SELECTED_SVC" ]; then
        S_NAME="$(field_name "$SELECTED_SVC")"
        S_NS="$(field_ns "$SELECTED_SVC")"
        S_UID="$(field_uid "$SELECTED_SVC")"
        check_body "Service 告警接收" "$(post_webhook /api/v1/alerts/webhook \
            "$(alert_payload "$FP_SVC" ServiceEndpointsUnavailable Service "$S_NAME" "$S_NS" warning firing "$S_UID" "")")" "success"
    else
        skip_test "Service 告警测试" "集群中无 Service 资源"
    fi

    if [ -n "$SELECTED_SS" ]; then
        SS_NAME="$(field_name "$SELECTED_SS")"
        SS_NS="$(field_ns "$SELECTED_SS")"
        SS_UID="$(field_uid "$SELECTED_SS")"
        check_body "StatefulSet 告警接收" "$(post_webhook /api/v1/alerts/webhook \
            "$(alert_payload "$FP_SS" StatefulSetReplicasNotReady StatefulSet "$SS_NAME" "$SS_NS" error firing "$SS_UID" "")")" "success"
    else
        skip_test "StatefulSet 告警测试" "集群中无 StatefulSet 资源"
    fi
fi

# ============================================================
# 8. 指标查询
# ============================================================
section "8. 指标查询"

if [ -z "$SELECTED_POD" ]; then
    skip_test "Pod 指标查询" "需要 Pod 资源"
else
    POD_NAME="$(field_name "$SELECTED_POD")"
    POD_NS="$(field_ns "$SELECTED_POD")"
    POD_METRICS="$(request GET "/api/v1/metrics/pod?namespace=${POD_NS}&pod=${POD_NAME}")"
    POD_CODE="$(code_of "$POD_METRICS")"
    if [ "$POD_CODE" = "200" ]; then
        pass "Pod 指标查询 (HTTP 200)"
    elif [ "$POD_CODE" = "503" ]; then
        skip_test "Pod 指标查询" "Prometheus 未配置 (503)"
    else
        fail "Pod 指标查询 (HTTP $POD_CODE)" "$(body_of "$POD_METRICS")"
    fi
fi

check_code "指标目录" "$(request GET /api/v1/monitoring/catalog)" "200"

# ============================================================
# 9. 告警恢复
# ============================================================
section "9. 告警恢复"

if [ -z "$WEBHOOK_SECRET" ] || [ -z "$SELECTED_POD" ]; then
    skip_test "告警恢复测试" "需要 webhook 密钥与 Pod 资源"
else
    POD_NAME="$(field_name "$SELECTED_POD")"
    POD_NS="$(field_ns "$SELECTED_POD")"
    POD_UID="$(field_uid "$SELECTED_POD")"
    RESOLVE="$(post_webhook /api/v1/alerts/webhook \
        "$(alert_payload "$FP_POD" PodCrashLooping Pod "$POD_NAME" "$POD_NS" warning resolved "$POD_UID" "")")"
    check_body "告警恢复接收" "$RESOLVE" "success"
fi

# ============================================================
# 汇总
# ============================================================
echo ""
echo -e "${BLUE}══════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}  测试汇总${NC}"
echo -e "${BLUE}══════════════════════════════════════════════════════${NC}"
echo -e "  总计: $((PASS + FAIL)) (执行)"
echo -e "  ${GREEN}通过: $PASS${NC}"
echo -e "  ${RED}失败: $FAIL${NC}"
echo -e "  ${CYAN}跳过: $SKIP${NC}"

if [ "$FAIL" -eq 0 ]; then
    echo -e "\n  ${GREEN}🎉 全部测试通过！${NC}"
    exit 0
else
    echo -e "\n  ${RED}⚠️  有 $FAIL 项失败，请检查日志${NC}"
    exit 1
fi
