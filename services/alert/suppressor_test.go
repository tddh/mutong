package alert

import (
	"testing"

	nebula "github.com/vesoft-inc/nebula-go/v3"
)

type mockGraphDBForSuppressor struct{}

func (m *mockGraphDBForSuppressor) Execute(query string) (*nebula.ResultSet, error) {
	return nil, nil
}
func (m *mockGraphDBForSuppressor) ExecuteAndCheck(query string) (*nebula.ResultSet, error) {
	return nil, nil
}

func TestNewAlertSuppressor_WithConfig(t *testing.T) {
	config := SuppressionConfig{
		TimeWindowSeconds:  600,
		MaxDepth:           5,
		SeverityExceptions: []string{"critical", "error"},
		MaxAlertAgeMinutes: 180,
		CleanupIntervalSec: 120,
	}

	suppressor, err := NewAlertSuppressor(&mockLogger{}, &mockGraphDBForSuppressor{}, nil, config)
	if err != nil {
		t.Fatalf("failed to create suppressor: %v", err)
	}

	s := suppressor.(*AlertSuppressor)
	if s.config.TimeWindowSeconds != 600 {
		t.Errorf("expected TimeWindowSeconds=600, got %d", s.config.TimeWindowSeconds)
	}
	if s.config.MaxDepth != 5 {
		t.Errorf("expected MaxDepth=5, got %d", s.config.MaxDepth)
	}
	if len(s.config.SeverityExceptions) != 2 {
		t.Errorf("expected 2 SeverityExceptions, got %d", len(s.config.SeverityExceptions))
	}
	if s.config.SeverityExceptions[0] != "critical" {
		t.Errorf("expected first SeverityException=critical, got %s", s.config.SeverityExceptions[0])
	}
	if s.config.MaxAlertAgeMinutes != 180 {
		t.Errorf("expected MaxAlertAgeMinutes=180, got %d", s.config.MaxAlertAgeMinutes)
	}
	if s.config.CleanupIntervalSec != 120 {
		t.Errorf("expected CleanupIntervalSec=120, got %d", s.config.CleanupIntervalSec)
	}
}

func TestNewAlertSuppressor_Defaults(t *testing.T) {
	suppressor, err := NewAlertSuppressor(&mockLogger{}, &mockGraphDBForSuppressor{}, nil, SuppressionConfig{})
	if err != nil {
		t.Fatalf("failed to create suppressor: %v", err)
	}

	s := suppressor.(*AlertSuppressor)
	if s.config.TimeWindowSeconds != 300 {
		t.Errorf("expected default TimeWindowSeconds=300, got %d", s.config.TimeWindowSeconds)
	}
	if s.config.MaxAlertAgeMinutes != 60 {
		t.Errorf("expected default MaxAlertAgeMinutes=60, got %d", s.config.MaxAlertAgeMinutes)
	}
	if s.config.CleanupIntervalSec != 60 {
		t.Errorf("expected default CleanupIntervalSec=60, got %d", s.config.CleanupIntervalSec)
	}
}
