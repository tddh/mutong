package alert

import (
	"context"
	"testing"

	alert_models "gitee.com/tddh/mutong/models/alert"
)

func TestRoute_DefaultReceiver(t *testing.T) {
	router := NewAlertRouter(&mockLogger{}, RoutingConfig{})

	alert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "fp-1",
			Labels:      map[string]string{},
		},
		EnrichTags: map[string]string{},
	}

	result, err := router.Route(context.Background(), alert)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Receiver != "default" {
		t.Errorf("Receiver = %q, want %q", result.Receiver, "default")
	}
}

func TestRoute_TeamLabelRouting(t *testing.T) {
	router := NewAlertRouter(&mockLogger{}, RoutingConfig{
		TeamRouting: map[string]string{
			"platform": "platform-team",
			"backend":  "backend-team",
		},
	})

	alert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "fp-1",
			Labels:      map[string]string{"team": "platform"},
		},
		EnrichTags: map[string]string{},
	}

	result, err := router.Route(context.Background(), alert)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Receiver != "platform-team" {
		t.Errorf("Receiver = %q, want %q", result.Receiver, "platform-team")
	}
}

func TestRoute_BusinessContextFallback(t *testing.T) {
	router := NewAlertRouter(&mockLogger{}, RoutingConfig{
		BusinessContextRouting: map[string]string{
			"payments": "payments-oncall",
			"infra":    "infra-oncall",
		},
	})

	alert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "fp-1",
			Labels:      map[string]string{},
		},
		EnrichTags: map[string]string{},
		BusinessContext: alert_models.BusinessContext{
			Team:        "payments",
			Criticality: "critical",
		},
	}

	result, err := router.Route(context.Background(), alert)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Receiver != "payments-oncall" {
		t.Errorf("Receiver = %q, want %q", result.Receiver, "payments-oncall")
	}
}

func TestRoute_TeamLabelTakesPriorityOverBusinessContext(t *testing.T) {
	router := NewAlertRouter(&mockLogger{}, RoutingConfig{
		TeamRouting: map[string]string{
			"frontend": "frontend-team",
		},
		BusinessContextRouting: map[string]string{
			"frontend": "frontend-oncall-fallback",
		},
	})

	alert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "fp-1",
			Labels:      map[string]string{"team": "frontend"},
		},
		EnrichTags: map[string]string{},
		BusinessContext: alert_models.BusinessContext{
			Team: "frontend",
		},
	}

	result, err := router.Route(context.Background(), alert)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Receiver != "frontend-team" {
		t.Errorf("Receiver = %q, want %q (label routing should take priority)", result.Receiver, "frontend-team")
	}
}

func TestRoute_ServiceRouting(t *testing.T) {
	router := NewAlertRouter(&mockLogger{}, RoutingConfig{
		ServiceRouting: map[string]string{
			"api-gateway": "api-team",
		},
	})

	alert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "fp-1",
			Labels:      map[string]string{"service": "api-gateway"},
		},
		EnrichTags: map[string]string{},
	}

	result, err := router.Route(context.Background(), alert)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Receiver != "api-team" {
		t.Errorf("Receiver = %q, want %q", result.Receiver, "api-team")
	}
}

func TestRoute_SeverityChannelRules(t *testing.T) {
	router := NewAlertRouter(&mockLogger{}, RoutingConfig{
		SeverityChannelRules: map[string]string{
			"critical": "pagerduty",
			"warning":  "slack",
		},
	})

	alert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "fp-1",
			Labels:      map[string]string{"severity": "critical"},
		},
		EnrichTags: map[string]string{},
	}

	result, err := router.Route(context.Background(), alert)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.NotifyChannel != "pagerduty" {
		t.Errorf("NotifyChannel = %q, want %q", result.NotifyChannel, "pagerduty")
	}
}

func TestRoute_CriticalSeverityDefaultsToPagerDuty(t *testing.T) {
	router := NewAlertRouter(&mockLogger{}, RoutingConfig{})

	alert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "fp-1",
			Labels:      map[string]string{"severity": "critical"},
		},
		EnrichTags: map[string]string{},
	}

	result, err := router.Route(context.Background(), alert)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.NotifyChannel != "pagerduty" {
		t.Errorf("NotifyChannel = %q, want %q", result.NotifyChannel, "pagerduty")
	}
}
