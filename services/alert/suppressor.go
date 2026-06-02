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

type AlertSuppressor struct {
	logger           interfaces.Logger
	graphDB          interfaces.GraphDB
	activeAlerts     map[string]*alert_models.EnrichedAlert
	activeMu         sync.RWMutex
	bizAppAlertIndex map[string]map[string]struct{}
	indexMu          sync.RWMutex
	config           SuppressionConfig
	lastCleanup      time.Time
}

// SuppressorStatus 表示抑制器状态
type SuppressorStatus struct {
	RuleEngineEnabled     bool     `json:"ruleEngineEnabled"`
	RuleChainID           string   `json:"ruleChainId,omitempty"`
	TimeWindowSeconds     int      `json:"timeWindowSeconds"`
	MaxDepth              int      `json:"maxDepth"`
	ActiveAlertsCount     int      `json:"activeAlertsCount"`
	SeverityExceptions    []string `json:"severityExceptions"`
	CriticalityExceptions []string `json:"criticalityExceptions"`
}

// GetStatus 返回当前抑制器状态信息
func (s *AlertSuppressor) GetStatus() SuppressorStatus {
	s.activeMu.RLock()
	count := len(s.activeAlerts)
	s.activeMu.RUnlock()

	status := SuppressorStatus{
		TimeWindowSeconds:     s.config.TimeWindowSeconds,
		MaxDepth:              s.config.MaxDepth,
		ActiveAlertsCount:     count,
		SeverityExceptions:    s.config.SeverityExceptions,
		CriticalityExceptions: s.config.CriticalityExceptions,
	}

	return status
}

// SuppressionConfig 抑制配置
type SuppressionConfig struct {
	TimeWindowSeconds     int      // 时间窗口 (秒)
	MaxDepth              int      // 最大抑制深度
	SeverityExceptions    []string // 不抑制的严重级别
	CriticalityExceptions []string // 不抑制的业务关键级别 (critical/P0)
	MaxAlertAgeMinutes    int      // 活跃告警最大保留时间 (分钟)，0=不限制
	CleanupIntervalSec    int      // 过期告警清理间隔 (秒)，0=默认60
}

// NewAlertSuppressor 创建告警抑制器实例
func NewAlertSuppressor(
	logger interfaces.Logger,
	graphDB interfaces.GraphDB,
	ruleChainDefs []string,
	config SuppressionConfig,
) (alert_interfaces.AlertSuppressor, error) {
	suppressor := &AlertSuppressor{
		logger:           logger,
		graphDB:          graphDB,
		activeAlerts:     make(map[string]*alert_models.EnrichedAlert),
		bizAppAlertIndex: make(map[string]map[string]struct{}),
		config:           config,
	}

	// Apply defaults for zero values
	if suppressor.config.TimeWindowSeconds <= 0 {
		suppressor.config.TimeWindowSeconds = 300
	}
	if suppressor.config.MaxAlertAgeMinutes <= 0 {
		suppressor.config.MaxAlertAgeMinutes = 60
	}
	if suppressor.config.CleanupIntervalSec <= 0 {
		suppressor.config.CleanupIntervalSec = 60
	}
	if suppressor.config.SeverityExceptions == nil {
		suppressor.config.SeverityExceptions = []string{}
	}
	if suppressor.config.CriticalityExceptions == nil {
		suppressor.config.CriticalityExceptions = []string{}
	}

	return suppressor, nil
}

// WithConfig 设置抑制配置
func (s *AlertSuppressor) WithConfig(config SuppressionConfig) *AlertSuppressor {
	if config.TimeWindowSeconds > 0 {
		s.config.TimeWindowSeconds = config.TimeWindowSeconds
	}
	s.config.MaxDepth = config.MaxDepth
	s.config.SeverityExceptions = config.SeverityExceptions
	return s
}

// CheckSuppression 检查告警是否应该被抑制
func (s *AlertSuppressor) CheckSuppression(ctx context.Context, a *alert_models.EnrichedAlert) (*alert_models.SuppressionResult, error) {
	s.evictStaleAlerts()

	result := &alert_models.SuppressionResult{
		IsSuppressed: false,
		SuppressedAt: time.Now(),
	}

	s.updateActiveAlerts(a)

	for _, exc := range s.config.SeverityExceptions {
		if a.EnrichTags["severity"] == exc {
			return result, nil
		}
	}

	// 业务关键级别告警跳过拓扑抑制
	if s.isCriticalityException(a) {
		return result, nil
	}

	if suppressed, reason := s.checkTopologySuppression(a); suppressed {
		result.IsSuppressed = true
		result.SuppressionReason = reason
		return result, nil
	}

	if suppressed, reason := s.checkCausalSuppression(a); suppressed {
		result.IsSuppressed = true
		result.SuppressionReason = reason
		s.logger.Debug("alert suppressed by causal chain",
			zap.String("fingerprint", a.Fingerprint),
			zap.String("reason", reason))
		return result, nil
	}

	return result, nil
}

// checkTopologySuppression 检查拓扑抑制规则
func (s *AlertSuppressor) checkTopologySuppression(a *alert_models.EnrichedAlert) (bool, string) {
	path := ParseTopologyPath(a.TopologyPath)
	if len(path) == 0 {
		return false, ""
	}

	cutoff := time.Now().Add(-time.Duration(s.config.TimeWindowSeconds) * time.Second)
	maxDepth := s.config.MaxDepth
	if maxDepth <= 0 || maxDepth > len(path)-1 {
		maxDepth = len(path) - 1
	}

	for i := 0; i < maxDepth; i++ {
		parent := path[i]
		parentAlerts := s.getParentAlerts(parent.Type, parent.Name)
		for _, parentAlert := range parentAlerts {
			if parentAlert.Status != "firing" {
				continue
			}
			if parentAlert.StartsAt.Before(cutoff) {
				continue
			}
			reason := fmt.Sprintf("被父节点告警抑制: %s:%s (告警: %s)",
				parent.Type, parent.Name, parentAlert.Labels["alertname"])
			return true, reason
		}
	}

	return false, ""
}

// isCriticalityException 检查告警是否属于业务关键级别
func (s *AlertSuppressor) isCriticalityException(a *alert_models.EnrichedAlert) bool {
	criticality := a.BusinessContext.Criticality
	if criticality == "" {
		criticality = a.EnrichTags["criticality"]
	}
	if criticality == "" {
		return false
	}
	for _, exc := range s.config.CriticalityExceptions {
		if criticality == exc {
			return true
		}
	}
	return false
}

// getParentAlerts 获取父节点的活跃告警
func (s *AlertSuppressor) getParentAlerts(resourceType, resourceName string) []*alert_models.EnrichedAlert {
	s.activeMu.RLock()
	defer s.activeMu.RUnlock()
	var alerts []*alert_models.EnrichedAlert
	for _, alert := range s.activeAlerts {
		if (resourceType == "Node" && alert.NodeName == resourceName) ||
			(alert.ResourceType == resourceType && alert.ResourceName == resourceName) {
			alerts = append(alerts, alert)
		}
	}
	return alerts
}

// updateActiveAlerts 更新活跃告警缓存
func (s *AlertSuppressor) updateActiveAlerts(a *alert_models.EnrichedAlert) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if a.Status == "firing" {
		s.activeAlerts[a.Fingerprint] = a
		s.indexByBusinessApp(a)
	} else if a.Status == "resolved" {
		s.removeFromBizAppIndex(a)
		delete(s.activeAlerts, a.Fingerprint)
	}
}

func (s *AlertSuppressor) indexByBusinessApp(a *alert_models.EnrichedAlert) {
	appName := a.BusinessContext.AppName
	if appName == "" {
		return
	}
	key := appName + "|" + a.BusinessContext.Namespace
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	if s.bizAppAlertIndex[key] == nil {
		s.bizAppAlertIndex[key] = make(map[string]struct{})
	}
	s.bizAppAlertIndex[key][a.Fingerprint] = struct{}{}
}

func (s *AlertSuppressor) removeFromBizAppIndex(a *alert_models.EnrichedAlert) {
	appName := a.BusinessContext.AppName
	if appName == "" {
		return
	}
	key := appName + "|" + a.BusinessContext.Namespace
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	delete(s.bizAppAlertIndex[key], a.Fingerprint)
	if len(s.bizAppAlertIndex[key]) == 0 {
		delete(s.bizAppAlertIndex, key)
	}
}

func (s *AlertSuppressor) checkCausalSuppression(a *alert_models.EnrichedAlert) (bool, string) {
	if a.BusinessContext.AppName == "" || a.BusinessCalls == nil || len(a.BusinessCalls.Downstreams) == 0 {
		return false, ""
	}
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()
	for _, ds := range a.BusinessCalls.Downstreams {
		key := ds.AppName + "|" + a.BusinessContext.Namespace
		if fps, ok := s.bizAppAlertIndex[key]; ok && len(fps) > 0 {
			return true, fmt.Sprintf("causal: downstream %s is firing", ds.AppName)
		}
	}
	return false, ""
}

func (s *AlertSuppressor) evictStaleAlerts() {
	maxAge := s.config.MaxAlertAgeMinutes
	if maxAge <= 0 {
		maxAge = 60
	}
	interval := s.config.CleanupIntervalSec
	if interval <= 0 {
		interval = 60
	}

	if time.Since(s.lastCleanup) < time.Duration(interval)*time.Second {
		return
	}

	s.activeMu.Lock()
	defer s.activeMu.Unlock()

	cutoff := time.Now().Add(-time.Duration(maxAge) * time.Minute)
	for fp, alert := range s.activeAlerts {
		if alert.StartsAt.Before(cutoff) {
			delete(s.activeAlerts, fp)
		}
	}
	s.lastCleanup = time.Now()
}

// DefaultSuppressionRuleChain 默认的抑制规则链定义
var DefaultSuppressionRuleChain = `{
	"ruleChain": {
		"id": "alert_suppression",
		"name": "告警抑制规则链",
		"root": true
	},
	"metadata": {
		"nodes": [
			{
				"id": "s1",
				"type": "jsFilter",
				"name": "检查是否为Pod告警",
				"configuration": {
					"jsScript": "return msg.resourceType === 'Pod' && msg.status === 'firing';"
				}
			}
		],
		"connections": []
	}
}`

// SuppressionRuleBuilder 抑制规则构建器
type SuppressionRuleBuilder struct {
	ruleChainID string
	conditions  []SuppressionCondition
}

// SuppressionCondition 抑制条件
type SuppressionCondition struct {
	ParentResourceType string
	ParentAlertNames   []string
	ChildResourceType  string
	ChildAlertNames    []string
}

// NewSuppressionRuleBuilder 创建抑制规则构建器
func NewSuppressionRuleBuilder(ruleChainID string) *SuppressionRuleBuilder {
	return &SuppressionRuleBuilder{
		ruleChainID: ruleChainID,
		conditions:  []SuppressionCondition{},
	}
}

// AddCondition 添加抑制条件
func (b *SuppressionRuleBuilder) AddCondition(
	parentType string,
	parentAlertNames []string,
	childType string,
	childAlertNames []string,
) *SuppressionRuleBuilder {
	b.conditions = append(b.conditions, SuppressionCondition{
		ParentResourceType: parentType,
		ParentAlertNames:   parentAlertNames,
		ChildResourceType:  childType,
		ChildAlertNames:    childAlertNames,
	})
	return b
}

// Build 构建规则链 JSON
func (b *SuppressionRuleBuilder) Build() (string, error) {
	ruleChain := map[string]interface{}{
		"ruleChain": map[string]interface{}{
			"id":   b.ruleChainID,
			"name": "抑制规则链",
			"root": true,
		},
		"metadata": map[string]interface{}{
			"nodes":       b.buildNodes(),
			"connections": b.buildConnections(),
		},
	}

	data, err := json.MarshalIndent(ruleChain, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to build rule chain: %w", err)
	}

	return string(data), nil
}

func (b *SuppressionRuleBuilder) buildNodes() []map[string]interface{} {
	var nodes []map[string]interface{}

	for i, cond := range b.conditions {
		parentNode := map[string]interface{}{
			"id":   fmt.Sprintf("parent_%d", i),
			"type": "jsFilter",
			"name": fmt.Sprintf("检查%s告警", cond.ParentResourceType),
			"configuration": map[string]interface{}{
				"jsScript": fmt.Sprintf(
					"return msg.resourceType === '%s' && msg.status === 'firing';",
					cond.ParentResourceType,
				),
			},
		}
		nodes = append(nodes, parentNode)

		childNode := map[string]interface{}{
			"id":   fmt.Sprintf("child_%d", i),
			"type": "jsFilter",
			"name": fmt.Sprintf("抑制%s告警", cond.ChildResourceType),
			"configuration": map[string]interface{}{
				"jsScript": fmt.Sprintf(
					"return msg.resourceType === '%s' && msg.status === 'firing';",
					cond.ChildResourceType,
				),
			},
		}
		nodes = append(nodes, childNode)
	}

	return nodes
}

func (b *SuppressionRuleBuilder) buildConnections() []map[string]interface{} {
	var connections []map[string]interface{}

	for i := range b.conditions {
		connections = append(connections, map[string]interface{}{
			"fromId": fmt.Sprintf("parent_%d", i),
			"toId":   fmt.Sprintf("child_%d", i),
			"type":   "True",
		})
	}

	return connections
}
