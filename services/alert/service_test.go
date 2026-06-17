package alert

import (
	"context"
	"sync"
	"testing"
	"time"

	alert_models "gitee.com/tddh/mutong/models/alert"
)

type mockEnricher struct {
	enriched *alert_models.EnrichedAlert
	err      error
}

func (m *mockEnricher) Enrich(ctx context.Context, a *alert_models.Alert) (*alert_models.EnrichedAlert, error) {
	if m.enriched != nil {
		result := *m.enriched
		result.Alert = *a
		return &result, m.err
	}
	return nil, m.err
}

type mockSuppressor struct {
	result *alert_models.SuppressionResult
	err    error
}

func (m *mockSuppressor) CheckSuppression(ctx context.Context, a *alert_models.EnrichedAlert) (*alert_models.SuppressionResult, error) {
	return m.result, m.err
}

type mockRouter struct {
	result *alert_models.RoutingResult
	err    error
}

func (m *mockRouter) Route(ctx context.Context, a *alert_models.EnrichedAlert) (*alert_models.RoutingResult, error) {
	return m.result, m.err
}

type mockNotifier struct {
	mu       sync.Mutex
	notified []*alert_models.ProcessedAlert
	err      error
}

func (m *mockNotifier) Notify(ctx context.Context, a *alert_models.ProcessedAlert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notified = append(m.notified, a)
	return m.err
}

type mockStorage struct {
	mu    sync.Mutex
	saved []*alert_models.ProcessedAlert
}

func (m *mockStorage) Save(ctx context.Context, a *alert_models.ProcessedAlert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved = append(m.saved, a)
	return nil
}

func (m *mockStorage) GetActiveAlerts(ctx context.Context, filters map[string]string) ([]*alert_models.ProcessedAlert, error) {
	return m.saved, nil
}

func (m *mockStorage) GetAlertByFingerprint(ctx context.Context, fingerprint string) (*alert_models.ProcessedAlert, error) {
	for _, a := range m.saved {
		if a.Fingerprint == fingerprint {
			return a, nil
		}
	}
	return nil, nil
}

func (m *mockStorage) CleanupResolvedAlerts(olderThan time.Duration) int {
	return 0
}

func (m *mockStorage) GetStats() map[string]int64 {
	return map[string]int64{"total": int64(len(m.saved)), "firing": 0, "resolved": 0}
}

func TestAlertService_Process(t *testing.T) {
	enricher := &mockEnricher{
		enriched: &alert_models.EnrichedAlert{
			Alert:        alert_models.Alert{Fingerprint: "fp-1"},
			ResourceType: "Pod",
			ResourceName: "test-pod",
			Namespace:    "default",
		},
	}
	suppressor := &mockSuppressor{
		result: &alert_models.SuppressionResult{IsSuppressed: false},
	}
	router := &mockRouter{
		result: &alert_models.RoutingResult{Receiver: "default", NotifyChannel: "slack", Severity: "warning"},
	}
	notifier := &mockNotifier{}
	storage := &mockStorage{}

	svc := NewAlertService(&mockLogger{}, nil, enricher, suppressor, router, notifier, storage, nil)

	payload := &alert_models.WebhookPayload{
		GroupKey: "test-group",
		Status:   "firing",
		Alerts: []alert_models.Alert{
			{
				Fingerprint: "fp-1",
				Status:      "firing",
				Labels:      map[string]string{"alertname": "TestAlert"},
				StartsAt:    time.Now(),
			},
		},
	}

	ctx := context.Background()
	processed, err := svc.Process(ctx, payload)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(processed) != 1 {
		t.Fatalf("Process() returned %d alerts, want 1", len(processed))
	}

	if len(storage.saved) != 1 {
		t.Errorf("Storage saved %d alerts, want 1", len(storage.saved))
	}

	if len(notifier.notified) != 1 {
		t.Errorf("Notifier notified %d alerts, want 1", len(notifier.notified))
	}
}

func TestAlertService_ProcessSuppressed(t *testing.T) {
	enricher := &mockEnricher{
		enriched: &alert_models.EnrichedAlert{
			Alert:        alert_models.Alert{Fingerprint: "fp-1"},
			ResourceType: "Pod",
		},
	}
	suppressor := &mockSuppressor{
		result: &alert_models.SuppressionResult{IsSuppressed: true, SuppressionReason: "parent node down"},
	}
	router := &mockRouter{
		result: &alert_models.RoutingResult{Receiver: "default"},
	}
	notifier := &mockNotifier{}
	storage := &mockStorage{}

	svc := NewAlertService(&mockLogger{}, nil, enricher, suppressor, router, notifier, storage, nil)

	payload := &alert_models.WebhookPayload{
		Status: "firing",
		Alerts: []alert_models.Alert{
			{Fingerprint: "fp-1", Status: "firing", StartsAt: time.Now()},
		},
	}

	ctx := context.Background()
	processed, err := svc.Process(ctx, payload)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(processed) != 1 {
		t.Fatalf("Process() returned %d alerts, want 1", len(processed))
	}

	if !processed[0].Suppression.IsSuppressed {
		t.Error("Expected alert to be suppressed")
	}

	if len(notifier.notified) != 0 {
		t.Errorf("Expected no notifications for suppressed alert, got %d", len(notifier.notified))
	}
}

func TestAlertService_GetActiveAlerts(t *testing.T) {
	storage := &mockStorage{
		saved: []*alert_models.ProcessedAlert{
			{EnrichedAlert: alert_models.EnrichedAlert{Alert: alert_models.Alert{Fingerprint: "fp-1", Status: "firing"}}},
			{EnrichedAlert: alert_models.EnrichedAlert{Alert: alert_models.Alert{Fingerprint: "fp-2", Status: "firing"}}},
		},
	}

	svc := &AlertService{storage: storage, logger: &mockLogger{}}

	ctx := context.Background()
	alerts, err := svc.GetActiveAlerts(ctx, nil)
	if err != nil {
		t.Fatalf("GetActiveAlerts() error = %v", err)
	}

	if len(alerts) != 2 {
		t.Errorf("GetActiveAlerts() returned %d alerts, want 2", len(alerts))
	}
}
