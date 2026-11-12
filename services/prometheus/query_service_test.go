package prometheus_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gitee.com/tddh/mutong/interfaces"
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

// FUNC-02：nil 服务必须返回 nil 接口，调用方判空才不会被非 nil 接口绕过
func TestAsMetricsQuerier_NilServiceReturnsNilInterface(t *testing.T) {
	var svc *prometheus.QueryService
	var querier interfaces.MetricsQuerier = svc.AsMetricsQuerier()
	if querier != nil {
		t.Fatalf("expected nil interface for nil service, got %#v", querier)
	}
	if svc.AsAlertPrometheusQuerier() != nil {
		t.Fatal("expected nil AlertPrometheusQuerier for nil service")
	}
	// nil 接收者直接调用也不得 panic
	if res, err := svc.QueryMetric(context.Background(), "Pod", "default", "pod1"); err != nil || res != nil {
		t.Fatalf("nil service QueryMetric should be a no-op, got %v / %v", res, err)
	}
	if svc.IsEnabled() {
		t.Fatal("nil service must not report enabled")
	}
}

// 未配置 URL 的服务返回可用适配器，但查询为空结果而非 panic
func TestQueryMetricWithoutBaseURL(t *testing.T) {
	svc := prometheus.NewQueryService(zap.NewNop(), prometheus.PrometheusConfig{})
	if svc.AsMetricsQuerier() == nil {
		t.Fatal("configured service should expose a querier")
	}
	res, err := svc.QueryMetric(context.Background(), "Pod", "default", "pod1")
	if err != nil || len(res) != 0 {
		t.Fatalf("expected empty result without error, got %v / %v", res, err)
	}
}

// SEC-03：namespace/name 注入样例必须被拒绝，且不得发出任何查询
func TestQueryMetricRejectsPromQLInjection(t *testing.T) {
	var queries []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer ts.Close()

	svc := prometheus.NewQueryService(zap.NewNop(), prometheus.PrometheusConfig{URL: ts.URL, Timeout: 5})

	injections := []struct{ namespace, name string }{
		{`default",pod="evil`, "pod1"},
		{`default\`, "pod1"},
		{"default", `pod1"} or up{`},
		{"default", "pod1\n"},
		{"default", `pod1\"`},
		{"Default", "pod1"},
		{"default", "pod_1"},
		{"default", "pod1; DROP"},
		{"", "pod1"},
	}
	for _, tc := range injections {
		if _, err := svc.GetPodMetrics(context.Background(), tc.namespace, tc.name); err == nil {
			t.Errorf("expected rejection for namespace=%q name=%q", tc.namespace, tc.name)
		}
	}
	if len(queries) != 0 {
		t.Fatalf("no query should reach Prometheus for injected input, got %v", queries)
	}

	// 合法命名仍然放行，且标签值原样进入匹配器
	if _, err := svc.GetPodMetrics(context.Background(), "kube-system", "pod-1.abc"); err != nil {
		t.Fatalf("valid K8s names must be accepted: %v", err)
	}
	if len(queries) == 0 {
		t.Fatal("expected queries for valid input")
	}
	for _, q := range queries {
		if !strings.Contains(q, `namespace="kube-system"`) || !strings.Contains(q, `pod="pod-1.abc"`) {
			t.Errorf("unexpected query built from valid names: %s", q)
		}
		if strings.Contains(q, "%!(EXTRA") || strings.Contains(q, "%!s(MISSING)") {
			t.Errorf("query template args mismatch: %s", q)
		}
	}
}

// Node 指标只按 instance 匹配，不得把空 namespace 塞进模板
func TestGetNodeMetricsUsesResourceName(t *testing.T) {
	var queries []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer ts.Close()

	svc := prometheus.NewQueryService(zap.NewNop(), prometheus.PrometheusConfig{URL: ts.URL, Timeout: 5})
	if _, err := svc.GetNodeMetrics(context.Background(), "node-1"); err != nil {
		t.Fatalf("GetNodeMetrics: %v", err)
	}
	if len(queries) == 0 {
		t.Fatal("expected node queries")
	}
	for _, q := range queries {
		if !strings.Contains(q, `instance="node-1"`) {
			t.Errorf("node query should match instance label: %s", q)
		}
		if strings.Contains(q, "%!(EXTRA") || strings.Contains(q, "%!s(MISSING)") {
			t.Errorf("node query template args mismatch: %s", q)
		}
	}
}

// SEC-03：HPA 查询同样拒绝注入样例
func TestGetHPAStatusRejectsInjection(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no query should be issued for injected HPA input: %s", r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer ts.Close()

	svc := prometheus.NewQueryService(zap.NewNop(), prometheus.PrometheusConfig{URL: ts.URL, Timeout: 5})
	if _, err := svc.GetHPAStatus(context.Background(), `ns",hpa="x`, "hpa1"); err == nil {
		t.Fatal("expected error for injected namespace")
	}
	if _, err := svc.GetHPAStatus(context.Background(), "default", `hpa1"}`); err == nil {
		t.Fatal("expected error for injected hpa name")
	}
}
