package oauth2

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	oauth2model "gitee.com/tddh/mutong/models/oauth2"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DeviceHandler handles the Device Authorization Grant (RFC 8628) endpoints.
type DeviceHandler struct {
	DB *gorm.DB
}

// userCodeChars is the character set for user codes (excludes ambiguous chars like 0/O, 1/I/l).
const userCodeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func generateUserCode() (string, error) {
	code := make([]byte, 8)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(userCodeChars))))
		if err != nil {
			return "", err
		}
		code[i] = userCodeChars[n.Int64()]
	}
	return string(code), nil
}

func generateDeviceCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

// HandleDeviceAuth handles POST /oauth2/device/auth
func (h *DeviceHandler) HandleDeviceAuth(c *gin.Context) {
	clientID := c.PostForm("client_id")
	scope := c.PostForm("scope")

	if clientID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_id is required"})
		return
	}

	// Verify client exists
	var client oauth2model.OAuth2Client
	if err := h.DB.Where("client_id = ?", clientID).First(&client).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid client_id"})
		return
	}

	deviceCode, err := generateDeviceCode()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate device code"})
		return
	}

	userCode, err := generateUserCode()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate user code"})
		return
	}

	// Store device code
	dc := &oauth2model.DeviceCode{
		DeviceCode: deviceCode,
		UserCode:   userCode,
		ClientID:   clientID,
		Scope:      scope,
		Status:     "pending",
		ExpiresAt:  time.Now().Add(10 * time.Minute),
	}

	if err := h.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "device_code"}},
		DoUpdates: clause.AssignmentColumns([]string{"user_code", "client_id", "scope", "status", "expires_at"}),
	}).Create(dc).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store device code"})
		return
	}

	issuerURL := "http://localhost:8888"
	if origin := c.Request.Header.Get("Origin"); origin != "" {
		issuerURL = origin
	}

	c.JSON(http.StatusOK, gin.H{
		"device_code":               deviceCode,
		"user_code":                 userCode,
		"verification_uri":          issuerURL + "/device",
		"verification_uri_complete": issuerURL + "/device?code=" + userCode,
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

	deviceCode := c.PostForm("device_code")
	if deviceCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device_code is required"})
		return
	}

	var dc oauth2model.DeviceCode
	if err := h.DB.Where("device_code = ?", deviceCode).First(&dc).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_device_code", "error_description": "device code not found"})
		return
	}

	if time.Now().After(dc.ExpiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expired_device_code", "error_description": "device code has expired"})
		return
	}

	switch dc.Status {
	case "pending":
		c.JSON(http.StatusBadRequest, gin.H{"error": "authorization_pending"})
		return
	case "denied":
		c.JSON(http.StatusBadRequest, gin.H{"error": "access_denied"})
		return
	case "authorized":
		// Issue tokens
		accessToken := generateRandomToken()
		refreshToken := generateRandomToken()

		h.DB.Model(&dc).Update("status", "completed")

		c.JSON(http.StatusOK, gin.H{
			"access_token":  accessToken,
			"refresh_token": refreshToken,
			"token_type":    "Bearer",
			"expires_in":    900,
			"scope":         dc.Scope,
		})
	case "expired":
		fallthrough
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "expired_device_code"})
	}
}

// HandleDevicePage serves a simple page for users to enter their device code.
func (h *DeviceHandler) HandleDevicePage(c *gin.Context) {
	// Require authentication before approving device code
	if _, exists := c.Get("user_id"); !exists {
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

		var dc oauth2model.DeviceCode
		if err := h.DB.Where("user_code = ?", userCode).First(&dc).Error; err != nil {
			c.Data(http.StatusOK, "text/html; charset=utf-8", devicePageHTML("无效的设备代码", code, true))
			return
		}

		if action == "approve" {
			h.DB.Model(&dc).Updates(map[string]interface{}{
				"status": "authorized",
				"scope":  dc.Scope,
			})
			c.Data(http.StatusOK, "text/html; charset=utf-8", devicePageHTML("授权成功！您可以关闭此页面。", "", false))
		} else {
			h.DB.Model(&dc).Update("status", "denied")
			c.Data(http.StatusOK, "text/html; charset=utf-8", devicePageHTML("授权已拒绝。", "", false))
		}
		return
	}

	c.Data(http.StatusOK, "text/html; charset=utf-8", devicePageHTML("", code, false))
}

func generateRandomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
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
		}() + `;">` + message + `</div>`
	}
	codeValue := code
	if codeValue == "" {
		codeValue = ``
	}
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

func init() {
	// Register c.HTML to not panic — the device page is a simple inline template.
	// In production, load from view/dist/device.html or use an inline template.
}
