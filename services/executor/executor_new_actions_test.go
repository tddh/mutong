package executor

import (
	"context"
	"testing"

	ex "gitee.com/tddh/mutong/models/executor"
)

func TestNewActionTypesExist(t *testing.T) {
	actions := []ex.ActionType{
		ex.ActionUpdateConfigMap,
		ex.ActionUpdateSecret,
		ex.ActionUpdateResourceLimits,
		ex.ActionUpdateDeploymentImage,
		ex.ActionUpdateAnnotations,
		ex.ActionUpdateLabels,
	}
	expected := []string{
		"update_configmap",
		"update_secret",
		"update_resource_limits",
		"update_deployment_image",
		"update_annotations",
		"update_labels",
	}
	for i, a := range actions {
		if string(a) != expected[i] {
			t.Errorf("ActionType %v = %q, want %q", a, string(a), expected[i])
		}
	}
}

func TestExecutionPlanNewFields(t *testing.T) {
	plan := ex.ExecutionPlan{
		ConfigData:    map[string]string{"key": "val"},
		Annotations:   map[string]string{"ann": "val"},
		Labels:        map[string]string{"lbl": "val"},
		Image:         "nginx:1.21",
		ContainerName: "app",
	}
	if plan.ConfigData["key"] != "val" {
		t.Error("ConfigData field not working")
	}
	if plan.Image != "nginx:1.21" {
		t.Error("Image field not working")
	}
	if plan.ContainerName != "app" {
		t.Error("ContainerName field not working")
	}
}

func TestExecutorRejectsUnknownAction(t *testing.T) {
	e := &K8sExecutor{}
	_, err := e.Execute(context.Background(), ex.ExecutionPlan{Action: ex.ActionType("nonexistent_action")})
	if err == nil {
		t.Error("expected error for unknown action type")
	}
}
