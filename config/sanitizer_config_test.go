package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSanitizerConfigYAMLKeys(t *testing.T) {
	src := `
enabled: true
high_pii_block_threshold: 0.7
banned_terms:
  - "internal.corp.net"
max_query_length: 300
prompt_injection_patterns:
  - "ignore previous"
rules:
  - name: email
    pattern: '@'
    enabled: true
`
	var c SanitizerConfig
	if err := yaml.Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}
	if !c.Enabled {
		t.Fatal("enabled not loaded")
	}
	if c.HighPIIBlockThreshold != 0.7 {
		t.Fatalf("high_pii_block_threshold not loaded: %v", c.HighPIIBlockThreshold)
	}
	if len(c.BannedTerms) != 1 || c.BannedTerms[0] != "internal.corp.net" {
		t.Fatalf("banned_terms not loaded: %v", c.BannedTerms)
	}
	if c.MaxQueryLength != 300 {
		t.Fatalf("max_query_length not loaded: %v", c.MaxQueryLength)
	}
	if len(c.PromptInjectionPatterns) != 1 {
		t.Fatalf("prompt_injection_patterns not loaded: %v", c.PromptInjectionPatterns)
	}
	if len(c.Rules) != 1 || c.Rules[0].Name != "email" {
		t.Fatalf("rules not loaded: %v", c.Rules)
	}
}

func TestSanitizerApplyDefaults(t *testing.T) {
	c := SanitizerConfig{Enabled: true, BannedTerms: []string{"x"}}
	c.ApplyDefaults()
	d := DefaultSanitizerConfig()
	if c.HighPIIBlockThreshold != d.HighPIIBlockThreshold {
		t.Fatalf("threshold default not applied: %v", c.HighPIIBlockThreshold)
	}
	if c.MaxQueryLength != d.MaxQueryLength {
		t.Fatalf("max length default not applied: %v", c.MaxQueryLength)
	}
	if len(c.PromptInjectionPatterns) == 0 {
		t.Fatal("injection patterns default not applied")
	}
	if len(c.Rules) == 0 {
		t.Fatal("rules default not applied")
	}
	if len(c.BannedTerms) != 1 || c.BannedTerms[0] != "x" {
		t.Fatal("user banned terms overwritten")
	}

	c2 := SanitizerConfig{
		MaxQueryLength:          42,
		HighPIIBlockThreshold:   0.9,
		PromptInjectionPatterns: []string{"p"},
		Rules:                   []SanitizerRule{{Name: "r", Pattern: "p", Enabled: true}},
	}
	c2.ApplyDefaults()
	if c2.MaxQueryLength != 42 || c2.HighPIIBlockThreshold != 0.9 {
		t.Fatal("explicit values overwritten by defaults")
	}
	if len(c2.PromptInjectionPatterns) != 1 || len(c2.Rules) != 1 {
		t.Fatal("explicit lists overwritten by defaults")
	}
}
