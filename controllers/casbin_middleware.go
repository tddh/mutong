package controllers

import (
	"fmt"
	"net/http"

	auth "gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
)

func CasbinMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		userID, exists := ctx.Get("user_id")
		if !exists {
			ctx.Next()
			return
		}

		uid := ""
		switch v := userID.(type) {
		case string:
			uid = v
		case uint:
			uid = fmt.Sprintf("%d", v)
		case *uint:
			if v != nil {
				uid = fmt.Sprintf("%d", *v)
			}
		default:
			ctx.Next()
			return
		}

		if uid == "" {
			ctx.Next()
			return
		}

		allowed, err := auth.Enforce(uid, ctx.Request.URL.Path, ctx.Request.Method)
		if err != nil || !allowed {
			ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied"})
			return
		}
		ctx.Next()
	}
}
