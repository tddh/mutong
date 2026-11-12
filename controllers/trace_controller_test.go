package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitee.com/tddh/mutong/interfaces"
	"github.com/gin-gonic/gin"
)

type stubTraceQuerier struct {
	traces   []interfaces.Trace
	spans    []interfaces.Span
	services []string
	err      error

	gotService string
	gotOp      string
	gotTraceID string
	gotLimit   int
}

func (s *stubTraceQuerier) QueryTraces(service, op, traceID string, limit int) ([]interfaces.Trace, error) {
	s.gotService, s.gotOp, s.gotTraceID, s.gotLimit = service, op, traceID, limit
	return s.traces, s.err
}
func (s *stubTraceQuerier) GetSpans(traceID string) ([]interfaces.Span, error) {
	s.gotTraceID = traceID
	return s.spans, s.err
}
func (s *stubTraceQuerier) GetServices() ([]string, error) {
	return s.services, s.err
}

func TestTraceEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	q := &stubTraceQuerier{
		traces:   []interfaces.Trace{{TraceID: "t1", Duration: 120}},
		spans:    []interfaces.Span{{SpanID: "s1", TraceID: "t1"}},
		services: []string{"svc-a", "svc-b"},
	}
	r := gin.New()
	NewTraceController(q).RegisterRoutes(r)

	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}

	// Default limit is 100; filters are forwarded.
	w := get("/api/v1/trace/query?serviceName=svc-a&operationName=op&traceId=t1")
	if w.Code != http.StatusOK {
		t.Fatalf("query: got %d: %s", w.Code, w.Body.String())
	}
	if q.gotService != "svc-a" || q.gotOp != "op" || q.gotTraceID != "t1" || q.gotLimit != 100 {
		t.Fatalf("query params not forwarded: %+v", q)
	}
	var traces []interfaces.Trace
	if err := json.Unmarshal(w.Body.Bytes(), &traces); err != nil {
		t.Fatal(err)
	}
	if len(traces) != 1 || traces[0].TraceID != "t1" {
		t.Fatalf("unexpected traces: %+v", traces)
	}

	// Explicit limit wins; invalid limit falls back to the default.
	if w := get("/api/v1/trace/query?limit=5"); w.Code != http.StatusOK {
		t.Fatalf("query with limit: got %d", w.Code)
	}
	if q.gotLimit != 5 {
		t.Fatalf("limit = %d, want 5", q.gotLimit)
	}
	if w := get("/api/v1/trace/query?limit=abc"); w.Code != http.StatusOK {
		t.Fatalf("query with bad limit: got %d", w.Code)
	}
	if q.gotLimit != 100 {
		t.Fatalf("invalid limit must fall back to 100, got %d", q.gotLimit)
	}

	// Spans and services.
	w = get("/api/v1/trace/spans?traceId=t1")
	if w.Code != http.StatusOK {
		t.Fatalf("spans: got %d", w.Code)
	}
	var spans []interfaces.Span
	_ = json.Unmarshal(w.Body.Bytes(), &spans)
	if len(spans) != 1 || spans[0].SpanID != "s1" {
		t.Fatalf("unexpected spans: %+v", spans)
	}
	w = get("/api/v1/trace/services")
	if w.Code != http.StatusOK {
		t.Fatalf("services: got %d", w.Code)
	}
	var svcs []string
	_ = json.Unmarshal(w.Body.Bytes(), &svcs)
	if len(svcs) != 2 || svcs[0] != "svc-a" {
		t.Fatalf("unexpected services: %v", svcs)
	}

	// Backend errors surface as 500.
	q.err = errors.New("collector unreachable")
	for _, path := range []string{"/api/v1/trace/query", "/api/v1/trace/spans?traceId=t", "/api/v1/trace/services"} {
		if w := get(path); w.Code != http.StatusInternalServerError {
			t.Fatalf("%s with failing backend: got %d", path, w.Code)
		}
	}
}
