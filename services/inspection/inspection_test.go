package inspection

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	inspection_model "gitee.com/tddh/mutong/models/inspection"
)

type mockRule struct {
	name string
}

func (m *mockRule) Name() string        { return m.name }
func (m *mockRule) Description() string { return "mock" }
func (m *mockRule) Severity() string    { return "info" }
func (m *mockRule) Execute(ctx context.Context, graphDB interfaces.GraphDB) ([]inspection_model.InspectionResult, error) {
	return nil, nil
}

type nopLogger struct {
	*zap.Logger
}

func (n *nopLogger) Debug(msg string, fields ...zap.Field) { n.Logger.Debug(msg, fields...) }
func (n *nopLogger) Info(msg string, fields ...zap.Field)  { n.Logger.Info(msg, fields...) }
func (n *nopLogger) Warn(msg string, fields ...zap.Field)  { n.Logger.Warn(msg, fields...) }
func (n *nopLogger) Error(msg string, fields ...zap.Field) { n.Logger.Error(msg, fields...) }

func TestRegisterRuleFirstComeWins(t *testing.T) {
	logger := &nopLogger{zap.NewNop()}
	engine := NewInspectionEngine(logger, nil)

	engine.RegisterRule(&mockRule{name: "test_rule"})
	engine.RegisterRule(&mockRule{name: "test_rule"}) // same name, should be skipped

	_, err := engine.ExecuteRule(context.Background(), "test_rule")
	if err != nil {
		t.Fatal("rule should exist:", err)
	}
}

func TestRegisterRuleDifferentNames(t *testing.T) {
	logger := &nopLogger{zap.NewNop()}
	engine := NewInspectionEngine(logger, nil)

	engine.RegisterRule(&mockRule{name: "rule_a"})
	engine.RegisterRule(&mockRule{name: "rule_b"})

	if _, err := engine.ExecuteRule(context.Background(), "rule_a"); err != nil {
		t.Fatal("rule_a should exist:", err)
	}
	if _, err := engine.ExecuteRule(context.Background(), "rule_b"); err != nil {
		t.Fatal("rule_b should exist:", err)
	}
}

func TestYAMLRuleAdapterWithSharedEngine(t *testing.T) {
	logger := &nopLogger{zap.NewNop()}

	rule1 := Rule{
		Name:        "shared_rule_1",
		Description: "test",
		Query:       "RETURN 1",
		Check:       CheckConfig{Type: "min_rows", Threshold: 1},
		Severity:    "info",
		Suggestion:  "test suggestion",
	}
	rule2 := Rule{
		Name:        "shared_rule_2",
		Description: "test",
		Query:       "RETURN 1",
		Check:       CheckConfig{Type: "min_rows", Threshold: 1},
		Severity:    "warning",
		Suggestion:  "test suggestion",
	}

	engine := NewYAMLEngine(logger, nil, nil)
	adapter1 := NewYAMLRuleAdapterWithEngine(rule1, engine)
	adapter2 := NewYAMLRuleAdapterWithEngine(rule2, engine)

	if adapter1.Name() != "shared_rule_1" {
		t.Fatalf("expected shared_rule_1, got %s", adapter1.Name())
	}
	if adapter2.Name() != "shared_rule_2" {
		t.Fatalf("expected shared_rule_2, got %s", adapter2.Name())
	}
	if adapter1.Severity() != "info" {
		t.Fatalf("expected info, got %s", adapter1.Severity())
	}
}
