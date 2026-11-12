package controllers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/tddh/mutong/config"
	exModel "gitee.com/tddh/mutong/models/executor"
	executorpkg "gitee.com/tddh/mutong/services/executor"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func jsonBody(s string) io.Reader { return strings.NewReader(s) }

func newExecutorTestController(t *testing.T, enabled bool) (*ExecutorController, *executorpkg.K8sExecutor) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Executor.Enabled = enabled
	exec := executorpkg.NewK8sExecutor(zap.NewNop(), nil, cfg)
	t.Cleanup(exec.Stop)
	return NewExecutorController(zap.NewNop(), exec, cfg, nil), exec
}

func executorRouter(ctrl *ExecutorController, role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if role != "" {
		r.Use(func(c *gin.Context) { c.Set("role", role) })
	}
	ctrl.RegisterRoutes(r)
	return r
}

func TestExecutorRequireAdminKey(t *testing.T) {
	ctrl, _ := newExecutorTestController(t, false)

	// Without MUTONG_ADMIN_KEY the admin role is required.
	t.Setenv("MUTONG_ADMIN_KEY", "")
	r := executorRouter(ctrl, "operator")
	w := doJSON(r, http.MethodPost, "/api/v1/executor/toggle", `{"autoMode":true}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("operator without admin key: got %d", w.Code)
	}
	r = executorRouter(ctrl, "admin")
	w = doJSON(r, http.MethodPost, "/api/v1/executor/toggle", `{"autoMode":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("admin role: got %d: %s", w.Code, w.Body.String())
	}

	// With a configured key, the role alone is not enough.
	t.Setenv("MUTONG_ADMIN_KEY", "s3cret-key")
	r = executorRouter(ctrl, "admin")
	w = doJSON(r, http.MethodPost, "/api/v1/executor/toggle", `{"autoMode":true}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("admin role without key header: got %d", w.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/executor/toggle", nil)
	req.Body = http.NoBody
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("missing key: got %d", w.Code)
	}
	// Wrong key.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/executor/toggle", jsonBody(`{"autoMode":false}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", "wrong")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("wrong key: got %d", w.Code)
	}
	// Correct key.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/executor/toggle", jsonBody(`{"autoMode":false}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", "s3cret-key")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("correct key: got %d: %s", w.Code, w.Body.String())
	}
}

func TestExecutorToggleAndStatus(t *testing.T) {
	ctrl, exec := newExecutorTestController(t, true)
	t.Setenv("MUTONG_ADMIN_KEY", "")
	r := executorRouter(ctrl, "admin")

	// Invalid payload.
	if w := doJSON(r, http.MethodPost, "/api/v1/executor/toggle", `{"autoMode":`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad toggle payload: got %d", w.Code)
	}

	if exec.IsAutoMode() {
		t.Fatal("auto mode must start disabled")
	}
	w := doJSON(r, http.MethodPost, "/api/v1/executor/toggle", `{"autoMode":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("toggle: got %d", w.Code)
	}
	if !exec.IsAutoMode() {
		t.Fatal("toggle must enable auto mode on the executor")
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/executor/status", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	var status struct {
		Enabled      bool   `json:"enabled"`
		AutoMode     bool   `json:"autoMode"`
		AuditLogType string `json:"auditLogType"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Enabled || !status.AutoMode || status.AuditLogType != "memory" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestExecutorAuditEndpoints(t *testing.T) {
	ctrl, exec := newExecutorTestController(t, true)
	t.Setenv("MUTONG_ADMIN_KEY", "")
	r := executorRouter(ctrl, "admin")

	// Empty audit log.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/executor/audit?fingerprint=fp&action=restart_pod&namespace=ns&autoExecuted=true", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("audit: got %d", w.Code)
	}
	var logs []exModel.AuditLog
	if err := json.Unmarshal(w.Body.Bytes(), &logs); err != nil {
		t.Fatal(err)
	}
	if len(logs) != 0 {
		t.Fatalf("expected empty audit log, got %d", len(logs))
	}

	// Approve: invalid ids are rejected before any lookup.
	for _, id := range []string{"audit-abc", "not-a-number"} {
		w := doJSON(r, http.MethodPost, "/api/v1/executor/audit/"+id+"/approve", `{}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("approve %s: expected 400, got %d", id, w.Code)
		}
	}
	// Unknown audit id -> 404.
	w = doJSON(r, http.MethodPost, "/api/v1/executor/audit/audit-999/approve", `{}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("approve missing: expected 404, got %d", w.Code)
	}

	// Execute: invalid plan payload -> 400 (admin gate passes first).
	w = doJSON(r, http.MethodPost, "/api/v1/executor/execute", `{"action":`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("execute bad payload: got %d", w.Code)
	}
	// Non-admin cannot even reach payload validation.
	opRouter := executorRouter(ctrl, "operator")
	w = doJSON(opRouter, http.MethodPost, "/api/v1/executor/execute", `{}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("execute as operator: got %d", w.Code)
	}
	_ = exec
}

func TestExecutorDiagnoseAndExecuteGates(t *testing.T) {
	t.Setenv("MUTONG_ADMIN_KEY", "")

	// Disabled executor -> 403.
	ctrl, _ := newExecutorTestController(t, false)
	r := executorRouter(ctrl, "admin")
	w := doJSON(r, http.MethodPost, "/api/v1/executor/diagnose-and-execute", `{"fingerprint":"fp"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("disabled executor: got %d", w.Code)
	}

	// Enabled but no diagnosis engine -> 500.
	ctrl, _ = newExecutorTestController(t, true)
	r = executorRouter(ctrl, "admin")
	w = doJSON(r, http.MethodPost, "/api/v1/executor/diagnose-and-execute", `{"fingerprint":"fp"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("missing engine: got %d: %s", w.Code, w.Body.String())
	}
	// Invalid payload -> 400.
	w = doJSON(r, http.MethodPost, "/api/v1/executor/diagnose-and-execute", `{bad`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad payload: got %d", w.Code)
	}
}
