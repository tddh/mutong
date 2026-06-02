package diagnosis

import (
	"encoding/json"
	"testing"

	"gitee.com/tddh/mutong/models/diagnosis"
)

func TestFormatResources(t *testing.T) {
	resources := []diagnosis.ResourceRef{
		{Kind: "Pod", Name: "test-pod"},
		{Kind: "Service", Name: "test-svc"},
	}

	result := formatResources(resources)

	if len(result) != 2 {
		t.Errorf("expected 2 resources, got %d", len(result))
	}
	if result[0] != "Pod/test-pod" {
		t.Errorf("expected 'Pod/test-pod', got '%s'", result[0])
	}
	if result[1] != "Service/test-svc" {
		t.Errorf("expected 'Service/test-svc', got '%s'", result[1])
	}
}

func TestFormatResources_Empty(t *testing.T) {
	result := formatResources([]diagnosis.ResourceRef{})
	if len(result) != 0 {
		t.Errorf("expected 0 resources, got %d", len(result))
	}
}

func TestTopologySnapshot_MarshalJSON(t *testing.T) {
	snapshot := &diagnosis.TopologySnapshot{
		Nodes: []diagnosis.TopologyNode{
			{UID: "uid-1", Kind: "Pod", Name: "test-pod", Namespace: "default"},
		},
		Edges: []diagnosis.TopologyEdge{
			{From: "uid-1", To: "uid-2", Type: "RunsOn"},
		},
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("failed to marshal snapshot: %v", err)
	}

	if len(data) == 0 {
		t.Error("marshaled data should not be empty")
	}
}
