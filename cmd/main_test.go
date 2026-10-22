package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gitee.com/tddh/mutong/config"
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
