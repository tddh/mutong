package controllers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

// signedHeaders builds a valid signature/timestamp pair for the given body.
func signedHeaders(secret string, body []byte, ts int64) map[string]string {
	timestamp := strconv.FormatInt(ts, 10)
	return map[string]string{
		SignatureHeader: GenerateWebhookSignature(secret, timestamp, body),
		TimestampHeader: timestamp,
	}
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
	if !v.VerifySignature(webhookContext(body, signedHeaders("test-secret", body, time.Now().Unix()))) {
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

	stale := signedHeaders("test-secret", body, time.Now().Add(-2*MaxTimestampSkew*time.Second).Unix())
	if v.VerifySignature(webhookContext(body, stale)) {
		t.Fatal("stale timestamp must be rejected")
	}
}

func TestWebhookVerifier_RejectsReplay(t *testing.T) {
	t.Setenv("MUTONG_WEBHOOK_SECRET", "test-secret")
	v := NewWebhookVerifier(zap.NewNop())

	body := []byte(`{"status":"firing"}`)
	headers := signedHeaders("test-secret", body, time.Now().Unix())
	if !v.VerifySignature(webhookContext(body, headers)) {
		t.Fatal("first delivery must be accepted")
	}
	if v.VerifySignature(webhookContext(body, headers)) {
		t.Fatal("replayed signature must be rejected")
	}
}

// TestWebhookVerifier_TimestampBoundSignature covers SEC-01: the signature must
// cover the timestamp, so a captured signature cannot be reused with a fresh
// timestamp to escape the replay cache and the skew window.
func TestWebhookVerifier_TimestampBoundSignature(t *testing.T) {
	t.Setenv("MUTONG_WEBHOOK_SECRET", "test-secret")
	v := NewWebhookVerifier(zap.NewNop())
	body := []byte(`{"status":"firing"}`)

	ts1 := time.Now().Unix()
	good := signedHeaders("test-secret", body, ts1)
	if !v.VerifySignature(webhookContext(body, good)) {
		t.Fatal("first delivery must be accepted")
	}

	// Same body, new timestamp, but the OLD signature: must be rejected because
	// the signature no longer matches the timestamp it is presented with.
	ts2 := ts1 + 1
	replayed := map[string]string{
		SignatureHeader: good[SignatureHeader],
		TimestampHeader: strconv.FormatInt(ts2, 10),
	}
	if v.VerifySignature(webhookContext(body, replayed)) {
		t.Fatal("old signature with a changed timestamp must be rejected")
	}

	// A correctly re-signed delivery with the new timestamp is accepted, proving
	// the rejection above is due to the timestamp binding, not the replay cache.
	if !v.VerifySignature(webhookContext(body, signedHeaders("test-secret", body, ts2))) {
		t.Fatal("re-signed delivery with a fresh timestamp must be accepted")
	}
}

// TestWebhookVerifier_RejectsOversizedBody covers SEC-05: a body over the 1MB
// cap must be rejected with 413 before it is fully buffered.
func TestWebhookVerifier_RejectsOversizedBody(t *testing.T) {
	t.Setenv("MUTONG_WEBHOOK_SECRET", "test-secret")
	v := NewWebhookVerifier(zap.NewNop())

	body := bytes.Repeat([]byte("a"), MaxWebhookBodyBytes+1)
	c := webhookContext(body, signedHeaders("test-secret", body, time.Now().Unix()))
	if v.VerifySignature(c) {
		t.Fatal("oversized body must be rejected")
	}
	if !c.IsAborted() {
		t.Fatal("oversized body must abort the request")
	}
	if c.Writer.Status() != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", c.Writer.Status())
	}
}

func TestWebhookVerifier_AcceptsBodyAtLimit(t *testing.T) {
	t.Setenv("MUTONG_WEBHOOK_SECRET", "test-secret")
	v := NewWebhookVerifier(zap.NewNop())

	body := bytes.Repeat([]byte("a"), MaxWebhookBodyBytes)
	if !v.VerifySignature(webhookContext(body, signedHeaders("test-secret", body, time.Now().Unix()))) {
		t.Fatal("body exactly at the limit must be accepted")
	}
}

// TestWebhookSignatureMiddleware_OversizedBodyReturns413 verifies the middleware
// surfaces the 413 status rather than masking it with a 401.
func TestWebhookSignatureMiddleware_OversizedBodyReturns413(t *testing.T) {
	t.Setenv("MUTONG_WEBHOOK_SECRET", "test-secret")
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(WebhookSignatureMiddleware(zap.NewNop()))
	r.POST("/api/v1/alerts/webhook", func(c *gin.Context) { c.Status(http.StatusOK) })

	body := bytes.Repeat([]byte("a"), MaxWebhookBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/webhook", bytes.NewReader(body))
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set(SignatureHeader, GenerateWebhookSignature("test-secret", ts, body))
	req.Header.Set(TimestampHeader, ts)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body.String())
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
	ts := "1700000000"
	sig := GenerateWebhookSignature("test-secret", ts, body)
	if sig == "" {
		t.Fatal("expected non-empty signature")
	}
	if sig != GenerateWebhookSignature("test-secret", ts, body) {
		t.Fatal("signature must be deterministic")
	}
	if sig == GenerateWebhookSignature("other-secret", ts, body) {
		t.Fatal("different secrets must produce different signatures")
	}
	if sig == GenerateWebhookSignature("test-secret", ts, []byte(`{"status":"resolved"}`)) {
		t.Fatal("different bodies must produce different signatures")
	}
	if sig == GenerateWebhookSignature("test-secret", "1700000001", body) {
		t.Fatal("different timestamps must produce different signatures")
	}
	// The signature must bind timestamp and body with a newline separator, not
	// a bare concatenation that could be ambiguous.
	if strings.Contains(sig, "\n") {
		t.Fatal("signature must be hex encoded")
	}
}
