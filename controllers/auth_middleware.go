package controllers

import (
	"crypto/sha256"
	"encoding/hex"
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

func AuthMiddleware(logger *zap.Logger) gin.HandlerFunc {
	webhookIPs := parseTrustedIPs(os.Getenv("MUTONG_WEBHOOK_TRUSTED_IPS"))

	return func(ctx *gin.Context) {
		path := ctx.Request.URL.Path

		if strings.HasPrefix(path, "/api/alert/webhook") {
			if len(webhookIPs) > 0 {
				remoteIP := ctx.ClientIP()
				if !isTrustedIP(remoteIP, webhookIPs) {
					logger.Warn("Webhook from untrusted IP",
						zap.String("remote_addr", remoteIP))
					ctx.AbortWithStatusJSON(403, gin.H{"error": "forbidden: untrusted source"})
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
func BearerTokenMiddleware(db *gorm.DB, sessions *auth.SessionManager, validator auth.AccessTokenValidator, oauth2Provider fosite.OAuth2Provider) gin.HandlerFunc {
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

				// Load user to get UUID
				if t.UserID != nil {
					var user models.User
					if err := db.First(&user, *t.UserID).Error; err == nil {
						ctx.Set("user_uuid", user.UUID)
					}
				}

				ctx.Set("user_id", t.UserID)
				ctx.Set("token_type", t.TokenType)
				ctx.Set("scopes", t.Scopes)
				ctx.Next()
				return
			}

			// Path 2: OIDC access token
			if validator != nil {
				claims, err := validator.ValidateAccessToken(ctx.Request.Context(), token)
				if err == nil {
					var user models.User
					if err := db.Where("zitadel_user_id = ?", claims.Subject).First(&user).Error; err == nil {
						ctx.Set("user_id", user.ID)
						ctx.Set("user_uuid", user.UUID)
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
					if err := db.Where("uuid = ? OR zitadel_user_id = ?", subject, subject).First(&user).Error; err == nil {
						ctx.Set("user_id", user.ID)
						ctx.Set("user_uuid", user.UUID)
						ctx.Set("token_type", "oauth2")
						ctx.Next()
						return
					}
				}
			}

			// No valid path matched for Bearer token
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		// Path 4: Session cookie (mutong_session)
		if sessions != nil {
			if cookie, err := ctx.Cookie("mutong_session"); err == nil && cookie != "" {
				claims, err := sessions.Verify(cookie)
				if err == nil {
					ctx.Set("user_id", claims.UserID)
					ctx.Set("user_uuid", claims.UUID)
					ctx.Set("token_type", "session")
				} else {
					// Legacy backward compatibility: plain UUID from local login
					var user models.User
					if err := db.Where("uuid = ?", cookie).First(&user).Error; err == nil {
						ctx.Set("user_id", user.ID)
						ctx.Set("user_uuid", user.UUID)
						ctx.Set("token_type", "session")
					}
				}
			}
		}
		ctx.Next()
	}
}

// RequireAuthMiddleware aborts with 401 if no user_id is set in context.
// Only applies to /api/ paths; non-API paths (/oauth2, /view, /metrics) pass through.
// Public API endpoints (login, captcha, webhook, external alerts) are whitelisted.
func RequireAuthMiddleware() gin.HandlerFunc {
	publicPaths := []string{
		"/api/auth/login",
		"/api/auth/captcha",
		"/api/auth/captcha/verify",
		"/api/auth/login-status",
		"/api/v1/alerts/webhook",
		"/api/v1/alerts/external/",
	}
	return func(ctx *gin.Context) {
		path := ctx.Request.URL.Path

		if !strings.HasPrefix(path, "/api/") {
			ctx.Next()
			return
		}

		for _, p := range publicPaths {
			if strings.HasPrefix(path, p) {
				ctx.Next()
				return
			}
		}

		if _, exists := ctx.Get("user_id"); !exists {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		ctx.Next()
	}
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
		// Fall through to "not found" if no DB lookup available
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
