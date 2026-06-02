package controllers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	SignatureHeader  = "X-Alertmanager-Signature"
	TimestampHeader  = "X-Alertmanager-Timestamp"
	MaxTimestampSkew = 300
)

type WebhookVerifier struct {
	secret    []byte
	logger    *zap.Logger
	enabled   bool
	skipPaths map[string]bool
}

func NewWebhookVerifier(logger *zap.Logger) *WebhookVerifier {
	secret := os.Getenv("MUTONG_WEBHOOK_SECRET")
	enabled := secret != ""

	if !enabled {
		logger.Warn("MUTONG_WEBHOOK_SECRET not set — webhook signature verification disabled")
	}

	return &WebhookVerifier{
		secret:  []byte(secret),
		logger:  logger,
		enabled: enabled,
		skipPaths: map[string]bool{
			"/view":    true,
			"/metrics": true,
			"/health":  true,
			"/favicon": true,
		},
	}
}

func (v *WebhookVerifier) VerifySignature(ctx *gin.Context) bool {
	if !v.enabled {
		return true
	}

	path := ctx.Request.URL.Path
	for skipPrefix := range v.skipPaths {
		if strings.HasPrefix(path, skipPrefix) {
			return true
		}
	}

	if !strings.HasPrefix(path, "/api/v1/alerts/webhook") {
		return true
	}

	signature := ctx.GetHeader(SignatureHeader)
	if signature == "" {
		v.logger.Warn("Webhook missing signature header",
			zap.String("path", path),
			zap.String("remote", ctx.ClientIP()))
		return false
	}

	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		v.logger.Error("Failed to read request body", zap.Error(err))
		return false
	}
	// 恢复 body 以便后续 handler 可以读取
	ctx.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	expectedSig := v.computeSignature(body)
	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		v.logger.Warn("Webhook signature verification failed",
			zap.String("expected", expectedSig),
			zap.String("received", signature),
			zap.String("remote", ctx.ClientIP()))
		return false
	}

	return true
}

func (v *WebhookVerifier) computeSignature(body []byte) string {
	h := hmac.New(sha256.New, v.secret)
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func WebhookSignatureMiddleware(logger *zap.Logger) gin.HandlerFunc {
	verifier := NewWebhookVerifier(logger)

	return func(ctx *gin.Context) {
		if !verifier.VerifySignature(ctx) {
			ctx.AbortWithStatusJSON(401, gin.H{
				"error": "invalid webhook signature",
			})
			return
		}
		ctx.Next()
	}
}

func GenerateWebhookSignature(secret string, body []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
