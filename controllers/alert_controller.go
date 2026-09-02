package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	alert_interfaces "gitee.com/tddh/mutong/interfaces/alert"
	alert_models "gitee.com/tddh/mutong/models/alert"
)

// AlertController 告警控制器
// 处理 Prometheus Alertmanager Webhook 请求和外部系统告警
type AlertController struct {
	logger       *zap.Logger
	processor    alert_interfaces.AlertProcessor
	alertSources map[string]*alert_models.AlertSource
}

// NewAlertController 创建告警控制器实例
func NewAlertController(
	logger *zap.Logger,
	processor alert_interfaces.AlertProcessor,
	alertSources map[string]*alert_models.AlertSource,
) *AlertController {
	return &AlertController{
		logger:       logger,
		processor:    processor,
		alertSources: alertSources,
	}
}

// HandleWebhook 处理 Alertmanager Webhook 请求
// POST /api/v1/alerts/webhook
func (c *AlertController) HandleWebhook(ctx *gin.Context) {
	var payload alert_models.WebhookPayload
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		c.logger.Error("Failed to parse webhook payload", zap.Error(err))
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid payload format",
		})
		return
	}

	c.logger.Debug("Received alert webhook",
		zap.String("groupKey", payload.GroupKey),
		zap.String("status", payload.Status),
		zap.Int("alertCount", len(payload.Alerts)))

	// 处理告警
	processedAlerts, err := c.processor.Process(ctx.Request.Context(), &payload)
	if err != nil {
		c.logger.Error("Failed to process alerts", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to process alerts",
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":    "success",
		"received":  len(payload.Alerts),
		"processed": len(processedAlerts),
	})
}

// GetActiveAlerts 获取活跃告警列表
// GET /api/v1/alerts
func (c *AlertController) GetActiveAlerts(ctx *gin.Context) {
	filters := make(map[string]string)

	// 从查询参数提取过滤器
	if namespace := ctx.Query("namespace"); namespace != "" {
		filters["namespace"] = namespace
	}
	if resourceType := ctx.Query("resourceType"); resourceType != "" {
		filters["resourceType"] = resourceType
	}
	if severity := ctx.Query("severity"); severity != "" {
		filters["severity"] = severity
	}
	if nodeName := ctx.Query("nodeName"); nodeName != "" {
		filters["nodeName"] = nodeName
	}
	if status := ctx.Query("status"); status != "" {
		filters["status"] = status
	}

	alerts, err := c.processor.GetActiveAlerts(ctx.Request.Context(), filters)
	if err != nil {
		c.logger.Error("Failed to get active alerts", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get active alerts",
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"alerts": alerts,
		"count":  len(alerts),
	})
}

// GetAlertByFingerprint 根据指纹获取告警详情
// GET /api/v1/alerts/{fingerprint}
func (c *AlertController) GetAlertByFingerprint(ctx *gin.Context) {
	fingerprint := ctx.Param("fingerprint")
	if fingerprint == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "Fingerprint is required",
		})
		return
	}

	alert, err := c.processor.GetAlertByFingerprint(ctx.Request.Context(), fingerprint)
	if err != nil {
		c.logger.Error("Failed to get alert", zap.String("fingerprint", fingerprint), zap.Error(err))
		ctx.JSON(http.StatusNotFound, gin.H{
			"error": "Alert not found",
		})
		return
	}

	ctx.JSON(http.StatusOK, alert)
}

// HealthCheck 健康检查端点
// GET /api/v1/alerts/health
func (c *AlertController) HealthCheck(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"status": "healthy",
	})
}

// HandleExternalWebhook 处理外部系统告警
// POST /api/v1/alerts/external/:source
func (c *AlertController) HandleExternalWebhook(ctx *gin.Context) {
	sourceName := ctx.Param("source")
	if sourceName == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "source parameter is required"})
		return
	}

	source, ok := c.alertSources[sourceName]
	if !ok {
		c.logger.Warn("Unknown alert source",
			zap.String("source", sourceName),
			zap.Strings("available", c.availableSourceNames()))
		ctx.JSON(http.StatusNotFound, gin.H{
			"error":            fmt.Sprintf("unknown alert source: %s", sourceName),
			"availableSources": c.availableSourceNames(),
		})
		return
	}

	var rawPayload map[string]interface{}
	if err := ctx.ShouldBindJSON(&rawPayload); err != nil {
		c.logger.Error("Failed to parse external alert payload",
			zap.String("source", sourceName),
			zap.Error(err))
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON payload"})
		return
	}

	c.logger.Info("Received external alert",
		zap.String("source", sourceName),
		zap.String("type", source.Type),
		zap.String("strategy", string(source.EnrichmentStrategy)))

	alert := source.MapToAlert(rawPayload)

	processedAlerts, err := c.processor.ProcessExternal(
		ctx.Request.Context(), alert, source,
	)
	if err != nil {
		c.logger.Error("Failed to process external alert",
			zap.String("source", sourceName),
			zap.String("fingerprint", alert.Fingerprint),
			zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to process alert",
		})
		return
	}

	c.logger.Info("External alert processed",
		zap.String("source", sourceName),
		zap.String("fingerprint", alert.Fingerprint),
		zap.Int("processed", len(processedAlerts)))

	ctx.JSON(http.StatusOK, gin.H{
		"status":      "success",
		"source":      sourceName,
		"fingerprint": alert.Fingerprint,
		"processed":   len(processedAlerts),
	})
}

// ListAlertSources 列出所有已注册的外部告警源
// GET /api/v1/alerts/sources
func (c *AlertController) ListAlertSources(ctx *gin.Context) {
	type SourceInfo struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		DisplayName string `json:"displayName"`
		Strategy    string `json:"strategy"`
	}
	sources := make([]SourceInfo, 0, len(c.alertSources))
	for _, s := range c.alertSources {
		sources = append(sources, SourceInfo{
			Name:        s.Name,
			Type:        s.Type,
			DisplayName: s.DisplayName,
			Strategy:    string(s.EnrichmentStrategy),
		})
	}
	ctx.JSON(http.StatusOK, gin.H{
		"sources": sources,
		"count":   len(sources),
	})
}

// availableSourceNames 返回所有已注册的告警源名称
func (c *AlertController) availableSourceNames() []string {
	names := make([]string, 0, len(c.alertSources))
	for name := range c.alertSources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RegisterRoutes 注册路由
// 重要: 具体路径必须在通配符路由之前注册
func (c *AlertController) RegisterRoutes(app *gin.Engine) {
	alerts := app.Group("/api/v1/alerts")
	{
		// 具体路径优先注册
		alerts.POST("/webhook", c.HandleWebhook)
		alerts.POST("/external/:source", c.HandleExternalWebhook)
		alerts.GET("", c.GetActiveAlerts)
		alerts.GET("/health", c.HealthCheck)
		alerts.GET("/sources", c.ListAlertSources)
		// 通配符路由最后注册
		alerts.GET("/:fingerprint", c.GetAlertByFingerprint)
	}
	app.GET("/metrics", gin.WrapH(promhttp.Handler()))
}

// AlertWebhookPayload 用于调试的 Webhook Payload 结构
type AlertWebhookPayload struct {
	Receiver          string            `json:"receiver"`
	Status            string            `json:"status"`
	Alerts            []json.RawMessage `json:"alerts"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
}
