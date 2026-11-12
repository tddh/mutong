package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitee.com/tddh/mutong/interfaces"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// stubLogQuerier records the arguments it receives so defaults can be asserted.
type stubLogQuerier struct {
	entries []interfaces.LogEntry
	err     error

	gotNamespace string
	gotPod       string
	gotContainer string
	gotSince     int
	gotTail      int
	gotQuery     string
	gotMax       int
}

func (s *stubLogQuerier) GetPodLogs(_ context.Context, ns, pod, container string, since, tail int) ([]interfaces.LogEntry, error) {
	s.gotNamespace, s.gotPod, s.gotContainer, s.gotSince, s.gotTail = ns, pod, container, since, tail
	return s.entries, s.err
}
func (s *stubLogQuerier) SearchLogs(_ context.Context, query, ns string, since, max int) ([]interfaces.LogEntry, error) {
	s.gotQuery, s.gotNamespace, s.gotSince, s.gotMax = query, ns, since, max
	return s.entries, s.err
}
func (s *stubLogQuerier) GetErrorLogs(_ context.Context, ns string, since int) ([]interfaces.LogEntry, error) {
	s.gotNamespace, s.gotSince = ns, since
	return s.entries, s.err
}
func (s *stubLogQuerier) GetWarnLogs(_ context.Context, ns string, since int) ([]interfaces.LogEntry, error) {
	s.gotNamespace, s.gotSince = ns, since
	return s.entries, s.err
}
func (s *stubLogQuerier) GetInfoLogs(_ context.Context, ns string, since int) ([]interfaces.LogEntry, error) {
	s.gotNamespace, s.gotSince = ns, since
	return s.entries, s.err
}

func TestLogEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	q := &stubLogQuerier{entries: []interfaces.LogEntry{{Message: "hello", Level: "error"}}}
	r := gin.New()
	NewLogController(zap.NewNop(), q).RegisterRoutes(r)

	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}

	// Validation.
	if w := get("/api/v1/logs/pod"); w.Code != http.StatusBadRequest {
		t.Fatalf("pod logs without params: got %d", w.Code)
	}
	if w := get("/api/v1/logs/search"); w.Code != http.StatusBadRequest {
		t.Fatalf("search without query: got %d", w.Code)
	}

	// Defaults: since=15, tail=50.
	if w := get("/api/v1/logs/pod?namespace=ns&name=p"); w.Code != http.StatusOK {
		t.Fatalf("pod logs: got %d: %s", w.Code, w.Body.String())
	}
	if q.gotNamespace != "ns" || q.gotPod != "p" || q.gotSince != 15 || q.gotTail != 50 {
		t.Fatalf("unexpected defaults passed to querier: %+v", q)
	}
	var entries []interfaces.LogEntry
	if err := json.Unmarshal(get("/api/v1/logs/pod?namespace=ns&name=p").Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Message != "hello" {
		t.Fatalf("unexpected entries: %+v", entries)
	}

	// Explicit params override defaults.
	if w := get("/api/v1/logs/pod?namespace=ns&name=p&container=c&since=60&tail=10"); w.Code != http.StatusOK {
		t.Fatalf("pod logs with params: got %d", w.Code)
	}
	if q.gotContainer != "c" || q.gotSince != 60 || q.gotTail != 10 {
		t.Fatalf("explicit params not forwarded: %+v", q)
	}

	// Search forwards query and max.
	if w := get("/api/v1/logs/search?query=oom&namespace=ns&max=5"); w.Code != http.StatusOK {
		t.Fatalf("search: got %d", w.Code)
	}
	if q.gotQuery != "oom" || q.gotMax != 5 {
		t.Fatalf("search params not forwarded: %+v", q)
	}

	// Level endpoints.
	for _, level := range []string{"errors", "warn", "info"} {
		if w := get("/api/v1/logs/" + level + "?namespace=ns"); w.Code != http.StatusOK {
			t.Fatalf("%s logs: got %d", level, w.Code)
		}
	}

	// Querier failure -> 500.
	q.err = errors.New("es down")
	for _, path := range []string{
		"/api/v1/logs/pod?namespace=ns&name=p",
		"/api/v1/logs/search?query=x",
		"/api/v1/logs/errors", "/api/v1/logs/warn", "/api/v1/logs/info",
	} {
		if w := get(path); w.Code != http.StatusInternalServerError {
			t.Fatalf("%s with failing querier: got %d", path, w.Code)
		}
	}
}
