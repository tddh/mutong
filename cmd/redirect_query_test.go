package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gitee.com/tddh/mutong/controllers"
	authsvc "gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 旧路径 308 重定向必须保留 query string，否则 graph/search 的必填参数
// resourceId 丢失导致 400。
func TestLegacyRedirectPreservesQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("MUTONG_ISSUER_URL", "https://mutong.example.com")
	t.Setenv("MUTONG_INIT_ADMIN_PASSWORD", "")
	t.Setenv("MUTONG_CONFIG_DIR", "..")
	t.Setenv("MUTONG_ALLOWED_ORIGINS", "")
	prevRBAC := authsvc.GetRBACMode()
	t.Cleanup(func() { authsvc.SetRBACMode(prevRBAC) })

	cfg := newInitializeGinConfig(t)
	sessions := authsvc.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	ctrls := controllers.NewControllers(nil, nil, nil)

	engine, _, _, _, _, termMgr := initializeGin(
		context.Background(), ctrls, nil, zap.NewNop(),
		nil, nil, fakeGraphDB{}, nil, cfg, nil, nil, sessions, nil,
	)
	t.Cleanup(func() { termMgr.StopCleanup() })

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/k8s/resources/graph/search?resourceId=test-uid&depth=2", nil))
	if w.Code != http.StatusPermanentRedirect {
		t.Fatalf("legacy path = %d, want 308", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/api/v1/k8s/resources/graph/search") {
		t.Fatalf("redirect target = %q, want /api/v1 prefix", loc)
	}
	if !strings.Contains(loc, "resourceId=test-uid") || !strings.Contains(loc, "depth=2") {
		t.Fatalf("redirect lost query params: %q", loc)
	}
}
