package controllers

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestParseTrustedIPs(t *testing.T) {
	nets := parseTrustedIPs("10.0.0.0/8, 192.168.1.5, 2001:db8::1, garbage, ,fe80::/10")
	if len(nets) != 4 {
		t.Fatalf("expected 4 valid networks, got %d: %v", len(nets), nets)
	}
	if !isTrustedIP("10.1.2.3", nets) {
		t.Fatal("CIDR member must be trusted")
	}
	if !isTrustedIP("192.168.1.5", nets) {
		t.Fatal("bare IPv4 must be trusted as /32")
	}
	if isTrustedIP("192.168.1.6", nets) {
		t.Fatal("bare IPv4 must not widen to a subnet")
	}
	if !isTrustedIP("2001:db8::1", nets) {
		t.Fatal("bare IPv6 must be trusted as /128")
	}
	if !isTrustedIP("fe80::1234", nets) {
		t.Fatal("IPv6 CIDR member must be trusted")
	}
	if isTrustedIP("not-an-ip", nets) {
		t.Fatal("unparseable IP must never be trusted")
	}
	if parseTrustedIPs("") != nil {
		t.Fatal("empty env must yield no networks")
	}
}

func TestAuthMiddlewareWebhookIPAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("MUTONG_WEBHOOK_TRUSTED_IPS", "10.0.0.0/8")

	build := func() *gin.Engine {
		r := gin.New()
		r.Use(AuthMiddleware(zap.NewNop()))
		r.POST("/api/v1/alerts/webhook", func(c *gin.Context) { c.String(http.StatusOK, "intake") })
		r.POST("/api/v1/alerts/external/ceph", func(c *gin.Context) { c.String(http.StatusOK, "external") })
		r.GET("/api/v1/other", func(c *gin.Context) { c.String(http.StatusOK, "other") })
		return r
	}

	tests := []struct {
		name   string
		path   string
		remote string
		want   int
	}{
		{"trusted webhook", "/api/v1/alerts/webhook", "10.1.2.3:40000", http.StatusOK},
		{"untrusted webhook", "/api/v1/alerts/webhook", "203.0.113.9:40000", http.StatusForbidden},
		{"untrusted external", "/api/v1/alerts/external/ceph", "203.0.113.9:40000", http.StatusForbidden},
		{"trusted external", "/api/v1/alerts/external/ceph", "10.9.9.9:40000", http.StatusOK},
		{"non-webhook unaffected", "/api/v1/other", "203.0.113.9:40000", http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			if tc.path == "/api/v1/other" {
				req = httptest.NewRequest(http.MethodGet, tc.path, nil)
			}
			req.RemoteAddr = tc.remote
			build().ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, w.Code, w.Body.String())
			}
		})
	}
}

func TestAuthMiddlewareNoAllowlistConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("MUTONG_WEBHOOK_TRUSTED_IPS", "")
	r := gin.New()
	r.Use(AuthMiddleware(zap.NewNop()))
	r.POST("/api/v1/alerts/webhook", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/webhook", nil)
	req.RemoteAddr = "203.0.113.9:40000"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("without an allowlist the IP gate must stay open (signature verification still applies), got %d", w.Code)
	}
}

func TestIsTrustedIPInvalidInput(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("127.0.0.0/8")
	if isTrustedIP("", []*net.IPNet{cidr}) {
		t.Fatal("empty IP must not be trusted")
	}
	if isTrustedIP("127.0.0.1", nil) {
		t.Fatal("no networks means nothing is trusted")
	}
}
