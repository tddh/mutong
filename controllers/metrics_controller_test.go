package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/tddh/mutong/interfaces"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// cannedMetricsQuerier implements interfaces.MetricsQuerier with canned results.
type cannedMetricsQuerier struct {
	points []interfaces.MetricDataPoint
	err    error
	calls  []string
}

func (s *cannedMetricsQuerier) record(name string) { s.calls = append(s.calls, name) }

func (s *cannedMetricsQuerier) GetNodeMetrics(context.Context, string) ([]interfaces.MetricDataPoint, error) {
	s.record("node")
	return s.points, s.err
}
func (s *cannedMetricsQuerier) GetPodMetrics(context.Context, string, string) ([]interfaces.MetricDataPoint, error) {
	s.record("pod")
	return s.points, s.err
}
func (s *cannedMetricsQuerier) GetDeploymentMetrics(context.Context, string, string) ([]interfaces.MetricDataPoint, error) {
	s.record("deployment")
	return s.points, s.err
}
func (s *cannedMetricsQuerier) GetStatefulSetMetrics(context.Context, string, string) ([]interfaces.MetricDataPoint, error) {
	s.record("statefulset")
	return s.points, s.err
}
func (s *cannedMetricsQuerier) GetDaemonSetMetrics(context.Context, string, string) ([]interfaces.MetricDataPoint, error) {
	s.record("daemonset")
	return s.points, s.err
}
func (s *cannedMetricsQuerier) GetServiceMetrics(context.Context, string, string) ([]interfaces.MetricDataPoint, error) {
	s.record("service")
	return s.points, s.err
}
func (s *cannedMetricsQuerier) QueryMetricTimeseries(context.Context, string, int64, int64, int64) ([]interfaces.TimeSeriesDataPoint, error) {
	return nil, nil
}
func (s *cannedMetricsQuerier) GetMetricCatalog(context.Context, bool, bool) ([]interfaces.MetricCatalogEntry, error) {
	return nil, nil
}
func (s *cannedMetricsQuerier) CheckHealth(context.Context) error { return nil }

func TestMetricsEndpointsWithoutQuerierReturn503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewMetricsController(zap.NewNop(), nil).RegisterRoutes(r)

	for _, path := range []string{
		"/api/v1/metrics/pod?namespace=ns&name=p",
		"/api/v1/metrics/node?name=n",
		"/api/v1/metrics/deployment?namespace=ns&name=d",
		"/api/v1/metrics/statefulset?namespace=ns&name=s",
		"/api/v1/metrics/daemonset?namespace=ns&name=d",
		"/api/v1/metrics/service?namespace=ns&name=s",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: expected 503 without Prometheus, got %d", path, w.Code)
		}
	}
}

func TestMetricsEndpointsValidationAndResults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	q := &cannedMetricsQuerier{points: []interfaces.MetricDataPoint{{MetricName: "cpu", Value: 0.42}}}
	r := gin.New()
	NewMetricsController(zap.NewNop(), q).RegisterRoutes(r)

	// Missing required params -> 400.
	for _, path := range []string{
		"/api/v1/metrics/pod", "/api/v1/metrics/pod?namespace=ns",
		"/api/v1/metrics/node", "/api/v1/metrics/deployment?name=d",
		"/api/v1/metrics/statefulset", "/api/v1/metrics/daemonset?namespace=ns",
		"/api/v1/metrics/service",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", path, w.Code)
		}
	}

	// Happy path returns the querier's points.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/metrics/pod?namespace=ns&name=p", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("pod metrics: got %d: %s", w.Code, w.Body.String())
	}
	var points []interfaces.MetricDataPoint
	if err := json.Unmarshal(w.Body.Bytes(), &points); err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].MetricName != "cpu" || points[0].Value != 0.42 {
		t.Fatalf("unexpected points: %+v", points)
	}

	// Querier failure -> 500 with a generic message (no internal detail).
	q.err = errors.New("prometheus dial tcp 10.0.0.1:9090: secret detail")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/metrics/node?name=n", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("node metrics error: got %d", w.Code)
	}
	if body := w.Body.String(); !json.Valid([]byte(body)) || strings.Contains(body, "secret detail") {
		t.Fatalf("internal error detail leaked: %s", body)
	}
}
