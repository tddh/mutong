package services

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/allegro/bigcache/v3"
	nebula "github.com/vesoft-inc/nebula-go/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// --- Tests for generateLabelUID ---

func TestGenerateLabelUID(t *testing.T) {
	uid := generateLabelUID("app", "myapp")
	expected := "label-app-myapp"
	if uid != expected {
		t.Errorf("Expected %q, got %q", expected, uid)
	}
}

func TestGenerateLabelUID_EmptyValue(t *testing.T) {
	uid := generateLabelUID("app", "")
	expected := "label-app-"
	if uid != expected {
		t.Errorf("Expected %q, got %q", expected, uid)
	}
}

// --- Tests for mapToKeyValuePairs ---

func TestMapToKeyValuePairs(t *testing.T) {
	m := map[string]string{"app": "myapp", "env": "prod"}
	result := mapToKeyValuePairs(m)

	if result == "" {
		t.Fatal("Expected non-empty result")
	}

	// Verify both pairs are present (order may vary)
	if !containsStr(result, "app=myapp") {
		t.Error("Expected 'app=myapp' in result")
	}
	if !containsStr(result, "env=prod") {
		t.Error("Expected 'env=prod' in result")
	}
}

func TestMapToKeyValuePairs_Empty(t *testing.T) {
	result := mapToKeyValuePairs(nil)
	if result != "" {
		t.Errorf("Expected empty string for nil map, got %q", result)
	}

	result = mapToKeyValuePairs(map[string]string{})
	if result != "" {
		t.Errorf("Expected empty string for empty map, got %q", result)
	}
}

func TestMapToKeyValuePairs_SingleEntry(t *testing.T) {
	result := mapToKeyValuePairs(map[string]string{"key": "value"})
	if result != "key=value" {
		t.Errorf("Expected 'key=value', got %q", result)
	}
}

// --- Tests for unmarshalResourceDefine ---

func TestUnmarshalResourceDefine_ValidJSON(t *testing.T) {
	svc := &K8sResoureService{}

	objJSON, _ := json.Marshal(map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]interface{}{"name": "test-pod", "uid": "test-uid"},
	})
	quoted := strconv.Quote(string(objJSON))

	result, err := svc.unmarshalResourceDefine(quoted)
	if err != nil {
		t.Fatalf("unmarshalResourceDefine() error = %v", err)
	}

	if result.GetKind() != "Pod" {
		t.Errorf("Expected kind 'Pod', got %q", result.GetKind())
	}
	if result.GetName() != "test-pod" {
		t.Errorf("Expected name 'test-pod', got %q", result.GetName())
	}
}

func TestUnmarshalResourceDefine_InvalidJSON(t *testing.T) {
	svc := &K8sResoureService{}

	_, err := svc.unmarshalResourceDefine(`"not valid json"`)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestUnmarshalResourceDefine_NotQuoted(t *testing.T) {
	svc := &K8sResoureService{}

	_, err := svc.unmarshalResourceDefine(`{"key":"value"}`)
	if err == nil {
		t.Error("Expected error for unquoted string")
	}
}

// --- Tests for processLabelsRelationship ---

func TestProcessLabelsRelationship_NoLabels(t *testing.T) {
	mockDB := &mockNebulaGraphDB{}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			return nil, bigcache.ErrEntryNotFound
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, mockCache)

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata":   map[string]interface{}{"name": "test-pod", "uid": "test-uid"},
		},
	}

	// Should not panic or call DB when no labels
	svc.processLabelsRelationship(obj)

	if len(mockDB.calls) != 0 {
		t.Errorf("Expected no DB calls for object without labels, got %d", len(mockDB.calls))
	}
}

func TestProcessLabelsRelationship_WithLabels(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			return nil, bigcache.ErrEntryNotFound
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, mockCache)

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name": "test-pod",
				"uid":  "test-uid-123",
				"labels": map[string]interface{}{
					"app": "myapp",
				},
			},
		},
	}

	svc.processLabelsRelationship(obj)

	// Should have 2 calls: INSERT VERTEX Label + INSERT EDGE BelongsToLabel
	if len(mockDB.calls) < 2 {
		t.Errorf("Expected at least 2 DB calls (label insert + edge insert), got %d", len(mockDB.calls))
	}

	// Verify label insert query
	found := false
	for _, call := range mockDB.calls {
		if containsStr(call, "INSERT VERTEX IF NOT EXISTS Label") {
			found = true
			if !containsStr(call, "label-app-myapp") {
				t.Error("Expected label UID in query")
			}
			if !containsStr(call, "app") {
				t.Error("Expected label key in query")
			}
			if !containsStr(call, "myapp") {
				t.Error("Expected label value in query")
			}
		}
	}
	if !found {
		t.Error("Expected Label INSERT query")
	}
}

func TestProcessLabelsRelationship_ExcludedLabel(t *testing.T) {
	mockDB := &mockNebulaGraphDB{}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			if key == "ExcludeLabels-app" {
				return []byte(""), nil
			}
			return nil, bigcache.ErrEntryNotFound
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, mockCache)

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name": "test-pod",
				"uid":  "test-uid-456",
				"labels": map[string]interface{}{
					"app": "myapp",
				},
			},
		},
	}

	svc.processLabelsRelationship(obj)

	// Label is excluded, should not insert
	if len(mockDB.calls) != 0 {
		t.Errorf("Expected no DB calls for excluded label, got %d", len(mockDB.calls))
	}
}

func TestProcessLabelsRelationship_MatchingExcludedValue(t *testing.T) {
	mockDB := &mockNebulaGraphDB{}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			if key == "ExcludeLabels-app" {
				return []byte("myapp"), nil
			}
			return nil, bigcache.ErrEntryNotFound
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, mockCache)

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name": "test-pod",
				"uid":  "test-uid-789",
				"labels": map[string]interface{}{
					"app": "myapp",
				},
			},
		},
	}

	svc.processLabelsRelationship(obj)

	// Label value matches excluded value, should not insert
	if len(mockDB.calls) != 0 {
		t.Errorf("Expected no DB calls for matching excluded value, got %d", len(mockDB.calls))
	}
}

// --- Tests for processMutatingWebhookConfigurationRelationship ---

func TestProcessMutatingWebhookConfigurationRelationship_NoPanic(t *testing.T) {
	svc := &K8sResoureService{
		logger: &mockNebulaLogger{},
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "admissionregistration.k8s.io/v1",
			"kind":       "MutatingWebhookConfiguration",
			"metadata":   map[string]interface{}{"name": "test-webhook", "uid": "test-uid"},
		},
	}

	// This is a stub function, should not panic
	svc.processMutatingWebhookConfigurationRelationship(obj)
}

// --- Tests for processOwnerReferences ---

func TestProcessOwnerReferences_NoOwnerRefs(t *testing.T) {
	mockDB := &mockNebulaGraphDB{}
	svc := newTestK8sServiceWithMocks(mockDB, nil)

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name": "test-pod",
				"uid":  "test-uid",
			},
		},
	}

	svc.processOwnerReferences(obj)

	if len(mockDB.calls) != 0 {
		t.Errorf("Expected no DB calls for object without ownerReferences, got %d", len(mockDB.calls))
	}
}

func TestProcessOwnerReferences_WithOwnerRefs(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, nil)

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name": "test-pod",
				"uid":  "pod-uid-123",
				"ownerReferences": []interface{}{
					map[string]interface{}{
						"apiVersion": "apps/v1",
						"kind":       "ReplicaSet",
						"name":       "test-rs",
						"uid":        "rs-uid-456",
					},
				},
			},
		},
	}

	svc.processOwnerReferences(obj)

	// Should have 1 call: INSERT EDGE OwnedBy
	if len(mockDB.calls) != 1 {
		t.Errorf("Expected 1 DB call, got %d", len(mockDB.calls))
	}
	if !containsStr(mockDB.calls[0], "OwnedBy") {
		t.Errorf("Expected OwnedBy edge, got %q", mockDB.calls[0])
	}
}

// --- Tests for processNamespaceRelationship ---

func TestProcessNamespaceRelationship_CreatesNamespaceAndEdge(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, &mockNebulaCache{})

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      "test-pod",
				"uid":       "pod-uid-789",
				"namespace": "default",
			},
		},
	}

	svc.processNamespaceRelationship(obj)

	if len(mockDB.calls) < 1 {
		t.Errorf("Expected at least 1 DB call (query), got %d", len(mockDB.calls))
	}

	hasQuery := false
	for _, call := range mockDB.calls {
		if containsStr(call, "LOOKUP ON") && containsStr(call, "Namespace") {
			hasQuery = true
		}
	}
	if !hasQuery {
		t.Error("Expected Namespace LOOKUP ON query")
	}
}

func TestProcessNamespaceRelationship_NoNamespace(t *testing.T) {
	mockDB := &mockNebulaGraphDB{}
	svc := newTestK8sServiceWithMocks(mockDB, &mockNebulaCache{})

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Node",
			"metadata": map[string]interface{}{
				"name": "test-node",
				"uid":  "node-uid",
			},
		},
	}

	svc.processNamespaceRelationship(obj)

	if len(mockDB.calls) != 0 {
		t.Errorf("Expected no DB calls for cluster-scoped resource, got %d", len(mockDB.calls))
	}
}
