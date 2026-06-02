package interfaces

import (
	"context"
	"encoding/json"
	"strings"

	"gitee.com/tddh/mutong/models/diagnosis"
)

type LLMProvider interface {
	Diagnose(ctx context.Context, prompt DiagnosisPrompt) (*LLMDiagnosisResult, error)
	GeneratePostmortemInsights(ctx context.Context, prompt PostmortemInsightsPrompt) (*PostmortemInsightsResult, error)
	GenerateEmbedding(ctx context.Context, text string) ([]float32, error)
}

type PostmortemInsightsPrompt struct {
	IncidentTitle string
	Severity      string
	Duration      string
	RootCause     string
	Timeline      string
	AlertName     string
	ResourceKind  string
	ResourceName  string
	Namespace     string

	// 丰富化新增上下文
	CausalChain      string   `json:"causal_chain,omitempty"`
	ImpactSummary    string   `json:"impact_summary,omitempty"`
	DiagnosisSummary string   `json:"diagnosis_summary,omitempty"`
	AffectedServices []string `json:"affected_services,omitempty"`
	MetricsSummary   string   `json:"metrics_summary,omitempty"`
	LogsHighlights   string   `json:"logs_highlights,omitempty"`
}

type PostmortemInsightsResult struct {
	LessonsLearned      []string `json:"lessons_learned"`
	WhatWentWell        []string `json:"what_went_well,omitempty"`
	WhatWentWrong       []string `json:"what_went_wrong,omitempty"`
	ContributingFactors []string `json:"contributing_factors,omitempty"`
	ActionItems         []struct {
		Description  string `json:"description"`
		Owner        string `json:"owner"`
		Priority     string `json:"priority"`
		ExitCriteria string `json:"exit_criteria,omitempty"`
		Category     string `json:"category,omitempty"`
	} `json:"action_items"`
	Resolution string `json:"resolution"`
}

type DiagnosisPrompt struct {
	Alert            interface{}
	TopologySnapshot *diagnosis.TopologySnapshot
	ImpactAssessment *diagnosis.ImpactAssessment
	KnowledgeMatches []diagnosis.RemediationSuggestion
	RelatedAlerts    []string
	BusinessAppCalls *diagnosis.BusinessAppCalls
	// BusinessImpactContext 业务影响上下文（LLM 自主分析用）
	// 实际类型为 *diagnosis.BusinessImpactContext，用 interface{} 避免循环依赖
	BusinessImpactContext interface{}
}

type LLMDiagnosisResult struct {
	RootCause      string                            `json:"root_cause"`
	RootCauseAlt   string                            `json:"rootCause,omitempty"`
	Confidence     llmConfidence                     `json:"confidence"`
	Evidence       []string                          `json:"evidence"`
	Remediation    llmRemediation                    `json:"remediation"`
	BusinessImpact *diagnosis.BusinessImpactAnalysis `json:"businessImpact,omitempty"`
}

// llmConfidence 处理 LLM 可能返回数字或字符串的 confidence 值
type llmConfidence float64

func (c *llmConfidence) UnmarshalJSON(data []byte) error {
	// 尝试数字
	var f float64
	if err := json.Unmarshal(data, &f); err == nil {
		*c = llmConfidence(f)
		return nil
	}
	// 尝试字符串，映射为数值
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch strings.ToLower(s) {
	case "critical", "high":
		*c = 0.9
	case "medium", "warning":
		*c = 0.6
	case "low", "info":
		*c = 0.3
	default:
		*c = 0.5
	}
	return nil
}

// llmRemediation handles the fact that LLM sometimes returns remediation as a single object
// and sometimes as an array of objects.
type llmRemediation []diagnosis.RemediationSuggestion

func (r *llmRemediation) UnmarshalJSON(data []byte) error {
	// Try array of objects first
	var arr []diagnosis.RemediationSuggestion
	if err := json.Unmarshal(data, &arr); err == nil {
		*r = arr
		return nil
	}
	// Try array of strings (LLM sometimes returns steps as plain strings)
	var strArr []string
	if err := json.Unmarshal(data, &strArr); err == nil {
		for _, s := range strArr {
			*r = append(*r, diagnosis.RemediationSuggestion{
				Action:      s,
				Description: s,
			})
		}
		return nil
	}
	// Try single string
	var singleStr string
	if err := json.Unmarshal(data, &singleStr); err == nil {
		*r = []diagnosis.RemediationSuggestion{{Action: singleStr, Description: singleStr}}
		return nil
	}
	// Fall back to single object
	var single diagnosis.RemediationSuggestion
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	*r = []diagnosis.RemediationSuggestion{single}
	return nil
}

// First returns the first remediation suggestion, or an empty one if none.
func (r llmRemediation) First() diagnosis.RemediationSuggestion {
	if len(r) > 0 {
		return r[0]
	}
	return diagnosis.RemediationSuggestion{}
}

// SetRemediation sets the remediation from a single suggestion.
// This is a convenience method for tests and construction.
func (res *LLMDiagnosisResult) SetRemediation(s diagnosis.RemediationSuggestion) {
	res.Remediation = llmRemediation{s}
}
