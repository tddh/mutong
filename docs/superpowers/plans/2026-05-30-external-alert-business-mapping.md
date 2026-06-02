# 外部告警与业务关联 — 详细实现方案

> **针对 agentic workers:** 使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐步实施。步骤使用复选框 (`- [ ]`) 追踪进度。

**目标：** 让 Ceph、Kafka、MySQL、网络设备等外部系统的告警能够接入 mutong 告警流水线，并自动关联到内部业务拓扑（BusinessApp/Team/Stakeholder），与现有 K8s 告警享受同等的富化、抑制、路由和通知能力。

**架构思路：** 两种互补路径——**路径 A** 通过 mutong 后端做标签映射 + NebulaGraph 拓扑反查，适用于任意外部系统；**路径 B** 在 Prometheus 侧通过 relabel 预打业务标签，适用于 Prometheus 配置可管理的场景。两者共享同一套 `AlertSource` 配置模型和处理流水线。

**技术栈：** Go 1.25 + Gin + NebulaGraph + Viper 配置 + 现有 enricher/suppressor/router/notifier 流水线

---

## 问题分析

### 现状

| 问题 | 详情 |
|------|------|
| **入口单一** | 仅支持 Alertmanager Webhook 格式 (`POST /api/v1/alerts/webhook`)，解析器要求 `kubernetes_uid`/`kubernetes_kind` 等 K8s 特有标签 |
| **富化强依赖 K8s** | `AlertEnricher.Enrich()` → `ExtractK8sLabels()` → `enrichByUID/enrichFromPod/enrichFromNode`，无法处理无 K8s 标签的外部告警 |
| **无外部告警源注册** | Ceph/Kafka 等系统的告警格式各异，无统一的接入规范、标签映射模板和桥接策略 |
| **业务归属无法建立** | 外部告警无法找到 `BusinessApp`，导致抑制/路由/Stakeholder 通知全部失效 |

### 目标

- 支持 **任意外部系统** 通过统一 Webhook 端点接入
- 告警自动关联到业务拓扑（BusinessApp → Team → Stakeholder）
- 复用现有富化/抑制/路由/通知流水线
- 不引入独立 CMDB，利用现有 NebulaGraph 拓扑数据
- 同时支持两种标签注入路径（后端映射 + Prometheus 侧预打标签）

---

## 架构设计

### 两种路径对比

```
┌──────────────────────────────────────────────────────────────────┐
│                     路径 A：后端映射（通用路径）                    │
│                                                                    │
│  外部系统            Prometheus         Alertmanager              │
│  ┌──────┐   指标    ┌──────────┐   告警   ┌─────────────┐        │
│  │ Ceph │─────────▶│  scrape  │────────▶│ matchers →  │        │
│  │ Kafka│   指标    │relabel(basic)│       │ webhook_url  │        │
│  │ MySQL│─────────▶│          │────────▶│             │        │
│  └──────┘          └──────────┘          └──────┬──────┘        │
│                                                  │                │
│                                        POST /api/v1/alerts/      │
│                                        external/:source          │
│                                                  ▼                │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                    mutong 后端                             │    │
│  │                                                          │    │
│  │  ① AlertSource.MapToAlert()                              │    │
│  │     外部标签 → 内部标准标签 (labelMapping)                  │    │
│  │                                                          │    │
│  │  ② Enricher.enrichFromExternal()                         │    │
│  │     node_affinity:   host → Node → Pod → BusinessApp     │    │
│  │     service_graph:   serviceName → BusinessApp → Calls   │    │
│  │     direct_business: appName → BusinessApp 直接匹配       │    │
│  │                                                          │    │
│  │  ③ 复用现有流水线: Suppress → Route → Notify             │    │
│  └─────────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────┐
│                 路径 B：Prometheus 侧标签（简化路径）              │
│                                                                    │
│  外部系统            Prometheus                                │
│  ┌──────┐   指标    ┌──────────────────────┐                    │
│  │ Ceph │─────────▶│ scrape_config:        │                    │
│  │      │          │   relabel_configs:    │                    │
│  │      │          │   - target_label: team│                    │
│  │      │          │     replacement: 存储  │                    │
│  │      │          │   - target_label: biz │                    │
│  │      │          │     replacement: ceph │                    │
│  └──────┘          └──────────┬───────────┘                    │
│                               │                                  │
│                        Alertmanager                              │
│                        (标签已含业务信息)                          │
│                               │                                  │
│                POST /api/v1/alerts/webhook (标准端点)             │
│                               ▼                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                    mutong 后端                             │    │
│  │                                                          │    │
│  │  ① ExtractK8sLabels() 提取 labels["team"] → team         │    │
│  │     直接注入 BusinessContext (无需 NebulaGraph 反查)       │    │
│  │                                                          │    │
│  │  ② 如有 resourceType/name 则走拓扑反查（补充拓扑上下文）    │    │
│  │                                                          │    │
│  │  ③ 复用现有流水线                                         │    │
│  └─────────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────────┘
```

### 推荐组合策略

| 标签层 | 来源 | 示例 | 变动频率 |
|--------|------|------|:---:|
| **基础设施标签** | Prometheus relabel | `cluster=prod-sh`, `env=production`, `region=cn-east` | 极低 |
| **业务标签** | Prometheus relabel（可选）或 mutong 映射 | `team=storage-team`, `business_unit=基础设施` | 中等 |
| **拓扑上下文** | mutong NebulaGraph 反查 | `stakeholders=[支付, 物流]`, `affectedApps=[订单服务]` | 自动 |

---

## 文件改动清单

### 新增文件

| 文件 | 职责 |
|------|------|
| `models/alert/alert_source.go` | `AlertSource` 配置模型 + `MapToAlert()` + `GenerateFingerprint()` + `ExtractBusinessLabels()` |
| `configs/config.alert.yaml.example`（扩展） | `alertSources` 配置段，含 Ceph/Kafka/通用 示例 |
| `docs/external-alert-integration-guide.md` | 外部告警接入指南（运维文档） |

### 修改文件

| 文件 | 改动 |
|------|------|
| `config/config_base.go` | 新增 `AlertSourceConfig` 结构体 + 在 `Config` 中引用 |
| `config/config.go` | `LoadConfig()` 中加载告警源配置 → 注入到 `AlertService` 和 `AlertController` |
| `controllers/alert_controller.go` | 新增 `POST /api/v1/alerts/external/:source` 端点 + 注入 `alertSources` |
| `services/alert/enricher.go` | 新增 `enrichFromExternal()` + `enrichByNodeAffinity()` + `enrichByServiceGraph()` + `enrichByDirectBusinessApp()` + `enrichFromBusinessLabels()` |
| `services/alert/alert_service.go` | 新增 `ProcessExternal()` 方法，扩展 `AlertService` 注入 `alertSources` + `bizCtxProvider` |
| `interfaces/alert/processor.go` | `AlertProcessor` 接口新增 `ProcessExternal()` 方法 |

---

## 详细实施任务

---

### Task 1: 新增 AlertSource 数据模型

**文件:**
- 创建: `models/alert/alert_source.go`

- [ ] **Step 1: 创建 AlertSource 模型文件**

```go
package alert

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// EnrichmentStrategy 外部告警的富化策略
type EnrichmentStrategy string

const (
	StrategyNodeAffinity       EnrichmentStrategy = "node_affinity"        // 物理节点 → Node → Pod → BusinessApp
	StrategyServiceGraph       EnrichmentStrategy = "service_graph"        // 服务名 → BusinessApp → CallsApp 上下游
	StrategyDirectBusinessApp  EnrichmentStrategy = "direct_business_app"  // 应用名 → BusinessApp 直接匹配
	StrategyBusinessLabels     EnrichmentStrategy = "business_labels"      // 直接从标签提取业务信息（路径 B）
	StrategyNone               EnrichmentStrategy = "none"                 // 不做富化，仅透传
)

// AlertSource 外部告警源配置
type AlertSource struct {
	Name               string               `json:"name" yaml:"name"`
	Type               string               `json:"type" yaml:"type"`                               // ceph/middleware/database/network/application/custom
	DisplayName        string               `json:"displayName" yaml:"displayName"`                 // 显示名称
	LabelMapping       map[string]string    `json:"labelMapping" yaml:"labelMapping"`               // 外部标签名 → 内部标签名
	EnrichmentStrategy EnrichmentStrategy   `json:"enrichmentStrategy" yaml:"enrichmentStrategy"`   // 富化策略
	BusinessMapping    BusinessMappingConf  `json:"businessMapping" yaml:"businessMapping"`         // 默认业务归属（兜底）
	FingerprintKeys    []string             `json:"fingerprintKeys" yaml:"fingerprintKeys"`         // 用于生成指纹的标签键（默认: alertname, instance）
}

// BusinessMappingConf 默认业务归属配置
type BusinessMappingConf struct {
	Team        string `json:"team" yaml:"team"`
	BusinessUnit string `json:"businessUnit" yaml:"businessUnit"`
	Criticality string `json:"criticality" yaml:"criticality"`
	ServiceType string `json:"serviceType" yaml:"serviceType"` // middleware/database/cache
	AppName     string `json:"appName" yaml:"appName"`         // 直接指定 BusinessApp（用于 strategy=direct_business_app）
}

// MapToAlert 将外部告警的原始 payload 映射为标准 Alert
// raw 是外部系统发来的 JSON 对象（已解析为 map[string]interface{}）
func (s *AlertSource) MapToAlert(raw map[string]interface{}) *Alert {
	labels := make(map[string]string)
	annotations := make(map[string]string)

	for internalKey, externalKey := range s.LabelMapping {
		val := deepGet(raw, externalKey)
		if val != "" {
			labels[internalKey] = val
		}
	}

	// 注入 source 标识
	labels["alertSource"] = s.Name
	labels["alertSourceType"] = s.Type

	// 兜底: 注入 businessMapping 中的默认值
	if s.BusinessMapping.Team != "" {
		labels["team"] = s.BusinessMapping.Team
	}
	if s.BusinessMapping.BusinessUnit != "" {
		labels["businessUnit"] = s.BusinessMapping.BusinessUnit
	}
	if s.BusinessMapping.Criticality != "" {
		labels["criticality"] = s.BusinessMapping.Criticality
	}
	if s.BusinessMapping.AppName != "" {
		labels["appName"] = s.BusinessMapping.AppName
	}

	// 提取 annotations
	if desc, ok := raw["description"]; ok {
		if s, ok := desc.(string); ok {
			annotations["description"] = s
		}
	}
	if summary, ok := raw["summary"]; ok {
		if s, ok := summary.(string); ok {
			annotations["summary"] = s
		}
	}
	// 所有未映射的字段放入 annotations
	for k, v := range raw {
		if _, isMapped := s.LabelMapping[k]; !isMapped && k != "description" && k != "summary" {
			annotations[k] = fmt.Sprint(v)
		}
	}

	// 确定 status
	status := "firing"
	if st, ok := raw["status"]; ok {
		if s, ok := st.(string); ok && strings.ToLower(s) == "resolved" {
			status = "resolved"
		}
	}

	fingerprint := s.GenerateFingerprint(raw)

	return &Alert{
		Status:      status,
		Labels:      labels,
		Annotations: annotations,
		Fingerprint: fingerprint,
	}
}

// GenerateFingerprint 根据配置的 fingerprintKeys 生成唯一指纹
// 默认使用 alertname + instance；无匹配键时回退到 payload 全量 hash
func (s *AlertSource) GenerateFingerprint(raw map[string]interface{}) string {
	keys := s.FingerprintKeys
	if len(keys) == 0 {
		keys = []string{"alertname", "instance"}
	}

	var parts []string
	for _, key := range keys {
		// 先尝试从 LabelMapping 中找到对应的外部键名
		externalKey := key
		if ek, ok := s.LabelMapping[key]; ok {
			externalKey = ek
		}
		val := deepGet(raw, externalKey)
		if val == "" {
			val = deepGet(raw, key) // 回退：直接用 key 查找
		}
		if val != "" {
			parts = append(parts, val)
		}
	}

	if len(parts) == 0 {
		// 回退：使用整个 payload 的 hash
		payloadBytes, _ := json.Marshal(raw)
		h := sha256.Sum256(payloadBytes)
		return fmt.Sprintf("%x", h[:8])
	}

	combined := strings.Join(parts, "|")
	h := sha256.Sum256([]byte(combined))
	return fmt.Sprintf("%s-%x", s.Name, h[:8])
}

// deepGet 从嵌套 map 中获取点分隔路径的值
// 例如 deepGet(raw, "labels.alertname") 返回 raw["labels"]["alertname"]
func deepGet(data map[string]interface{}, path string) string {
	parts := strings.Split(path, ".")
	var current interface{} = data

	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		current = m[part]
		if current == nil {
			return ""
		}
	}

	switch v := current.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%v", v)
	case int:
		return fmt.Sprintf("%d", v)
	case bool:
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprint(v)
	}
}

// ExtractBusinessLabels 从告警标签中提取业务上下文（路径 B: 监控侧已打标签）
// 当告警已经携带 team/businessUnit/criticality/appName 等标签时，
// 直接构建 BusinessContext，无需 NebulaGraph 反查。
func (s *AlertSource) ExtractBusinessLabels(labels map[string]string) *BusinessContext {
	// 尝试从 LabelMapping 的反向映射或直接标签读取
	getVal := func(internalKeys ...string) string {
		for _, k := range internalKeys {
			if v, ok := labels[k]; ok && v != "" {
				return v
			}
		}
		return ""
	}

	appName := getVal("appName", "app_name", "application", "service")
	team := getVal("team")
	businessUnit := getVal("businessUnit", "business_unit")
	criticality := getVal("criticality")
	environment := getVal("environment", "env")

	if appName == "" && team == "" && businessUnit == "" {
		return nil // 没有足够的业务标签
	}

	return &BusinessContext{
		AppName:      appName,
		Team:         team,
		BusinessUnit: businessUnit,
		Criticality:  criticality,
		Environment:  environment,
		Source:       s.Name,
		ServiceType:  s.BusinessMapping.ServiceType,
	}
}
```

- [ ] **Step 2: 验证编译通过**

```bash
cd /Users/tddh/code/mutong && go build ./models/alert/
```
期望: 无编译错误

- [ ] **Step 3: 编写单元测试**

创建 `models/alert/alert_source_test.go`:

```go
package alert

import (
	"testing"
)

func TestMapToAlert_BasicLabelMapping(t *testing.T) {
	source := &AlertSource{
		Name: "ceph-prod",
		Type: "ceph",
		LabelMapping: map[string]string{
			"alertname":    "alert_name",
			"severity":     "severity",
			"node":         "host",
			"resourceType": "resourceType", // 静态值
		},
		EnrichmentStrategy: StrategyNodeAffinity,
		BusinessMapping: BusinessMappingConf{
			Team:       "storage-team",
			Criticality: "high",
		},
	}

	raw := map[string]interface{}{
		"alert_name": "CephOSDNearFull",
		"severity":   "critical",
		"host":       "node-12",
		"osd_id":     "42",
		"pool":       "cephfs_data",
	}

	alert := source.MapToAlert(raw)

	if alert.Labels["alertname"] != "CephOSDNearFull" {
		t.Errorf("expected alertname=CephOSDNearFull, got %s", alert.Labels["alertname"])
	}
	if alert.Labels["node"] != "node-12" {
		t.Errorf("expected node=node-12, got %s", alert.Labels["node"])
	}
	if alert.Labels["team"] != "storage-team" {
		t.Errorf("expected team=storage-team, got %s", alert.Labels["team"])
	}
	if alert.Labels["alertSource"] != "ceph-prod" {
		t.Errorf("expected alertSource=ceph-prod, got %s", alert.Labels["alertSource"])
	}
	if alert.Fingerprint == "" {
		t.Error("expected non-empty fingerprint")
	}
}

func TestGenerateFingerprint_Deterministic(t *testing.T) {
	source := &AlertSource{
		Name: "test",
		LabelMapping: map[string]string{
			"alertname": "alert_name",
		},
	}

	raw := map[string]interface{}{
		"alert_name": "TestAlert",
		"instance":   "10.0.0.1:9090",
	}

	fp1 := source.GenerateFingerprint(raw)
	fp2 := source.GenerateFingerprint(raw)

	if fp1 != fp2 {
		t.Errorf("fingerprints should be deterministic: %s vs %s", fp1, fp2)
	}
}

func TestExtractBusinessLabels_DirectMatch(t *testing.T) {
	source := &AlertSource{
		Name:               "kafka-broker",
		Type:               "middleware",
		EnrichmentStrategy: StrategyBusinessLabels,
		BusinessMapping: BusinessMappingConf{
			ServiceType: "middleware",
		},
	}

	labels := map[string]string{
		"alertname":    "KafkaBrokerDown",
		"team":         "data-platform-team",
		"businessUnit": "数据基础设施",
		"criticality":  "P0",
		"appName":      "kafka-cluster",
	}

	bc := source.ExtractBusinessLabels(labels)
	if bc == nil {
		t.Fatal("expected non-nil BusinessContext")
	}
	if bc.AppName != "kafka-cluster" {
		t.Errorf("expected appName=kafka-cluster, got %s", bc.AppName)
	}
	if bc.Team != "data-platform-team" {
		t.Errorf("expected team=data-platform-team, got %s", bc.Team)
	}
	if bc.BusinessUnit != "数据基础设施" {
		t.Errorf("expected businessUnit=数据基础设施, got %s", bc.BusinessUnit)
	}
}

func TestExtractBusinessLabels_NilWhenEmpty(t *testing.T) {
	source := &AlertSource{Name: "test"}
	labels := map[string]string{
		"alertname": "SomeAlert",
		"severity":  "warning",
	}
	bc := source.ExtractBusinessLabels(labels)
	if bc != nil {
		t.Error("expected nil BusinessContext when no business labels present")
	}
}

func TestDeepGet_NestedPath(t *testing.T) {
	data := map[string]interface{}{
		"labels": map[string]interface{}{
			"alertname": "TestAlert",
		},
	}

	result := deepGet(data, "labels.alertname")
	if result != "TestAlert" {
		t.Errorf("expected TestAlert, got %s", result)
	}

	empty := deepGet(data, "labels.nonexistent")
	if empty != "" {
		t.Errorf("expected empty string, got %s", empty)
	}
}
```

- [ ] **Step 4: 运行测试**

```bash
cd /Users/tddh/code/mutong && go test ./models/alert/ -v -run "TestMapToAlert|TestGenerateFingerprint|TestExtractBusinessLabels|TestDeepGet"
```
期望: 所有测试 PASS

- [ ] **Step 5: 提交**

```bash
git add models/alert/alert_source.go models/alert/alert_source_test.go
git commit -m "feat(alert): 新增 AlertSource 外部告警源数据模型

- AlertSource 配置驱动: labelMapping + enrichmentStrategy + businessMapping
- MapToAlert(): 外部 JSON → 标准 Alert 转换
- GenerateFingerprint(): 基于配置键的确定性指纹生成
- ExtractBusinessLabels(): 从标签直接提取业务上下文（路径 B）
- deepGet(): 点分隔路径的嵌套 map 取值"
```

---

### Task 2: 扩展配置系统 — AlertSource 配置加载

**文件:**
- 修改: `config/config_base.go`
- 修改: `config/config.go`
- 修改: `configs/config.alert.yaml.example`
- 修改: `cmd/main.go`（注入 alertSources）

- [ ] **Step 1: 新增 AlertSourceConfig 结构体**

在 `config/config_base.go` 中的 `Config` 结构体添加告警源配置字段：

```go
// 在 config/config_base.go 中新增

// AlertSourceConfig 告警源配置段
type AlertSourceConfig struct {
	Sources []alert.AlertSource `mapstructure:"sources" yaml:"sources"`
}

// 在 Config 结构体中添加:
type Config struct {
	// ... 现有字段 ...
	AlertSources AlertSourceConfig `mapstructure:"alertSources" yaml:"alertSources"`
}
```

- [ ] **Step 2: 新增 alertSources 配置模板**

在 `configs/config.alert.yaml.example` 末尾追加:

```yaml
  # ============================================================
  # 外部告警源配置（alertSources）
  # ============================================================
  # 每种外部系统定义一个告警源，通过 labelMapping 将外部标签映射到内部标准标签。
  # enrichmentStrategy 决定了如何通过 NebulaGraph 反查业务归属。
  #
  # 富化策略说明:
  #   node_affinity:        host/node → K8s Node → Pod → BusinessApp（物理节点桥接）
  #   service_graph:        serviceName → BusinessApp → CallsApp 上下游（服务拓扑桥接）
  #   direct_business_app:  appName → BusinessApp 直接匹配
  #   business_labels:      直接从标签提取 team/businessUnit/appName（Prometheus 侧已打标签）
  #   none:                 不做业务富化，仅透传
  #
  # 指纹生成:
  #   fingerprintKeys 指定用于生成唯一指纹的标签键。告警指纹用于去重和关联。
  #   默认使用 alertname + instance，可根据实际告警格式自定义。

alertSources:
  sources:
    # 示例1: Ceph 存储告警（node_affinity 策略）
    - name: "ceph-prod"
      type: "ceph"
      displayName: "Ceph 生产集群"
      labelMapping:
        alertname: "alert_name"
        severity: "severity"
        node: "host"                    # ★ 桥接到 K8s 物理节点
        custom_pool: "pool"
        custom_osd_id: "osd_id"
      enrichmentStrategy: "node_affinity"
      businessMapping:
        team: "storage-team"
        criticality: "high"
      fingerprintKeys:
        - "alert_name"
        - "host"
        - "osd_id"

    # 示例2: Kafka 中间件告警（service_graph 策略）
    - name: "kafka-broker"
      type: "middleware"
      displayName: "Kafka 消息队列"
      labelMapping:
        alertname: "alert_name"
        severity: "severity"
        serviceName: "kafka"            # ★ 桥接到 BusinessApp
        custom_topic: "topic"
        custom_consumer_group: "consumer_group"
      enrichmentStrategy: "service_graph"
      businessMapping:
        team: "data-platform-team"
        criticality: "high"
        serviceType: "middleware"
      fingerprintKeys:
        - "alert_name"
        - "topic"
        - "consumer_group"

    # 示例3: 通用应用层告警（direct_business_app 策略）
    - name: "app-generic"
      type: "application"
      displayName: "通用应用告警"
      labelMapping:
        alertname: "alert_name"
        severity: "severity"
        appName: "service"              # ★ 直接映射到 BusinessApp
      enrichmentStrategy: "direct_business_app"
      businessMapping:
        criticality: "medium"
      fingerprintKeys:
        - "alert_name"
        - "service"

    # 示例4: Prometheus 侧已打标签（business_labels 策略 - 路径 B）
    - name: "prometheus-labeled"
      type: "prometheus"
      displayName: "Prometheus 标签告警"
      labelMapping:
        alertname: "alertname"
        severity: "severity"
      enrichmentStrategy: "business_labels"
      businessMapping:
        serviceType: "application"
      fingerprintKeys:
        - "alertname"
        - "instance"
```

- [ ] **Step 3: 加载告警源配置**

在 `config/config.go` 的 `LoadConfig()` 方法中添加:

```go
// 在 LoadConfig 中加载 alertSources 配置
func (c *Config) loadAlertSources(v *viper.Viper) error {
	if err := v.UnmarshalKey("alertSources", &c.AlertSources); err != nil {
		return fmt.Errorf("failed to unmarshal alertSources: %w", err)
	}
	// 验证: enrichmentStrategy 必须合法
	validStrategies := map[string]bool{
		"node_affinity":        true,
		"service_graph":        true,
		"direct_business_app":  true,
		"business_labels":      true,
		"none":                 true,
		"":                     true, // 空值回退到 node_affinity
	}
	for i, src := range c.AlertSources.Sources {
		if src.Name == "" {
			return fmt.Errorf("alertSources.sources[%d]: name is required", i)
		}
		if _, ok := validStrategies[string(src.EnrichmentStrategy)]; !ok {
			return fmt.Errorf("alertSources.sources[%d] (%s): unknown enrichmentStrategy '%s'",
				i, src.Name, src.EnrichmentStrategy)
		}
		if src.EnrichmentStrategy == "" {
			c.AlertSources.Sources[i].EnrichmentStrategy = "node_affinity" // 默认策略
		}
	}
	return nil
}
```

在 `LoadConfig()` 方法中调用:

```go
// 在现有的配置加载逻辑之后
if err := c.loadAlertSources(v); err != nil {
    return fmt.Errorf("load alertSources: %w", err)
}
```

- [ ] **Step 4: 注入 alertSources 到控制器和服务**

在 `config/config.go` 的 `initializeServices()` 或 `InitializeAlertPipeline()` 中：

```go
// 构建 alertSources map (name → *AlertSource)
alertSources := make(map[string]*alert.AlertSource)
for i := range cfg.AlertSources.Sources {
    alertSources[cfg.AlertSources.Sources[i].Name] = &cfg.AlertSources.Sources[i]
}

// 注入到 AlertController
alertCtrl := controllers.NewAlertController(logger, alertSvc, alertSources)

// 注入到 AlertService (用于 ProcessExternal)
alertSvc.SetAlertSources(alertSources)
```

- [ ] **Step 5: 验证编译**

```bash
cd /Users/tddh/code/mutong && go build ./...
```
期望: 无编译错误

- [ ] **Step 6: 提交**

```bash
git add config/config_base.go config/config.go configs/config.alert.yaml.example cmd/main.go
git commit -m "feat(config): 新增 alertSources 外部告警源配置支持

- Config 新增 AlertSourceConfig 结构体
- loadAlertSources() 从配置文件加载并校验
- 注入到 AlertController 和 AlertService
- config.alert.yaml.example 含 Ceph/Kafka/通用 三类示例"
```

---

### Task 3: 新增外部告警 Webhook 端点

**文件:**
- 修改: `controllers/alert_controller.go`

- [ ] **Step 1: 增强 AlertController 结构体**

```go
// 修改 AlertController 结构体，新增 alertSources
type AlertController struct {
	logger       *zap.Logger
	processor    alert_interfaces.AlertProcessor
	alertSources map[string]*alert_models.AlertSource  // 新增
}

// 修改构造函数
func NewAlertController(
	logger *zap.Logger,
	processor alert_interfaces.AlertProcessor,
	alertSources map[string]*alert_models.AlertSource,  // 新增参数
) *AlertController {
	return &AlertController{
		logger:       logger,
		processor:    processor,
		alertSources: alertSources,
	}
}
```

- [ ] **Step 2: 新增 HandleExternalWebhook 处理器**

```go
// HandleExternalWebhook 处理外部系统告警
// POST /api/v1/alerts/external/:source
func (c *AlertController) HandleExternalWebhook(ctx *gin.Context) {
	sourceName := ctx.Param("source")
	if sourceName == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "source parameter is required"})
		return
	}

	// 1. 查找告警源配置
	source, ok := c.alertSources[sourceName]
	if !ok {
		c.logger.Warn("Unknown alert source",
			zap.String("source", sourceName),
			zap.Strings("available", c.availableSources()))
		ctx.JSON(http.StatusNotFound, gin.H{
			"error":           fmt.Sprintf("unknown alert source: %s", sourceName),
			"availableSources": c.availableSources(),
		})
		return
	}

	// 2. 解析原始 Payload（通用 JSON）
	var rawPayload map[string]interface{}
	if err := ctx.ShouldBindJSON(&rawPayload); err != nil {
		c.logger.Error("Failed to parse external alert payload",
			zap.String("source", sourceName),
			zap.Error(err))
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid JSON payload",
		})
		return
	}

	c.logger.Info("Received external alert",
		zap.String("source", sourceName),
		zap.String("type", source.Type),
		zap.String("strategy", string(source.EnrichmentStrategy)))

	// 3. 标签映射 → 标准 Alert
	alert := source.MapToAlert(rawPayload)

	// 4. 标准告警源注入（用于后续处理差异识别）
	alert.Labels["alertSourceName"] = sourceName
	alert.Labels["alertSourceType"] = source.Type
	alert.Labels["enrichmentStrategy"] = string(source.EnrichmentStrategy)

	// 5. 走 ProcessExternal 专用流水线（带 source 上下文）
	processedAlerts, err := c.processor.ProcessExternal(
		ctx.Request.Context(), alert, source)
	if err != nil {
		c.logger.Error("Failed to process external alert",
			zap.String("source", sourceName),
			zap.String("fingerprint", alert.Fingerprint),
			zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to process alert",
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":      "success",
		"source":      sourceName,
		"fingerprint": alert.Fingerprint,
		"processed":   len(processedAlerts),
	})
}

// availableSources 返回所有已注册的告警源名称
func (c *AlertController) availableSources() []string {
	names := make([]string, 0, len(c.alertSources))
	for name := range c.alertSources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
```

- [ ] **Step 3: 注册新路由**

在 `RegisterRoutes()` 方法中添加:

```go
func (c *AlertController) RegisterRoutes(app *gin.Engine) {
	alerts := app.Group("/api/v1/alerts")
	{
		// 具体路径优先注册
		alerts.POST("/webhook", c.HandleWebhook)
		alerts.POST("/external/:source", c.HandleExternalWebhook)  // 新增
		alerts.GET("/", c.GetActiveAlerts)
		alerts.GET("/health", c.HealthCheck)
		alerts.GET("/sources", c.ListAlertSources)                 // 新增：列出所有告警源
		// 通配符路由最后注册
		alerts.GET("/:fingerprint", c.GetAlertByFingerprint)
	}
	app.GET("/metrics", gin.WrapH(promhttp.Handler()))
}

// ListAlertSources 列出所有已注册的外部告警源
// GET /api/v1/alerts/sources
func (c *AlertController) ListAlertSources(ctx *gin.Context) {
	type SourceInfo struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		DisplayName string `json:"displayName"`
		Strategy  string `json:"strategy"`
	}
	sources := make([]SourceInfo, 0, len(c.alertSources))
	for _, s := range c.alertSources {
		sources = append(sources, SourceInfo{
			Name:        s.Name,
			Type:        s.Type,
			DisplayName: s.DisplayName,
			Strategy:    string(s.EnrichmentStrategy),
		})
	}
	ctx.JSON(http.StatusOK, gin.H{
		"sources": sources,
		"count":   len(sources),
	})
}
```

- [ ] **Step 4: 验证编译**

```bash
cd /Users/tddh/code/mutong && go build ./controllers/
```
期望: 无编译错误

- [ ] **Step 5: 提交**

```bash
git add controllers/alert_controller.go
git commit -m "feat(alert): 新增 POST /api/v1/alerts/external/:source 端点

- HandleExternalWebhook: 通用外部告警 Webhook 处理
- ListAlertSources: 列出所有注册的告警源
- 支持 source 参数路由到对应 AlertSource 配置
- 复用 MapToAlert() 做标签映射"
```

---

### Task 4: 扩展 Enricher — 四种桥接策略

**文件:**
- 修改: `services/alert/enricher.go`

- [ ] **Step 1: 新增 enrichFromExternal 入口方法**

在 `enricher.go` 中 `enrichFromService` 方法之后新增:

```go
// enrichFromExternal 处理外部告警的富化
// 根据 AlertSource.EnrichmentStrategy 选择桥接策略
func (e *AlertEnricher) enrichFromExternal(
	ctx context.Context,
	enriched *alert_models.EnrichedAlert,
	source *alert_models.AlertSource,
) {
	e.logger.Debug("Enriching external alert",
		zap.String("source", source.Name),
		zap.String("strategy", string(source.EnrichmentStrategy)),
		zap.String("fingerprint", enriched.Fingerprint))

	switch source.EnrichmentStrategy {
	case alert_models.StrategyNodeAffinity:
		e.enrichByNodeAffinity(ctx, enriched, source)
	case alert_models.StrategyServiceGraph:
		e.enrichByServiceGraph(ctx, enriched, source)
	case alert_models.StrategyDirectBusinessApp:
		e.enrichByDirectBusinessApp(ctx, enriched, source)
	case alert_models.StrategyBusinessLabels:
		e.enrichFromBusinessLabels(enriched, source)
	case alert_models.StrategyNone:
		e.logger.Debug("No enrichment strategy configured, skipping")
	default:
		e.logger.Warn("Unknown enrichment strategy, falling back to node_affinity",
			zap.String("strategy", string(source.EnrichmentStrategy)))
		e.enrichByNodeAffinity(ctx, enriched, source)
	}
}
```

- [ ] **Step 2: 实现 enrichByNodeAffinity（物理节点桥接）**

```go
// enrichByNodeAffinity 通过物理节点反查业务归属
// 适用场景: Ceph OSD、物理机硬件监控、网络设备
// 查询路径: node → K8sResource(Node) → Pod (RunsOn) → BusinessApp (BelongsToApp)
func (e *AlertEnricher) enrichByNodeAffinity(
	ctx context.Context,
	enriched *alert_models.EnrichedAlert,
	source *alert_models.AlertSource,
) {
	nodeName := enriched.Labels["node"]
	if nodeName == "" {
		e.logger.Warn("node_affinity strategy requires 'node' label, skipping",
			zap.String("source", source.Name))
		return
	}

	// 复用现有的 buildBusinessImpactFromNode 和 queryTeamsOnNode
	enriched.BusinessImpact = e.buildBusinessImpactFromNode(ctx, nodeName)

	// 查找该节点上运行的 BusinessApp
	query := fmt.Sprintf(`
		MATCH (n:K8sResource{kind:'Node',name:%s,is_deleted:false})
		MATCH (p:K8sResource{kind:'Pod',is_deleted:false})-[:RunsOn]->(n)
		MATCH (p)-[:BelongsToApp]->(b:BusinessApp)
		RETURN DISTINCT b.BusinessApp.uid AS uid,
			b.BusinessApp.app_name AS appName,
			b.BusinessApp.team AS team,
			b.BusinessApp.namespace AS namespace,
			b.BusinessApp.criticality AS criticality,
			b.BusinessApp.environment AS environment,
			b.BusinessApp.business_unit AS businessUnit
		LIMIT 10
	`, strconv.Quote(nodeName))

	resultSet, err := e.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet.GetRowSize() == 0 {
		e.logger.Warn("No BusinessApp found on node, using businessMapping defaults",
			zap.String("node", nodeName),
			zap.Error(err))

		// 兜底: 使用 BusinessMapping 配置
		enriched.BusinessContext = alert_models.BusinessContext{
			Team:         source.BusinessMapping.Team,
			BusinessUnit: source.BusinessMapping.BusinessUnit,
			Criticality:  source.BusinessMapping.Criticality,
			Source:       source.Name,
			ServiceType:  source.BusinessMapping.ServiceType,
		}
		enriched.EnrichTags["businessSource"] = source.Name
		enriched.EnrichTags["team"] = source.BusinessMapping.Team
		return
	}

	// 取第一个 BusinessApp 作为主要业务上下文
	row, _ := resultSet.GetRowValuesByIndex(0)
	bc := alert_models.BusinessContext{
		UID:          colStr(row, "uid"),
		AppName:      colStr(row, "appName"),
		Namespace:    colStr(row, "namespace"),
		Team:         colStr(row, "team"),
		BusinessUnit: colStr(row, "businessUnit"),
		Criticality:  colStr(row, "criticality"),
		Environment:  colStr(row, "environment"),
		Source:       source.Name,
		ServiceType:  source.BusinessMapping.ServiceType,
	}

	enriched.BusinessContext = bc
	enriched.ResourceType = "Node"
	enriched.ResourceName = nodeName
	enriched.NodeName = nodeName
	enriched.EnrichTags["businessSource"] = source.Name
	enriched.EnrichTags["businessApp"] = bc.AppName
	enriched.EnrichTags["team"] = bc.Team
	enriched.EnrichTags["businessUnit"] = bc.BusinessUnit

	// 补充 Stakeholder 信息
	e.enrichBizCalls(enriched)
	e.enrichStakeholders(enriched)
}
```

- [ ] **Step 3: 实现 enrichByServiceGraph（服务拓扑桥接）**

```go
// enrichByServiceGraph 通过服务名在拓扑图中查找业务应用
// 适用场景: Kafka、MySQL、Redis、Elasticsearch 等中间件
// 查询路径: serviceName → BusinessApp (标签匹配) → CallsApp 上下游
func (e *AlertEnricher) enrichByServiceGraph(
	ctx context.Context,
	enriched *alert_models.EnrichedAlert,
	source *alert_models.AlertSource,
) {
	serviceName := enriched.Labels["serviceName"]
	if serviceName == "" {
		// 回退: 尝试从已知标签提取
		serviceName = enriched.Labels["appName"]
	}
	if serviceName == "" {
		e.logger.Warn("service_graph strategy requires 'serviceName' or 'appName' label",
			zap.String("source", source.Name))
		return
	}

	// 查询 BusinessApp（按 app_name 精确匹配或模糊匹配）
	query := fmt.Sprintf(`
		MATCH (b:BusinessApp)
		WHERE b.BusinessApp.app_name == %s
		RETURN b.BusinessApp.uid AS uid,
			b.BusinessApp.app_name AS appName,
			b.BusinessApp.team AS team,
			b.BusinessApp.namespace AS namespace,
			b.BusinessApp.criticality AS criticality,
			b.BusinessApp.environment AS environment,
			b.BusinessApp.business_unit AS businessUnit
		LIMIT 1
	`, strconv.Quote(serviceName))

	resultSet, err := e.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet.GetRowSize() == 0 {
		e.logger.Warn("BusinessApp not found for service, using businessMapping",
			zap.String("serviceName", serviceName))
		// 兜底
		enriched.BusinessContext = alert_models.BusinessContext{
			AppName:     serviceName,
			Team:        source.BusinessMapping.Team,
			BusinessUnit: source.BusinessMapping.BusinessUnit,
			Criticality: source.BusinessMapping.Criticality,
			Source:      source.Name,
			ServiceType: source.BusinessMapping.ServiceType,
		}
		enriched.EnrichTags["businessSource"] = source.Name
		return
	}

	row, _ := resultSet.GetRowValuesByIndex(0)
	bc := alert_models.BusinessContext{
		UID:          colStr(row, "uid"),
		AppName:      colStr(row, "appName"),
		Namespace:    colStr(row, "namespace"),
		Team:         colStr(row, "team"),
		BusinessUnit: colStr(row, "businessUnit"),
		Criticality:  colStr(row, "criticality"),
		Environment:  colStr(row, "environment"),
		Source:       source.Name,
		ServiceType:  source.BusinessMapping.ServiceType,
	}

	enriched.BusinessContext = bc
	enriched.EnrichTags["businessSource"] = source.Name
	enriched.EnrichTags["businessApp"] = bc.AppName
	enriched.EnrichTags["team"] = bc.Team
	enriched.EnrichTags["businessUnit"] = bc.BusinessUnit

	// 自动发现上下游依赖方作为 Stakeholder
	e.enrichBizCalls(enriched)
	e.enrichStakeholders(enriched)
}
```

- [ ] **Step 4: 实现 enrichByDirectBusinessApp（直接业务匹配）**

```go
// enrichByDirectBusinessApp 通过 appName 直接匹配 BusinessApp
// 适用场景: 告警中已经携带了明确的业务应用名称
func (e *AlertEnricher) enrichByDirectBusinessApp(
	ctx context.Context,
	enriched *alert_models.EnrichedAlert,
	source *alert_models.AlertSource,
) {
	appName := enriched.Labels["appName"]
	if appName == "" {
		appName = source.BusinessMapping.AppName
	}
	if appName == "" {
		e.logger.Warn("direct_business_app strategy requires 'appName' label or businessMapping.appName",
			zap.String("source", source.Name))
		return
	}

	// 直接构建 BusinessContext
	bc := alert_models.BusinessContext{
		AppName:      appName,
		Team:         enriched.Labels["team"],
		BusinessUnit: enriched.Labels["businessUnit"],
		Criticality:  enriched.Labels["criticality"],
		Source:       source.Name,
		ServiceType:  source.BusinessMapping.ServiceType,
	}

	// 如果标签中没有 team/criticality，用 businessMapping 兜底
	if bc.Team == "" {
		bc.Team = source.BusinessMapping.Team
	}
	if bc.Criticality == "" {
		bc.Criticality = source.BusinessMapping.Criticality
	}
	if bc.BusinessUnit == "" {
		bc.BusinessUnit = source.BusinessMapping.BusinessUnit
	}

	// 尝试从 NebulaGraph 补全信息
	query := fmt.Sprintf(`
		MATCH (b:BusinessApp{app_name:%s})
		RETURN b.BusinessApp.uid AS uid,
			b.BusinessApp.namespace AS namespace,
			b.BusinessApp.environment AS environment
		LIMIT 1
	`, strconv.Quote(appName))

	if resultSet, err := e.graphDB.ExecuteAndCheck(query); err == nil && resultSet.GetRowSize() > 0 {
		row, _ := resultSet.GetRowValuesByIndex(0)
		bc.UID = colStr(row, "uid")
		bc.Namespace = colStr(row, "namespace")
		if bc.Environment == "" {
			bc.Environment = colStr(row, "environment")
		}
	}

	enriched.BusinessContext = bc
	enriched.EnrichTags["businessSource"] = source.Name
	enriched.EnrichTags["businessApp"] = bc.AppName
	enriched.EnrichTags["team"] = bc.Team
	enriched.EnrichTags["businessUnit"] = bc.BusinessUnit

	e.enrichBizCalls(enriched)
	e.enrichStakeholders(enriched)
}
```

- [ ] **Step 5: 实现 enrichFromBusinessLabels（路径 B — 标签直接提取）**

```go
// enrichFromBusinessLabels 从告警标签直接提取业务上下文
// 适用场景: Prometheus 侧已经通过 relabel 打好了 team/businessUnit/appName 等标签
// 这是"路径 B"的专属实现，跳过 NebulaGraph 反查
func (e *AlertEnricher) enrichFromBusinessLabels(
	enriched *alert_models.EnrichedAlert,
	source *alert_models.AlertSource,
) {
	bc := source.ExtractBusinessLabels(enriched.Labels)
	if bc == nil {
		e.logger.Warn("No business labels found in alert, using businessMapping defaults",
			zap.String("source", source.Name))
		// 兜底
		enriched.BusinessContext = alert_models.BusinessContext{
			Team:         source.BusinessMapping.Team,
			BusinessUnit: source.BusinessMapping.BusinessUnit,
			Criticality:  source.BusinessMapping.Criticality,
			Source:       source.Name,
			ServiceType:  source.BusinessMapping.ServiceType,
		}
		enriched.EnrichTags["businessSource"] = source.Name
		return
	}

	enriched.BusinessContext = *bc
	enriched.EnrichTags["businessSource"] = source.Name
	enriched.EnrichTags["businessApp"] = bc.AppName
	enriched.EnrichTags["team"] = bc.Team
	enriched.EnrichTags["businessUnit"] = bc.BusinessUnit

	// 如果有 appName 且 NebulaGraph 中存在，补全拓扑上下文
	if bc.AppName != "" && e.graphDB != nil {
		e.enrichBizCalls(enriched)
		e.enrichStakeholders(enriched)
	}

	e.logger.Debug("Enriched from business labels",
		zap.String("appName", bc.AppName),
		zap.String("team", bc.Team),
		zap.String("strategy", "business_labels"))
}
```

- [ ] **Step 6: 验证编译**

```bash
cd /Users/tddh/code/mutong && go build ./services/alert/
```
期望: 无编译错误

- [ ] **Step 7: 提交**

```bash
git add services/alert/enricher.go
git commit -m "feat(alert): 新增外部告警四种桥接富化策略

- enrichFromExternal(): 根据 enrichmentStrategy 分发
- enrichByNodeAffinity(): 物理节点 → Node → Pod → BusinessApp
- enrichByServiceGraph(): 服务名 → BusinessApp → CallsApp 上下游
- enrichByDirectBusinessApp(): 应用名直接匹配 BusinessApp
- enrichFromBusinessLabels(): 路径 B — 从标签直接提取业务上下文"
```

---

### Task 5: 扩展 AlertService — ProcessExternal 流水线

**文件:**
- 修改: `services/alert/alert_service.go`
- 修改: `interfaces/alert/processor.go`

- [ ] **Step 1: 扩展 AlertProcessor 接口**

在 `interfaces/alert/processor.go` 中新增 `ProcessExternal` 方法:

```go
// AlertProcessor 告警处理器接口
type AlertProcessor interface {
	// Process 处理 Alertmanager Webhook 告警（现有）
	Process(ctx context.Context, payload *alert.WebhookPayload) ([]*alert.ProcessedAlert, error)

	// ProcessExternal 处理外部系统告警（新增）
	// source 提供标签映射和富化策略上下文
	ProcessExternal(ctx context.Context, alert *alert.Alert, source *alert.AlertSource) ([]*alert.ProcessedAlert, error)

	// GetActiveAlerts 获取活跃告警
	GetActiveAlerts(ctx context.Context, filters map[string]string) ([]*alert.ProcessedAlert, error)

	// GetAlertByFingerprint 根据指纹获取告警
	GetAlertByFingerprint(ctx context.Context, fingerprint string) (*alert.ProcessedAlert, error)
}
```

- [ ] **Step 2: AlertService 新增 alertSources 字段和 SetAlertSources 方法**

```go
// 在 AlertService 结构体中添加
type AlertService struct {
	// ... 现有字段 ...
	alertSources map[string]*alert_models.AlertSource  // 新增
	bizCtxProvider interfaces.BusinessContextProvider    // 新增（如果尚未注入）
}

// SetAlertSources 注入告警源配置
func (s *AlertService) SetAlertSources(sources map[string]*alert_models.AlertSource) {
	s.alertSources = sources
}

// SetBizCtxProvider 注入业务上下文提供者
func (s *AlertService) SetBizCtxProvider(p interfaces.BusinessContextProvider) {
	s.bizCtxProvider = p
	// 如果 enricher 支持 WithBusinessContextProvider，同步注入
	if e, ok := s.enricher.(*AlertEnricher); ok {
		e.WithBusinessContextProvider(p)
	}
}
```

- [ ] **Step 3: 实现 ProcessExternal 方法**

```go
// ProcessExternal 处理单个外部告警
// 与 Process (批量) 不同，外部告警是逐条到达的
func (s *AlertService) ProcessExternal(
	ctx context.Context,
	alert *alert_models.Alert,
	source *alert_models.AlertSource,
) ([]*alert_models.ProcessedAlert, error) {
	s.logger.Info("Processing external alert",
		zap.String("source", source.Name),
		zap.String("type", source.Type),
		zap.String("strategy", string(source.EnrichmentStrategy)),
		zap.String("fingerprint", alert.Fingerprint))

	s.metrics.RecordReceived(alert.Status)

	// Step 1: 富化 — 使用外部告警专用富化流程
	enrichCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	enriched, err := s.enrichExternal(enrichCtx, alert, source)
	if err != nil || enriched == nil {
		s.logger.Warn("External alert enrich failed, using fallback",
			zap.String("fingerprint", alert.Fingerprint),
			zap.Error(err))
		enriched = &alert_models.EnrichedAlert{
			Alert: *alert,
		}
		// 确保至少有基本的 source 信息
		enriched.EnrichTags = map[string]string{
			"alertSource": source.Name,
		}
	}

	// Step 2: 抑制
	var suppression alert_models.SuppressionResult
	if s.suppressor != nil {
		suppression, err = s.suppressor.CheckSuppression(ctx, enriched)
		if err != nil {
			s.logger.Warn("Suppression check failed", zap.Error(err))
		}
	}

	// Step 3: 路由
	var routing alert_models.RoutingResult
	if s.router != nil {
		routing, err = s.router.Route(ctx, enriched)
		if err != nil {
			s.logger.Warn("Routing failed", zap.Error(err))
		}
	}

	// Step 4: 构建 ProcessedAlert
	processed := &alert_models.ProcessedAlert{
		EnrichedAlert: *enriched,
		Suppression:   suppression,
		Routing:       routing,
		ProcessedAt:   time.Now(),
	}

	// Step 5: 存储
	if s.storage != nil {
		if err := s.storage.Save(ctx, processed); err != nil {
			s.logger.Error("Failed to save external alert", zap.Error(err))
		}
	}

	// Step 6: 通知（未被抑制的告警）
	if !suppression.IsSuppressed && s.notifier != nil {
		if err := s.notifier.Notify(ctx, processed); err != nil {
			s.logger.Error("Failed to notify external alert", zap.Error(err))
			s.metrics.RecordNotified("failed")
		} else {
			s.metrics.RecordNotified(alert.Status)
		}
	} else if suppression.IsSuppressed {
		s.metrics.RecordSuppressed()
	}

	// Step 7: 聚合
	if s.aggregator != nil {
		s.aggregator.Add(processed)
	}

	// Step 8: 自动诊断触发
	if s.autoDiagnosisPipeline != nil && !suppression.IsSuppressed {
		s.logger.Debug("Triggering auto-diagnosis for external alert",
			zap.String("fingerprint", alert.Fingerprint))
		s.autoDiagnosisPipeline.OnAlertProcessed(ctx, processed)
	}

	return []*alert_models.ProcessedAlert{processed}, nil
}

// enrichExternal 外部告警专用的富化流程
func (s *AlertService) enrichExternal(
	ctx context.Context,
	alert *alert_models.Alert,
	source *alert_models.AlertSource,
) (*alert_models.EnrichedAlert, error) {
	// 构建基础 EnrichedAlert
	k8sLabels := alert_models.ExtractK8sLabels(alert.Labels)

	enriched := &alert_models.EnrichedAlert{
		Alert:         *alert,
		Namespace:     k8sLabels.Namespace,
		ResourceName:  alert.Labels["node"],
		ResourceType:  "Unknown",
		ResourceUID:   k8sLabels.ResourceUID,
		EnrichTags:    make(map[string]string),
		TopologyPath:  []string{},
		RelatedAlerts: []string{},
	}

	// 设置 source 标识
	enriched.EnrichTags["alertSource"] = source.Name
	enriched.EnrichTags["alertSourceType"] = source.Type

	// 根据 enrichmentStrategy 调用对应的富化方法
	if enricher, ok := s.enricher.(*AlertEnricher); ok {
		enricher.enrichFromExternal(ctx, enriched, source)
	} else {
		// 非 AlertEnricher 实现，走基本 Enrich 流程
		// 这种情况下只能依赖 alert.Labels 中已有的信息
		s.logger.Warn("Enricher is not AlertEnricher, skipping external enrichment")
	}

	return enriched, nil
}
```

- [ ] **Step 4: 验证编译和接口实现**

```bash
cd /Users/tddh/code/mutong && go build ./services/alert/ && go build ./interfaces/alert/
```
期望: 无编译错误

- [ ] **Step 5: 提交**

```bash
git add services/alert/alert_service.go interfaces/alert/processor.go
git commit -m "feat(alert): 新增 ProcessExternal 外部告警处理流水线

- AlertProcessor 接口新增 ProcessExternal() 方法
- AlertService 新增 alertSources/bizCtxProvider 注入
- ProcessExternal: Enrich → Suppress → Route → Notify 完整流水线
- enrichExternal: 根据 AlertSource.EnrichmentStrategy 分发富化策略
- 外部告警享受与 K8s 告警同等的抑制/路由/通知/自动诊断能力"
```

---

### Task 6: 集成 — 注入告警源到现有流水线

**文件:**
- 修改: `config/config.go`（InitializeAlertPipeline）
- 修改: `cmd/main.go`（路由注册）

- [ ] **Step 1: 在初始化流水线中注入 alertSources 和 bizCtxProvider**

在 `config/config.go` 的告警流水线初始化函数中:

```go
func InitializeAlertPipeline(cfg *Config, logger *zap.Logger) (*AlertPipeline, error) {
	// ... 现有的 graphDB, enricher, suppressor, router, notifier, storage 初始化 ...

	// 构建 alertSources map
	alertSources := make(map[string]*alert_model.AlertSource)
	for i := range cfg.AlertSources.Sources {
		src := &cfg.AlertSources.Sources[i]
		alertSources[src.Name] = src
	}

	// 注入 bizCtxProvider 到 enricher（如果尚未注入）
	bizCtxProvider := NewBusinessContextProvider(graphDB, cfg.BusinessTopology)
	if e, ok := enricher.(*alert_service.AlertEnricher); ok {
		e.WithBusinessContextProvider(bizCtxProvider)
	}

	// 创建 AlertService 并注入
	alertSvc := alert_service.NewAlertService(
		logger, graphDB, enricher, suppressor, router, notifier, storage, aggregator,
	)
	alertSvc.SetAlertSources(alertSources)
	alertSvc.SetBizCtxProvider(bizCtxProvider)
	// ... 其余初始化 ...

	return &AlertPipeline{
		Service:      alertSvc,
		AlertSources: alertSources,
		// ...
	}, nil
}
```

- [ ] **Step 2: 修改控制器构造函数调用**

在 `cmd/main.go` 中:

```go
// 之前
alertCtrl := controllers.NewAlertController(logger, alertSvc)

// 之后
alertCtrl := controllers.NewAlertController(logger, alertSvc, alertSources)
```

- [ ] **Step 3: 启动时打印已注册的告警源**

在 `cmd/main.go` 中的 `startServer()` 或 `initializeServices()` 之后:

```go
// 打印已注册的外部告警源
if len(alertSources) > 0 {
    logger.Info("Registered external alert sources",
        zap.Int("count", len(alertSources)))
    for name, src := range alertSources {
        logger.Info("  Alert source",
            zap.String("name", name),
            zap.String("type", src.Type),
            zap.String("strategy", string(src.EnrichmentStrategy)))
    }
} else {
    logger.Info("No external alert sources configured")
}
```

- [ ] **Step 4: 验证全量编译**

```bash
cd /Users/tddh/code/mutong && go build ./...
```
期望: 无编译错误

- [ ] **Step 5: 提交**

```bash
git add config/config.go cmd/main.go
git commit -m "feat(alert): 集成外部告警源到现有流水线

- InitializeAlertPipeline 注入 alertSources 和 bizCtxProvider
- AlertController 构造函数新增 alertSources 参数
- 启动时打印已注册的告警源列表"
```

---

### Task 7: 端到端测试

**文件:**
- 创建: `services/alert/external_integration_test.go`（集成测试）
- 创建: `docs/external-alert-integration-guide.md`（运维文档）

- [ ] **Step 1: 编写集成测试**

```go
package alert

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	alert_models "gitee.com/tddh/mutong/models/alert"
)

func TestExternalAlertWebhook_CephNodeAffinity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 准备: 创建 AlertSource 和 Mock Processor
	cephSource := &alert_models.AlertSource{
		Name:               "ceph-prod",
		Type:               "ceph",
		EnrichmentStrategy: alert_models.StrategyNodeAffinity,
		LabelMapping: map[string]string{
			"alertname": "alert_name",
			"severity":  "severity",
			"node":      "host",
		},
		BusinessMapping: alert_models.BusinessMappingConf{
			Team:       "storage-team",
			Criticality: "high",
		},
	}

	alertSources := map[string]*alert_models.AlertSource{
		"ceph-prod": cephSource,
	}

	// Mock processor
	mockProcessor := &mockAlertProcessor{
		processExternalFunc: func(ctx context.Context, alert *alert_models.Alert, source *alert_models.AlertSource) ([]*alert_models.ProcessedAlert, error) {
			// 验证标签映射
			if alert.Labels["alertname"] != "CephOSDNearFull" {
				t.Errorf("expected alertname=CephOSDNearFull, got %s", alert.Labels["alertname"])
			}
			if alert.Labels["node"] != "node-12" {
				t.Errorf("expected node=node-12, got %s", alert.Labels["node"])
			}
			if alert.Labels["team"] != "storage-team" {
				t.Errorf("expected team=storage-team, got %s", alert.Labels["team"])
			}
			return []*alert_models.ProcessedAlert{{
				EnrichedAlert: alert_models.EnrichedAlert{Alert: *alert},
			}}, nil
		},
	}

	logger := zap.NewNop()
	ctrl := &AlertController{
		logger:       logger,
		processor:    mockProcessor,
		alertSources: alertSources,
	}

	// 构造请求
	payload := map[string]interface{}{
		"alert_name": "CephOSDNearFull",
		"severity":   "critical",
		"host":       "node-12",
		"osd_id":     "42",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/v1/alerts/external/ceph-prod", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router := gin.New()
	router.POST("/api/v1/alerts/external/:source", ctrl.HandleExternalWebhook)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExternalAlertWebhook_UnknownSource(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctrl := &AlertController{
		logger:       zap.NewNop(),
		alertSources: map[string]*alert_models.AlertSource{},
	}

	req := httptest.NewRequest("POST", "/api/v1/alerts/external/unknown", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router := gin.New()
	router.POST("/api/v1/alerts/external/:source", ctrl.HandleExternalWebhook)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for unknown source, got %d", w.Code)
	}
}

func TestListAlertSources(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctrl := &AlertController{
		logger: zap.NewNop(),
		alertSources: map[string]*alert_models.AlertSource{
			"ceph-prod":    {Name: "ceph-prod", Type: "ceph", DisplayName: "Ceph 生产集群", EnrichmentStrategy: alert_models.StrategyNodeAffinity},
			"kafka-broker": {Name: "kafka-broker", Type: "middleware", DisplayName: "Kafka", EnrichmentStrategy: alert_models.StrategyServiceGraph},
		},
	}

	req := httptest.NewRequest("GET", "/api/v1/alerts/sources", nil)
	w := httptest.NewRecorder()

	router := gin.New()
	router.GET("/api/v1/alerts/sources", ctrl.ListAlertSources)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}
```

- [ ] **Step 2: 运行集成测试**

```bash
cd /Users/tddh/code/mutong && go test ./controllers/ -v -run "TestExternalAlert"
```
期望: 测试 PASS（注：可能需要调整 import 路径和 mock 实现）

- [ ] **Step 3: 编写运维文档**

创建 `docs/external-alert-integration-guide.md`:

```markdown
# 外部告警集成指南

## 概述

重明 (Mutong) 支持两种方式将外部系统（Ceph、Kafka、MySQL、网络设备等）的告警接入到统一告警流水线：

- **路径 A（通用）**: 外部系统 → mutong Webhook → AlertSource 标签映射 → NebulaGraph 拓扑反查 → 业务归属
- **路径 B（Prometheus 管理）**: Prometheus relabel 预打业务标签 → Alertmanager → mutong Webhook → 直接消费标签

## 路径 A: 外部系统直接接入 mutong

### 步骤 1: 注册告警源

在 `configs/config.alert.yaml` 中添加 `alertSources.sources` 配置段:

```yaml
alertSources:
  sources:
    - name: "my-system"
      type: "custom"                     # ceph/middleware/database/network/application/custom
      displayName: "我的系统"
      labelMapping:
        alertname: "alert_title"         # 外部标签名 → 内部标准标签
        severity: "level"
        node: "machine_ip"               # 桥接键（用于拓扑反查）
      enrichmentStrategy: "node_affinity" # 富化策略
      businessMapping:
        team: "my-team"
        criticality: "high"
      fingerprintKeys:
        - "alert_title"
        - "machine_ip"
```

### 步骤 2: 配置外部系统推送到 mutong

```bash
# 推送格式: POST /api/v1/alerts/external/:source
curl -X POST https://mutong.example.com/api/v1/alerts/external/my-system \
  -H "Content-Type: application/json" \
  -d '{
    "alert_title": "DiskNearFull",
    "level": "critical",
    "machine_ip": "10.0.0.12",
    "disk_usage": "95%"
  }'
```

### 富化策略选择

| 策略 | 适用场景 | 所需标签 | 原理 |
|------|---------|---------|------|
| `node_affinity` | 物理节点相关告警 | `node`（主机名/IP） | host → K8s Node → Pod → BusinessApp |
| `service_graph` | 中间件/数据库告警 | `serviceName`（服务名） | 服务名 → BusinessApp → 上下游依赖方 |
| `direct_business_app` | 应用层告警 | `appName`（应用名） | 直接匹配 BusinessApp |
| `business_labels` | Prometheus 已打标签 | `team`/`businessUnit`/`appName` | 直接从告警标签提取 |
| `none` | 透传模式 | 无 | 不做业务富化 |

## 路径 B: Prometheus 侧预打标签

### 步骤 1: 在 Prometheus scrape_config 中添加 relabel

```yaml
scrape_configs:
  - job_name: 'ceph-exporter'
    static_configs:
      - targets: ['ceph-exporter:9283']
    relabel_configs:
      - target_label: team
        replacement: 'storage-team'
      - target_label: business_unit
        replacement: '基础设施'
      - target_label: criticality
        replacement: 'high'
```

### 步骤 2: Alertmanager 路由到 mutong 标准 webhook

```yaml
receivers:
  - name: 'mutong'
    webhook_configs:
      - url: 'https://mutong.example.com/api/v1/alerts/webhook'
```

告警到达 mutong 时，`team`/`businessUnit`/`criticality` 标签已随 Prometheus 告警携带，Enricher 自动提取并构建 BusinessContext。

### 步骤 3: （可选）同时注册 business_labels 告警源

在 `config.alert.yaml` 中注册 source，利用 `business_labels` 策略做额外验证：

```yaml
alertSources:
  sources:
    - name: "prometheus-ceph"
      type: "prometheus"
      enrichmentStrategy: "business_labels"
      businessMapping:
        serviceType: "storage"
```

## 组合推荐

- **基础设施标签**（cluster/env/region）→ Prometheus relabel 打
- **业务标签**（team/businessUnit）→ 可选 Prometheus relabel 打，或 mutong AlertSource businessMapping 兜底
- **拓扑上下文**（stakeholder/affectedApps）→ mutong NebulaGraph 自动反查

## 验证

```bash
# 查看已注册的告警源
curl https://mutong.example.com/api/v1/alerts/sources

# 发送测试告警
curl -X POST https://mutong.example.com/api/v1/alerts/external/ceph-prod \
  -H "Content-Type: application/json" \
  -d '{"alert_name":"TestAlert","severity":"info","host":"test-node"}'
```
```

- [ ] **Step 4: 提交**

```bash
git add services/alert/external_integration_test.go docs/external-alert-integration-guide.md
git commit -m "test(alert): 新增外部告警端到端测试 + 集成指南文档

- TestExternalAlertWebhook: 验证 Ceph node_affinity 场景
- TestExternalAlertWebhook_UnknownSource: 验证未知源返回 404
- TestListAlertSources: 验证告警源列表 API
- docs/external-alert-integration-guide.md: 运维集成指南"
```

---

## 验证清单

- [ ] `models/alert/alert_source.go` 编译通过，单元测试 PASS
- [ ] `configs/config.alert.yaml.example` 含 Ceph/Kafka/通用 三类示例
- [ ] `POST /api/v1/alerts/external/ceph-prod` 返回 200，告警进入流水线
- [ ] `POST /api/v1/alerts/external/unknown` 返回 404 + availableSources
- [ ] `GET /api/v1/alerts/sources` 返回已注册的告警源列表
- [ ] Ceph 告警经 `node_affinity` 策略后正确关联到节点的 BusinessApp
- [ ] Kafka 告警经 `service_graph` 策略后正确关联到 BusinessApp 及其 Stakeholder
- [ ] Prometheus 侧打了 `team` 标签的告警到达后，`business_labels` 策略直接提取
- [ ] 全量编译 `go build ./...` 无错误
- [ ] 现有 Prometheus Alertmanager webhook 端点不受影响

---

## 与现有系统的兼容性

| 现有能力 | 是否受影响 | 说明 |
|----------|:---:|------|
| `POST /api/v1/alerts/webhook` | 否 | 现有 Alertmanager 端点不变 |
| `AlertEnricher.Enrich()` | 否 | 新增 `enrichFromExternal` 作为独立入口，不影响现有 `Enrich` 流程 |
| `AlertService.Process()` | 否 | `ProcessExternal` 是独立方法，不修改 `Process` |
| 抑制/路由/通知 | 否 | `ProcessExternal` 复用相同的 suppressor/router/notifier |
| KnownServices 配置 | 否 | 外部告警的 serviceType 来自 `AlertSource.BusinessMapping.ServiceType`，不影响现有已知服务 |
