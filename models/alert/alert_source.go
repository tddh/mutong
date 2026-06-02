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
	// StrategyNodeAffinity 物理节点桥接: host/node → K8s Node → Pod → BusinessApp
	StrategyNodeAffinity EnrichmentStrategy = "node_affinity"

	// StrategyServiceGraph 服务拓扑桥接: serviceName → BusinessApp → CallsApp 上下游
	StrategyServiceGraph EnrichmentStrategy = "service_graph"

	// StrategyDirectBusinessApp 直接业务匹配: appName → BusinessApp
	StrategyDirectBusinessApp EnrichmentStrategy = "direct_business_app"

	// StrategyBusinessLabels 标签直接提取: 从 Prometheus 侧预打的标签提取业务信息（路径 B）
	StrategyBusinessLabels EnrichmentStrategy = "business_labels"

	// StrategyNone 不做业务富化，仅透传
	StrategyNone EnrichmentStrategy = "none"
)

// AlertSource 外部告警源配置
// 每种外部系统定义一个告警源，通过 labelMapping 将外部标签映射到内部标准标签。
type AlertSource struct {
	Name               string              `json:"name" yaml:"name"`
	Type               string              `json:"type" yaml:"type"`                             // ceph/middleware/database/network/application/custom
	DisplayName        string              `json:"displayName" yaml:"displayName"`               // 显示名称
	LabelMapping       map[string]string   `json:"labelMapping" yaml:"labelMapping"`             // 外部标签名 → 内部标签名
	EnrichmentStrategy EnrichmentStrategy  `json:"enrichmentStrategy" yaml:"enrichmentStrategy"` // 富化策略
	BusinessMapping    BusinessMappingConf `json:"businessMapping" yaml:"businessMapping"`       // 默认业务归属（兜底）
	FingerprintKeys    []string            `json:"fingerprintKeys" yaml:"fingerprintKeys"`       // 用于生成指纹的标签键
}

// BusinessMappingConf 默认业务归属配置
type BusinessMappingConf struct {
	Team         string `json:"team" yaml:"team"`
	BusinessUnit string `json:"businessUnit" yaml:"businessUnit"`
	Criticality  string `json:"criticality" yaml:"criticality"`
	ServiceType  string `json:"serviceType" yaml:"serviceType"` // middleware/database/cache
	AppName      string `json:"appName" yaml:"appName"`         // 直接指定 BusinessApp
}

// MapToAlert 将外部告警的原始 JSON payload 映射为标准 Alert
// 按照 AlertSource.LabelMapping 将外部标签转换为内部标准标签，
// 未映射的字段放入 annotations，同时注入 source 标识和默认业务归属。
func (s *AlertSource) MapToAlert(raw map[string]interface{}) *Alert {
	labels := make(map[string]string)
	annotations := make(map[string]string)

	// 1. 标签映射: externalKey → internalKey
	for internalKey, externalKey := range s.LabelMapping {
		val := deepGet(raw, externalKey)
		if val != "" {
			labels[internalKey] = val
		}
	}

	// 2. 注入 source 标识
	labels["alertSource"] = s.Name
	labels["alertSourceType"] = s.Type

	// 3. 兜底: 注入 businessMapping 中的默认值
	if s.BusinessMapping.Team != "" && labels["team"] == "" {
		labels["team"] = s.BusinessMapping.Team
	}
	if s.BusinessMapping.BusinessUnit != "" && labels["businessUnit"] == "" {
		labels["businessUnit"] = s.BusinessMapping.BusinessUnit
	}
	if s.BusinessMapping.Criticality != "" && labels["criticality"] == "" {
		labels["criticality"] = s.BusinessMapping.Criticality
	}
	if s.BusinessMapping.AppName != "" && labels["appName"] == "" {
		labels["appName"] = s.BusinessMapping.AppName
	}

	// 4. 提取 annotations
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

	// 5. 确定 status
	status := "firing"
	if st, ok := raw["status"]; ok {
		if sv, ok := st.(string); ok && strings.ToLower(sv) == "resolved" {
			status = "resolved"
		}
	}

	// 6. 生成指纹
	fingerprint := s.GenerateFingerprint(raw)

	return &Alert{
		Status:      status,
		Labels:      labels,
		Annotations: annotations,
		Fingerprint: fingerprint,
	}
}

// GenerateFingerprint 根据配置的 fingerprintKeys 生成唯一指纹
// 默认使用 alertname + instance；无匹配键时回退到 payload 全量 SHA256 hash。
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
			val = deepGet(raw, key) // 回退: 直接用 key 查找
		}
		if val != "" {
			parts = append(parts, val)
		}
	}

	if len(parts) == 0 {
		// 回退: 使用整个 payload 的 hash
		payloadBytes, _ := json.Marshal(raw)
		h := sha256.Sum256(payloadBytes)
		return fmt.Sprintf("%x", h[:8])
	}

	combined := strings.Join(parts, "|")
	h := sha256.Sum256([]byte(combined))
	return fmt.Sprintf("%s-%x", s.Name, h[:8])
}

// ExtractBusinessLabels 从告警标签直接提取业务上下文（路径 B: 监控侧已打标签）
// 当告警已经携带 team/businessUnit/criticality/appName 等标签时，
// 直接构建 BusinessContext，无需 NebulaGraph 反查。
// 如果没有足够的业务标签，返回 nil。
func (s *AlertSource) ExtractBusinessLabels(labels map[string]string) *BusinessContext {
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
