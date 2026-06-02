package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuthModeDefaultsToLocal(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()
	if cfg.Auth.Mode != "local" {
		t.Fatalf("expected auth mode local, got %q", cfg.Auth.Mode)
	}
}

func TestZitadelConfigLoadsFromFile(t *testing.T) {
	yaml := []byte(`auth:
  mode: hybrid
  zitadel:
    issuer: https://id.example.com
    client_id: mutong-web
    client_secret: secret
    redirect_uri: http://localhost:8888/api/auth/oidc/callback
    scopes: [openid, profile, email]
`)
	path := filepath.Join(t.TempDir(), "config.auth.yaml")
	if err := os.WriteFile(path, yaml, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Mode != "hybrid" {
		t.Fatalf("expected hybrid, got %q", cfg.Auth.Mode)
	}
	if cfg.Auth.Zitadel.ClientID != "mutong-web" {
		t.Fatalf("unexpected client id %q", cfg.Auth.Zitadel.ClientID)
	}
}
