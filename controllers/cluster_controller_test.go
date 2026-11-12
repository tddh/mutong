package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitee.com/tddh/mutong/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"k8s.io/client-go/rest"
)

func TestClusterListClusters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Nil config -> empty list, not an error.
	r := gin.New()
	NewClusterController(zap.NewNop(), nil, nil).RegisterRoutes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("nil cfg: got %d", w.Code)
	}
	var resp struct {
		Clusters []interface{} `json:"clusters"`
		Count    int           `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Count != 0 || resp.Clusters == nil || len(resp.Clusters) != 0 {
		t.Fatalf("expected empty non-null cluster list, got %+v", resp)
	}

	// Populated config.
	client, restCfg := newFakeK8sAPI(t)
	clusterCfg := &config.Cluster{
		Region: "cn-east", Enable: true,
		K8sClusterClient: map[string]*config.K8sClusterClient{
			"prod": {Name: "prod-ctx", RootKubeClientSet: client, RootRestConfig: restCfg, Status: ""},
		},
	}
	r = gin.New()
	NewClusterController(zap.NewNop(), clusterCfg, nil).RegisterRoutes(r)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d", w.Code)
	}
	var list struct {
		Count    int `json:"count"`
		Clusters []struct {
			Name     string `json:"name"`
			Region   string `json:"region"`
			Context  string `json:"context"`
			Endpoint string `json:"endpoint"`
			Status   string `json:"status"`
			Enable   bool   `json:"enable"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Count != 1 || len(list.Clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %+v", list)
	}
	c := list.Clusters[0]
	if c.Name != "prod" || c.Region != "cn-east" || c.Context != "prod-ctx" || !c.Enable {
		t.Fatalf("unexpected cluster info: %+v", c)
	}
	if c.Status != "unknown" {
		t.Fatalf("empty status must default to unknown, got %q", c.Status)
	}
	if c.Endpoint != restCfg.Host {
		t.Fatalf("endpoint = %q, want %q", c.Endpoint, restCfg.Host)
	}
}

func TestClusterCheckHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client, restCfg := newFakeK8sAPI(t)
	clusterCfg := &config.Cluster{
		K8sClusterClient: map[string]*config.K8sClusterClient{
			"prod": {Name: "prod", RootKubeClientSet: client, RootRestConfig: restCfg},
		},
	}
	r := gin.New()
	NewClusterController(zap.NewNop(), clusterCfg, nil).RegisterRoutes(r)

	// Unknown cluster.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/ghost/health", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown cluster: got %d", w.Code)
	}

	// Healthy API.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/prod/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("health: got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "healthy" || resp["name"] != "prod" {
		t.Fatalf("unexpected health response: %v", resp)
	}
	if clusterCfg.K8sClusterClient["prod"].Status != "healthy" {
		t.Fatal("health check must update the cached cluster status")
	}
	if clusterCfg.K8sClusterClient["prod"].LastHealthCheck.IsZero() {
		t.Fatal("LastHealthCheck must be recorded")
	}

	// Failing API marks the cluster unhealthy but still answers 200.
	clusterCfg.K8sClusterClient["prod"].RootKubeClientSet = newFailingK8sAPI(t)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/prod/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("unhealthy probe: got %d", w.Code)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "unhealthy" {
		t.Fatalf("expected unhealthy, got %v", resp)
	}
	if clusterCfg.K8sClusterClient["prod"].Status != "unhealthy" {
		t.Fatal("cached status must flip to unhealthy")
	}
}

func TestClusterGetStats(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client, restCfg := newFakeK8sAPI(t)
	clusterCfg := &config.Cluster{
		K8sClusterClient: map[string]*config.K8sClusterClient{
			"prod": {Name: "prod", RootKubeClientSet: client, RootRestConfig: &rest.Config{Host: restCfg.Host}},
		},
	}
	r := gin.New()
	NewClusterController(zap.NewNop(), clusterCfg, nil).RegisterRoutes(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/ghost/stats", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown cluster: got %d", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/prod/stats", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("stats: got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Cluster string         `json:"cluster"`
		Stats   map[string]int `json:"stats"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"pods": 1, "nodes": 1, "services": 2, "deployments": 1}
	for k, v := range want {
		if resp.Stats[k] != v {
			t.Errorf("stats[%s] = %d, want %d (full: %v)", k, resp.Stats[k], v, resp.Stats)
		}
	}
}
