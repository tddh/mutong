package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
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

func TestLoadResourceProfilesFromDir(t *testing.T) {
	dir := t.TempDir()
	profilesDir := filepath.Join(dir, "configs", "profiles")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	podYAML := []byte(`kind: Pod
metrics:
  - name: cpu_usage_cores
    promql: 'test_promql{pod="{{name}}"}'
    unit: "cores"
topology:
  - relation: "RunsOn"
    direction: "outbound"
    label: "node"
evidence:
  - source: "k8s_events"
    level: "Warning"
    query: 'test_query'
    limit: 10
`)
	if err := os.WriteFile(filepath.Join(profilesDir, "pod.yml"), podYAML, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{}
	cfg.Logger = zap.NewNop()
	cfg.ResourceProfileFiles = nil

	// Run from the temp dir so configs/profiles/ is relative
	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	cfg.loadResourceProfiles()

	if cfg.ResourceProfiles == nil {
		t.Fatal("expected ResourceProfiles to be initialized")
	}
	profile, ok := cfg.ResourceProfiles["Pod"]
	if !ok {
		t.Fatal("expected Pod profile to be loaded")
	}
	if len(profile.Metrics) != 1 || profile.Metrics[0].Name != "cpu_usage_cores" {
		t.Fatalf("unexpected metrics: %+v", profile.Metrics)
	}
}

func TestLoadInspectionRulesFromDir(t *testing.T) {
	// This tests the file-scanning part of loadInspectionRules without needing a real engine.
	// We verify it correctly scans *.yml and filters non-yml and directories.
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "configs", "rules", "inspection")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a valid .yml rule
	ruleYAML := []byte(`name: "test_rule"
description: "A test rule"
query: "MATCH (n) RETURN n"
check:
  type: min_rows
  threshold: 1
severity: warning
suggestion: "fix it"
`)
	if err := os.WriteFile(filepath.Join(rulesDir, "test_rule.yml"), ruleYAML, 0o600); err != nil {
		t.Fatal(err)
	}

	// Create a non-.yml file that should be ignored
	if err := os.WriteFile(filepath.Join(rulesDir, "README.md"), []byte("docs"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create a subdir that should be ignored
	if err := os.MkdirAll(filepath.Join(rulesDir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{}
	cfg.Logger = zap.NewNop()
	cfg.Inspection.Enabled = true

	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	// loadInspectionRules needs an engine; since we can't easily mock the full
	// inspection engine, we test the file discovery logic indirectly via the
	// fact that it doesn't panic and correctly scans the directory.
	// The actual rule loading is validated by the YAMLEngine tests.
	files := cfg.scanRuleFiles()
	if len(files) != 1 {
		t.Fatalf("expected 1 rule file, got %d: %v", len(files), files)
	}
	if !strings.HasSuffix(files[0], "test_rule.yml") {
		t.Fatalf("expected test_rule.yml, got %s", files[0])
	}
}

func TestLoadInspectionRulesDirPathSkipped(t *testing.T) {
	cfg := &Config{}
	// ruleFiles 包含一个目录路径 → 目录应该被过滤掉
	cfg.Inspection.RuleFiles = []string{"configs/rules/inspection"}

	oldWd, _ := os.Getwd()
	os.Chdir(t.TempDir())
	defer os.Chdir(oldWd)

	files := cfg.scanRuleFiles()
	// 没有创建任何 .yml 文件，应该返回空
	if len(files) != 0 {
		t.Fatalf("expected 0 files when only directory specified, got %d: %v", len(files), files)
	}
}
