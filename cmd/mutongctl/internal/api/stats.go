package api

import (
	"context"
	"encoding/json"
	"fmt"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
)

// StatsAPI provides typed access to the Mutong Stats API.
type StatsAPI struct {
	client *client.Client
}

// NewStatsAPI creates a new StatsAPI.
func NewStatsAPI(c *client.Client) *StatsAPI {
	return &StatsAPI{client: c}
}

// Overview represents the resource overview statistics.
type Overview struct {
	TotalNamespaces   int            `json:"total_namespaces"`
	TotalPods         int            `json:"total_pods"`
	TotalDeployments  int            `json:"total_deployments"`
	TotalServices     int            `json:"total_services"`
	TotalNodes        int            `json:"total_nodes"`
	ClusterCount      int            `json:"cluster_count"`
	ResourceBreakdown map[string]int `json:"resource_breakdown,omitempty"`
}

// SyncResult represents the result of a stats sync operation.
type SyncResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Synced  int    `json:"synced"`
}

// GetOverview retrieves the resource overview statistics.
func (a *StatsAPI) GetOverview() (*Overview, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/stats/overview", nil)
	if err != nil {
		return nil, fmt.Errorf("getting stats overview: %w", err)
	}
	var overview Overview
	if err := json.Unmarshal(data, &overview); err != nil {
		return nil, err
	}
	return &overview, nil
}

// SyncStats triggers a manual sync of resource statistics.
func (a *StatsAPI) SyncStats() (*SyncResult, error) {
	data, err := a.client.Do(context.Background(), "POST", "/api/v1/stats/sync", nil)
	if err != nil {
		return nil, fmt.Errorf("syncing stats: %w", err)
	}
	var result SyncResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
