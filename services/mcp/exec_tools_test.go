package mcp

import (
	"context"
	"testing"
)

func TestIsExecTool(t *testing.T) {
	for _, name := range []string{
		"restart_pod_safe", "rollout_restart", "scale_deployment",
		"update_deployment_image", "adjust_resource_limits", "delete_pod", "rollout_undo",
	} {
		if !IsExecTool(name) {
			t.Errorf("%q should be classified as an execution tool", name)
		}
	}
	for _, name := range []string{"query_topology", "get_active_alerts", "search_logs", ""} {
		if IsExecTool(name) {
			t.Errorf("%q must not be classified as an execution tool", name)
		}
	}
}

func TestExecuteToolUnknown(t *testing.T) {
	s := &Server{tools: map[string]ToolHandler{}}
	if _, err := s.ExecuteTool(context.TODO(), "does_not_exist", nil); err == nil {
		t.Fatal("unknown tool must return an error")
	}
}
