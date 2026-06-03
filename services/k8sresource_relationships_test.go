package services

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/allegro/bigcache/v3"
	nebula "github.com/vesoft-inc/nebula-go/v3"
	corev1 "k8s.io/api/core/v1"
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

func TestProcessWebhookConfigurationRelationship_NoWebhooks(t *testing.T) {
	mockDB := &mockNebulaGraphDB{}
	svc := newTestK8sServiceWithMocks(mockDB, &mockNebulaCache{})

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "admissionregistration.k8s.io/v1",
			"kind":       "MutatingWebhookConfiguration",
			"metadata": map[string]interface{}{
				"name": "test-webhook",
				"uid":  "webhook-uid-1",
			},
		},
	}

	svc.processWebhookConfigurationRelationship(obj)

	if len(mockDB.calls) != 0 {
		t.Errorf("Expected no DB calls when no webhooks, got %d calls", len(mockDB.calls))
	}
}

func TestProcessWebhookConfigurationRelationship_URLMode(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, &mockNebulaCache{})

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "admissionregistration.k8s.io/v1",
			"kind":       "MutatingWebhookConfiguration",
			"metadata": map[string]interface{}{
				"name":      "test-webhook",
				"uid":       "webhook-uid-2",
				"namespace": "default",
			},
			"webhooks": []interface{}{
				map[string]interface{}{
					"name": "url-webhook",
					"clientConfig": map[string]interface{}{
						"url": "https://external.example.com/mutate",
					},
				},
			},
		},
	}

	svc.processWebhookConfigurationRelationship(obj)

	// CleanupOutgoingEdgesByType does a GO FROM query, and no INSERT EDGE should follow
	for _, call := range mockDB.calls {
		if containsStr(call, "WebhookRefSvc") && !containsStr(call, "GO FROM") {
			t.Errorf("Expected no WebhookRefSvc insert for URL-mode webhook, got %q", call)
		}
	}
}

func TestProcessWebhookConfigurationRelationship_ServiceFound(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			if key == "uid:Service:default:webhook-svc" {
				return []byte("svc-uid-123"), nil
			}
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, mockCache)

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "admissionregistration.k8s.io/v1",
			"kind":       "MutatingWebhookConfiguration",
			"metadata": map[string]interface{}{
				"name":      "test-webhook",
				"uid":       "webhook-uid-3",
				"namespace": "default",
			},
			"webhooks": []interface{}{
				map[string]interface{}{
					"name": "pod-mutator",
					"clientConfig": map[string]interface{}{
						"service": map[string]interface{}{
							"name":      "webhook-svc",
							"namespace": "default",
							"path":      "/mutate",
							"port":      float64(443),
						},
					},
				},
			},
		},
	}

	svc.processWebhookConfigurationRelationship(obj)

	// Verify edge insert
	found := false
	for _, call := range mockDB.calls {
		if containsStr(call, "INSERT EDGE WebhookRefSvc") {
			found = true
			if !containsStr(call, "svc-uid-123") {
				t.Error("Expected edge to reference svc-uid-123")
			}
			if !containsStr(call, "pod-mutator") {
				t.Error("Expected edge to include webhook_name 'pod-mutator'")
			}
			if !containsStr(call, "/mutate") {
				t.Error("Expected edge to include path '/mutate'")
			}
		}
	}
	if !found {
		t.Error("Expected WebhookRefSvc edge insert")
	}
}

func TestProcessWebhookConfigurationRelationship_ServiceNotFound(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	// Cache returns an error → lookupUID falls through to DB query → returns empty
	svc := newTestK8sServiceWithMocks(mockDB, &mockNebulaCache{})

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "admissionregistration.k8s.io/v1",
			"kind":       "ValidatingWebhookConfiguration",
			"metadata": map[string]interface{}{
				"name":      "test-webhook",
				"uid":       "webhook-uid-4",
				"namespace": "default",
			},
			"webhooks": []interface{}{
				map[string]interface{}{
					"name": "policy-check",
					"clientConfig": map[string]interface{}{
						"service": map[string]interface{}{
							"name": "nonexistent-svc",
						},
					},
				},
			},
		},
	}

	svc.processWebhookConfigurationRelationship(obj)

	// No WebhookRefSvc insert should be generated
	for _, call := range mockDB.calls {
		if containsStr(call, "INSERT EDGE WebhookRefSvc") {
			t.Errorf("Expected no WebhookRefSvc insert for missing Service, got %q", call)
		}
	}
}

func TestProcessWebhookConfigurationRelationship_NamespaceFallback(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			// Service namespace should fall back to webhook namespace "kube-system"
			if key == "uid:Service:kube-system:webhook-svc" {
				return []byte("svc-uid-fallback"), nil
			}
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, mockCache)

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "admissionregistration.k8s.io/v1",
			"kind":       "MutatingWebhookConfiguration",
			"metadata": map[string]interface{}{
				"name":      "test-webhook",
				"uid":       "webhook-uid-5",
				"namespace": "kube-system",
			},
			"webhooks": []interface{}{
				map[string]interface{}{
					"name": "ns-fallback",
					"clientConfig": map[string]interface{}{
						"service": map[string]interface{}{
							"name": "webhook-svc",
							// no namespace → falls back to "kube-system"
						},
					},
				},
			},
		},
	}

	svc.processWebhookConfigurationRelationship(obj)

	found := false
	for _, call := range mockDB.calls {
		if containsStr(call, "INSERT EDGE WebhookRefSvc") {
			found = true
			if !containsStr(call, "svc-uid-fallback") {
				t.Error("Expected namespace fallback to resolve svc-uid-fallback")
			}
		}
	}
	if !found {
		t.Error("Expected WebhookRefSvc edge insert with namespace fallback")
	}
}

func TestProcessWebhookConfigurationRelationship_MultipleWebhooks(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			switch key {
			case "uid:Service:default:svc-a":
				return []byte("svc-uid-a"), nil
			case "uid:Service:default:svc-b":
				return []byte("svc-uid-b"), nil
			}
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, mockCache)

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "admissionregistration.k8s.io/v1",
			"kind":       "MutatingWebhookConfiguration",
			"metadata": map[string]interface{}{
				"name":      "multi-webhook",
				"uid":       "webhook-uid-6",
				"namespace": "default",
			},
			"webhooks": []interface{}{
				map[string]interface{}{
					"name": "hook-a",
					"clientConfig": map[string]interface{}{
						"service": map[string]interface{}{
							"name": "svc-a",
						},
					},
				},
				map[string]interface{}{
					"name": "hook-b",
					"clientConfig": map[string]interface{}{
						"url": "https://external.example.com", // URL mode
					},
				},
				map[string]interface{}{
					"name": "hook-c",
					"clientConfig": map[string]interface{}{
						"service": map[string]interface{}{
							"name": "svc-b",
							"path": "/validate",
						},
					},
				},
			},
		},
	}

	svc.processWebhookConfigurationRelationship(obj)

	insertCount := 0
	for _, call := range mockDB.calls {
		if containsStr(call, "INSERT EDGE WebhookRefSvc") {
			insertCount++
		}
	}
	if insertCount != 2 {
		t.Errorf("Expected 2 WebhookRefSvc inserts (hook-b is URL mode), got %d", insertCount)
	}
}

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

	// Should not panic — delegates to processWebhookConfigurationRelationship
	svc.processMutatingWebhookConfigurationRelationship(obj)
}

func TestProcessValidatingWebhookConfigurationRelationship_NoPanic(t *testing.T) {
	svc := &K8sResoureService{
		logger: &mockNebulaLogger{},
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "admissionregistration.k8s.io/v1",
			"kind":       "ValidatingWebhookConfiguration",
			"metadata":   map[string]interface{}{"name": "test-webhook", "uid": "test-uid"},
		},
	}

	// Should not panic — delegates to processWebhookConfigurationRelationship
	svc.processValidatingWebhookConfigurationRelationship(obj)
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

// --- Tests for processNetworkPolicyRelationship ---

func TestProcessNP_PodSelectorMatchLabels(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, &mockNebulaCache{})

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "NetworkPolicy",
			"apiVersion": "networking.k8s.io/v1",
			"metadata": map[string]interface{}{
				"name": "test-np", "uid": "np-uid-1", "namespace": "default",
			},
			"spec": map[string]interface{}{
				"podSelector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"app": "nginx",
					},
				},
			},
		},
	}

	svc.processNetworkPolicyRelationship(obj)

	found := false
	for _, call := range mockDB.calls {
		if containsStr(call, "INSERT EDGE NpSelectsByLabel") {
			found = true
			if !containsStr(call, "label-app-nginx") {
				t.Error("Expected NpSelectsByLabel to reference label-app-nginx")
			}
		}
	}
	if !found {
		t.Error("Expected NpSelectsByLabel edge for podSelector.matchLabels")
	}
}

func TestProcessNP_EmptyPodSelector(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, &mockNebulaCache{})

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "NetworkPolicy",
			"apiVersion": "networking.k8s.io/v1",
			"metadata": map[string]interface{}{
				"name": "test-np", "uid": "np-uid-2", "namespace": "default",
			},
			"spec": map[string]interface{}{
				"podSelector": map[string]interface{}{},
			},
		},
	}

	svc.processNetworkPolicyRelationship(obj)

	for _, call := range mockDB.calls {
		if containsStr(call, "INSERT EDGE NpSelectsByLabel") {
			t.Errorf("Expected no NpSelectsByLabel for empty podSelector, got %q", call)
		}
	}
}

func TestProcessNP_IngressPodSelector(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, &mockNebulaCache{})

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "NetworkPolicy",
			"apiVersion": "networking.k8s.io/v1",
			"metadata": map[string]interface{}{
				"name": "test-np", "uid": "np-uid-3", "namespace": "default",
			},
			"spec": map[string]interface{}{
				"podSelector": map[string]interface{}{},
				"ingress": []interface{}{
					map[string]interface{}{
						"from": []interface{}{
							map[string]interface{}{
								"podSelector": map[string]interface{}{
									"matchLabels": map[string]interface{}{
										"role": "frontend",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	svc.processNetworkPolicyRelationship(obj)

	found := false
	for _, call := range mockDB.calls {
		if containsStr(call, "NpSelectsByLabel") && containsStr(call, "label-role-frontend") {
			found = true
		}
	}
	if !found {
		t.Error("Expected NpSelectsByLabel for ingress.from[].podSelector")
	}
}

func TestProcessNP_EgressToNamespaceSelector(t *testing.T) {
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
			"kind":       "NetworkPolicy",
			"apiVersion": "networking.k8s.io/v1",
			"metadata": map[string]interface{}{
				"name": "test-np", "uid": "np-uid-4", "namespace": "default",
			},
			"spec": map[string]interface{}{
				"podSelector": map[string]interface{}{},
				"egress": []interface{}{
					map[string]interface{}{
						"to": []interface{}{
							map[string]interface{}{
								"namespaceSelector": map[string]interface{}{
									"matchLabels": map[string]interface{}{
										"env": "prod",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// addNpNamespaceEdge 查询 BelongsToLabel 反查 Namespace；
	// unit test mock 返回 nil/nil，模拟"无匹配 Namespace"场景。
	// 正向路径（找到 Namespace 并建 NpSelectsNs 边）需要构造 nebula.ResultSet，
	// 适合在集成测试中覆盖。
	svc.processNetworkPolicyRelationship(obj)
}

func TestProcessNP_IPBlockSkipped(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, &mockNebulaCache{})

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "NetworkPolicy",
			"apiVersion": "networking.k8s.io/v1",
			"metadata": map[string]interface{}{
				"name": "test-np", "uid": "np-uid-5", "namespace": "default",
			},
			"spec": map[string]interface{}{
				"podSelector": map[string]interface{}{},
				"ingress": []interface{}{
					map[string]interface{}{
						"from": []interface{}{
							map[string]interface{}{
								"ipBlock": map[string]interface{}{
									"cidr": "10.0.0.0/8",
								},
							},
						},
					},
				},
			},
		},
	}

	svc.processNetworkPolicyRelationship(obj)

	for _, call := range mockDB.calls {
		if containsStr(call, "INSERT EDGE") && containsStr(call, "NpSelects") {
			t.Errorf("Expected no NpSelects edges for ipBlock peer, got %q", call)
		}
	}
}

func TestProcessNP_MixedPeers(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, &mockNebulaCache{})

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "NetworkPolicy",
			"apiVersion": "networking.k8s.io/v1",
			"metadata": map[string]interface{}{
				"name": "test-np", "uid": "np-uid-6", "namespace": "default",
			},
			"spec": map[string]interface{}{
				"podSelector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"app": "web",
					},
				},
				"ingress": []interface{}{
					map[string]interface{}{
						"from": []interface{}{
							map[string]interface{}{
								"podSelector": map[string]interface{}{
									"matchLabels": map[string]interface{}{
										"role": "api",
									},
								},
								"namespaceSelector": map[string]interface{}{
									"matchLabels": map[string]interface{}{
										"env": "staging",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	svc.processNetworkPolicyRelationship(obj)

	nLabelCount := 0
	for _, call := range mockDB.calls {
		if containsStr(call, "INSERT EDGE NpSelectsByLabel") {
			nLabelCount++
		}
	}
	// spec.podSelector(app=web) + ingress.podSelector(role=api) = 2
	if nLabelCount != 2 {
		t.Errorf("Expected 2 NpSelectsByLabel inserts, got %d", nLabelCount)
	}
}

func TestProcessNP_NoSpec(t *testing.T) {
	mockDB := &mockNebulaGraphDB{}
	svc := newTestK8sServiceWithMocks(mockDB, &mockNebulaCache{})

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "NetworkPolicy",
			"apiVersion": "networking.k8s.io/v1",
			"metadata": map[string]interface{}{
				"name": "test-np", "uid": "np-uid-7", "namespace": "default",
			},
		},
	}

	// Should not panic
	svc.processNetworkPolicyRelationship(obj)
}

// --- Tests for processPriorityClassRelationship ---

func TestProcessPrioClass_HasClass(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			if key == "uid:PriorityClass::high-priority" {
				return []byte("pc-uid-1"), nil
			}
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, mockCache)

	pod := &corev1.Pod{}
	pod.UID = "pod-uid-1"
	pod.Spec.PriorityClassName = "high-priority"

	svc.processPriorityClassRelationship(pod)

	found := false
	for _, call := range mockDB.calls {
		if containsStr(call, "INSERT EDGE PodPrioClass") {
			found = true
		}
	}
	if !found {
		t.Error("Expected PodPrioClass edge")
	}
}

func TestProcessPrioClass_EmptyClass(t *testing.T) {
	mockDB := &mockNebulaGraphDB{}
	svc := newTestK8sServiceWithMocks(mockDB, &mockNebulaCache{})

	pod := &corev1.Pod{}
	pod.UID = "pod-uid-2"

	svc.processPriorityClassRelationship(pod)

	if len(mockDB.calls) != 0 {
		t.Errorf("Expected no DB calls for empty PriorityClassName, got %d", len(mockDB.calls))
	}
}

// --- Tests for processRuntimeClassRelationship ---

func TestProcessRuntimeClass_HasClass(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			if key == "uid:RuntimeClass::kata" {
				return []byte("rc-uid-1"), nil
			}
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, mockCache)

	runtimeClass := "kata"
	pod := &corev1.Pod{}
	pod.UID = "pod-uid-3"
	pod.Spec.RuntimeClassName = &runtimeClass

	svc.processRuntimeClassRelationship(pod)

	found := false
	for _, call := range mockDB.calls {
		if containsStr(call, "INSERT EDGE PodRuntimeClass") {
			found = true
		}
	}
	if !found {
		t.Error("Expected PodRuntimeClass edge")
	}
}

func TestProcessRuntimeClass_NilClass(t *testing.T) {
	mockDB := &mockNebulaGraphDB{}
	svc := newTestK8sServiceWithMocks(mockDB, &mockNebulaCache{})

	pod := &corev1.Pod{}
	pod.UID = "pod-uid-4"

	svc.processRuntimeClassRelationship(pod)

	if len(mockDB.calls) != 0 {
		t.Errorf("Expected no DB calls for nil RuntimeClassName, got %d", len(mockDB.calls))
	}
}

func TestProcessRuntimeClass_EmptyString(t *testing.T) {
	mockDB := &mockNebulaGraphDB{}
	svc := newTestK8sServiceWithMocks(mockDB, &mockNebulaCache{})

	emptyRC := ""
	pod := &corev1.Pod{}
	pod.UID = "pod-uid-5"
	pod.Spec.RuntimeClassName = &emptyRC

	svc.processRuntimeClassRelationship(pod)

	if len(mockDB.calls) != 0 {
		t.Errorf("Expected no DB calls for empty RuntimeClassName string, got %d", len(mockDB.calls))
	}
}

// --- Tests for processVolumeAttachmentRelationship ---

func TestProcessVA_BothEdges(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			switch key {
			case "uid:Node::worker-1":
				return []byte("node-uid-1"), nil
			case "uid:PersistentVolume::pv-001":
				return []byte("pv-uid-1"), nil
			}
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, mockCache)

	pvName := "pv-001"
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "VolumeAttachment",
			"apiVersion": "storage.k8s.io/v1",
			"metadata": map[string]interface{}{
				"name": "va-1", "uid": "va-uid-1",
			},
			"spec": map[string]interface{}{
				"attacher": "ebs.csi.aws.com",
				"nodeName": "worker-1",
				"source": map[string]interface{}{
					"persistentVolumeName": pvName,
				},
			},
		},
	}

	svc.processVolumeAttachmentRelationship(obj)

	foundNode := false
	foundPV := false
	for _, call := range mockDB.calls {
		if containsStr(call, "INSERT EDGE VolAttachToNode") {
			foundNode = true
		}
		if containsStr(call, "INSERT EDGE VolAttachToPV") {
			foundPV = true
		}
	}
	if !foundNode {
		t.Error("Expected VolAttachToNode edge")
	}
	if !foundPV {
		t.Error("Expected VolAttachToPV edge")
	}
}

func TestProcessVA_PVNameNil(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			if key == "uid:Node::worker-2" {
				return []byte("node-uid-2"), nil
			}
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, mockCache)

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "VolumeAttachment",
			"apiVersion": "storage.k8s.io/v1",
			"metadata": map[string]interface{}{
				"name": "va-2", "uid": "va-uid-2",
			},
			"spec": map[string]interface{}{
				"nodeName": "worker-2",
				"source":   map[string]interface{}{},
			},
		},
	}

	svc.processVolumeAttachmentRelationship(obj)

	for _, call := range mockDB.calls {
		if containsStr(call, "INSERT EDGE VolAttachToPV") {
			t.Errorf("Expected no VolAttachToPV when persistentVolumeName is nil, got %q", call)
		}
	}
}

func TestProcessVA_NodeNameEmpty(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			if key == "uid:PersistentVolume::pv-002" {
				return []byte("pv-uid-2"), nil
			}
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, mockCache)

	pvName := "pv-002"
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "VolumeAttachment",
			"apiVersion": "storage.k8s.io/v1",
			"metadata": map[string]interface{}{
				"name": "va-3", "uid": "va-uid-3",
			},
			"spec": map[string]interface{}{
				"nodeName": "",
				"source": map[string]interface{}{
					"persistentVolumeName": pvName,
				},
			},
		},
	}

	svc.processVolumeAttachmentRelationship(obj)

	for _, call := range mockDB.calls {
		if containsStr(call, "INSERT EDGE VolAttachToNode") {
			t.Errorf("Expected no VolAttachToNode when nodeName is empty, got %q", call)
		}
	}
}
