# NebulaGraph Schema 参考与查询示例

> 可执行 DDL 脚本：`scripts/schema.ngql`（`just init-nebula` 自动执行）。
> 本文档提供 Schema 结构说明和常用 nGQL 查询参考。

---

## 一、Schema 概览

### 1.1 图空间

| 参数 | 值 |
|------|-----|
| 名称 | `mutong` |
| 分区数 | 3 |
| 副本数 | 2 |
| VID 类型 | FIXED_STRING(128) |

### 1.2 TAG

| TAG | 说明 | 属性 |
|-----|------|------|
| **K8sResource** | K8s 资源实体 | uid, name, kind, api_version, api_group, name_space, labels, resource_define, cluster, is_deleted, deleted_at |
| **Label** | K8s 标签 | uid, key, value |
| **BusinessApp** | 业务应用实体 | uid, app_name, namespace, criticality, environment, team, business_unit, owner_name, owner_kind |

### 1.3 EDGE（按类别）

| 类别 | 边类型 | 方向 | 说明 |
|------|--------|------|------|
| **归属** | OwnedBy | K8sResource → K8sResource | OwnerReferences 管理关系（含 owner_kind/uid/name 元信息） |
| | BelongsTo | K8sResource → Namespace | 命名空间归属 |
| **调度** | RunsOn | Pod → Node | Pod 调度节点 |
| | MountsConfig | Pod → ConfigMap | 配置挂载 |
| | MountsSecret | Pod → Secret | 密钥挂载 |
| | MountsPVC | Pod → PVC | 持久卷挂载 |
| | PodPrioClass | Pod → PriorityClass | Pod 引用优先级类 |
| | PodRuntimeClass | Pod → RuntimeClass | Pod 引用运行时类 |
| **存储** | BoundToPV | PVC → PV | 卷绑定 |
| | BelongsToStorageClass | PV → StorageClass | 存储类归属 |
| | ScRefCSIDriver | StorageClass → CSIDriver | CSI 驱动引用 |
| | RegisteredON | CSINode → Node | CSI 节点注册 |
| **服务** | SvcToEp | Service → Endpoint | 服务端点 |
| | EpToPods | Endpoint → Pod | 端点指向 Pod |
| | SvcToPods | Service → Pod | 服务直连 Pod |
| | EpSliceToPods | EndpointSlice → Pod | EndpointSlice 指向 |
| **Ingress** | BelongsToIngressClass | Ingress → IngressClass | IngressClass 绑定 |
| | RoutesToSvc | Ingress → Service | 路由目标 |
| | UsesTLS | Ingress → Secret | TLS 证书 |
| | Uses | APIService → Service | 聚合型 APIService 的后端 Service |
| **RBAC** | ServiceAccount | Pod → ServiceAccount | SA 挂载 |
| | BelongsToClusterRole | ClusterRoleBinding → ClusterRole | 集群角色绑定 |
| | BelongsToRole | RoleBinding → Role | 角色绑定 |
| | BelongsToUser | RoleBinding → User | 用户绑定 |
| | BelongsToGroup | RoleBinding → Group | 组绑定 |
| | BelongsToServiceAccount | RoleBinding → SA | SA 绑定 |
| **PDB** | PdbToPod | PDB → Pod | 中断预算关联 |
| **Webhook** | WebhookRefSvc | WebhookConfiguration → Service | Webhook 引用 Service（含 webhook_name/path/port 元信息） |
| **网络** | NpSelectsByLabel | NetworkPolicy → Label | NP podSelector 选中标签 |
| | NpSelectsNs | NetworkPolicy → Namespace | NP namespaceSelector 选中命名空间 |
| **存储** | VolAttachToNode | VolumeAttachment → Node | 卷挂载到节点 |
| | VolAttachToPV | VolumeAttachment → PV | 卷挂载引用 PV |
| **事件** | Events | K8sResource → Event | K8s 事件（TTL 24h） |
| **标签** | BelongsToLabel | K8sResource → Label | 标签关联 |
| **业务** | BelongsToApp | K8sResource → BusinessApp | 资源归属业务 |
| | CallsApp | BusinessApp → BusinessApp | 应用间调用 |
| | AutoScales | HPA → ScaleTarget | HPA 扩缩容（含 min/max/current replicas） |
| **元数据/APF/CNI** | ReferencesPriorityLevel | FlowSchema → PriorityLevelConfiguration | APF：FlowSchema 引用的优先级（FlowSchema→SA/User/Group 复用上面 RBAC 的 BelongsTo*）|
| | CiliumEpToPod | CiliumEndpoint → Pod | Cilium 端点对应 Pod（同名同 ns）|
| | CiliumEpHasIdentity | CiliumEndpoint → CiliumIdentity | Cilium 端点使用的安全身份（status.identity.id）|
| | DefinesResource | CustomResourceDefinition → K8sResource | CRD 定义的 CR 实例（按 group+kind 匹配）|
| | ServesCRD | APIService → CustomResourceDefinition | CRD 支撑型 APIService 服务的 API group 下的 CRD（同 spec.group）|

> **集群级资源的拓扑连通**：CRD / APIService / FlowSchema / PriorityLevelConfiguration / Cilium* 等集群级资源既无 ownerReferences 也无 namespace，通用的 OwnedBy/BelongsTo 边对它们落空，此前在拓扑图里表现为孤立点（点进去只有自己）。上面「元数据/APF/CNI」类边（外加 FlowSchema→subjects 复用 RBAC 的 BelongsTo*）按各自 `resource_define` 里的真实引用把它们接入关系图。
> **仍天然孤立**（无关系可建，属正常）：Local 型 APIService（内置 group，`spec.service` 为 null 且无对应 CRD）、未被任何 Binding 引用的 ClusterRole、既无存活 CR 实例又无同 group APIService 的 CRD。
> **存量回填**：`POST /api/v1/stats/backfill-relationships` 对 FlowSchema/CiliumEndpoint/CustomResourceDefinition/APIService 重跑 `Relationship`（幂等）；新部署重启时 informer relist 也会对这些资源重新建边。

### 1.4 索引

| TAG | 索引名 | 字段 |
|-----|--------|------|
| K8sResource | `name_space` | name_space(128) |
| | `kind_name_space` | kind(128), name_space(128) |
| | `kind_name` | kind(128), name(128) |
| | `kind_name_name_space` | kind(128), name(128), name_space(128) |
| | `is_deleted_idx` | is_deleted |
| | `kind_is_deleted_idx` | kind(128), is_deleted |
| Label | `label_key` | key(128) |
| | `label_value` | value(128) |
| | `label_key_value` | key(128), value(128) |
| BusinessApp | `bizapp_name` | app_name(128) |
| | `bizapp_ns` | namespace(128) |
| | `bizapp_owner_name` | owner_name(128) |
| OwnedBy | `owner_id` | owner_uid(128) |
| | `owner_kind` | owner_kind(128) |
| | `owner_name` | owner_name(128) |

---

## 二、常用查询示例

### 2.1 关系遍历

```ngql
-- 查询资源拓扑：从 Pod 出发，经 Ownership 链 → BelongsTo → RunsOn
MATCH (n:K8sResource)<-[ne:BelongsTo]-(d:K8sResource)<-[de:OwnedBy*]-(v:K8sResource)-[e:RunsOn]->(k:K8sResource)
RETURN n.K8sResource.name, d.K8sResource.name, d.K8sResource.kind,
       k.K8sResource.name, k.K8sResource.kind, e, ne, de;

-- 按 UID 查询资源关联关系
MATCH (n:K8sResource{uid:"588de9a5-e71b-4dee-b638-a02c8736ba30"})-[ne]-(d:K8sResource)
RETURN src(ne) as s_uid, dst(ne) as d_uid, type(ne) as edge_type;

-- GO 语句：指定方向遍历
GO FROM "0aa1472c-1ce3-4472-a29d-64ff65e7145b" OVER BelongsTo
    WHERE dst(edge) == "0aa1472c-1ce3-4472-a29d-64ff65e7145b"
       OR src(EDGE) == "0aa1472c-1ce3-4472-a29d-64ff65e7145b"
    YIELD src(edge) AS src, dst(edge) AS dst, rank(edge) AS rank, type(EDGE) as edge_type;
```

### 2.2 特定关系查询

```ngql
-- 获取 Pod 挂载的 ConfigMap
MATCH (v:K8sResource)-[e:MountsConfig]->(c:K8sResource)
RETURN v.K8sResource.name, c.K8sResource.name, c.K8sResource.kind, e;

-- 获取 Job 的 Pod
MATCH (v:K8sResource{kind:"Job"})-[e]-(k:K8sResource{kind:"Pod"})
RETURN v.K8sResource.name, v.K8sResource.kind, type(e),
       k.K8sResource.name, k.K8sResource.kind;

-- 获取 CronJob 的 Pod 事件链
MATCH (cronjob:K8sResource{kind:"CronJob"})-[ce:OwnedBy]-(v:K8sResource{kind:"Job"})
      -[e:OwnedBy]-(k:K8sResource{kind:"Pod"})-[ev:Events]-(r:K8sResource)
RETURN ev.reason, datetime(ev.eventTime),
       cronjob.K8sResource.name, v.K8sResource.name, k.K8sResource.name,
       type(ce), type(e), type(ev);
```

### 2.3 Rank 与子图

```ngql
-- 按 rank 排序获取最大 rank 的边
MATCH (v:K8sResource{kind:"Pod", is_deleted: false})-[e]->(k:K8sResource)
WITH v, MAX(rank(e)) AS max_rank
MATCH (v)-[e]->(k) WHERE rank(e) == max_rank
RETURN v, k, e;

-- 获取子图（4 步深度）
GET SUBGRAPH 4 STEPS FROM "e0614425-93a4-43d1-b153-431aa13c4b8b"
YIELD VERTICES AS nodes, EDGES AS relationships;
```

### 2.4 运维

```ngql
-- 清理超过 20s 的 session（替换 IP 后执行）
SHOW SESSIONS
| YIELD $-.SessionId AS sid, timestamp($-.UpdateTime) AS ut, $-.ClientIp AS ip
| RETURN $-.sid AS sid, (now() - $-.ut) AS del, $-.ip AS ip
| YIELD $-.sid AS sid WHERE $-.ip == "192.168.1.x"
| KILL SESSION $-.sid;
```
