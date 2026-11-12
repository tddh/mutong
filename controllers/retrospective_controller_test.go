package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	alert_models "gitee.com/tddh/mutong/models/alert"
	retro_model "gitee.com/tddh/mutong/models/retrospective"
	retro_svc "gitee.com/tddh/mutong/services/retrospective"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newRetroRouter(t *testing.T, p *fakeAlertProcessor, db *gorm.DB) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := retro_svc.NewService(zap.NewNop(), nil, p, db, nil, nil)
	r := gin.New()
	NewRetrospectiveController(zap.NewNop(), svc).RegisterRoutes(r)
	return r
}

func newRetroDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&retro_model.PostmortemModel{}, &retro_model.PostmortemResourceLink{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRetroTimelineAndCausalChain(t *testing.T) {
	now := time.Now()
	p := &fakeAlertProcessor{processed: []*alert_models.ProcessedAlert{
		{
			EnrichedAlert: alert_models.EnrichedAlert{
				Alert:        alert_models.Alert{Fingerprint: "fp1", Labels: map[string]string{"alertname": "HighCPU"}, StartsAt: now},
				ResourceType: "Pod", ResourceName: "api-1", Namespace: "team-a",
			},
		},
	}}
	r := newRetroRouter(t, p, nil)

	// Unknown fingerprint -> 500 (alert lookup failed).
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/retrospective/timeline/ghost", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("timeline for unknown alert: got %d", w.Code)
	}

	// Known fingerprint -> timeline with the trigger event.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/retrospective/timeline/fp1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("timeline: got %d: %s", w.Code, w.Body.String())
	}
	var events []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[0]["event_type"] != "alert_triggered" {
		t.Fatalf("expected alert_triggered event, got %+v", events)
	}

	// Causal chain.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/retrospective/causal-chain/fp1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("causal chain: got %d: %s", w.Code, w.Body.String())
	}
	var chain map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &chain)
	if chain["root_cause"] != "Pod/api-1" {
		t.Fatalf("unexpected causal chain: %v", chain)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/retrospective/causal-chain/ghost", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("causal chain for unknown alert: got %d", w.Code)
	}
}

func TestRetroPostmortemPersistence(t *testing.T) {
	db := newRetroDB(t)
	if err := db.Create(&retro_model.PostmortemModel{
		Fingerprint: "fp1",
		ReportJSON:  `{"incident_title":"API outage","severity":"critical"}`,
	}).Error; err != nil {
		t.Fatal(err)
	}
	r := newRetroRouter(t, &fakeAlertProcessor{}, db)

	// History by fingerprint.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/retrospective/history/fp1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("history: got %d: %s", w.Code, w.Body.String())
	}
	var model retro_model.PostmortemModel
	if err := json.Unmarshal(w.Body.Bytes(), &model); err != nil {
		t.Fatal(err)
	}
	if model.Fingerprint != "fp1" {
		t.Fatalf("unexpected postmortem: %+v", model)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/retrospective/history/ghost", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing history: got %d", w.Code)
	}

	// List without filters (sqlite-compatible path).
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/retrospective/list?page=1&page_size=500", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d: %s", w.Code, w.Body.String())
	}
	var listResp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	if listResp["total"] != float64(1) {
		t.Fatalf("expected total=1, got %v", listResp)
	}
	if listResp["page_size"] != float64(100) {
		t.Fatalf("page_size must clamp to 100, got %v", listResp["page_size"])
	}

	// By-resource lookup (empty link table).
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/retrospective/resource/uid-1/postmortems", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("by resource: got %d: %s", w.Code, w.Body.String())
	}
	var byRes struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &byRes)
	if byRes.Count != 0 {
		t.Fatalf("expected 0 linked postmortems, got %d", byRes.Count)
	}

	// Update with invalid payload -> 400.
	w = doJSON(r, http.MethodPut, "/api/v1/retrospective/postmortem/fp1", `{bad`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("update bad payload: got %d", w.Code)
	}
}

func TestRetroWithoutDatabase(t *testing.T) {
	r := newRetroRouter(t, &fakeAlertProcessor{}, nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/retrospective/list", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("list without db: got %d", w.Code)
	}
}
