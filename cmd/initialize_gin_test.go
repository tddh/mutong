package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitee.com/tddh/mutong/config"
	"gitee.com/tddh/mutong/controllers"
	"gitee.com/tddh/mutong/models"
	alert_models "gitee.com/tddh/mutong/models/alert"
	diag_model "gitee.com/tddh/mutong/models/diagnosis"
	oauth2model "gitee.com/tddh/mutong/models/oauth2"
	authsvc "gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
	nebula "github.com/vesoft-inc/nebula-go/v3"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// fakeAlertProcessor is a minimal AlertProcessor so initializeGin takes the
// diagnosis-wiring branch without a real alert pipeline.
type fakeAlertProcessor struct{}

func (fakeAlertProcessor) Process(context.Context, *alert_models.WebhookPayload) ([]*alert_models.ProcessedAlert, error) {
	return nil, nil
}
func (fakeAlertProcessor) ProcessExternal(context.Context, *alert_models.Alert, *alert_models.AlertSource) ([]*alert_models.ProcessedAlert, error) {
	return nil, nil
}
func (fakeAlertProcessor) GetActiveAlerts(context.Context, map[string]string) ([]*alert_models.ProcessedAlert, error) {
	return nil, nil
}
func (fakeAlertProcessor) GetAlertByFingerprint(context.Context, string) (*alert_models.ProcessedAlert, error) {
	return nil, errors.New("not found")
}

// fakeGraphDB satisfies interfaces.GraphDB without a Nebula connection.
type fakeGraphDB struct{}

func (fakeGraphDB) Execute(string) (*nebula.ResultSet, error) {
	return nil, errors.New("graph db not configured")
}
func (fakeGraphDB) ExecuteAndCheck(string) (*nebula.ResultSet, error) {
	return nil, errors.New("graph db not configured")
}

func newInitializeGinConfig(t *testing.T) *config.Config {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.APIToken{}, &diag_model.StatsModel{},
		&oauth2model.OAuth2Client{}, &oauth2model.DeviceCode{}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DB: db, Logger: zap.NewNop()}
	cfg.Auth.Mode = "local"
	cfg.Auth.RBACMode = authsvc.RBACEnforce
	cfg.Server.RateLimitPerSec = 1000
	return cfg
}

// FUNC-01 regression: diagEngine/chatManager used to be shadowed by := inside
// the alertProcessor branch, so initializeGin returned nil engines and the
// shutdown path leaked goroutines. With a non-nil alertProcessor the returned
// engines must be the very instances that were wired.
func TestInitializeGinReturnsWiredDiagnosisEngines(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("MUTONG_ISSUER_URL", "https://mutong.example.com")
	t.Setenv("MUTONG_INIT_ADMIN_PASSWORD", "")
	t.Setenv("MUTONG_CONFIG_DIR", "..")
	// NOTE: an explicit origin is used here to exercise the CORS-enabled path;
	// the default (unset) path is covered by TestInitializeGinDefaultCORS.
	t.Setenv("MUTONG_ALLOWED_ORIGINS", "https://mutong.example.com")
	prevRBAC := authsvc.GetRBACMode()
	t.Cleanup(func() { authsvc.SetRBACMode(prevRBAC) })

	cfg := newInitializeGinConfig(t)
	sessions := authsvc.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	ctrls := controllers.NewControllers(nil, nil, nil)

	engine, k8sExec, diagEngine, chatManager, autoDiag, termMgr := initializeGin(
		context.Background(), ctrls, nil, zap.NewNop(),
		fakeAlertProcessor{}, nil, fakeGraphDB{}, nil, cfg, nil, nil, sessions, nil,
	)
	if engine == nil {
		t.Fatal("initializeGin returned nil engine")
	}
	if diagEngine == nil {
		t.Fatal("FUNC-01: diagEngine must be non-nil when alertProcessor is wired (variable shadowing regression)")
	}
	if chatManager == nil {
		t.Fatal("FUNC-01: chatManager must be non-nil when alertProcessor is wired (variable shadowing regression)")
	}
	if termMgr == nil {
		t.Fatal("terminal manager must be returned for cleanup")
	}
	// No K8s client and no concrete *alert.AlertService: executor-dependent
	// wiring must stay disabled instead of half-initialized.
	if k8sExec != nil || autoDiag != nil {
		t.Fatalf("executor wiring must stay nil without a K8s client, got k8sExec=%v autoDiag=%v", k8sExec, autoDiag)
	}
	t.Cleanup(func() {
		// NOTE: diagEngine.Stop() is deliberately not called: NewEngine never
		// initializes stopCh, so Stop() panics with "close of nil channel"
		// (reported as a bug; production shutdown hits the same path).
		chatManager.Stop()
		termMgr.StopCleanup()
	})

	// The returned engine must serve the unauthenticated liveness probe...
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Fatalf("/healthz = %d %q, want 200 ok", w.Code, w.Body.String())
	}
	// ...while protected API routes still demand authentication.
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("protected route without credentials = %d, want 401", w.Code)
	}

	// Device flow routes are registered and the seeded CLI client works
	// end-to-end through the real engine.
	form := url.Values{"client_id": {"mutongctl"}, "scope": {"read"}}
	req := httptest.NewRequest(http.MethodPost, "/oauth2/device/auth", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("seeded mutongctl device auth = %d, want 200: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "https://mutong.example.com/device") {
		t.Fatalf("verification_uri must use MUTONG_ISSUER_URL: %s", w.Body.String())
	}
}

// Without an alert processor the diagnosis branch must stay untouched: no
// engine, no chat manager, and no goroutine-backed managers to clean up.
func TestInitializeGinWithoutAlertProcessorSkipsDiagnosis(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("MUTONG_ISSUER_URL", "https://mutong.example.com")
	t.Setenv("MUTONG_INIT_ADMIN_PASSWORD", "")
	t.Setenv("MUTONG_CONFIG_DIR", "..")
	t.Setenv("MUTONG_ALLOWED_ORIGINS", "https://mutong.example.com")
	prevRBAC := authsvc.GetRBACMode()
	t.Cleanup(func() { authsvc.SetRBACMode(prevRBAC) })

	cfg := newInitializeGinConfig(t)
	sessions := authsvc.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	ctrls := controllers.NewControllers(nil, nil, nil)

	engine, k8sExec, diagEngine, chatManager, autoDiag, termMgr := initializeGin(
		context.Background(), ctrls, nil, zap.NewNop(),
		nil, nil, fakeGraphDB{}, nil, cfg, nil, nil, sessions, nil,
	)
	if engine == nil || termMgr == nil {
		t.Fatal("engine and terminal manager must always be returned")
	}
	t.Cleanup(func() { termMgr.StopCleanup() })
	if diagEngine != nil || chatManager != nil || k8sExec != nil || autoDiag != nil {
		t.Fatalf("diagnosis wiring must stay nil without alertProcessor: diag=%v chat=%v exec=%v auto=%v",
			diagEngine, chatManager, k8sExec, autoDiag)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200", w.Code)
	}
}

// 默认配置（未设 MUTONG_ALLOWED_ORIGINS）必须能启动：
// cors.New 对空 AllowOrigins 会 panic，修复后改为不挂 CORS 中间件。
func TestInitializeGinDefaultCORS(t *testing.T) {
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
	if engine == nil {
		t.Fatal("initializeGin must succeed without MUTONG_ALLOWED_ORIGINS")
	}
	t.Cleanup(func() { termMgr.StopCleanup() })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://evil.example")
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("cross-origin must be denied by default, got ACAO=%q", got)
	}
}

// CORS: cross-origin access is deny-by-default and a wildcard must never be
// honoured together with credentials.
func TestAllowedOrigins(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want []string
	}{
		{"unset denies cross-origin", "", []string{}},
		{"wildcard refused", "*", []string{}},
		{"wildcard among entries refused", "https://a.example,*", []string{"https://a.example"}},
		{"explicit origins trimmed", " https://a.example , https://b.example ,", []string{"https://a.example", "https://b.example"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MUTONG_ALLOWED_ORIGINS", tc.env)
			got := allowedOrigins()
			if len(got) != len(tc.want) {
				t.Fatalf("allowedOrigins() = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("allowedOrigins()[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// Static serving must only expose build output, keep pre-login pages public
// and redirect anonymous browsers away from protected pages.
func TestRegisterStaticRoutesAuthGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	t.Chdir(dir)
	for name, content := range map[string]string{
		"view/dist/index.html":     "<html>index</html>",
		"view/dist/login.html":     "<html>login</html>",
		"view/dist/dashboard.html": "<html>secret</html>",
		"view/dist/app.js":         "console.log(1)",
	} {
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		path     string
		authed   bool
		wantCode int
	}{
		{"/view/login.html", false, http.StatusOK},
		{"/view/app.js", false, http.StatusOK},
		{"/view/dashboard.html", false, http.StatusFound},
		{"/view/dashboard.html", true, http.StatusOK},
	}
	for _, tc := range tests {
		engine := gin.New()
		if tc.authed {
			engine.Use(func(c *gin.Context) { c.Set("user_id", uint(1)) })
		}
		registerStaticRoutes(engine, zap.NewNop())
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if w.Code != tc.wantCode {
			t.Errorf("%s authed=%v: got %d, want %d", tc.path, tc.authed, w.Code, tc.wantCode)
		}
		if tc.path == "/view/dashboard.html" && !tc.authed && w.Code == http.StatusFound {
			if loc := w.Header().Get("Location"); loc != "/view/login.html" {
				t.Errorf("anonymous redirect location = %q, want /view/login.html", loc)
			}
		}
	}
}
