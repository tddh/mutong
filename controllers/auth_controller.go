package controllers

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"gitee.com/tddh/mutong/models"
	"gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AuthController struct {
	DB            *gorm.DB
	PasswordSvc   *auth.PasswordService
	TokenSvc      *auth.TokenService
	Sessions      *auth.SessionManager
	LoginLimit    *auth.LoginLimiter
	OIDCAvailable bool
	AuthMode      string
}

// cookieSecure reports whether session cookies should carry the Secure flag.
// TLS may terminate at a reverse proxy, so X-Forwarded-Proto is honoured when
// MUTONG_COOKIE_SECURE is not set explicitly.
func cookieSecure(c *gin.Context) bool {
	if v := os.Getenv("MUTONG_COOKIE_SECURE"); v != "" {
		return v == "true"
	}
	if c.Request.TLS != nil {
		return true
	}
	return strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

// setSessionCookie writes the session cookie with an explicit SameSite policy
// and HttpOnly. maxAge < 0 deletes the cookie.
func setSessionCookie(c *gin.Context, value string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("mutong_session", value, maxAge, "/", "", cookieSecure(c), true)
}

func NewAuthController(db *gorm.DB, sessions *auth.SessionManager, loginLimit *auth.LoginLimiter) *AuthController {
	return &AuthController{
		DB:          db,
		PasswordSvc: auth.NewPasswordService(),
		TokenSvc:    auth.NewTokenService(),
		Sessions:    sessions,
		LoginLimit:  loginLimit,
	}
}

func (ac *AuthController) RegisterRoutes(r *gin.Engine) {
	auth := r.Group("/api/auth")
	auth.POST("/login", ac.Login)
	auth.GET("/me", ac.Me)
	auth.POST("/logout", ac.Logout)
	auth.POST("/tokens", ac.CreateToken)
	auth.GET("/tokens", ac.ListTokens)
	auth.DELETE("/tokens/:id", ac.RevokeToken)
	auth.GET("/login-status", ac.LoginStatus)
}

func (ac *AuthController) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	ip := c.ClientIP()

	if ac.LoginLimit != nil {
		if err := ac.LoginLimit.CheckIP(c.Request.Context(), ip); err != nil {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "请求过于频繁，请稍后再试"})
			return
		}
		if ac.LoginLimit.IsLocked(c.Request.Context(), req.Username) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "账号已被临时锁定，请15分钟后重试"})
			return
		}
	}

	var user models.User
	if err := ac.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		if ac.LoginLimit != nil {
			ac.LoginLimit.RecordFailure(c.Request.Context(), req.Username)
			ac.LoginLimit.RecordIPFailure(c.Request.Context(), ip)
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	if user.Status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "账号已被禁用"})
		return
	}
	if !ac.PasswordSvc.VerifyPassword(req.Password, user.PasswordHash) {
		if ac.LoginLimit != nil {
			ac.LoginLimit.RecordFailure(c.Request.Context(), req.Username)
			ac.LoginLimit.RecordIPFailure(c.Request.Context(), ip)
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	if ac.LoginLimit != nil {
		ac.LoginLimit.ClearFailures(c.Request.Context(), req.Username)
	}

	if ac.Sessions == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "session manager not configured; set MUTONG_SESSION_SECRET or auth.global_secret",
		})
		return
	}
	sessionToken, err := ac.Sessions.Issue(auth.SessionClaims{
		UserID:       user.ID,
		UUID:         user.UUID,
		Role:         user.Role,
		AuthProvider: "local",
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue session"})
		return
	}
	// Re-bind the role at login so policy changes take effect immediately.
	_ = auth.SyncUserRole(ac.DB, user.UUID)
	setSessionCookie(c, sessionToken, 86400)

	c.JSON(http.StatusOK, gin.H{
		"access_token": sessionToken,
		"user": gin.H{
			"id":       user.UUID,
			"username": user.Username,
			"role":     user.Role,
		},
	})
}

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

func (ac *AuthController) Logout(c *gin.Context) {
	setSessionCookie(c, "", -1)
	c.SetCookie("refresh_token", "", -1, "/oauth2", "", cookieSecure(c), true)
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

func (ac *AuthController) CreateToken(c *gin.Context) {
	uid, ok := GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	var req struct {
		Name          string   `json:"name"`
		Type          string   `json:"type"`
		Scopes        []string `json:"scopes"`
		ExpiresInDays int      `json:"expires_in_days"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.Type == "" {
		req.Type = "pat"
	}
	if req.ExpiresInDays <= 0 {
		req.ExpiresInDays = 90
	}

	u := uid
	token, raw, err := ac.TokenSvc.CreateToken(ac.DB, &u, nil, req.Name, req.Type, req.Scopes, req.ExpiresInDays)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create token"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"id":         token.ID,
		"name":       token.Name,
		"token":      raw,
		"prefix":     token.TokenPrefix,
		"type":       token.TokenType,
		"scopes":     req.Scopes,
		"expires_at": token.ExpiresAt,
	})
}

func (ac *AuthController) ListTokens(c *gin.Context) {
	uid, ok := GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	tokens, err := ac.TokenSvc.ListTokens(ac.DB, uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list tokens"})
		return
	}
	result := make([]gin.H, len(tokens))
	for i, t := range tokens {
		result[i] = gin.H{
			"id":         t.ID,
			"name":       t.Name,
			"prefix":     t.TokenPrefix,
			"type":       t.TokenType,
			"last_used":  t.LastUsedAt,
			"expires_at": t.ExpiresAt,
			"created_at": t.CreatedAt,
		}
	}
	c.JSON(http.StatusOK, result)
}

func (ac *AuthController) RevokeToken(c *gin.Context) {
	uid, ok := GetUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	tokenID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token id"})
		return
	}
	if err := ac.TokenSvc.RevokeToken(ac.DB, uint(tokenID), uid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "token revoked"})
}

func (ac *AuthController) CreateInitialAdmin(username, password string) error {
	hash, err := ac.PasswordSvc.HashPassword(password)
	if err != nil {
		return err
	}

	var existing models.User
	err = ac.DB.Where("username = ?", username).First(&existing).Error
	if err == nil {
		// Admin already exists. Never reset the password from an environment
		// variable on every startup: that turns a leaked env var into a
		// permanent, non-rotatable backdoor.
		return nil
	}

	return ac.DB.Create(&models.User{
		UUID:         uuid.New().String(),
		Username:     username,
		PasswordHash: hash,
		Email:        username + "@mutong.local",
		Role:         "admin",
		Status:       "active",
	}).Error
}

func (ac *AuthController) LoginStatus(c *gin.Context) {
	mode := os.Getenv("MUTONG_AUTH_MODE")
	if mode == "" && ac.AuthMode != "" {
		mode = ac.AuthMode
	}
	if mode == "" {
		mode = "local"
	}

	resp := gin.H{"mode": mode}
	if ac.OIDCAvailable {
		resp["provider_label"] = func() string {
			if l := os.Getenv("MUTONG_OIDC_LABEL"); l != "" {
				return l
			}
			return "SSO"
		}()
	}

	c.JSON(http.StatusOK, resp)
}
