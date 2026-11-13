package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func writeIndex(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>index</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveViewDirEnvOverride(t *testing.T) {
	dir := t.TempDir()
	writeIndex(t, dir)
	t.Setenv("MUTONG_VIEW_DIR", dir)
	got, _ := resolveViewDir()
	if got != dir {
		t.Fatalf("env override must win, got %q want %q", got, dir)
	}
}

func TestResolveViewDirRejectsFile(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MUTONG_VIEW_DIR", file)
	t.Chdir(tmp)
	if got, _ := resolveViewDir(); got != "" {
		t.Fatalf("a regular file must not be accepted as view dir, got %q", got)
	}
}

func TestResolveViewDirFromCWD(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MUTONG_VIEW_DIR", "")
	want := filepath.Join(tmp, "view", "dist")
	writeIndex(t, want)
	t.Chdir(tmp)
	got, _ := resolveViewDir()
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRegisterStaticRoutesServesLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmp := t.TempDir()
	writeIndex(t, tmp)
	t.Setenv("MUTONG_VIEW_DIR", tmp)
	if err := os.WriteFile(filepath.Join(tmp, "login.html"), []byte("<html>login</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	registerStaticRoutes(engine, zap.NewNop())
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/view/login.html", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/view/login.html = %d, want 200", w.Code)
	}
}

func TestRegisterStaticRoutesNoBuildRegistersNothing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmp := t.TempDir()
	t.Setenv("MUTONG_VIEW_DIR", "")
	t.Chdir(tmp)
	engine := gin.New()
	registerStaticRoutes(engine, zap.NewNop())
	for _, r := range engine.Routes() {
		if strings.HasPrefix(r.Path, "/view/") {
			t.Fatalf("must not register /view routes without a build, got %s", r.Path)
		}
	}
}
