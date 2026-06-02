package prometheus_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gitee.com/tddh/mutong/services/prometheus"
	"go.uber.org/zap"
)

func TestQueryServiceInit(t *testing.T) {
	logger := zap.NewNop()
	cfg := prometheus.PrometheusConfig{
		Enabled:       true,
		URL:           "http://test:9090",
		Timeout:       5,
		MetricMapping: nil,
	}
	svc := prometheus.NewQueryService(logger, cfg)
	if svc == nil {
		t.Error("QueryService init failed")
	}
}

func TestQueryRange(t *testing.T) {
	// Mock Prometheus range query endpoint
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "matrix",
				"result": []map[string]interface{}{
					{
						"metric": map[string]interface{}{"__name__": "demo_metric", "node": "node1"},
						"values": [][]interface{}{
							{float64(1620000000), "1.23"},
							{float64(1620000600), "4.56"},
						},
					},
				},
			},
		}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer ts.Close()

	logger := zap.NewNop()
	cfg := prometheus.PrometheusConfig{URL: ts.URL, Timeout: 5, MetricMapping: nil}
	svc := prometheus.NewQueryService(logger, cfg)

	ctx := context.Background()
	results, err := svc.QueryRange(ctx, "demo_metric", time.Unix(1620000000, 0), time.Unix(1620003600, 0), time.Minute)
	if err != nil {
		t.Fatalf("QueryRange error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	rr := results[0]
	if rr.MetricName != "demo_metric" {
		t.Fatalf("unexpected metric name: %s", rr.MetricName)
	}
	if len(rr.Values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(rr.Values))
	}
	if !rr.Values[0].Timestamp.Equal(time.Unix(1620000000, 0)) || rr.Values[0].Value != 1.23 {
		t.Fatalf("unexpected first data point: %+v", rr.Values[0])
	}
}

func TestQueryRange_EmptyBaseURL(t *testing.T) {
	logger := zap.NewNop()
	cfg := prometheus.PrometheusConfig{URL: "", Timeout: 5, MetricMapping: nil}
	svc := prometheus.NewQueryService(logger, cfg)
	ctx := context.Background()
	res, err := svc.QueryRange(ctx, "expr", time.Now(), time.Now(), time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("expected empty result when baseURL is empty, got %d", len(res))
	}
}

func TestMetricsEndpoints(t *testing.T) {
	// Common test server for Node/Pod/Deployment metrics
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return a single metric value for any query
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "vector",
				"result": []map[string]interface{}{
					{
						"metric": map[string]interface{}{"__name__": "metric_sample"},
						"value":  []interface{}{float64(1620000000), "0.5"},
					},
				},
			},
		}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer ts.Close()

	logger := zap.NewNop()
	cfg := prometheus.PrometheusConfig{URL: ts.URL, Timeout: 5, MetricMapping: nil}
	svc := prometheus.NewQueryService(logger, cfg)

	ctx := context.Background()
	// Node metrics
	nodeRes, err := svc.GetNodeMetrics(ctx, "node1")
	if err != nil {
		t.Fatalf("GetNodeMetrics error: %v", err)
	}
	if len(nodeRes) != 5 {
		t.Fatalf("expected 5 node metrics, got %d", len(nodeRes))
	}
	// Pod metrics
	podRes, err := svc.GetPodMetrics(ctx, "default", "pod1")
	if err != nil {
		t.Fatalf("GetPodMetrics error: %v", err)
	}
	if len(podRes) != 6 {
		t.Fatalf("expected 6 pod metrics, got %d", len(podRes))
	}
	// Deployment metrics (updated: Phase 2 added CPU/Memory utilization metrics)
	depRes, err := svc.GetDeploymentMetrics(ctx, "default", "dep1")
	if err != nil {
		t.Fatalf("GetDeploymentMetrics error: %v", err)
	}
	if len(depRes) != 3 {
		t.Fatalf("expected 3 deployment metrics, got %d", len(depRes))
	}
}
