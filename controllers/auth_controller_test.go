package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gitee.com/tddh/mutong/models"
	"gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func newAuthTestController(t *testing.T, db *gorm.DB) *AuthController {
	t.Helper()
	return &AuthController{
		DB:          db,
		PasswordSvc: auth.NewPasswordService(),
		TokenSvc:    auth.NewTokenService(),
		Sessions:    auth.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 24*time.Hour),
	}
}

func newAuthTestUser(t *testing.T, db *gorm.DB, username, password, status string) *models.User {
	t.Helper()
	hash, err := auth.NewPasswordService().HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	u := &models.User{UUID: "uuid-" + username, Username: username, PasswordHash: hash, Role: "operator", Status: status}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func authRouter(ac *AuthController, identity func(*gin.Context)) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if identity != nil {
		r.Use(identity)
	}
	ac.RegisterRoutes(r)
	return r
}

func doJSON(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestLoginFailurePaths(t *testing.T) {
	db := newPATTestDB(t)
	newAuthTestUser(t, db, "alice", "correct-password", "active")
	newAuthTestUser(t, db, "bob", "whatever", "disabled")
	ac := newAuthTestController(t, db)
	r := authRouter(ac, nil)

	tests := []struct {
		name     string
		body     string
		wantCode int
		wantErr  string
	}{
		{"malformed json", `{"username":`, http.StatusBadRequest, "invalid request"},
		{"unknown user", `{"username":"nobody","password":"x"}`, http.StatusUnauthorized, "用户名或密码错误"},
		{"wrong password", `{"username":"alice","password":"wrong"}`, http.StatusUnauthorized, "用户名或密码错误"},
		{"disabled account", `{"username":"bob","password":"whatever"}`, http.StatusForbidden, "账号已被禁用"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := doJSON(r, http.MethodPost, "/api/auth/login", tc.body)
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d: %s", tc.wantCode, w.Code, w.Body.String())
			}
			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if resp["error"] != tc.wantErr {
				t.Fatalf("expected error %q, got %v", tc.wantErr, resp["error"])
			}
			for _, c := range w.Result().Cookies() {
				if c.Name == "mutong_session" && c.Value != "" {
					t.Fatalf("failed login must not issue a session cookie: %+v", c)
				}
			}
		})
	}
}

func TestLoginWithoutSessionManagerFailsClosed(t *testing.T) {
	db := newPATTestDB(t)
	newAuthTestUser(t, db, "alice", "pw", "active")
	ac := newAuthTestController(t, db)
	ac.Sessions = nil
	r := authRouter(ac, nil)

	w := doJSON(r, http.MethodPost, "/api/auth/login", `{"username":"alice","password":"pw"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without session manager, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoginSuccessResponseBody(t *testing.T) {
	db := newPATTestDB(t)
	u := newAuthTestUser(t, db, "alice", "pw", "active")
	ac := newAuthTestController(t, db)
	r := authRouter(ac, nil)

	w := doJSON(r, http.MethodPost, "/api/auth/login", `{"username":"alice","password":"pw"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.AccessToken == "" {
		t.Fatal("access_token missing")
	}
	if resp.User.ID != u.UUID || resp.User.Username != "alice" || resp.User.Role != "operator" {
		t.Fatalf("unexpected user payload: %+v", resp.User)
	}
	// The returned token must verify as a session for this user.
	claims, err := ac.Sessions.Verify(resp.AccessToken)
	if err != nil {
		t.Fatalf("issued token must verify: %v", err)
	}
	if claims.UserID != u.ID || claims.UUID != u.UUID {
		t.Fatalf("session claims bound to wrong user: %+v", claims)
	}
}

func TestMeIdentityResolution(t *testing.T) {
	db := newPATTestDB(t)
	u := newAuthTestUser(t, db, "alice", "pw", "active")
	ac := newAuthTestController(t, db)

	tests := []struct {
		name     string
		identity func(*gin.Context)
		wantCode int
	}{
		{"no identity", nil, http.StatusUnauthorized},
		{"uuid identity", func(c *gin.Context) { c.Set("user_uuid", u.UUID) }, http.StatusOK},
		{"unknown uuid", func(c *gin.Context) { c.Set("user_uuid", "ghost") }, http.StatusUnauthorized},
		{"numeric identity", func(c *gin.Context) { c.Set("user_id", u.ID) }, http.StatusOK},
		{"unknown numeric id", func(c *gin.Context) { c.Set("user_id", uint(9999)) }, http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := authRouter(ac, tc.identity)
			w := doJSON(r, http.MethodGet, "/api/auth/me", "")
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d: %s", tc.wantCode, w.Code, w.Body.String())
			}
			if tc.wantCode == http.StatusOK {
				var resp map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatal(err)
				}
				if resp["uuid"] != u.UUID || resp["username"] != "alice" {
					t.Fatalf("unexpected me payload: %v", resp)
				}
				if _, leaked := resp["password_hash"]; leaked {
					t.Fatal("password hash leaked in /me")
				}
			}
		})
	}
}

func TestLogoutClearsCookies(t *testing.T) {
	db := newPATTestDB(t)
	ac := newAuthTestController(t, db)
	r := authRouter(ac, nil)

	w := doJSON(r, http.MethodPost, "/api/auth/logout", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var session, refresh *http.Cookie
	for _, c := range w.Result().Cookies() {
		switch c.Name {
		case "mutong_session":
			session = c
		case "refresh_token":
			refresh = c
		}
	}
	if session == nil || session.Value != "" || session.MaxAge >= 0 {
		t.Fatalf("session cookie must be deleted, got %+v", session)
	}
	if refresh == nil || refresh.Value != "" || refresh.MaxAge >= 0 || refresh.Path != "/oauth2" {
		t.Fatalf("refresh cookie must be deleted on /oauth2 path, got %+v", refresh)
	}
}

func TestTokenEndpointsLifecycle(t *testing.T) {
	db := newPATTestDB(t)
	u := newAuthTestUser(t, db, "alice", "pw", "active")
	other := newAuthTestUser(t, db, "mallory", "pw", "active")
	ac := newAuthTestController(t, db)
	identity := func(c *gin.Context) { c.Set("user_id", u.ID) }
	r := authRouter(ac, identity)

	// Unauthenticated access is rejected on every token endpoint.
	anon := authRouter(ac, nil)
	for _, req := range []struct{ method, path string }{
		{http.MethodPost, "/api/auth/tokens"},
		{http.MethodGet, "/api/auth/tokens"},
		{http.MethodDelete, "/api/auth/tokens/1"},
	} {
		w := doJSON(anon, req.method, req.path, `{}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s anonymous: expected 401, got %d", req.method, req.path, w.Code)
		}
	}

	// Invalid payload.
	if w := doJSON(r, http.MethodPost, "/api/auth/tokens", `{"name":`); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid create payload: expected 400, got %d", w.Code)
	}

	// Create with defaults.
	w := doJSON(r, http.MethodPost, "/api/auth/tokens", `{"name":"cli token"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created struct {
		ID     uint   `json:"id"`
		Token  string `json:"token"`
		Prefix string `json:"prefix"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Token, "mtp_") || created.Type != "pat" {
		t.Fatalf("default token must be a PAT, got %+v", created)
	}
	if created.Prefix != created.Token[:12] {
		t.Fatalf("prefix mismatch: %+v", created)
	}
	var row models.APIToken
	if err := db.First(&row, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.UserID == nil || *row.UserID != u.ID || row.RevokedAt != nil {
		t.Fatalf("token row not bound to creator: %+v", row)
	}
	if row.ExpiresAt == nil || time.Until(*row.ExpiresAt) < 89*24*time.Hour {
		t.Fatalf("default expiry must be ~90 days, got %v", row.ExpiresAt)
	}

	// Create a second token with explicit scopes/expiry, then list.
	w = doJSON(r, http.MethodPost, "/api/auth/tokens", `{"name":"scoped","scopes":["read"],"expires_in_days":7}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("scoped create: got %d: %s", w.Code, w.Body.String())
	}
	var scoped struct {
		ID uint `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &scoped)

	w = doJSON(r, http.MethodGet, "/api/auth/tokens", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d", w.Code)
	}
	var list []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(list))
	}
	for _, item := range list {
		if _, leaked := item["token_hash"]; leaked {
			t.Fatal("token hash leaked in list response")
		}
		if _, leaked := item["token"]; leaked {
			t.Fatal("raw token leaked in list response")
		}
	}

	// Revoking another user's token must 404 and leave it intact.
	w = doJSON(r, http.MethodDelete, fmt.Sprintf("/api/auth/tokens/%d", created.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("revoke own token: got %d: %s", w.Code, w.Body.String())
	}
	if err := db.First(&row, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.RevokedAt == nil {
		t.Fatal("revoked_at must be set in DB")
	}
	// Double revoke and foreign revoke both 404.
	if w := doJSON(r, http.MethodDelete, fmt.Sprintf("/api/auth/tokens/%d", created.ID), ""); w.Code != http.StatusNotFound {
		t.Fatalf("double revoke: expected 404, got %d", w.Code)
	}
	otherRouter := authRouter(ac, func(c *gin.Context) { c.Set("user_id", other.ID) })
	if w := doJSON(otherRouter, http.MethodDelete, fmt.Sprintf("/api/auth/tokens/%d", scoped.ID), ""); w.Code != http.StatusNotFound {
		t.Fatalf("foreign revoke: expected 404, got %d", w.Code)
	}
	var stillActive models.APIToken
	if err := db.First(&stillActive, scoped.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stillActive.RevokedAt != nil {
		t.Fatal("foreign revoke must not touch the token")
	}
	// Invalid id.
	if w := doJSON(r, http.MethodDelete, "/api/auth/tokens/not-a-number", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid id: expected 400, got %d", w.Code)
	}
	// Revoked tokens disappear from the list.
	w = doJSON(r, http.MethodGet, "/api/auth/tokens", "")
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("revoked token must not be listed, got %d entries", len(list))
	}
}

func TestLoginStatusModePrecedence(t *testing.T) {
	db := newPATTestDB(t)
	ac := newAuthTestController(t, db)

	tests := []struct {
		name        string
		envMode     string
		envLabel    string
		authMode    string
		oidc        bool
		wantMode    string
		wantLabel   string
		wantNoLabel bool
	}{
		{"default local", "", "", "", false, "local", "", true},
		{"config fallback", "", "", "hybrid", false, "hybrid", "", true},
		{"env overrides config", "zitadel", "", "hybrid", false, "zitadel", "", true},
		{"oidc default label", "", "", "local", true, "local", "SSO", false},
		{"oidc custom label", "", "Corp SSO", "local", true, "local", "Corp SSO", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MUTONG_AUTH_MODE", tc.envMode)
			t.Setenv("MUTONG_OIDC_LABEL", tc.envLabel)
			ac.AuthMode = tc.authMode
			ac.OIDCAvailable = tc.oidc
			r := authRouter(ac, nil)
			w := doJSON(r, http.MethodGet, "/api/auth/login-status", "")
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}
			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if resp["mode"] != tc.wantMode {
				t.Fatalf("mode = %v, want %q", resp["mode"], tc.wantMode)
			}
			label, has := resp["provider_label"]
			if tc.wantNoLabel && has {
				t.Fatalf("provider_label must be absent without OIDC: %v", resp)
			}
			if !tc.wantNoLabel && label != tc.wantLabel {
				t.Fatalf("provider_label = %v, want %q", label, tc.wantLabel)
			}
		})
	}
}

func TestCreateInitialAdminNeverResetsPassword(t *testing.T) {
	db := newPATTestDB(t)
	ac := newAuthTestController(t, db)

	if err := ac.CreateInitialAdmin("admin", "first-password"); err != nil {
		t.Fatal(err)
	}
	var admin models.User
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if admin.Role != "admin" || admin.Status != "active" {
		t.Fatalf("bootstrap admin misconfigured: %+v", admin)
	}
	originalHash := admin.PasswordHash

	// A second startup with a different env password must not reset it.
	if err := ac.CreateInitialAdmin("admin", "attacker-password"); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if admin.PasswordHash != originalHash {
		t.Fatal("existing admin password was reset on re-bootstrap")
	}
	if !ac.PasswordSvc.VerifyPassword("first-password", admin.PasswordHash) {
		t.Fatal("original password must still verify")
	}
}

func TestCookieSecureDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newCtx := func(proto string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		if proto != "" {
			c.Request.Header.Set("X-Forwarded-Proto", proto)
		}
		return c
	}

	t.Setenv("MUTONG_COOKIE_SECURE", "")
	if cookieSecure(newCtx("")) {
		t.Fatal("plain HTTP without proxy header must not be Secure")
	}
	if !cookieSecure(newCtx("https")) {
		t.Fatal("X-Forwarded-Proto=https must be honoured")
	}
	if cookieSecure(newCtx("http")) {
		t.Fatal("X-Forwarded-Proto=http must not be Secure")
	}
	t.Setenv("MUTONG_COOKIE_SECURE", "true")
	if !cookieSecure(newCtx("")) {
		t.Fatal("explicit env true must force Secure")
	}
	t.Setenv("MUTONG_COOKIE_SECURE", "false")
	if cookieSecure(newCtx("https")) {
		t.Fatal("explicit env false must override proxy header")
	}
}
