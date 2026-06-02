package alert

import (
	"context"
	"testing"
	"time"

	alert_models "gitee.com/tddh/mutong/models/alert"
)

func TestAlertStorage_SaveAndGet(t *testing.T) {
	storage := NewAlertStorage(&mockLogger{})

	alert := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			Alert: alert_models.Alert{
				Status:      "firing",
				Fingerprint: "test-fp-1",
				StartsAt:    time.Now(),
			},
			ResourceType: "Pod",
			ResourceName: "test-pod",
			Namespace:    "default",
		},
		Routing: alert_models.RoutingResult{
			Severity: "warning",
		},
		ProcessedAt: time.Now(),
	}

	ctx := context.Background()
	err := storage.Save(ctx, alert)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := storage.GetAlertByFingerprint(ctx, "test-fp-1")
	if err != nil {
		t.Fatalf("GetAlertByFingerprint() error = %v", err)
	}

	if got.Fingerprint != alert.Fingerprint {
		t.Errorf("Fingerprint = %v, want %v", got.Fingerprint, alert.Fingerprint)
	}
	if got.Status != alert.Status {
		t.Errorf("Status = %v, want %v", got.Status, alert.Status)
	}
}

func TestAlertStorage_GetActiveAlerts(t *testing.T) {
	storage := NewAlertStorage(&mockLogger{})
	ctx := context.Background()

	alerts := []*alert_models.ProcessedAlert{
		{
			EnrichedAlert: alert_models.EnrichedAlert{
				Alert:     alert_models.Alert{Status: "firing", Fingerprint: "fp-1"},
				Namespace: "default",
			},
			Routing:     alert_models.RoutingResult{Severity: "warning"},
			ProcessedAt: time.Now(),
		},
		{
			EnrichedAlert: alert_models.EnrichedAlert{
				Alert:     alert_models.Alert{Status: "resolved", Fingerprint: "fp-2"},
				Namespace: "default",
			},
			Routing:     alert_models.RoutingResult{Severity: "warning"},
			ProcessedAt: time.Now(),
		},
		{
			EnrichedAlert: alert_models.EnrichedAlert{
				Alert:     alert_models.Alert{Status: "firing", Fingerprint: "fp-3"},
				Namespace: "kube-system",
			},
			Routing:     alert_models.RoutingResult{Severity: "critical"},
			ProcessedAt: time.Now(),
		},
	}

	for _, a := range alerts {
		if err := storage.Save(ctx, a); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	t.Run("Get all active alerts", func(t *testing.T) {
		active, err := storage.GetActiveAlerts(ctx, nil)
		if err != nil {
			t.Fatalf("GetActiveAlerts() error = %v", err)
		}
		if len(active) != 2 {
			t.Errorf("GetActiveAlerts() count = %v, want 2", len(active))
		}
	})

	t.Run("Filter by namespace", func(t *testing.T) {
		active, err := storage.GetActiveAlerts(ctx, map[string]string{"namespace": "default"})
		if err != nil {
			t.Fatalf("GetActiveAlerts() error = %v", err)
		}
		if len(active) != 1 {
			t.Errorf("GetActiveAlerts() count = %v, want 1", len(active))
		}
	})

	t.Run("Filter by severity", func(t *testing.T) {
		active, err := storage.GetActiveAlerts(ctx, map[string]string{"severity": "critical"})
		if err != nil {
			t.Fatalf("GetActiveAlerts() error = %v", err)
		}
		if len(active) != 1 {
			t.Errorf("GetActiveAlerts() count = %v, want 1", len(active))
		}
	})
}

func TestAlertStorage_CleanupResolvedAlerts(t *testing.T) {
	storage := NewAlertStorage(&mockLogger{})
	ctx := context.Background()

	now := time.Now()

	alerts := []*alert_models.ProcessedAlert{
		{
			EnrichedAlert: alert_models.EnrichedAlert{
				Alert: alert_models.Alert{Status: "resolved", Fingerprint: "fp-resolved-old"},
			},
			ProcessedAt: now.Add(-2 * time.Hour),
		},
		{
			EnrichedAlert: alert_models.EnrichedAlert{
				Alert: alert_models.Alert{Status: "resolved", Fingerprint: "fp-resolved-new"},
			},
			ProcessedAt: now.Add(-30 * time.Minute),
		},
		{
			EnrichedAlert: alert_models.EnrichedAlert{
				Alert: alert_models.Alert{Status: "firing", Fingerprint: "fp-firing"},
			},
			ProcessedAt: now.Add(-1 * time.Hour),
		},
	}

	for _, a := range alerts {
		if err := storage.Save(ctx, a); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	cleaned := storage.CleanupResolvedAlerts(1 * time.Hour)
	if cleaned != 1 {
		t.Errorf("CleanupResolvedAlerts() count = %v, want 1", cleaned)
	}

	active, _ := storage.GetActiveAlerts(ctx, nil)
	if len(active) != 1 {
		t.Errorf("Active alerts count = %v, want 1", len(active))
	}
}

func TestAlertStorage_GetStats(t *testing.T) {
	storage := NewAlertStorage(&mockLogger{})
	ctx := context.Background()

	alerts := []*alert_models.ProcessedAlert{
		{EnrichedAlert: alert_models.EnrichedAlert{Alert: alert_models.Alert{Status: "firing", Fingerprint: "fp-1"}}, ProcessedAt: time.Now()},
		{EnrichedAlert: alert_models.EnrichedAlert{Alert: alert_models.Alert{Status: "firing", Fingerprint: "fp-2"}}, ProcessedAt: time.Now()},
		{EnrichedAlert: alert_models.EnrichedAlert{Alert: alert_models.Alert{Status: "resolved", Fingerprint: "fp-3"}}, ProcessedAt: time.Now()},
	}

	for _, a := range alerts {
		storage.Save(ctx, a)
	}

	stats := storage.GetStats()

	if stats["total"] != 3 {
		t.Errorf("Stats total = %v, want 3", stats["total"])
	}
	if stats["firing"] != 2 {
		t.Errorf("Stats firing = %v, want 2", stats["firing"])
	}
	if stats["resolved"] != 1 {
		t.Errorf("Stats resolved = %v, want 1", stats["resolved"])
	}
}

func TestAlertStorage_SaveEmptyFingerprint(t *testing.T) {
	storage := NewAlertStorage(&mockLogger{})
	ctx := context.Background()

	alert := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			Alert: alert_models.Alert{Status: "firing"},
		},
		ProcessedAt: time.Now(),
	}

	err := storage.Save(ctx, alert)
	if err == nil {
		t.Error("Save() expected error for empty fingerprint")
	}
}
