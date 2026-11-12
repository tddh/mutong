package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"gitee.com/tddh/mutong/models"
	"gitee.com/tddh/mutong/services"
	"gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// fakeOIDCService is a test double for the oidcService interface.
type fakeOIDCService struct {
	userinfo auth.ZitadelUserInfo
}

func (f *fakeOIDCService) BuildAuthURL() (authURL, state, verifier string) {
	return "https://zitadel.example.com/authorize", "ok", "test-verifier"
}

func (f *fakeOIDCService) Exchange(ctx context.Context, code, verifier string) (*oauth2.Token, *auth.ZitadelUserInfo, error) {
	return nil, &f.userinfo, nil
}

func newOIDCTestController(t *testing.T) (*OIDCController, *gin.Engine) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	ctl := NewOIDCController(services.NewUserService(db), &fakeOIDCService{
		userinfo: auth.ZitadelUserInfo{
			Subject: "zitadel-user-1", PreferredName: "alice",
			Email: "alice@example.com", Name: "Alice",
		},
	}, auth.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 24*time.Hour))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	ctl.RegisterRoutes(r)
	return ctl, r
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestCallbackCreatesSessionAndRedirects(t *testing.T) {
	_, r := newOIDCTestController(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=abc&state=ok", nil)
	req.AddCookie(&http.Cookie{Name: "mutong_oidc_state", Value: "ok"})
	req.AddCookie(&http.Cookie{Name: "mutong_oidc_verifier", Value: "test-verifier"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 got %d body=%s", w.Code, w.Body.String())
	}
	if findCookie(w.Result().Cookies(), "mutong_session") == nil {
		t.Fatalf("expected mutong_session cookie, got %v", w.Header().Values("Set-Cookie"))
	}
}

func TestLoginSetsOIDCCookiesWithScopedPath(t *testing.T) {
	_, r := newOIDCTestController(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/auth/oidc/login", nil))

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 got %d", w.Code)
	}
	for _, name := range []string{"mutong_oidc_state", "mutong_oidc_verifier"} {
		c := findCookie(w.Result().Cookies(), name)
		if c == nil {
			t.Fatalf("missing cookie %s", name)
		}
		if c.Path != oidcCookiePath {
			t.Fatalf("cookie %s path = %q, want %q", name, c.Path, oidcCookiePath)
		}
		if !c.HttpOnly {
			t.Fatalf("cookie %s must be HttpOnly", name)
		}
	}
}

func TestCallbackRejectsInvalidState(t *testing.T) {
	tests := []struct {
		name        string
		stateCookie string
		queryState  string
		setCookie   bool
	}{
		{"mismatched state", "ok", "tampered", true},
		{"missing state cookie", "", "ok", false},
		{"empty query state", "ok", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, r := newOIDCTestController(t)
			req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=abc&state="+tc.queryState, nil)
			if tc.setCookie {
				req.AddCookie(&http.Cookie{Name: "mutong_oidc_state", Value: tc.stateCookie})
				req.AddCookie(&http.Cookie{Name: "mutong_oidc_verifier", Value: "test-verifier"})
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for invalid state, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCallbackClearsOIDCCookiesOnScopedPath(t *testing.T) {
	_, r := newOIDCTestController(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=abc&state=ok", nil)
	req.AddCookie(&http.Cookie{Name: "mutong_oidc_state", Value: "ok"})
	req.AddCookie(&http.Cookie{Name: "mutong_oidc_verifier", Value: "test-verifier"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	for _, name := range []string{"mutong_oidc_state", "mutong_oidc_verifier"} {
		c := findCookie(w.Result().Cookies(), name)
		if c == nil {
			t.Fatalf("expected a clearing Set-Cookie for %s", name)
		}
		if c.Path != oidcCookiePath {
			t.Fatalf("clearing cookie %s path = %q, want %q (must match set path)", name, c.Path, oidcCookiePath)
		}
		if c.MaxAge >= 0 && c.Value != "" {
			t.Fatalf("cookie %s must be cleared, got value=%q maxAge=%d", name, c.Value, c.MaxAge)
		}
	}
}

func TestCallbackClearsOIDCCookiesOnInvalidState(t *testing.T) {
	_, r := newOIDCTestController(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=abc&state=tampered", nil)
	req.AddCookie(&http.Cookie{Name: "mutong_oidc_state", Value: "ok"})
	req.AddCookie(&http.Cookie{Name: "mutong_oidc_verifier", Value: "test-verifier"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	// Even on rejection the one-time cookies must be cleared.
	if c := findCookie(w.Result().Cookies(), "mutong_oidc_state"); c == nil || c.Path != oidcCookiePath {
		t.Fatalf("state cookie must be cleared on the scoped path, got %+v", c)
	}
}
