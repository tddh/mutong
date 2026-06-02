package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
)

// BusinessAPI provides typed access to the Mutong Business Topology API.
type BusinessAPI struct {
	client *client.Client
}

// NewBusinessAPI creates a new BusinessAPI.
func NewBusinessAPI(c *client.Client) *BusinessAPI {
	return &BusinessAPI{client: c}
}

// BusinessAppNode represents a business application node.
type BusinessAppNode struct {
	ID           string `json:"id"`
	AppName      string `json:"appName"`
	Namespace    string `json:"namespace"`
	Criticality  string `json:"criticality"`
	Environment  string `json:"environment"`
	Team         string `json:"team"`
	BusinessUnit string `json:"businessUnit"`
}

// CallEdge represents a call relationship between business apps.
type CallEdge struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Relation string `json:"relation"`
}

// BusinessTopologyGraph represents the full business topology.
type BusinessTopologyGraph struct {
	Nodes []BusinessAppNode `json:"nodes"`
	Edges []CallEdge        `json:"edges"`
}

// appsResponse mirrors the server response wrapper.
type appsResponse struct {
	Apps []BusinessAppNode `json:"apps"`
}

// ListApps retrieves business applications, optionally filtered.
func (a *BusinessAPI) ListApps(businessUnit, team, namespace string) ([]BusinessAppNode, error) {
	path := "/api/v1/business-topology/apps"
	var params []string
	if businessUnit != "" {
		params = append(params, "businessUnit="+businessUnit)
	}
	if team != "" {
		params = append(params, "team="+team)
	}
	if namespace != "" {
		params = append(params, "namespace="+namespace)
	}
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}

	data, err := a.client.Do(context.Background(), "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("listing business apps: %w", err)
	}
	var resp appsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Apps, nil
}

// GetGraph retrieves the full business topology graph.
func (a *BusinessAPI) GetGraph(businessUnit, team string) (*BusinessTopologyGraph, error) {
	path := "/api/v1/business-topology/graph"
	var params []string
	if businessUnit != "" {
		params = append(params, "businessUnit="+businessUnit)
	}
	if team != "" {
		params = append(params, "team="+team)
	}
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}

	data, err := a.client.Do(context.Background(), "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("getting business topology graph: %w", err)
	}
	var graph BusinessTopologyGraph
	if err := json.Unmarshal(data, &graph); err != nil {
		return nil, err
	}
	return &graph, nil
}
