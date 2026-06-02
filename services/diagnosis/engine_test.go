package diagnosis

import (
	"context"
	"testing"
	"time"

	nebula "github.com/vesoft-inc/nebula-go/v3"
	"go.uber.org/zap"

	alert_models "gitee.com/tddh/mutong/models/alert"
	"gitee.com/tddh/mutong/models/diagnosis"
)

type mockDiagLogger struct{}

func (m *mockDiagLogger) Debug(msg string, fields ...zap.Field) {}
func (m *mockDiagLogger) Info(msg string, fields ...zap.Field)  {}
func (m *mockDiagLogger) Warn(msg string, fields ...zap.Field)  {}
func (m *mockDiagLogger) Error(msg string, fields ...zap.Field) {}

type mockDiagGraphDB struct{}

func (m *mockDiagGraphDB) Execute(query string) (*nebula.ResultSet, error) {
	return nil, nil
}

func (m *mockDiagGraphDB) ExecuteAndCheck(query string) (*nebula.ResultSet, error) {
	return nil, nil
}

type mockDiagAlertProcessor struct {
	alerts map[string]*alert_models.ProcessedAlert
}

func (m *mockDiagAlertProcessor) Process(ctx context.Context, payload *alert_models.WebhookPayload) ([]*alert_models.ProcessedAlert, error) {
	return nil, nil
}

func (m *mockDiagAlertProcessor) ProcessExternal(ctx context.Context, a *alert_models.Alert, source *alert_models.AlertSource) ([]*alert_models.ProcessedAlert, error) {
	return nil, nil
}

func (m *mockDiagAlertProcessor) GetActiveAlerts(ctx context.Context, filters map[string]string) ([]*alert_models.ProcessedAlert, error) {
	var result []*alert_models.ProcessedAlert
	for _, a := range m.alerts {
		match := true
		for k, v := range filters {
			switch k {
			case "namespace":
				if a.Namespace != v {
					match = false
				}
			case "severity":
				if a.Routing.Severity != v {
					match = false
				}
			}
		}
		if match {
			result = append(result, a)
		}
	}
	return result, nil
}

func (m *mockDiagAlertProcessor) GetAlertByFingerprint(ctx context.Context, fp string) (*alert_models.ProcessedAlert, error) {
	if a, ok := m.alerts[fp]; ok {
		return a, nil
	}
	return nil, nil
}

func newTestEngine(alerts map[string]*alert_models.ProcessedAlert) *Engine {
	processor := &mockDiagAlertProcessor{alerts: alerts}
	return NewEngine(&mockDiagLogger{}, &mockDiagGraphDB{}, processor)
}

func makeAlert(fp, resourceType, resourceName, namespace, alertName string) *alert_models.ProcessedAlert {
	return &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			Alert: alert_models.Alert{
				Fingerprint: fp,
				Status:      "firing",
				Labels:      map[string]string{"alertname": alertName},
			},
			ResourceType: resourceType,
			ResourceName: resourceName,
			Namespace:    namespace,
			ResourceUID:  "test-uid-" + fp,
		},
		Routing: alert_models.RoutingResult{Severity: "warning"},
	}
}

func TestDiagnosePodMemoryAlert(t *testing.T) {
	alerts := map[string]*alert_models.ProcessedAlert{
		"fp-1": makeAlert("fp-1", "Pod", "test-pod", "default", "PodOOMKilled"),
	}

	engine := newTestEngine(alerts)
	ctx := context.Background()

	result, err := engine.Diagnose(ctx, diagnosis.DiagnosisRequest{Fingerprint: "fp-1"})
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}

	if len(result.RootCauses) == 0 {
		t.Fatal("Expected root causes")
	}

	if result.RootCauses[0].Confidence < 0.8 {
		t.Errorf("Expected high confidence for OOMKilled, got %.2f", result.RootCauses[0].Confidence)
	}

	if len(result.Remediations) == 0 {
		t.Error("Expected remediation suggestions")
	}
}

func TestDiagnoseNodeNotReady(t *testing.T) {
	alerts := map[string]*alert_models.ProcessedAlert{
		"fp-2": makeAlert("fp-2", "Node", "node-1", "", "NodeNotReady"),
	}

	engine := newTestEngine(alerts)
	ctx := context.Background()

	result, err := engine.Diagnose(ctx, diagnosis.DiagnosisRequest{Fingerprint: "fp-2"})
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}

	if len(result.RootCauses) == 0 {
		t.Fatal("Expected root causes")
	}

	if result.RootCauses[0].Confidence < 0.9 {
		t.Errorf("Expected very high confidence for NodeNotReady, got %.2f", result.RootCauses[0].Confidence)
	}
}

func TestDiagnoseNoMatchingAlert(t *testing.T) {
	engine := newTestEngine(map[string]*alert_models.ProcessedAlert{})
	ctx := context.Background()

	_, err := engine.Diagnose(ctx, diagnosis.DiagnosisRequest{Fingerprint: "nonexistent"})
	if err == nil {
		t.Error("Expected error for nonexistent alert")
	}
}

func TestKnowledgeBaseSuggestions(t *testing.T) {
	kb := NewKnowledgeBase()

	suggestions := kb.GetSuggestions("Pod", []string{"Pod exceeded memory limit", "OOMKilled"})
	if len(suggestions) == 0 {
		t.Error("Expected memory-related suggestions")
	}

	suggestions = kb.GetSuggestions("Node", []string{"Node is in NotReady state"})
	if len(suggestions) == 0 {
		t.Error("Expected node-related suggestions")
	}
}

func TestDiagnoseCrashLoopPod(t *testing.T) {
	alerts := map[string]*alert_models.ProcessedAlert{
		"fp-3": makeAlert("fp-3", "Pod", "crashing-pod", "default", "PodCrashLoopBackOff"),
	}

	engine := newTestEngine(alerts)
	ctx := context.Background()

	result, err := engine.Diagnose(ctx, diagnosis.DiagnosisRequest{Fingerprint: "fp-3"})
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}

	if len(result.RootCauses) == 0 {
		t.Fatal("Expected root causes")
	}

	if result.RootCauses[0].ResourceType != "Pod" {
		t.Errorf("Expected Pod root cause, got %s", result.RootCauses[0].ResourceType)
	}
}

func TestDiagnoseByNamespaceAndResource(t *testing.T) {
	alerts := map[string]*alert_models.ProcessedAlert{
		"fp-4": makeAlert("fp-4", "Pod", "test-pod", "kube-system", "HighMemoryUsage"),
	}

	engine := newTestEngine(alerts)
	ctx := context.Background()

	result, err := engine.Diagnose(ctx, diagnosis.DiagnosisRequest{
		Namespace: "kube-system",
		Resource:  "Pod",
	})
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}

	if len(result.RootCauses) == 0 {
		t.Fatal("Expected root causes")
	}
}

func TestGenerateSummary(t *testing.T) {
	result := &diagnosis.DiagnosisResult{
		ID:        "test-id",
		Timestamp: time.Now(),
		RootCauses: []diagnosis.RootCause{
			{ResourceType: "Pod", ResourceName: "test-pod", Namespace: "default", Confidence: 0.9, Evidence: []string{"OOMKilled"}},
		},
		Impact: diagnosis.ImpactAssessment{
			BlastRadius:       3,
			Severity:          "critical",
			AffectedResources: []string{"Pod/test-pod"},
		},
		Remediations: []diagnosis.RemediationSuggestion{
			{Action: "Increase memory limits", RiskLevel: "low"},
		},
	}

	engine := &Engine{}
	summary := engine.generateSummary(result)

	if summary == "" {
		t.Error("Expected non-empty summary")
	}
}
