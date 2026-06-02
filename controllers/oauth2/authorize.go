package oauth2

import (
	"net/http"
	"net/url"

	"gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
	"github.com/ory/fosite"
)

type AuthorizeHandler struct {
	OAuth2 fosite.OAuth2Provider
}

func getUserIdentifier(c *gin.Context) string {
	if v, exists := c.Get("user_uuid"); exists {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	if v, exists := c.Get("user_id"); exists {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func (h *AuthorizeHandler) HandleAuthorize(c *gin.Context) {
	if c.Request.Method == http.MethodGet {
		// If user is already authenticated, proceed with authorization
		if userID := getUserIdentifier(c); userID != "" {
			h.handleAuthorizeResponse(c, userID)
			return
		}
		c.Redirect(http.StatusFound, "/view/login.html?redirect="+url.QueryEscape(c.Request.URL.String()))
		return
	}

	h.handleAuthorizeResponse(c, getUserIdentifier(c))
}

func (h *AuthorizeHandler) handleAuthorizeResponse(c *gin.Context, userID string) {
	ctx := c.Request.Context()
	ar, err := h.OAuth2.NewAuthorizeRequest(ctx, c.Request)
	if err != nil {
		h.OAuth2.WriteAuthorizeError(ctx, c.Writer, ar, err)
		return
	}

	session := auth.NewMutongSession(userID)
	resp, err := h.OAuth2.NewAuthorizeResponse(ctx, ar, session)
	if err != nil {
		h.OAuth2.WriteAuthorizeError(ctx, c.Writer, ar, err)
		return
	}
	h.OAuth2.WriteAuthorizeResponse(ctx, c.Writer, ar, resp)
}
