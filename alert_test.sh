#!/bin/bash

# 告警收敛系统测试脚本

BASE_URL="http://localhost:8888"

echo "=========================================="
echo "告警收敛系统测试"
echo "=========================================="

# 1. 健康检查
echo ""
echo "1. 测试健康检查..."
HEALTH=$(curl -s ${BASE_URL}/api/v1/alerts/health)
echo "   返回: $HEALTH"
if echo "$HEALTH" | grep -q "healthy"; then
    echo "   ✅ 健康检查通过"
else
    echo "   ❌ 健康检查失败"
    exit 1
fi

# 2. 发送测试告警
echo ""
echo "2. 发送测试告警..."
RESPONSE=$(curl -s -X POST ${BASE_URL}/api/v1/alerts/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "version": "4",
    "groupKey": "test-group",
    "status": "firing",
    "receiver": "test",
    "groupLabels": {
      "alertname": "TestAlert"
    },
    "commonLabels": {
      "alertname": "TestAlert",
      "severity": "warning"
    },
    "alerts": [
      {
        "status": "firing",
        "labels": {
          "alertname": "PodCrashLooping",
          "namespace": "default",
          "pod": "test-pod",
          "severity": "warning"
        },
        "annotations": {
          "summary": "Test pod is crash looping",
          "description": "Pod test-pod has restarted 5 times"
        },
        "startsAt": "2026-04-02T10:00:00Z",
        "endsAt": "0001-01-01T00:00:00Z",
        "generatorURL": "http://prometheus:9090/graph",
        "fingerprint": "test-fingerprint-001"
      }
    ]
  }')
echo "   返回: $RESPONSE"
if echo "$RESPONSE" | grep -q "success"; then
    echo "   ✅ 告警发送成功"
else
    echo "   ❌ 告警发送失败"
fi

# 3. 查看活跃告警
echo ""
echo "3. 查看活跃告警..."
ALERTS=$(curl -s ${BASE_URL}/api/v1/alerts)
echo "   返回: $ALERTS" | head -c 500
echo ""
if echo "$ALERTS" | grep -q "PodCrashLooping"; then
    echo "   ✅ 告警已存储"
else
    echo "   ❌ 告警未找到"
fi

# 4. 发送节点告警（测试拓扑抑制）
echo ""
echo "4. 发送节点告警..."
RESPONSE=$(curl -s -X POST ${BASE_URL}/api/v1/alerts/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "version": "4",
    "groupKey": "node-group",
    "status": "firing",
    "receiver": "test",
    "groupLabels": {
      "alertname": "NodeNotReady"
    },
    "commonLabels": {
      "alertname": "NodeNotReady",
      "severity": "critical"
    },
    "alerts": [
      {
        "status": "firing",
        "labels": {
          "alertname": "NodeNotReady",
          "node": "test-node-1",
          "severity": "critical"
        },
        "annotations": {
          "summary": "Node test-node-1 is not ready"
        },
        "startsAt": "2026-04-02T10:00:00Z",
        "endsAt": "0001-01-01T00:00:00Z",
        "generatorURL": "http://prometheus:9090/graph",
        "fingerprint": "test-fingerprint-002"
      }
    ]
  }')
echo "   返回: $RESPONSE"

# 5. 发送该节点上的 Pod 告警（应被抑制）
echo ""
echo "5. 发送被抑制的 Pod 告警..."
RESPONSE=$(curl -s -X POST ${BASE_URL}/api/v1/alerts/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "version": "4",
    "groupKey": "pod-group",
    "status": "firing",
    "receiver": "test",
    "groupLabels": {
      "alertname": "PodNotReady"
    },
    "commonLabels": {
      "alertname": "PodNotReady",
      "severity": "warning"
    },
    "alerts": [
      {
        "status": "firing",
        "labels": {
          "alertname": "PodNotReady",
          "namespace": "default",
          "pod": "test-pod-on-node-1",
          "node": "test-node-1",
          "severity": "warning"
        },
        "annotations": {
          "summary": "Pod test-pod-on-node-1 is not ready"
        },
        "startsAt": "2026-04-02T10:01:00Z",
        "endsAt": "0001-01-01T00:00:00Z",
        "generatorURL": "http://prometheus:9090/graph",
        "fingerprint": "test-fingerprint-003"
      }
    ]
  }')
echo "   返回: $RESPONSE"

# 6. 发送告警恢复
echo ""
echo "6. 发送告警恢复..."
RESPONSE=$(curl -s -X POST ${BASE_URL}/api/v1/alerts/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "version": "4",
    "groupKey": "test-group",
    "status": "resolved",
    "receiver": "test",
    "groupLabels": {
      "alertname": "TestAlert"
    },
    "alerts": [
      {
        "status": "resolved",
        "labels": {
          "alertname": "PodCrashLooping",
          "namespace": "default",
          "pod": "test-pod",
          "severity": "warning"
        },
        "annotations": {
          "summary": "Test pod is crash looping",
          "description": "Pod test-pod has restarted 5 times"
        },
        "startsAt": "2026-04-02T10:00:00Z",
        "endsAt": "2026-04-02T10:05:00Z",
        "generatorURL": "http://prometheus:9090/graph",
        "fingerprint": "test-fingerprint-001"
      }
    ]
  }')
echo "   返回: $RESPONSE"

echo ""
echo "=========================================="
echo "测试完成"
echo "=========================================="