package oauth2

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDiscoveryDocument(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterOAuth2Routes(r, nil, "https://mutong.example.com", nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("discovery: got %d", w.Code)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"issuer":                 "https://mutong.example.com",
		"authorization_endpoint": "https://mutong.example.com/oauth2/authorize",
		"token_endpoint":         "https://mutong.example.com/oauth2/token",
		"introspection_endpoint": "https://mutong.example.com/oauth2/introspect",
		"revocation_endpoint":    "https://mutong.example.com/oauth2/revoke",
	}
	for k, v := range want {
		if doc[k] != v {
			t.Errorf("discovery[%s] = %v, want %q", k, doc[k], v)
		}
	}
	// PKCE S256 must be advertised.
	methods, _ := doc["code_challenge_methods_supported"].([]interface{})
	if len(methods) != 1 || methods[0] != "S256" {
		t.Errorf("code_challenge_methods_supported = %v, want [S256]", methods)
	}
}

// An anonymous browser hitting /oauth2/authorize must be sent to login with
// the full original URL preserved for the post-login redirect.
func TestAuthorizeAnonymousRedirectsToLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterOAuth2Routes(r, nil, "https://mutong.example.com", nil)

	original := "/oauth2/authorize?client_id=mutong-web&response_type=code&redirect_uri=/view/callback"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, original, nil))
	if w.Code != http.StatusFound {
		t.Fatalf("anonymous authorize: got %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/view/login.html?redirect=") {
		t.Fatalf("expected login redirect, got %q", loc)
	}
	if !strings.Contains(loc, url.QueryEscape(original)) {
		t.Fatalf("original authorize URL must be preserved: %q", loc)
	}
}

func TestGetUserIdentifier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		set  func(*gin.Context)
		want string
	}{
		{"none", func(*gin.Context) {}, ""},
		{"uuid preferred", func(c *gin.Context) { c.Set("user_uuid", "u-1"); c.Set("user_id", "u-2") }, "u-1"},
		{"string user_id fallback", func(c *gin.Context) { c.Set("user_id", "u-2") }, "u-2"},
		{"numeric user_id ignored", func(c *gin.Context) { c.Set("user_id", uint(7)) }, ""},
		{"empty uuid falls through", func(c *gin.Context) { c.Set("user_uuid", ""); c.Set("user_id", "u-3") }, "u-3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			tc.set(c)
			if got := getUserIdentifier(c); got != tc.want {
				t.Fatalf("getUserIdentifier = %q, want %q", got, tc.want)
			}
		})
	}
}
