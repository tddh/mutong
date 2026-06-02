package alert

import (
	"context"
	"testing"
	"time"

	alert_models "gitee.com/tddh/mutong/models/alert"
)

func TestAlertWebhookIntegration(t *testing.T) {
	enricher := &mockEnricher{
		enriched: &alert_models.EnrichedAlert{
			Alert:        alert_models.Alert{Fingerprint: "fp-1"},
			ResourceType: "Pod",
			ResourceName: "test-pod",
			Namespace:    "default",
			NodeName:     "node-1",
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

	webhookPayload := alert_models.WebhookPayload{
		Version:  "4",
		GroupKey: "test-group-key",
		Status:   "firing",
		Receiver: "mutong-webhook",
		Alerts: []alert_models.Alert{
			{
				Status:       "firing",
				Fingerprint:  "fp-1",
				StartsAt:     time.Now(),
				GeneratorURL: "http://prometheus:9090/graph?g0.expr=TestAlert",
				Labels: map[string]string{
					"alertname": "TestAlert",
					"namespace": "default",
					"pod":       "test-pod",
					"severity":  "warning",
				},
				Annotations: map[string]string{
					"summary":     "Test alert summary",
					"description": "Test alert description",
				},
			},
		},
	}

	ctx := context.Background()
	processed, err := svc.Process(ctx, &webhookPayload)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(processed) != 1 {
		t.Fatalf("Expected 1 alert processed, got %d", len(processed))
	}

	if len(storage.saved) != 1 {
		t.Errorf("Expected 1 alert saved, got %d", len(storage.saved))
	}

	if len(notifier.notified) != 1 {
		t.Errorf("Expected 1 alert notified, got %d", len(notifier.notified))
	}

	savedAlert := storage.saved[0]
	if savedAlert.Fingerprint != "fp-1" {
		t.Errorf("Saved alert fingerprint = %v, want fp-1", savedAlert.Fingerprint)
	}
	if savedAlert.ResourceType != "Pod" {
		t.Errorf("Saved alert resource type = %v, want Pod", savedAlert.ResourceType)
	}
	if savedAlert.Namespace != "default" {
		t.Errorf("Saved alert namespace = %v, want default", savedAlert.Namespace)
	}
}

func TestAlertWebhookMultipleAlerts(t *testing.T) {
	enricher := &mockEnricher{
		enriched: &alert_models.EnrichedAlert{
			Alert:        alert_models.Alert{Fingerprint: "fp-1"},
			ResourceType: "Pod",
			Namespace:    "default",
		},
	}
	suppressor := &mockSuppressor{
		result: &alert_models.SuppressionResult{IsSuppressed: false},
	}
	router := &mockRouter{
		result: &alert_models.RoutingResult{Receiver: "default", NotifyChannel: "slack"},
	}
	notifier := &mockNotifier{}
	storage := &mockStorage{}

	svc := NewAlertService(&mockLogger{}, nil, enricher, suppressor, router, notifier, storage, nil)

	webhookPayload := alert_models.WebhookPayload{
		Version:  "4",
		GroupKey: "test-group",
		Status:   "firing",
		Alerts: []alert_models.Alert{
			{Fingerprint: "fp-1", Status: "firing", StartsAt: time.Now()},
			{Fingerprint: "fp-2", Status: "firing", StartsAt: time.Now()},
			{Fingerprint: "fp-3", Status: "firing", StartsAt: time.Now()},
		},
	}

	ctx := context.Background()
	processed, err := svc.Process(ctx, &webhookPayload)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(processed) != 3 {
		t.Errorf("Expected 3 alerts processed, got %d", len(processed))
	}

	if len(storage.saved) != 3 {
		t.Errorf("Expected 3 alerts saved, got %d", len(storage.saved))
	}

	if len(notifier.notified) != 3 {
		t.Errorf("Expected 3 alerts notified, got %d", len(notifier.notified))
	}
}

func TestAlertWebhookWithResolvedAlert(t *testing.T) {
	enricher := &mockEnricher{
		enriched: &alert_models.EnrichedAlert{
			Alert:        alert_models.Alert{Fingerprint: "fp-1"},
			ResourceType: "Pod",
			Namespace:    "default",
		},
	}
	suppressor := &mockSuppressor{
		result: &alert_models.SuppressionResult{IsSuppressed: false},
	}
	router := &mockRouter{
		result: &alert_models.RoutingResult{Receiver: "default", NotifyChannel: "slack"},
	}
	notifier := &mockNotifier{}
	storage := &mockStorage{}

	svc := NewAlertService(&mockLogger{}, nil, enricher, suppressor, router, notifier, storage, nil)

	webhookPayload := alert_models.WebhookPayload{
		Version:  "4",
		GroupKey: "test-group",
		Status:   "resolved",
		Alerts: []alert_models.Alert{
			{Fingerprint: "fp-1", Status: "resolved", StartsAt: time.Now().Add(-1 * time.Hour), EndsAt: time.Now()},
		},
	}

	ctx := context.Background()
	processed, err := svc.Process(ctx, &webhookPayload)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(processed) != 1 {
		t.Fatalf("Expected 1 alert processed, got %d", len(processed))
	}

	if processed[0].Status != "resolved" {
		t.Errorf("Expected alert status = resolved, got %v", processed[0].Status)
	}
}

func TestAlertWebhookEmptyPayload(t *testing.T) {
	svc := NewAlertService(&mockLogger{}, nil, nil, nil, nil, nil, &mockStorage{}, nil)

	payload := &alert_models.WebhookPayload{
		Version:  "4",
		GroupKey: "empty-group",
		Status:   "firing",
		Alerts:   []alert_models.Alert{},
	}

	ctx := context.Background()
	processed, err := svc.Process(ctx, payload)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(processed) != 0 {
		t.Errorf("Expected 0 alerts processed, got %d", len(processed))
	}
}
