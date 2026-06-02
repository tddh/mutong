package alert

import (
	"context"
	"testing"
	"time"

	alert_models "gitee.com/tddh/mutong/models/alert"
)

func TestRouterDefaultConfig(t *testing.T) {
	router := NewAlertRouter(&mockLogger{}, RoutingConfig{})
	result, err := router.Route(context.Background(), &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "test-fp",
			Labels:      map[string]string{"severity": "warning"},
		},
		EnrichTags: map[string]string{"severity": "warning"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Receiver != "default" {
		t.Errorf("Receiver = %v, want default", result.Receiver)
	}
	if result.NotifyChannel != "slack" {
		t.Errorf("Channel = %v, want slack", result.NotifyChannel)
	}
}

func TestRouterSeverityMapping(t *testing.T) {
	router := &AlertRouter{
		logger: &mockLogger{},
		config: RoutingConfig{
			SeverityMapping: map[string]int{"critical": 1, "warning": 3},
		},
	}

	tests := []struct {
		severity string
		priority int
	}{
		{"critical", 1},
		{"warning", 3},
		{"unknown", 5},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			alert := &alert_models.EnrichedAlert{
				Alert:      alert_models.Alert{Labels: map[string]string{"severity": tt.severity}},
				EnrichTags: map[string]string{"severity": tt.severity},
			}
			priority := router.calculatePriority(alert)
			if priority != tt.priority {
				t.Errorf("calculatePriority(%s) = %v, want %v", tt.severity, priority, tt.priority)
			}
		})
	}
}

func TestRouterTeamRouting(t *testing.T) {
	router := NewAlertRouter(&mockLogger{}, RoutingConfig{
		TeamRouting: map[string]string{"platform": "platform-team"},
	})

	result, err := router.Route(context.Background(), &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{Labels: map[string]string{"team": "platform"}},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Receiver != "platform-team" {
		t.Errorf("Receiver = %v, want platform-team", result.Receiver)
	}
}

func TestRouterSeverityChannel(t *testing.T) {
	router := NewAlertRouter(&mockLogger{}, RoutingConfig{
		SeverityChannelRules: map[string]string{"critical": "pagerduty"},
	})

	result, err := router.Route(context.Background(), &alert_models.EnrichedAlert{
		Alert:      alert_models.Alert{Labels: map[string]string{"severity": "critical"}},
		EnrichTags: map[string]string{"severity": "critical"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.NotifyChannel != "pagerduty" {
		t.Errorf("Channel = %v, want pagerduty", result.NotifyChannel)
	}
}

func TestStorageSaveAndGet(t *testing.T) {
	storage := NewAlertStorage(&mockLogger{})
	alert := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			Alert: alert_models.Alert{Fingerprint: "fp-1", Status: "firing"},
		},
	}

	if err := storage.Save(context.Background(), alert); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	retrieved, err := storage.GetAlertByFingerprint(context.Background(), "fp-1")
	if err != nil {
		t.Fatalf("GetAlertByFingerprint() error = %v", err)
	}
	if retrieved.Fingerprint != "fp-1" {
		t.Errorf("Fingerprint = %v, want fp-1", retrieved.Fingerprint)
	}
}

func TestStorageGetActiveAlerts(t *testing.T) {
	storage := NewAlertStorage(&mockLogger{})

	firing := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			Alert: alert_models.Alert{Fingerprint: "fp-1", Status: "firing"},
		},
	}
	resolved := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			Alert: alert_models.Alert{Fingerprint: "fp-2", Status: "resolved"},
		},
	}

	storage.Save(context.Background(), firing)
	storage.Save(context.Background(), resolved)

	active, err := storage.GetActiveAlerts(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetActiveAlerts() error = %v", err)
	}
	if len(active) != 1 {
		t.Errorf("Active alerts count = %v, want 1", len(active))
	}
}

func TestStorageFilterByNamespace(t *testing.T) {
	storage := NewAlertStorage(&mockLogger{})

	a1 := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			Alert:     alert_models.Alert{Fingerprint: "fp-1", Status: "firing"},
			Namespace: "default",
		},
	}
	a2 := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			Alert:     alert_models.Alert{Fingerprint: "fp-2", Status: "firing"},
			Namespace: "kube-system",
		},
	}

	storage.Save(context.Background(), a1)
	storage.Save(context.Background(), a2)

	active, err := storage.GetActiveAlerts(context.Background(), map[string]string{"namespace": "default"})
	if err != nil {
		t.Fatalf("GetActiveAlerts() error = %v", err)
	}
	if len(active) != 1 {
		t.Errorf("Filtered alerts count = %v, want 1", len(active))
	}
}

func TestStorageCleanup(t *testing.T) {
	storage := NewAlertStorage(&mockLogger{})
	as := storage.(*AlertStorage)

	old := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			Alert: alert_models.Alert{Fingerprint: "fp-old", Status: "resolved"},
		},
		ProcessedAt: time.Now().Add(-2 * time.Hour),
	}
	recent := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			Alert: alert_models.Alert{Fingerprint: "fp-recent", Status: "resolved"},
		},
		ProcessedAt: time.Now().Add(-30 * time.Minute),
	}

	as.alerts.Store("fp-old", old)
	as.alerts.Store("fp-recent", recent)

	cleaned := storage.CleanupResolvedAlerts(1 * time.Hour)
	if cleaned != 1 {
		t.Errorf("Cleaned count = %v, want 1", cleaned)
	}

	stats := storage.GetStats()
	if stats["resolved"] != 1 {
		t.Errorf("Resolved count = %v, want 1", stats["resolved"])
	}
}

func TestStorageStats(t *testing.T) {
	storage := NewAlertStorage(&mockLogger{})

	storage.Save(context.Background(), &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			Alert: alert_models.Alert{Fingerprint: "fp-1", Status: "firing"},
		},
	})
	storage.Save(context.Background(), &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			Alert: alert_models.Alert{Fingerprint: "fp-2", Status: "firing"},
		},
	})
	storage.Save(context.Background(), &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			Alert: alert_models.Alert{Fingerprint: "fp-3", Status: "resolved"},
		},
	})

	stats := storage.GetStats()
	if stats["total"] != 3 {
		t.Errorf("Total = %v, want 3", stats["total"])
	}
	if stats["firing"] != 2 {
		t.Errorf("Firing = %v, want 2", stats["firing"])
	}
	if stats["resolved"] != 1 {
		t.Errorf("Resolved = %v, want 1", stats["resolved"])
	}
}
