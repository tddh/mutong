package alert

import (
	"sync"
	"testing"

	"go.uber.org/zap"
)

// capturingLogger 记录告警日志，用于断言解析失败被上报而非静默吞掉
type capturingLogger struct {
	mu       sync.Mutex
	warnings []string
}

func (l *capturingLogger) Debug(msg string, fields ...zap.Field) {}
func (l *capturingLogger) Info(msg string, fields ...zap.Field)  {}
func (l *capturingLogger) Error(msg string, fields ...zap.Field) {}
func (l *capturingLogger) Warn(msg string, fields ...zap.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warnings = append(l.warnings, msg)
}

func (l *capturingLogger) warnCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.warnings)
}

func baseAlertModel() AlertModel {
	return AlertModel{
		Fingerprint:  "fp-1",
		Labels:       `{"alertname":"OOMKilled"}`,
		Annotations:  `{}`,
		TopologyPath: `[]`,
		Suppression:  `{}`,
		Routing:      `{}`,
	}
}

// CODE-03：业务字段 JSON 损坏时必须记录告警，且不影响其余字段解析
func TestModelToProcessedAlertCorruptBusinessJSON(t *testing.T) {
	logger := &capturingLogger{}
	s := &PostgresAlertStorage{logger: logger}

	m := baseAlertModel()
	m.BusinessCtx = `{"appName":`
	m.BusinessCalls = `not-json`
	m.BusinessImpact = `{"affectedAppCount":`

	alert, err := s.modelToProcessedAlert(m)
	if err != nil {
		t.Fatalf("corrupt business JSON must not fail the whole read: %v", err)
	}
	if logger.warnCount() != 3 {
		t.Fatalf("expected 3 warnings for corrupt business fields, got %d (%v)", logger.warnCount(), logger.warnings)
	}
	if alert.BusinessContext.AppName != "" || alert.BusinessImpact.AffectedAppCount != 0 {
		t.Errorf("corrupt fields must stay zero-valued: %+v / %+v", alert.BusinessContext, alert.BusinessImpact)
	}
	if alert.Labels["alertname"] != "OOMKilled" {
		t.Errorf("valid fields must still be parsed: %+v", alert.Labels)
	}
}

// 合法业务 JSON 正常解析且不产生告警
func TestModelToProcessedAlertValidBusinessJSON(t *testing.T) {
	logger := &capturingLogger{}
	s := &PostgresAlertStorage{logger: logger}

	m := baseAlertModel()
	m.BusinessCtx = `{"appName":"pay","team":"core"}`
	m.BusinessImpact = `{"affectedAppCount":2,"containsCriticalApps":true}`
	m.BusinessCalls = `{}`
	m.EnrichTags = `{"businessApp":"pay"}`

	alert, err := s.modelToProcessedAlert(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logger.warnCount() != 0 {
		t.Fatalf("expected no warnings, got %v", logger.warnings)
	}
	if alert.BusinessContext.AppName != "pay" || alert.BusinessContext.Team != "core" {
		t.Errorf("business context not parsed: %+v", alert.BusinessContext)
	}
	if alert.BusinessImpact.AffectedAppCount != 2 || !alert.BusinessImpact.ContainsCriticalApps {
		t.Errorf("business impact not parsed: %+v", alert.BusinessImpact)
	}
	if alert.BusinessCalls == nil {
		t.Error("business calls should be parsed")
	}
	if alert.EnrichTags["businessApp"] != "pay" {
		t.Errorf("enrich tags not parsed: %+v", alert.EnrichTags)
	}
}

// enrich_tags 损坏时回退为空 map 并记录告警
func TestModelToProcessedAlertCorruptEnrichTags(t *testing.T) {
	logger := &capturingLogger{}
	s := &PostgresAlertStorage{logger: logger}

	m := baseAlertModel()
	m.EnrichTags = `{"businessApp":`

	alert, err := s.modelToProcessedAlert(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logger.warnCount() != 1 {
		t.Fatalf("expected 1 warning, got %v", logger.warnings)
	}
	if alert.EnrichTags == nil || len(alert.EnrichTags) != 0 {
		t.Errorf("enrich tags must fall back to an empty map, got %+v", alert.EnrichTags)
	}
}
