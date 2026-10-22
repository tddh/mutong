package controllers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	SignatureHeader  = "X-Alertmanager-Signature"
	TimestampHeader  = "X-Alertmanager-Timestamp"
	ExternalToken    = "X-Alert-Token"
	MaxTimestampSkew = 300
)

type WebhookVerifier struct {
	secret        []byte
	externalToken string
	logger        *zap.Logger
	enabled       bool
	allowUnsigned bool

	mu       sync.Mutex
	seenSigs map[string]time.Time
}

func NewWebhookVerifier(logger *zap.Logger) *WebhookVerifier {
	secret := os.Getenv("MUTONG_WEBHOOK_SECRET")
	externalToken := os.Getenv("MUTONG_EXTERNAL_ALERT_TOKEN")
	allowUnsigned := os.Getenv("MUTONG_WEBHOOK_ALLOW_UNSIGNED") == "true"
	enabled := secret != ""

	if !enabled {
		if allowUnsigned {
			logger.Warn("MUTONG_WEBHOOK_SECRET not set — unsigned webhooks are explicitly allowed")
		} else {
			logger.Warn("MUTONG_WEBHOOK_SECRET not set — alert webhooks will be rejected")
		}
	}

	return &WebhookVerifier{
		secret:        []byte(secret),
		externalToken: externalToken,
		logger:        logger,
		enabled:       enabled,
		allowUnsigned: allowUnsigned,
		seenSigs:      make(map[string]time.Time),
	}
}

// VerifySignature validates alert intake requests. Non-alert paths are ignored.
func (v *WebhookVerifier) VerifySignature(ctx *gin.Context) bool {
	path := ctx.Request.URL.Path

	if strings.HasPrefix(path, "/api/v1/alerts/external/") {
		return v.verifyExternal(ctx)
	}
	if !strings.HasPrefix(path, "/api/v1/alerts/webhook") {
		return true
	}

	if !v.enabled {
		return v.allowUnsigned
	}

	signature := ctx.GetHeader(SignatureHeader)
	if signature == "" {
		v.logger.Warn("Webhook missing signature header",
			zap.String("path", path), zap.String("remote", ctx.ClientIP()))
		return false
	}

	tsRaw := ctx.GetHeader(TimestampHeader)
	if tsRaw == "" {
		v.logger.Warn("Webhook missing timestamp header",
			zap.String("path", path), zap.String("remote", ctx.ClientIP()))
		return false
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(tsRaw), 10, 64)
	if err != nil {
		v.logger.Warn("Webhook timestamp is not a Unix timestamp",
			zap.String("timestamp", tsRaw), zap.String("remote", ctx.ClientIP()))
		return false
	}
	skew := time.Since(time.Unix(ts, 0))
	if skew < 0 {
		skew = -skew
	}
	if skew > MaxTimestampSkew*time.Second {
		v.logger.Warn("Webhook timestamp outside allowed skew",
			zap.Duration("skew", skew), zap.String("remote", ctx.ClientIP()))
		return false
	}

	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		v.logger.Error("Failed to read request body", zap.Error(err))
		return false
	}
	// Restore body so the handler can read it.
	ctx.Request.Body = io.NopCloser(bytes.NewBuffer(body))

	expected := v.computeSignature(body)
	if !hmac.Equal([]byte(strings.ToLower(signature)), []byte(expected)) {
		v.logger.Warn("Webhook signature verification failed",
			zap.String("remote", ctx.ClientIP()))
		return false
	}

	if v.isReplay(signature) {
		v.logger.Warn("Webhook signature replayed", zap.String("remote", ctx.ClientIP()))
		return false
	}

	return true
}

// verifyExternal protects /api/v1/alerts/external/:source. A per-source token
// is preferred; when it is not configured, the request must at least come from
// the configured trusted network (enforced by AuthMiddleware) or, if neither is
// configured, the request is rejected.
func (v *WebhookVerifier) verifyExternal(ctx *gin.Context) bool {
	if v.externalToken != "" {
		got := ctx.GetHeader(ExternalToken)
		if subtle.ConstantTimeCompare([]byte(got), []byte(v.externalToken)) == 1 {
			return true
		}
		v.logger.Warn("External alert token invalid", zap.String("remote", ctx.ClientIP()))
		return false
	}

	if len(parseTrustedIPs(os.Getenv("MUTONG_WEBHOOK_TRUSTED_IPS"))) > 0 {
		// The IP allowlist is enforced by AuthMiddleware.
		return true
	}

	v.logger.Warn("External alert rejected: neither MUTONG_EXTERNAL_ALERT_TOKEN nor MUTONG_WEBHOOK_TRUSTED_IPS is configured",
		zap.String("remote", ctx.ClientIP()))
	return false
}

// isReplay returns true when the same signature was accepted within the
// timestamp window. Expired entries are pruned opportunistically.
func (v *WebhookVerifier) isReplay(signature string) bool {
	now := time.Now()
	v.mu.Lock()
	defer v.mu.Unlock()

	for sig, seen := range v.seenSigs {
		if now.Sub(seen) > MaxTimestampSkew*time.Second {
			delete(v.seenSigs, sig)
		}
	}
	if _, ok := v.seenSigs[signature]; ok {
		return true
	}
	v.seenSigs[signature] = now
	return false
}

func (v *WebhookVerifier) computeSignature(body []byte) string {
	h := hmac.New(sha256.New, v.secret)
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// WebhookSignatureMiddleware verifies signatures and replay protection for
// alert intake endpoints.
func WebhookSignatureMiddleware(logger *zap.Logger) gin.HandlerFunc {
	verifier := NewWebhookVerifier(logger)

	return func(ctx *gin.Context) {
		if !verifier.VerifySignature(ctx) {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
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
