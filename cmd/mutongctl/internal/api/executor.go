package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
)

// ExecutorAPI provides typed access to the Mutong Executor API.
type ExecutorAPI struct {
	client *client.Client
}

// NewExecutorAPI creates a new ExecutorAPI.
func NewExecutorAPI(c *client.Client) *ExecutorAPI {
	return &ExecutorAPI{client: c}
}

// ExecutionPlan describes a planned remediation action.
type ExecutionPlan struct {
	ID           string  `json:"id"`
	Action       string  `json:"action"`
	Target       string  `json:"target"`
	Namespace    string  `json:"namespace"`
	ResourceName string  `json:"resourceName"`
	Reason       string  `json:"reason"`
	Confidence   float64 `json:"confidence"`
	Risk         string  `json:"risk"`
	Replicas     int32   `json:"replicas"`
	MinReplicas  int32   `json:"minReplicas"`
	MaxReplicas  int32   `json:"maxReplicas"`
	TargetCPU    int32   `json:"targetCpu"`
	TargetMemory int32   `json:"targetMemory,omitempty"`
}

// ExecutionResult records the outcome of an execution.
type ExecutionResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	AuditID string `json:"auditId"`
}

// AuditLog stores execution history.
type AuditLog struct {
	ID           string          `json:"id"`
	Plan         ExecutionPlan   `json:"plan"`
	Result       ExecutionResult `json:"result"`
	AutoExecuted bool            `json:"autoExecuted"`
	ApprovedBy   string          `json:"approvedBy,omitempty"`
}

// ExecutorStatus represents the executor status.
type ExecutorStatus struct {
	Enabled       bool   `json:"enabled"`
	AutoMode      bool   `json:"autoMode"`
	AuditLogCount int    `json:"auditLogCount"`
	AuditLogType  string `json:"auditLogType"`
}

// Execute submits an execution plan to the executor.
func (a *ExecutorAPI) Execute(plan ExecutionPlan) (*ExecutionResult, error) {
	data, err := a.client.Do(context.Background(), "POST", "/api/v1/executor/execute", plan)
	if err != nil {
		return nil, fmt.Errorf("executing plan: %w", err)
	}
	var result ExecutionResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetAuditLogs retrieves audit logs, optionally filtered.
func (a *ExecutorAPI) GetAuditLogs(action, namespace, autoExecuted string) ([]AuditLog, error) {
	path := "/api/v1/executor/audit"
	var params []string
	if action != "" {
		params = append(params, "action="+action)
	}
	if namespace != "" {
		params = append(params, "namespace="+namespace)
	}
	if autoExecuted != "" {
		params = append(params, "autoExecuted="+autoExecuted)
	}
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}

	data, err := a.client.Do(context.Background(), "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("getting audit logs: %w", err)
	}
	var logs []AuditLog
	if err := json.Unmarshal(data, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

// GetStatus retrieves the executor status.
func (a *ExecutorAPI) GetStatus() (*ExecutorStatus, error) {
	data, err := a.client.Do(context.Background(), "GET", "/api/v1/executor/status", nil)
	if err != nil {
		return nil, fmt.Errorf("getting executor status: %w", err)
	}
	var status ExecutorStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, err
	}
	return &status, nil
}
