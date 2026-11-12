package controllers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/services/auth"
)

// stubMetricsQuerier 返回固定错误，用于校验控制器不回显内部错误细节
type stubMetricsQuerier struct {
	err      error
	lastExpr string
}

func (s *stubMetricsQuerier) GetNodeMetrics(_ context.Context, _ string) ([]interfaces.MetricDataPoint, error) {
	return nil, s.err
}
func (s *stubMetricsQuerier) GetPodMetrics(_ context.Context, _, _ string) ([]interfaces.MetricDataPoint, error) {
	return nil, s.err
}
func (s *stubMetricsQuerier) GetDeploymentMetrics(_ context.Context, _, _ string) ([]interfaces.MetricDataPoint, error) {
	return nil, s.err
}
func (s *stubMetricsQuerier) GetStatefulSetMetrics(_ context.Context, _, _ string) ([]interfaces.MetricDataPoint, error) {
	return nil, s.err
}
func (s *stubMetricsQuerier) GetDaemonSetMetrics(_ context.Context, _, _ string) ([]interfaces.MetricDataPoint, error) {
	return nil, s.err
}
func (s *stubMetricsQuerier) GetServiceMetrics(_ context.Context, _, _ string) ([]interfaces.MetricDataPoint, error) {
	return nil, s.err
}
func (s *stubMetricsQuerier) QueryMetricTimeseries(_ context.Context, expr string, _, _, _ int64) ([]interfaces.TimeSeriesDataPoint, error) {
	s.lastExpr = expr
	return nil, s.err
}
func (s *stubMetricsQuerier) GetMetricCatalog(_ context.Context, _, _ bool) ([]interfaces.MetricCatalogEntry, error) {
	return nil, s.err
}
func (s *stubMetricsQuerier) CheckHealth(_ context.Context) error { return s.err }

func newMonitoringTestEngine(t *testing.T, querier interfaces.MetricsQuerier, role string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		if role != "" {
			c.Set("role", role)
		}
	})
	NewMonitoringController(zap.NewNop(), querier).RegisterRoutes(engine)
	return engine
}

func doMonitoringRequest(engine *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

// SEC-03：任意 expr 的 timeseries 端点只允许 admin
func TestMonitoringTimeseriesRequiresAdmin(t *testing.T) {
	prev := auth.GetRBACMode()
	auth.SetRBACMode(auth.RBACEnforce)
	defer auth.SetRBACMode(prev)

	for _, tc := range []struct {
		role string
		want int
	}{
		{"admin", http.StatusOK},
		{"operator", http.StatusForbidden},
		{"viewer", http.StatusForbidden},
		{"", http.StatusForbidden},
	} {
		engine := newMonitoringTestEngine(t, &stubMetricsQuerier{}, tc.role)
		w := doMonitoringRequest(engine, "/api/v1/monitoring/timeseries?expr=up")
		if w.Code != tc.want {
			t.Errorf("role=%q: expected status %d, got %d (%s)", tc.role, tc.want, w.Code, w.Body.String())
		}
	}
}

// FUNC-02：Prometheus 未配置（nil querier）时不得 panic，统一 503
func TestMonitoringNilQuerierReturnsUnavailable(t *testing.T) {
	prev := auth.GetRBACMode()
	auth.SetRBACMode(auth.RBACEnforce)
	defer auth.SetRBACMode(prev)

	engine := newMonitoringTestEngine(t, nil, "admin")
	for _, path := range []string{
		"/api/v1/monitoring/metrics/Pod?namespace=default&name=pod1",
		"/api/v1/monitoring/timeseries?expr=up",
		"/api/v1/monitoring/catalog",
		"/api/v1/monitoring/health",
	} {
		w := doMonitoringRequest(engine, path)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: expected 503, got %d (%s)", path, w.Code, w.Body.String())
		}
	}
}

// SEC-17：内部错误细节不得回显给客户端
func TestMonitoringDoesNotLeakInternalError(t *testing.T) {
	prev := auth.GetRBACMode()
	auth.SetRBACMode(auth.RBACEnforce)
	defer auth.SetRBACMode(prev)

	secret := "dial tcp 10.1.2.3:9090: connection refused"
	engine := newMonitoringTestEngine(t, &stubMetricsQuerier{err: errors.New(secret)}, "admin")
	for _, path := range []string{
		"/api/v1/monitoring/metrics/Pod?namespace=default&name=pod1",
		"/api/v1/monitoring/timeseries?expr=up",
		"/api/v1/monitoring/catalog",
		"/api/v1/monitoring/health",
	} {
		w := doMonitoringRequest(engine, path)
		if strings.Contains(w.Body.String(), "10.1.2.3") {
			t.Errorf("%s: internal error leaked to client: %s", path, w.Body.String())
		}
	}
}
