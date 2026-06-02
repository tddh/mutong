package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gitee.com/tddh/mutong/models"
	"gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// fakeTokenValidator is a stub that returns a fixed subject for any token.
type fakeTokenValidator struct {
	subject string
}

func (f *fakeTokenValidator) ValidateAccessToken(_ context.Context, _ string) (*auth.AccessTokenClaims, error) {
	return &auth.AccessTokenClaims{Subject: f.subject}, nil
}

func TestBearerTokenMiddlewareSupportsPATSessionAndOIDC(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.AutoMigrate(&models.User{}, &models.APIToken{})

	zid := "zitadel-user-1"
	user := models.User{
		UUID:          "local-uuid",
		Username:      "alice",
		Role:          "admin",
		AuthProvider:  "zitadel",
		ZitadelUserID: &zid,
		Status:        "active",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	tok, raw, err := auth.CreateToken(db, &user.ID, nil, "local pat", "pat", []string{"read:alerts"}, 30)
	if err != nil || tok == nil {
		t.Fatal(err)
	}

	sessions := auth.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 24*time.Hour)
	rawSession, err := sessions.Issue(auth.SessionClaims{
		UserID:       user.ID,
		UUID:         user.UUID,
		Role:         user.Role,
		AuthProvider: "zitadel",
	})
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BearerTokenMiddleware(db, sessions, &fakeTokenValidator{subject: "zitadel-user-1"}, nil))
	r.GET("/whoami", func(c *gin.Context) {
		c.JSON(200, gin.H{"uuid": GetUserUUID(c), "token_type": c.GetString("token_type")})
	})

	for _, tc := range []struct {
		name   string
		header string
		cookie *http.Cookie
	}{
		{name: "pat", header: "Bearer " + raw},
		{name: "session", cookie: &http.Cookie{Name: "mutong_session", Value: rawSession}},
		{name: "oidc", header: "Bearer external-jwt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			if tc.cookie != nil {
				req.AddCookie(tc.cookie)
			}
			r.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Fatalf("case %s expected 200 got %d: %s", tc.name, w.Code, w.Body.String())
			}
		})
	}
}

// TestBearerTokenMiddlewareLegacyLocalLogin verifies that a plain-UUID
// session cookie from AuthController.Login (before the HMAC-signed era)
// still works via the backward-compatible UUID lookup path.
func TestBearerTokenMiddlewareLegacyLocalLogin(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.AutoMigrate(&models.User{}, &models.APIToken{})

	// Local login user (no ZitadelUserID, AuthProvider="local")
	user := models.User{
		UUID:         "plain-legacy-uuid",
		Username:     "legacy-admin",
		Role:         "admin",
		AuthProvider: "local",
		Status:       "active",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	sessions := auth.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 24*time.Hour)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BearerTokenMiddleware(db, sessions, nil, nil))
	r.GET("/whoami", func(c *gin.Context) {
		c.JSON(200, gin.H{"uuid": GetUserUUID(c), "token_type": c.GetString("token_type")})
	})

	// Simulate old AuthController.Login: plain UUID as mutong_session
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.AddCookie(&http.Cookie{Name: "mutong_session", Value: "plain-legacy-uuid"})
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200 for legacy local login, got %d: %s", w.Code, w.Body.String())
	}
}

func TestViewAuthGuardAllowsCallbackSubpaths(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		path           string
		expectRedirect bool
	}{
		{"/view/login.html", false},
		{"/view/consent.html", false},
		{"/view/callback", false},
		{"/view/callback/something", false},
		{"/view/callback/nested/path", false},
		{"/view/dashboard.html", true},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			r := gin.New()
			r.Use(ViewAuthGuard())
			r.GET("/view/*filepath", func(c *gin.Context) {
				c.String(200, "ok")
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			r.ServeHTTP(w, req)

			if tc.expectRedirect {
				if w.Code != http.StatusFound {
					t.Errorf("expected redirect (302) for %s, got %d", tc.path, w.Code)
				}
			} else {
				if w.Code != 200 {
					t.Errorf("expected 200 for %s, got %d", tc.path, w.Code)
				}
			}
		})
	}
}

func TestLoginSetsHttpOnlyCookie(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.AutoMigrate(&models.User{})

	passwd := "testpass123"
	hashSvc := auth.NewPasswordService()
	hash, err := hashSvc.HashPassword(passwd)
	if err != nil {
		t.Fatal(err)
	}

	user := models.User{
		UUID:         "test-uuid",
		Username:     "testuser",
		PasswordHash: hash,
		Role:         "admin",
		Status:       "active",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	ac := &AuthController{
		DB:          db,
		PasswordSvc: hashSvc,
		TokenSvc:    auth.NewTokenService(),
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/login", ac.Login)

	w := httptest.NewRecorder()
	body := `{"username":"testuser","password":"testpass123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "mutong_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("mutong_session cookie not set")
	}
	if !sessionCookie.HttpOnly {
		t.Error("mutong_session cookie should be HttpOnly")
	}
}
