package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestTerminalResourceEndpointsWithoutClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewTerminalController(zap.NewNop(), nil, nil).RegisterRoutes(r)

	for _, path := range []string{
		"/api/v1/terminal/namespaces",
		"/api/v1/terminal/pods",
		"/api/v1/terminal/containers?pod=p1",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s without k8s client: got %d, want 503", path, w.Code)
		}
	}
}

func TestTerminalResourceEndpointsWithClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client, restCfg := newFakeK8sAPI(t)
	r := gin.New()
	ctrl := NewTerminalController(zap.NewNop(), client, restCfg)
	ctrl.RegisterRoutes(r)
	t.Cleanup(ctrl.Manager.StopCleanup)

	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}

	// Namespaces.
	w := get("/api/v1/terminal/namespaces")
	if w.Code != http.StatusOK {
		t.Fatalf("namespaces: got %d: %s", w.Code, w.Body.String())
	}
	var nsResp struct {
		Namespaces []string `json:"namespaces"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &nsResp); err != nil {
		t.Fatal(err)
	}
	if len(nsResp.Namespaces) != 2 || nsResp.Namespaces[0] != "default" || nsResp.Namespaces[1] != "team-a" {
		t.Fatalf("unexpected namespaces: %v", nsResp.Namespaces)
	}

	// Pods default to the default namespace.
	w = get("/api/v1/terminal/pods")
	if w.Code != http.StatusOK {
		t.Fatalf("pods: got %d", w.Code)
	}
	var podResp struct {
		Namespace string   `json:"namespace"`
		Pods      []string `json:"pods"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &podResp)
	if podResp.Namespace != "default" || len(podResp.Pods) != 1 || podResp.Pods[0] != "p1" {
		t.Fatalf("unexpected pods: %+v", podResp)
	}

	// Containers without pod param lists pods instead.
	w = get("/api/v1/terminal/containers")
	if w.Code != http.StatusOK {
		t.Fatalf("containers without pod: got %d", w.Code)
	}
	var noPod struct {
		Pods       []string `json:"pods"`
		Containers []string `json:"containers"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &noPod)
	if len(noPod.Pods) != 1 || len(noPod.Containers) != 0 {
		t.Fatalf("unexpected containers fallback: %+v", noPod)
	}

	// Containers of a specific pod include init containers separately.
	w = get("/api/v1/terminal/containers?pod=p1")
	if w.Code != http.StatusOK {
		t.Fatalf("containers: got %d: %s", w.Code, w.Body.String())
	}
	var cResp struct {
		Pod            string   `json:"pod"`
		Containers     []string `json:"containers"`
		InitContainers []string `json:"initContainers"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &cResp)
	if cResp.Pod != "p1" || len(cResp.Containers) != 2 || cResp.Containers[0] != "app" || cResp.Containers[1] != "sidecar" {
		t.Fatalf("unexpected containers: %+v", cResp)
	}
	if len(cResp.InitContainers) != 1 || cResp.InitContainers[0] != "init" {
		t.Fatalf("unexpected init containers: %+v", cResp)
	}

	// Unknown pod -> 500 (K8s API error surfaced as failure).
	if w := get("/api/v1/terminal/containers?pod=ghost"); w.Code != http.StatusInternalServerError {
		t.Fatalf("unknown pod: got %d", w.Code)
	}
}

func TestTerminalResourceEndpointsAPIFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	ctrl := NewTerminalController(zap.NewNop(), newFailingK8sAPI(t), nil)
	ctrl.RegisterRoutes(r)
	t.Cleanup(ctrl.Manager.StopCleanup)

	for _, path := range []string{
		"/api/v1/terminal/namespaces",
		"/api/v1/terminal/pods",
		"/api/v1/terminal/containers",
		"/api/v1/terminal/containers?pod=p1",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusInternalServerError {
			t.Errorf("%s with failing API: got %d, want 500", path, w.Code)
		}
	}
}
