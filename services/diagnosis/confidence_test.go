package diagnosis

import (
	"testing"

	alert_models "gitee.com/tddh/mutong/models/alert"
)

func TestComputeDiagnosisConfidence_Basic(t *testing.T) {
	alert := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			BusinessContext: alert_models.BusinessContext{Criticality: "medium"},
		},
	}
	ctx := &PipelineContext{RelatedAlerts: nil}
	score := computeDiagnosisConfidence(alert, ctx, 0.5)
	if score != 0.5 {
		t.Errorf("expected 0.5, got %f", score)
	}
}

func TestComputeDiagnosisConfidence_WithRelatedAlerts(t *testing.T) {
	alert := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			BusinessContext: alert_models.BusinessContext{Criticality: "medium"},
		},
	}
	ctx := &PipelineContext{RelatedAlerts: []string{"fp1", "fp2", "fp3"}}
	score := computeDiagnosisConfidence(alert, ctx, 0.5)
	if score != 0.75 {
		t.Errorf("expected 0.75 (0.5 + 0.25), got %f", score)
	}
}

func TestComputeDiagnosisConfidence_Critical(t *testing.T) {
	alert := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			BusinessContext: alert_models.BusinessContext{Criticality: "critical"},
		},
	}
	ctx := &PipelineContext{RelatedAlerts: nil}
	score := computeDiagnosisConfidence(alert, ctx, 0.6)
	if score != 0.75 {
		t.Errorf("expected 0.75 (0.6 + 0.15), got %f", score)
	}
}

func TestComputeDiagnosisConfidence_CappedAtOne(t *testing.T) {
	alert := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			BusinessContext: alert_models.BusinessContext{Criticality: "critical"},
		},
	}
	ctx := &PipelineContext{RelatedAlerts: []string{"fp1", "fp2", "fp3"}}
	score := computeDiagnosisConfidence(alert, ctx, 0.9)
	if score != 1.0 {
		t.Errorf("expected capped at 1.0, got %f", score)
	}
}
