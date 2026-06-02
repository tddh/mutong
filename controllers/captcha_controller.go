package controllers

import (
	"net/http"

	"gitee.com/tddh/mutong/services/auth"
	"github.com/gin-gonic/gin"
)

type CaptchaController struct {
	CaptchaSvc *auth.CaptchaService
	LoginLimit *auth.LoginLimiter
}

func NewCaptchaController(captchaSvc *auth.CaptchaService, loginLimit *auth.LoginLimiter) *CaptchaController {
	return &CaptchaController{CaptchaSvc: captchaSvc, LoginLimit: loginLimit}
}

func (cc *CaptchaController) RegisterRoutes(r *gin.Engine) {
	r.GET("/api/auth/captcha", cc.Generate)
	r.POST("/api/auth/captcha/verify", cc.Verify)
	r.POST("/api/auth/login-status", cc.LoginStatus)
}

func (cc *CaptchaController) Generate(c *gin.Context) {
	data, err := cc.CaptchaSvc.Generate(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "captcha generation failed"})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (cc *CaptchaController) Verify(c *gin.Context) {
	var req struct {
		CaptchaID string `json:"captcha_id"`
		Dots      string `json:"dots"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	valid := cc.CaptchaSvc.Verify(c.Request.Context(), req.CaptchaID, req.Dots)
	c.JSON(http.StatusOK, gin.H{"valid": valid})
}

func (cc *CaptchaController) LoginStatus(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
	}
	_ = c.ShouldBindJSON(&req)

	ip := c.ClientIP()
	failCount, requireCaptcha, locked := cc.LoginLimit.GetStatus(c.Request.Context(), ip, req.Username)

	c.JSON(http.StatusOK, gin.H{
		"fail_count":      failCount,
		"require_captcha": requireCaptcha,
		"locked":          locked,
	})
}
