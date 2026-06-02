# 重明 (Mutong) 9 项改进方案

> **目标**：针对当前代码库中 9 个核心不足，给出具体、可执行的改进方案。
> **设计原则**：最小改动、复用现有架构、避免过度设计、每项方案独立可交付。

---

## 优先级总览

| 优先级 | 问题 | 预计工作量 | 依赖 |
|:---:|------|:---:|------|
| P0 | 1. 硬编码导出 | 1-2 天 | 无 |
| P0 | 2. 执行器操作类型扩展 | 3-4 天 | 问题 1（执行器配置段） |
| P0 | 3. 外部告警与业务关联 | 4-5 天 | 无 |
| P0 | 5. 用户管理体系 | 5-7 天 | 无 |
| P1 | 4. 巡检规则页面配置 | 3-4 天 | 问题 5（鉴权） |
| P1 | 7. CLI 增强 | 3-4 天 | 无 |
| P1 | 8. 业务拓扑清理 | 2-3 天 | 无 |
| P1 | 9. ES 日志索引映射 | 2-3 天 | 无 |
| P2 | 6. 业务属性与资源属性扩展 | 3-4 天 | 问题 3/5/8 |

---

## 问题 1：硬编码导出 ok

### 现状

代码中存在约 86 个硬编码值，分布在 `cmd/main.go`、各 `services/` 文件、`controllers/` 中。`config_base.go` 中已有配置结构体覆盖了大部分基础设施参数，但以下关键值仍是硬编码。

### 社区参考

K8s 生态的配置管理主流模式是 **分层配置 + 合理默认值**，参考：
- **Prometheus Operator**：每个组件独立的 config struct，`SetDefaultConfig()` 中统一设置回退值
- **kube-bench**：配置结构体中用 `mapstructure` tag + `default` tag 声明默认值

mutton 项目已采用此模式（`config_base.go` + `SetDefaultConfig()`），只需补齐缺失字段。

### 方案设计

在 `config/config_base.go` 中新增/扩展配置结构体，在 `SetDefaultConfig()` 中统一设置默认值，替换各文件中的硬编码常量。

### 具体改动

#### 1.1 新增 `ServerConfig` 结构体

**文件**：`config/config_base.go`（新增结构体）

```go
type ServerConfig struct {
    Port               int `mapstructure:"port"`                // 默认 8888
    ReadTimeoutSec     int `mapstructure:"readTimeoutSec"`      // 默认 30
    WriteTimeoutSec    int `mapstructure:"writeTimeoutSec"`     // 默认 600
    IdleTimeoutSec     int `mapstructure:"idleTimeoutSec"`      // 默认 120
    ShutdownTimeoutSec int `mapstructure:"shutdownTimeoutSec"`  // 默认 10
    RateLimitPerSec    int `mapstructure:"rateLimitPerSec"`     // 默认 100
}
```

在 `Config` 结构体中添加字段：
```go
type Config struct {
    // ... 现有字段 ...
    Server ServerConfig `mapstructure:"server"`
}
```

在 `SetDefaultConfig()` 中设置默认值：
```go
v.SetDefault("server.port", 8888)
v.SetDefault("server.readTimeoutSec", 30)
v.SetDefault("server.writeTimeoutSec", 600)
v.SetDefault("server.idleTimeoutSec", 120)
v.SetDefault("server.shutdownTimeoutSec", 10)
v.SetDefault("server.rateLimitPerSec", 100)
```

**文件**：`cmd/main.go:131-139`（使用配置替换硬编码）

```go
// 之前
addr := ":" + os.Getenv("MUTONG_PORT")  // 复杂的环境变量逻辑

// 之后
addr := fmt.Sprintf(":%d", cfg.Server.Port)

srv := &http.Server{
    Addr:         addr,
    ReadTimeout:  time.Duration(cfg.Server.ReadTimeoutSec) * time.Second,
    WriteTimeout: time.Duration(cfg.Server.WriteTimeoutSec) * time.Second,
    IdleTimeout:  time.Duration(cfg.Server.IdleTimeoutSec) * time.Second,
}
```

#### 1.2 扩展 `KafkaConfig` 结构体

**文件**：`config/config_base.go`（扩展已有结构体）

```go
type KafkaConfig struct {
    // ... 现有字段 (Broker, Topic, Group, MaxRetries, RetryBackoffMs, DLTTopic, MaxRetryBytes) ...
    WorkerPoolSize       int `mapstructure:"workerPoolSize"`       // 默认 10
    TaskChanBuffer       int `mapstructure:"taskChanBuffer"`       // 默认 1000
    MaxBackoffMs         int `mapstructure:"maxBackoffMs"`         // 默认 30000
    PublishTimeoutSec    int `mapstructure:"publishTimeoutSec"`    // 默认 60
    KafkaSemaphore       int `mapstructure:"kafkaSemaphore"`       // 默认 50
    BizPublishChanBuffer int `mapstructure:"bizPublishChanBuffer"` // 默认 500
}
```

替换位置：
- `services/k8sresource_kafka.go:113` — `workerPoolSize` → `cfg.Kafka.WorkerPoolSize`
- `services/k8sresource_kafka.go:114` — `taskChan` buffer → `cfg.Kafka.TaskChanBuffer`
- `services/k8sresource_kafka.go:160` — `maxBackoff` → `cfg.Kafka.MaxBackoffMs`
- `services/k8sresource_kafka.go:74` — `PublishTimeout` → `cfg.Kafka.PublishTimeoutSec`
- `services/k8sresource_service.go:162,170` — `kafkaSem`/`bizPublishChan` → `cfg.Kafka.KafkaSemaphore`/`BizPublishChanBuffer`

#### 1.3 扩展 `ExecutorConf` 结构体

**文件**：`config/config_base.go`（扩展已有结构体）

```go
type ExecutorConf struct {
    Enabled   bool                   `mapstructure:"enabled"`
    AutoMode  bool                   `mapstructure:"autoMode"`
    AuditLog  AuditLogConfig         `mapstructure:"auditLog"`
    Actions   map[string]ActionConf  `mapstructure:"actions"`
    // 新增
    DefaultNamespace       string `mapstructure:"defaultNamespace"`        // 默认 "default"
    MaxRestartCount        int    `mapstructure:"maxRestartCount"`         // 默认 5
    CoolDownMinutes        int    `mapstructure:"coolDownMinutes"`         // 默认 5
    GracePeriodSec         int    `mapstructure:"gracePeriodSec"`          // 默认 30
    DeleteGracePeriodSec   int    `mapstructure:"deleteGracePeriodSec"`    // 默认 5
    MaxReplicas            int    `mapstructure:"maxReplicas"`             // 默认 100
    DefaultHPA             struct {
        MinReplicas int32 `mapstructure:"minReplicas"` // 默认 1
        MaxReplicas int32 `mapstructure:"maxReplicas"` // 默认 5
        TargetCPU   int32 `mapstructure:"targetCPU"`   // 默认 50
    } `mapstructure:"defaultHPA"`
}
```

替换位置：`services/executor/executor.go` 中所有硬编码常量 → 通过 `ExecutorConf` 传入。

#### 1.4 紧急修复：配置 key 对齐

**文件**：`configs/config.infra.yaml.example`

```yaml
# 修复前（Bug：key 与代码 ActionType 不匹配）
actions:
  deleteResource: { risk: high, autoThreshold: 1.0 }

# 修复后
actions:
  delete_pod: { risk: high, autoThreshold: 1.0 }      # 对齐 ActionDeletePod = "delete_pod"
  create_hpa: { risk: medium, autoThreshold: 0.9 }     # 补充缺失配置
  update_hpa: { risk: medium, autoThreshold: 0.85 }    # 补充缺失配置
```

#### 1.5 新增诊断规则配置段（可选，P2）

在 `config.diagnosis.yaml` 中增加 `ruleScores` 段，将 20+ 个硬编码置信度得分导出为可配置项。因改动较大且诊断引擎结构需同步调整，建议 P2 再做。

### 实施步骤

1. `config/config_base.go` — 新增 `ServerConfig`，扩展 `KafkaConfig`/`ExecutorConf`
2. `config/config.go` — `SetDefaultConfig()` 中补充默认值
3. `cmd/main.go` — HTTP Server 初始化和限流中间件使用配置值
4. `services/k8sresource_kafka.go` / `services/k8sresource_service.go` — 替换硬编码为配置值
5. `services/executor/executor.go` / `templates.go` — 替换硬编码为配置值
6. `configs/config.infra.yaml.example` — 修复 `deleteResource` → `delete_pod`，补充 `create_hpa`/`update_hpa`

---

## 问题 2：执行器操作类型扩展 ok

### 现状

当前 5 种操作：`restartPod`、`deletePod`、`scaleDeployment`、`createHPA`、`updateHPA`。缺失配置变更类操作（ConfigMap/Secret/ResourceLimits），且状态检查和安全门不完善。

### 社区参考（已验证）

| 项目 | 操作类型设计 | 可借鉴点 |
|------|-------------|---------|
| **Robusta.dev** | `@action` 装饰器 + 类型化事件参数。20+ 操作类型：`kubectl_command`（任意命令）、`rollout_restart`、`cordon/drain`、`node_bash_enricher`、`pod_bash_enricher`、`python_profiler`、`pod_ps` 等 | `kubectl_command` 模式用占位符 `$namespace/$kind/$name` 支持任意操作，是"万能操作类型"的优雅实现 |
| **heal8s** | CRD 定义 `Remediation` 资源，支持 `IncreaseMemory`/`ScaleUp`/`RollbackImage` + `Direct`/`PR` 双模式。Alert → Remediation CR → 执行 | 参数化 Action params（`memoryIncreasePercent: "50"`），支持 Git PR 模式 |
| **node-doctor** | ConfigMap 定义探测规则 + 修复策略，支持 `systemd-restart`/`network-remediation`/`custom-script`。多层安全防护：cooldown 5m + maxAttempts 3 + maxRemediationsPerHour 10 | 冷却期 + 最大重试 + 速率限制 三层安全防护 |

mutong 的差异化定位：不是通用的 GitOps 工具，而是**诊断驱动的精准修复**。操作类型应以"诊断结论能映射到的具体修复动作"为准。参考 Robusta 的 `kubectl_command` 作为万能 fallback。

### 方案设计

分两批新增操作类型。第一批（P0）覆盖配置变更核心场景，第二批（P1）覆盖运维增强。

#### 第一批：配置变更类（6 种）

| 操作类型 | 常量 | 场景 | 风险 | 实现方式 |
|------|------|------|:---:|------|
| `update_configmap` | `ActionUpdateConfigMap` | 运行时调参（连接池、日志级别） | high | Patch ConfigMap data |
| `update_secret` | `ActionUpdateSecret` | 证书/密钥轮换 | high | Patch Secret data |
| `update_resource_limits` | `ActionUpdateResourceLimits` | CPU/Memory limits 调整 | medium | Patch Deployment/STS container resources |
| `update_deployment_image` | `ActionUpdateDeploymentImage` | 版本回滚 | high | Patch Deployment image |
| `update_annotations` | `ActionUpdateAnnotations` | 修改注解（影响 Ingress 路由等） | medium | Patch annotations |
| `update_labels` | `ActionUpdateLabels` | 修改标签（影响 Service selector） | medium | Patch labels |

#### 第二批：运维增强（4 种）

| 操作类型 | 场景 | 风险 |
|------|------|:---:|
| `rollout_restart` | 滚动重启 Deployment/StatefulSet | medium |
| `scale_statefulset` | StatefulSet 扩缩容 | medium |
| `cordon_node` | 故障节点隔离 | high |
| `rollout_undo` | Deployment 版本回滚 | high |

### 具体改动

#### 2.1 数据模型扩展

**文件**：`models/executor/executor.go`

```go
const (
    // ... 现有常量 ...
    ActionUpdateConfigMap        ActionType = "update_configmap"
    ActionUpdateSecret           ActionType = "update_secret"
    ActionUpdateResourceLimits   ActionType = "update_resource_limits"
    ActionUpdateDeploymentImage  ActionType = "update_deployment_image"
    ActionUpdateAnnotations      ActionType = "update_annotations"
    ActionUpdateLabels           ActionType = "update_labels"
    ActionRolloutRestart         ActionType = "rollout_restart"
    ActionScaleStatefulSet       ActionType = "scale_statefulset"
    ActionCordonNode             ActionType = "cordon_node"
    ActionRolloutUndo            ActionType = "rollout_undo"
)

type ExecutionPlan struct {
    // ... 现有字段 ...
    ConfigData      map[string]string          `json:"configData,omitempty"`
    ResourceLimits  *corev1.ResourceRequirements `json:"resourceLimits,omitempty"`
    Annotations     map[string]string          `json:"annotations,omitempty"`
    Labels          map[string]string          `json:"labels,omitempty"`
    Image           string                     `json:"image,omitempty"`
    ContainerName   string                     `json:"containerName,omitempty"`
    RollbackRevision int64                     `json:"rollbackRevision,omitempty"`
}
```

#### 2.2 执行逻辑实现

**文件**：`services/executor/executor.go`

```go
// switch 分发扩展
case models.ActionUpdateConfigMap:
    return e.executeUpdateConfigMap(plan)
case models.ActionUpdateSecret:
    return e.executeUpdateSecret(plan)
case models.ActionUpdateResourceLimits:
    return e.executeUpdateResourceLimits(plan)
// ... 其余新增 case

// executeUpdateConfigMap 实现
func (e *K8sExecutor) executeUpdateConfigMap(plan *models.ExecutionPlan) (*models.ExecutionResult, error) {
    cm, err := e.k8sClient.CoreV1().ConfigMaps(plan.Namespace).Get(ctx, plan.ResourceName, metav1.GetOptions{})
    if err != nil {
        return nil, fmt.Errorf("get configmap: %w", err)
    }
    for k, v := range plan.ConfigData {
        cm.Data[k] = v
    }
    _, err = e.k8sClient.CoreV1().ConfigMaps(plan.Namespace).Update(ctx, cm, metav1.UpdateOptions{})
    return &models.ExecutionResult{Success: true, Message: "ConfigMap updated"}, nil
}

// executeUpdateResourceLimits 实现
func (e *K8sExecutor) executeUpdateResourceLimits(plan *models.ExecutionPlan) (*models.ExecutionResult, error) {
    deploy, err := e.k8sClient.AppsV1().Deployments(plan.Namespace).Get(ctx, plan.ResourceName, metav1.GetOptions{})
    // 找到目标容器（默认第一个，或按 plan.ContainerName）
    idx := 0
    if plan.ContainerName != "" {
        for i, c := range deploy.Spec.Template.Spec.Containers {
            if c.Name == plan.ContainerName {
                idx = i
                break
            }
        }
    }
    deploy.Spec.Template.Spec.Containers[idx].Resources = *plan.ResourceLimits
    _, err = e.k8sClient.AppsV1().Deployments(plan.Namespace).Update(ctx, deploy, metav1.UpdateOptions{})
    return &models.ExecutionResult{Success: true, Message: "Resource limits updated"}, nil
}
```

#### 2.3 Bridge 映射扩展

**文件**：`services/executor/bridge.go`

```go
// 扩展 action 映射表
var actionMap = map[string]models.ActionType{
    "Restart":      models.ActionRestartPod,
    "Scale":        models.ActionScaleDeployment,
    "Delete":       models.ActionDeletePod,
    "CreateHPA":    models.ActionCreateHPA,
    "UpdateHPA":    models.ActionUpdateHPA,
    // 新增
    "UpdateConfig":    models.ActionUpdateConfigMap,
    "UpdateSecret":    models.ActionUpdateSecret,
    "AdjustLimits":    models.ActionUpdateResourceLimits,
    "Rollback":        models.ActionUpdateDeploymentImage,
    "UpdateAnnotation": models.ActionUpdateAnnotations,
    "UpdateLabel":     models.ActionUpdateLabels,
    "RolloutRestart":  models.ActionRolloutRestart,
}
```

#### 2.4 安全门增强

为所有操作类型统一添加：
- **CoolDown 机制**：`services/executor/executor.go` 中新增 `coolDownMap map[string]time.Time`，所有操作类型共享冷却
- **变更前快照**：ConfigMap/Secret/Deployment 变更前保存原始状态到审计日志
- **DryRun 支持**：高危操作（high risk）支持 `dryRun: true`，执行前模拟验证

### 实施步骤

1. `models/executor/executor.go` — 新增 ActionType 常量 + 扩展 ExecutionPlan
2. `services/executor/executor.go` — 实现 6 种配置变更操作的执行逻辑
3. `services/executor/executor.go` — 统一添加 CoolDown + 变更前快照 + DryRun
4. `services/executor/bridge.go` — 扩展 action 映射表
5. `services/executor/executor_test.go` — 为新操作添加单元测试（Mock K8s Client）
6. `configs/config.infra.yaml.example` — 补充新操作的风险等级配置

---

## 问题 3：外部告警与业务关联 ok

### 现状

告警入口仅支持 Prometheus Alertmanager Webhook 格式。富化逻辑（`enricher.go`）强依赖 `kubernetes_uid`/`kubernetes_kind` 等 K8s 标签。Ceph、中间件、应用层告警无法接入，因为缺少 K8s 标签且没有外部告警源的注册/映射机制。

### 社区参考

**[待社区参考确认]** 主流方案：
- **Robusta.dev**：通过 `custom_playbook` + `enrichment` 机制处理外部告警，用 label mapping 将任意格式映射到 K8s 资源
- **Grafana OnCall / PagerDuty**：告警路由基于 escalation chain + service mapping
- **Alertmanager**：通过 `inhibit_rules` 的 `source_matchers`/`target_matchers` 处理跨源关联

mutong 的差异化：已有完整的 NebulaGraph 拓扑，外部告警的价值在于**通过拓扑桥接**而非创建独立的告警源管理。核心思路是"外部告警 → 拓扑反查 → 业务归属"。

### 方案设计

核心思路：**"外部告警 → 物理/逻辑桥接点 → NebulaGraph 拓扑反查 → K8s 资源/BusinessApp"**

不引入独立的 CMDB，不创建外部资源模型。充分利用现有的拓扑数据。

#### 3.1 三级桥接策略

```
外部告警
  │
  ├── Level 1: 物理节点桥接 (node → K8s Node → Pod → BusinessApp)
  │     场景：Ceph OSD、物理机硬件、网络设备
  │     关键标签：host/node/fqdn
  │
  ├── Level 2: 服务名桥接 (service name → BusinessApp → K8s workload)
  │     场景：Kafka Consumer、数据库连接池、应用健康检查
  │     关键标签：service/application/service_name
  │
  └── Level 3: 业务直接桥接 (team/business → BusinessApp 列表)
        场景：SaaS 告警、第三方 API、业务指标
        关键标签：team/business_unit/business_app
```

#### 3.2 新增通用 Webhook 端点

**文件**：`controllers/alert_controller.go`

```go
// POST /api/v1/alerts/external/:source_name
func (ctrl *AlertController) HandleExternalWebhook(c *gin.Context) {
    sourceName := c.Param("source_name")
    // 1. 查找 AlertSource 配置
    source, ok := ctrl.alertSources[sourceName]
    if !ok {
        c.JSON(404, gin.H{"error": "unknown alert source"})
        return
    }
    // 2. 解析原始 Payload (通用 JSON)
    var rawPayload map[string]interface{}
    c.ShouldBindJSON(&rawPayload)
    // 3. 标签映射 → 标准 Alert
    alert := source.MapToAlert(rawPayload)
    // 4. 指纹生成
    alert.Fingerprint = source.GenerateFingerprint(rawPayload)
    // 5. 走现有流水线
    ctrl.alertService.ProcessExternal(alert, source)
}
```

#### 3.3 告警源配置

**文件**：`configs/config.alert.yaml.example`（新增 `alertSources` 段）

```yaml
alertSources:
  # Ceph 存储告警
  ceph-prod:
    type: ceph
    labelMapping:
      alertname: "alert_name"
      severity: "severity"
      resourceType: "CephOSD"
      resourceName: "osd_id"
      node: "host"                    # ★ 桥接点：物理节点
      custom:
        pool: "pool"
        used_pct: "used_percent"
    enrichmentStrategy: "node_affinity" # 策略：按物理节点反查
    businessMapping:
      team: "storage-team"
      criticality: "high"

  # Kafka 中间件告警
  kafka-broker:
    type: middleware
    labelMapping:
      alertname: "alert_name"
      severity: "severity"
      resourceType: "KafkaBroker"
      resourceName: "broker_id"
      serviceName: "kafka"           # ★ 桥接点：服务名
      custom:
        topic: "topic"
        consumer_group: "consumer_group"
    enrichmentStrategy: "service_graph" # 策略：按服务拓扑图扩散
    businessMapping:
      team: "data-platform-team"

  # 应用层告警（通用模式）
  app-generic:
    type: application
    labelMapping:
      alertname: "alert_name"
      severity: "severity"
      appName: "service"             # ★ 桥接点：直接映射到 BusinessApp
      custom: {}                     # 透传所有未知字段
    enrichmentStrategy: "direct_business_app" # 策略：直接查 BusinessApp
```

#### 3.4 Enricher 扩展

**文件**：`services/alert/enricher.go`（新增方法）

```go
// enrichFromExternal 处理外部告警的富化
func (e *Enricher) enrichFromExternal(alert *models.Alert, source *models.AlertSource) {
    switch source.EnrichmentStrategy {
    case "node_affinity":
        e.enrichByNodeAffinity(alert, source)
    case "service_graph":
        e.enrichByServiceGraph(alert, source)
    case "direct_business_app":
        e.enrichByDirectBusinessApp(alert, source)
    }
}

// enrichByNodeAffinity 通过物理节点反查
func (e *Enricher) enrichByNodeAffinity(alert *models.Alert, source *models.AlertSource) {
    nodeName := alert.Labels["node"]
    if nodeName == "" {
        return
    }
    // 查 NebulaGraph: MATCH (pod)-[:RunsOn]->(node:K8sResource{kind:"Node",name:nodeName})
    //     MATCH (pod)-[:BelongsToApp]->(biz:BusinessApp)
    //     RETURN biz
    bizApps := e.topologyQuerier.QueryBusinessAppsOnNode(ctx, nodeName)
    // 注入 BusinessContext
    for _, biz := range bizApps {
        alert.BusinessContext = append(alert.BusinessContext, models.BusinessAppContext{
            AppName: biz.AppName,
            Team:    biz.Team,
            Criticality: biz.Criticality,
        })
    }
}

// enrichByServiceGraph 通过服务拓扑图扩散
func (e *Enricher) enrichByServiceGraph(alert *models.Alert, source *models.AlertSource) {
    serviceName := alert.Labels["serviceName"]
    // 查 config.business.yaml 的 knownServices
    // 查 NebulaGraph: MATCH (biz:BusinessApp{app_name:serviceName})
    //     MATCH (biz)-[:CallsApp]->(downstream:BusinessApp)
    // 自动关联下游依赖方为 Stakeholder
    downstreams := e.topologyQuerier.QueryDownstreamApps(ctx, serviceName)
    for _, ds := range downstreams {
        alert.Stakeholders = append(alert.Stakeholders, models.Stakeholder{
            AppName: ds.AppName,
            Team:    ds.Team,
            Role:    "downstream_consumer",
        })
    }
}
```

#### 3.5 新增告警源数据模型

**文件**：`models/alert/alert_source.go`（新建）

```go
type AlertSource struct {
    Name               string            `json:"name"`
    Type               string            `json:"type"`               // ceph/middleware/application/custom
    LabelMapping       map[string]string `json:"labelMapping"`       // 外部标签 → 内部标签
    EnrichmentStrategy string            `json:"enrichmentStrategy"` // node_affinity/service_graph/direct_business_app
    BusinessMapping    BusinessMapping   `json:"businessMapping"`
}

type BusinessMapping struct {
    Team        string `json:"team"`
    Criticality string `json:"criticality"`
    Strategy    string `json:"strategy"` // calls_app_downstream / calls_app_upstream / all
}

func (s *AlertSource) MapToAlert(raw map[string]interface{}) *Alert {
    labels := make(map[string]string)
    for internalKey, externalKey := range s.LabelMapping {
        if val, ok := raw[externalKey]; ok {
            labels[internalKey] = fmt.Sprint(val)
        }
    }
    // 兜底：注入 BusinessMapping 中的默认值
    labels["team"] = s.BusinessMapping.Team
    labels["criticality"] = s.BusinessMapping.Criticality
    return &Alert{Labels: labels, Status: "firing"}
}
```

### 实施步骤

1. `models/alert/alert_source.go` — 新建 AlertSource 模型
2. `configs/config.alert.yaml.example` — 新增 `alertSources` 配置段
3. `config/config_base.go` — 新增 `AlertSourceConfig` 结构体
4. `config/config.go` — 加载告警源配置
5. `controllers/alert_controller.go` — 新增 `POST /api/v1/alerts/external/:source` 端点
6. `services/alert/enricher.go` — 新增 `enrichFromExternal()` + 三种桥接策略实现
7. `services/alert/alert_service.go` — 新增 `ProcessExternal()` 方法

---

## 问题 4：巡检规则页面配置 ok

### 现状

巡检规则通过 Go 代码硬编码（`services/inspection/rules/` 下 6 个文件），在 `config.go:InitInspectionService()` 中手动注册。`YAMLEngine`（`yaml_engine.go`）已实现 3 种检查类型（`min_rows`/`field_contains`/`command`）但未被启用。

### 社区参考（已验证）

| 项目 | 规则定义方式 | 可借鉴点 |
|------|-------------|---------|
| **kube-bench** | YAML 三级层次结构：`controls → groups → checks`。9 种比较操作符（eq/has/nothave/regex/valid_elements 等）。`audit` 字段执行 shell，`tests.test_items` 做条件判断 | 规则结构清晰、运维人员可编辑。`valid_elements` 操作符（检查值是否在白名单内）可直接用于 mutong 的 nGQL 查询结果校验 |
| **Popeye** | Go 代码 Linter（26 种资源类型各一个）+ Spinach YAML 配置（阈值/排除/严重级别覆盖）。排除规则支持 regex/label/annotation/FQN 多维度过滤 | Spinach 的排除机制：按 namespace/resource type/code/label 灵活排除，适合多租户场景 |
| **Kyverno** | CNCF 毕业项目，声明式 `ClusterPolicy` CRD，CEL/JMESPath 表达式，支持 `audit`/`enforce` 双模式 | 太重，mutong 不需要 admission 集成。但 `audit` 模式"只记录不阻止"的思路值得参考 |

mutong 的 YAMLEngine 已有 `min_rows`/`field_contains`/`command` 三种检查类型，参考 kube-bench 的 `valid_elements` 增加第四种 `field_in_whitelist`。YAML 引擎 + PostgreSQL 存储 + CRUD API 的思路与社区主流一致。

### 方案设计

打通 YAMLEngine → PostgreSQL 存储 → CRUD API → 前端编辑 全链路。不需要重新设计规则引擎。

### 具体改动

#### 4.1 新增规则持久化模型

**文件**：`models/inspection/rule.go`（新建）

```go
type InspectionRuleModel struct {
    ID          uint           `gorm:"primarykey" json:"id"`
    CreatedAt   time.Time      `json:"createdAt"`
    UpdatedAt   time.Time      `json:"updatedAt"`
    DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
    Name        string         `gorm:"uniqueIndex;size:128" json:"name"`
    Description string         `json:"description"`
    Query       string         `gorm:"type:text" json:"query"`         // nGQL 查询语句
    CheckType   string         `json:"checkType"`                      // min_rows / field_contains / command
    CheckConfig string         `gorm:"type:text" json:"checkConfig"`   // JSON: CheckConfig
    Severity    string         `json:"severity"`                       // critical/warning/info
    Suggestion  string         `json:"suggestion"`
    Enabled     bool           `gorm:"default:true" json:"enabled"`
}
```

#### 4.2 新增规则存储层

**文件**：`services/inspection/rule_store.go`（新建）

```go
type RuleStore struct {
    db *gorm.DB
}

func (s *RuleStore) List(enabledOnly bool) ([]models.InspectionRuleModel, error)
func (s *RuleStore) Get(id uint) (*models.InspectionRuleModel, error)
func (s *RuleStore) GetByName(name string) (*models.InspectionRuleModel, error)
func (s *RuleStore) Create(rule *models.InspectionRuleModel) error
func (s *RuleStore) Update(rule *models.InspectionRuleModel) error
func (s *RuleStore) Delete(id uint) error
func (s *RuleStore) Toggle(id uint, enabled bool) error
```

#### 4.3 引擎融合——启动时合并加载

**文件**：`services/inspection/inspection_engine.go`（修改）

```go
func (e *InspectionEngine) LoadRulesFromDB(store *RuleStore) error {
    rules, err := store.List(true) // 只加载启用的规则
    for _, rule := range rules {
        yamlRule := &YAMLRule{
            name:       rule.Name,
            query:      rule.Query,
            checkType:  rule.CheckType,
            checkConfig: rule.CheckConfig,
            severity:   rule.Severity,
            suggestion: rule.Suggestion,
        }
        e.RegisterRule(yamlRule) // 注册为 InspectionRule 接口实现
    }
    return nil
}
```

#### 4.4 新增 CRUD API

**文件**：`controllers/inspection_controller.go`（新增端点）

```go
// GET    /api/v1/inspection/rules           — 规则列表
// POST   /api/v1/inspection/rules           — 创建规则（JSON Body）
// PUT    /api/v1/inspection/rules/:id       — 更新规则
// DELETE /api/v1/inspection/rules/:id       — 删除规则
// POST   /api/v1/inspection/rules/:id/toggle — 启停规则
```

#### 4.5 前端页面

**文件**：`view/src/inspection/`（新增文件）

- `rules.html` + `rules.js` — 规则列表页（表格 + 启停开关 + 操作按钮）
- `rule-editor.html` + `rule-editor.js` — 规则编辑页（表单：名称/描述/查询/检查类型/参数/严重级别/建议）

交互流程：`巡检页面 → "规则管理"按钮 → 规则列表 → "新增"/"编辑" → 表单编辑 → 保存 → 自动 reload 引擎`

### 实施步骤

1. `models/inspection/rule.go` — 新建 GORM 模型
2. `config/config.go` — AutoMigrate 注册 `InspectionRuleModel`
3. `services/inspection/rule_store.go` — 新建 CRUD 存储层
4. `services/inspection/inspection_engine.go` — 新增 `LoadRulesFromDB()`
5. `config/config.go:InitInspectionService()` — 代码规则 + DB 规则合并注册
6. `controllers/inspection_controller.go` — 新增 5 个 API 端点
7. `view/src/inspection/` — 新增规则列表 + 编辑前端页面

---

## 问题 5：用户管理体系

### 现状

User/Role 为极简桩代码（仅 ID+Name），服务层返回硬编码 mock。无认证系统、无 JWT、AuthMiddleware 仅做 webhook IP 白名单。`config.business.yaml` 中定义了三种角色但代码未使用。

### 社区参考（已验证）

| 方案 | 模式 | 可借鉴点 |
|------|------|---------|
| **gin-admin** (LyricTian/gin-admin) | Casbin `rbac_model.conf` + GORM adapter + `EnableAutoSave(true)`。使用 `keyMatch2`/`keyMatch3` 支持 RESTful 路由匹配（如 `/api/users/:id` → `/api/users/*`）。中间件注入 Casbin Enforcer，`Enforce(userID, path, method)` | 完整的三层架构（api → biz → dal），对于 admin/operator/viewer 三种角色场景，Casbin 的 model.conf 只需 15 行即可覆盖 |
| **gin-casbin** (maxwellhertz/gin-casbin) | `RequiresPermissions([]string{"user:read"})` + `RequiresRoles([]string{"admin"})` 路由级中间件 | 更简洁的 API 风格，适合简单场景 |
| **Casbin GORM Adapter** | `gormadapter.NewAdapterByDB(db)` + `e.EnableAutoSave(true)` → 策略变更自动持久化到 `casbin_rule` 表 | 无需单独维护策略表，Casbin 自动管理 |

**关键决策**：对于 mutong 的三种角色（admin/operator/viewer），Casbin 可能过度设计（模式匹配、策略表达式），但它的 `EnableAutoSave` + 策略管理 API 能显著减少样板代码。权衡后，采用**直接实现 GORM User/Role/Permission 模型 + 简单的路径-方法匹配中间件**——保持轻量，避免引入 Casbin 依赖。

### 方案设计

从零构建最小可行的用户认证和授权系统。分两阶段：基础认证 → RBAC 授权。

#### 5.1 数据库表设计

采用最简单的 RBAC 五表结构：

```sql
users (id, username, email, password_hash, status, last_login_at, created_at, updated_at)
roles (id, name, description, created_at)
user_roles (user_id, role_id)
permissions (id, name, resource, action, created_at)
role_permissions (role_id, permission_id)
```

用 GORM AutoMigrate 自动建表。

#### 5.2 JWT 认证

使用 `golang-jwt/jwt/v5`（项目已有依赖）。

```go
// models/user.go — 扩展 User 模型
type User struct {
    ID           uint      `gorm:"primarykey" json:"id"`
    Username     string    `gorm:"uniqueIndex;size:64" json:"username"`
    Email        string    `gorm:"size:128" json:"email"`
    PasswordHash string    `gorm:"size:256" json:"-"`
    Status       string    `gorm:"default:active" json:"status"` // active/disabled
    LastLoginAt  *time.Time `json:"lastLoginAt"`
    CreatedAt    time.Time  `json:"createdAt"`
    UpdatedAt    time.Time  `json:"updatedAt"`
    Roles        []Role     `gorm:"many2many:user_roles" json:"roles"`
}

// interfaces/user_interface.go — 扩展接口
type UserInterface interface {
    Authenticate(username, password string) (*User, string, error) // 返回 JWT token
    GetByID(id uint) (*User, error)
    List() ([]User, error)
    Create(user *User) error
    Update(user *User) error
    Delete(id uint) error
}
```

#### 5.3 AuthMiddleware 重构

**文件**：`controllers/auth_middleware.go`（重写）

```go
func AuthMiddleware(userService interfaces.UserInterface, logger *zap.Logger) gin.HandlerFunc {
    return func(c *gin.Context) {
        // 1. 公开路径跳过（webhook、login、health）
        if isPublicPath(c.Request.URL.Path) {
            c.Next()
            return
        }
        // 2. 从 Authorization header 提取 Bearer Token
        tokenStr := extractBearerToken(c)
        if tokenStr == "" {
            c.AbortWithStatusJSON(401, gin.H{"error": "missing token"})
            return
        }
        // 3. 解析 JWT，获取 userID
        claims, err := parseJWT(tokenStr)
        if err != nil {
            c.AbortWithStatusJSON(401, gin.H{"error": "invalid token"})
            return
        }
        // 4. 查用户及其角色
        user, err := userService.GetByID(claims.UserID)
        if err != nil || user.Status != "active" {
            c.AbortWithStatusJSON(403, gin.H{"error": "user not active"})
            return
        }
        // 5. 注入到 context
        c.Set("user", user)
        c.Set("permissions", getUserPermissions(user))
        c.Next()
    }
}

// 公开路径
func isPublicPath(path string) bool {
    publicPaths := []string{"/api/v1/alerts/webhook", "/api/v1/alerts/external", "/api/v1/login", "/metrics", "/swagger"}
    for _, p := range publicPaths {
        if strings.HasPrefix(path, p) {
            return true
        }
    }
    return false
}
```

#### 5.4 RBAC 权限检查辅助函数

**文件**：`controllers/gin_helpers.go`（新增）

```go
func RequirePermission(permission string) gin.HandlerFunc {
    return func(c *gin.Context) {
        perms, exists := c.Get("permissions")
        if !exists {
            c.AbortWithStatusJSON(403, gin.H{"error": "no permissions"})
            return
        }
        for _, p := range perms.([]string) {
            if p == permission || p == "admin" { // admin 通配
                c.Next()
                return
            }
        }
        c.AbortWithStatusJSON(403, gin.H{"error": "permission denied"})
    }
}
```

使用方式：
```go
executorGroup := engine.Group("/api/v1/executor")
executorGroup.Use(RequirePermission("execute"))
```

#### 5.5 新增 API

```go
POST   /api/v1/login                    // {username, password} → {token, user}
POST   /api/v1/refresh-token            // 刷新 JWT
GET    /api/v1/users/me                 // 当前用户信息
POST   /api/v1/users                    // 创建用户（admin only）
PUT    /api/v1/users/:id                // 更新用户
DELETE /api/v1/users/:id                // 删除用户
GET    /api/v1/users                    // 用户列表（admin only）
GET    /api/v1/roles                    // 角色列表
POST   /api/v1/roles                    // 创建角色（admin only）
POST   /api/v1/users/:id/roles          // 分配角色
```

#### 5.6 初始化管理员

在 `SetDefaultConfig()` 或 `InitPostgres()` 中自动创建默认管理员：
```go
func initDefaultAdmin(db *gorm.DB) {
    var count int64
    db.Model(&User{}).Count(&count)
    if count == 0 {
        admin := &User{Username: "admin", Email: "admin@localhost"}
        admin.SetPassword("admin123") // 首次登录强制修改
        db.Create(admin)
        // 绑定 admin 角色
        adminRole := &Role{Name: "admin"}
        db.FirstOrCreate(adminRole, Role{Name: "admin"})
        db.Model(admin).Association("Roles").Append(adminRole)
    }
}
```

### 实施步骤

1. `models/user_models.go` — 扩展 User/Role 模型，新建 Permission 模型
2. `config/config.go` — AutoMigrate 注册 User/Role/Permission/UserRole/RolePermission
3. `services/user_service.go` — 重写：JWT 生成/验证、密码哈希、CRUD
4. `services/role_service.go` — 重写：角色/权限 CRUD
5. `controllers/auth_middleware.go` — 重写为 JWT 认证中间件
6. `controllers/gin_helpers.go` — 新增 RBAC 权限检查辅助函数
7. `controllers/user_controller.go` — 扩展：登录/注册/个人信息/角色分配
8. `controllers/role_controller.go` — 扩展：角色/权限 CRUD
9. `cmd/main.go` — 在 `initializeServices()` 中添加 `initDefaultAdmin()`
10. 在敏感路由（executor、config）挂载 `RequirePermission("execute"/"admin")` 中间件

---

## 问题 6：业务属性与资源属性扩展

### 现状

业务属性字段固定在 6 个（appName/namespace/criticality/environment/team/businessUnit），不支持扩展。资源属性的 labels 以 JSON 字符串存储在 NebulaGraph，无独立索引。无 CMDB 系统。

### 方案设计

**原则**：不引入独立的 CMDB。利用现有 NebulaGraph + PostgreSQL 做轻量扩展——属性定义用配置，属性值存在图的顶点属性中。

#### 6.1 业务属性自定义字段

**方案**：在 NebulaGraph `BusinessApp` 顶点增加 `extended_attrs` 属性（JSON 字符串），字段名由 `config.business.yaml` 声明。

```yaml
# config.business.yaml
businessTopology:
  customBusinessAttributes:
    - name: "owner_email"
      label: "负责人邮箱"
      type: "string"
      source: "label"
      labelKeys: ["app.mutong.io/owner-email", "owner"]
    - name: "cost_center"
      label: "成本中心"
      type: "string"
      source: "manual"        # 仅手动设置
    - name: "sla"
      label: "SLA 等级"
      type: "enum"
      options: ["P0", "P1", "P2", "P3"]
      source: "label"
      labelKeys: ["app.mutong.io/sla"]
```

改动点：
- `config/config_base.go` — `BusinessTopologyConf` 增加 `CustomAttributes []AttributeDef`
- `services/business_label_syncer.go` — `extractBusinessAttributes()` 扩展，从 labels 提取自定义字段
- `models/business_app.go` — `BusinessApp` 增加 `ExtendedAttrs string`（JSON）
- NebulaGraph Schema — `ALTER TAG BusinessApp ADD (extended_attrs string)`
- 前端业务列表页 — 可配置列显示自定义字段

#### 6.2 资源属性管理 API

```go
// 新增
GET    /api/v1/resources/{uid}/labels      // 获取资源标签
PUT    /api/v1/resources/{uid}/labels      // 更新资源标签（仅 merge 模式）
GET    /api/v1/resources?label=k1:v1,k2:v2 // 按标签过滤资源
```

不创建独立的 CMDB 表。利用已有 K8s labels（JSON 字符串）做轻量查询。如需更复杂的资源属性查询，后续可考虑将 `Label` TAG 落地使用（Schema 已定义，代码未用）。

### 实施步骤

1. `config/config_base.go` — `BusinessTopologyConf` 扩展
2. `configs/config.business.yaml.example` — 新增 `customBusinessAttributes`
3. `models/business_app.go` — 扩展字段
4. `scripts/schema.ngql` — ALTER TAG 语句
5. `services/business_label_syncer.go` — 扩展提取逻辑
6. `controllers/k8sresource_controller.go` — 新增标签查询 API

---

## 问题 7：mutongctl CLI 增强

### 现状

18 个命令模块。Table 格式是假的（输出 JSON），缺失 `diagnose ask`/`trace`/`mcp` 命令。无流式输出。

### 方案设计

**P0**：补齐缺失命令 + 修复 Table 格式。
**P1**：轮询/交互增强。

#### 7.1 实现真正的 Table 输出

**文件**：`cmd/mutongctl/internal/format/format.go`（重写 `PrintTable`）

使用 `github.com/olekukoneko/tablewriter`（或直接用 `text/tabwriter`，不引入新依赖）：

```go
func PrintTable(data interface{}) error {
    // 反射获取字段和值
    // 用 tabwriter 对齐输出列
    w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
    // 写表头
    for _, col := range columns {
        fmt.Fprintf(w, "%s\t", strings.ToUpper(col))
    }
    fmt.Fprintln(w)
    // 写数据行
    for _, row := range rows {
        for _, val := range row {
            fmt.Fprintf(w, "%s\t", val)
        }
        fmt.Fprintln(w)
    }
    w.Flush()
    return nil
}
```

#### 7.2 新增 `diagnose ask` 命令

**文件**：`cmd/mutongctl/diagnose/ask.go`（新建）

```go
var askCmd = &cobra.Command{
    Use:   "ask",
    Short: "交互式 AI 诊断问答",
    RunE: func(cmd *cobra.Command, args []string) error {
        question, _ := cmd.Flags().GetString("question")
        // 接入 POST /api/v1/diagnosis/chat/ask
        result, err := api.AskDiagnosis(question)
        format.Print(result)
        return nil
    },
}
```

```
mutongctl diagnose ask --question "为什么 pod nginx-xxx 一直在重启？"
mutongctl diagnose ask --question "最近的 kafka 告警是什么原因？"
```

#### 7.3 新增 `trace` 命令组

**文件**：`cmd/mutongctl/trace/`（新建目录）

```go
mutongctl trace query --service myapp --operation "GET /api/users"
mutongctl trace services                    // 列出所有服务
mutongctl trace spans --trace-id "xxx"      // Span 瀑布图
```

#### 7.4 新增 `mcp` 命令组

```go
mutongctl mcp list-tools                    // 列出 20 个 MCP 工具
mutongctl mcp call query_topology --uid "xxx" --depth 2
mutongctl mcp call get_pod_logs --namespace prod --name nginx-xxx
```

### 实施步骤

1. `internal/format/format.go` — 重写 PrintTable
2. `cmd/mutongctl/diagnose/ask.go` — 新建 ask 命令
3. `cmd/mutongctl/trace/` — 新建 trace 命令组（query/services/spans）
4. `cmd/mutongctl/mcp/` — 新建 mcp 命令组（list-tools/call）
5. `root.go` — 注册新命令

---

## 问题 8：业务拓扑清理

### 现状

CallsApp 边无限累积（`TraceTopologySyncer` 只 INSERT 不 DELETE），trace 派生 BusinessApp 永不清除。

### 社区参考（已验证）

NebulaGraph 原生 TTL 机制（已验证可用）：

**nGQL 方式**：
```nGQL
ALTER EDGE CallsApp ADD (last_seen_at timestamp DEFAULT 0);
ALTER EDGE CallsApp TTL_COL = "last_seen_at", TTL_DURATION = 604800;  -- 7天
```

**Go Client 方式**（nebula-go v3.8+ 推荐）：
```go
edgeSchema := nebula.LabelSchema{
    Name: "CallsApp",
    Fields: []nebula.LabelFieldSchema{
        {Field: "last_seen_at", Type: "int64", Nullable: false},
    },
    TTLCol:      "last_seen_at",
    TTLDuration: 604800,  // 单位：秒
}
sessionPool.CreateEdge(edgeSchema)  // 或 SchemaManager.ApplyEdge 声明式同步
```

`SchemaManager.ApplyEdge()` 能自动判断 CREATE 还是 ALTER，适合在 `init-nebula` 中声明式管理 Schema 变更。

### 方案设计

采用 NebulaGraph 原生 TTL 机制处理 CallsApp 边，应用层定时任务处理孤儿 BusinessApp。

#### 8.1 CallsApp 边 TTL

**文件**：`scripts/schema.ngql`

```ngql
-- 修改 CallsApp 边，增加 last_seen_at 属性 + TTL
ALTER EDGE CallsApp ADD (last_seen_at timestamp DEFAULT 0);
ALTER EDGE CallsApp TTL_COL = "last_seen_at", TTL_DURATION = 604800; -- 7天
```

**文件**：`services/trace_topology_syncer.go` — UPSERT 时更新 `last_seen_at`

```go
func (s *TraceTopologySyncer) upsertCallEdge(srcUID, dstUID string) error {
    ngql := fmt.Sprintf(`
        INSERT EDGE CallsApp(last_seen_at) 
        VALUES "%s"->"%s":(%d)
    `, srcUID, dstUID, time.Now().Unix())
    return s.graphDB.Execute(ngql)
}
```

#### 8.2 孤儿 BusinessApp 定时清理

**文件**：`services/business_topology_cleanup.go`（新建）

```go
func (c *BusinessTopologyCleanup) Start(ctx context.Context, interval time.Duration) {
    ticker := time.NewTicker(interval)
    for {
        select {
        case <-ticker.C:
            c.cleanupOrphanBusinessApps(ctx)
        case <-ctx.Done():
            return
        }
    }
}

func (c *BusinessTopologyCleanup) cleanupOrphanBusinessApps(ctx context.Context) {
    // 查询既无 BelongsToApp 边也无 CallsApp 边的 BusinessApp
    ngql := `
        MATCH (biz:BusinessApp)
        WHERE NOT (biz)<-[:BelongsToApp]-() AND NOT (biz)-[:CallsApp]->() AND NOT ()-[:CallsApp]->(biz)
        RETURN id(biz) AS vid
    `
    // 批量 DELETE VERTEX（参考 k8sresource_nebula.go 的 batchSize 模式）
}
```

#### 8.3 BusinessApp 活跃度标记

```ngql
ALTER TAG BusinessApp ADD (last_seen_at timestamp DEFAULT 0, is_active bool DEFAULT true);
```

`BusinessLabelSyncer` 和 `TraceTopologySyncer` 在写入时更新 `last_seen_at`。可选地通过 Web UI 展示活跃/不活跃状态。

### 实施步骤

1. `scripts/schema.ngql` — ALTER EDGE/TAG 添加 TTL 和 last_seen_at
2. `services/trace_topology_syncer.go` — UPSERT 时更新 last_seen_at
3. `services/business_label_syncer.go` — 写入时更新 last_seen_at/is_active
4. `services/business_topology_cleanup.go` — 新建定时清理器
5. `cmd/main.go` — 启动 Cleanup 调度器
6. `configs/config.business.yaml.example` — 新增 cleanup 配置段

---

## 问题 9：ES 日志查询索引映射 ok

### 现状

单一 `indexPattern: "k8s-logs-*"`，无 serviceToIndex 路由，无索引模板管理。

### 社区参考（已验证）

| 主题 | 方案 | 具体规范 |
|------|------|---------|
| **ECS 业务元数据** | 使用 `labels` 对象存储自定义 KV | `labels.team`、`labels.business_unit` — ECS core 级别字段，所有 ELK 组件原生支持 |
| **ECS 服务标识** | 使用 `service.*` 字段族 | `service.name`、`service.environment`、`orchestrator.namespace` — 标准 ECS 规范 |
| **ES ILM Policy** | hot(1d rollover) → warm(3d forcemerge) → cold(30d) → delete(90d) | 多环境差异化：prod 90d、staging 30d、dev 7d |
| **Index Template** | 绑定 ILM policy + rollover alias | `k8s-logs-prod-{now/d}-000001` 自动创建，写入指向 alias `k8s-logs-prod` |

**关键决策**：使用 ECS `labels.*` 而非自定义顶层命名空间（如 `Mutong.team`）。理由：(1) `labels` 是 ECS core 字段，所有 ES 生态工具原生支持；(2) 不需要额外的 index template mapping 声明；(3) 与其他日志系统（Filebeat、Logstash）互通性最好。

### 方案设计

分两层：索引模板（确保 ES 字段类型正确）+ 索引路由（按 service name 路由到不同索引）。

#### 9.1 索引模板

**文件**：`scripts/init_es_template.sh`（新建）

```json
{
  "index_patterns": ["k8s-logs-*", "kafka-logs-*", "redis-logs-*"],
  "template": {
    "mappings": {
      "properties": {
        "@timestamp": { "type": "date" },
        "level": { "type": "keyword" },
        "message": { "type": "text" },
        "kubernetes": {
          "properties": {
            "namespace_name": { "type": "keyword" },
            "pod_name": { "type": "keyword" },
            "container_name": { "type": "keyword" }
          }
        },
        "service": {
          "properties": {
            "name": { "type": "keyword" }
          }
        },
        "labels": {
          "properties": {
            "team": { "type": "keyword" },
            "business_unit": { "type": "keyword" },
            "criticality": { "type": "keyword" }
          }
        }
      }
    }
  }
}
```

#### 9.2 serviceToIndex 路由

**文件**：`configs/config.infra.yaml.example`

```yaml
elasticsearch:
  serviceToIndex:
    default: "k8s-logs-*"
    overrides:
      "kafka": "kafka-logs-*"
      "redis": "redis-logs-*"
```

**文件**：`config/config_base.go`

```go
type ElasticsearchConf struct {
    // ... 现有字段 ...
    ServiceToIndex map[string]string `mapstructure:"serviceToIndex"`
}
```

**文件**：`services/logsearch/es_query.go`

```go
func (s *ESQueryService) resolveIndex(serviceName string) string {
    if idx, ok := s.config.ServiceToIndex[serviceName]; ok {
        return idx
    }
    return s.config.IndexPattern
}
```

#### 9.3 日志查询——业务上下文过滤

在 `SearchLogs` 接口中增加 `businessUnit`/`team` 参数，通过反查 BusinessApp 得到 namespace 列表，再在 ES 查询中添加 `kubernetes.namespace_name` filter。

```go
func (s *ESQueryService) SearchLogsByBusiness(ctx context.Context, businessUnit string, team string, keyword string) {
    // 1. 查 BusinessApp: 获取 businessUnit/team 对应的 namespace 列表
    namespaces := s.businessTopoSvc.GetNamespacesByBusinessUnit(businessUnit)
    // 2. 构建 ES query: terms kubernetes.namespace_name + query_string message
    return s.searchLogs(ctx, namespaces, keyword)
}
```

### 实施步骤

1. `config/config_base.go` — `ElasticsearchConf` 扩展 `ServiceToIndex`
2. `configs/config.infra.yaml.example` — 新增 `serviceToIndex` 配置
3. `services/logsearch/es_query.go` — `resolveIndex()` + `SearchLogsByBusiness()`
4. `controllers/log_controller.go` — 新增 `GET /api/v1/logs/business?businessUnit=&team=&keyword=` 端点
5. `scripts/init_es_template.sh` — 新建索引模板初始化脚本
6. `Justfile` — 新增 `just init-es-template` 命令

---
