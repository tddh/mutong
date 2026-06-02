package inspection

import "testing"

func TestInspectionRuleModelTableName(t *testing.T) {
	m := InspectionRuleModel{}
	if m.TableName() != "inspection_rules" {
		t.Errorf("expected table name 'inspection_rules', got '%s'", m.TableName())
	}
}

func TestInspectionRuleModelEnabledDefault(t *testing.T) {
	m := InspectionRuleModel{}
	if m.Enabled {
		t.Error("Enabled should default to false in Go zero-value")
	}
}
