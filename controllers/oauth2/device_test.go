package oauth2

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gitee.com/tddh/mutong/controllers"
	"gitee.com/tddh/mutong/models"
	oauth2model "gitee.com/tddh/mutong/models/oauth2"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const testIssuer = "https://mutong.example.com"

// newDeviceTestDB builds an isolated in-memory DB with every table the device
// flow touches: clients, device codes, issued PATs and users.
func newDeviceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&oauth2model.OAuth2Client{}, &oauth2model.DeviceCode{}, &models.APIToken{}, &models.User{}); err != nil {
		t.Fatal(err)
	}
	client := oauth2model.OAuth2Client{
		ClientID: "mutongctl", Name: "Mutong CLI", RedirectURIs: "[]",
		GrantTypes: `["urn:ietf:params:oauth:grant-type:device_code"]`, ResponseTypes: "[]",
		Scopes: `["openid","profile","read","write"]`, Public: true,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func newDeviceTestUser(t *testing.T, db *gorm.DB, username string) *models.User {
	t.Helper()
	u := &models.User{UUID: "uuid-" + username, Username: username, Role: "operator", Status: "active"}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

// newDeviceRouter wires the real device routes. identity, when non-nil, is
// injected as the verified user_id exactly like the auth middleware does.
func newDeviceRouter(t *testing.T, db *gorm.DB, identity interface{}) (*gin.Engine, *DeviceHandler) {
	t.Helper()
	t.Setenv("MUTONG_ISSUER_URL", testIssuer)
	gin.SetMode(gin.TestMode)
	h := NewDeviceHandler(db)
	r := gin.New()
	if identity != nil {
		r.Use(func(c *gin.Context) { c.Set("user_id", identity) })
	}
	h.RegisterDeviceRoutes(r)
	return r, h
}

func postForm(r *gin.Engine, path string, form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func startDeviceFlow(t *testing.T, r *gin.Engine, clientID, scope string) map[string]interface{} {
	t.Helper()
	w := postForm(r, "/oauth2/device/auth", url.Values{"client_id": {clientID}, "scope": {scope}}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("start device flow: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func loadDeviceCode(t *testing.T, db *gorm.DB, deviceCode string) *oauth2model.DeviceCode {
	t.Helper()
	var dc oauth2model.DeviceCode
	if err := db.Where("device_code = ?", deviceCode).First(&dc).Error; err != nil {
		t.Fatalf("device code row not found: %v", err)
	}
	return &dc
}

// --- Start (POST /oauth2/device/auth) ---

func TestDeviceAuthStart(t *testing.T) {
	db := newDeviceTestDB(t)
	r, _ := newDeviceRouter(t, db, nil)

	tests := []struct {
		name     string
		path     string
		form     url.Values
		wantCode int
		wantErr  string
	}{
		{
			name:     "valid client",
			path:     "/oauth2/device/auth",
			form:     url.Values{"client_id": {"mutongctl"}, "scope": {"read write"}},
			wantCode: http.StatusOK,
		},
		{
			name:     "legacy alias route",
			path:     "/oauth/v2/device_authorization",
			form:     url.Values{"client_id": {"mutongctl"}},
			wantCode: http.StatusOK,
		},
		{
			name:     "missing client_id",
			path:     "/oauth2/device/auth",
			form:     url.Values{"scope": {"read"}},
			wantCode: http.StatusBadRequest,
			wantErr:  "client_id is required",
		},
		{
			name:     "unknown client",
			path:     "/oauth2/device/auth",
			form:     url.Values{"client_id": {"no-such-client"}},
			wantCode: http.StatusBadRequest,
			wantErr:  "invalid client_id",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := postForm(r, tc.path, tc.form, nil)
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d: %s", tc.wantCode, w.Code, w.Body.String())
			}
			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if tc.wantErr != "" {
				if resp["error"] != tc.wantErr {
					t.Fatalf("expected error %q, got %v", tc.wantErr, resp["error"])
				}
				return
			}
			dc, _ := resp["device_code"].(string)
			uc, _ := resp["user_code"].(string)
			if len(dc) != 64 {
				t.Errorf("device_code should be 64 hex chars, got %q", dc)
			}
			if len(uc) != 8 {
				t.Errorf("user_code should be 8 chars, got %q", uc)
			}
			if resp["verification_uri"] != testIssuer+"/device" {
				t.Errorf("verification_uri = %v, want %s/device", resp["verification_uri"], testIssuer)
			}
			if resp["verification_uri_complete"] != testIssuer+"/device?code="+uc {
				t.Errorf("verification_uri_complete = %v", resp["verification_uri_complete"])
			}
			if resp["expires_in"] != float64(600) || resp["interval"] != float64(5) {
				t.Errorf("unexpected expires_in/interval: %v / %v", resp["expires_in"], resp["interval"])
			}
			row := loadDeviceCode(t, db, dc)
			if row.Status != "pending" || row.ClientID != "mutongctl" || row.UserID != nil {
				t.Errorf("unexpected persisted grant: %+v", row)
			}
			if row.Scope != tc.form.Get("scope") {
				t.Errorf("scope not persisted: %q", row.Scope)
			}
			if time.Until(row.ExpiresAt) <= 0 || time.Until(row.ExpiresAt) > 10*time.Minute {
				t.Errorf("expires_at out of range: %v", row.ExpiresAt)
			}
		})
	}
}

// SEC: verification URIs must come from MUTONG_ISSUER_URL, never from the
// attacker-controlled Origin header.
func TestDeviceAuthStartIgnoresMaliciousOrigin(t *testing.T) {
	db := newDeviceTestDB(t)
	r, _ := newDeviceRouter(t, db, nil)

	w := postForm(r, "/oauth2/device/auth",
		url.Values{"client_id": {"mutongctl"}},
		map[string]string{"Origin": "https://evil.attacker.example", "Referer": "https://evil.attacker.example/phish"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"verification_uri", "verification_uri_complete"} {
		v, _ := resp[key].(string)
		if !strings.HasPrefix(v, testIssuer+"/device") {
			t.Fatalf("%s must use configured issuer, got %q", key, v)
		}
		if strings.Contains(v, "evil.attacker.example") {
			t.Fatalf("%s leaked attacker Origin: %q", key, v)
		}
	}
}

// --- Approval page (GET/POST /device) ---

func TestDevicePageRedirectsAnonymousToLogin(t *testing.T) {
	db := newDeviceTestDB(t)
	r, _ := newDeviceRouter(t, db, nil)

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/device?code=ABCD2345", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusFound {
			t.Fatalf("%s /device anonymous: expected 302, got %d", method, w.Code)
		}
		loc := w.Header().Get("Location")
		if !strings.HasPrefix(loc, "/view/login.html?redirect=") {
			t.Fatalf("expected login redirect, got %q", loc)
		}
		if !strings.Contains(loc, url.QueryEscape("/device?code=ABCD2345")) {
			t.Fatalf("redirect must preserve the device query, got %q", loc)
		}
	}
}

func TestDeviceApproveBindsUser(t *testing.T) {
	for _, identityKind := range []string{"uint", "*uint"} {
		t.Run(identityKind, func(t *testing.T) {
			db := newDeviceTestDB(t)
			user := newDeviceTestUser(t, db, "approver-"+identityKind)
			var identity interface{} = user.ID
			if identityKind == "*uint" {
				uid := user.ID
				identity = &uid
			}
			r, _ := newDeviceRouter(t, db, identity)

			resp := startDeviceFlow(t, r, "mutongctl", "read write")
			userCode := resp["user_code"].(string)

			// Lowercase input must be normalized by the handler.
			w := postForm(r, "/device", url.Values{"code": {strings.ToLower(userCode)}, "action": {"approve"}}, nil)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "授权成功") {
				t.Fatalf("approve: expected success page, got %d: %s", w.Code, w.Body.String())
			}
			row := loadDeviceCode(t, db, resp["device_code"].(string))
			if row.Status != "authorized" {
				t.Fatalf("expected status authorized, got %q", row.Status)
			}
			if row.UserID == nil || *row.UserID != user.ID {
				t.Fatalf("grant must be bound to approving user %d, got %v", user.ID, row.UserID)
			}

			// A second decision on the same grant must be rejected.
			w = postForm(r, "/device", url.Values{"code": {userCode}, "action": {"approve"}}, nil)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "该设备代码已被处理") {
				t.Fatalf("repeat approve must report already processed, got %d: %s", w.Code, w.Body.String())
			}
			// Deny after approve must not flip the state either.
			w = postForm(r, "/device", url.Values{"code": {userCode}, "action": {"deny"}}, nil)
			if !strings.Contains(w.Body.String(), "该设备代码已被处理") {
				t.Fatalf("deny after approve must be rejected, got %s", w.Body.String())
			}
			row = loadDeviceCode(t, db, resp["device_code"].(string))
			if row.Status != "authorized" || *row.UserID != user.ID {
				t.Fatalf("state changed after rejected decisions: %+v", row)
			}
		})
	}
}

func TestDeviceDeny(t *testing.T) {
	db := newDeviceTestDB(t)
	user := newDeviceTestUser(t, db, "denier")
	r, _ := newDeviceRouter(t, db, user.ID)

	resp := startDeviceFlow(t, r, "mutongctl", "read")
	userCode := resp["user_code"].(string)

	w := postForm(r, "/device", url.Values{"code": {userCode}, "action": {"deny"}}, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "授权已拒绝") {
		t.Fatalf("deny: expected rejection page, got %d: %s", w.Code, w.Body.String())
	}
	row := loadDeviceCode(t, db, resp["device_code"].(string))
	if row.Status != "denied" || row.UserID == nil || *row.UserID != user.ID {
		t.Fatalf("expected denied grant bound to user, got %+v", row)
	}
}

func TestDevicePageInputValidation(t *testing.T) {
	db := newDeviceTestDB(t)
	user := newDeviceTestUser(t, db, "validator")
	r, _ := newDeviceRouter(t, db, user.ID)

	tests := []struct {
		name    string
		form    url.Values
		wantMsg string
	}{
		{"empty code", url.Values{"code": {"  "}, "action": {"approve"}}, "请输入设备代码"},
		{"unknown code", url.Values{"code": {"ZZZZZZZZ"}, "action": {"approve"}}, "无效的设备代码"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := postForm(r, "/device", tc.form, nil)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), tc.wantMsg) {
				t.Fatalf("expected %q, got %d: %s", tc.wantMsg, w.Code, w.Body.String())
			}
		})
	}

	// Expired grant must be reported as expired, not as invalid.
	expired := &oauth2model.DeviceCode{
		DeviceCode: "expired-device-code", UserCode: "EXPIRED1", ClientID: "mutongctl",
		Status: "pending", ExpiresAt: time.Now().Add(-time.Minute),
	}
	if err := db.Create(expired).Error; err != nil {
		t.Fatal(err)
	}
	w := postForm(r, "/device", url.Values{"code": {"EXPIRED1"}, "action": {"approve"}}, nil)
	if !strings.Contains(w.Body.String(), "设备代码已过期") {
		t.Fatalf("expected expired message, got %s", w.Body.String())
	}
}

// The code query parameter is attacker-controllable and rendered into an HTML
// attribute; it must be escaped.
func TestDevicePageEscapesUserCode(t *testing.T) {
	db := newDeviceTestDB(t)
	user := newDeviceTestUser(t, db, "escape")
	r, _ := newDeviceRouter(t, db, user.ID)

	payload := `"><script>alert(1)</script>`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/device?code="+url.QueryEscape(payload), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatalf("unescaped script injected into page: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("expected escaped payload in page, got %s", body)
	}
}

// --- Token exchange ---

func exchangeDeviceCode(r *gin.Engine, deviceCode, clientID string) *httptest.ResponseRecorder {
	return postForm(r, "/oauth2/token", url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
		"client_id":   {clientID},
	}, nil)
}

func TestDeviceTokenExchangeErrorStates(t *testing.T) {
	db := newDeviceTestDB(t)
	user := newDeviceTestUser(t, db, "exchanger")
	r, h := newDeviceRouter(t, db, user.ID)
	// The real token endpoint dispatches device grants through TokenHandler.
	tr := gin.New()
	th := &TokenHandler{DeviceH: h}
	tr.POST("/oauth2/token", th.HandleToken)

	pending := startDeviceFlow(t, r, "mutongctl", "read")

	denied := startDeviceFlow(t, r, "mutongctl", "read")
	if w := postForm(r, "/device", url.Values{"code": {denied["user_code"].(string)}, "action": {"deny"}}, nil); w.Code != http.StatusOK {
		t.Fatalf("deny setup failed: %d", w.Code)
	}

	expired := &oauth2model.DeviceCode{
		DeviceCode: "expired-exchange-code", UserCode: "EXPIRED2", ClientID: "mutongctl",
		Status: "pending", ExpiresAt: time.Now().Add(-time.Minute),
	}
	if err := db.Create(expired).Error; err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		deviceCode string
		clientID   string
		wantErr    string
	}{
		{"pending", pending["device_code"].(string), "mutongctl", "authorization_pending"},
		{"denied", denied["device_code"].(string), "mutongctl", "access_denied"},
		{"unknown code", "does-not-exist", "mutongctl", "invalid_device_code"},
		{"empty code", "", "mutongctl", "invalid_device_code"},
		{"expired", "expired-exchange-code", "mutongctl", "expired_device_code"},
		{"wrong client", pending["device_code"].(string), "another-client", "invalid_client"},
		{"missing client", pending["device_code"].(string), "", "invalid_client"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := exchangeDeviceCode(tr, tc.deviceCode, tc.clientID)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if resp["error"] != tc.wantErr {
				t.Fatalf("expected error %q, got %v", tc.wantErr, resp["error"])
			}
		})
	}

	// None of the failed exchanges may consume or mutate the pending grant.
	row := loadDeviceCode(t, db, pending["device_code"].(string))
	if row.Status != "pending" {
		t.Fatalf("failed exchanges must not consume the grant, status=%q", row.Status)
	}
}

func TestDeviceTokenExchangeIssuesBoundPAT(t *testing.T) {
	db := newDeviceTestDB(t)
	user := newDeviceTestUser(t, db, "pat-owner")
	r, h := newDeviceRouter(t, db, user.ID)
	tr := gin.New()
	tr.POST("/oauth2/token", (&TokenHandler{DeviceH: h}).HandleToken)

	resp := startDeviceFlow(t, r, "mutongctl", "read write")
	deviceCode := resp["device_code"].(string)
	if w := postForm(r, "/device", url.Values{"code": {resp["user_code"].(string)}, "action": {"approve"}}, nil); w.Code != http.StatusOK {
		t.Fatalf("approve failed: %d", w.Code)
	}

	w := exchangeDeviceCode(tr, deviceCode, "mutongctl")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var tok map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &tok); err != nil {
		t.Fatal(err)
	}
	raw, _ := tok["access_token"].(string)
	if !strings.HasPrefix(raw, "mtp_") {
		t.Fatalf("expected a real mtp_ PAT, got %q", raw)
	}
	if tok["token_type"] != "Bearer" || tok["scope"] != "read write" || tok["expires_in"] != float64(86400) {
		t.Fatalf("unexpected token response: %v", tok)
	}

	// The PAT must be persisted, hashed (never stored raw) and bound to the
	// approving user with the granted scopes.
	hsum := sha256.Sum256([]byte(raw))
	var pat models.APIToken
	if err := db.Where("token_hash = ?", hex.EncodeToString(hsum[:])).First(&pat).Error; err != nil {
		t.Fatalf("issued PAT not persisted: %v", err)
	}
	if pat.UserID == nil || *pat.UserID != user.ID {
		t.Fatalf("PAT must be bound to approving user %d, got %v", user.ID, pat.UserID)
	}
	if pat.TokenType != "pat" || pat.RevokedAt != nil {
		t.Fatalf("unexpected PAT row: %+v", pat)
	}
	var scopes []string
	if err := json.Unmarshal([]byte(pat.Scopes), &scopes); err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 2 || scopes[0] != "read" || scopes[1] != "write" {
		t.Fatalf("granted scopes not persisted: %v", scopes)
	}
	if pat.ExpiresAt == nil || time.Until(*pat.ExpiresAt) < 23*time.Hour {
		t.Fatalf("PAT should expire in ~24h, got %v", pat.ExpiresAt)
	}
	if strings.Contains(pat.TokenHash, raw) || pat.TokenPrefix != raw[:12] {
		t.Fatalf("raw token leaked into storage: %+v", pat)
	}

	// Single use: the grant is completed and a replay is rejected.
	row := loadDeviceCode(t, db, deviceCode)
	if row.Status != "completed" {
		t.Fatalf("expected completed grant after exchange, got %q", row.Status)
	}
	w2 := exchangeDeviceCode(tr, deviceCode, "mutongctl")
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("replay: expected 400, got %d: %s", w2.Code, w2.Body.String())
	}
	var replay map[string]interface{}
	_ = json.Unmarshal(w2.Body.Bytes(), &replay)
	if replay["error"] != "invalid_grant" {
		t.Fatalf("replay must return invalid_grant, got %v", replay["error"])
	}
	var count int64
	db.Model(&models.APIToken{}).Count(&count)
	if count != 1 {
		t.Fatalf("replay must not issue a second token, found %d tokens", count)
	}
}

// End-to-end closure: the PAT issued by the device flow must authenticate
// through the same BearerTokenMiddleware as every other token.
func TestDeviceIssuedPATPassesBearerMiddleware(t *testing.T) {
	db := newDeviceTestDB(t)
	user := newDeviceTestUser(t, db, "e2e-user")
	r, h := newDeviceRouter(t, db, user.ID)
	tr := gin.New()
	tr.POST("/oauth2/token", (&TokenHandler{DeviceH: h}).HandleToken)

	resp := startDeviceFlow(t, r, "mutongctl", "read")
	if w := postForm(r, "/device", url.Values{"code": {resp["user_code"].(string)}, "action": {"approve"}}, nil); w.Code != http.StatusOK {
		t.Fatalf("approve failed: %d", w.Code)
	}
	w := exchangeDeviceCode(tr, resp["device_code"].(string), "mutongctl")
	if w.Code != http.StatusOK {
		t.Fatalf("exchange failed: %d: %s", w.Code, w.Body.String())
	}
	var tok map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &tok)
	raw := tok["access_token"].(string)

	gin.SetMode(gin.TestMode)
	api := gin.New()
	api.Use(controllers.BearerTokenMiddleware(db, nil, nil, nil))
	api.Use(controllers.RequireAuthMiddleware())
	api.GET("/api/v1/whoami", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"uuid": controllers.GetUserUUID(c), "role": controllers.GetUserRole(c)})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	aw := httptest.NewRecorder()
	api.ServeHTTP(aw, req)
	if aw.Code != http.StatusOK {
		t.Fatalf("device PAT must authenticate, got %d: %s", aw.Code, aw.Body.String())
	}
	var who map[string]interface{}
	_ = json.Unmarshal(aw.Body.Bytes(), &who)
	if who["uuid"] != user.UUID || who["role"] != user.Role {
		t.Fatalf("PAT must resolve to the approving user, got %v", who)
	}

	// Revoking the PAT must immediately cut off access.
	if err := db.Model(&models.APIToken{}).Where("user_id = ?", user.ID).Update("revoked_at", time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	aw2 := httptest.NewRecorder()
	api.ServeHTTP(aw2, req)
	if aw2.Code != http.StatusUnauthorized {
		t.Fatalf("revoked device PAT must be rejected, got %d", aw2.Code)
	}
}

func TestDeviceTokenAuthorizedWithoutUserIsServerError(t *testing.T) {
	db := newDeviceTestDB(t)
	_, h := newDeviceRouter(t, db, nil)
	tr := gin.New()
	tr.POST("/oauth2/token", (&TokenHandler{DeviceH: h}).HandleToken)

	// Defensive branch: an authorized row with no bound user must never issue
	// a token.
	orphan := &oauth2model.DeviceCode{
		DeviceCode: "orphan-code", UserCode: "ORPHAN01", ClientID: "mutongctl",
		Status: "authorized", UserID: nil, ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	if err := db.Create(orphan).Error; err != nil {
		t.Fatal(err)
	}
	w := exchangeDeviceCode(tr, "orphan-code", "mutongctl")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for user-less grant, got %d: %s", w.Code, w.Body.String())
	}
	var count int64
	db.Model(&models.APIToken{}).Count(&count)
	if count != 0 {
		t.Fatalf("no token may be issued for a user-less grant, found %d", count)
	}
}

func TestDeviceTokenPersistenceFailureIsServerError(t *testing.T) {
	db := newDeviceTestDB(t)
	user := newDeviceTestUser(t, db, "persist-fail")
	r, h := newDeviceRouter(t, db, user.ID)
	tr := gin.New()
	tr.POST("/oauth2/token", (&TokenHandler{DeviceH: h}).HandleToken)

	resp := startDeviceFlow(t, r, "mutongctl", "read")
	if w := postForm(r, "/device", url.Values{"code": {resp["user_code"].(string)}, "action": {"approve"}}, nil); w.Code != http.StatusOK {
		t.Fatalf("approve failed: %d", w.Code)
	}
	// Break token persistence after approval.
	if err := db.Migrator().DropTable(&models.APIToken{}); err != nil {
		t.Fatal(err)
	}
	w := exchangeDeviceCode(tr, resp["device_code"].(string), "mutongctl")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when token persistence fails, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "mtp_") {
		t.Fatalf("no token may leak on failure: %s", w.Body.String())
	}
}

// A non-device grant must fall through to the next handler, not be swallowed.
func TestDeviceTokenNonDeviceGrantPassesThrough(t *testing.T) {
	db := newDeviceTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewDeviceHandler(db)
	r := gin.New()
	r.POST("/oauth2/token", h.HandleDeviceToken, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"handled_by": "next"})
	})
	w := postForm(r, "/oauth2/token", url.Values{"grant_type": {"authorization_code"}, "code": {"abc"}}, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "next") {
		t.Fatalf("non-device grant must reach the next handler, got %d: %s", w.Code, w.Body.String())
	}
}
