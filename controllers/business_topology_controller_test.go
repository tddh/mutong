package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/tddh/mutong/services"
	"github.com/gin-gonic/gin"
	nebula "github.com/vesoft-inc/nebula-go/v3"
	nebula0 "github.com/vesoft-inc/nebula-go/v3/nebula"
	nebulaGraph "github.com/vesoft-inc/nebula-go/v3/nebula/graph"
	"go.uber.org/zap"
)

func bizAppResultSet(t *testing.T) *nebula.ResultSet {
	t.Helper()
	cols := [][]byte{
		[]byte("uid"), []byte("app_name"), []byte("namespace"), []byte("criticality"),
		[]byte("environment"), []byte("team"), []byte("business_unit"), []byte("owner_name"), []byte("owner_kind"),
	}
	vals := make([]*nebula0.Value, 0, len(cols))
	for _, s := range []string{"app-uid-1", "payments", "team-a", "critical", "prod", "team-a", "bu-1", "owner", "Deployment"} {
		vals = append(vals, &nebula0.Value{SVal: []byte(s)})
	}
	ds := &nebula0.DataSet{ColumnNames: cols, Rows: []*nebula0.Row{{Values: vals}}}
	rs, err := nebula.GenResultSet(&nebulaGraph.ExecutionResponse{ErrorCode: 0, Data: ds})
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func newBizTopoRouter(t *testing.T, db *stubGraphDB) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	svc := services.NewBusinessTopologyService(zap.NewNop(), db)
	NewBusinessTopologyController(svc).RegisterRoutes(r)
	return r
}

func TestBusinessTopologyGraphUnavailable(t *testing.T) {
	r := newBizTopoRouter(t, &stubGraphDB{}) // all queries fail
	for _, path := range []string{
		"/api/v1/business-topology/apps",
		"/api/v1/business-topology/calls",
		"/api/v1/business-topology/graph",
		"/api/v1/business-topology/app?uid=x",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusInternalServerError {
			t.Errorf("%s with graph down: got %d, want 500", path, w.Code)
		}
	}
}

func TestBusinessTopologyAppsAndGraph(t *testing.T) {
	db := &stubGraphDB{executeFn: func(query string) (*nebula.ResultSet, error) {
		switch {
		case strings.Contains(query, "BelongsToApp"):
			// No members: provenance falls back to trace.
			ds := &nebula0.DataSet{ColumnNames: [][]byte{[]byte("uid")}}
			return nebula.GenResultSet(&nebulaGraph.ExecutionResponse{ErrorCode: 0, Data: ds})
		case strings.Contains(query, "CallsApp"):
			ds := &nebula0.DataSet{
				ColumnNames: [][]byte{[]byte("source"), []byte("target")},
				Rows: []*nebula0.Row{
					{Values: []*nebula0.Value{{SVal: []byte("app-uid-1")}, {SVal: []byte("app-uid-1")}}},
					{Values: []*nebula0.Value{{SVal: []byte("app-uid-1")}, {SVal: []byte("ghost")}}},
				},
			}
			return nebula.GenResultSet(&nebulaGraph.ExecutionResponse{ErrorCode: 0, Data: ds})
		default:
			return bizAppResultSet(t), nil
		}
	}}
	r := newBizTopoRouter(t, db)

	// Apps list.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/business-topology/apps?businessUnit=bu-1&team=team-a&namespace=team-a", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("apps: got %d: %s", w.Code, w.Body.String())
	}
	var appsResp struct {
		Apps []services.BusinessAppNode `json:"apps"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &appsResp); err != nil {
		t.Fatal(err)
	}
	if len(appsResp.Apps) != 1 || appsResp.Apps[0].AppName != "payments" || appsResp.Apps[0].UID != "app-uid-1" {
		t.Fatalf("unexpected apps: %+v", appsResp.Apps)
	}
	if appsResp.Apps[0].Provenance != "trace" {
		t.Fatalf("provenance = %q, want trace", appsResp.Apps[0].Provenance)
	}
	// Filters must be quoted into the query (sanitized), not concatenated raw.
	found := false
	for _, q := range db.queries {
		if strings.Contains(q, "WHERE") && strings.Contains(q, "'bu-1'") {
			found = true
		}
	}
	if !found {
		t.Fatalf("filters not applied as quoted literals: %v", db.queries)
	}

	// Graph keeps only edges whose endpoints are known apps.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/business-topology/graph", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("graph: got %d", w.Code)
	}
	var graph struct {
		Nodes []services.BusinessAppNode `json:"nodes"`
		Edges []services.CallEdge        `json:"edges"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &graph); err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %+v", graph.Nodes)
	}
	if len(graph.Edges) != 1 || graph.Edges[0].Source != "app-uid-1" || graph.Edges[0].Target != "app-uid-1" {
		t.Fatalf("dangling edge must be filtered out: %+v", graph.Edges)
	}

	// Calls endpoint returns raw edges.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/business-topology/calls", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("calls: got %d", w.Code)
	}
	var callsResp struct {
		Edges []services.CallEdge `json:"edges"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &callsResp)
	if len(callsResp.Edges) != 2 {
		t.Fatalf("expected 2 raw edges, got %+v", callsResp.Edges)
	}
}
