package interfaces

import (
	"context"
	"time"

	"gitee.com/tddh/mutong/models/alert"
)

// AlertProcessor 告警处理器接口
type AlertProcessor interface {
	// Process 处理 Alertmanager Webhook 告警
	Process(ctx context.Context, payload *alert.WebhookPayload) ([]*alert.ProcessedAlert, error)

	// ProcessExternal 处理外部系统告警
	ProcessExternal(ctx context.Context, a *alert.Alert, source *alert.AlertSource) ([]*alert.ProcessedAlert, error)

	// GetActiveAlerts 获取活跃告警
	GetActiveAlerts(ctx context.Context, filters map[string]string) ([]*alert.ProcessedAlert, error)
	// GetAlertByFingerprint 根据指纹获取告警
	GetAlertByFingerprint(ctx context.Context, fingerprint string) (*alert.ProcessedAlert, error)
}

// AlertEnricher 告警富化器接口（从拓扑图中获取上下文）
type AlertEnricher interface {
	// Enrich 富化告警信息
	Enrich(ctx context.Context, a *alert.Alert) (*alert.EnrichedAlert, error)
}

// AlertSuppressor 告警抑制器接口
type AlertSuppressor interface {
	// CheckSuppression 检查告警是否应该被抑制
	CheckSuppression(ctx context.Context, a *alert.EnrichedAlert) (*alert.SuppressionResult, error)
}

// AlertRouter 告警路由器接口
type AlertRouter interface {
	// Route 计算告警的路由信息
	Route(ctx context.Context, a *alert.EnrichedAlert) (*alert.RoutingResult, error)
}

// AlertNotifier 告警通知器接口
type AlertNotifier interface {
	// Notify 发送告警通知
	Notify(ctx context.Context, a *alert.ProcessedAlert) error
}

// AlertStorage 告警存储接口
type AlertStorage interface {
	// Save 保存告警
	Save(ctx context.Context, a *alert.ProcessedAlert) error
	// GetActiveAlerts 获取活跃告警
	GetActiveAlerts(ctx context.Context, filters map[string]string) ([]*alert.ProcessedAlert, error)
	// GetAlertByFingerprint 根据指纹获取告警
	GetAlertByFingerprint(ctx context.Context, fingerprint string) (*alert.ProcessedAlert, error)
	// CleanupResolvedAlerts 清理已解决的告警
	CleanupResolvedAlerts(olderThan time.Duration) int
	// GetStats 获取存储统计信息
	GetStats() map[string]int64
}
