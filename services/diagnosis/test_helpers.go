package diagnosis

import (
	"context"
	"sync"
	"time"

	nebula "github.com/vesoft-inc/nebula-go/v3"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	alert_interfaces "gitee.com/tddh/mutong/interfaces/alert"
	alert_models "gitee.com/tddh/mutong/models/alert"
)

type TestLogger struct {
	mu   sync.Mutex
	Logs []string
}

func (l *TestLogger) Debug(msg string, fields ...zap.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Logs = append(l.Logs, "DEBUG:"+msg)
}

func (l *TestLogger) Info(msg string, fields ...zap.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Logs = append(l.Logs, "INFO:"+msg)
}

func (l *TestLogger) Warn(msg string, fields ...zap.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Logs = append(l.Logs, "WARN:"+msg)
}

func (l *TestLogger) Error(msg string, fields ...zap.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Logs = append(l.Logs, "ERROR:"+msg)
}

type TestGraphDB struct {
	ExecuteFn         func(query string) (*nebula.ResultSet, error)
	ExecuteAndCheckFn func(query string) (*nebula.ResultSet, error)
}

func (m *TestGraphDB) Execute(query string) (*nebula.ResultSet, error) {
	if m.ExecuteFn != nil {
		return m.ExecuteFn(query)
	}
	return nil, nil
}

func (m *TestGraphDB) ExecuteAndCheck(query string) (*nebula.ResultSet, error) {
	if m.ExecuteAndCheckFn != nil {
		return m.ExecuteAndCheckFn(query)
	}
	return m.Execute(query)
}

type TestAlertProcessor struct {
	Alerts map[string]*alert_models.ProcessedAlert
}

func (m *TestAlertProcessor) Process(ctx context.Context, payload *alert_models.WebhookPayload) ([]*alert_models.ProcessedAlert, error) {
	return nil, nil
}

func (m *TestAlertProcessor) GetActiveAlerts(ctx context.Context, filter map[string]string) ([]*alert_models.ProcessedAlert, error) {
	var result []*alert_models.ProcessedAlert
	for _, a := range m.Alerts {
		match := true
		for k, v := range filter {
			switch k {
			case "namespace":
				if a.Namespace != v {
					match = false
				}
			case "severity":
				if a.Routing.Severity != v {
					match = false
				}
			default:
				match = false
			}
		}
		if match {
			result = append(result, a)
		}
	}
	return result, nil
}

func (m *TestAlertProcessor) GetAlertByFingerprint(ctx context.Context, fingerprint string) (*alert_models.ProcessedAlert, error) {
	if alert, ok := m.Alerts[fingerprint]; ok {
		return alert, nil
	}
	return nil, nil
}

func (m *TestAlertProcessor) ProcessExternal(ctx context.Context, alert *alert_models.Alert, source *alert_models.AlertSource) ([]*alert_models.ProcessedAlert, error) {
	processed := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{Alert: *alert},
		ProcessedAt:   time.Now(),
	}
	return []*alert_models.ProcessedAlert{processed}, nil
}

var (
	_ interfaces.Logger               = (*TestLogger)(nil)
	_ interfaces.GraphDB              = (*TestGraphDB)(nil)
	_ alert_interfaces.AlertProcessor = (*TestAlertProcessor)(nil)
)
