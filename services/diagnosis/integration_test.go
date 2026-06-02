package diagnosis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	nebula "github.com/vesoft-inc/nebula-go/v3"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/diagnosis"
)

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, fields ...zap.Field) {}
func (m *mockLogger) Info(msg string, fields ...zap.Field)  {}
func (m *mockLogger) Warn(msg string, fields ...zap.Field)  {}
func (m *mockLogger) Error(msg string, fields ...zap.Field) {}

var _ interfaces.Logger = (*mockLogger)(nil)

type mockGraphDBForTopology struct {
	executeAndCheckFunc func(query string) (*nebula.ResultSet, error)
}

func (m *mockGraphDBForTopology) Execute(query string) (*nebula.ResultSet, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockGraphDBForTopology) ExecuteAndCheck(query string) (*nebula.ResultSet, error) {
	return m.executeAndCheckFunc(query)
}

var _ interfaces.GraphDB = (*mockGraphDBForTopology)(nil)

func TestTopologyQuerier_GetTopologySnapshot_Error(t *testing.T) {
	mockDB := &mockGraphDBForTopology{
		executeAndCheckFunc: func(query string) (*nebula.ResultSet, error) {
			return nil, fmt.Errorf("mock: no nebula connection")
		},
	}

	querier := NewTopologyQuerier(&mockLogger{}, mockDB)
	snapshot := querier.GetTopologySnapshot("test-uid-123")

	if snapshot.Error == "" {
		t.Error("expected error from mock, got nil")
	}
}

func TestImpactAssessor_AssessImpact_Mock(t *testing.T) {
	mockDB := &mockGraphDBForTopology{
		executeAndCheckFunc: func(query string) (*nebula.ResultSet, error) {
			return nil, fmt.Errorf("mock: no nebula connection")
		},
	}

	querier := NewTopologyQuerier(&mockLogger{}, mockDB)
	assessor := NewImpactAssessor(&mockLogger{}, querier)

	impact := assessor.AssessImpact("uid-1", "Pod", "test-pod", "default", "critical")

	if impact.Severity != "critical" {
		t.Errorf("expected severity critical, got %s", impact.Severity)
	}
	if impact.BlastRadius != 0 {
		t.Errorf("expected 0 blast radius on error, got %d", impact.BlastRadius)
	}
}

func TestTopologyEdge_Deduplication(t *testing.T) {
	edgeSet := make(map[string]bool)
	edges := []struct{ from, to, edgeType string }{
		{"uid-1", "uid-2", "RunsOn"},
		{"uid-1", "uid-2", "RunsOn"},
		{"uid-2", "uid-3", "BelongsTo"},
		{"uid-1", "uid-3", "SvcToPods"},
	}

	var result []struct{ from, to, edgeType string }
	for _, e := range edges {
		key := fmt.Sprintf("%s->%s->%s", e.from, e.to, e.edgeType)
		if edgeSet[key] {
			continue
		}
		edgeSet[key] = true
		result = append(result, e)
	}

	if len(result) != 3 {
		t.Errorf("expected 3 unique edges, got %d", len(result))
	}
}

func TestHTTPLLMProvider_Diagnose_WithFullContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]string{
				{"type": "text", "text": `{"root_cause":"HighCPU on api-server","confidence":0.88,"evidence":["CPU > 90%"],"remediation":{"action":"Scale horizontally","description":"Add more replicas","steps":["Increase replicas"],"risk_level":"low","auto_fixable":true}}`},
			},
		})
	}))
	defer server.Close()

	provider := NewHTTPLLMProvider("anthropic", "claude-sonnet-4-20250514", "test", server.URL, 30)

	result, err := provider.Diagnose(context.Background(), interfaces.DiagnosisPrompt{
		Alert: map[string]string{"alertname": "HighCPU"},
		TopologySnapshot: &diagnosis.TopologySnapshot{
			Nodes: []diagnosis.TopologyNode{{UID: "uid-1", Kind: "Pod", Name: "api-server", Namespace: "prod"}},
			Edges: []diagnosis.TopologyEdge{{From: "uid-1", To: "uid-2", Type: "RunsOn"}},
		},
		ImpactAssessment: &diagnosis.ImpactAssessment{
			Severity:         "critical",
			BlastRadius:      5,
			UserFacingImpact: true,
			AffectedServices: []string{"api-gateway"},
		},
		KnowledgeMatches: []diagnosis.RemediationSuggestion{{
			Action:      "Restart pod",
			RiskLevel:   "low",
			AutoFixable: true,
		}},
		RelatedAlerts: []string{"fp-123", "fp-456"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RootCause != "HighCPU on api-server" {
		t.Errorf("expected 'HighCPU on api-server', got %q", result.RootCause)
	}
	if result.Confidence != 0.88 {
		t.Errorf("expected confidence 0.88, got %f", result.Confidence)
	}
}
