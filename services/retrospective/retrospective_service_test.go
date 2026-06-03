package retrospective

import (
	"context"
	"fmt"
	"testing"
	"time"

	nebula "github.com/vesoft-inc/nebula-go/v3"
	"go.uber.org/zap"

	alert_models "gitee.com/tddh/mutong/models/alert"
)

type mockRetroLogger struct{}

func (m *mockRetroLogger) Debug(msg string, fields ...zap.Field) {}
func (m *mockRetroLogger) Info(msg string, fields ...zap.Field)  {}
func (m *mockRetroLogger) Warn(msg string, fields ...zap.Field)  {}
func (m *mockRetroLogger) Error(msg string, fields ...zap.Field) {}

type mockRetroGraphDB struct{}

func (m *mockRetroGraphDB) Execute(query string) (*nebula.ResultSet, error) { return nil, nil }
func (m *mockRetroGraphDB) ExecuteAndCheck(query string) (*nebula.ResultSet, error) {
	return nil, nil
}

type mockRetroAlertProcessor struct {
	alerts map[string]*alert_models.ProcessedAlert
}

func (m *mockRetroAlertProcessor) Process(ctx context.Context, payload *alert_models.WebhookPayload) ([]*alert_models.ProcessedAlert, error) {
	return nil, nil
}

func (m *mockRetroAlertProcessor) ProcessExternal(ctx context.Context, a *alert_models.Alert, source *alert_models.AlertSource) ([]*alert_models.ProcessedAlert, error) {
	return nil, nil
}

func (m *mockRetroAlertProcessor) GetActiveAlerts(ctx context.Context, filters map[string]string) ([]*alert_models.ProcessedAlert, error) {
	var result []*alert_models.ProcessedAlert
	for _, a := range m.alerts {
		match := true
		for k, v := range filters {
			switch k {
			case "namespace":
				if a.Namespace != v {
					match = false
				}
			case "nodeName":
				if a.NodeName != v {
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

func (m *mockRetroAlertProcessor) GetAlertByFingerprint(ctx context.Context, fp string) (*alert_models.ProcessedAlert, error) {
	if a, ok := m.alerts[fp]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("alert not found: %s", fp)
}

func newTestService(alerts map[string]*alert_models.ProcessedAlert) *Service {
	return NewService(&mockRetroLogger{}, &mockRetroGraphDB{}, &mockRetroAlertProcessor{alerts: alerts}, nil, nil, nil)
}

func makeRetroAlert(fp, resourceType, resourceName, namespace, nodeName, alertName string, startsAt time.Time) *alert_models.ProcessedAlert {
	return &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			Alert: alert_models.Alert{
				Fingerprint: fp,
				Status:      "firing",
				Labels:      map[string]string{"alertname": alertName},
				StartsAt:    startsAt,
			},
			ResourceType: resourceType,
			ResourceName: resourceName,
			Namespace:    namespace,
			NodeName:     nodeName,
		},
		Routing: alert_models.RoutingResult{Severity: "critical"},
	}
}

func TestBuildTimeline(t *testing.T) {
	now := time.Now()
	alerts := map[string]*alert_models.ProcessedAlert{
		"fp-1": makeRetroAlert("fp-1", "Node", "node-1", "default", "node-1", "NodeNotReady", now.Add(-10*time.Minute)),
		"fp-2": makeRetroAlert("fp-2", "Pod", "pod-1", "default", "node-1", "PodCrashLoop", now.Add(-5*time.Minute)),
	}

	svc := newTestService(alerts)
	ctx := context.Background()

	timeline, err := svc.BuildTimeline(ctx, "fp-1")
	if err != nil {
		t.Fatalf("BuildTimeline() error = %v", err)
	}

	if len(timeline) < 1 {
		t.Errorf("Expected at least 1 event, got %d", len(timeline))
	}

	if timeline[0].EventType != "alert_triggered" {
		t.Errorf("First event should be alert_triggered, got %s", timeline[0].EventType)
	}
}

func TestAnalyzeCausalChain(t *testing.T) {
	now := time.Now()
	alerts := map[string]*alert_models.ProcessedAlert{
		"fp-1": makeRetroAlert("fp-1", "Node", "node-1", "default", "node-1", "NodeNotReady", now.Add(-10*time.Minute)),
		"fp-2": makeRetroAlert("fp-2", "Pod", "pod-1", "default", "node-1", "PodCrashLoop", now.Add(-5*time.Minute)),
	}

	svc := newTestService(alerts)
	ctx := context.Background()

	chain, err := svc.AnalyzeCausalChain(ctx, "fp-1")
	if err != nil {
		t.Fatalf("AnalyzeCausalChain() error = %v", err)
	}

	if chain.RootCause == "" {
		t.Error("Expected root cause")
	}

	if len(chain.Links) == 0 {
		t.Error("Expected causal links")
	}
}

func TestGeneratePostmortem(t *testing.T) {
	now := time.Now()
	alerts := map[string]*alert_models.ProcessedAlert{
		"fp-1": makeRetroAlert("fp-1", "Node", "node-1", "default", "node-1", "NodeNotReady", now.Add(-10*time.Minute)),
		"fp-2": makeRetroAlert("fp-2", "Pod", "pod-1", "default", "node-1", "PodCrashLoop", now.Add(-5*time.Minute)),
	}

	svc := newTestService(alerts)
	ctx := context.Background()

	report, err := svc.GeneratePostmortem(ctx, "fp-1")
	if err != nil {
		t.Fatalf("GeneratePostmortem() error = %v", err)
	}

	if report.ID == "" {
		t.Error("Expected report ID")
	}

	if report.IncidentTitle == "" {
		t.Error("Expected incident title")
	}

	if len(report.Timeline) == 0 {
		t.Error("Expected timeline events")
	}

	if len(report.LessonsLearned) == 0 {
		t.Error("Expected lessons learned")
	}

	if len(report.ActionItems) == 0 {
		t.Error("Expected action items")
	}
}

func TestFormatReport(t *testing.T) {
	now := time.Now()
	alerts := map[string]*alert_models.ProcessedAlert{
		"fp-1": makeRetroAlert("fp-1", "Node", "node-1", "default", "node-1", "NodeNotReady", now.Add(-10*time.Minute)),
	}

	svc := newTestService(alerts)
	ctx := context.Background()

	report, err := svc.GeneratePostmortem(ctx, "fp-1")
	if err != nil {
		t.Fatalf("GeneratePostmortem() error = %v", err)
	}

	formatted := svc.FormatReport(report)
	if formatted == "" {
		t.Error("Expected non-empty formatted report")
	}

	if !contains(formatted, "# 故障复盘报告") {
		t.Error("Expected markdown heading in report")
	}

	if !contains(formatted, "📅 事件时间线") {
		t.Error("Expected Timeline section in report")
	}

	if !contains(formatted, "📝 根因") {
		t.Error("Expected Root Cause section in report")
	}

	if !contains(formatted, "🎯 改进项") {
		t.Error("Expected Action Items section in report")
	}
}

func TestGeneratePostmortemNonExistent(t *testing.T) {
	svc := newTestService(map[string]*alert_models.ProcessedAlert{})
	ctx := context.Background()

	_, err := svc.GeneratePostmortem(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent alert")
	}
}

func TestParseTimeFlexible(t *testing.T) {
	cst := time.FixedZone("CST", 8*60*60)
	tests := []struct {
		name    string
		input   string
		loc     *time.Location
		wantUTC string
		wantErr bool
	}{
		{
			name:    "RFC3339 with Z",
			input:   "2026-05-19T16:00:00Z",
			wantUTC: "2026-05-19T16:00:00Z",
		},
		{
			name:    "RFC3339 with +08:00",
			input:   "2026-05-19T16:00:00+08:00",
			wantUTC: "2026-05-19T08:00:00Z",
		},
		{
			name:    "datetime-local format",
			input:   "2026-05-20T00:00",
			loc:     cst,
			wantUTC: "2026-05-19T16:00:00Z",
		},
		{
			name:    "date only",
			input:   "2026-05-20",
			loc:     cst,
			wantUTC: "2026-05-19T16:00:00Z",
		},
		{
			name:    "datetime with seconds",
			input:   "2026-05-20 00:00:00",
			loc:     cst,
			wantUTC: "2026-05-19T16:00:00Z",
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid format",
			input:   "not-a-date",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Temporarily override time.Local for deterministic testing
			origLocal := time.Local
			if tt.loc != nil {
				time.Local = tt.loc
				defer func() { time.Local = origLocal }()
			}

			got, err := parseTimeFlexible(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseTimeFlexible(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseTimeFlexible(%q) unexpected error: %v", tt.input, err)
				return
			}
			gotUTC := got.UTC().Format(time.RFC3339)
			if gotUTC != tt.wantUTC {
				t.Errorf("parseTimeFlexible(%q) = %s, want %s", tt.input, gotUTC, tt.wantUTC)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	if start+len(substr) > len(s) {
		return false
	}
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
