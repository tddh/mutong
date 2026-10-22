package services

import (
	"strings"
	"testing"
)

func TestRedactResourceDefineStringSecret(t *testing.T) {
	raw := `{"kind":"Secret","metadata":{"name":"db"},"data":{"password":"c2VjcmV0"},"stringData":{"token":"abc"}}`
	out := redactResourceDefineString("Secret", raw)

	if strings.Contains(out, "c2VjcmV0") || strings.Contains(out, "stringData") || strings.Contains(out, `"data"`) {
		t.Fatalf("secret payload was not redacted: %s", out)
	}
	if !strings.Contains(out, `"name":"db"`) {
		t.Fatalf("metadata should be preserved: %s", out)
	}
}

func TestRedactResourceDefineStringNonSecret(t *testing.T) {
	raw := `{"kind":"ConfigMap","data":{"key":"value"}}`
	if out := redactResourceDefineString("ConfigMap", raw); out != raw {
		t.Fatalf("non-secret resources must be unchanged, got %s", out)
	}
	if out := redactResourceDefineString("Secret", "not-json"); out != "not-json" {
		t.Fatalf("unparseable input must be unchanged, got %s", out)
	}
}
