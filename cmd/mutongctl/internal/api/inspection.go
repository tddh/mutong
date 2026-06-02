package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
)

// InspectionAPI provides typed access to the Mutong Inspection API.
type InspectionAPI struct {
	client *client.Client
}

// NewInspectionAPI creates a new InspectionAPI.
func NewInspectionAPI(c *client.Client) *InspectionAPI {
	return &InspectionAPI{client: c}
}

// InspectionResult represents the response after triggering an inspection.
type InspectionResult struct {
	ID        string `json:"id"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Status    string `json:"status"`
}

// InspectionReport represents a single inspection report.
type InspectionReport struct {
	ID          string            `json:"id"`
	GeneratedAt string            `json:"generatedAt"`
	Issues      []InspectionIssue `json:"issues"`
	Summary     InspectionSummary `json:"summary"`
}

// InspectionIssue represents a single issue found during inspection.
type InspectionIssue struct {
	RuleName   string   `json:"ruleName"`
	Severity   string   `json:"severity"`
	Message    string   `json:"message"`
	Resources  []string `json:"resources"`
	Suggestion string   `json:"suggestion"`
	Timestamp  string   `json:"timestamp"`
}

// InspectionSummary aggregates inspection statistics.
type InspectionSummary struct {
	Total      int            `json:"total"`
	Critical   int            `json:"critical"`
	Warning    int            `json:"warning"`
	Info       int            `json:"info"`
	ByCategory map[string]int `json:"byCategory"`
}

// ComparedReports contains the comparison result between two reports.
type ComparedReports struct {
	NewIssues        []InspectionIssue `json:"newIssues"`
	ResolvedIssues   []InspectionIssue `json:"resolvedIssues"`
	PersistentIssues []InspectionIssue `json:"persistentIssues"`
	Trend            string            `json:"trend"`
}

// InspectionTrendResponse wraps the trend API response.
type InspectionTrendResponse struct {
	Days  int                   `json:"days"`
	Trend []InspectionTrendItem `json:"trend"`
}

// InspectionTrendItem represents a single data point in trend analysis.
type InspectionTrendItem struct {
	Date      string  `json:"date"`
	PassRate  float64 `json:"pass_rate"`
	FailCount int     `json:"fail_count"`
	WarnCount int     `json:"warn_count"`
	Total     int     `json:"total"`
}

// RunInspection triggers a manual inspection execution.
func (a *InspectionAPI) RunInspection() (*InspectionResult, error) {
	data, err := a.client.Do(context.Background(), "POST", "/api/v1/inspection/execute", nil)
	if err != nil {
		return nil, fmt.Errorf("running inspection: %w", err)
	}
	var result InspectionResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetLatestReport retrieves the most recent inspection report.
func (a *InspectionAPI) GetLatestReport() (*InspectionReport, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/inspection/report", nil)
	if err != nil {
		return nil, fmt.Errorf("getting latest report: %w", err)
	}
	var report InspectionReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

// ListReports retrieves all historical inspection reports.
func (a *InspectionAPI) ListReports() ([]InspectionReport, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/inspection/reports", nil)
	if err != nil {
		return nil, fmt.Errorf("listing reports: %w", err)
	}
	var reports []InspectionReport
	if err := json.Unmarshal(data, &reports); err != nil {
		return nil, err
	}
	return reports, nil
}

// GetReport retrieves a specific inspection report by ID.
func (a *InspectionAPI) GetReport(id string) (*InspectionReport, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/inspection/reports/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("getting report: %w", err)
	}
	var report InspectionReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

// CompareReports compares two inspection reports by their IDs.
func (a *InspectionAPI) CompareReports(id1, id2 string) (*ComparedReports, error) {
	path := fmt.Sprintf("/api/v1/inspection/reports/%s/compare/%s", id1, id2)
	data, err := a.client.Do(context.Background(), "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("comparing reports: %w", err)
	}
	var result ComparedReports
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetTrend retrieves inspection trend data for the specified number of days.
func (a *InspectionAPI) GetTrend(days int) ([]InspectionTrendItem, error) {
	path := "/api/v1/inspection/trend"
	var params []string
	if days > 0 {
		params = append(params, "days="+strconv.Itoa(days))
	}
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}

	data, err := a.client.Do(context.Background(), "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("getting trend: %w", err)
	}
	var resp InspectionTrendResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Trend, nil
}
