package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateCodeVerifier(t *testing.T) {
	v, err := generateCodeVerifier()
	if err != nil {
		t.Fatal(err)
	}
	if len(v) < 43 || len(v) > 128 {
		t.Fatalf("verifier length %d, want 43-128", len(v))
	}
	for _, c := range v {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			t.Fatalf("verifier contains invalid char: %c", c)
		}
	}
}

func TestComputeCodeChallenge(t *testing.T) {
	verifier := "test-verifier-string-for-sha256-hashing"
	challenge := computeCodeChallenge(verifier)
	if challenge == "" || challenge == verifier {
		t.Fatal("challenge should be SHA256 hash of verifier")
	}
	if computeCodeChallenge(verifier) != challenge {
		t.Fatal("computeCodeChallenge should be deterministic")
	}
}

func TestGenerateRandomToken(t *testing.T) {
	t1 := generateRandomToken()
	t2 := generateRandomToken()
	if t1 == t2 {
		t.Fatal("generateRandomToken should produce different values")
	}
	if len(t1) != 32 {
		t.Fatalf("token length %d, want 32", len(t1))
	}
}

func TestExchangeCodeForToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/token" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "authorization_code" {
			t.Fatalf("grant_type = %q, want authorization_code", r.Form.Get("grant_type"))
		}
		if r.Form.Get("client_id") != "mutongctl" {
			t.Fatalf("client_id = %q, want mutongctl", r.Form.Get("client_id"))
		}
		if r.Form.Get("code") != "test-code" {
			t.Fatalf("code = %q, want test-code", r.Form.Get("code"))
		}
		if r.Form.Get("redirect_uri") != "http://127.0.0.1:12345/callback" {
			t.Fatalf("redirect_uri mismatch")
		}
		if r.Form.Get("code_verifier") == "" {
			t.Fatal("code_verifier is required")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "at",
			"refresh_token": "rt",
			"token_type":    "Bearer",
			"expires_in":    900,
		})
	}))
	defer server.Close()

	opts := &loginOptions{
		ServerURL:  server.URL,
		HTTPClient: server.Client(),
	}
	token, err := exchangeCodeForToken(opts, "test-code", "http://127.0.0.1:12345/callback", "test-verifier")
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "at" || token.RefreshToken != "rt" {
		t.Fatalf("unexpected token values: %+v", token)
	}
}

func TestExchangeCodeForTokenServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_grant",
			"error_description": "code expired",
		})
	}))
	defer server.Close()

	opts := &loginOptions{
		ServerURL:  server.URL,
		HTTPClient: server.Client(),
	}
	_, err := exchangeCodeForToken(opts, "bad-code", "http://127.0.0.1:12345/callback", "v")
	if err == nil {
		t.Fatal("expected error for invalid code")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("unexpected error: %v", err)
	}
}
