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
	"github.com/redis/go-redis/v9"
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

// TestBearerTokenMiddlewareRejectsPlainUUIDCookie verifies the legacy
// "cookie is a UUID" fallback is gone: a plain-UUID mutong_session cookie must
// not authenticate. BearerTokenMiddleware alone establishes no identity, and
// combined with RequireAuthMiddleware the request is rejected with 401.
func TestBearerTokenMiddlewareRejectsPlainUUIDCookie(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.AutoMigrate(&models.User{}, &models.APIToken{})

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
	r.Use(RequireAuthMiddleware())
	r.GET("/whoami", func(c *gin.Context) {
		c.JSON(200, gin.H{"uuid": GetUserUUID(c), "token_type": c.GetString("token_type")})
	})

	// A plain UUID (not an HMAC-signed session token) must be rejected.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.AddCookie(&http.Cookie{Name: "mutong_session", Value: "plain-legacy-uuid"})
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("plain UUID cookie must not authenticate: expected 401, got %d: %s", w.Code, w.Body.String())
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
		Sessions:    auth.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 24*time.Hour),
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

// --- SEC-02 / SEC-13: PAT/SAT lifecycle, scopes, and session revocation ---

func newPATTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.APIToken{}, &models.ServiceAccount{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func patRouter(db *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BearerTokenMiddleware(db, nil, nil, nil))
	r.GET("/whoami", func(c *gin.Context) { c.JSON(200, gin.H{"uuid": GetUserUUID(c)}) })
	r.HEAD("/whoami", func(c *gin.Context) { c.Status(200) })
	r.POST("/whoami", func(c *gin.Context) { c.JSON(200, gin.H{"uuid": GetUserUUID(c)}) })
	return r
}

func doBearer(r *gin.Engine, method, raw string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	r.ServeHTTP(w, req)
	return w
}

func TestPATLifecycleEnforcement(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*gorm.DB) string
		want  int
	}{
		{
			name: "active user PAT allowed",
			setup: func(db *gorm.DB) string {
				u := models.User{UUID: "u-ok", Username: "ok", Role: "viewer", Status: "active"}
				db.Create(&u)
				_, raw, _ := auth.CreateToken(db, &u.ID, nil, "pat", "pat", nil, 30)
				return raw
			},
			want: http.StatusOK,
		},
		{
			name: "disabled user PAT rejected",
			setup: func(db *gorm.DB) string {
				u := models.User{UUID: "u-dis", Username: "dis", Role: "viewer", Status: "disabled"}
				db.Create(&u)
				_, raw, _ := auth.CreateToken(db, &u.ID, nil, "pat", "pat", nil, 30)
				return raw
			},
			want: http.StatusUnauthorized,
		},
		{
			name: "missing user PAT rejected",
			setup: func(db *gorm.DB) string {
				bogus := uint(9999)
				_, raw, _ := auth.CreateToken(db, &bogus, nil, "pat", "pat", nil, 30)
				return raw
			},
			want: http.StatusUnauthorized,
		},
		{
			name: "soft-deleted user PAT rejected",
			setup: func(db *gorm.DB) string {
				u := models.User{UUID: "u-del", Username: "del", Role: "viewer", Status: "active"}
				db.Create(&u)
				_, raw, _ := auth.CreateToken(db, &u.ID, nil, "pat", "pat", nil, 30)
				db.Delete(&u)
				return raw
			},
			want: http.StatusUnauthorized,
		},
		{
			name: "active SAT allowed",
			setup: func(db *gorm.DB) string {
				sa := models.ServiceAccount{Name: "svc-ok", Status: "active"}
				db.Create(&sa)
				_, raw, _ := auth.CreateToken(db, nil, &sa.ID, "sat", "sat", nil, 30)
				return raw
			},
			want: http.StatusOK,
		},
		{
			name: "disabled SAT rejected",
			setup: func(db *gorm.DB) string {
				sa := models.ServiceAccount{Name: "svc-dis", Status: "disabled"}
				db.Create(&sa)
				_, raw, _ := auth.CreateToken(db, nil, &sa.ID, "sat", "sat", nil, 30)
				return raw
			},
			want: http.StatusUnauthorized,
		},
		{
			name: "missing SAT rejected",
			setup: func(db *gorm.DB) string {
				bogus := uint(9999)
				_, raw, _ := auth.CreateToken(db, nil, &bogus, "sat", "sat", nil, 30)
				return raw
			},
			want: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := newPATTestDB(t)
			raw := tc.setup(db)
			w := doBearer(patRouter(db), http.MethodGet, raw)
			if w.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, w.Code, w.Body.String())
			}
		})
	}
}

func TestPATScopeEnforcement(t *testing.T) {
	tests := []struct {
		name   string
		scopes []string
		method string
		want   int
	}{
		{"read scope allows GET", []string{"read"}, http.MethodGet, http.StatusOK},
		{"read scope blocks POST", []string{"read"}, http.MethodPost, http.StatusForbidden},
		{"read:alerts blocks POST", []string{"read:alerts"}, http.MethodPost, http.StatusForbidden},
		{"read scope allows HEAD", []string{"read"}, http.MethodHead, http.StatusOK},
		{"empty scopes allow POST", nil, http.MethodPost, http.StatusOK},
		{"read+write allow POST", []string{"read", "write"}, http.MethodPost, http.StatusOK},
		{"write allows POST", []string{"write"}, http.MethodPost, http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := newPATTestDB(t)
			u := models.User{UUID: "u-scope", Username: "scope", Role: "viewer", Status: "active"}
			db.Create(&u)
			_, raw, err := auth.CreateToken(db, &u.ID, nil, "pat", "pat", tc.scopes, 30)
			if err != nil {
				t.Fatal(err)
			}
			w := doBearer(patRouter(db), tc.method, raw)
			if w.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, w.Code, w.Body.String())
			}
		})
	}
}

func TestSessionRevokedWhenUserDisabled(t *testing.T) {
	db := newPATTestDB(t)
	u := models.User{UUID: "u-sess", Username: "carol", Role: "admin", Status: "active", PasswordHash: "h"}
	db.Create(&u)
	sessions := auth.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 24*time.Hour)
	raw, err := sessions.Issue(auth.SessionClaims{
		UserID: u.ID, UUID: u.UUID, Role: u.Role, AuthProvider: "local",
		PasswordFingerprint: auth.PasswordFingerprint(u.PasswordHash),
	})
	if err != nil {
		t.Fatal(err)
	}

	build := func() *gin.Engine {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(BearerTokenMiddleware(db, sessions, nil, nil))
		r.Use(RequireAuthMiddleware())
		r.GET("/whoami", func(c *gin.Context) { c.JSON(200, gin.H{"uuid": GetUserUUID(c)}) })
		return r
	}
	do := func(r *gin.Engine) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
		req.AddCookie(&http.Cookie{Name: "mutong_session", Value: raw})
		r.ServeHTTP(w, req)
		return w
	}

	// Active user: session authenticates.
	if w := do(build()); w.Code != http.StatusOK {
		t.Fatalf("active session expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Disable the user; a fresh middleware (fresh guard cache) must revoke it.
	db.Model(&models.User{}).Where("uuid = ?", u.UUID).Update("status", "disabled")
	if w := do(build()); w.Code != http.StatusUnauthorized {
		t.Fatalf("disabled user session expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

// --- SEC-02: casbin denies empty identity, exempts public paths ---

func TestCasbinRejectsEmptyUserUUID(t *testing.T) {
	auth.SetRBACMode(auth.RBACEnforce)
	t.Cleanup(func() { auth.SetRBACMode(auth.RBACEnforce) })
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(CasbinMiddleware(nil))
	r.GET("/api/v1/things", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/things", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("empty user_uuid on protected path expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCasbinSkipsPublicPaths(t *testing.T) {
	auth.SetRBACMode(auth.RBACEnforce)
	t.Cleanup(func() { auth.SetRBACMode(auth.RBACEnforce) })
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(CasbinMiddleware(nil))
	r.POST("/api/auth/login", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/login", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("public path must not be blocked by casbin, got %d: %s", w.Code, w.Body.String())
	}
}

// --- SEC-10: login fails closed when the limiter's Redis is unavailable ---

func TestLoginFailsClosedWhenLimiterUnavailable(t *testing.T) {
	db := newPATTestDB(t)
	hash, err := auth.NewPasswordService().HashPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	u := models.User{UUID: "u-login", Username: "loginuser", Role: "admin", Status: "active", PasswordHash: hash}
	db.Create(&u)

	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  200 * time.Millisecond,
		ReadTimeout:  200 * time.Millisecond,
		WriteTimeout: 200 * time.Millisecond,
		MaxRetries:   -1,
	})
	t.Cleanup(func() { _ = rdb.Close() })

	ac := &AuthController{
		DB:          db,
		PasswordSvc: auth.NewPasswordService(),
		TokenSvc:    auth.NewTokenService(),
		Sessions:    auth.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 24*time.Hour),
		LoginLimit:  auth.NewLoginLimiter(rdb),
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/login", ac.Login)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"loginuser","password":"pw"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected fail-closed 503, got %d: %s", w.Code, w.Body.String())
	}
}
