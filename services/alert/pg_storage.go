package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"gitee.com/tddh/mutong/interfaces"
	alert_interfaces "gitee.com/tddh/mutong/interfaces/alert"
	alert_models "gitee.com/tddh/mutong/models/alert"
)

type AlertModel struct {
	ID             uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Fingerprint    string     `gorm:"type:varchar(64);uniqueIndex;not null" json:"fingerprint"`
	Status         string     `gorm:"type:varchar(16);index" json:"status"`
	AlertName      string     `gorm:"type:varchar(128);index" json:"alert_name"`
	Namespace      string     `gorm:"type:varchar(128);index" json:"namespace"`
	ResourceType   string     `gorm:"type:varchar(64)" json:"resource_type"`
	ResourceName   string     `gorm:"type:varchar(256)" json:"resource_name"`
	NodeName       string     `gorm:"type:varchar(256)" json:"node_name"`
	Severity       string     `gorm:"type:varchar(16);index" json:"severity"`
	Labels         string     `gorm:"type:json" json:"labels"`
	Annotations    string     `gorm:"type:json" json:"annotations"`
	TopologyPath   string     `gorm:"type:json" json:"topology_path"`
	Suppression    string     `gorm:"type:json" json:"suppression"`
	Routing        string     `gorm:"type:json" json:"routing"`
	EnrichTags     string     `gorm:"type:json" json:"enrich_tags"`
	BusinessCtx    string     `gorm:"type:json" json:"business_ctx"`
	BusinessCalls  string     `gorm:"type:json" json:"business_calls"`
	BusinessImpact string     `gorm:"type:json" json:"business_impact"`
	StartsAt       time.Time  `gorm:"index" json:"starts_at"`
	EndsAt         *time.Time `json:"ends_at"`
	ProcessedAt    time.Time  `gorm:"index" json:"processed_at"`
	RuleChainID    string     `gorm:"type:varchar(128)" json:"rule_chain_id"`
	CreatedAt      time.Time  `gorm:"autoCreateTime;index" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (AlertModel) TableName() string {
	return "alerts"
}

type PostgresAlertStorage struct {
	logger interfaces.Logger
	db     *gorm.DB
}

func NewPostgresAlertStorage(logger interfaces.Logger, db *gorm.DB) (alert_interfaces.AlertStorage, error) {
	if db == nil {
		return nil, fmt.Errorf("gorm.DB is required for PostgresAlertStorage")
	}

	storage := &PostgresAlertStorage{
		logger: logger,
		db:     db,
	}

	if err := db.AutoMigrate(&AlertModel{}); err != nil {
		return nil, fmt.Errorf("failed to auto migrate alerts table: %w", err)
	}

	logger.Info("PostgresAlertStorage initialized")
	return storage, nil
}

func (s *PostgresAlertStorage) Save(ctx context.Context, a *alert_models.ProcessedAlert) error {
	if a.Fingerprint == "" {
		return fmt.Errorf("alert fingerprint is required")
	}

	a.ProcessedAt = time.Now()

	labelsJSON, _ := json.Marshal(a.Labels)
	annotationsJSON, _ := json.Marshal(a.Annotations)
	topologyPathJSON, _ := json.Marshal(a.TopologyPath)
	suppressionJSON, _ := json.Marshal(a.Suppression)
	routingJSON, _ := json.Marshal(a.Routing)
	enrichTagsJSON, _ := json.Marshal(a.EnrichTags)
	businessCtxJSON, _ := json.Marshal(a.BusinessContext)
	bizCallsJSON, _ := json.Marshal(a.BusinessCalls)
	bizImpactJSON, _ := json.Marshal(a.BusinessImpact)

	model := AlertModel{
		Fingerprint:    a.Fingerprint,
		Status:         a.Status,
		AlertName:      a.Labels["alertname"],
		Namespace:      a.Namespace,
		ResourceType:   a.ResourceType,
		ResourceName:   a.ResourceName,
		NodeName:       a.NodeName,
		Severity:       a.Routing.Severity,
		Labels:         string(labelsJSON),
		Annotations:    string(annotationsJSON),
		TopologyPath:   string(topologyPathJSON),
		Suppression:    string(suppressionJSON),
		Routing:        string(routingJSON),
		EnrichTags:     string(enrichTagsJSON),
		BusinessCtx:    string(businessCtxJSON),
		BusinessCalls:  string(bizCallsJSON),
		BusinessImpact: string(bizImpactJSON),
		StartsAt:       a.StartsAt,
		ProcessedAt:    a.ProcessedAt,
		RuleChainID:    a.RuleChainID,
	}

	if !a.EndsAt.IsZero() {
		model.EndsAt = &a.EndsAt
	}

	result := s.db.WithContext(ctx).Where("fingerprint = ?", a.Fingerprint).
		Assign(model).
		FirstOrCreate(&model)

	if result.Error != nil {
		s.logger.Error("Failed to save alert to PostgreSQL",
			zap.String("fingerprint", a.Fingerprint),
			zap.Error(result.Error))
		return fmt.Errorf("failed to save alert: %w", result.Error)
	}

	s.logger.Info("Alert saved to PostgreSQL",
		zap.String("fingerprint", a.Fingerprint),
		zap.String("status", a.Status))

	return nil
}

func (s *PostgresAlertStorage) GetActiveAlerts(ctx context.Context, filters map[string]string) ([]*alert_models.ProcessedAlert, error) {
	// status 过滤：缺省 firing；"all" 表示不过滤（含已恢复）
	status := "firing"
	if v, ok := filters["status"]; ok && v != "" {
		status = v
	}
	query := s.db.WithContext(ctx)
	if status != "all" {
		query = query.Where("status = ?", status)
	}

	for key, value := range filters {
		switch key {
		case "status":
			// 已处理
		case "namespace":
			query = query.Where("namespace = ?", value)
		case "resourceType":
			query = query.Where("resource_type = ?", value)
		case "severity":
			query = query.Where("severity = ?", value)
		case "nodeName":
			query = query.Where("node_name = ?", value)
		default:
			query = query.Where("labels ->> ? = ?", key, value)
		}
	}

	var models []AlertModel
	if err := query.Order("processed_at DESC").Find(&models).Error; err != nil {
		s.logger.Error("Failed to get active alerts from PostgreSQL", zap.Error(err))
		return nil, fmt.Errorf("failed to get active alerts: %w", err)
	}

	alerts := make([]*alert_models.ProcessedAlert, 0, len(models))
	for _, m := range models {
		alert, err := s.modelToProcessedAlert(m)
		if err != nil {
			s.logger.Warn("Failed to convert alert model",
				zap.String("fingerprint", m.Fingerprint),
				zap.Error(err))
			continue
		}
		alerts = append(alerts, alert)
	}

	return alerts, nil
}

func (s *PostgresAlertStorage) GetAlertByFingerprint(ctx context.Context, fingerprint string) (*alert_models.ProcessedAlert, error) {
	var model AlertModel
	if err := s.db.WithContext(ctx).Where("fingerprint = ?", fingerprint).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("alert not found: %s", fingerprint)
		}
		s.logger.Error("Failed to get alert by fingerprint",
			zap.String("fingerprint", fingerprint),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get alert: %w", err)
	}

	return s.modelToProcessedAlert(model)
}

func (s *PostgresAlertStorage) CleanupResolvedAlerts(olderThan time.Duration) int {
	cutoff := time.Now().Add(-olderThan)

	result := s.db.Where("status = ? AND processed_at < ?", "resolved", cutoff).Delete(&AlertModel{})
	if result.Error != nil {
		s.logger.Error("Failed to cleanup resolved alerts", zap.Error(result.Error))
		return 0
	}

	count := int(result.RowsAffected)
	if count > 0 {
		s.logger.Info("Cleaned up resolved alerts from PostgreSQL", zap.Int("count", count))
	}

	return count
}

func (s *PostgresAlertStorage) GetStats() map[string]int64 {
	var total, firing, resolved int64

	s.db.Model(&AlertModel{}).Count(&total)
	s.db.Model(&AlertModel{}).Where("status = ?", "firing").Count(&firing)
	s.db.Model(&AlertModel{}).Where("status = ?", "resolved").Count(&resolved)

	return map[string]int64{
		"total":    total,
		"firing":   firing,
		"resolved": resolved,
	}
}

func (s *PostgresAlertStorage) modelToProcessedAlert(m AlertModel) (*alert_models.ProcessedAlert, error) {
	alert := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			Alert: alert_models.Alert{
				Status:      m.Status,
				StartsAt:    m.StartsAt,
				Fingerprint: m.Fingerprint,
			},
			ResourceType: m.ResourceType,
			ResourceName: m.ResourceName,
			Namespace:    m.Namespace,
			NodeName:     m.NodeName,
		},
		ProcessedAt: m.ProcessedAt,
		RuleChainID: m.RuleChainID,
	}

	if m.EndsAt != nil {
		alert.EndsAt = *m.EndsAt
	}

	if err := json.Unmarshal([]byte(m.Labels), &alert.Labels); err != nil {
		return nil, fmt.Errorf("failed to unmarshal labels: %w", err)
	}
	if err := json.Unmarshal([]byte(m.Annotations), &alert.Annotations); err != nil {
		return nil, fmt.Errorf("failed to unmarshal annotations: %w", err)
	}
	if err := json.Unmarshal([]byte(m.TopologyPath), &alert.TopologyPath); err != nil {
		return nil, fmt.Errorf("failed to unmarshal topology_path: %w", err)
	}
	if err := json.Unmarshal([]byte(m.Suppression), &alert.Suppression); err != nil {
		return nil, fmt.Errorf("failed to unmarshal suppression: %w", err)
	}
	if err := json.Unmarshal([]byte(m.Routing), &alert.Routing); err != nil {
		return nil, fmt.Errorf("failed to unmarshal routing: %w", err)
	}
	if m.EnrichTags != "" {
		if err := json.Unmarshal([]byte(m.EnrichTags), &alert.EnrichTags); err != nil {
			alert.EnrichTags = make(map[string]string)
		}
	}
	if alert.EnrichTags == nil {
		alert.EnrichTags = make(map[string]string)
	}

	if m.BusinessCtx != "" {
		_ = json.Unmarshal([]byte(m.BusinessCtx), &alert.BusinessContext)
	}
	if m.BusinessCalls != "" {
		_ = json.Unmarshal([]byte(m.BusinessCalls), &alert.BusinessCalls)
	}
	if m.BusinessImpact != "" {
		_ = json.Unmarshal([]byte(m.BusinessImpact), &alert.BusinessImpact)
	}

	return alert, nil
}
