package alert

import (
	"testing"

	alert_models "gitee.com/tddh/mutong/models/alert"
	"gitee.com/tddh/mutong/models/diagnosis"
)

func TestCheckCausalSuppression_NoBusinessApp(t *testing.T) {
	s := &AlertSuppressor{bizAppAlertIndex: make(map[string]map[string]struct{})}
	a := &alert_models.EnrichedAlert{}
	suppressed, _ := s.checkCausalSuppression(a)
	if suppressed {
		t.Error("expected no suppression when BusinessContext.AppName is empty")
	}
}

func TestCheckCausalSuppression_NoDownstreams(t *testing.T) {
	s := &AlertSuppressor{bizAppAlertIndex: make(map[string]map[string]struct{})}
	a := &alert_models.EnrichedAlert{
		BusinessContext: alert_models.BusinessContext{AppName: "order-service", Namespace: "ecommerce"},
		BusinessCalls:   &diagnosis.BusinessAppCalls{Downstreams: nil},
	}
	suppressed, _ := s.checkCausalSuppression(a)
	if suppressed {
		t.Error("expected no suppression when no downstreams")
	}
}

func TestCheckCausalSuppression_DownstreamFiring(t *testing.T) {
	idx := map[string]map[string]struct{}{
		"kafka|ecommerce": {"fp1": {}},
	}
	s := &AlertSuppressor{bizAppAlertIndex: idx}
	a := &alert_models.EnrichedAlert{
		BusinessContext: alert_models.BusinessContext{AppName: "order-service", Namespace: "ecommerce"},
		BusinessCalls: &diagnosis.BusinessAppCalls{
			Downstreams: []diagnosis.BusinessAppRef{
				{AppName: "kafka", Team: "middleware-team", Criticality: "high"},
			},
		},
	}
	suppressed, reason := s.checkCausalSuppression(a)
	if !suppressed {
		t.Error("expected suppression when downstream is firing")
	}
	if reason == "" {
		t.Error("expected reason to be non-empty")
	}
}

func TestCheckCausalSuppression_DownstreamNotFiring(t *testing.T) {
	idx := map[string]map[string]struct{}{
		"redis|redis": {"fp2": {}},
	}
	s := &AlertSuppressor{bizAppAlertIndex: idx}
	a := &alert_models.EnrichedAlert{
		BusinessContext: alert_models.BusinessContext{AppName: "order-service", Namespace: "ecommerce"},
		BusinessCalls: &diagnosis.BusinessAppCalls{
			Downstreams: []diagnosis.BusinessAppRef{
				{AppName: "kafka", Team: "middleware-team"},
			},
		},
	}
	suppressed, _ := s.checkCausalSuppression(a)
	if suppressed {
		t.Error("expected no suppression when downstream is not in index")
	}
}
