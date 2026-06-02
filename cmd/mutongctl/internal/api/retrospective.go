package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
)

// RetrospectiveAPI provides typed access to the Mutong Retrospective API.
type RetrospectiveAPI struct {
	client *client.Client
}

// NewRetrospectiveAPI creates a new RetrospectiveAPI.
func NewRetrospectiveAPI(c *client.Client) *RetrospectiveAPI {
	return &RetrospectiveAPI{client: c}
}

// RetrospectivePostmortem represents a generated postmortem report.
type RetrospectivePostmortem struct {
	Fingerprint    string `json:"fingerprint"`
	Title          string `json:"title"`
	Severity       string `json:"severity"`
	Status         string `json:"status"`
	RootCause      string `json:"root_cause"`
	BusinessImpact string `json:"business_impact"`
	Resolution     string `json:"resolution"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// RetrospectiveListItem is a lightweight item for list responses.
type RetrospectiveListItem struct {
	Fingerprint      string `json:"fingerprint"`
	IncidentTitle    string `json:"incident_title"`
	Severity         string `json:"severity"`
	StartTime        string `json:"start_time"`
	EndTime          string `json:"end_time"`
	Duration         string `json:"duration"`
	ResourceKind     string `json:"resource_kind"`
	ResourceName     string `json:"resource_name"`
	Namespace        string `json:"namespace"`
	RootCauseSummary string `json:"root_cause_summary"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

// RetrospectiveListResult wraps a paginated list response.
type RetrospectiveListResult struct {
	Items    []RetrospectiveListItem `json:"items"`
	Total    int                     `json:"total"`
	Page     int                     `json:"page"`
	PageSize int                     `json:"page_size"`
}

// KnowledgeResult represents a single knowledge search result.
type KnowledgeResult struct {
	Fingerprint string  `json:"fingerprint"`
	Title       string  `json:"title"`
	Severity    string  `json:"severity"`
	Similarity  float64 `json:"similarity"`
	Summary     string  `json:"summary"`
}

// SearchKnowledgeResult wraps a knowledge search response.
type SearchKnowledgeResult struct {
	Results []KnowledgeResult `json:"results"`
	Query   string            `json:"query"`
}

// Generate generates a postmortem report for the given fingerprint (POST).
func (a *RetrospectiveAPI) Generate(fingerprint string) (*RetrospectivePostmortem, error) {
	data, err := a.client.Do(context.Background(), "POST", "/api/v1/retrospective/postmortem/"+fingerprint, nil)
	if err != nil {
		return nil, fmt.Errorf("generating postmortem: %w", err)
	}
	var pm RetrospectivePostmortem
	if err := json.Unmarshal(data, &pm); err != nil {
		return nil, err
	}
	return &pm, nil
}

// List returns a paginated list of retrospective reports, optionally filtered by severity.
func (a *RetrospectiveAPI) List(severity string, page int) (*RetrospectiveListResult, error) {
	var params []string
	if severity != "" {
		params = append(params, "severity="+severity)
	}
	if page > 0 {
		params = append(params, fmt.Sprintf("page=%d", page))
	}
	path := "/api/v1/retrospective/list"
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}

	data, err := a.client.Do(context.Background(), "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("listing retrospectives: %w", err)
	}
	var result RetrospectiveListResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Get retrieves a single retrospective report by fingerprint.
func (a *RetrospectiveAPI) Get(fingerprint string) (*RetrospectivePostmortem, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/retrospective/history/"+fingerprint, nil)
	if err != nil {
		return nil, fmt.Errorf("getting retrospective: %w", err)
	}
	var pm RetrospectivePostmortem
	if err := json.Unmarshal(data, &pm); err != nil {
		return nil, err
	}
	return &pm, nil
}

// Export returns the postmortem as plain text (Markdown).
func (a *RetrospectiveAPI) Export(fingerprint string) (string, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/retrospective/postmortem/"+fingerprint+"/text", nil)
	if err != nil {
		return "", fmt.Errorf("exporting postmortem: %w", err)
	}
	return string(data), nil
}

// Search searches the retrospective knowledge base.
func (a *RetrospectiveAPI) Search(query string) (*SearchKnowledgeResult, error) {
	path := "/api/v1/retrospective/knowledge/search?q=" + url.QueryEscape(query)
	data, err := a.client.Do(context.Background(), "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("searching knowledge: %w", err)
	}
	var result SearchKnowledgeResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// TimelineEvent represents an event in the incident timeline.
type TimelineEvent struct {
	Timestamp   string `json:"timestamp"`
	EventType   string `json:"event_type"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Severity    string `json:"severity"`
	Resource    string `json:"resource"`
}

// CausalLink represents a cause-effect relationship in the causal chain.
type CausalLink struct {
	Cause      string        `json:"cause"`
	Effect     string        `json:"effect"`
	Confidence float64       `json:"confidence"`
	Evidence   []interface{} `json:"evidence"`
}

// CausalChain represents the full causal analysis.
type CausalChain struct {
	RootCause string       `json:"root_cause"`
	Links     []CausalLink `json:"links"`
	Impact    string       `json:"impact"`
}

// GetTimeline retrieves the incident timeline for a fingerprint.
func (a *RetrospectiveAPI) GetTimeline(fingerprint string) ([]TimelineEvent, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/retrospective/timeline/"+fingerprint, nil)
	if err != nil {
		return nil, fmt.Errorf("getting timeline: %w", err)
	}
	var events []TimelineEvent
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, err
	}
	return events, nil
}

// GetCausalChain retrieves the causal chain analysis for a fingerprint.
func (a *RetrospectiveAPI) GetCausalChain(fingerprint string) (*CausalChain, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/retrospective/causal-chain/"+fingerprint, nil)
	if err != nil {
		return nil, fmt.Errorf("getting causal chain: %w", err)
	}
	var chain CausalChain
	if err := json.Unmarshal(data, &chain); err != nil {
		return nil, err
	}
	return &chain, nil
}
