package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	insp_model "gitee.com/tddh/mutong/models/inspection"
	insp_svc "gitee.com/tddh/mutong/services/inspection"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeInspectionProcessor struct {
	results []insp_model.InspectionResult
	report  *insp_model.InspectionReport
	reports []*insp_model.InspectionReport
	trend   []insp_model.TrendPoint
	err     error
}

func (f *fakeInspectionProcessor) Start() error { return nil }
func (f *fakeInspectionProcessor) Stop()        {}
func (f *fakeInspectionProcessor) ExecuteNow(context.Context) ([]insp_model.InspectionResult, error) {
	return f.results, f.err
}
func (f *fakeInspectionProcessor) GetLatestReport() (*insp_model.InspectionReport, error) {
	return f.report, f.err
}
func (f *fakeInspectionProcessor) GetReportByID(id string) (*insp_model.InspectionReport, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, r := range f.reports {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, errors.New("not found")
}
func (f *fakeInspectionProcessor) GetReportsByRange(time.Time, time.Time, int, int) ([]*insp_model.InspectionReport, error) {
	return f.reports, f.err
}
func (f *fakeInspectionProcessor) GetTrendData(int) ([]insp_model.TrendPoint, error) {
	return f.trend, f.err
}
func (f *fakeInspectionProcessor) CompareReports(a, b *insp_model.InspectionReport) *insp_model.ComparisonResult {
	return insp_svc.CompareReports(a, b)
}

func TestInspectionWithoutProcessor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewInspectionController(zap.NewNop(), nil).RegisterRoutes(r)

	for _, req := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/inspection/execute"},
		{http.MethodGet, "/api/v1/inspection/report"},
	} {
		w := doJSON(r, req.method, req.path, `{}`)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: got %d, want 503", req.method, req.path, w.Code)
		}
	}
	// Rule endpoints without a store also degrade to 503.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/inspection/rules", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("rules without store: got %d", w.Code)
	}
}

func TestInspectionExecuteAndReports(t *testing.T) {
	gin.SetMode(gin.TestMode)
	p := &fakeInspectionProcessor{
		results: []insp_model.InspectionResult{{RuleName: "r1"}, {RuleName: "r2"}},
		report:  &insp_model.InspectionReport{ID: "rep-1"},
		reports: []*insp_model.InspectionReport{{ID: "rep-1"}, {ID: "rep-2"}},
		trend:   []insp_model.TrendPoint{{Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Count: 3}},
	}
	r := gin.New()
	NewInspectionController(zap.NewNop(), p).RegisterRoutes(r)

	w := doJSON(r, http.MethodPost, "/api/v1/inspection/execute", `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("execute: got %d: %s", w.Code, w.Body.String())
	}
	var execResp struct {
		Status string `json:"status"`
		Total  int    `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &execResp)
	if execResp.Status != "success" || execResp.Total != 2 {
		t.Fatalf("unexpected execute response: %+v", execResp)
	}

	// Latest report.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/inspection/report", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("report: got %d", w.Code)
	}

	// History requires a valid RFC3339 range.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/inspection/reports", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("history without range: got %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/v1/inspection/reports?start=2026-01-01T00:00:00Z&end=2026-01-02T00:00:00Z", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("history: got %d: %s", w.Code, w.Body.String())
	}
	var histResp struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &histResp)
	if histResp.Count != 2 {
		t.Fatalf("history count = %d, want 2", histResp.Count)
	}

	// Detail and compare.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/inspection/reports/rep-1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("detail: got %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/inspection/reports/ghost", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing detail: got %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/inspection/reports/rep-1/compare/rep-2", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("compare: got %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/inspection/reports/rep-1/compare/ghost", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("compare missing: got %d", w.Code)
	}

	// Trend.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/inspection/trend?days=7", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("trend: got %d", w.Code)
	}
	var trendResp struct {
		Days int `json:"days"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &trendResp)
	if trendResp.Days != 7 {
		t.Fatalf("days = %d, want 7", trendResp.Days)
	}

	// Processor failure paths.
	p.err = errors.New("inspection backend down")
	if w := doJSON(r, http.MethodPost, "/api/v1/inspection/execute", `{}`); w.Code != http.StatusInternalServerError {
		t.Fatalf("execute error: got %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/inspection/report", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("report error: got %d", w.Code)
	}
}

func TestInspectionRuleCRUD(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&insp_model.InspectionRuleModel{}); err != nil {
		t.Fatal(err)
	}
	ctrl := NewInspectionController(zap.NewNop(), &fakeInspectionProcessor{})
	ctrl.SetRuleStore(insp_svc.NewRuleStore(db))
	r := gin.New()
	ctrl.RegisterRoutes(r)

	// Create.
	w := doJSON(r, http.MethodPost, "/api/v1/inspection/rules",
		`{"name":"pod-restarts","query":"MATCH (v:Pod) RETURN v","checkType":"threshold","checkConfig":"{}","severity":"warning"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create rule: got %d: %s", w.Code, w.Body.String())
	}
	var rule insp_model.InspectionRuleModel
	_ = json.Unmarshal(w.Body.Bytes(), &rule)
	if rule.ID == 0 || rule.Name != "pod-restarts" {
		t.Fatalf("unexpected created rule: %+v", rule)
	}

	// Invalid payload.
	if w := doJSON(r, http.MethodPost, "/api/v1/inspection/rules", `{bad`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad rule payload: got %d", w.Code)
	}

	// List.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/inspection/rules", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list rules: got %d", w.Code)
	}
	var rules []insp_model.InspectionRuleModel
	_ = json.Unmarshal(w.Body.Bytes(), &rules)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}

	// Update.
	w = doJSON(r, http.MethodPut, fmt.Sprintf("/api/v1/inspection/rules/%d", rule.ID),
		`{"name":"pod-restarts","query":"MATCH (v:Pod) RETURN v","checkType":"threshold","checkConfig":"{}","severity":"critical"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update rule: got %d: %s", w.Code, w.Body.String())
	}
	var updated insp_model.InspectionRuleModel
	if err := db.First(&updated, rule.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Severity != "critical" {
		t.Fatalf("severity not updated: %+v", updated)
	}
	// Update of a missing rule -> 404; invalid id -> 400.
	if w := doJSON(r, http.MethodPut, "/api/v1/inspection/rules/999", `{}`); w.Code != http.StatusNotFound {
		t.Fatalf("update missing: got %d", w.Code)
	}
	if w := doJSON(r, http.MethodPut, "/api/v1/inspection/rules/abc", `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("update invalid id: got %d", w.Code)
	}

	// Toggle.
	w = doJSON(r, http.MethodPost, fmt.Sprintf("/api/v1/inspection/rules/%d/toggle", rule.ID), `{"enabled":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("toggle rule: got %d: %s", w.Code, w.Body.String())
	}
	var toggled insp_model.InspectionRuleModel
	if err := db.First(&toggled, rule.ID).Error; err != nil {
		t.Fatal(err)
	}
	if toggled.Enabled {
		t.Fatal("toggle must disable the rule in DB")
	}

	// Delete.
	w = doJSON(r, http.MethodDelete, fmt.Sprintf("/api/v1/inspection/rules/%d", rule.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete rule: got %d: %s", w.Code, w.Body.String())
	}
	var count int64
	db.Model(&insp_model.InspectionRuleModel{}).Count(&count)
	if count != 0 {
		t.Fatalf("rule must be deleted, %d remain", count)
	}
}
