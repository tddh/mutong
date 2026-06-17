package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	alert_interfaces "gitee.com/tddh/mutong/interfaces/alert"
	alert_models "gitee.com/tddh/mutong/models/alert"
	"gitee.com/tddh/mutong/services/httpclient"
)

// AlertNotifier 告警通知器
// 支持多种通知渠道：Slack、PagerDuty、钉钉、邮件等
type AlertNotifier struct {
	logger     interfaces.Logger
	notifier   NotifierConfig
	httpClient *http.Client
}

// NotifierConfig 通知器配置
type NotifierConfig struct {
	SlackWebhookURL string               // Slack Webhook URL
	PagerDutyAPIKey string               // PagerDuty API Key
	DingTalkWebhook string               // 钉钉 Webhook URL
	EmailConfig     *EmailNotifierConfig // 邮件配置
	ChannelEnabled  map[string]bool      // 渠道启用状态
}

// EmailNotifierConfig 邮件通知配置
type EmailNotifierConfig struct {
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	FromAddress  string
}

// NewAlertNotifier 创建告警通知器实例
func NewAlertNotifier(logger interfaces.Logger, config NotifierConfig) alert_interfaces.AlertNotifier {
	return &AlertNotifier{
		logger:     logger,
		notifier:   config,
		httpClient: httpclient.New(10 * time.Second),
	}
}

// Notify 发送告警通知
func (n *AlertNotifier) Notify(ctx context.Context, a *alert_models.ProcessedAlert) error {
	channel := a.Routing.NotifyChannel

	if enabled, exists := n.notifier.ChannelEnabled[channel]; exists && !enabled {
		n.logger.Debug("Notification channel disabled", zap.String("channel", channel))
		return nil
	}

	if err := n.sendByChannel(ctx, a, channel); err != nil {
		return err
	}

	for _, notice := range a.Routing.StakeholderNotices {
		n.logger.Debug("sending stakeholder notice",
			zap.String("team", notice.Team),
			zap.String("channel", notice.Channel),
			zap.String("fingerprint", a.Fingerprint))
		if err := n.sendStakeholderByChannel(ctx, a, notice); err != nil {
			n.logger.Warn("failed to send stakeholder notice",
				zap.String("team", notice.Team), zap.Error(err))
		}
	}
	return nil
}

func (n *AlertNotifier) sendByChannel(ctx context.Context, a *alert_models.ProcessedAlert, channel string) error {
	switch channel {
	case "slack":
		return n.notifySlack(ctx, a)
	case "pagerduty":
		return n.notifyPagerDuty(ctx, a)
	case "dingtalk":
		return n.notifyDingTalk(ctx, a)
	case "email":
		return n.notifyEmail(ctx, a)
	default:
		n.logger.Warn("Unknown notification channel", zap.String("channel", channel))
		return nil
	}
}

func (n *AlertNotifier) sendStakeholderByChannel(ctx context.Context, a *alert_models.ProcessedAlert, notice alert_models.StakeholderNotice) error {
	switch notice.Channel {
	case "slack":
		return n.notifySlackStakeholder(ctx, a, notice)
	default:
		return n.notifySlackStakeholder(ctx, a, notice)
	}
}

func (n *AlertNotifier) notifySlackStakeholder(ctx context.Context, a *alert_models.ProcessedAlert, notice alert_models.StakeholderNotice) error {
	if n.notifier.SlackWebhookURL == "" {
		return nil
	}
	payload := map[string]interface{}{
		"attachments": []map[string]interface{}{{
			"color": "warning",
			"title": fmt.Sprintf("业务影响通知 - %s", notice.Team),
			"text": fmt.Sprintf("影响原因: %s\n原始告警: %s\n告警级别: %s\n对您的影响: %s\n负责团队: 已在处理中",
				notice.Reason, a.Labels["alertname"], a.Routing.Severity, notice.Severity),
			"footer": fmt.Sprintf("Mutong Alert System • %s", a.ProcessedAt.Format(time.RFC3339)),
		}},
	}
	return n.sendWebhook(n.notifier.SlackWebhookURL, payload)
}

// notifySlack 发送 Slack 通知
func (n *AlertNotifier) notifySlack(ctx context.Context, a *alert_models.ProcessedAlert) error {
	if n.notifier.SlackWebhookURL == "" {
		return fmt.Errorf("slack webhook URL not configured")
	}

	color := "warning"
	if a.Routing.Severity == "critical" {
		color = "danger"
	} else if a.Routing.Severity == "info" {
		color = "good"
	}

	payload := map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"color":  color,
				"title":  fmt.Sprintf("[%s] %s", a.Status, a.Labels["alertname"]),
				"text":   a.Annotations["summary"],
				"fields": n.buildSlackFields(a),
				"footer": fmt.Sprintf("Mutong Alert System • %s", a.ProcessedAt.Format(time.RFC3339)),
			},
		},
	}

	return n.sendWebhook(n.notifier.SlackWebhookURL, payload)
}

// buildSlackFields 构建 Slack 附件字段
func (n *AlertNotifier) buildSlackFields(a *alert_models.ProcessedAlert) []map[string]string {
	fields := []map[string]string{}

	if a.Namespace != "" {
		fields = append(fields, map[string]string{"title": "Namespace", "value": a.Namespace, "short": "true"})
	}
	if a.ResourceType != "" && a.ResourceName != "" {
		fields = append(fields, map[string]string{"title": "Resource", "value": fmt.Sprintf("%s/%s", a.ResourceType, a.ResourceName), "short": "true"})
	}
	if a.NodeName != "" {
		fields = append(fields, map[string]string{"title": "Node", "value": a.NodeName, "short": "true"})
	}
	if a.Routing.Severity != "" {
		fields = append(fields, map[string]string{"title": "Severity", "value": a.Routing.Severity, "short": "true"})
	}

	return fields
}

// notifyPagerDuty 发送 PagerDuty 通知
func (n *AlertNotifier) notifyPagerDuty(ctx context.Context, a *alert_models.ProcessedAlert) error {
	if n.notifier.PagerDutyAPIKey == "" {
		return fmt.Errorf("pagerduty API key not configured")
	}

	eventAction := "trigger"
	if a.Status == "resolved" {
		eventAction = "resolve"
	}

	payload := map[string]interface{}{
		"routing_key":  n.notifier.PagerDutyAPIKey,
		"event_action": eventAction,
		"dedup_key":    a.Fingerprint,
		"payload": map[string]interface{}{
			"summary":   fmt.Sprintf("[%s] %s", a.Status, a.Labels["alertname"]),
			"severity":  a.Routing.Severity,
			"source":    a.ResourceName,
			"component": a.ResourceType,
			"custom_details": map[string]interface{}{
				"namespace":    a.Namespace,
				"nodeName":     a.NodeName,
				"annotations":  a.Annotations,
				"topologyPath": a.TopologyPath,
			},
		},
	}

	return n.sendWebhook("https://events.pagerduty.com/v2/enqueue", payload)
}

// notifyDingTalk 发送钉钉通知
// severityEmoji 根据告警严重性返回对应的表情符号
func (n *AlertNotifier) severityEmoji(sev string) string {
	switch strings.ToLower(sev) {
	case "critical":
		return "🔴"
	case "warning":
		return "🟠"
	case "info":
		return "🟡"
	default:
		return "🔶"
	}
}

// notifyDingTalk 发送钉钉通知
func (n *AlertNotifier) notifyDingTalk(ctx context.Context, a *alert_models.ProcessedAlert) error {
	if n.notifier.DingTalkWebhook == "" {
		return fmt.Errorf("dingtalk webhook URL not configured")
	}

	// 构建丰富的 Markdown 内容
	emoji := n.severityEmoji(a.Routing.Severity)
	// 使用状态+告警名作为主标题
	header := fmt.Sprintf("%s **[%s] %s**", emoji, a.Status, a.Labels["alertname"])

	var textBuilder strings.Builder
	textBuilder.WriteString(fmt.Sprintf("%s\n\n", header))

	// Topology path 以箭头分隔（Node → Pod → Deployment）
	if len(a.TopologyPath) > 0 {
		topology := strings.Join(a.TopologyPath, " → ")
		textBuilder.WriteString(fmt.Sprintf("**Topology:** %s\n", topology))
	}
	// Namespace / Resource / Node
	if a.Namespace != "" {
		textBuilder.WriteString(fmt.Sprintf("**Namespace:** %s\n", a.Namespace))
	}
	if a.ResourceName != "" {
		textBuilder.WriteString(fmt.Sprintf("**Resource:** %s/%s\n", a.ResourceType, a.ResourceName))
	}
	if a.NodeName != "" {
		textBuilder.WriteString(fmt.Sprintf("**Node:** %s\n", a.NodeName))
	}
	// Owner 信息
	if a.OwnerKind != "" || a.OwnerName != "" {
		textBuilder.WriteString(fmt.Sprintf("**Owner:** %s/%s\n", a.OwnerKind, a.OwnerName))
	}
	// Related alerts count
	if len(a.RelatedAlerts) > 0 {
		textBuilder.WriteString(fmt.Sprintf("**Related Alerts:** %d\n", len(a.RelatedAlerts)))
	} else {
		textBuilder.WriteString("**Related Alerts:** 0\n")
	}
	// Enriched tags
	if len(a.EnrichTags) > 0 {
		textBuilder.WriteString("\n**Tags:**\n")
		// 按键名排序以保持输出稳定性
		keys := make([]string, 0, len(a.EnrichTags))
		for k := range a.EnrichTags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := a.EnrichTags[k]
			textBuilder.WriteString(fmt.Sprintf("%s: %s\n", k, v))
		}
	}
	// Routing 信息
	if a.Routing.Severity != "" {
		textBuilder.WriteString(fmt.Sprintf("\n**Severity:** %s\n", a.Routing.Severity))
	}
	if a.Routing.NotifyChannel != "" || a.Routing.Receiver != "" {
		textBuilder.WriteString(fmt.Sprintf("**Routing:** Channel=%s, Receiver=%s\n", a.Routing.NotifyChannel, a.Routing.Receiver))
	}
	// ProcessedAt 时区信息
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		textBuilder.WriteString(fmt.Sprintf("**Processed At (Asia/Shanghai):** %s\n", a.ProcessedAt.In(loc).Format("2006-01-02 15:04:05 MST")))
	} else {
		textBuilder.WriteString(fmt.Sprintf("**Processed At:** %s\n", a.ProcessedAt.Format(time.RFC3339)))
	}
	// Fingerprint
	if a.Fingerprint != "" {
		textBuilder.WriteString(fmt.Sprintf("\nFingerprint: %s\n", a.Fingerprint))
	}

	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": fmt.Sprintf("[%s] %s", a.Status, a.Labels["alertname"]),
			"text":  textBuilder.String(),
		},
	}

	return n.sendWebhook(n.notifier.DingTalkWebhook, payload)
}

// notifyEmail 发送邮件通知
func (n *AlertNotifier) notifyEmail(ctx context.Context, a *alert_models.ProcessedAlert) error {
	if n.notifier.EmailConfig == nil {
		return fmt.Errorf("email notifier not configured")
	}

	cfg := n.notifier.EmailConfig
	if cfg.SMTPHost == "" || cfg.SMTPPort == 0 {
		return fmt.Errorf("email SMTP host or port not configured")
	}

	if len(a.Routing.NotifyUsers) == 0 {
		n.logger.Debug("No email recipients configured, skipping email notification",
			zap.String("fingerprint", a.Fingerprint))
		return nil
	}

	subject := fmt.Sprintf("[%s] %s - %s", a.Status, a.Routing.Severity, a.Labels["alertname"])
	textBody := n.buildEmailBody(a)

	from := cfg.FromAddress
	if from == "" {
		from = cfg.SMTPUser
	}

	headers := make(map[string]string)
	headers["From"] = from
	headers["To"] = strings.Join(a.Routing.NotifyUsers, ",")
	headers["Subject"] = subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/plain; charset=UTF-8"
	headers["Date"] = time.Now().Format(time.RFC1123Z)

	var msg strings.Builder
	for k, v := range headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	msg.WriteString("\r\n")
	msg.WriteString(textBody)

	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	recipients := a.Routing.NotifyUsers

	if cfg.SMTPPort == 465 {
		return fmt.Errorf("SMTPS (port 465) not supported, use port 587 with STARTTLS")
	}

	var auth smtp.Auth
	if cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPassword, cfg.SMTPHost)
	}

	if err := smtp.SendMail(addr, auth, from, recipients, []byte(msg.String())); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	n.logger.Debug("Email notification sent",
		zap.String("fingerprint", a.Fingerprint),
		zap.Strings("recipients", recipients))

	return nil
}

func (n *AlertNotifier) buildEmailBody(a *alert_models.ProcessedAlert) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Alert Status: %s\n", a.Status))
	sb.WriteString(fmt.Sprintf("Severity: %s\n", a.Routing.Severity))

	if a.Labels != nil {
		if name, ok := a.Labels["alertname"]; ok {
			sb.WriteString(fmt.Sprintf("Alert Name: %s\n", name))
		}
	}

	if a.Namespace != "" {
		sb.WriteString(fmt.Sprintf("Namespace: %s\n", a.Namespace))
	}
	if a.ResourceType != "" && a.ResourceName != "" {
		sb.WriteString(fmt.Sprintf("Resource: %s/%s\n", a.ResourceType, a.ResourceName))
	}
	if a.NodeName != "" {
		sb.WriteString(fmt.Sprintf("Node: %s\n", a.NodeName))
	}

	if a.Annotations != nil {
		if summary, ok := a.Annotations["summary"]; ok && summary != "" {
			sb.WriteString(fmt.Sprintf("\nSummary:\n%s\n", summary))
		}
	}

	sb.WriteString(fmt.Sprintf("\n---\nFingerprint: %s\nProcessed at: %s\n", a.Fingerprint, a.ProcessedAt.Format(time.RFC3339)))

	return sb.String()
}

// sendWebhook 发送 Webhook 请求
func (n *AlertNotifier) sendWebhook(url string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// sendAggregatedAlerts sends a summarized aggregated alert across configured channels.
func (n *AlertNotifier) sendAggregatedAlerts(groups []*AlertGroup) error {
	if len(groups) == 0 {
		return nil
	}

	// Build a concise textual summary for all groups
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Aggregated alerts: %d group(s)\n", len(groups)))
	for _, g := range groups {
		if g == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("- %s: %d alert(s) in %s (first: %s, last: %s)\n",
			g.ID, len(g.Alerts), g.Namespace,
			g.FirstSeen.Format(time.RFC3339), g.LastSeen.Format(time.RFC3339)))
	}
	payload := map[string]interface{}{
		"text": sb.String(),
	}

	// Slack (if configured)
	if n.notifier.SlackWebhookURL != "" {
		_ = n.sendWebhook(n.notifier.SlackWebhookURL, payload)
	}

	// DingTalk (if configured) - reuse same text payload
	if n.notifier.DingTalkWebhook != "" {
		payloadDT := map[string]interface{}{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"title": "Aggregated Mutong Alerts",
				"text":  sb.String(),
			},
		}
		_ = n.sendWebhook(n.notifier.DingTalkWebhook, payloadDT)
	}

	// PagerDuty (if configured)
	if n.notifier.PagerDutyAPIKey != "" {
		payloadPD := map[string]interface{}{
			"routing_key":  n.notifier.PagerDutyAPIKey,
			"event_action": "trigger",
			"dedup_key":    "aggregated_mutong",
			"payload": map[string]interface{}{
				"summary": fmt.Sprintf("Aggregated Alerts: %d groups", len(groups)),
				"details": sb.String(),
			},
		}
		_ = n.sendWebhook("https://events.pagerduty.com/v2/enqueue", payloadPD)
	}

	return nil
}
