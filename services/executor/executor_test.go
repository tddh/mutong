package executor

import (
	"context"
	"testing"

	diagModel "gitee.com/tddh/mutong/models/diagnosis"
	ex "gitee.com/tddh/mutong/models/executor"
)

// MockExecutor implements a minimal subset of interfaces.Executor to be used
// with RemediationBridge.ExecuteRemediation without needing a real Kubernetes cluster.
type MockExecutor struct{}

func (m *MockExecutor) Execute(_ context.Context, plan ex.ExecutionPlan) (*ex.ExecutionResult, error) {
	return &ex.ExecutionResult{Success: true, Message: "mocked"}, nil
}

func (m *MockExecutor) GetAuditLogs(_ context.Context, _ map[string]string) ([]ex.AuditLog, error) {
	return []ex.AuditLog{}, nil
}

func (m *MockExecutor) RecordAudit(_ context.Context, _ ex.AuditLog) error { return nil }

func (m *MockExecutor) IsAutoMode() bool   { return false }
func (m *MockExecutor) SetAutoMode(_ bool) {}

func TestRemediationBridge_ShouldExecute(t *testing.T) {
	b := NewRemediationBridge()
	rem := diagModel.RemediationSuggestion{AutoFixable: true, RiskLevel: "low", Action: "Restart"}
	if !b.ShouldExecute(rem, true) {
		t.Fatalf("expected ShouldExecute to be true when autoMode is on for low risk with AutoFixable=true")
	}
	if b.ShouldExecute(rem, false) {
		t.Fatalf("expected ShouldExecute to be false when autoMode is off")
	}
	// High risk should not auto-execute
	rem2 := diagModel.RemediationSuggestion{AutoFixable: true, RiskLevel: "high", Action: "Restart"}
	if b.ShouldExecute(rem2, true) {
		t.Fatalf("expected ShouldExecute to be false for high risk")
	}
}

func TestRemediationBridge_CreatePlanFromDiagnosis(t *testing.T) {
	b := NewRemediationBridge()
	root := diagModel.RootCause{ResourceType: "Pod", Namespace: "default", ResourceName: "pod-1", Confidence: 0.9}
	rem := diagModel.RemediationSuggestion{Action: "Restart", AutoFixable: true, RiskLevel: "low"}
	result := &diagModel.DiagnosisResult{
		Summary:      "summary",
		Request:      diagModel.DiagnosisRequest{Fingerprint: "fp-test-123"},
		RootCauses:   []diagModel.RootCause{root},
		Remediations: []diagModel.RemediationSuggestion{rem},
	}
	plan, shouldAuto := b.CreatePlanFromDiagnosis(result, true)
	if plan == nil {
		t.Fatalf("expected a plan to be created from diagnosis")
	}
	if !shouldAuto {
		t.Fatalf("expected shouldAuto to be true for low risk auto mode")
	}
	if plan.Action != ex.ActionRestartPod {
		t.Fatalf("expected plan action to be RestartPod, got %v", plan.Action)
	}
	if plan.Fingerprint != "fp-test-123" {
		t.Fatalf("expected plan fingerprint to be fp-test-123, got %q", plan.Fingerprint)
	}
}

func TestRemediationBridge_ExecuteRemediation(t *testing.T) {
	b := NewRemediationBridge()
	// Prepare a plan
	plan := &ex.ExecutionPlan{ID: "plan-3", Action: ex.ActionRestartPod, Namespace: "default", ResourceName: "pod-3"}
	mock := &MockExecutor{}
	ctx := context.Background()
	res, err := b.ExecuteRemediation(ctx, plan, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || !res.Success {
		t.Fatalf("expected remediation to return success from mock executor, got: %#v", res)
	}
}
