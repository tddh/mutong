package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	nebula "github.com/vesoft-inc/nebula-go/v3"
	nebula0 "github.com/vesoft-inc/nebula-go/v3/nebula"
	nebulaGraph "github.com/vesoft-inc/nebula-go/v3/nebula/graph"
	"go.uber.org/zap"
)

// stubGraphDB implements interfaces.GraphDB with per-query canned responses.
type stubGraphDB struct {
	executeFn func(query string) (*nebula.ResultSet, error)
	queries   []string
}

func (s *stubGraphDB) Execute(query string) (*nebula.ResultSet, error) {
	s.queries = append(s.queries, query)
	if s.executeFn != nil {
		return s.executeFn(query)
	}
	return nil, errors.New("graph unavailable")
}

func (s *stubGraphDB) ExecuteAndCheck(query string) (*nebula.ResultSet, error) {
	return s.Execute(query)
}

// countResultSet builds a single-row result with an integer "count" column.
func countResultSet(t *testing.T, n int64) *nebula.ResultSet {
	t.Helper()
	ds := &nebula0.DataSet{
		ColumnNames: [][]byte{[]byte("count")},
		Rows:        []*nebula0.Row{{Values: []*nebula0.Value{{IVal: &n}}}},
	}
	rs, err := nebula.GenResultSet(&nebulaGraph.ExecutionResponse{ErrorCode: 0, Data: ds})
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func TestStatsOverviewAggregatesCounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := &stubGraphDB{executeFn: func(query string) (*nebula.ResultSet, error) {
		switch {
		case strings.Contains(query, `'Pod'`):
			return countResultSet(t, 3), nil
		case strings.Contains(query, `'Node'`):
			return countResultSet(t, 2), nil
		case strings.Contains(query, `'Service'`):
			return countResultSet(t, 4), nil
		case strings.Contains(query, `'Deployment'`):
			return countResultSet(t, 1), nil
		}
		return nil, errors.New("unexpected query")
	}}
	r := gin.New()
	NewStatsController(zap.NewNop(), db, nil).RegisterRoutes(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/stats/overview", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("overview: got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Resources map[string]int `json:"resources"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"pods": 3, "nodes": 2, "services": 4, "deployments": 1, "total": 10}
	for k, v := range want {
		if resp.Resources[k] != v {
			t.Errorf("resources[%s] = %d, want %d (full: %v)", k, resp.Resources[k], v, resp.Resources)
		}
	}
	// Queries must be sanitized: kind literals are quoted, no raw injection.
	for _, q := range db.queries {
		if !strings.Contains(q, "is_deleted:false") {
			t.Errorf("overview query must filter deleted resources: %s", q)
		}
	}
}

func TestStatsOverviewToleratesGraphErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := &stubGraphDB{} // every query fails
	r := gin.New()
	NewStatsController(zap.NewNop(), db, nil).RegisterRoutes(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/stats/overview", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("overview must degrade gracefully, got %d", w.Code)
	}
	var resp struct {
		Resources map[string]int `json:"resources"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Resources["total"] != 0 {
		t.Fatalf("total must be 0 when the graph is down, got %v", resp.Resources)
	}
}

func TestStatsResourceTotal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ds := &nebula0.DataSet{
		ColumnNames: [][]byte{[]byte("vid")},
		Rows: []*nebula0.Row{
			{Values: []*nebula0.Value{{SVal: []byte("v1")}}},
			{Values: []*nebula0.Value{{SVal: []byte("v2")}}},
		},
	}
	rs, err := nebula.GenResultSet(&nebulaGraph.ExecutionResponse{ErrorCode: 0, Data: ds})
	if err != nil {
		t.Fatal(err)
	}
	db := &stubGraphDB{executeFn: func(string) (*nebula.ResultSet, error) { return rs, nil }}
	r := gin.New()
	NewStatsController(zap.NewNop(), db, nil).RegisterRoutes(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/stats/resource-total", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("resource-total: got %d", w.Code)
	}
	var resp map[string]int
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total"] != 2 {
		t.Fatalf("total = %d, want 2", resp["total"])
	}

	// Graph failure degrades to total=0, not 5xx.
	db.executeFn = nil
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/stats/resource-total", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("resource-total on error: got %d", w.Code)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total"] != 0 {
		t.Fatalf("total on error = %d, want 0", resp["total"])
	}
}

func TestStatsManualSyncWithoutService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewStatsController(zap.NewNop(), &stubGraphDB{}, nil).RegisterRoutes(r)

	for _, path := range []string{"/api/v1/stats/sync", "/api/v1/stats/backfill-relationships"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: got %d", path, w.Code)
		}
		var resp map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["status"] != "error" {
			t.Fatalf("%s without k8s service must report error status, got %v", path, resp)
		}
	}
}
