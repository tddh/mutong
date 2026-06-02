package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
)

// AlertAPI provides typed access to the Mutong Alert API.
type AlertAPI struct {
	client *client.Client
}

// NewAlertAPI creates a new AlertAPI.
func NewAlertAPI(c *client.Client) *AlertAPI {
	return &AlertAPI{client: c}
}

// Alert represents an alert from the Mutong API.
type Alert struct {
	Fingerprint   string            `json:"fingerprint"`
	Status        string            `json:"status"`
	Labels        map[string]string `json:"labels"`
	Annotations   map[string]string `json:"annotations"`
	ResourceUID   string            `json:"resourceUID"`
	ResourceType  string            `json:"resourceType"`
	ResourceName  string            `json:"resourceName"`
	Namespace     string            `json:"namespace"`
	NodeName      string            `json:"nodeName"`
	StartsAt      string            `json:"startsAt"`
	EndsAt        string            `json:"endsAt"`
	RelatedAlerts []string          `json:"relatedAlerts"`
}

// listAlertsResponse matches the API response wrapper: {"alerts":[...],"count":N}
type listAlertsResponse struct {
	Alerts []Alert `json:"alerts"`
	Count  int     `json:"count"`
}

// ListAlerts retrieves active alerts, optionally filtered by severity and namespace.
func (a *AlertAPI) ListAlerts(severity, namespace string) ([]Alert, error) {
	path := "/api/v1/alerts/"
	var params []string
	if severity != "" {
		params = append(params, "severity="+severity)
	}
	if namespace != "" {
		params = append(params, "namespace="+namespace)
	}
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}
	data, err := a.client.Do(context.Background(), "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("listing alerts: %w", err)
	}
	var resp listAlertsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Alerts, nil
}

// GetAlert retrieves a single alert by fingerprint.
func (a *AlertAPI) GetAlert(fingerprint string) (*Alert, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/alerts/"+fingerprint, nil)
	if err != nil {
		return nil, fmt.Errorf("getting alert: %w", err)
	}
	var alert Alert
	if err := json.Unmarshal(data, &alert); err != nil {
		return nil, err
	}
	return &alert, nil
}

// SuppressionStatus represents the alert suppression engine state.
type SuppressionStatus struct {
	RuleEngineEnabled     bool     `json:"ruleEngineEnabled"`
	TimeWindowSeconds     int      `json:"timeWindowSeconds"`
	MaxDepth              int      `json:"maxDepth"`
	ActiveAlertsCount     int      `json:"activeAlertsCount"`
	SeverityExceptions    []string `json:"severityExceptions"`
	CriticalityExceptions []string `json:"criticalityExceptions"`
}

// GetSuppressionStatus retrieves the current suppression status.
func (a *AlertAPI) GetSuppressionStatus() (*SuppressionStatus, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/alerts/suppression/status", nil)
	if err != nil {
		return nil, fmt.Errorf("getting suppression status: %w", err)
	}
	var status SuppressionStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, err
	}
	return &status, nil
}
