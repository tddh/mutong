package controllers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
)

type MonitoringController struct {
	logger         *zap.Logger
	metricsQuerier interfaces.MetricsQuerier
}

func NewMonitoringController(logger *zap.Logger, querier interfaces.MetricsQuerier) *MonitoringController {
	return &MonitoringController{
		logger:         logger,
		metricsQuerier: querier,
	}
}

func (c *MonitoringController) RegisterRoutes(app *gin.Engine) {
	api := app.Group("/api/v1/monitoring")
	api.GET("/metrics/:resourceType", c.GetResourceMetrics)
	api.GET("/timeseries", c.GetMetricTimeseries)
	api.GET("/health", c.GetSystemHealth)
	api.GET("/catalog", c.GetMetricCatalog)
}

func (c *MonitoringController) GetResourceMetrics(ctx *gin.Context) {
	resourceType := ctx.Param("resourceType")
	namespace := ctx.Query("namespace")
	name := ctx.Query("name")

	if namespace == "" || name == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "namespace and name are required"})
		return
	}

	var results []interfaces.MetricDataPoint
	var err error

	switch resourceType {
	case "Pod":
		results, err = c.metricsQuerier.GetPodMetrics(ctx.Request.Context(), namespace, name)
	case "Node":
		results, err = c.metricsQuerier.GetNodeMetrics(ctx.Request.Context(), name)
	case "Deployment":
		results, err = c.metricsQuerier.GetDeploymentMetrics(ctx.Request.Context(), namespace, name)
	case "StatefulSet":
		results, err = c.metricsQuerier.GetStatefulSetMetrics(ctx.Request.Context(), namespace, name)
	case "DaemonSet":
		results, err = c.metricsQuerier.GetDaemonSetMetrics(ctx.Request.Context(), namespace, name)
	case "Service":
		results, err = c.metricsQuerier.GetServiceMetrics(ctx.Request.Context(), namespace, name)
	default:
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "unsupported resource type: " + resourceType})
		return
	}

	if err != nil {
		c.logger.Error("Failed to get metrics", zap.Error(err), zap.String("resourceType", resourceType), zap.String("namespace", namespace), zap.String("name", name))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, results)
}

func (c *MonitoringController) GetMetricTimeseries(ctx *gin.Context) {
	expr := ctx.Query("expr")
	if expr == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "expr is required"})
		return
	}

	now := time.Now()
	start := parseInt64Param(ctx, "start", now.Add(-1*time.Hour).Unix())
	end := parseInt64Param(ctx, "end", now.Unix())
	step := parseInt64Param(ctx, "step", 60)

	results, err := c.metricsQuerier.QueryMetricTimeseries(ctx.Request.Context(), expr, start, end, step)
	if err != nil {
		c.logger.Error("Failed to query timeseries", zap.Error(err), zap.String("expr", expr))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, results)
}

func (c *MonitoringController) GetSystemHealth(ctx *gin.Context) {
	if c.metricsQuerier == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  "metrics querier not configured",
		})
		return
	}

	err := c.metricsQuerier.CheckHealth(ctx.Request.Context())
	if err != nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status": "healthy",
	})
}

func (c *MonitoringController) GetMetricCatalog(ctx *gin.Context) {
	catalog, err := c.metricsQuerier.GetMetricCatalog(ctx.Request.Context(), true, false)
	if err != nil {
		c.logger.Error("Failed to get metric catalog", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, catalog)
}

func parseInt64Param(ctx *gin.Context, name string, defaultValue int64) int64 {
	val := ctx.Query(name)
	if val == "" {
		return defaultValue
	}
	i, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return defaultValue
	}
	return i
}
