# 重明 1/2/4/9 精细化实施方案

> **批次**：第一批（P0+高价值P1）
> **涉及问题**：硬编码导出 · 执行器扩展 · 巡检规则页面配置 · ES 日志索引映射
> **设计原则**：最小改动、精确行号、可逐任务执行、每个改动可独立验证

---

## 问题 1：硬编码导出

### 现状精确数据

| 位置 | 行号 | 硬编码值 | 替换目标 |
|------|:---:|------|------|
| `cmd/main.go` | 131 | `"8888"`（MUTONG_PORT 环境变量，无 YAML） | `cfg.Server.Port` |
| `cmd/main.go` | 137 | `30 * time.Second`（ReadTimeout） | `cfg.Server.ReadTimeoutSec` |
| `cmd/main.go` | 138 | `600 * time.Second`（WriteTimeout） | `cfg.Server.WriteTimeoutSec` |
| `cmd/main.go` | 139 | `120 * time.Second`（IdleTimeout） | `cfg.Server.IdleTimeoutSec` |
| `cmd/main.go` | 172 | `10 * time.Second`（ShutdownTimeout） | `cfg.Server.ShutdownTimeoutSec` |
| `cmd/main.go` | 367 | `100, time.Second`（RateLimitMiddleware） | `cfg.Server.RateLimitPerSec` |
| `services/k8sresource_kafka.go` | 113 | `const workerPoolSize = 10` | `cfg.Kafka.WorkerPoolSize` |
| `services/k8sresource_kafka.go` | 114 | `make(chan ..., 1000)`（taskChan） | `cfg.Kafka.TaskChanBuffer` |
| `services/k8sresource_kafka.go` | 160 | `30 * time.Second`（maxBackoff） | `cfg.Kafka.MaxBackoffMs` |
| `services/k8sresource_kafka.go` | 74 | `60 * time.Second`（PublishTimeout） | `cfg.Kafka.PublishTimeoutSec` |
| `services/k8sresource_service.go` | 162 | `make(chan struct{}, 50)`（kafkaSem） | `cfg.Kafka.KafkaSemaphore` |
| `services/k8sresource_service.go` | 170 | `make(chan ..., 500)`（bizPublishChan） | `cfg.Kafka.BizPublishChanBuffer` |
| `services/executor/executor.go` | 289 | `cs.RestartCount > 5` | `cfg.Executor.MaxRestartCount` |
| `services/executor/executor.go` | 296 | `5*time.Minute`（coolDown） | `cfg.Executor.CoolDownMinutes` |
| `services/executor/executor.go` | 312 | `int64(30)`（gracePeriod eviction） | `cfg.Executor.GracePeriodSec` |
| `services/executor/executor.go` | 327 | `int64(5)`（gracePeriod delete） | `cfg.Executor.DeleteGracePeriodSec` |
| `services/executor/executor.go` | 337 | `replicas > 100` | `cfg.Executor.MaxReplicas` |
| `services/executor/templates.go` | 67-69 | `MinReplicas=1, MaxReplicas=5, TargetCPU=50` | `cfg.Executor.DefaultHPA` |

### Bug 修复（不需要新增配置字段）

| Bug | 文件 | 现状 | 修复 |
|-----|------|------|------|
| config key 不匹配 | `configs/config.infra.yaml.example` | `deleteResource: {...}` | 改为 `delete_pod: {...}` |
| create_hpa 无配置 | `configs/config.infra.yaml.example` | 缺失 | 新增 `create_hpa: { risk: medium, autoThreshold: 0.9 }` |
| update_hpa 无配置 | `configs/config.infra.yaml.example` | 缺失 | 新增 `update_hpa: { risk: medium, autoThreshold: 0.85 }` |

### 实施步骤

#### Step 1: 新增 `ServerConfig` 结构体

**文件**: `config/config_base.go`（在现有 Config 结构体附近新增）

```go
// ServerConfig holds HTTP server tuning parameters.
type ServerConfig struct {
	Port               int `mapstructure:"port"`                // default 8888
	ReadTimeoutSec     int `mapstructure:"readTimeoutSec"`      // default 30
	WriteTimeoutSec    int `mapstructure:"writeTimeoutSec"`     // default 600
	IdleTimeoutSec     int `mapstructure:"idleTimeoutSec"`      // default 120
	ShutdownTimeoutSec int `mapstructure:"shutdownTimeoutSec"`  // default 10
	RateLimitPerSec    int `mapstructure:"rateLimitPerSec"`     // default 100
}
```

在 `Config` struct 中添加字段：
```go
Server ServerConfig `mapstructure:"server"`
```

#### Step 2: 扩展 `KafkaConfig` + `ExecutorConf`

**文件**: `config/config_base.go`

```go
// 在现有 KafkaConfig 结构体中添加
WorkerPoolSize       int `mapstructure:"workerPoolSize"`       // default 10
TaskChanBuffer       int `mapstructure:"taskChanBuffer"`       // default 1000
MaxBackoffMs         int `mapstructure:"maxBackoffMs"`         // default 30000
PublishTimeoutSec    int `mapstructure:"publishTimeoutSec"`    // default 60
KafkaSemaphore       int `mapstructure:"kafkaSemaphore"`       // default 50
BizPublishChanBuffer int `mapstructure:"bizPublishChanBuffer"` // default 500
```

```go
// 在现有 ExecutorConf 结构体中添加
DefaultNamespace     string `mapstructure:"defaultNamespace"`     // default "default"
MaxRestartCount      int    `mapstructure:"maxRestartCount"`      // default 5
CoolDownMinutes      int    `mapstructure:"coolDownMinutes"`      // default 5
GracePeriodSec       int    `mapstructure:"gracePeriodSec"`       // default 30
DeleteGracePeriodSec int    `mapstructure:"deleteGracePeriodSec"` // default 5
MaxReplicas          int    `mapstructure:"maxReplicas"`          // default 100
DefaultHPA struct {
    MinReplicas int32 `mapstructure:"minReplicas"` // default 1
    MaxReplicas int32 `mapstructure:"maxReplicas"` // default 5
    TargetCPU   int32 `mapstructure:"targetCPU"`   // default 50
} `mapstructure:"defaultHPA"`
```

#### Step 3: 设置默认值

**文件**: `config/config.go` — 在 `SetDefaultConfig()` 函数中添加：

```go
// Server
v.SetDefault("server.port", 8888)
v.SetDefault("server.readTimeoutSec", 30)
v.SetDefault("server.writeTimeoutSec", 600)
v.SetDefault("server.idleTimeoutSec", 120)
v.SetDefault("server.shutdownTimeoutSec", 10)
v.SetDefault("server.rateLimitPerSec", 100)

// Kafka additionals
v.SetDefault("kafka.workerPoolSize", 10)
v.SetDefault("kafka.taskChanBuffer", 1000)
v.SetDefault("kafka.maxBackoffMs", 30000)
v.SetDefault("kafka.publishTimeoutSec", 60)
v.SetDefault("kafka.kafkaSemaphore", 50)
v.SetDefault("kafka.bizPublishChanBuffer", 500)

// Executor additionals
v.SetDefault("executor.defaultNamespace", "default")
v.SetDefault("executor.maxRestartCount", 5)
v.SetDefault("executor.coolDownMinutes", 5)
v.SetDefault("executor.gracePeriodSec", 30)
v.SetDefault("executor.deleteGracePeriodSec", 5)
v.SetDefault("executor.maxReplicas", 100)
v.SetDefault("executor.defaultHPA.minReplicas", int32(1))
v.SetDefault("executor.defaultHPA.maxReplicas", int32(5))
v.SetDefault("executor.defaultHPA.targetCPU", int32(50))
```

#### Step 4: 替换硬编码 — cmd/main.go

```go
// 行 131 附近 — HTTP Server 初始化
addr := fmt.Sprintf(":%d", cfg.Server.Port)
srv := &http.Server{
    Addr:         addr,
    Handler:      router,
    ReadTimeout:  time.Duration(cfg.Server.ReadTimeoutSec) * time.Second,
    WriteTimeout: time.Duration(cfg.Server.WriteTimeoutSec) * time.Second,
    IdleTimeout:  time.Duration(cfg.Server.IdleTimeoutSec) * time.Second,
}

// 行 172 附近 — 优雅关闭
shutdownCtx, cancel := context.WithTimeout(context.Background(),
    time.Duration(cfg.Server.ShutdownTimeoutSec)*time.Second)

// 行 367 附近 — 限流中间件
router.Use(RateLimitMiddleware(cfg.Server.RateLimitPerSec, time.Second))
```

#### Step 5: 替换硬编码 — services/k8sresource_kafka.go

```go
// 行 113 — workerPoolSize
const workerPoolSize = 10  // 删除
// 改为: workerPoolSize := s.cfg.Kafka.WorkerPoolSize

// 行 114 — taskChan buffer
taskChan := make(chan *interfaces.Message, 1000)  // 删除
// 改为: taskChan := make(chan *interfaces.Message, s.cfg.Kafka.TaskChanBuffer)

// 行 160 — maxBackoff
maxBackoff := 30 * time.Second  // 删除
// 改为: maxBackoff := time.Duration(s.cfg.Kafka.MaxBackoffMs) * time.Millisecond

// 行 74 — publishTimeout
ctx, cancel := context.WithTimeout(ctx, 60*time.Second)  // 删除
// 改为: ctx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.Kafka.PublishTimeoutSec)*time.Second)
```

#### Step 6: 替换硬编码 — services/executor/

```go
// executor.go 行 289
if cs.RestartCount > 5 {  // 删除
// 改为: if int(cs.RestartCount) > e.cfg.GetExecutorConf().MaxRestartCount {

// executor.go 行 296
if time.Since(lastRestart) < 5*time.Minute {  // 删除
// 改为: if time.Since(lastRestart) < time.Duration(e.cfg.GetExecutorConf().CoolDownMinutes)*time.Minute {

// executor.go 行 312
gracePeriod := int64(30)  // 删除
// 改为: gracePeriod := int64(e.cfg.GetExecutorConf().GracePeriodSec)

// executor.go 行 327
gracePeriod := int64(5)  // 删除
// 改为: gracePeriod := int64(e.cfg.GetExecutorConf().DeleteGracePeriodSec)

// executor.go 行 337
if replicas > 100 {  // 删除
// 改为: if replicas > e.cfg.GetExecutorConf().MaxReplicas {

// templates.go 行 67-69
defaultHPA := e.cfg.GetExecutorConf().DefaultHPA
minReplicas := defaultHPA.MinReplicas
maxReplicas := defaultHPA.MaxReplicas
targetCPU := defaultHPA.TargetCPU
```

#### Step 7: 修复配置 key 不匹配 + 补充缺失项

**文件**: `configs/config.infra.yaml.example`

```yaml
executor:
  actions:
    restart_pod:      { risk: low,    autoThreshold: 0.7 }
    scale_deployment: { risk: medium, autoThreshold: 0.85 }
    delete_pod:       { risk: high,   autoThreshold: 1.0 }   # 修复: 原为 deleteResource
    create_hpa:       { risk: medium, autoThreshold: 0.9 }   # 新增
    update_hpa:       { risk: medium, autoThreshold: 0.85 }  # 新增
```

#### 验证

```bash
just build-mac && just run          # 编译+运行
go test -v -race ./config/...       # 配置加载测试
go test -v -race ./services/executor/...  # 执行器测试
```

---

## 问题 2：执行器操作类型扩展

### 精确代码现状

`models/executor/executor.go` 当前定义：
- **ActionType 常量（5种）**: `restart_pod` / `scale_deployment` / `delete_pod` / `create_hpa` / `update_hpa`（第 10-18 行）
- **ExecutionPlan 字段**: ID / Action / Target / Namespace / ResourceName / Reason / Confidence / Risk / ApprovedBy / Replicas / MinReplicas / MaxReplicas / TargetCPU / TargetMemory / CreatedAt（第 31-47 行）
- **RiskLevel 常量**: `low` / `medium` / `high`（第 25-27 行）

`services/executor/executor.go` 当前分发（第 119 行）：
```go
if action != ex.ActionRestartPod && action != ex.ActionScaleDeployment &&
   action != ex.ActionDeletePod && action != ex.ActionCreateHPA &&
   action != ex.ActionUpdateHPA {
    // 白名单校验 — 5 种操作
}
```

### 实施步骤

#### Step 1: 数据模型扩展

**文件**: `models/executor/executor.go`

```go
// 在第 18 行之后新增常量
ActionUpdateConfigMap         ActionType = "update_configmap"          // 行 19
ActionUpdateSecret            ActionType = "update_secret"             // 行 20
ActionUpdateResourceLimits    ActionType = "update_resource_limits"   // 行 21
ActionUpdateDeploymentImage   ActionType = "update_deployment_image"  // 行 22
ActionUpdateAnnotations       ActionType = "update_annotations"       // 行 23
ActionUpdateLabels            ActionType = "update_labels"            // 行 24

// 在第 47 行之后扩展 ExecutionPlan
ConfigData       map[string]string          `json:"configData,omitempty"`
ResourceLimits   *corev1.ResourceRequirements `json:"resourceLimits,omitempty"`
Annotations      map[string]string          `json:"annotations,omitempty"`
Labels           map[string]string          `json:"labels,omitempty"`
Image            string                     `json:"image,omitempty"`
ContainerName    string                     `json:"containerName,omitempty"`
```

#### Step 2: 执行逻辑实现

**文件**: `services/executor/executor.go`

在 `Execute()` 方法的白名单校验（第 119 行）中添加新类型：

```go
validActions := map[ex.ActionType]bool{
    ex.ActionRestartPod: true, ex.ActionScaleDeployment: true,
    ex.ActionDeletePod: true, ex.ActionCreateHPA: true, ex.ActionUpdateHPA: true,
    // 新增
    ex.ActionUpdateConfigMap: true, ex.ActionUpdateSecret: true,
    ex.ActionUpdateResourceLimits: true, ex.ActionUpdateDeploymentImage: true,
    ex.ActionUpdateAnnotations: true, ex.ActionUpdateLabels: true,
}
```

新增 6 个 `execute*` 函数（参考现有模式）：

```go
// executeUpdateConfigMap — Patch ConfigMap data
func (e *K8sExecutor) executeUpdateConfigMap(ctx context.Context, plan ex.ExecutionPlan) (*ex.ExecutionResult, error) {
    ns := plan.Namespace
    if ns == "" { ns = e.cfg.GetExecutorConf().DefaultNamespace }
    cm, err := e.k8sClient.CoreV1().ConfigMaps(ns).Get(ctx, plan.ResourceName, metav1.GetOptions{})
    if err != nil { return nil, fmt.Errorf("get configmap: %w", err) }
    for k, v := range plan.ConfigData { cm.Data[k] = v }
    _, err = e.k8sClient.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
    if err != nil { return nil, fmt.Errorf("update configmap: %w", err) }
    return &ex.ExecutionResult{Success: true, Message: fmt.Sprintf("ConfigMap %s/%s updated", ns, plan.ResourceName)}, nil
}

// executeUpdateSecret — Patch Secret stringData
func (e *K8sExecutor) executeUpdateSecret(ctx context.Context, plan ex.ExecutionPlan) (*ex.ExecutionResult, error) {
    ns := plan.Namespace
    if ns == "" { ns = e.cfg.GetExecutorConf().DefaultNamespace }
    s, err := e.k8sClient.CoreV1().Secrets(ns).Get(ctx, plan.ResourceName, metav1.GetOptions{})
    if err != nil { return nil, fmt.Errorf("get secret: %w", err) }
    if s.StringData == nil { s.StringData = make(map[string]string) }
    for k, v := range plan.ConfigData { s.StringData[k] = v }
    _, err = e.k8sClient.CoreV1().Secrets(ns).Update(ctx, s, metav1.UpdateOptions{})
    if err != nil { return nil, fmt.Errorf("update secret: %w", err) }
    return &ex.ExecutionResult{Success: true, Message: fmt.Sprintf("Secret %s/%s updated", ns, plan.ResourceName)}, nil
}

// executeUpdateResourceLimits — Patch container resources in Deployment
func (e *K8sExecutor) executeUpdateResourceLimits(ctx context.Context, plan ex.ExecutionPlan) (*ex.ExecutionResult, error) {
    ns := plan.Namespace
    if ns == "" { ns = e.cfg.GetExecutorConf().DefaultNamespace }
    deploy, err := e.k8sClient.AppsV1().Deployments(ns).Get(ctx, plan.ResourceName, metav1.GetOptions{})
    if err != nil { return nil, fmt.Errorf("get deployment: %w", err) }
    idx := 0
    if plan.ContainerName != "" {
        for i, c := range deploy.Spec.Template.Spec.Containers {
            if c.Name == plan.ContainerName { idx = i; break }
        }
    }
    deploy.Spec.Template.Spec.Containers[idx].Resources = *plan.ResourceLimits
    _, err = e.k8sClient.AppsV1().Deployments(ns).Update(ctx, deploy, metav1.UpdateOptions{})
    if err != nil { return nil, fmt.Errorf("update resources: %w", err) }
    return &ex.ExecutionResult{Success: true, Message: "resource limits updated"}, nil
}

// executeUpdateDeploymentImage — Patch Deployment container image
func (e *K8sExecutor) executeUpdateDeploymentImage(ctx context.Context, plan ex.ExecutionPlan) (*ex.ExecutionResult, error) {
    ns := plan.Namespace
    if ns == "" { ns = e.cfg.GetExecutorConf().DefaultNamespace }
    deploy, err := e.k8sClient.AppsV1().Deployments(ns).Get(ctx, plan.ResourceName, metav1.GetOptions{})
    if err != nil { return nil, fmt.Errorf("get deployment: %w", err) }
    idx := 0
    if plan.ContainerName != "" {
        for i, c := range deploy.Spec.Template.Spec.Containers {
            if c.Name == plan.ContainerName { idx = i; break }
        }
    }
    deploy.Spec.Template.Spec.Containers[idx].Image = plan.Image
    _, err = e.k8sClient.AppsV1().Deployments(ns).Update(ctx, deploy, metav1.UpdateOptions{})
    if err != nil { return nil, fmt.Errorf("update image: %w", err) }
    return &ex.ExecutionResult{Success: true, Message: fmt.Sprintf("image updated to %s", plan.Image)}, nil
}

// executeUpdateAnnotations — Patch resource annotations
func (e *K8sExecutor) executeUpdateAnnotations(ctx context.Context, plan ex.ExecutionPlan) (*ex.ExecutionResult, error) {
    ns := plan.Namespace
    if ns == "" { ns = e.cfg.GetExecutorConf().DefaultNamespace }
    // 用 DynamicClient 做通用 annotation patch（不限定资源类型）
    gvr := schema.GroupVersionResource{
        Group: "", Version: "v1", Resource: strings.ToLower(plan.Target),
    }
    obj, err := e.dynamicClient.Resource(gvr).Namespace(ns).Get(ctx, plan.ResourceName, metav1.GetOptions{})
    if err != nil { return nil, fmt.Errorf("get resource: %w", err) }
    ann := obj.GetAnnotations()
    if ann == nil { ann = make(map[string]string) }
    for k, v := range plan.Annotations { ann[k] = v }
    obj.SetAnnotations(ann)
    _, err = e.dynamicClient.Resource(gvr).Namespace(ns).Update(ctx, obj, metav1.UpdateOptions{})
    if err != nil { return nil, fmt.Errorf("update annotations: %w", err) }
    return &ex.ExecutionResult{Success: true, Message: "annotations updated"}, nil
}

// executeUpdateLabels — Patch resource labels（同上模式，操作 labels）
```

#### Step 3: Bridge 映射扩展

**文件**: `services/executor/bridge.go`

在 `CreatePlanFromDiagnosis` 的 action 映射表（switch-case）中新增：

```go
case "UpdateConfig":       action = ex.ActionUpdateConfigMap
case "UpdateSecret":       action = ex.ActionUpdateSecret
case "AdjustLimits":       action = ex.ActionUpdateResourceLimits
case "Rollback":           action = ex.ActionUpdateDeploymentImage
case "UpdateAnnotation":   action = ex.ActionUpdateAnnotations
case "UpdateLabel":        action = ex.ActionUpdateLabels
```

#### Step 4: 配置补充

**文件**: `configs/config.infra.yaml.example`

```yaml
executor:
  actions:
    # 现有...
    # 新增（高风操作，始终需审批）
    update_configmap:       { risk: high, autoThreshold: 1.0 }
    update_secret:          { risk: high, autoThreshold: 1.0 }
    update_deployment_image:{ risk: high, autoThreshold: 1.0 }
    # 新增（中风操作）
    update_resource_limits: { risk: medium, autoThreshold: 0.9 }
    update_annotations:     { risk: medium, autoThreshold: 0.85 }
    update_labels:          { risk: medium, autoThreshold: 0.85 }
```

#### 验证

```bash
go build ./services/executor/...              # 编译通过
go test -v -race ./services/executor/...      # 单元测试
# 手动测试：通过 mutongctl exec execute --action update_configmap --name test-cm -n default --config-data '{"key":"val"}'
```

---

## 问题 4：巡检规则页面配置

### 精确代码现状

**YAMLEngine** (`yaml_engine.go`):
- `Rule` 结构体（第 17-24 行）：Name / Description / Query / Check / Severity / Suggestion
- `CheckConfig`（第 27-37 行）：Type / Threshold / Field / Value / Operator / PromQL / Command / Timeout / Params
- 3 种检查类型（第 157-167 行）：`min_rows` / `field_contains` / `command`
- `Execute`（第 83 行）→ `executeQuery`（传入 graphDB）+ `check`（分发到三种检查器）

**InspectionEngine** (`inspection_engine.go`):
- `rules map[string]InspectionRule`（第 18 行）+ `sync.RWMutex`（第 19 行）
- `RegisterRule`（第 30 行）支持运行时动态注册
- `ExecuteAll`（第 37 行）并发执行所有规则，sync.WaitGroup + 逐个 goroutine

**InitInspectionService** (`config/config.go:1152-1177`):
- 硬编码 `RegisterRule` 6 条，YAMLEngine 未被使用

**InspectionReportModel** (`models/inspection/inspection_report.go`):
- GORM 模型已存在，表名 `inspection_reports`，有 `ToReport()` / `FromReport()` 转换函数

### 实施步骤

#### Step 1: 新建规则持久化模型

**文件**: `models/inspection/rule.go`（新建）

```go
package inspection

import (
	"gorm.io/gorm"
	"time"
)

type InspectionRuleModel struct {
	ID          uint           `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	Name        string         `gorm:"uniqueIndex;size:128;not null" json:"name"`
	Description string         `json:"description"`
	Query       string         `gorm:"type:text;not null" json:"query"`
	CheckType   string         `gorm:"size:32;not null" json:"checkType"`
	CheckConfig string         `gorm:"type:text;not null" json:"checkConfig"` // JSON
	Severity    string         `gorm:"size:16;not null" json:"severity"`
	Suggestion  string         `json:"suggestion"`
	Enabled     bool           `gorm:"default:true" json:"enabled"`
}

func (InspectionRuleModel) TableName() string {
	return "inspection_rules"
}
```

**文件**: `config/config.go` — 在 `migrateDiagnosisTables()` 中注册：

```go
db.AutoMigrate(&inspection.InspectionRuleModel{})
```

#### Step 2: 新建规则存储层

**文件**: `services/inspection/rule_store.go`（新建）

```go
package inspection

type RuleStore struct {
	db *gorm.DB
}

func NewRuleStore(db *gorm.DB) *RuleStore { return &RuleStore{db: db} }

func (s *RuleStore) List(enabledOnly bool) ([]inspection.InspectionRuleModel, error) {
    tx := s.db
    if enabledOnly { tx = tx.Where("enabled = ?", true) }
    var rules []inspection.InspectionRuleModel
    err := tx.Find(&rules).Error
    return rules, err
}

func (s *RuleStore) Get(id uint) (*inspection.InspectionRuleModel, error) {
    var r inspection.InspectionRuleModel
    err := s.db.First(&r, id).Error
    if err != nil { return nil, err }
    return &r, nil
}

func (s *RuleStore) GetByName(name string) (*inspection.InspectionRuleModel, error) {
    var r inspection.InspectionRuleModel
    err := s.db.Where("name = ?", name).First(&r).Error
    if err != nil { return nil, err }
    return &r, nil
}

func (s *RuleStore) Create(r *inspection.InspectionRuleModel) error {
    return s.db.Create(r).Error
}

func (s *RuleStore) Update(r *inspection.InspectionRuleModel) error {
    return s.db.Save(r).Error
}

func (s *RuleStore) Delete(id uint) error {
    return s.db.Delete(&inspection.InspectionRuleModel{}, id).Error
}

func (s *RuleStore) Toggle(id uint, enabled bool) error {
    return s.db.Model(&inspection.InspectionRuleModel{}).Where("id = ?", id).Update("enabled", enabled).Error
}
```

#### Step 3: 引擎融合 — 从 DB 加载规则

**文件**: `services/inspection/inspection_engine.go`（扩展）

```go
// LoadRulesFromDB 从数据库加载启用的 YAML 规则并注册到引擎
func (e *InspectionEngine) LoadRulesFromDB(store *RuleStore) error {
    dbRules, err := store.List(true) // only enabled
    if err != nil {
        return fmt.Errorf("load rules from db: %w", err)
    }
    for _, dbRule := range dbRules {
        yamlRule := &DBBackedRule{
            model:  dbRule,
            graphDB: e.graphDB,
            yamlEngine: NewYAMLEngine(e.logger, e.graphDB, []Rule{{
                Name:        dbRule.Name,
                Description: dbRule.Description,
                Query:       dbRule.Query,
                Check:       parseCheckConfig(dbRule.CheckConfig),
                Severity:    dbRule.Severity,
                Suggestion:  dbRule.Suggestion,
            }}),
        }
        e.RegisterRule(yamlRule)
    }
    e.logger.Info("Loaded inspection rules from database", zap.Int("count", len(dbRules)))
    return nil
}

// parseCheckConfig 从 JSON 解析 CheckConfig
func parseCheckConfig(jsonStr string) CheckConfig {
    var cfg CheckConfig
    json.Unmarshal([]byte(jsonStr), &cfg)
    return cfg
}
```

**文件**: `services/inspection/db_backed_rule.go`（新建）

```go
package inspection

// DBBackedRule 封装数据库规则为 InspectionRule 接口
type DBBackedRule struct {
    model      inspection.InspectionRuleModel
    graphDB    interfaces.GraphDB
    yamlEngine *YAMLEngine
}

func (r *DBBackedRule) Name() string          { return r.model.Name }
func (r *DBBackedRule) Description() string   { return r.model.Description }
func (r *DBBackedRule) Severity() string       { return r.model.Severity }

func (r *DBBackedRule) Execute(ctx context.Context, graphDB interfaces.GraphDB) ([]inspection.InspectionResult, error) {
    report := r.yamlEngine.Execute(ctx)
    var results []inspection.InspectionResult
    for _, ruleResult := range report.Rules {
        for _, finding := range ruleResult.Findings {
            results = append(results, inspection.InspectionResult{
                RuleName:   r.model.Name,
                Severity:   finding.Severity,
                Message:    finding.Message,
                Resources:  []string{finding.Resource},
                Suggestion: r.model.Suggestion,
                Timestamp:  report.Timestamp,
            })
        }
    }
    return results, nil
}
```

#### Step 4: 修改初始化流程

**文件**: `config/config.go` — `InitInspectionService()`

```go
func (c *Config) InitInspectionService(...) interfaces.InspectionProcessor {
    engine := insp_service.NewInspectionEngine(logger, graphDB)

    // 1. 注册硬编码规则（保持不变）
    engine.RegisterRule(insp_rules.NewSinglePointFailureRule(logger))
    // ... 其余 5 条

    // 2. 从数据库加载自定义规则
    if c.GetConfigGormDB() != nil {
        store := insp_service.NewRuleStore(c.GetConfigGormDB())
        if err := engine.LoadRulesFromDB(store); err != nil {
            logger.Warn("Failed to load inspection rules from DB", zap.Error(err))
        }
    }

    // ... 其余代码不变
}
```

#### Step 5: 新增 CRUD API 端点

**文件**: `controllers/inspection_controller.go`（新增端点）

```go
func (c *InspectionController) RegisterRoutes(app *gin.Engine) {
    api := app.Group("/api/v1/inspection")
    // ... 现有 6 个端点 ...

    // 新增：规则管理
    rules := api.Group("/rules")
    rules.GET("", c.listRules)             // GET  /api/v1/inspection/rules
    rules.POST("", c.createRule)           // POST /api/v1/inspection/rules
    rules.PUT("/:id", c.updateRule)        // PUT  /api/v1/inspection/rules/:id
    rules.DELETE("/:id", c.deleteRule)     // DELETE /api/v1/inspection/rules/:id
    rules.POST("/:id/toggle", c.toggleRule) // POST /api/v1/inspection/rules/:id/toggle
}

func (c *InspectionController) listRules(ctx *gin.Context) {
    rules, err := c.ruleStore.List(false) // 包含禁用的
    ctx.JSON(200, rules)
}

func (c *InspectionController) createRule(ctx *gin.Context) {
    var rule models.InspectionRuleModel
    if err := ctx.ShouldBindJSON(&rule); err != nil {
        ctx.JSON(400, gin.H{"error": err.Error()})
        return
    }
    if err := c.ruleStore.Create(&rule); err != nil {
        ctx.JSON(500, gin.H{"error": err.Error()})
        return
    }
    c.reloadEngine() // 重新加载引擎
    ctx.JSON(201, rule)
}
// updateRule / deleteRule / toggleRule 类似
```

#### 验证

```bash
just build-mac && just run
# POST /api/v1/inspection/rules 创建一条测试规则
# POST /api/v1/inspection/execute 触发巡检，验证新规则生效
```

---

## 问题 9：ES 日志查询索引映射

### 精确代码现状

`services/logsearch/es_query.go`:
- `ESQueryService` 结构体（第 17-22 行）：`logger / client / indexPattern / timeout`
- 单一 `indexPattern`（第 46 行默认 `"k8s-logs-*"`）
- `searchLogs()` 方法（第 233 行）：`esapi.SearchRequest{Index: []string{s.indexPattern}}` — **硬编码单索引**
- `parseLogEntry()`（第 271 行）：提取 `@timestamp / level / message / kubernetes.namespace_name / kubernetes.pod_name / kubernetes.container_name` — 7 个固定字段

### 实施步骤

#### Step 1: 扩展配置结构体

**文件**: `config/config_base.go`

```go
type ElasticsearchConf struct {
    Addresses       []string            `mapstructure:"addresses"`
    IndexPattern    string              `mapstructure:"indexPattern"`
    ServiceToIndex  map[string]string   `mapstructure:"serviceToIndex"`  // 新增
    Username        string              `mapstructure:"username"`
    Password        string              `mapstructure:"password"`
    Timeout         int                 `mapstructure:"timeout"`
}
```

#### Step 2: 配置示例

**文件**: `configs/config.infra.yaml.example`

```yaml
elasticsearch:
  addresses:
    - "http://localhost:9200"
  indexPattern: "k8s-logs-*"
  serviceToIndex:
    default: "k8s-logs-*"
    overrides:
      "kafka": "kafka-logs-*"
      "redis": "redis-logs-*"
  username: ""
  password: ""
  timeout: 10
```

#### Step 3: 扩展 ESQueryService — 索引路由

**文件**: `services/logsearch/es_query.go`

```go
// 新增字段
type ESQueryService struct {
    logger         interfaces.Logger
    client         *elasticsearch.Client
    indexPattern   string
    serviceToIndex map[string]string   // 新增
    timeout        time.Duration
}

// 构造函数增加参数
func NewESQueryService(logger interfaces.Logger, addresses []string,
    indexPattern string, serviceToIndex map[string]string,
    username, password string, timeoutSec int) (*ESQueryService, error) {
    // ... 现有逻辑 ...
    return &ESQueryService{
        logger: logger, client: client,
        indexPattern: indexPattern, serviceToIndex: serviceToIndex,
        timeout: timeout,
    }, nil
}

// 新增方法：按 service name 解析索引
func (s *ESQueryService) resolveIndex(serviceName string) string {
    if serviceName == "" || s.serviceToIndex == nil {
        return s.indexPattern
    }
    if idx, ok := s.serviceToIndex[serviceName]; ok {
        return idx
    }
    if idx, ok := s.serviceToIndex["default"]; ok {
        return idx
    }
    return s.indexPattern
}

// 修改 searchLogs — 支持多索引
func (s *ESQueryService) searchLogs(ctx context.Context, query map[string]interface{}, serviceName string) ([]interfaces.LogEntry, error) {
    index := s.resolveIndex(serviceName)
    body, _ := json.Marshal(query)
    req := esapi.SearchRequest{
        Index: []string{index},  // 原来的 s.indexPattern 改为动态
        Body:  bytes.NewReader(body),
    }
    // ... 其余代码不变 ...
}
```

#### Step 4: 新增 SearchLogsByBusiness 方法

**文件**: `services/logsearch/es_query.go`（新增方法）

```go
// SearchLogsByBusiness — 按业务上下文过滤日志
// 先通过 businessUnit/team 查 BusinessApp 得到 namespace 列表，再查 ES
func (s *ESQueryService) SearchLogsByBusiness(ctx context.Context, namespaces []string, keyword string, sinceMinutes int, maxResults int) ([]interfaces.LogEntry, error) {
    if !s.IsEnabled() { return nil, nil }
    if maxResults <= 0 { maxResults = 100 }
    if sinceMinutes <= 0 { sinceMinutes = 15 }

    filters := []interface{}{
        map[string]interface{}{"query_string": map[string]interface{}{"query": keyword}},
    }
    if len(namespaces) > 0 {
        filters = append(filters, map[string]interface{}{
            "terms": map[string]interface{}{"kubernetes.namespace_name": namespaces},
        })
    }
    sinceTime := time.Now().Add(-time.Duration(sinceMinutes) * time.Minute).Format(time.RFC3339)
    filters = append(filters, map[string]interface{}{
        "range": map[string]interface{}{"@timestamp": map[string]interface{}{"gte": sinceTime}},
    })
    query := map[string]interface{}{
        "query": map[string]interface{}{"bool": map[string]interface{}{"filter": filters}},
        "sort":  []interface{}{map[string]interface{}{"@timestamp": map[string]interface{}{"order": "desc"}}},
        "size":  maxResults,
    }
    return s.searchLogs(ctx, query, "") // serviceName 不需要，按 namespace 路由
}
```

#### Step 5: 更新接口定义

**文件**: `interfaces/dependencies.go`（如果 LogQuerier 接口存在则扩展）

```go
type LogQuerier interface {
    // ... 现有方法 ...
    SearchLogsByBusiness(ctx context.Context, namespaces []string, keyword string, sinceMinutes int, maxResults int) ([]LogEntry, error)
}
```

#### Step 6: 新增 ES 索引模板初始化脚本

**文件**: `scripts/init_es_template.sh`（新建）

```bash
#!/bin/bash
ES_URL="${ES_URL:-http://localhost:9200}"

curl -X PUT "$ES_URL/_index_template/mutong-logs" -H 'Content-Type: application/json' -d '{
  "index_patterns": ["k8s-logs-*", "kafka-logs-*", "redis-logs-*"],
  "priority": 200,
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
        "labels": {
          "properties": {
            "team": { "type": "keyword" },
            "business_unit": { "type": "keyword" }
          }
        }
      }
    }
  }
}'
echo "✅ ES index template created"
```

#### Step 7: Justfile 增加命令

```justfile
init-es-template:
    chmod +x scripts/init_es_template.sh
    scripts/init_es_template.sh
```

#### 验证

```bash
# 1. 启动 ES（docker 或外部）
# 2. 初始化模板
ES_URL=http://localhost:9200 just init-es-template

# 3. 写入测试数据
curl -X POST "http://localhost:9200/k8s-logs-2026.05.30/_doc" -H 'Content-Type: application/json' -d '{
  "@timestamp": "2026-05-30T10:00:00Z",
  "level": "ERROR",
  "message": "test error",
  "kubernetes": {"namespace_name": "prod", "pod_name": "test-pod"}
}'

# 4. 测试查询
curl "http://localhost:8888/api/v1/logs/errors?namespace=prod&since=60"

# 5. 测试 serviceToIndex
# 配置中设置 kafka -> kafka-logs-*，写数据到 kafka-logs-2026.05.30，
# 通过 SearchLogs 验证路由正确
```

---

## 附录：实施顺序建议

```
Day 1: 问题 1 (硬编码导出)
  ├── Step 1-3: config_base.go + config.go 新增字段和默认值（纯配置改动，无行为变更）
  ├── Step 4-6: 替换 cmd/main.go + services/ 中的硬编码
  └── Step 7: 修复 config key 不匹配 bug

Day 2: 问题 2 (执行器扩展)
  ├── Step 1: models/executor/executor.go 扩展 ActionType + ExecutionPlan
  ├── Step 2: services/executor/executor.go 实现 6 个新操作
  └── Step 3-4: bridge.go 映射 + config 补充

Day 3: 问题 4 (巡检规则)
  ├── Step 1-2: models/ + services/ 新建 RuleStore
  ├── Step 3: InspectionEngine.LoadRulesFromDB
  └── Step 4-5: config.go 修改初始化 + controllers/ 新增 API

Day 4: 问题 9 (ES 索引)
  ├── Step 1-3: config_base.go + es_query.go 扩展 serviceToIndex
  ├── Step 4-5: SearchLogsByBusiness + 接口更新
  └── Step 6-7: 初始化脚本 + Justfile
```
