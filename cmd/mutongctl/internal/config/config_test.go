package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_NoFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "http://localhost:8888" {
		t.Errorf("default server = %q, want http://localhost:8888", cfg.Server)
	}
}

func TestLoad_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
contexts:
  prod:
    server: https://mutong.example.com
    namespace: default
current-context: prod
`
	os.WriteFile(path, []byte(content), 0o600)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "https://mutong.example.com" {
		t.Errorf("server = %q, want https://mutong.example.com", cfg.Server)
	}
	if cfg.Namespace != "default" {
		t.Errorf("namespace = %q, want default", cfg.Namespace)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	os.Setenv("MUTONG_SERVER", "https://env.example.com")
	defer os.Unsetenv("MUTONG_SERVER")

	cfg, err := Load("/nonexistent/path")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "https://env.example.com" {
		t.Errorf("server from env = %q", cfg.Server)
	}
}
