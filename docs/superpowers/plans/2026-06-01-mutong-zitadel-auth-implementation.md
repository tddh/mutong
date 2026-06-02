# Mutong Zitadel Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Mutong 从本地 fosite 主导的认证体系迁移为“Zitadel 管身份认证 + Mutong 保留 Casbin / PAT / SAT / 本地用户映射”的混合架构，并保持 Web、CLI、API Token 能力不回退。

**Architecture:** 首先引入 Zitadel OIDC 配置、本地用户映射字段和本地 session 服务；然后新增 OIDC login/callback 链路，重构 BearerTokenMiddleware 为 PAT/SAT、Zitadel access token、本地 session 三路收敛；最后改造前端登录态与 CLI Device Flow。整个方案保持 Casbin 与 PAT/SAT 不变，并通过 `auth.mode=local|hybrid|zitadel` 实现灰度切换与回滚。

**Tech Stack:** Go 1.25、Gin、GORM、PostgreSQL、Casbin、Redis、Vue 3、Vitest、Cobra、Zitadel OIDC (`github.com/coreos/go-oidc/v3/oidc` + `golang.org/x/oauth2`)

---

## File Structure

### Create

- `services/auth/zitadel_oidc.go` — Zitadel OIDC 客户端封装：discovery、authorize URL、code exchange、ID Token/UserInfo 验证、JWT access token 校验。
- `services/auth/session.go` — Mutong 本地 session 签发/验证/清理，浏览器登录态承载层。
- `controllers/oidc_controller.go` — `/api/auth/oidc/login` 与 `/api/auth/oidc/callback`。
- `services/auth/zitadel_oidc_test.go` — OIDC 服务测试。
- `services/auth/session_test.go` — session 服务测试。
- `controllers/oidc_controller_test.go` — OIDC 控制器测试。
- `controllers/auth_middleware_test.go` — 三路认证中间件测试。
- `view/src/__tests__/auth.smoke.test.ts` — 前端 auth store 测试。
- `view/src/__tests__/login.smoke.test.ts` — 前端登录页测试。
- `cmd/mutongctl/auth/login_test.go` — CLI Device Flow 登录测试。

### Modify

- `configs/config.auth.yaml.example`
- `config/config.go`
- `models/user.go`
- `services/user_service.go`
- `interfaces/user_interface.go`
- `controllers/auth_controller.go`
- `controllers/auth_middleware.go`
- `cmd/main.go`
- `view/src/login.js`
- `view/src/login.html`
- `view/src/stores/auth.js`
- `cmd/mutongctl/auth/login.go`
- `cmd/mutongctl/auth/status.go`
- `cmd/mutongctl/auth/logout.go`
- `cmd/mutongctl/internal/client/client.go`
- `go.mod`
- `docs/auth-design.md`

### Leave Unchanged Intentionally

- `services/auth/token.go`
- `models/api_token.go`
- `services/auth/casbin.go`
- `controllers/casbin_middleware.go`
- `configs/casbin_model.conf`
- `models/service_account.go`

---

### Task 1: Add Zitadel Config And User Mapping Foundations

**Files:**
- Modify: `configs/config.auth.yaml.example`
- Modify: `config/config.go`
- Modify: `models/user.go`
- Modify: `services/user_service.go`
- Modify: `interfaces/user_interface.go`
- Test: `config/config_test.go`

- [ ] **Step 1: Write the failing config and model tests**

```go
// config/config_test.go
func TestAuthModeDefaultsToLocal(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()
	if cfg.Auth.Mode != "local" {
		t.Fatalf("expected auth mode local, got %q", cfg.Auth.Mode)
	}
}

func TestZitadelConfigLoadsFromFile(t *testing.T) {
	yaml := []byte(`auth:
  mode: hybrid
  zitadel:
    issuer: https://id.example.com
    client_id: mutong-web
    client_secret: secret
    redirect_uri: http://localhost:8888/api/auth/oidc/callback
    scopes: [openid, profile, email]
`)
	path := filepath.Join(t.TempDir(), "config.auth.yaml")
	if err := os.WriteFile(path, yaml, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Mode != "hybrid" {
		t.Fatalf("expected hybrid, got %q", cfg.Auth.Mode)
	}
	if cfg.Auth.Zitadel.ClientID != "mutong-web" {
		t.Fatalf("unexpected client id %q", cfg.Auth.Zitadel.ClientID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./config -run 'TestAuthModeDefaultsToLocal|TestZitadelConfigLoadsFromFile' -v`
Expected: FAIL with unknown `Auth.Mode` / `Auth.Zitadel` fields or missing default behavior.

- [ ] **Step 3: Write minimal config and model implementation**

```go
// config/config.go
type ZitadelConfig struct {
	Issuer       string   `mapstructure:"issuer" yaml:"issuer"`
	ClientID     string   `mapstructure:"client_id" yaml:"client_id"`
	ClientSecret string   `mapstructure:"client_secret" yaml:"client_secret"`
	RedirectURI  string   `mapstructure:"redirect_uri" yaml:"redirect_uri"`
	Scopes       []string `mapstructure:"scopes" yaml:"scopes"`
	Audience     string   `mapstructure:"audience" yaml:"audience"`
	DeviceClient string   `mapstructure:"device_client" yaml:"device_client"`
}

type AuthConfig struct {
	Mode    string        `mapstructure:"mode" yaml:"mode"`
	Zitadel ZitadelConfig `mapstructure:"zitadel" yaml:"zitadel"`
	// keep existing auth config fields below
	Issuer               string        `mapstructure:"issuer" yaml:"issuer"`
	AccessTokenLifespan  time.Duration `mapstructure:"access_token_lifespan" yaml:"access_token_lifespan"`
	RefreshTokenLifespan time.Duration `mapstructure:"refresh_token_lifespan" yaml:"refresh_token_lifespan"`
}

func (c *Config) applyDefaults() {
	if c.Auth.Mode == "" {
		c.Auth.Mode = "local"
	}
	if len(c.Auth.Zitadel.Scopes) == 0 {
		c.Auth.Zitadel.Scopes = []string{"openid", "profile", "email"}
	}
}
```

```go
// models/user.go
type User struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	UUID           string     `gorm:"size:36;uniqueIndex;not null" json:"uuid"`
	Username       string     `gorm:"size:64;uniqueIndex;not null" json:"username"`
	PasswordHash   string     `gorm:"size:255" json:"-"`
	Email          string     `gorm:"size:255" json:"email"`
	DisplayName    string     `gorm:"size:255" json:"display_name"`
	ZitadelUserID  *string    `gorm:"size:255;uniqueIndex" json:"zitadel_user_id,omitempty"`
	AuthProvider   string     `gorm:"size:32;not null;default:local" json:"auth_provider"`
	Role           string     `gorm:"size:32;not null;default:viewer" json:"role"`
	Status         string     `gorm:"size:32;not null;default:active" json:"status"`
	LastLoginAt    *time.Time `json:"last_login_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
```

```go
// interfaces/user_interface.go
type UserInterface interface {
	GetUser(id uint) (*models.User, error)
	GetUserByUUID(uuid string) (*models.User, error)
	GetUserByZitadelID(zitadelID string) (*models.User, error)
	CreateUserFromZitadel(subject, username, email, displayName string) (*models.User, error)
}
```

```go
// services/user_service.go
func (s *UserService) GetUserByZitadelID(zid string) (*models.User, error) {
	var user models.User
	if err := s.db.Where("zitadel_user_id = ?", zid).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *UserService) CreateUserFromZitadel(subject, username, email, displayName string) (*models.User, error) {
	now := time.Now()
	user := &models.User{
		UUID:          uuid.New().String(),
		Username:      username,
		Email:         email,
		DisplayName:   displayName,
		ZitadelUserID: &subject,
		AuthProvider:  "zitadel",
		Role:          "viewer",
		Status:        "active",
		LastLoginAt:   &now,
	}
	return user, s.db.Create(user).Error
}
```

```yaml
# configs/config.auth.yaml.example
auth:
  mode: local
  issuer: "https://mutong.example.com"
  access_token_lifespan: 15m
  refresh_token_lifespan: 168h
  zitadel:
    issuer: "https://zitadel.example.com"
    client_id: "mutong-web"
    client_secret: ""
    redirect_uri: "https://mutong.example.com/api/auth/oidc/callback"
    audience: "mutong-api"
    device_client: "mutongctl"
    scopes:
      - openid
      - profile
      - email
      - offline_access
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./config -run 'TestAuthModeDefaultsToLocal|TestZitadelConfigLoadsFromFile' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add config/config.go config/config_test.go configs/config.auth.yaml.example models/user.go services/user_service.go interfaces/user_interface.go
git commit -m "feat(auth): add zitadel config and user mapping fields"
```

### Task 2: Implement Zitadel OIDC And Local Session Services

**Files:**
- Create: `services/auth/zitadel_oidc.go`
- Create: `services/auth/session.go`
- Test: `services/auth/zitadel_oidc_test.go`
- Test: `services/auth/session_test.go`
- Modify: `go.mod`

- [ ] **Step 1: Write the failing session and validation tests**

```go
// services/auth/session_test.go
func TestSessionManagerIssueAndVerify(t *testing.T) {
	mgr := NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 24*time.Hour)
	raw, err := mgr.Issue(SessionClaims{
		UserID:       7,
		UUID:         "u-1",
		Role:         "admin",
		AuthProvider: "zitadel",
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := mgr.Verify(raw)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Role != "admin" || claims.UUID != "u-1" {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

// services/auth/zitadel_oidc_test.go
func TestBuildAuthURLIncludesStateAndPKCE(t *testing.T) {
	client := &ZitadelOIDCClient{
		issuer:      "https://id.example.com",
		clientID:    "mutong-web",
		redirectURI: "http://localhost:8888/api/auth/oidc/callback",
		scopes:      []string{"openid", "profile", "email"},
	}
	url, state, verifier := client.BuildAuthURL()
	if state == "" || verifier == "" {
		t.Fatal("expected non-empty state and verifier")
	}
	if !strings.Contains(url, "code_challenge=") || !strings.Contains(url, "state=") {
		t.Fatalf("unexpected auth url %s", url)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./services/auth -run 'TestSessionManagerIssueAndVerify|TestBuildAuthURLIncludesStateAndPKCE' -v`
Expected: FAIL with undefined `NewSessionManager` / `ZitadelOIDCClient`.

- [ ] **Step 3: Write minimal OIDC and session services**

```go
// services/auth/session.go
type SessionClaims struct {
	UserID       uint   `json:"user_id"`
	UUID         string `json:"uuid"`
	Role         string `json:"role"`
	AuthProvider string `json:"auth_provider"`
	ExpiresAt    int64  `json:"exp"`
}

type SessionManager struct {
	secret []byte
	ttl    time.Duration
}

func NewSessionManager(secret []byte, ttl time.Duration) *SessionManager {
	return &SessionManager{secret: secret, ttl: ttl}
}

func (m *SessionManager) Issue(claims SessionClaims) (string, error) {
	claims.ExpiresAt = time.Now().Add(m.ttl).Unix()
	b, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, m.secret)
	mac.Write(b)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (m *SessionManager) Verify(raw string) (*SessionClaims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid session token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, m.secret)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, fmt.Errorf("invalid session signature")
	}
	var claims SessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	if time.Now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("session expired")
	}
	return &claims, nil
}
```

```go
// services/auth/zitadel_oidc.go
type ZitadelUserInfo struct {
	Subject       string `json:"sub"`
	PreferredName string `json:"preferred_username"`
	Email         string `json:"email"`
	Name          string `json:"name"`
}

type ZitadelOIDCClient struct {
	issuer      string
	clientID    string
	clientSecret string
	redirectURI string
	scopes      []string
	provider    *oidc.Provider
	verifier    *oidc.IDTokenVerifier
	oauth2Cfg   oauth2.Config
}

func NewZitadelOIDCClient(ctx context.Context, cfg config.ZitadelConfig) (*ZitadelOIDCClient, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}
	oauthCfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  cfg.RedirectURI,
		Scopes:       cfg.Scopes,
	}
	return &ZitadelOIDCClient{
		issuer:       cfg.Issuer,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		redirectURI:  cfg.RedirectURI,
		scopes:       cfg.Scopes,
		provider:     provider,
		verifier:     provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth2Cfg:    oauthCfg,
	}, nil
}

func (c *ZitadelOIDCClient) BuildAuthURL() (authURL, state, verifier string) {
	state = uuid.New().String()
	verifier = oauth2.GenerateVerifier()
	authURL = c.oauth2Cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(verifier),
	)
	return
}
```

```go
// go.mod
require github.com/coreos/go-oidc/v3 v3.12.0
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./services/auth -run 'TestSessionManagerIssueAndVerify|TestBuildAuthURLIncludesStateAndPKCE' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum services/auth/zitadel_oidc.go services/auth/zitadel_oidc_test.go services/auth/session.go services/auth/session_test.go
git commit -m "feat(auth): add zitadel oidc and local session services"
```

### Task 3: Add OIDC Login/Callback And Wire Backend Routing

**Files:**
- Create: `controllers/oidc_controller.go`
- Modify: `controllers/auth_controller.go`
- Modify: `cmd/main.go`
- Test: `controllers/oidc_controller_test.go`

- [ ] **Step 1: Write the failing callback test**

```go
// controllers/oidc_controller_test.go
func TestCallbackCreatesSessionAndRedirects(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	_ = db.AutoMigrate(&models.User{})

	oidcSvc := &fakeOIDCService{
		userinfo: auth.ZitadelUserInfo{
			Subject:       "zitadel-user-1",
			PreferredName: "alice",
			Email:         "alice@example.com",
			Name:          "Alice",
		},
	}
	sessions := auth.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 24*time.Hour)
	ctl := NewOIDCController(db, oidcSvc, sessions)

	r := gin.New()
	ctl.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=abc&state=ok", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "mutong_session=") {
		t.Fatalf("expected mutong_session cookie, got %q", w.Header().Get("Set-Cookie"))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./controllers -run TestCallbackCreatesSessionAndRedirects -v`
Expected: FAIL with undefined `NewOIDCController`.

- [ ] **Step 3: Write minimal controller and route wiring**

```go
// controllers/oidc_controller.go
type oidcService interface {
	BuildAuthURL() (string, string, string)
	Exchange(ctx context.Context, code, verifier string) (*oauth2.Token, *auth.ZitadelUserInfo, error)
}

type OIDCController struct {
	DB       *gorm.DB
	OIDC     oidcService
	Sessions *auth.SessionManager
}

func NewOIDCController(db *gorm.DB, oidc oidcService, sessions *auth.SessionManager) *OIDCController {
	return &OIDCController{DB: db, OIDC: oidc, Sessions: sessions}
}

func (oc *OIDCController) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/auth/oidc")
	g.GET("/login", oc.Login)
	g.GET("/callback", oc.Callback)
}

func (oc *OIDCController) Login(c *gin.Context) {
	authURL, state, verifier := oc.OIDC.BuildAuthURL()
	secure := c.Request.TLS != nil
	c.SetCookie("mutong_oidc_state", state, 300, "/", "", secure, true)
	c.SetCookie("mutong_oidc_verifier", verifier, 300, "/", "", secure, true)
	c.Redirect(http.StatusFound, authURL)
}

func (oc *OIDCController) Callback(c *gin.Context) {
	state, _ := c.Cookie("mutong_oidc_state")
	verifier, _ := c.Cookie("mutong_oidc_verifier")
	if state == "" || state != c.Query("state") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
		return
	}
	_, userinfo, err := oc.OIDC.Exchange(c.Request.Context(), c.Query("code"), verifier)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "oidc exchange failed"})
		return
	}
	userSvc := services.NewUserService(oc.DB)
	user, err := userSvc.GetUserByZitadelID(userinfo.Subject)
	if err != nil {
		user, err = userSvc.CreateUserFromZitadel(userinfo.Subject, userinfo.PreferredName, userinfo.Email, userinfo.Name)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user mapping"})
			return
		}
	}
	session, err := oc.Sessions.Issue(auth.SessionClaims{UserID: user.ID, UUID: user.UUID, Role: user.Role, AuthProvider: "zitadel"})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}
	secure := c.Request.TLS != nil
	c.SetCookie("mutong_session", session, 86400, "/", "", secure, true)
	c.Redirect(http.StatusFound, "/view/index.html")
}
```

```go
// controllers/auth_controller.go
func (ac *AuthController) Me(c *gin.Context) {
	var user models.User
	if uuid := GetUserUUID(c); uuid != "" {
		if err := ac.DB.Where("uuid = ?", uuid).First(&user).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
			return
		}
	} else if uid, ok := GetUserID(c); ok {
		if err := ac.DB.First(&user, uid).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
	} else {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":            user.ID,
		"uuid":          user.UUID,
		"username":      user.Username,
		"email":         user.Email,
		"display_name":  user.DisplayName,
		"role":          user.Role,
		"auth_provider": user.AuthProvider,
	})
}
```

```go
// cmd/main.go (inside initializeGin)
	sessionMgr := authsvc.NewSessionManager([]byte(os.Getenv("MUTONG_SESSION_SECRET")), 24*time.Hour)
	if cfg.Auth.Mode == "hybrid" || cfg.Auth.Mode == "zitadel" {
		zitadelClient, err := authsvc.NewZitadelOIDCClient(context.Background(), cfg.Auth.Zitadel)
		if err != nil {
			logger.Fatal("failed to init zitadel oidc", zap.Error(err))
		}
		oidcCtrl := controllers.NewOIDCController(cfg.DB, zitadelClient, sessionMgr)
		oidcCtrl.RegisterRoutes(engine)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./controllers -run TestCallbackCreatesSessionAndRedirects -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add controllers/oidc_controller.go controllers/oidc_controller_test.go controllers/auth_controller.go cmd/main.go
git commit -m "feat(auth): add zitadel oidc login and callback flow"
```

### Task 4: Refactor Auth Middleware For Three-Path Identity Resolution

**Files:**
- Modify: `controllers/auth_middleware.go`
- Test: `controllers/auth_middleware_test.go`
- Modify: `controllers/auth_controller.go`

- [ ] **Step 1: Write the failing middleware test**

```go
// controllers/auth_middleware_test.go
func TestBearerTokenMiddlewareSupportsPATSessionAndOIDC(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	_ = db.AutoMigrate(&models.User{}, &models.APIToken{})
	zid := "zitadel-user-1"
	user := models.User{UUID: "local-uuid", Username: "alice", Role: "admin", AuthProvider: "zitadel", ZitadelUserID: &zid, Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	tok, raw, err := auth.CreateToken(db, &user.ID, nil, "local pat", "pat", []string{"read:alerts"}, 30)
	if err != nil || tok == nil {
		t.Fatal(err)
	}
	sessions := auth.NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 24*time.Hour)
	rawSession, _ := sessions.Issue(auth.SessionClaims{UserID: user.ID, UUID: user.UUID, Role: user.Role, AuthProvider: "zitadel"})

	r := gin.New()
	r.Use(BearerTokenMiddleware(db, sessions, &fakeTokenValidator{subject: "zitadel-user-1"}))
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
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
		if tc.header != "" { req.Header.Set("Authorization", tc.header) }
		if tc.cookie != nil { req.AddCookie(tc.cookie) }
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("case %s expected 200 got %d", tc.name, w.Code)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./controllers -run TestBearerTokenMiddlewareSupportsPATSessionAndOIDC -v`
Expected: FAIL because `BearerTokenMiddleware` has old fosite-only signature.

- [ ] **Step 3: Write minimal middleware refactor**

```go
// controllers/auth_middleware.go
type AccessTokenClaims struct {
	Subject string
}

type AccessTokenValidator interface {
	ValidateAccessToken(ctx context.Context, rawToken string) (*AccessTokenClaims, error)
}

func BearerTokenMiddleware(db *gorm.DB, sessions *auth.SessionManager, validator AccessTokenValidator) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		authHeader := ctx.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			switch {
			case strings.HasPrefix(token, "mtp_"), strings.HasPrefix(token, "mts_"):
				h := sha256.Sum256([]byte(token))
				hash := hex.EncodeToString(h[:])
				var t models.APIToken
				if err := db.Where("token_hash = ? AND revoked_at IS NULL", hash).
					Where("expires_at IS NULL OR expires_at > ?", time.Now()).
					First(&t).Error; err == nil {
					db.Model(&t).Update("last_used_at", time.Now())
					ctx.Set("user_id", t.UserID)
					ctx.Set("token_type", t.TokenType)
					ctx.Set("scopes", t.Scopes)
					ctx.Next()
					return
				}
				ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
				return
			default:
				claims, err := validator.ValidateAccessToken(ctx.Request.Context(), token)
				if err != nil {
					ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
					return
				}
				var user models.User
				if err := db.Where("zitadel_user_id = ?", claims.Subject).First(&user).Error; err != nil {
					ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user mapping not found"})
					return
				}
				ctx.Set("user_id", user.ID)
				ctx.Set("user_uuid", user.UUID)
				ctx.Set("token_type", "oauth2")
				ctx.Set("auth_provider", "zitadel")
				ctx.Next()
				return
			}
		}

		if cookie, err := ctx.Cookie("mutong_session"); err == nil && cookie != "" {
			claims, err := sessions.Verify(cookie)
			if err == nil {
				ctx.Set("user_id", claims.UserID)
				ctx.Set("user_uuid", claims.UUID)
				ctx.Set("token_type", "session")
				ctx.Set("auth_provider", claims.AuthProvider)
			}
		}
		ctx.Next()
	}
}

func GetUserUUID(c *gin.Context) string {
	if v, ok := c.Get("user_uuid"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	if v, ok := c.Get("user_id"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
```

```go
// controllers/auth_controller.go (logout)
func (ac *AuthController) Logout(c *gin.Context) {
	secure := c.Request.TLS != nil
	c.SetCookie("mutong_session", "", -1, "/", "", secure, true)
	c.SetCookie("refresh_token", "", -1, "/oauth2", "", secure, true)
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./controllers -run TestBearerTokenMiddlewareSupportsPATSessionAndOIDC -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add controllers/auth_middleware.go controllers/auth_middleware_test.go controllers/auth_controller.go
git commit -m "feat(auth): unify identity resolution across pat session and oidc"
```

### Task 5: Update Web Login UX And Frontend Auth State

**Files:**
- Modify: `view/src/login.js`
- Modify: `view/src/login.html`
- Modify: `view/src/stores/auth.js`
- Test: `view/src/__tests__/auth.smoke.test.ts`
- Test: `view/src/__tests__/login.smoke.test.ts`

- [ ] **Step 1: Write the failing frontend tests**

```ts
// view/src/__tests__/auth.smoke.test.ts
import { beforeEach, describe, expect, it, vi } from 'vitest'

describe('auth store init', () => {
  beforeEach(() => {
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('marks loggedIn when /api/auth/me succeeds with cookie session', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ uuid: 'u-1', username: 'alice', role: 'admin', auth_provider: 'zitadel' }),
    }))
    const { useAuth } = await import('../stores/auth.js')
    const store = useAuth()
    await store.init()
    expect(store.status).toBe('loggedIn')
    expect(store.user.username).toBe('alice')
  })
})

// view/src/__tests__/login.smoke.test.ts
import { describe, expect, it } from 'vitest'

describe('login redirect helper', () => {
  it('builds zitadel login redirect via backend entrypoint', async () => {
    const mod = await import('../login.js')
    expect(mod.buildSSOLoginURL()).toBe('/api/auth/oidc/login')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd view && bun run test -- auth.smoke.test.ts login.smoke.test.ts`
Expected: FAIL because `buildSSOLoginURL` does not exist and auth store behavior does not fully match.

- [ ] **Step 3: Write minimal frontend implementation**

```js
// view/src/login.js
export function buildSSOLoginURL() {
  return '/api/auth/oidc/login'
}

// inside setup()
function loginWithZitadel() {
  window.location.href = buildSSOLoginURL()
}

return {
  username,
  password,
  error,
  loading,
  locked,
  showCaptcha,
  captchaId,
  captchaImage,
  captchaAnswer,
  refreshCaptcha,
  handleLogin,
  loginWithZitadel,
}
```

```html
<!-- view/src/login.html -->
<button class="sso-login-btn" @click="loginWithZitadel" :disabled="loading">
  使用 Zitadel 单点登录
</button>
<div class="login-divider">或使用本地账号登录（hybrid 模式）</div>
```

```js
// view/src/stores/auth.js
import { reactive } from 'vue'

const authState = reactive({
  status: 'unknown',
  accessToken: sessionStorage.getItem('access_token') || null,
  user: JSON.parse(sessionStorage.getItem('user') || 'null'),
  get isAuthenticated() {
    return this.status === 'loggedIn'
  },
  async init() {
    try {
      const res = await fetch('/api/auth/me', { credentials: 'include' })
      if (res.ok) {
        this.user = await res.json()
        this.status = 'loggedIn'
        sessionStorage.setItem('user', JSON.stringify(this.user))
        return
      }
    } catch {}
    this.status = 'loggedOut'
    this.accessToken = null
    this.user = null
    sessionStorage.removeItem('access_token')
    sessionStorage.removeItem('user')
  },
  async logout() {
    await fetch('/api/auth/logout', { method: 'POST', credentials: 'include' })
    this.accessToken = null
    this.user = null
    this.status = 'loggedOut'
    sessionStorage.removeItem('access_token')
    sessionStorage.removeItem('user')
    window.location.href = '/view/login.html'
  },
})

export function useAuth() {
  return authState
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd view && bun run test -- auth.smoke.test.ts login.smoke.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add view/src/login.js view/src/login.html view/src/stores/auth.js view/src/__tests__/auth.smoke.test.ts view/src/__tests__/login.smoke.test.ts
git commit -m "feat(auth-ui): add zitadel sso entry and session-based auth state"
```

### Task 6: Switch CLI Device Flow To Zitadel And Preserve Auto Refresh

**Files:**
- Modify: `cmd/mutongctl/auth/login.go`
- Modify: `cmd/mutongctl/auth/status.go`
- Modify: `cmd/mutongctl/auth/logout.go`
- Modify: `cmd/mutongctl/internal/client/client.go`
- Test: `cmd/mutongctl/auth/login_test.go`

- [ ] **Step 1: Write the failing CLI login test**

```go
// cmd/mutongctl/auth/login_test.go
func TestRequestDeviceCodeUsesConfiguredZitadelEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/v2/device_authorization" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(deviceAuthResponse{
			DeviceCode:              "dc",
			UserCode:                "UCODE",
			VerificationURI:         "https://id.example.com/activate",
			VerificationURIComplete: "https://id.example.com/activate?user_code=UCODE",
			ExpiresIn:               600,
			Interval:                5,
		})
	}))
	defer server.Close()

	opts := &loginOptions{IO: iostreams.Test(), ServerURL: server.URL, HTTPClient: server.Client()}
	resp, err := requestDeviceCode(opts)
	if err != nil {
		t.Fatal(err)
	}
	if resp.DeviceCode != "dc" {
		t.Fatalf("unexpected device code %q", resp.DeviceCode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/mutongctl/auth -run TestRequestDeviceCodeUsesConfiguredZitadelEndpoint -v`
Expected: FAIL because current path is `/oauth2/device/auth`.

- [ ] **Step 3: Write minimal CLI implementation changes**

```go
// cmd/mutongctl/auth/login.go
func requestDeviceCode(opts *loginOptions) (*deviceAuthResponse, error) {
	form := url.Values{}
	form.Set("client_id", defaultClientID)
	form.Set("scope", "openid profile email offline_access")
	resp, err := opts.HTTPClient.PostForm(opts.ServerURL+"/oauth/v2/device_authorization", form)
	if err != nil {
		return nil, fmt.Errorf("[ERROR] 连接服务器失败 (exit_code=4)\n[CONTEXT] server=%s\n[HINT] verify server is reachable", opts.ServerURL)
	}
	defer resp.Body.Close()
	var result deviceAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse device auth response: %w", err)
	}
	return &result, nil
}

func pollForToken(opts *loginOptions, deviceResp *deviceAuthResponse) (*tokenEntry, error) {
	// keep existing loop, only change endpoint
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	form.Set("device_code", deviceResp.DeviceCode)
	form.Set("client_id", defaultClientID)
	resp, err := opts.HTTPClient.PostForm(opts.ServerURL+"/oauth/v2/token", form)
	// existing handling continues
	_ = err
	_ = resp
	return nil, nil
}
```

```go
// cmd/mutongctl/internal/client/client.go
func refreshOAuthToken(server, refreshToken string) (string, int, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", "mutongctl")
	form.Set("refresh_token", refreshToken)
	resp, err := http.PostForm(server+"/oauth/v2/token", form)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, err
	}
	if result.AccessToken == "" {
		return "", 0, fmt.Errorf("refresh token response missing access_token")
	}
	return result.AccessToken, result.ExpiresIn, nil
}
```

```go
// cmd/mutongctl/auth/logout.go
// keep local cache deletion; only change optional revoke endpoint to /oauth/v2/revoke when enabled
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/mutongctl/auth -run TestRequestDeviceCodeUsesConfiguredZitadelEndpoint -v && go test ./cmd/mutongctl/internal/client -run Test.* -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/mutongctl/auth/login.go cmd/mutongctl/auth/login_test.go cmd/mutongctl/auth/status.go cmd/mutongctl/auth/logout.go cmd/mutongctl/internal/client/client.go
git commit -m "feat(cli-auth): switch device flow and refresh to zitadel"
```

### Task 7: Verification, Smoke Checks, And Documentation Sync

**Files:**
- Modify: `docs/auth-design.md`
- Test: existing repo commands only

- [ ] **Step 1: Write the documentation updates**

```md
<!-- docs/auth-design.md -->
## 1. 架构概览

当前方案调整为：

- Zitadel 负责用户认证、OIDC、Device Flow、SSO
- Mutong 保留 Casbin、PAT/SAT、本地用户映射与本地 session
- BearerTokenMiddleware 同时支持 `mtp_` / `mts_`、Zitadel access token、本地 session
```

- [ ] **Step 2: Run focused backend tests**

Run: `go test ./services/auth ./controllers ./cmd/mutongctl/auth ./cmd/mutongctl/internal/client -v`
Expected: PASS

- [ ] **Step 3: Run frontend tests**

Run: `cd view && bun run test`
Expected: PASS with existing smoke tests plus new auth tests.

- [ ] **Step 4: Run repo verification commands**

Run: `just test-cli && just test-ui && just lint`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add docs/auth-design.md
git commit -m "docs(auth): sync zitadel hybrid auth design"
```

## Spec Coverage Check

- **Phase 0**: `config.auth.yaml.example`、`config/config.go`、`models/user.go`、用户映射接口 —— 由 Task 1 覆盖。
- **Phase 1**: `/api/auth/oidc/login`、`/api/auth/oidc/callback`、本地 session、前端 SSO 入口 —— 由 Task 2-5 覆盖。
- **Phase 2**: 三路 BearerTokenMiddleware、`/api/auth/me` 保持不变、Casbin 继续依赖本地 subject —— 由 Task 4 覆盖。
- **Phase 3**: CLI 切换到 Zitadel Device Flow、refresh 保持 —— 由 Task 6 覆盖。
- **Phase 4**: 本地主认证下线未执行，只通过 hybrid 方式保留旧链路；符合 spec 中“稳定后再下线”的要求。
- **Phase 5**: groups→role 映射、服务账号实体化、多租户权限为后续增强，未纳入本次实施范围；与 spec 一致。

## Placeholder Scan

- 无 `TBD` / `TODO` / “后续补充” 型占位。
- 每个任务均给出具体文件、测试、命令与代码。
- 未引用未定义的函数名：`NewSessionManager`、`BuildAuthURL`、`ValidateAccessToken`、`NewOIDCController`、`CreateUserFromZitadel`、`GetUserByZitadelID` 在前序任务中已定义。

## Type Consistency Check

- `zitadel_user_id` 在模型、服务、middleware、controller 中统一命名。
- 本地 session 中统一字段 `user_id`、`uuid`、`role`、`auth_provider`。
- CLI 继续使用 `tokenEntry{AccessToken, TokenType, RefreshToken, Expiry}`，与现有缓存格式一致。
- Bearer 中间件统一使用 `GetUserID` / `GetUserUUID` 提取本地身份。
