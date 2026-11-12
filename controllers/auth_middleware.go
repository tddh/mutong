package controllers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"gitee.com/tddh/mutong/models"
	"gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
	"github.com/ory/fosite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AuthMiddleware enforces the trusted-IP allowlist for alert intake.
// It is intentionally narrow: signature verification lives in WebhookVerifier.
func AuthMiddleware(logger *zap.Logger) gin.HandlerFunc {
	webhookIPs := parseTrustedIPs(os.Getenv("MUTONG_WEBHOOK_TRUSTED_IPS"))

	return func(ctx *gin.Context) {
		path := ctx.Request.URL.Path
		if strings.HasPrefix(path, "/api/v1/alerts/webhook") || strings.HasPrefix(path, "/api/v1/alerts/external/") {
			if len(webhookIPs) > 0 {
				remoteIP := ctx.ClientIP()
				if !isTrustedIP(remoteIP, webhookIPs) {
					logger.Warn("Webhook from untrusted IP",
						zap.String("remote_addr", remoteIP))
					ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden: untrusted source"})
					return
				}
			}
		}

		ctx.Next()
	}
}

func parseTrustedIPs(env string) []*net.IPNet {
	if env == "" {
		return nil
	}
	var networks []*net.IPNet
	for _, cidr := range strings.Split(env, ",") {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			ip := net.ParseIP(cidr)
			if ip == nil {
				continue
			}
			ipNet = &net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}
			if ip.To4() == nil {
				ipNet.Mask = net.CIDRMask(128, 128)
			}
		}
		networks = append(networks, ipNet)
	}
	return networks
}

func isTrustedIP(ip string, networks []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range networks {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

// BearerTokenMiddleware validates tokens from four identity paths:
//  1. PAT/SAT (local API tokens with mts_/mtp_ prefix) — resolved via DB.
//  2. OIDC access token — validated via AccessTokenValidator, then local user
//     is resolved by zitadel_user_id (subject).
//  3. Local OAuth2 access token — validated via fosite.OAuth2Provider introspection.
//  4. Session cookie (mutong_session) — verified via SessionManager.
//
// A cookie value is never trusted as a user identifier: only HMAC-verified
// session tokens are accepted.
func BearerTokenMiddleware(db *gorm.DB, sessions *auth.SessionManager, validator auth.AccessTokenValidator, oauth2Provider fosite.OAuth2Provider) gin.HandlerFunc {
	guard := auth.NewSessionGuard(db, auth.DefaultSessionGuardTTL)
	return func(ctx *gin.Context) {
		authHeader := ctx.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")

			// Path 1: Local PAT / SAT
			if strings.HasPrefix(token, "mtp_") || strings.HasPrefix(token, "mts_") {
				h := sha256.Sum256([]byte(token))
				hash := hex.EncodeToString(h[:])
				var t models.APIToken
				if err := db.Where("token_hash = ? AND revoked_at IS NULL", hash).
					Where("expires_at IS NULL OR expires_at > ?", time.Now()).
					First(&t).Error; err != nil {
					ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
					return
				}
				db.Model(&t).Update("last_used_at", time.Now())

				// Enforce the owning identity's lifecycle: a token whose user or
				// service account is missing, disabled or soft-deleted must not
				// authenticate. Never continue with an empty user_uuid.
				if t.UserID != nil {
					var user models.User
					if err := db.First(&user, *t.UserID).Error; err != nil || user.Status != "active" {
						ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
						return
					}
					ctx.Set("user_uuid", user.UUID)
					ctx.Set("role", user.Role)
					ctx.Set("username", user.Username)
				}
				if t.ServiceAccountID != nil {
					var sa models.ServiceAccount
					if err := db.First(&sa, *t.ServiceAccountID).Error; err != nil || sa.Status != "active" {
						ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
						return
					}
				}

				ctx.Set("user_id", t.UserID)
				ctx.Set("token_type", t.TokenType)
				ctx.Set("scopes", t.Scopes)
				if !tokenScopesAllowMethod(t.Scopes, ctx.Request.Method) {
					ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "token scope is read-only"})
					return
				}
				ctx.Next()
				return
			}

			// Path 2: OIDC access token
			if validator != nil {
				claims, err := validator.ValidateAccessToken(ctx.Request.Context(), token)
				if err == nil {
					var user models.User
					if err := db.Where("zitadel_user_id = ?", claims.Subject).First(&user).Error; err == nil && user.Status == "active" {
						ctx.Set("user_id", user.ID)
						ctx.Set("user_uuid", user.UUID)
						ctx.Set("role", user.Role)
						ctx.Set("username", user.Username)
						ctx.Set("token_type", "oidc")
						ctx.Next()
						return
					}
				}
			}

			// Path 3: Local OAuth2 access token (fosite introspection)
			if oauth2Provider != nil {
				_, ar, err := oauth2Provider.IntrospectToken(
					ctx.Request.Context(),
					token,
					fosite.AccessToken,
					new(fosite.DefaultSession),
				)
				if err == nil && ar != nil {
					subject := ar.GetSession().GetSubject()
					var user models.User
					if err := db.Where("uuid = ? OR zitadel_user_id = ?", subject, subject).First(&user).Error; err == nil && user.Status == "active" {
						ctx.Set("user_id", user.ID)
						ctx.Set("user_uuid", user.UUID)
						ctx.Set("role", user.Role)
						ctx.Set("username", user.Username)
						ctx.Set("token_type", "oauth2")
						ctx.Next()
						return
					}
				}
			}

			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		// Path 4: Session cookie (mutong_session). Signature verification is
		// mandatory; the legacy "cookie is a UUID" fallback was removed because
		// it allowed session forgery with a publicly readable UUID. The guard
		// additionally revokes sessions whose user was disabled/deleted or whose
		// role/password changed since issue.
		if sessions != nil {
			if cookie, err := ctx.Cookie("mutong_session"); err == nil && cookie != "" {
				claims, err := sessions.Verify(cookie)
				if err == nil && guard.Validate(claims) == nil {
					ctx.Set("user_id", claims.UserID)
					ctx.Set("user_uuid", claims.UUID)
					ctx.Set("role", claims.Role)
					ctx.Set("token_type", "session")
				}
			}
		}
		ctx.Next()
	}
}

// tokenScopesAllowMethod applies the minimal PAT/SAT scope semantics: a token
// whose scopes are all read-only ("read" or "read:*") may not perform write
// methods. Empty or unparseable scopes grant full access for backward
// compatibility. Read methods are always allowed.
func tokenScopesAllowMethod(scopesJSON, method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	var scopes []string
	if err := json.Unmarshal([]byte(scopesJSON), &scopes); err != nil || len(scopes) == 0 {
		return true
	}
	for _, s := range scopes {
		if s != "read" && !strings.HasPrefix(s, "read:") {
			return true // a non-read scope grants write access
		}
	}
	return false // every scope is read-only
}

// RequireAuthMiddleware applies deny-by-default authentication to every route
// except explicitly public endpoints. It does not rely on a path prefix: a new
// route is protected unless it is listed here.
func RequireAuthMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		path := ctx.Request.URL.Path

		if isPublicPath(path) {
			ctx.Next()
			return
		}

		if _, exists := ctx.Get("user_id"); !exists {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		ctx.Next()
	}
}

// isPublicPath lists the only endpoints that may be reached without a verified
// identity. Keep this list minimal and exact where possible.
func isPublicPath(path string) bool {
	// Exact public paths.
	switch path {
	case "/", "/login", "/oidc/login", "/device", "/healthz", "/api/auth/login", "/api/auth/login-status":
		return true
	}

	// Prefixes for protocol endpoints that authenticate their own requests or
	// serve assets required before login.
	for _, prefix := range []string{
		"/api/auth/captcha",
		"/api/auth/oidc/",
		"/api/v1/alerts/webhook",
		"/api/v1/alerts/external/",
		"/oauth2/",
		"/view/",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	// Metrics are public only when explicitly requested for a Prometheus
	// scraper; otherwise they require authentication.
	if path == "/metrics" && os.Getenv("MUTONG_METRICS_PUBLIC") == "true" {
		return true
	}

	return false
}

// ViewAuthGuard redirects unauthenticated users to the login page.
// Public paths (/view/login.html, /view/consent.html, /view/callback) are exempt.
func ViewAuthGuard() gin.HandlerFunc {
	publicPaths := map[string]bool{
		"/view/login.html":   true,
		"/view/consent.html": true,
		"/view/callback":     true,
		"/device":            true,
	}
	return func(ctx *gin.Context) {
		path := ctx.Request.URL.Path
		if !strings.HasPrefix(path, "/view/") {
			ctx.Next()
			return
		}
		if publicPaths[path] || strings.HasPrefix(path, "/view/callback") {
			ctx.Next()
			return
		}
		if _, exists := ctx.Get("user_id"); !exists {
			ctx.Redirect(http.StatusFound, "/view/login.html?redirect="+ctx.Request.URL.Path)
			ctx.Abort()
			return
		}
		ctx.Next()
	}
}

// GetUserID extracts the user ID from gin context, handling both string (OAuth2 UUID)
// and *uint (PAT/SAT token) types. Returns the numeric user ID and true if found.
func GetUserID(c *gin.Context) (uint, bool) {
	v, exists := c.Get("user_id")
	if !exists {
		return 0, false
	}
	switch id := v.(type) {
	case *uint:
		if id == nil {
			return 0, false
		}
		return *id, true
	case uint:
		return id, true
	case string:
		// OAuth2 JWT: user_id is a UUID string — need to look up numeric ID
		return 0, false
	default:
		return 0, false
	}
}

// GetUserUUID extracts the user UUID string from context. It prefers the
// "user_uuid" key (set by session/OIDC paths) and falls back to string-typed
// "user_id" (legacy OAuth2 or direct cookie assignment).
func GetUserUUID(c *gin.Context) string {
	if v, exists := c.Get("user_uuid"); exists {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	v, exists := c.Get("user_id")
	if !exists {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// GetUserRole returns the authenticated user's role, if known.
func GetUserRole(c *gin.Context) string {
	if v, exists := c.Get("role"); exists {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
