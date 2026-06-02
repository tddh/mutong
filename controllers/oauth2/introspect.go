package oauth2

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ory/fosite"
)

type IntrospectHandler struct {
	OAuth2 fosite.OAuth2Provider
}

func (h *IntrospectHandler) HandleIntrospect(c *gin.Context) {
	ctx := c.Request.Context()
	ir, err := h.OAuth2.NewIntrospectionRequest(ctx, c.Request, &fosite.DefaultSession{})
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	h.OAuth2.WriteIntrospectionResponse(ctx, c.Writer, ir)
}
