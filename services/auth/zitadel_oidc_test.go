package auth

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestBuildAuthURLIncludesStateAndPKCE(t *testing.T) {
	client := &ZitadelOIDCClient{
		issuer:      "https://id.example.com",
		clientID:    "mutong-web",
		redirectURI: "http://localhost:8888/api/auth/oidc/callback",
		oauth2Cfg: oauth2.Config{
			ClientID:    "mutong-web",
			RedirectURL: "http://localhost:8888/api/auth/oidc/callback",
			Scopes:      []string{"openid", "profile", "email"},
			Endpoint: oauth2.Endpoint{
				AuthURL: "https://id.example.com/oauth/v2/authorize",
			},
		},
	}
	url, state, verifier := client.BuildAuthURL()
	if state == "" || verifier == "" {
		t.Fatal("expected non-empty state and verifier")
	}

	wantPrefix := "https://id.example.com/oauth/v2/authorize?"
	if !strings.HasPrefix(url, wantPrefix) {
		t.Fatalf("url should start with %q, got %s", wantPrefix, url)
	}
	if !strings.Contains(url, "code_challenge=") {
		t.Fatalf("url missing code_challenge: %s", url)
	}
	if !strings.Contains(url, fmt.Sprintf("state=%s", state)) {
		t.Fatalf("url missing expected state=%s: %s", state, url)
	}
}
