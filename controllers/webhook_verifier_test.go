package controllers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func webhookContext(body []byte, headers map[string]string) *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/alerts/webhook", bytes.NewReader(body))
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	return c
}

func TestWebhookVerifier_RejectsWhenSecretMissing(t *testing.T) {
	t.Setenv("MUTONG_WEBHOOK_SECRET", "")
	t.Setenv("MUTONG_WEBHOOK_ALLOW_UNSIGNED", "")
	v := NewWebhookVerifier(zap.NewNop())

	if v.VerifySignature(webhookContext([]byte(`{}`), nil)) {
		t.Fatal("unsigned webhook must be rejected when no secret is configured")
	}
}

func TestWebhookVerifier_AcceptsValidSignature(t *testing.T) {
	t.Setenv("MUTONG_WEBHOOK_SECRET", "test-secret")
	t.Setenv("MUTONG_WEBHOOK_ALLOW_UNSIGNED", "")
	v := NewWebhookVerifier(zap.NewNop())

	body := []byte(`{"status":"firing"}`)
	headers := map[string]string{
		SignatureHeader: GenerateWebhookSignature("test-secret", body),
		TimestampHeader: strconv.FormatInt(time.Now().Unix(), 10),
	}
	if !v.VerifySignature(webhookContext(body, headers)) {
		t.Fatal("valid signature must be accepted")
	}
}

func TestWebhookVerifier_RejectsMissingSignatureAndStaleTimestamp(t *testing.T) {
	t.Setenv("MUTONG_WEBHOOK_SECRET", "test-secret")
	v := NewWebhookVerifier(zap.NewNop())
	body := []byte(`{"status":"firing"}`)

	if v.VerifySignature(webhookContext(body, nil)) {
		t.Fatal("missing signature must be rejected")
	}

	stale := map[string]string{
		SignatureHeader: GenerateWebhookSignature("test-secret", body),
		TimestampHeader: strconv.FormatInt(time.Now().Add(-2*MaxTimestampSkew*time.Second).Unix(), 10),
	}
	if v.VerifySignature(webhookContext(body, stale)) {
		t.Fatal("stale timestamp must be rejected")
	}
}

func TestWebhookVerifier_RejectsReplay(t *testing.T) {
	t.Setenv("MUTONG_WEBHOOK_SECRET", "test-secret")
	v := NewWebhookVerifier(zap.NewNop())

	body := []byte(`{"status":"firing"}`)
	headers := map[string]string{
		SignatureHeader: GenerateWebhookSignature("test-secret", body),
		TimestampHeader: strconv.FormatInt(time.Now().Unix(), 10),
	}
	if !v.VerifySignature(webhookContext(body, headers)) {
		t.Fatal("first delivery must be accepted")
	}
	if v.VerifySignature(webhookContext(body, headers)) {
		t.Fatal("replayed signature must be rejected")
	}
}

func TestWebhookVerifier_RejectsExternalWithoutTokenOrAllowlist(t *testing.T) {
	t.Setenv("MUTONG_EXTERNAL_ALERT_TOKEN", "")
	t.Setenv("MUTONG_WEBHOOK_TRUSTED_IPS", "")
	v := NewWebhookVerifier(zap.NewNop())

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/alerts/external/ceph", bytes.NewReader([]byte(`{}`)))

	if v.VerifySignature(c) {
		t.Fatal("external alerts must be rejected when no token or allowlist is configured")
	}
}

func TestWebhookVerifier_ExternalToken(t *testing.T) {
	t.Setenv("MUTONG_EXTERNAL_ALERT_TOKEN", "external-secret")
	v := NewWebhookVerifier(zap.NewNop())

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/alerts/external/ceph", bytes.NewReader([]byte(`{}`)))
	c.Request.Header.Set(ExternalToken, "external-secret")

	if !v.VerifySignature(c) {
		t.Fatal("matching external token must be accepted")
	}
}

func TestGenerateWebhookSignature(t *testing.T) {
	body := []byte(`{"status":"firing"}`)
	sig := GenerateWebhookSignature("test-secret", body)
	if sig == "" {
		t.Fatal("expected non-empty signature")
	}
	if sig != GenerateWebhookSignature("test-secret", body) {
		t.Fatal("signature must be deterministic")
	}
	if sig == GenerateWebhookSignature("other-secret", body) {
		t.Fatal("different secrets must produce different signatures")
	}
	if sig == GenerateWebhookSignature("test-secret", []byte(`{"status":"resolved"}`)) {
		t.Fatal("different bodies must produce different signatures")
	}
}
