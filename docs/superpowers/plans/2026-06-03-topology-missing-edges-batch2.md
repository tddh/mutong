# K8s 资源关系拓扑补充方案（第二批 · 修订版）

> 日期：2026-06-03（修订：三轮审查后） | 涉及文件：4 个 | 预估改动量：~160 行
> 分支：`feat/webhook-topology-relationships`

## 背景

第一批已补齐 `WebhookRefSvc`（WebhookConfiguration → Service）。

本批补齐 **4 种资源类型、6 条边**：

| 边类型 | 方向 | 引用方式 | 处理方法 |
|--------|------|---------|---------|
| `NpSelectsByLabel` | NetworkPolicy → Label | matchLabels | 强类型 + 辅助函数 |
| `NpSelectsNs` | NetworkPolicy → Namespace | namespaceSelector matchLabels | 强类型 + Label 反查 |
| `PodPrioClass` | Pod → PriorityClass | 名字引用 | 在 processPodRelationships 内追加 |
| `PodRuntimeClass` | Pod → RuntimeClass | 名字引用 | 在 processPodRelationships 内追加 |
| `VolAttachToNode` | VolumeAttachment → Node | 名字引用 | 强类型 |
| `VolAttachToPV` | VolumeAttachment → PV | 名字引用 | 强类型 |

**设计原则**：
- 能用强类型的用强类型（`networkingv1`、`scv1` 已导入），和现有代码保持一致
- 只有两种类型共用一个处理器时才用 unstructured（如 Webhook）
- NP 走 Label 不直接连 Pod，避免 Pod UID 频繁变化导致边抖动

---

## 一、K8s 源码数据结构

### 1.1 NetworkPolicy（`networking.k8s.io/v1`）

```go
// 已导入: networkingv1 "k8s.io/api/networking/v1"

type NetworkPolicy struct {
    metav1.TypeMeta
    metav1.ObjectMeta
    Spec NetworkPolicySpec
}

type NetworkPolicySpec struct {
    PodSelector metav1.LabelSelector              // 值类型，非指针
    Ingress     []NetworkPolicyIngressRule
    Egress      []NetworkPolicyEgressRule
    PolicyTypes []PolicyType
}

type NetworkPolicyIngressRule struct {
    Ports []NetworkPolicyPort
    From  []NetworkPolicyPeer    // json:"from"
}

type NetworkPolicyEgressRule struct {
    Ports []NetworkPolicyPort
    To    []NetworkPolicyPeer    // json:"to" ← 注意：egress 用 to 不是 from
}

type NetworkPolicyPeer struct {
    PodSelector       *metav1.LabelSelector   // 指针，可选
    NamespaceSelector *metav1.LabelSelector   // 指针，可选
    IPBlock           *IPBlock                // CIDR（非 K8s 资源，跳过）
}

type LabelSelector struct {
    MatchLabels      map[string]string             // ← 仅处理此字段
    MatchExpressions []LabelSelectorRequirement    // ← 已知限制：集合选择器(In/NotIn/Exists)无法用 Label(k=v) 节点表达
}
```

**matchExpressions 限制说明**：和 PDB 处理器一致，仅处理 `matchLabels`。`matchExpressions`（如 `key In [a,b]`）的语义无法用 `Label(key=value)` 节点表示，标记为已知限制。

### 1.2 Pod 引用字段

```go
// PodSpec 中已获取的相关字段（corev1 已有）
Pod.Spec.PriorityClassName  string   // 空字符串 = 无优先级类
Pod.Spec.RuntimeClassName   *string  // nil = 使用默认运行时
```

处理位置：`processPodRelationships`（第 540-550 行）末尾追加调用。

### 1.3 VolumeAttachment（`storage.k8s.io/v1`）

```go
// 已导入: scv1 "k8s.io/api/storage/v1"

type VolumeAttachment struct {
    metav1.TypeMeta
    metav1.ObjectMeta
    Spec   VolumeAttachmentSpec
    Status VolumeAttachmentStatus
}

type VolumeAttachmentSpec struct {
    Attacher string                    // CSI 驱动名（非 K8s 资源，不建边）
    Source   VolumeAttachmentSource
    NodeName string                    // → Node 名字引用
}

type VolumeAttachmentSource struct {
    PersistentVolumeName *string                       // → PV 名字引用（指针,可 nil）
    InlineVolumeSpec     *corev1.PersistentVolumeSpec  // 内嵌 spec,非引用
}
```

VolumeAttachment 是**非命名空间**资源（cluster-scoped），`lookupUID` 时 namespace 传 `""`。

---

## 二、Schema 定义（`scripts/schema.ngql`）

```ngql
-- =====================================================
-- EDGE: NetworkPolicy
-- =====================================================

CREATE EDGE IF NOT EXISTS NpSelectsByLabel () COMMENT = 'NP podSelector 选中 Label';
CREATE EDGE IF NOT EXISTS NpSelectsNs () COMMENT = 'NP namespaceSelector 选中 Namespace';

-- =====================================================
-- EDGE: Pod 调度相关
-- =====================================================

CREATE EDGE IF NOT EXISTS PodPrioClass () COMMENT = 'Pod 引用 PriorityClass';
CREATE EDGE IF NOT EXISTS PodRuntimeClass () COMMENT = 'Pod 引用 RuntimeClass';

-- =====================================================
-- EDGE: VolumeAttachment
-- =====================================================

CREATE EDGE IF NOT EXISTS VolAttachToNode () COMMENT = 'VolumeAttachment 挂载到 Node';
CREATE EDGE IF NOT EXISTS VolAttachToPV () COMMENT = 'VolumeAttachment 绑定 PV';
```

无属性边，统一用 `() VALUES ... -> ... :()` 格式。

---

## 三、关系处理函数（`services/k8sresource_relationships.go`）

### 3.1 NetworkPolicy（强类型，~60 行）

```go
// processNetworkPolicyRelationship 处理 NetworkPolicy 的关系：
//   - spec.podSelector.matchLabels         → NpSelectsByLabel (NP → Label)
//   - ingress.from[].podSelector           → NpSelectsByLabel
//   - egress.to[].podSelector              → NpSelectsByLabel
//   - ingress.from[].namespaceSelector     → NpSelectsNs (NP → Namespace, 通过 Label 反查)
//   - egress.to[].namespaceSelector        → NpSelectsNs
//   - ipBlock                              → 跳过（非 K8s 资源）
//   - matchExpressions                     → 已知限制，不处理
func (d *K8sResoureService) processNetworkPolicyRelationship(unstructuredObj *unstructured.Unstructured) {
	var np networkingv1.NetworkPolicy
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &np); err != nil {
		d.logger.Error("processNetworkPolicyRelationship", zap.Error(err))
		return
	}

	npUID := string(unstructuredObj.GetUID())

	d.CleanupOutgoingEdgesByType(npUID, "NpSelectsByLabel")
	d.CleanupOutgoingEdgesByType(npUID, "NpSelectsNs")

	// spec.podSelector.matchLabels → NpSelectsByLabel
	for k, v := range np.Spec.PodSelector.MatchLabels {
		labelUID := generateLabelUID(k, v)
		_ = d.insertEdge("NpSelectsByLabel", npUID, labelUID)
	}

	// ingress rules (from)
	for _, rule := range np.Spec.Ingress {
		for _, peer := range rule.From {
			d.processNetworkPolicyPeer(npUID, peer)
		}
	}

	// egress rules (to)
	for _, rule := range np.Spec.Egress {
		for _, peer := range rule.To {
			d.processNetworkPolicyPeer(npUID, peer)
		}
	}
}

// processNetworkPolicyPeer 处理单个 NetworkPolicyPeer：
//   - podSelector.matchLabels     → NpSelectsByLabel
//   - namespaceSelector.matchLabels → NpSelectsNs (Label 反查 → 过滤 Namespace)
//   - ipBlock                     → 跳过
func (d *K8sResoureService) processNetworkPolicyPeer(npUID string, peer networkingv1.NetworkPolicyPeer) {
	if peer.PodSelector != nil {
		for k, v := range peer.PodSelector.MatchLabels {
			labelUID := generateLabelUID(k, v)
			_ = d.insertEdge("NpSelectsByLabel", npUID, labelUID)
		}
	}

	if peer.NamespaceSelector != nil {
		for nsKey, nsValue := range peer.NamespaceSelector.MatchLabels {
			d.addNpNamespaceEdge(npUID, nsKey, nsValue)
		}
	}
}

// addNpNamespaceEdge 通过 Label 反查匹配的 Namespace 并建 NpSelectsNs 边。
// 复用 processLabelsRelationship 为 Namespace 节点创建的 BelongsToLabel 边，
// GO FROM labelUID OVER BelongsToLabel REVERSELY → 过滤 kind=="Namespace" → 建边。
func (d *K8sResoureService) addNpNamespaceEdge(npUID, labelKey, labelValue string) {
	labelUID := generateLabelUID(labelKey, labelValue)

	query := fmt.Sprintf(
		"GO FROM %s OVER BelongsToLabel REVERSELY YIELD dst(edge) AS resource_uid LIMIT 100;",
		strconv.Quote(labelUID))

	rows, err := d.executenGQL(query)
	if err != nil {
		d.logger.Error("addNpNamespaceEdge: query failed",
			zap.String("label", fmt.Sprintf("%s=%s", labelKey, labelValue)),
			zap.Error(err))
		return
	}

	for _, row := range rows {
		resourceUID := string(row.Values[0].GetSVal())

		verifyQuery := fmt.Sprintf(
			"FETCH PROP ON K8sResource %s YIELD properties(vertex).kind AS kind;",
			strconv.Quote(resourceUID))
		verifyRows, err := d.executenGQL(verifyQuery)
		if err != nil || len(verifyRows) == 0 {
			continue
		}
		if string(verifyRows[0].Values[0].GetSVal()) == "Namespace" {
			_ = d.insertEdge("NpSelectsNs", npUID, resourceUID)
		}
	}
}
```

### 3.2 Pod → PriorityClass / RuntimeClass（在现有函数内追加，~25 行）

**改动位置**：`processPodRelationships`（第 540 行），在 `processVolumeRelationships` 之后追加：

```go
func (d *K8sResoureService) processPodRelationships(unstructuredObj *unstructured.Unstructured) {
	var pod corev1.Pod
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &pod); err != nil {
		d.logger.Error("Error converting to Pod", zap.Error(err))
		return
	}

	d.processServiceAccountRelationship(unstructuredObj, &pod)
	d.processNodeRelationship(unstructuredObj, &pod)
	d.processVolumeRelationships(&pod)
	d.processPriorityClassRelationship(&pod)      // 新增
	d.processRuntimeClassRelationship(&pod)        // 新增
}

// processPriorityClassRelationship 建立 Pod → PriorityClass 边
func (d *K8sResoureService) processPriorityClassRelationship(pod *corev1.Pod) {
	if pod.Spec.PriorityClassName == "" {
		return
	}
	// PriorityClass 是集群级资源，namespace 为空
	if uid, found := d.lookupUID("PriorityClass", "", pod.Spec.PriorityClassName); found {
		_ = d.insertEdge("PodPrioClass", string(pod.UID), uid)
	}
}

// processRuntimeClassRelationship 建立 Pod → RuntimeClass 边
func (d *K8sResoureService) processRuntimeClassRelationship(pod *corev1.Pod) {
	if pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName == "" {
		return
	}
	// RuntimeClass 是集群级资源，namespace 为空
	if uid, found := d.lookupUID("RuntimeClass", "", *pod.Spec.RuntimeClassName); found {
		_ = d.insertEdge("PodRuntimeClass", string(pod.UID), uid)
	}
}
```

**不需要新增 import**：`corev1.Pod` 已在现有 import 中，只需读取 `PriorityClassName`（string）和 `RuntimeClassName`（*string）。

### 3.3 VolumeAttachment（强类型，~25 行）

```go
// processVolumeAttachmentRelationship 处理 VolumeAttachment 的关系：
//   - spec.nodeName                    → VolAttachToNode (VA → Node)
//   - spec.source.persistentVolumeName  → VolAttachToPV  (VA → PV)
//   - spec.source.inlineVolumeSpec      → 跳过（内嵌 spec，非引用）
//   - spec.attacher                     → 跳过（CSI 驱动名，非 K8s 资源）
func (d *K8sResoureService) processVolumeAttachmentRelationship(unstructuredObj *unstructured.Unstructured) {
	var va scv1.VolumeAttachment
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &va); err != nil {
		d.logger.Error("processVolumeAttachmentRelationship", zap.Error(err))
		return
	}

	vaUID := string(unstructuredObj.GetUID())

	d.CleanupOutgoingEdgesByType(vaUID, "VolAttachToNode")
	d.CleanupOutgoingEdgesByType(vaUID, "VolAttachToPV")

	// VA → Node（集群级资源）
	if va.Spec.NodeName != "" {
		if uid, found := d.lookupUID("Node", "", va.Spec.NodeName); found {
			_ = d.insertEdge("VolAttachToNode", vaUID, uid)
		}
	}

	// VA → PV（PersistentVolumeName 是指针,可能为空）
	if va.Spec.Source.PersistentVolumeName != nil && *va.Spec.Source.PersistentVolumeName != "" {
		if uid, found := d.lookupUID("PersistentVolume", "", *va.Spec.Source.PersistentVolumeName); found {
			_ = d.insertEdge("VolAttachToPV", vaUID, uid)
		}
	}
}
```

**不需要新增 import**：`scv1 "k8s.io/api/storage/v1"` 已导入，`scv1.VolumeAttachment` 可用。

---

## 四、调度分发（`services/k8sresource_collector.go`）

在 `Relationship()` 函数中，于现有 `if` 分支之后新增 2 个分支：

```go
// 在 processSecretTokenRelationship 之后新增
if k8sResource.Kind == "NetworkPolicy" {
    d.processNetworkPolicyRelationship(unstructuredObj)
}
if k8sResource.Kind == "VolumeAttachment" {
    d.processVolumeAttachmentRelationship(unstructuredObj)
}
```

PriorityClass 和 RuntimeClass **不需要**独立分支 — 它们在 `processPodRelationships` 内部被调用，Pod 分支已存在。

---

## 五、文档更新（`docs/ngql.md`）

```markdown
| **网络** | NpSelectsByLabel | NetworkPolicy → Label | NP podSelector 选中标签 |
| | NpSelectsNs | NetworkPolicy → Namespace | NP namespaceSelector 选中命名空间 |
| **调度** | PodPrioClass | Pod → PriorityClass | Pod 引用优先级类 |
| | PodRuntimeClass | Pod → RuntimeClass | Pod 引用运行时类 |
| **存储** | VolAttachToNode | VolumeAttachment → Node | 卷挂载到节点 |
| | VolAttachToPV | VolumeAttachment → PV | 卷挂载引用 PV |
```

---

## 六、测试用例设计

| 测试函数 | 覆盖场景 |
|---------|---------|
| `TestProcessNP_PodSelectorMatchLabels` | spec.podSelector.matchLabels → NpSelectsByLabel |
| `TestProcessNP_NamespaceSelector` | namespaceSelector.matchLabels → NpSelectsNs |
| `TestProcessNP_IngressPodSelector` | ingress.from[].podSelector → NpSelectsByLabel |
| `TestProcessNP_EgressToNamespaceSelector` | egress.to[].namespaceSelector → NpSelectsNs |
| `TestProcessNP_IPBlockSkipped` | ipBlock 不产生边 |
| `TestProcessNP_EmptySelectors` | 无 selector 不产生边（空 matchLabels） |
| `TestProcessNP_MixedPeers` | 同规则中 podSelector + namespaceSelector 共存 |
| `TestProcessPrioClass_HasClass` | priorityClassName 非空 → PodPrioClass |
| `TestProcessPrioClass_EmptyClass` | priorityClassName 为空 → 无调用 |
| `TestProcessRuntimeClass_HasClass` | runtimeClassName 非空 → PodRuntimeClass |
| `TestProcessRuntimeClass_NilClass` | runtimeClassName 为 nil → 无调用 |
| `TestProcessVA_BothEdges` | nodeName + persistentVolumeName 都非空 |
| `TestProcessVA_PVNameNil` | persistentVolumeName 为 nil → 只建 VolAttachToNode |
| `TestProcessVA_NodeNameEmpty` | nodeName 为空 → 只建 VolAttachToPV |

---

## 七、改动清单

| 文件 | 改动量 | 新增函数 |
|------|:---:|------|
| `scripts/schema.ngql` | +14 行（6 条 CREATE EDGE） | — |
| `services/k8sresource_relationships.go` | +120 行 | `processNetworkPolicyRelationship`, `processNetworkPolicyPeer`, `addNpNamespaceEdge`, `processPriorityClassRelationship`, `processRuntimeClassRelationship`, `processVolumeAttachmentRelationship` |
| `services/k8sresource_collector.go` | +6 行（2 个新分支） | — |
| `docs/ngql.md` | +6 行 | — |
| `services/k8sresource_relationships_test.go` | +180 行（14 个测试） | — |
| **合计** | **~325 行** | — |

---

## 八、已知限制

| 限制 | 说明 | 影响 |
|------|------|:---:|
| NP `matchExpressions` 不处理 | `LabelSelector.matchExpressions` 使用集合语义(In/NotIn/Exists)，无法用 Label(k=v) 节点表达 | 低 — 绝大多数 NP 用 matchLabels |
| 只处理 `matchLabels` | 和 PDB 处理器一致 | 低 |
| NP Label 反查有 LIMIT 100 | `addNpNamespaceEdge` 查询限 100 条 | 低 — 单标签匹配的 Namespace 数通常 < 10 |
| VA `inlineVolumeSpec` 不处理 | CSI Migration 场景的内嵌 PV spec，非资源引用 | 极低 |

---

## 九、执行步骤

```
Step 1: scripts/schema.ngql — 新增 6 条 CREATE EDGE
        ↓ just init-nebula

Step 2: services/k8sresource_relationships.go
        - 新增 processNetworkPolicyRelationship + processNetworkPolicyPeer + addNpNamespaceEdge
        - 修改 processPodRelationships：追加 2 个调用
        - 新增 processPriorityClassRelationship + processRuntimeClassRelationship
        - 新增 processVolumeAttachmentRelationship

Step 3: services/k8sresource_collector.go
        - Relationship() 新增 NetworkPolicy + VolumeAttachment 分支

Step 4: docs/ngql.md — 更新 EDGE 表

Step 5: services/k8sresource_relationships_test.go — 补充 14 个测试用例

Step 6: just test + 编译验证
```
