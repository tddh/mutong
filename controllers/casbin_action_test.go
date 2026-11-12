package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestActionForRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		method string
		path   string
		want   string
	}{
		// Destructive surfaces are "execute" even on GET.
		{http.MethodGet, "/api/v1/terminal/ws", "execute"},
		{http.MethodGet, "/api/v1/executor/status", "execute"},
		{http.MethodPost, "/api/v1/executor/execute", "execute"},
		{http.MethodGet, "/api/v1/diagnosis/mcp/tool/restart_pod", "execute"},
		// Ordinary reads.
		{http.MethodGet, "/api/v1/alerts", "read"},
		{http.MethodHead, "/api/v1/alerts", "read"},
		{http.MethodOptions, "/api/v1/alerts", "read"},
		// Ordinary writes.
		{http.MethodPost, "/api/v1/alerts/webhook", "write"},
		{http.MethodPut, "/api/v1/inspection/rules/1", "write"},
		{http.MethodDelete, "/api/v1/diagnosis/chat/sessions/x", "write"},
	}
	for _, tc := range tests {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(tc.method, tc.path, nil)
		if got := actionForRequest(c); got != tc.want {
			t.Errorf("%s %s: action = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestRequireRole(t *testing.T) {
	prev := auth.GetRBACMode()
	t.Cleanup(func() { auth.SetRBACMode(prev) })
	gin.SetMode(gin.TestMode)

	build := func(role string) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			if role != "" {
				c.Set("role", role)
			}
		})
		r.Use(RequireRole("admin"))
		r.POST("/op", func(c *gin.Context) { c.String(http.StatusOK, "done") })
		return r
	}

	auth.SetRBACMode(auth.RBACEnforce)
	for _, tc := range []struct {
		role string
		want int
	}{
		{"admin", http.StatusOK},
		{"operator", http.StatusForbidden},
		{"", http.StatusForbidden},
	} {
		w := httptest.NewRecorder()
		build(tc.role).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/op", nil))
		if w.Code != tc.want {
			t.Errorf("role %q: got %d, want %d", tc.role, w.Code, tc.want)
		}
	}

	// Off mode bypasses the role gate entirely.
	auth.SetRBACMode(auth.RBACOff)
	w := httptest.NewRecorder()
	build("viewer").ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/op", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("RBAC off must bypass RequireRole, got %d", w.Code)
	}
}

func TestCasbinMiddlewareModeBehavior(t *testing.T) {
	prev := auth.GetRBACMode()
	t.Cleanup(func() { auth.SetRBACMode(prev) })
	gin.SetMode(gin.TestMode)

	build := func(withIdentity bool) *gin.Engine {
		r := gin.New()
		if withIdentity {
			r.Use(func(c *gin.Context) { c.Set("user_uuid", "uuid-test") })
		}
		r.Use(CasbinMiddleware(zap.NewNop()))
		r.GET("/api/v1/things", func(c *gin.Context) { c.String(http.StatusOK, "data") })
		return r
	}
	do := func(r *gin.Engine) int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/things", nil))
		return w.Code
	}

	// Off: everything passes.
	auth.SetRBACMode(auth.RBACOff)
	if code := do(build(false)); code != http.StatusOK {
		t.Fatalf("off mode: got %d, want 200", code)
	}

	// Shadow: a denied policy decision (no enforcer configured here) is logged
	// but not enforced.
	auth.SetRBACMode(auth.RBACShadow)
	if code := do(build(true)); code != http.StatusOK {
		t.Fatalf("shadow mode must not block, got %d", code)
	}

	// Enforce: the same denied decision now blocks.
	auth.SetRBACMode(auth.RBACEnforce)
	if code := do(build(true)); code != http.StatusForbidden {
		t.Fatalf("enforce mode with denied policy: got %d, want 403", code)
	}

	// Missing identity is always denied, regardless of mode.
	if code := do(build(false)); code != http.StatusForbidden {
		t.Fatalf("enforce mode without identity: got %d, want 403", code)
	}
	auth.SetRBACMode(auth.RBACShadow)
	if code := do(build(false)); code != http.StatusForbidden {
		t.Fatalf("shadow mode must still deny missing identity, got %d", code)
	}
}
