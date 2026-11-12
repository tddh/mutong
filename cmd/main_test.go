package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gitee.com/tddh/mutong/config"
	"gitee.com/tddh/mutong/controllers"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestResolveSessionSecretGeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session_secret")
	t.Setenv("MUTONG_SESSION_SECRET", "")
	t.Setenv("MUTONG_SESSION_SECRET_FILE", path)

	cfg := &config.Config{}
	logger := zap.NewNop()

	first, err := resolveSessionSecret(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 32 {
		t.Fatalf("generated secret too short: %d bytes", len(first))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("secret was not persisted: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("secret permissions = %o, want 600", perm)
	}

	second, err := resolveSessionSecret(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("second call must reuse the persisted secret")
	}
}

func TestResolveSessionSecretPrecedence(t *testing.T) {
	t.Setenv("MUTONG_SESSION_SECRET", "env-secret")
	cfg := &config.Config{}
	cfg.Auth.GlobalSecret = "config-secret"

	got, err := resolveSessionSecret(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "env-secret" {
		t.Fatalf("environment variable must take precedence, got %q", got)
	}
}

// FUNC-07: the container HEALTHCHECK hits /healthz, which must answer 200
// without credentials. Registering it before the auth chain is what makes
// that work, so the ordering itself is asserted here.
func TestHealthzBypassesAuthWhenRegisteredFirst(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerHealthz(engine)
	engine.Use(controllers.RequireAuthMiddleware())
	engine.GET("/api/v1/protected", func(c *gin.Context) { c.String(http.StatusOK, "secret") })

	probe := httptest.NewRecorder()
	engine.ServeHTTP(probe, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if probe.Code != http.StatusOK {
		t.Fatalf("/healthz status = %d, want 200", probe.Code)
	}
	if probe.Body.String() != "ok" {
		t.Fatalf("/healthz body = %q, want ok", probe.Body.String())
	}

	protected := httptest.NewRecorder()
	engine.ServeHTTP(protected, httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil))
	if protected.Code != http.StatusUnauthorized {
		t.Fatalf("protected route status = %d, want 401 (auth chain must still apply)", protected.Code)
	}
}

// FUNC-08: profiling and the gops agent are opt-in. A non-empty default for
// --scope-server-addr used to enable pyroscope on every start.
func TestDiagnosticsFlagsAreOptIn(t *testing.T) {
	for name, want := range map[string]string{
		"scope-server-addr": "",
		"gops":              "false",
	} {
		flag := rootCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("flag --%s is not registered", name)
		}
		if flag.DefValue != want {
			t.Fatalf("--%s default = %q, want %q", name, flag.DefValue, want)
		}
	}
}

func TestStartDiagnosticsDisabledIsNoop(t *testing.T) {
	restoreDiagnosticsFlags(t)
	scopeServerAddr, gopsEnabled = "", false

	startDiagnostics(zap.NewNop())
}

// FUNC-08: an unreachable/invalid profiler backend must degrade to a log line,
// never panic and abort startup.
func TestStartDiagnosticsDegradesOnProfilerError(t *testing.T) {
	restoreDiagnosticsFlags(t)
	scopeServerAddr, gopsEnabled = "://invalid-address", false

	startDiagnostics(zap.NewNop())
}

func restoreDiagnosticsFlags(t *testing.T) {
	t.Helper()
	addr, gops := scopeServerAddr, gopsEnabled
	t.Cleanup(func() { scopeServerAddr, gopsEnabled = addr, gops })
}
