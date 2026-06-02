package alert

import (
	"context"
	"strings"

	"gitee.com/tddh/mutong/interfaces"
	alert_interfaces "gitee.com/tddh/mutong/interfaces/alert"
	alert_models "gitee.com/tddh/mutong/models/alert"
)

// AlertRouter 告警路由器
// 根据告警标签（severity, team, service等）决定告警的路由信息
// 路由决策：接收器、通知渠道、优先级、通知人员/组
type AlertRouter struct {
	logger interfaces.Logger
	config RoutingConfig
}

// RoutingConfig 路由配置
type RoutingConfig struct {
	DefaultReceiver        string
	DefaultChannel         string
	DefaultSeverity        string
	SeverityMapping        map[string]int
	TeamRouting            map[string]string
	ServiceRouting         map[string]string
	SeverityChannelRules   map[string]string
	BusinessContextRouting map[string]string
	ImpactRouting          ImpactRoutingConfig
}

// ImpactRoutingConfig stakeholder通知配置
type ImpactRoutingConfig struct {
	Enabled                    bool
	MinOwnerSeverity           string
	DefaultStakeholderChannel  string
	StakeholderDefaultReceiver string
}

// NewAlertRouter 创建告警路由器实例
func NewAlertRouter(logger interfaces.Logger, config RoutingConfig) alert_interfaces.AlertRouter {
	if config.DefaultReceiver == "" {
		config.DefaultReceiver = "default"
	}
	if config.DefaultChannel == "" {
		config.DefaultChannel = "slack"
	}
	if config.DefaultSeverity == "" {
		config.DefaultSeverity = "warning"
	}
	if config.SeverityMapping == nil {
		config.SeverityMapping = map[string]int{
			"critical": 1,
			"error":    2,
			"warning":  3,
			"info":     4,
		}
	}

	return &AlertRouter{
		logger: logger,
		config: config,
	}
}

// Route 计算告警的路由信息
func (r *AlertRouter) Route(ctx context.Context, a *alert_models.EnrichedAlert) (*alert_models.RoutingResult, error) {
	result := &alert_models.RoutingResult{
		Receiver:      r.config.DefaultReceiver,
		NotifyChannel: r.config.DefaultChannel,
		Severity:      r.determineSeverity(a),
		Priority:      r.calculatePriority(a),
		NotifyUsers:   r.extractNotifyUsers(a),
		NotifyGroups:  r.extractNotifyGroups(a),
	}

	if team, ok := a.Labels["team"]; ok {
		if receiver, exists := r.config.TeamRouting[team]; exists {
			result.Receiver = receiver
		}
	}
	if result.Receiver == r.config.DefaultReceiver {
		if bizTeam := a.BusinessContext.Team; bizTeam != "" {
			if receiver, exists := r.config.BusinessContextRouting[bizTeam]; exists {
				result.Receiver = receiver
			}
		}
	}
	if service, ok := a.Labels["service"]; ok {
		if receiver, exists := r.config.ServiceRouting[service]; exists {
			result.Receiver = receiver
		}
	}
	if channel, exists := r.config.SeverityChannelRules[result.Severity]; exists {
		result.NotifyChannel = channel
	} else if result.Severity == "critical" {
		result.NotifyChannel = "pagerduty"
	}

	// Stakeholder routing
	if r.config.ImpactRouting.Enabled && len(a.Stakeholders) > 0 {
		if severityToInt(result.Severity) <= severityToInt(r.config.ImpactRouting.MinOwnerSeverity) {
			for _, s := range a.Stakeholders {
				notice := alert_models.StakeholderNotice{
					Team:     s.Team,
					Channel:  r.config.ImpactRouting.DefaultStakeholderChannel,
					Reason:   s.Reason,
					Severity: r.downgradeForStakeholder(result.Severity),
				}
				if receiver, ok := r.config.TeamRouting[s.Team]; ok {
					notice.Receiver = receiver
				} else if receiver, ok := r.config.BusinessContextRouting[s.Team]; ok {
					notice.Receiver = receiver
				} else {
					notice.Receiver = r.config.ImpactRouting.StakeholderDefaultReceiver
				}
				result.StakeholderNotices = append(result.StakeholderNotices, notice)
			}
			if len(result.StakeholderNotices) >= 5 {
				result.StakeholderNotices = r.aggregateNotices(result.StakeholderNotices)
			}
		}
	}

	return result, nil
}

func (r *AlertRouter) downgradeForStakeholder(ownerSev string) string {
	switch ownerSev {
	case "critical":
		return "error"
	case "error":
		return "warning"
	default:
		return "info"
	}
}

func (r *AlertRouter) aggregateNotices(notices []alert_models.StakeholderNotice) []alert_models.StakeholderNotice {
	teams := make([]string, len(notices))
	for i, n := range notices {
		teams[i] = n.Team
	}
	return []alert_models.StakeholderNotice{{
		Team:     strings.Join(teams, ","),
		Channel:  notices[0].Channel,
		Receiver: r.config.ImpactRouting.StakeholderDefaultReceiver,
		Reason:   "汇总通知: " + strings.Join(teams, ", ") + " 受同一告警影响",
		Severity: notices[0].Severity,
	}}
}

func severityToInt(s string) int {
	switch strings.ToLower(s) {
	case "critical":
		return 1
	case "error":
		return 2
	case "warning":
		return 3
	default:
		return 4
	}
}

// determineSeverity 确定告警严重级别
func (r *AlertRouter) determineSeverity(a *alert_models.EnrichedAlert) string {
	// 从标签中获取严重级别
	if severity, ok := a.Labels["severity"]; ok {
		return severity
	}

	// 从富化标签中获取
	if severity, ok := a.EnrichTags["severity"]; ok {
		return severity
	}

	return r.config.DefaultSeverity
}

// calculatePriority 计算告警优先级（数字越小越优先）
func (r *AlertRouter) calculatePriority(a *alert_models.EnrichedAlert) int {
	severity := r.determineSeverity(a)

	if priority, exists := r.config.SeverityMapping[severity]; exists {
		return priority
	}

	// 默认优先级
	return 5
}

// extractNotifyUsers 从告警标签中提取通知人员
func (r *AlertRouter) extractNotifyUsers(a *alert_models.EnrichedAlert) []string {
	var users []string

	if notifyUser, ok := a.Labels["notify_user"]; ok {
		users = append(users, notifyUser)
	}

	if notifyUsers, ok := a.Labels["notify_users"]; ok {
		users = append(users, notifyUsers)
	}

	return users
}

// extractNotifyGroups 从告警标签中提取通知组
func (r *AlertRouter) extractNotifyGroups(a *alert_models.EnrichedAlert) []string {
	var groups []string

	if notifyGroup, ok := a.Labels["notify_group"]; ok {
		groups = append(groups, notifyGroup)
	}

	if notifyGroups, ok := a.Labels["notify_groups"]; ok {
		groups = append(groups, notifyGroups)
	}

	return groups
}
