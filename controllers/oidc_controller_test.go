package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestCallbackCreatesSessionAndRedirects(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	userSvc := services.NewUserService(db)
	oidcSvc := &fakeOIDCService{
		userinfo: auth.ZitadelUserInfo{
			Subject:       "zitadel-user-1",
			PreferredName: "alice",
			Email:         "alice@example.com",
			Name:          "Alice",
		},
	}
	sessions := auth.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 24*time.Hour)
	ctl := NewOIDCController(userSvc, oidcSvc, sessions)

	r := gin.New()
	ctl.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=abc&state=ok", nil)
	req.AddCookie(&http.Cookie{Name: "mutong_oidc_state", Value: "ok"})
	req.AddCookie(&http.Cookie{Name: "mutong_oidc_verifier", Value: "test-verifier"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "mutong_session=") {
		t.Fatalf("expected mutong_session cookie, got %q", w.Header().Get("Set-Cookie"))
	}
}
