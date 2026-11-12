package controllers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestTokenBucketBurstAndRefill(t *testing.T) {
	tb := newTokenBucket(2, 20*time.Millisecond)
	defer tb.Stop()

	// Fresh bucket grants the full burst.
	if !tb.allow("1.1.1.1") || !tb.allow("1.1.1.1") {
		t.Fatal("first maxTokens requests must be allowed")
	}
	if tb.allow("1.1.1.1") {
		t.Fatal("request beyond the burst must be rejected")
	}
	// Buckets are per-IP.
	if !tb.allow("2.2.2.2") {
		t.Fatal("a different IP must have its own bucket")
	}
	// Rewind the refill clock by 1.5 intervals: exactly one token comes back.
	tb.mu.Lock()
	tb.buckets["1.1.1.1"].lastRefill = time.Now().Add(-30 * time.Millisecond)
	tb.mu.Unlock()
	if !tb.allow("1.1.1.1") {
		t.Fatal("refilled token must be granted")
	}
	if tb.allow("1.1.1.1") {
		t.Fatal("only one token should have refilled")
	}
}

func TestTokenBucketRefillCapsAtMax(t *testing.T) {
	tb := newTokenBucket(2, time.Millisecond)
	defer tb.Stop()

	if !tb.allow("ip") {
		t.Fatal("first request must pass")
	}
	// Long idle: refill must saturate at maxTokens, not grow unbounded.
	time.Sleep(30 * time.Millisecond)
	for i := 0; i < 2; i++ {
		if !tb.allow("ip") {
			t.Fatalf("request %d within the capped burst must pass", i+1)
		}
	}
	if tb.allow("ip") {
		t.Fatal("burst must stay capped at maxTokens after idle")
	}
}

func TestRateLimitMiddleware429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RateLimitMiddleware(2, time.Hour))
	r.GET("/limited", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	do := func(remote string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/limited", nil)
		req.RemoteAddr = remote
		r.ServeHTTP(w, req)
		return w
	}

	if w := do("10.0.0.1:1111"); w.Code != http.StatusOK {
		t.Fatalf("first request: got %d", w.Code)
	}
	if w := do("10.0.0.1:2222"); w.Code != http.StatusOK {
		t.Fatalf("second request: got %d", w.Code)
	}
	w := do("10.0.0.1:3333")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("third request must be rate limited, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("429 must be JSON, got content-type %q", ct)
	}
	if body := w.Body.String(); !strings.Contains(body, "too many requests") {
		t.Fatalf("unexpected 429 body: %s", body)
	}
	// Another client IP is unaffected.
	if w := do("10.0.0.2:1111"); w.Code != http.StatusOK {
		t.Fatalf("different IP must not be limited, got %d", w.Code)
	}
}
