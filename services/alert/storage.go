package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	alert_interfaces "gitee.com/tddh/mutong/interfaces/alert"
	alert_models "gitee.com/tddh/mutong/models/alert"
)

// AlertStorage 告警存储
// 当前实现为内存存储，生产环境应替换为持久化存储（MySQL、ClickHouse等）
type AlertStorage struct {
	logger interfaces.Logger
	alerts *sync.Map // fingerprint -> *alert_models.ProcessedAlert
}

// NewAlertStorage 创建告警存储实例
func NewAlertStorage(logger interfaces.Logger) alert_interfaces.AlertStorage {
	return &AlertStorage{
		logger: logger,
		alerts: &sync.Map{},
	}
}

// Save 保存告警
func (s *AlertStorage) Save(ctx context.Context, a *alert_models.ProcessedAlert) error {
	if a.Fingerprint == "" {
		return fmt.Errorf("alert fingerprint is required")
	}

	if a.ProcessedAt.IsZero() {
		a.ProcessedAt = time.Now()
	}
	s.alerts.Store(a.Fingerprint, a)

	s.logger.Debug("Alert saved",
		zap.String("fingerprint", a.Fingerprint),
		zap.String("status", a.Status))

	return nil
}

// GetActiveAlerts 获取活跃告警
func (s *AlertStorage) GetActiveAlerts(ctx context.Context, filters map[string]string) ([]*alert_models.ProcessedAlert, error) {
	var alerts []*alert_models.ProcessedAlert

	s.alerts.Range(func(key, value interface{}) bool {
		alert, ok := value.(*alert_models.ProcessedAlert)
		if !ok {
			return true
		}

		// 只返回 firing 状态的告警
		if alert.Status != "firing" {
			return true
		}

		// 应用过滤器
		if s.matchFilters(alert, filters) {
			alerts = append(alerts, alert)
		}

		return true
	})

	return alerts, nil
}

// GetAlertByFingerprint 根据指纹获取告警
func (s *AlertStorage) GetAlertByFingerprint(ctx context.Context, fingerprint string) (*alert_models.ProcessedAlert, error) {
	value, ok := s.alerts.Load(fingerprint)
	if !ok {
		return nil, fmt.Errorf("alert not found: %s", fingerprint)
	}

	alert, ok := value.(*alert_models.ProcessedAlert)
	if !ok {
		return nil, fmt.Errorf("invalid alert type")
	}

	return alert, nil
}

// matchFilters 检查告警是否匹配过滤器
func (s *AlertStorage) matchFilters(alert *alert_models.ProcessedAlert, filters map[string]string) bool {
	for key, value := range filters {
		switch key {
		case "namespace":
			if alert.Namespace != value {
				return false
			}
		case "resourceType":
			if alert.ResourceType != value {
				return false
			}
		case "severity":
			if alert.Routing.Severity != value {
				return false
			}
		case "nodeName":
			if alert.NodeName != value {
				return false
			}
		case "resourceName":
			if alert.ResourceName != value {
				return false
			}
		default:
			// 检查标签
			if labelValue, ok := alert.Labels[key]; !ok || labelValue != value {
				return false
			}
		}
	}
	return true
}

// ExportAlerts 导出告警为 JSON（用于持久化）
func (s *AlertStorage) ExportAlerts() ([]byte, error) {
	var alerts []*alert_models.ProcessedAlert

	s.alerts.Range(func(key, value interface{}) bool {
		alert, ok := value.(*alert_models.ProcessedAlert)
		if ok {
			alerts = append(alerts, alert)
		}
		return true
	})

	return json.MarshalIndent(alerts, "", "  ")
}

// ImportAlerts 从 JSON 导入告警（用于恢复）
func (s *AlertStorage) ImportAlerts(data []byte) error {
	var alerts []*alert_models.ProcessedAlert

	if err := json.Unmarshal(data, &alerts); err != nil {
		return fmt.Errorf("failed to unmarshal alerts: %w", err)
	}

	for _, alert := range alerts {
		if alert.Fingerprint != "" {
			s.alerts.Store(alert.Fingerprint, alert)
		}
	}

	return nil
}

// CleanupResolvedAlerts 清理已解决的告警
func (s *AlertStorage) CleanupResolvedAlerts(olderThan time.Duration) int {
	cutoff := time.Now().Add(-olderThan)
	count := 0

	s.alerts.Range(func(key, value interface{}) bool {
		alert, ok := value.(*alert_models.ProcessedAlert)
		if !ok {
			return true
		}

		if alert.Status == "resolved" && alert.ProcessedAt.Before(cutoff) {
			s.alerts.Delete(key)
			count++
		}

		return true
	})

	if count > 0 {
		s.logger.Debug("Cleaned up resolved alerts", zap.Int("count", count))
	}

	return count
}

// GetStats 获取存储统计信息
func (s *AlertStorage) GetStats() map[string]int64 {
	stats := map[string]int64{
		"total":    0,
		"firing":   0,
		"resolved": 0,
	}

	s.alerts.Range(func(key, value interface{}) bool {
		stats["total"]++
		if alert, ok := value.(*alert_models.ProcessedAlert); ok {
			if alert.Status == "firing" {
				stats["firing"]++
			} else {
				stats["resolved"]++
			}
		}
		return true
	})

	return stats
}
