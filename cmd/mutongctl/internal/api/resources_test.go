package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
)

func TestListPods(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		resources := []K8sResource{
			{UID: "uid-1", Name: "nginx", Kind: "Pod", Namespace: "default", Status: "Running"},
			{UID: "uid-2", Name: "redis", Kind: "Pod", Namespace: "default", Status: "Pending"},
		}
		json.NewEncoder(w).Encode(graphNodesResponse{Nodes: resources})
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	pods, err := api.ListPods("")
	if err != nil {
		t.Fatalf("ListPods error: %v", err)
	}
	if len(pods) != 2 {
		t.Fatalf("expected 2 pods, got %d", len(pods))
	}
	if pods[0].Name != "nginx" {
		t.Errorf("expected first pod name 'nginx', got '%s'", pods[0].Name)
	}
}

func TestListPodsWithNamespace(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		json.NewEncoder(w).Encode(graphNodesResponse{})
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	_, err := api.ListPods("kube-system")
	if err != nil {
		t.Fatalf("ListPods error: %v", err)
	}
	expected := "/k8s/resources/graph/nodes?kind=Pod&namespace=kube-system"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestListNodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resources := []K8sResource{
			{UID: "node-1", Name: "worker-1", Kind: "Node", Status: "Ready"},
		}
		json.NewEncoder(w).Encode(graphNodesResponse{Nodes: resources})
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	nodes, err := api.ListNodes()
	if err != nil {
		t.Fatalf("ListNodes error: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Name != "worker-1" {
		t.Errorf("expected node name 'worker-1', got '%s'", nodes[0].Name)
	}
}

func TestListDeployments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resources := []K8sResource{
			{UID: "dep-1", Name: "api-server", Kind: "Deployment", Namespace: "default", Status: "Available"},
		}
		json.NewEncoder(w).Encode(graphNodesResponse{Nodes: resources})
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	deps, err := api.ListDeployments("default")
	if err != nil {
		t.Fatalf("ListDeployments error: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("expected 1 deployment, got %d", len(deps))
	}
	if deps[0].Name != "api-server" {
		t.Errorf("expected deployment name 'api-server', got '%s'", deps[0].Name)
	}
}

func TestListStatefulSets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resources := []K8sResource{
			{UID: "sts-1", Name: "kafka", Kind: "StatefulSet", Namespace: "default", Status: "Running"},
		}
		json.NewEncoder(w).Encode(graphNodesResponse{Nodes: resources})
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	sts, err := api.ListStatefulSets("")
	if err != nil {
		t.Fatalf("ListStatefulSets error: %v", err)
	}
	if len(sts) != 1 {
		t.Fatalf("expected 1 statefulset, got %d", len(sts))
	}
}

func TestListDaemonSets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resources := []K8sResource{
			{UID: "ds-1", Name: "fluentd", Kind: "DaemonSet", Namespace: "kube-system", Status: "Running"},
		}
		json.NewEncoder(w).Encode(graphNodesResponse{Nodes: resources})
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	ds, err := api.ListDaemonSets("kube-system")
	if err != nil {
		t.Fatalf("ListDaemonSets error: %v", err)
	}
	if len(ds) != 1 {
		t.Fatalf("expected 1 daemonset, got %d", len(ds))
	}
}

func TestListServices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resources := []K8sResource{
			{UID: "svc-1", Name: "nginx-svc", Kind: "Service", Namespace: "default", Status: "Active"},
		}
		json.NewEncoder(w).Encode(graphNodesResponse{Nodes: resources})
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	svcs, err := api.ListServices("")
	if err != nil {
		t.Fatalf("ListServices error: %v", err)
	}
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
}

func TestListResourcesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	_, err := api.ListPods("")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestGetPod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resources := []K8sResource{
			{UID: "uid-1", Name: "nginx", Kind: "Pod", Namespace: "default", Status: "Running"},
			{UID: "uid-2", Name: "redis", Kind: "Pod", Namespace: "default", Status: "Pending"},
		}
		json.NewEncoder(w).Encode(graphNodesResponse{Nodes: resources})
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	pod, err := api.GetPod("nginx", "default")
	if err != nil {
		t.Fatalf("GetPod error: %v", err)
	}
	if pod.Name != "nginx" {
		t.Errorf("expected name 'nginx', got '%s'", pod.Name)
	}
	if pod.Status != "Running" {
		t.Errorf("expected status 'Running', got '%s'", pod.Status)
	}
}

func TestGetPodNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(graphNodesResponse{})
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	_, err := api.GetPod("nonexistent", "default")
	if err == nil {
		t.Fatal("expected error for missing pod, got nil")
	}
}

func TestGetNode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resources := []K8sResource{
			{UID: "node-1", Name: "worker-1", Kind: "Node", Status: "Ready"},
		}
		json.NewEncoder(w).Encode(graphNodesResponse{Nodes: resources})
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	node, err := api.GetNode("worker-1")
	if err != nil {
		t.Fatalf("GetNode error: %v", err)
	}
	if node.Name != "worker-1" {
		t.Errorf("expected name 'worker-1', got '%s'", node.Name)
	}
	if node.Status != "Ready" {
		t.Errorf("expected status 'Ready', got '%s'", node.Status)
	}
}

func TestGetNodeNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(graphNodesResponse{})
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	_, err := api.GetNode("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing node, got nil")
	}
}

func TestTopoGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"nodes":[{"id":"uid-1","label":"nginx","kind":"Pod","namespace":"default"}],"edges":[{"source":"uid-1","target":"uid-2","label":"BelongsTo"}],"totalCount":2}`))
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	graph, err := api.TopoGet("uid-1", 2)
	if err != nil {
		t.Fatalf("TopoGet error: %v", err)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(graph.Nodes))
	}
	if graph.Nodes[0].Label != "nginx" {
		t.Errorf("expected label 'nginx', got '%s'", graph.Nodes[0].Label)
	}
	if len(graph.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(graph.Edges))
	}
	if graph.Edges[0].Source != "uid-1" {
		t.Errorf("expected source 'uid-1', got '%s'", graph.Edges[0].Source)
	}
}

func TestTopoGetURL(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"nodes":[],"edges":[],"totalCount":0}`))
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	_, err := api.TopoGet("uid-abc", 5)
	if err != nil {
		t.Fatalf("TopoGet error: %v", err)
	}
	expected := "/k8s/resources/graph/search?resourceId=uid-abc&depth=5"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestTopoSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"nodes":[{"id":"uid-1","label":"nginx","kind":"Pod","namespace":"default"}],"edges":[],"totalCount":1}`))
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	graph, err := api.TopoSearch("Pod", "nginx")
	if err != nil {
		t.Fatalf("TopoSearch error: %v", err)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(graph.Nodes))
	}
	if graph.Nodes[0].Label != "nginx" {
		t.Errorf("expected label 'nginx', got '%s'", graph.Nodes[0].Label)
	}
}

func TestTopoSearchNoName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"nodes":[{"id":"uid-1","label":"nginx","kind":"Pod","namespace":"default"},{"id":"uid-2","label":"redis","kind":"Pod","namespace":"default"}],"edges":[],"totalCount":2}`))
	}))
	defer srv.Close()

	api := NewResourcesAPI(client.NewClient(srv.URL, "test-token", "test"))
	graph, err := api.TopoSearch("Pod", "")
	if err != nil {
		t.Fatalf("TopoSearch error: %v", err)
	}
	if len(graph.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(graph.Nodes))
	}
}
