package alert

import (
	"time"

	"gitee.com/tddh/mutong/models/diagnosis"
)

type StatsModel struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	StatKey   string    `gorm:"type:varchar(50);not null;uniqueIndex:uk_alert_stat_key;comment:统计项标识" json:"stat_key"`
	StatValue int64     `gorm:"not null;default:0;comment:统计值" json:"stat_value"`
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime;comment:更新时间" json:"updated_at"`
}

func (StatsModel) TableName() string {
	return "alert_stats"
}

// Alert 告警数据结构，对应 Alertmanager Webhook Payload
type Alert struct {
	Status       string            `json:"status"`       // firing, resolved
	Labels       map[string]string `json:"labels"`       // 告警标签（用于聚合和路由）
	Annotations  map[string]string `json:"annotations"`  // 告警详情
	StartsAt     time.Time         `json:"startsAt"`     // 告警开始时间
	EndsAt       time.Time         `json:"endsAt"`       // 告警结束时间
	GeneratorURL string            `json:"generatorURL"` // 告警规则链接
	Fingerprint  string            `json:"fingerprint"`  // 告警指纹（用于去重）
}

// WebhookPayload Alertmanager Webhook 请求体
type WebhookPayload struct {
	Version           string            `json:"version"`           // "4"
	GroupKey          string            `json:"groupKey"`          // 告警组key
	Status            string            `json:"status"`            // firing, resolved, resolved
	Receiver          string            `json:"receiver"`          // 接收器名称
	GroupLabels       map[string]string `json:"groupLabels"`       // 分组标签
	CommonLabels      map[string]string `json:"commonLabels"`      // 公共标签
	CommonAnnotations map[string]string `json:"commonAnnotations"` // 公共注解
	ExternalURL       string            `json:"externalURL"`       // Alertmanager URL
	Alerts            []Alert           `json:"alerts"`            // 告警列表
	TruncatedAlerts   int               `json:"truncatedAlerts"`   // 截断的告警数量
}

// BusinessImpact 节点告警的业务影响推断结果。
// 基础设施归属与业务影响分离；Node 告警不重新分配给单一 BusinessApp。
type BusinessImpact struct {
	AffectedBusinessApps []BusinessAppBrief `json:"affectedBusinessApps"`
	AffectedAppCount     int                `json:"affectedAppCount"`
	ContainsCriticalApps bool               `json:"containsCriticalApps"`
	PrimaryBusinessApp   string             `json:"primaryBusinessApp,omitempty"`
}

// BusinessAppBrief 业务应用简要信息。
type BusinessAppBrief struct {
	AppName     string `json:"appName"`
	Criticality string `json:"criticality"`
	Team        string `json:"team,omitempty"`
}

// BusinessContext 告警业务上下文（与 interfaces.BusinessAppContext 对齐，可序列化）
type BusinessContext struct {
	UID          string `json:"uid"`
	AppName      string `json:"appName"`
	Namespace    string `json:"namespace"`
	Criticality  string `json:"criticality"`
	Environment  string `json:"environment"`
	Team         string `json:"team"`
	BusinessUnit string `json:"businessUnit"`
	Source       string `json:"source"`
	ServiceType  string `json:"serviceType"` // 来自 knownServices 配置: middleware/database/cache
}

// EnrichedAlert 富化后的告警（包含拓扑上下文）
type EnrichedAlert struct {
	Alert
	// 拓扑富化信息
	ResourceUID     string                      `json:"resourceUID"`             // K8s 资源 UID
	ResourceType    string                      `json:"resourceType"`            // Pod, Node, Service, Deployment 等
	ResourceName    string                      `json:"resourceName"`            // 资源名称
	Namespace       string                      `json:"namespace"`               // 命名空间
	NodeName        string                      `json:"nodeName"`                // 节点名称（Pod所在节点）
	OwnerKind       string                      `json:"ownerKind"`               // 所属控制器类型
	OwnerName       string                      `json:"ownerName"`               // 所属控制器名称
	RelatedAlerts   []string                    `json:"relatedAlerts"`           // 相关联的告警
	TopologyPath    []string                    `json:"topologyPath"`            // 拓扑路径（如: Node->Pod->Container）
	EnrichTags      map[string]string           `json:"enrichTags"`              // 富化的额外标签
	BusinessContext BusinessContext             `json:"businessContext"`         // 显式业务上下文
	BusinessImpact  BusinessImpact              `json:"businessImpact"`          // 节点告警的业务影响推断
	BusinessCalls   *diagnosis.BusinessAppCalls `json:"businessCalls,omitempty"` // 业务上下游调用链
	Stakeholders    []StakeholderInfo           `json:"stakeholders,omitempty"`  // 受影响方列表
}

// StakeholderInfo 受影响方信息
type StakeholderInfo struct {
	Team        string `json:"team"`
	Channel     string `json:"channel"`
	Reason      string `json:"reason"`
	ImpactLevel string `json:"impactLevel"` // direct | indirect
	AppName     string `json:"appName"`
	Namespace   string `json:"namespace"`
}

// SuppressionResult 抑制结果
type SuppressionResult struct {
	IsSuppressed      bool      `json:"isSuppressed"`      // 是否被抑制
	SuppressionReason string    `json:"suppressionReason"` // 抑制原因
	SuppressedBy      []string  `json:"suppressedBy"`      // 被哪些告警抑制
	SuppressedAt      time.Time `json:"suppressedAt"`      // 抑制时间
}

// RoutingResult 路由结果
type RoutingResult struct {
	Receiver           string              `json:"receiver"`
	NotifyChannel      string              `json:"notifyChannel"`
	Severity           string              `json:"severity"`
	Priority           int                 `json:"priority"`
	NotifyUsers        []string            `json:"notifyUsers"`
	NotifyGroups       []string            `json:"notifyGroups"`
	StakeholderNotices []StakeholderNotice `json:"stakeholderNotices,omitempty"`
}

// StakeholderNotice 受影响方通知
type StakeholderNotice struct {
	Team     string   `json:"team"`
	Receiver string   `json:"receiver"`
	Channel  string   `json:"channel"`
	Users    []string `json:"users"`
	Reason   string   `json:"reason"`
	Severity string   `json:"severity"`
}

// ProcessedAlert 处理后的完整告警
type ProcessedAlert struct {
	EnrichedAlert
	Suppression SuppressionResult `json:"suppression"` // 抑制结果
	Routing     RoutingResult     `json:"routing"`     // 路由结果
	ProcessedAt time.Time         `json:"processedAt"` // 处理时间
	RuleChainID string            `json:"ruleChainId"` // 执行的规则链ID
}

// AlertLabels K8s 告警常用标签
// 设计说明：
// - ResourceUID/ResourceKind 来自 Prometheus relabel 配置 (kubernetes_uid, kubernetes_kind)
// - 目前只有 Pod 类型 Prometheus 会自动提供 __meta_kubernetes_pod_uid
// - Node/Service/Deployment 等类型 Prometheus 不提供 UID，需降级使用 Name+Namespace+Kind 查询
type AlertLabels struct {
	ResourceUID  string
	ResourceKind string
	ResourceType string
	ResourceName string

	AlertName string
	Namespace string
	Pod       string
	Node      string
	Container string
	Service   string
	Severity  string
	Instance  string
	Job       string
}

// ExtractK8sLabels 从告警标签中提取 K8s 相关信息
// 提取优先级：
// 1. ResourceUID (kubernetes_uid) - 精确匹配，仅 Pod 类型可用
// 2. ResourceKind (kubernetes_kind) - 资源类型标识
// 3. 原有标签 - 向后兼容，用于降级查询
func ExtractK8sLabels(labels map[string]string) AlertLabels {
	// 优先使用显式声明的 resourceType 和 name（适用于所有资源类型）
	resourceType := labels["resourceType"]
	if resourceType == "" {
		resourceType = labels["kubernetes_kind"]
	}
	resourceName := labels["name"]
	if resourceName == "" {
		resourceName = labels["pod"]
	}
	if resourceName == "" {
		resourceName = labels["node"]
	}
	if resourceName == "" {
		resourceName = labels["service"]
	}

	return AlertLabels{
		ResourceUID:  labels["kubernetes_uid"],
		ResourceKind: resourceType,

		AlertName: labels["alertname"],
		Namespace: labels["namespace"],
		Pod:       labels["pod"],
		Node:      labels["node"],
		Container: labels["container"],
		Service:   labels["service"],
		Severity:  labels["severity"],
		Instance:  labels["instance"],
		Job:       labels["job"],
		// 新增：显式资源类型和名称
		ResourceType: resourceType,
		ResourceName: resourceName,
	}
}
