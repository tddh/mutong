package api

import (
	"context"
	"encoding/json"
	"fmt"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
)

// DiagnosisAPI provides typed access to the Mutong Diagnosis API.
type DiagnosisAPI struct {
	client *client.Client
}

// NewDiagnosisAPI creates a new DiagnosisAPI.
func NewDiagnosisAPI(c *client.Client) *DiagnosisAPI {
	return &DiagnosisAPI{client: c}
}

// DiagnosisResult represents the response from running a diagnosis.
type DiagnosisResult struct {
	ID                string                 `json:"id"`
	Timestamp         string                 `json:"timestamp"`
	Summary           string                 `json:"summary"`
	RootCauses        []DiagnosisRootCause   `json:"root_causes"`
	Impact            DiagnosisImpact        `json:"impact"`
	Remediations      []DiagnosisRemediation `json:"remediations"`
	TopologySnapshot  TopologySnapshot       `json:"topology_snapshot"`
	RelatedAlerts     []string               `json:"related_alerts"`
	Metrics           []interface{}          `json:"metrics"`
	BusinessAppCalls  interface{}            `json:"business_app_calls"`
	BusinessImpactCtx map[string]interface{} `json:"business_impact_context"`
}

// DiagnosisRootCause represents a single root cause finding.
type DiagnosisRootCause struct {
	ResourceType string        `json:"resource_type"`
	ResourceName string        `json:"resource_name"`
	Namespace    string        `json:"namespace"`
	Confidence   float64       `json:"confidence"`
	Evidence     []interface{} `json:"evidence"`
}

// DiagnosisImpact represents the impact assessment.
type DiagnosisImpact struct {
	Severity           string        `json:"severity"`
	BlastRadius        int           `json:"blast_radius"`
	AffectedResources  []string      `json:"affected_resources"`
	DirectImpact       []interface{} `json:"direct_impact"`
	IndirectImpact     []interface{} `json:"indirect_impact"`
	PotentialImpact    []interface{} `json:"potential_impact"`
	AffectedServices   []string      `json:"affected_services"`
	AffectedIngresses  []string      `json:"affected_ingresses"`
	UserFacingImpact   bool          `json:"user_facing_impact"`
	BusinessContext    string        `json:"business_context"`
	BusinessImpactNote string        `json:"business_impact_note"`
	AffectedAppNames   []string      `json:"affected_app_names"`
}

// DiagnosisRemediation represents a suggested remediation step.
type DiagnosisRemediation struct {
	Action      string   `json:"action"`
	Description string   `json:"description"`
	Steps       []string `json:"steps"`
	RiskLevel   string   `json:"risk_level"`
	AutoFixable bool     `json:"auto_fixable"`
}

// TopologySnapshot captures the topology at diagnosis time.
type TopologySnapshot struct {
	Nodes []interface{} `json:"nodes"`
	Edges []interface{} `json:"edges"`
}

// DiagnosisStatus represents the diagnosis service status.
type DiagnosisStatus struct {
	LLM       map[string]interface{} `json:"llm"`
	Diagnosis map[string]interface{} `json:"diagnosis"`
}

// RunDiagnosis triggers a diagnosis for the given alert fingerprint.
func (a *DiagnosisAPI) RunDiagnosis(fingerprint string) (*DiagnosisResult, error) {
	body := map[string]string{"fingerprint": fingerprint}
	data, err := a.client.DoLongRunning(context.Background(), "POST", "/api/v1/diagnosis/run", body)
	if err != nil {
		return nil, fmt.Errorf("running diagnosis: %w", err)
	}
	var result DiagnosisResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetStatus retrieves the diagnosis service status.
func (a *DiagnosisAPI) GetStatus() (*DiagnosisStatus, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/diagnosis/status", nil)
	if err != nil {
		return nil, fmt.Errorf("getting diagnosis status: %w", err)
	}
	var status DiagnosisStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, err
	}
	return &status, nil
}
