package oauth2

import (
	"gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
	"github.com/ory/fosite"
)

type TokenHandler struct {
	OAuth2  fosite.OAuth2Provider
	DeviceH *DeviceHandler
}

func (h *TokenHandler) HandleToken(c *gin.Context) {
	ctx := c.Request.Context()

	if c.PostForm("grant_type") == "urn:ietf:params:oauth:grant-type:device_code" {
		if h.DeviceH != nil {
			h.DeviceH.HandleDeviceToken(c)
		}
		c.Abort()
		return
	}

	accessReq, err := h.OAuth2.NewAccessRequest(ctx, c.Request, auth.NewMutongSession(""))
	if err != nil {
		h.OAuth2.WriteAccessError(ctx, c.Writer, accessReq, err)
		return
	}

	accessResp, err := h.OAuth2.NewAccessResponse(ctx, accessReq)
	if err != nil {
		h.OAuth2.WriteAccessError(ctx, c.Writer, accessReq, err)
		return
	}

	if accessResp.GetExtra("refresh_token") != nil {
		secure := c.Request.TLS != nil
		domain := ""
		c.SetCookie("refresh_token", accessResp.GetExtra("refresh_token").(string),
			604800, "/oauth2", domain, secure, true)
		c.SetCookie("mutong_session", "1",
			86400, "/", domain, secure, false)
	}

	h.OAuth2.WriteAccessResponse(ctx, c.Writer, accessReq, accessResp)
}
