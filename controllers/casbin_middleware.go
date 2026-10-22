package controllers

import (
	"fmt"
	"net/http"
	"strings"

	auth "gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// actionForRequest maps an HTTP request to the policy action vocabulary.
// Destructive surfaces are treated as execute even when they use GET (the
// terminal WebSocket is the notable case).
func actionForRequest(ctx *gin.Context) string {
	path := ctx.Request.URL.Path
	switch {
	case strings.HasPrefix(path, "/api/v1/terminal/ws"),
		strings.HasPrefix(path, "/api/v1/executor/"),
		strings.HasPrefix(path, "/api/v1/diagnosis/mcp/tool/"):
		return "execute"
	}

	switch ctx.Request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return "read"
	default:
		return "write"
	}
}

// CasbinMiddleware enforces role-based access control. In shadow mode it only
// logs denials, which allows a safe rollout; enforce mode aborts with 403.
func CasbinMiddleware(logger *zap.Logger) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if auth.GetRBACMode() == auth.RBACOff {
			ctx.Next()
			return
		}

		uid := GetUserUUID(ctx)
		if uid == "" {
			// RequireAuthMiddleware guarantees an identity for non-public paths;
			// if we get here on a public path there is nothing to enforce.
			ctx.Next()
			return
		}

		action := actionForRequest(ctx)
		allowed, err := auth.Enforce(uid, ctx.Request.URL.Path, action)
		if err == nil && allowed {
			ctx.Next()
			return
		}

		if logger != nil {
			logger.Warn("RBAC denied",
				zap.String("user", uid),
				zap.String("path", ctx.Request.URL.Path),
				zap.String("action", action),
				zap.String("mode", auth.GetRBACMode()),
				zap.Error(err))
		}

		if auth.GetRBACMode() == auth.RBACShadow {
			ctx.Next()
			return
		}

		ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied"})
	}
}

// RequireRole aborts unless the authenticated user has one of the given roles.
// It complements Casbin for surfaces where a role check is clearer than a
// path-based policy.
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(ctx *gin.Context) {
		if auth.GetRBACMode() == auth.RBACOff {
			ctx.Next()
			return
		}
		role := GetUserRole(ctx)
		if !allowed[role] {
			ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": fmt.Sprintf("role %q is not permitted for this operation", role),
			})
			return
		}
		ctx.Next()
	}
}
