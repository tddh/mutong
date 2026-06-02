# 业务拓扑组件部署

## 组件说明

| 组件 | 用途 | Chart 仓库 |
|------|------|-----------|
| Beyla (OBI) | eBPF 零代码插桩，采集 HTTP/gRPC 调用 | `https://grafana.github.io/helm-charts` |
| OTel Collector | 接收 OTLP Spans，批量导出到 Kafka | `https://open-telemetry.github.io/opentelemetry-helm-charts` |

## 在线安装

```bash
# 添加仓库
helm repo add grafana https://grafana.github.io/helm-charts
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo update

# 安装
helm install beyla grafana/beyla -n beyla --create-namespace -f helm-beyla-values.yaml
helm install otel-collector open-telemetry/opentelemetry-collector -n otel-collector --create-namespace -f helm-otelcol-values.yaml
```

## 离线安装

```bash
# 1. 在有网络的机器上下载 Chart 包
helm repo add grafana https://grafana.github.io/helm-charts
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo update
helm pull grafana/beyla
helm pull open-telemetry/opentelemetry-collector

# 2. 将 .tgz 文件和 values 文件传输到目标机器

# 3. 本地安装
helm install beyla ./beyla-*.tgz -n beyla --create-namespace -f helm-beyla-values.yaml
helm install otel-collector ./opentelemetry-collector-*.tgz -n otel-collector --create-namespace -f helm-otelcol-values.yaml
```

## 验证

```bash
# 查看 Pod 状态
kubectl get pods -n beyla
kubectl get pods -n otel-collector

# 查看 OTel Collector 日志
kubectl logs -n otel-collector -l app.kubernetes.io/name=opentelemetry-collector --tail=100

# 验证 Kafka Topic 有数据
kafka-consumer-groups.sh --bootstrap-server kafka:9092 --describe --group mutong-trace-topology
```

## 配置调整

- `helm-beyla-values.yaml` — 修改 Namespace 白名单、忽略路径
- `helm-otelcol-values.yaml` — 修改 Kafka brokers、Topic 名称
