package api

import (
	"context"
	"encoding/json"
	"fmt"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
)

// ClusterAPI provides typed access to the Mutong Cluster API.
type ClusterAPI struct {
	client *client.Client
}

// NewClusterAPI creates a new ClusterAPI.
func NewClusterAPI(c *client.Client) *ClusterAPI {
	return &ClusterAPI{client: c}
}

// Cluster represents a cluster from the Mutong API.
type Cluster struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Nodes  int    `json:"nodes"`
}

// ClusterHealth represents cluster health information.
type ClusterHealth struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// ClusterStats represents cluster statistics.
type ClusterStats struct {
	Cluster string                 `json:"cluster"`
	Stats   map[string]interface{} `json:"stats"`
}

// clusterListResponse matches API: {"clusters":[...],"count":N}
type clusterListResponse struct {
	Clusters []Cluster `json:"clusters"`
	Count    int       `json:"count"`
}

// ListClusters retrieves all clusters.
func (a *ClusterAPI) ListClusters() ([]Cluster, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/clusters/", nil)
	if err != nil {
		return nil, fmt.Errorf("listing clusters: %w", err)
	}
	var resp clusterListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Clusters, nil
}

// GetClusterHealth retrieves health information for a specific cluster.
func (a *ClusterAPI) GetClusterHealth(name string) (*ClusterHealth, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/clusters/"+name+"/health", nil)
	if err != nil {
		return nil, fmt.Errorf("getting cluster health: %w", err)
	}
	var health ClusterHealth
	if err := json.Unmarshal(data, &health); err != nil {
		return nil, err
	}
	return &health, nil
}

// GetClusterStats retrieves statistics for a specific cluster.
func (a *ClusterAPI) GetClusterStats(name string) (*ClusterStats, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/clusters/"+name+"/stats", nil)
	if err != nil {
		return nil, fmt.Errorf("getting cluster stats: %w", err)
	}
	var stats ClusterStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return nil, err
	}
	return &stats, nil
}
