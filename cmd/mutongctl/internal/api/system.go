package api

import (
	"context"
	"encoding/json"
	"fmt"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
)

// SystemAPI provides typed access to the Mutong System API.
type SystemAPI struct {
	client *client.Client
}

// NewSystemAPI creates a new SystemAPI.
func NewSystemAPI(c *client.Client) *SystemAPI {
	return &SystemAPI{client: c}
}

// SystemStatus represents the overall system status.
type SystemStatus struct {
	Version    string            `json:"version"`
	Uptime     string            `json:"uptime"`
	Components map[string]string `json:"components"`
}

// GetSystemStatus retrieves the system status.
func (a *SystemAPI) GetSystemStatus() (*SystemStatus, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/system/status", nil)
	if err != nil {
		return nil, fmt.Errorf("getting system status: %w", err)
	}
	var status SystemStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, err
	}
	return &status, nil
}
