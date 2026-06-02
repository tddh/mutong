package diagnosis

import "time"

type DiagnosisRequest struct {
	Fingerprint  string `json:"fingerprint"`
	Namespace    string `json:"namespace"`
	Resource     string `json:"resource"`
	Query        string `json:"query"`
	ResourceKind string `json:"resourceKind"`
	ResourceName string `json:"resourceName"`
	AlertName    string `json:"alertName"`
	Severity     string `json:"severity"`
}

type RootCause struct {
	ResourceType string   `json:"resource_type"`
	ResourceName string   `json:"resource_name"`
	Namespace    string   `json:"namespace"`
	Confidence   float64  `json:"confidence"`
	Evidence     []string `json:"evidence"`
}

type ImpactAssessment struct {
	Severity string `json:"severity"`

	// A. 简单计数
	BlastRadius       int      `json:"blast_radius"`
	AffectedResources []string `json:"affected_resources"`

	// B. 分层影响
	DirectImpact    []ResourceRef `json:"direct_impact"`
	IndirectImpact  []ResourceRef `json:"indirect_impact"`
	PotentialImpact []ResourceRef `json:"potential_impact"`

	// C. 业务影响
	AffectedServices  []string `json:"affected_services"`
	AffectedIngresses []string `json:"affected_ingresses"`
	UserFacingImpact  bool     `json:"user_facing_impact"`

	// D. 业务上下文（来自告警富化）
	BusinessContext    string   `json:"business_context,omitempty"`     // 应用名/团队/关键度摘要
	BusinessImpactNote string   `json:"business_impact_note,omitempty"` // 节点告警的业务影响摘要
	AffectedAppNames   []string `json:"affected_app_names,omitempty"`   // 受影响的业务应用名称列表

	// E. LLM 结构化业务影响分析（新增）
	BusinessImpact *BusinessImpactAnalysis `json:"businessImpact,omitempty"`
}

// ResourceRef 资源引用
type ResourceRef struct {
	UID       string `json:"uid"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// TopologySnapshot 拓扑快照
type TopologySnapshot struct {
	Nodes []TopologyNode `json:"nodes"`
	Edges []TopologyEdge `json:"edges"`
	Error string         `json:"error,omitempty"`
}

// TopologyNode 拓扑节点
type TopologyNode struct {
	UID       string `json:"uid"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	IsDeleted bool   `json:"is_deleted"`
}

// TopologyEdge 拓扑边
type TopologyEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
	Rank int64  `json:"rank,omitempty"`
}

type RemediationSuggestion struct {
	Action      string   `json:"action"`
	Description string   `json:"description"`
	Steps       []string `json:"steps"`
	RiskLevel   string   `json:"risk_level"`
	AutoFixable bool     `json:"auto_fixable"`
	// Optional HPA-based remediation suggestion
	HPA *HPARecommendation `json:"hpa,omitempty"`
}

// HPARecommendation 存放针对 Horizontal Pod Autoscaler 的建议
type HPARecommendation struct {
	DeploymentName string  `json:"deployment_name"`
	Namespace      string  `json:"namespace"`
	MinReplicas    int32   `json:"min_replicas"`
	MaxReplicas    int32   `json:"max_replicas"`
	TargetCPU      int32   `json:"target_cpu"`              // 目标 CPU 利用率百分比
	TargetMemory   int32   `json:"target_memory,omitempty"` // 目标内存利用率百分比（可选）
	CurrentCPU     float64 `json:"current_cpu"`
	CurrentMemory  float64 `json:"current_memory"`
	Reason         string  `json:"reason"`
}

type DiagnosisResult struct {
	ID                    string                  `json:"id"`
	Timestamp             time.Time               `json:"timestamp"`
	Request               DiagnosisRequest        `json:"request"`
	RootCauses            []RootCause             `json:"root_causes"`
	Impact                ImpactAssessment        `json:"impact"`
	Remediations          []RemediationSuggestion `json:"remediations"`
	TopologySnapshot      *TopologySnapshot       `json:"topology_snapshot"`
	RelatedAlerts         []string                `json:"related_alerts"`
	Summary               string                  `json:"summary"`
	Metrics               []MetricEntry           `json:"metrics,omitempty"`
	RecentLogs            []LogEntry              `json:"recentLogs,omitempty"`
	BusinessAppCalls      *BusinessAppCalls       `json:"business_app_calls,omitempty"`
	BusinessImpactContext interface{}             `json:"business_impact_context,omitempty"`
}

type MetricEntry struct {
	ResourceKind string  `json:"resourceKind"`
	ResourceName string  `json:"resourceName"`
	MetricName   string  `json:"metricName"`
	Value        float64 `json:"value"`
	Status       string  `json:"status,omitempty"`
}

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Pod       string `json:"pod"`
	Namespace string `json:"namespace"`
	Container string `json:"container"`
}

// BusinessAppRef 业务应用引用（用于调用链上下文）
type BusinessAppRef struct {
	AppName     string `json:"appName"`
	Team        string `json:"team"`
	Criticality string `json:"criticality"`
}

// BusinessAppCalls 指定业务应用的上下游调用关系
type BusinessAppCalls struct {
	AppName     string           `json:"appName"`
	Namespace   string           `json:"namespace"`
	Upstreams   []BusinessAppRef `json:"upstreams"`
	Downstreams []BusinessAppRef `json:"downstreams"`
}

type BusinessImpactEntry struct {
	AppName      string `json:"appName"`
	Namespace    string `json:"namespace"`
	Team         string `json:"team,omitempty"`
	BusinessUnit string `json:"businessUnit,omitempty"`
	Criticality  string `json:"criticality,omitempty"`
	ImpactType   string `json:"impactType"`
	HopDistance  int    `json:"hopDistance"`
	ImpactPath   string `json:"impactPath"`
	Reasoning    string `json:"reasoning"`
}

type BusinessImpactAnalysis struct {
	DirectImpacts   []BusinessImpactEntry `json:"directImpacts"`
	IndirectImpacts []BusinessImpactEntry `json:"indirectImpacts"`
	Summary         string                `json:"summary"`
	RiskLevel       string                `json:"riskLevel"`
}

type ToolDefinition struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  []ParamDef `json:"parameters"`
}

type ParamDef struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

type WorkloadContext struct {
	ControllerKind string        `json:"controller_kind"`
	ControllerName string        `json:"controller_name"`
	Namespace      string        `json:"namespace"`
	TotalPods      int           `json:"total_pods"`
	HealthyPods    int           `json:"healthy_pods"`
	Pods           []WorkloadPod `json:"pods"`
}

type WorkloadPod struct {
	Name      string `json:"name"`
	IsAlerted bool   `json:"is_alerted"`
	IsDeleted bool   `json:"is_deleted"`
}
