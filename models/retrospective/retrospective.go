package retrospective

import (
	"time"

	"gorm.io/gorm"

	"gitee.com/tddh/mutong/models/diagnosis"
)

type TimelineEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	EventType   string    `json:"event_type"`
	Description string    `json:"description"`
	Source      string    `json:"source"`
	Severity    string    `json:"severity"`
	Resource    string    `json:"resource"`
}

type CausalLink struct {
	Cause      string   `json:"cause"`
	Effect     string   `json:"effect"`
	Confidence float64  `json:"confidence"`
	Evidence   []string `json:"evidence"`
}

type CausalChain struct {
	RootCause string       `json:"root_cause"`
	Links     []CausalLink `json:"links"`
	Impact    string       `json:"impact"`
}

// ExecutionActionRecord 复盘报告中引用的自愈执行审计记录
type ExecutionActionRecord struct {
	Timestamp    time.Time `json:"timestamp"`
	Action       string    `json:"action"`
	Target       string    `json:"target"`
	Risk         string    `json:"risk,omitempty"`
	ApprovedBy   string    `json:"approved_by,omitempty"`   // 审批人；空表示未审批（待审批或自动执行）
	AutoExecuted bool      `json:"auto_executed"`           // 是否自动模式执行
	Success      bool      `json:"success"`
	Message      string    `json:"message,omitempty"`
	Reason       string    `json:"reason,omitempty"`
}

// EditableField 可编辑字段，保留 AI 原始值和人工修正值
type EditableField struct {
	AIGenerated string `json:"ai_generated"` // LLM 原始输出（只读）
	Final       string `json:"final"`        // 最终版本
}

type PostmortemReport struct {
	ID             string          `json:"id"`
	IncidentTitle  string          `json:"incident_title"`
	StartTime      time.Time       `json:"start_time"`
	EndTime        time.Time       `json:"end_time"`
	Duration       string          `json:"duration"`
	Severity       string          `json:"severity"`
	Timeline       []TimelineEvent `json:"timeline"`
	CausalChain    CausalChain     `json:"causal_chain"`
	ImpactedSvc    []string        `json:"impacted_services"`
	RootCause      EditableField   `json:"root_cause"`
	Resolution     EditableField   `json:"resolution"`
	LessonsLearned []string        `json:"lessons_learned"`
	ActionItems    []ActionItem    `json:"action_items"`
	GeneratedAt    time.Time       `json:"generated_at"`

	BusinessContext *BusinessReportContext `json:"business_context,omitempty"`
	BusinessCalls   *BusinessReportCalls   `json:"business_calls,omitempty"`
	BusinessImpact  *BusinessReportImpact  `json:"business_impact,omitempty"`
	AIDiagnosis     *AIDiagnosisSummary    `json:"ai_diagnosis,omitempty"`
	WorkloadContext *WorkloadReportContext `json:"workload_context,omitempty"`

	// 从诊断缓存提取的完整数据（丰富化）
	TopologySnapshot *diagnosis.TopologySnapshot       `json:"topology_snapshot,omitempty"`
	DiagnosisMetrics []diagnosis.MetricEntry           `json:"diagnosis_metrics,omitempty"`
	DiagnosisLogs    []diagnosis.LogEntry              `json:"diagnosis_logs,omitempty"`
	AllRootCauses    []diagnosis.RootCause             `json:"all_root_causes,omitempty"`
	AllRemediations  []diagnosis.RemediationSuggestion `json:"all_remediations,omitempty"`
	ImpactAssessment *diagnosis.ImpactAssessment       `json:"impact_assessment,omitempty"`
	RelatedAlerts    []string                          `json:"related_alerts,omitempty"`

	// 业界标配新增字段
	DetectionMethod     string   `json:"detection_method,omitempty"`
	MTTD                string   `json:"mttd,omitempty"`
	WhatWentWell        []string `json:"what_went_well,omitempty"`
	WhatWentWrong       []string `json:"what_went_wrong,omitempty"`
	ContributingFactors []string `json:"contributing_factors,omitempty"`

	// 自愈执行记录（审批/执行的审计闭环）
	ExecutionActions []ExecutionActionRecord `json:"execution_actions,omitempty"`
}

type BusinessReportContext struct {
	AppName      string `json:"app_name"`
	Team         string `json:"team"`
	Criticality  string `json:"criticality"`
	BusinessUnit string `json:"business_unit,omitempty"`
	Environment  string `json:"environment,omitempty"`
}

type BusinessReportCalls struct {
	AppName     string           `json:"app_name"`
	Upstreams   []BusinessAppRef `json:"upstreams"`
	Downstreams []BusinessAppRef `json:"downstreams"`
}

type BusinessAppRef struct {
	AppName     string `json:"app_name"`
	Team        string `json:"team"`
	Criticality string `json:"criticality"`
}

type BusinessReportImpact struct {
	DirectImpacts   []BusinessImpactItem `json:"direct_impacts"`
	IndirectImpacts []BusinessImpactItem `json:"indirect_impacts"`
	RiskLevel       string               `json:"risk_level"`
}

type BusinessImpactItem struct {
	AppName     string `json:"app_name"`
	Criticality string `json:"criticality"`
	Team        string `json:"team"`
	ImpactPath  string `json:"impact_path,omitempty"`
	Reasoning   string `json:"reasoning,omitempty"`
}

type AIDiagnosisSummary struct {
	RootCause   string   `json:"root_cause"`
	Confidence  float64  `json:"confidence"`
	Evidence    []string `json:"evidence"`
	Summary     string   `json:"summary"`
	Remediation string   `json:"remediation"`
}

type WorkloadReportContext struct {
	ControllerKind string            `json:"controller_kind"`
	ControllerName string            `json:"controller_name"`
	TotalPods      int               `json:"total_pods"`
	HealthyPods    int               `json:"healthy_pods"`
	Pods           []WorkloadPodItem `json:"pods"`
}

type WorkloadPodItem struct {
	Name      string `json:"name"`
	IsAlerted bool   `json:"is_alerted"`
}

type ActionItem struct {
	Description  string `json:"description"`
	Owner        string `json:"owner"`
	Priority     string `json:"priority"`
	DueDate      string `json:"due_date"`
	Status       string `json:"status"`
	ExitCriteria string `json:"exit_criteria,omitempty"`
	Category     string `json:"category,omitempty"`
}

type PostmortemModel struct {
	ID          uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Fingerprint string         `gorm:"type:varchar(64);uniqueIndex;not null" json:"fingerprint"`
	ReportJSON  string         `gorm:"type:json" json:"report_json"`
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	UpdatedBy   string         `gorm:"type:varchar(64)" json:"updated_by"`
}

func (PostmortemModel) TableName() string { return "postmortems" }

type DecisionRecord struct {
	ID            uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Fingerprint   string    `gorm:"type:varchar(64);index;not null" json:"fingerprint"`
	NodeName      string    `gorm:"type:varchar(32);not null" json:"node_name"`
	InputSummary  string    `gorm:"type:varchar(512)" json:"input_summary"`
	OutputSummary string    `gorm:"type:varchar(512)" json:"output_summary"`
	Decision      string    `gorm:"type:varchar(64)" json:"decision"`
	DurationMs    int64     `json:"duration_ms"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (DecisionRecord) TableName() string { return "decision_records" }

type IncidentKnowledge struct {
	ID                uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Fingerprint       string    `gorm:"type:varchar(64);index;not null" json:"fingerprint"`
	FaultType         string    `gorm:"type:varchar(64);index" json:"fault_type"`
	ResourceKind      string    `gorm:"type:varchar(64);index" json:"resource_kind"`
	RootCauseKeywords string    `gorm:"type:text" json:"root_cause_keywords"`
	ResolutionSummary string    `gorm:"type:text" json:"resolution_summary"`
	CreatedAt         time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (IncidentKnowledge) TableName() string { return "incident_knowledge" }

type PostmortemResourceLink struct {
	ID           uint      `gorm:"primaryKey;autoIncrement"`
	Fingerprint  string    `gorm:"type:varchar(64);index:idx_fp_res;not null"`
	ResourceUID  string    `gorm:"type:varchar(128);index:idx_fp_res;not null"`
	ResourceKind string    `gorm:"type:varchar(64)"`
	ResourceName string    `gorm:"type:varchar(256)"`
	Namespace    string    `gorm:"type:varchar(256)"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`
}

func (PostmortemResourceLink) TableName() string { return "postmortem_resource_links" }
