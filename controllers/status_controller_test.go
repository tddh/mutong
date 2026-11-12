package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	diagnosis_svc "gitee.com/tddh/mutong/services/diagnosis"
	"github.com/gin-gonic/gin"
	nebula "github.com/vesoft-inc/nebula-go/v3"
	"go.uber.org/zap"
)

func TestStatusEndpointsWithoutBackends(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewStatusController(zap.NewNop(), nil, nil, nil).RegisterRoutes(r)

	get := func(path string) map[string]interface{} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: got %d: %s", path, w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}

	diag := get("/api/v1/diagnosis/status")
	llm := diag["llm"].(map[string]interface{})
	if llm["configured"] != false || llm["available"] != false {
		t.Fatalf("llm status must report unconfigured, got %v", llm)
	}

	sup := get("/api/v1/alerts/suppression/status")
	rule := sup["ruleEngine"].(map[string]interface{})
	if rule["enabled"] != false {
		t.Fatalf("rule engine must report disabled, got %v", rule)
	}

	sys := get("/api/v1/system/status")
	infra := sys["infrastructure"].(map[string]interface{})
	neb := infra["nebula"].(map[string]interface{})
	if neb["available"] != false {
		t.Fatalf("nebula must be unavailable without a graph db, got %v", neb)
	}
	alerts := sys["alerts"].(map[string]interface{})
	metrics := alerts["metrics"].(map[string]interface{})
	if metrics["receivedTotal"] != float64(0) {
		t.Fatalf("alert metrics must be zeroed, got %v", metrics)
	}
}

func TestSystemStatusWithEngineAndGraph(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := diagnosis_svc.NewEngine(zap.NewNop(), nil, nil)
	// NOTE: engine.Stop() is not called — NewEngine leaves stopCh nil, so Stop
	// panics (reported bug). No goroutine is started without StartStatsSync.

	for _, graphAvailable := range []bool{true, false} {
		db := &stubGraphDB{}
		if graphAvailable {
			db.executeFn = func(string) (*nebula.ResultSet, error) { return nil, nil }
		}
		r := gin.New()
		NewStatusController(zap.NewNop(), engine, nil, db).RegisterRoutes(r)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("system status: got %d", w.Code)
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		infra := resp["infrastructure"].(map[string]interface{})
		neb := infra["nebula"].(map[string]interface{})
		if neb["available"] != graphAvailable {
			t.Fatalf("nebula availability = %v, want %v", neb["available"], graphAvailable)
		}
		if _, ok := resp["diagnosis"]; !ok {
			t.Fatal("diagnosis section missing")
		}
	}
}
