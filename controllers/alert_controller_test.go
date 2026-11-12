package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	alert_interfaces "gitee.com/tddh/mutong/interfaces/alert"
	alert_models "gitee.com/tddh/mutong/models/alert"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// fakeAlertProcessor records calls and returns canned results.
type fakeAlertProcessor struct {
	processed   []*alert_models.ProcessedAlert
	err         error
	gotFilters  map[string]string
	gotPayload  *alert_models.WebhookPayload
	gotExternal *alert_models.Alert
	gotSource   *alert_models.AlertSource
}

var _ alert_interfaces.AlertProcessor = (*fakeAlertProcessor)(nil)

func (f *fakeAlertProcessor) Process(_ context.Context, p *alert_models.WebhookPayload) ([]*alert_models.ProcessedAlert, error) {
	f.gotPayload = p
	return f.processed, f.err
}
func (f *fakeAlertProcessor) ProcessExternal(_ context.Context, a *alert_models.Alert, s *alert_models.AlertSource) ([]*alert_models.ProcessedAlert, error) {
	f.gotExternal, f.gotSource = a, s
	return f.processed, f.err
}
func (f *fakeAlertProcessor) GetActiveAlerts(_ context.Context, filters map[string]string) ([]*alert_models.ProcessedAlert, error) {
	f.gotFilters = filters
	return f.processed, f.err
}
func (f *fakeAlertProcessor) GetAlertByFingerprint(_ context.Context, fp string) (*alert_models.ProcessedAlert, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, a := range f.processed {
		if a.Fingerprint == fp {
			return a, nil
		}
	}
	return nil, errors.New("not found")
}

func newAlertTestRouter(t *testing.T, p *fakeAlertProcessor, sources map[string]*alert_models.AlertSource) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewAlertController(zap.NewNop(), p, sources).RegisterRoutes(r)
	return r
}

func TestAlertWebhookHandling(t *testing.T) {
	p := &fakeAlertProcessor{processed: []*alert_models.ProcessedAlert{{Fingerprint: "fp1"}}}
	r := newAlertTestRouter(t, p, nil)

	// Malformed payload.
	w := doJSON(r, http.MethodPost, "/api/v1/alerts/webhook", `{"alerts":`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad payload: got %d", w.Code)
	}

	// Valid payload: counts reflect received vs processed.
	w = doJSON(r, http.MethodPost, "/api/v1/alerts/webhook",
		`{"version":"4","status":"firing","alerts":[{"status":"firing","labels":{"alertname":"A"}},{"status":"firing","labels":{"alertname":"B"}}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("webhook: got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "success" || resp["received"] != float64(2) || resp["processed"] != float64(1) {
		t.Fatalf("unexpected webhook response: %v", resp)
	}
	if p.gotPayload == nil || len(p.gotPayload.Alerts) != 2 {
		t.Fatalf("payload not forwarded to processor: %+v", p.gotPayload)
	}

	// Processor failure -> 500 without internal detail.
	p.err = errors.New("storage exploded")
	w = doJSON(r, http.MethodPost, "/api/v1/alerts/webhook", `{"alerts":[]}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("processor error: got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "storage exploded") {
		t.Fatalf("internal detail leaked: %s", w.Body.String())
	}
}

func TestAlertListAndDetail(t *testing.T) {
	p := &fakeAlertProcessor{processed: []*alert_models.ProcessedAlert{
		{Fingerprint: "fp1"}, {Fingerprint: "fp2"},
	}}
	r := newAlertTestRouter(t, p, nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/v1/alerts?namespace=ns&resourceType=Pod&severity=critical&nodeName=n1&status=firing", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d", w.Code)
	}
	wantFilters := map[string]string{"namespace": "ns", "resourceType": "Pod", "severity": "critical", "nodeName": "n1", "status": "firing"}
	for k, v := range wantFilters {
		if p.gotFilters[k] != v {
			t.Errorf("filter %s = %q, want %q", k, p.gotFilters[k], v)
		}
	}
	var resp struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Count != 2 {
		t.Fatalf("count = %d, want 2", resp.Count)
	}

	// Detail by fingerprint.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/alerts/fp1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("detail: got %d", w.Code)
	}
	var detail alert_models.ProcessedAlert
	_ = json.Unmarshal(w.Body.Bytes(), &detail)
	if detail.Fingerprint != "fp1" {
		t.Fatalf("unexpected detail: %+v", detail)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/alerts/missing", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing fingerprint: got %d", w.Code)
	}

	// Health probe.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/alerts/health", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "healthy") {
		t.Fatalf("health: got %d %s", w.Code, w.Body.String())
	}
}

func TestAlertExternalWebhook(t *testing.T) {
	source := &alert_models.AlertSource{
		Name: "ceph", Type: "ceph", DisplayName: "Ceph",
		LabelMapping:       map[string]string{"alertname": "rule_name"},
		EnrichmentStrategy: alert_models.EnrichmentStrategy("generic"),
	}
	p := &fakeAlertProcessor{processed: []*alert_models.ProcessedAlert{{Fingerprint: "x"}}}
	r := newAlertTestRouter(t, p, map[string]*alert_models.AlertSource{"ceph": source})

	// Unknown source lists available alternatives.
	w := doJSON(r, http.MethodPost, "/api/v1/alerts/external/unknown", `{}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown source: got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ceph") {
		t.Fatalf("available sources must be listed: %s", w.Body.String())
	}

	// Invalid JSON.
	w = doJSON(r, http.MethodPost, "/api/v1/alerts/external/ceph", `{bad`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad json: got %d", w.Code)
	}

	// Valid: payload is mapped through the source before processing.
	w = doJSON(r, http.MethodPost, "/api/v1/alerts/external/ceph", `{"rule_name":"CephOSDDown"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("external webhook: got %d: %s", w.Code, w.Body.String())
	}
	if p.gotExternal == nil || p.gotExternal.Labels["alertname"] != "CephOSDDown" {
		t.Fatalf("label mapping not applied: %+v", p.gotExternal)
	}
	if p.gotExternal.Labels["alertSource"] != "ceph" {
		t.Fatalf("source label not injected: %+v", p.gotExternal.Labels)
	}
	if p.gotSource != source {
		t.Fatal("source must be forwarded to the processor")
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["source"] != "ceph" || resp["processed"] != float64(1) {
		t.Fatalf("unexpected response: %v", resp)
	}

	// Processor failure.
	p.err = errors.New("nope")
	w = doJSON(r, http.MethodPost, "/api/v1/alerts/external/ceph", `{}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("processor error: got %d", w.Code)
	}
}

func TestAlertListSources(t *testing.T) {
	sources := map[string]*alert_models.AlertSource{
		"ceph":  {Name: "ceph", Type: "ceph", DisplayName: "Ceph集群"},
		"mysql": {Name: "mysql", Type: "database", DisplayName: "MySQL"},
	}
	r := newAlertTestRouter(t, &fakeAlertProcessor{}, sources)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/alerts/sources", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("sources: got %d", w.Code)
	}
	var resp struct {
		Count   int `json:"count"`
		Sources []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"sources"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Count != 2 || len(resp.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %+v", resp)
	}
}
