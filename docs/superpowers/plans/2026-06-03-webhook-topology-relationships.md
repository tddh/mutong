# Webhook 资源关系拓扑实现方案

> 日期：2026-06-03 | 涉及文件：3 个 | 预估改动量：~40 行

## 背景

`MutatingWebhookConfiguration` 和 `ValidatingWebhookConfiguration` 的 `clientConfig.service` 字段直接引用集群内 Service，但在当前 `services/k8sresource_relationships.go` 中：
- `processMutatingWebhookConfigurationRelationship` 是空 stub（只有 `var _ admissionv1.MutatingWebhookConfiguration` 占位）
- `ValidatingWebhookConfiguration` 完全没有专用处理器

导致 Webhook → Service 的核心引用关系完全丢失。

## K8s 源码数据结构

两个类型共享相同的引用模式（`k8s.io/api/admissionregistration/v1`）：

```
MutatingWebhookConfiguration          ValidatingWebhookConfiguration
  └─ Webhooks []MutatingWebhook        └─ Webhooks []ValidatingWebhook
       └─ ClientConfig                      └─ ClientConfig
            ├─ URL *string (外部署)               ├─ URL *string (外部署)
            ├─ Service *ServiceReference  ← 关键    ├─ Service *ServiceReference  ← 关键
            │    ├─ Namespace string (空=默认本ns)   │    ├─ Namespace string
            │    ├─ Name string (必填)              │    ├─ Name string
            │    ├─ Path *string                   │    ├─ Path *string
            │    └─ Port *int32                    │    └─ Port *int32
            └─ CABundle []byte (内嵌,非引用)         └─ CABundle []byte (内嵌,非引用)
```

**结论**：
- 两者结构对称，用统一函数处理
- `clientConfig.service` 是唯一直接资源引用
- `CABundle` 是内嵌字节，不引用 Secret

## 改动清单

### 文件一：`scripts/schema.ngql`

在 Ingress 段附近新增边类型定义：

```ngql
-- =====================================================
-- EDGE: Webhook
-- =====================================================

CREATE EDGE IF NOT EXISTS WebhookRefSvc (
    webhook_name string,
    path string,
    port int
) COMMENT = 'Webhook 配置引用 Service';
```

### 文件二：`services/k8sresource_relationships.go`

**改动 1**：替换现有空 stub（第 89-91 行）

```go
// processWebhookConfigurationRelationship 统一处理
// MutatingWebhookConfiguration 和 ValidatingWebhookConfiguration
// 遍历所有 webhooks，建立 WebhookRefSvc 边指向 clientConfig.service
func (d *K8sResoureService) processWebhookConfigurationRelationship(unstructuredObj *unstructured.Unstructured) {
	obj := unstructuredObj.Object
	webhooksRaw, ok := obj["webhooks"]
	if !ok {
		return
	}
	webhooks, ok := webhooksRaw.([]interface{})
	if !ok || len(webhooks) == 0 {
		return
	}

	d.CleanupOutgoingEdgesByType(string(unstructuredObj.GetUID()), "WebhookRefSvc")

	defaultNS := unstructuredObj.GetNamespace()

	for _, whRaw := range webhooks {
		wh, ok := whRaw.(map[string]interface{})
		if !ok {
			continue
		}

		whName, _ := wh["name"].(string)

		ccRaw, ok := wh["clientConfig"].(map[string]interface{})
		if !ok {
			continue
		}

		svcRaw, ok := ccRaw["service"].(map[string]interface{})
		if !ok {
			continue // url 模式
		}

		svcName, _ := svcRaw["name"].(string)
		if svcName == "" {
			continue
		}

		svcNS, _ := svcRaw["namespace"].(string)
		if svcNS == "" {
			svcNS = defaultNS
		}

		svcUID, found := d.lookupUID("Service", svcNS, svcName)
		if !found {
			d.logger.Debug("processWebhookConfigurationRelationship: Service not found",
				zap.String("webhook", whName),
				zap.String("serviceNS", svcNS),
				zap.String("serviceName", svcName))
			continue
		}

		path := ""
		if p, ok := svcRaw["path"].(string); ok {
			path = p
		}
		port := int32(0)
		if p, ok := svcRaw["port"].(float64); ok {
			port = int32(p)
		}

		query := fmt.Sprintf(
			"INSERT EDGE WebhookRefSvc(webhook_name, path, port) VALUES %s -> %s:(%s, %s, %d);",
			strconv.Quote(string(unstructuredObj.GetUID())),
			strconv.Quote(svcUID),
			strconv.Quote(whName),
			strconv.Quote(path),
			port,
		)
		d.logger.Debug("processWebhookConfigurationRelationship",
			zap.String("nGQL", query))
		_, err := d.graphDB.Execute(query)
		if err != nil {
			d.logger.Error("Failed to insert WebhookRefSvc edge", zap.Error(err))
		}
	}
}

func (d *K8sResoureService) processMutatingWebhookConfigurationRelationship(unstructuredObj *unstructured.Unstructured) {
	d.processWebhookConfigurationRelationship(unstructuredObj)
}

func (d *K8sResoureService) processValidatingWebhookConfigurationRelationship(unstructuredObj *unstructured.Unstructured) {
	d.processWebhookConfigurationRelationship(unstructuredObj)
}
```

> **为什么用 unstructured 而不用强类型？** `MutatingWebhook` 和 `ValidatingWebhook` 是不同 Go 类型（虽然结构相同），无法用一个函数同时 unmarshal。直接用 `unstructured` 访问嵌套 map 避免了类型分叉。

### 文件三：`services/k8sresource_collector.go`

在 `Relationship()` 中新增 `ValidatingWebhookConfiguration` 分支（`MutatingWebhookConfiguration` 分支已存在，调用替换为有效实现）：

```go
// 在 processSecretTokenRelationship 之后新增
if k8sResource.Kind == "MutatingWebhookConfiguration" {
    d.processMutatingWebhookConfigurationRelationship(unstructuredObj)
}
if k8sResource.Kind == "ValidatingWebhookConfiguration" {
    d.processValidatingWebhookConfigurationRelationship(unstructuredObj)
}
```

### 文件四：`docs/ngql.md`

在 EDGE 表中新增一行：

```
| WebhookRefSvc | WebhookConfiguration → Service | Webhook 引用 Service（含 webhook_name/path/port 元信息） |
```

## 执行步骤

```
Step 1: scripts/schema.ngql — 新增 CREATE EDGE WebhookRefSvc
        ↓ just init-nebula (或手动执行 nGQL)

Step 2: services/k8sresource_relationships.go
        - 新增 processWebhookConfigurationRelationship (+~50 行)
        - 替换 processMutatingWebhookConfigurationRelationship 空 stub
        - 新增 processValidatingWebhookConfigurationRelationship

Step 3: services/k8sresource_collector.go
        - Relationship() 新增 ValidatingWebhookConfiguration 分支
        - MutatingWebhookConfiguration 分支调用已存在，替换为有效实现

Step 4: docs/ngql.md — 更新 EDGE 表

Step 5: just test (确保编译通过 + 现有测试不受影响)
```

## 关系示意

```
MutatingWebhookConfiguration ──WebhookRefSvc──▶ Service (k8s-api)
                         └─────────┘ webhook_name="pod-mutator"
                                    path="/mutate"
                                    port=443

ValidatingWebhookConfiguration ──WebhookRefSvc──▶ Service (policy-svc)
                           └─────────┘ webhook_name="policy-check"
                                      path="/validate"
                                      port=8443
```

## 工作量

| 文件 | 改动量 | 难度 |
|------|:---:|:---:|
| `scripts/schema.ngql` | +3 行 | 低 |
| `services/k8sresource_relationships.go` | ~+55 行 / -3 行 | 低 |
| `services/k8sresource_collector.go` | +3 行 | 低 |
| `docs/ngql.md` | +1 行 | 低 |
| **合计** | **~60 行** | — |
