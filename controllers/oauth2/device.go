package oauth2

import (
	"errors"
	"html"
	"net/http"
	"net/url"
	"strings"

	authsvc "gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// DeviceHandler handles the Device Authorization Grant (RFC 8628) endpoints.
// Security-critical state transitions live in authsvc.DeviceFlowService.
type DeviceHandler struct {
	DB  *gorm.DB
	svc *authsvc.DeviceFlowService
}

func NewDeviceHandler(db *gorm.DB) *DeviceHandler {
	return &DeviceHandler{DB: db, svc: authsvc.NewDeviceFlowService(db, "")}
}

// deviceUserID extracts the verified identity set by the auth middleware,
// which stores uint for sessions and *uint for PAT/SAT paths.
func deviceUserID(c *gin.Context) (uint, bool) {
	v, ok := c.Get("user_id")
	if !ok {
		return 0, false
	}
	switch id := v.(type) {
	case uint:
		return id, id != 0
	case *uint:
		if id != nil {
			return *id, *id != 0
		}
	}
	return 0, false
}

// HandleDeviceAuth handles POST /oauth2/device/auth
func (h *DeviceHandler) HandleDeviceAuth(c *gin.Context) {
	authz, err := h.svc.Start(c.Request.Context(), c.PostForm("client_id"), c.PostForm("scope"))
	if err != nil {
		switch {
		case errors.Is(err, authsvc.ErrDeviceClientRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "client_id is required"})
		case errors.Is(err, authsvc.ErrDeviceInvalidClient):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid client_id"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start device authorization"})
		}
		return
	}

	uri, complete := h.svc.VerificationURIs(authz.UserCode)
	c.JSON(http.StatusOK, gin.H{
		"device_code":               authz.DeviceCode,
		"user_code":                 authz.UserCode,
		"verification_uri":          uri,
		"verification_uri_complete": complete,
		"expires_in":                600,
		"interval":                  5,
	})
}

// HandleDeviceToken handles the device token exchange in the token endpoint.
// This is called by the client during polling with grant_type=urn:ietf:params:oauth:grant-type:device_code
func (h *DeviceHandler) HandleDeviceToken(c *gin.Context) {
	grantType := c.PostForm("grant_type")
	if grantType != "urn:ietf:params:oauth:grant-type:device_code" {
		c.Next() // Not a device token request, let other handlers process
		return
	}

	authz, err := h.svc.Exchange(c.Request.Context(), c.PostForm("device_code"), c.PostForm("client_id"))
	if err != nil {
		switch {
		case errors.Is(err, authsvc.ErrDeviceNotFound):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_device_code", "error_description": "device code not found"})
		case errors.Is(err, authsvc.ErrDeviceExpired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "expired_device_code", "error_description": "device code has expired"})
		case errors.Is(err, authsvc.ErrDevicePending):
			c.JSON(http.StatusBadRequest, gin.H{"error": "authorization_pending"})
		case errors.Is(err, authsvc.ErrDeviceDenied):
			c.JSON(http.StatusBadRequest, gin.H{"error": "access_denied"})
		case errors.Is(err, authsvc.ErrDeviceClientMismatch):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_client", "error_description": "client_id does not match device authorization"})
		case errors.Is(err, authsvc.ErrDeviceUsed):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant", "error_description": "device code already used"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		}
		return
	}

	if authz.UserID == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	// Issue a real persisted PAT bound to the approving user and granted scope,
	// so it passes the same validation/revocation path as every other token.
	_, raw, err := authsvc.CreateToken(h.DB, authz.UserID, nil, "device authorization", "pat", strings.Fields(authz.Scope), 1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token": raw,
		"token_type":   "Bearer",
		"expires_in":   86400,
		"scope":        authz.Scope,
	})
}

// HandleDevicePage serves a simple page for users to enter their device code.
func (h *DeviceHandler) HandleDevicePage(c *gin.Context) {
	userID, authed := deviceUserID(c)
	if !authed {
		c.Redirect(http.StatusFound, "/view/login.html?redirect="+url.QueryEscape("/device?"+c.Request.URL.RawQuery))
		c.Abort()
		return
	}

	code := c.Query("code")
	if c.Request.Method == http.MethodPost {
		userCode := strings.ToUpper(strings.TrimSpace(c.PostForm("code")))
		action := c.PostForm("action")

		if userCode == "" {
			c.Data(http.StatusOK, "text/html; charset=utf-8", devicePageHTML("请输入设备代码", code, true))
			return
		}

		var err error
		if action == "approve" {
			err = h.svc.Approve(c.Request.Context(), userCode, userID)
		} else {
			err = h.svc.Deny(c.Request.Context(), userCode, userID)
		}
		if err != nil {
			msg := "无效的设备代码"
			switch {
			case errors.Is(err, authsvc.ErrDeviceExpired):
				msg = "设备代码已过期"
			case errors.Is(err, authsvc.ErrDeviceUsed):
				msg = "该设备代码已被处理"
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", devicePageHTML(msg, code, true))
			return
		}

		if action == "approve" {
			c.Data(http.StatusOK, "text/html; charset=utf-8", devicePageHTML("授权成功！您可以关闭此页面。", "", false))
		} else {
			c.Data(http.StatusOK, "text/html; charset=utf-8", devicePageHTML("授权已拒绝。", "", false))
		}
		return
	}

	c.Data(http.StatusOK, "text/html; charset=utf-8", devicePageHTML("", code, false))
}

func devicePageHTML(message string, code string, isError bool) []byte {
	var msgHTML string
	if message != "" {
		msgHTML = `<div style="padding:12px;margin-bottom:16px;border-radius:4px;` +
			`background:` + func() string {
			if isError {
				return "#fff2e8;color:#d4380d;border:1px solid #ffd591"
			}
			return "#f6ffed;color:#389e0d;border:1px solid #b7eb8f"
		}() + `;">` + html.EscapeString(message) + `</div>`
	}
	// The code is attacker-controllable via the query string and is embedded
	// in an HTML attribute, so escape it (quotes included).
	codeValue := html.EscapeString(code)
	return []byte(`<!DOCTYPE html>
<html lang="zh-CN">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>设备授权 - 重明平台</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f0f2f5;min-height:100vh;display:flex;align-items:center;justify-content:center}
.card{width:400px;background:#fff;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,0.08);padding:32px}
.card h2{font-size:20px;color:#333;margin-bottom:8px;text-align:center}
.card p{font-size:14px;color:#666;text-align:center;margin-bottom:20px}
.card input{width:100%;height:40px;padding:0 12px;border:1px solid #e8e8e8;border-radius:4px;font-size:16px;text-align:center;letter-spacing:4px;outline:none;margin-bottom:16px}
.card input:focus{border-color:#1890ff}
.btn{width:100%;height:40px;background:#1890ff;color:#fff;border:none;border-radius:4px;font-size:15px;font-weight:500;cursor:pointer}
.btn:hover{background:#40a9ff}
.btn-deny{margin-top:8px;background:#fff;color:#666;border:1px solid #e8e8e8}
</style></head>
<body><div class="card">
<h2>&#x1F3D4;&#xFE0F; 重明设备授权</h2>
<p>请输入您在终端中看到的设备代码</p>
` + msgHTML + `
<form method="post">
<input type="text" name="code" placeholder="输入8位代码" value="` + codeValue + `" maxlength="8" autofocus>
<button type="submit" name="action" value="approve" class="btn">授  权</button>
<button type="submit" name="action" value="deny" class="btn btn-deny">拒  绝</button>
</form>
</div></body></html>`)
}

// RegisterDeviceRoutes registers the device authorization endpoints.
func (h *DeviceHandler) RegisterDeviceRoutes(r *gin.Engine) {
	r.POST("/oauth2/device/auth", h.HandleDeviceAuth)
	r.POST("/oauth/v2/device_authorization", h.HandleDeviceAuth)
	r.GET("/device", h.HandleDevicePage)
	r.POST("/device", h.HandleDevicePage)
}
