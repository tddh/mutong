package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
)

type MetricsController struct {
	logger  *zap.Logger
	querier interfaces.MetricsQuerier
}

func NewMetricsController(logger *zap.Logger, querier interfaces.MetricsQuerier) *MetricsController {
	return &MetricsController{logger: logger, querier: querier}
}

func (c *MetricsController) RegisterRoutes(app *gin.Engine) {
	api := app.Group("/api/v1/metrics")
	api.GET("/pod", c.getPodMetrics)
	api.GET("/node", c.getNodeMetrics)
	api.GET("/deployment", c.getDeploymentMetrics)
	api.GET("/statefulset", c.getStatefulSetMetrics)
	api.GET("/daemonset", c.getDaemonSetMetrics)
	api.GET("/service", c.getServiceMetrics)
}

func (c *MetricsController) getPodMetrics(ctx *gin.Context) {
	if c.querier == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "Prometheus metrics are not configured. Enable prometheus in config.yaml to use this endpoint."})
		return
	}
	namespace := ctx.Query("namespace")
	name := ctx.Query("name")
	if namespace == "" || name == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "namespace and name are required"})
		return
	}
	points, err := c.querier.GetPodMetrics(ctx.Request.Context(), namespace, name)
	if err != nil {
		c.logger.Error("Failed to get pod metrics", zap.Error(err), zap.String("namespace", namespace), zap.String("pod", name))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, points)
}

func (c *MetricsController) getNodeMetrics(ctx *gin.Context) {
	if c.querier == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "Prometheus metrics are not configured. Enable prometheus in config.yaml to use this endpoint."})
		return
	}
	name := ctx.Query("name")
	if name == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	points, err := c.querier.GetNodeMetrics(ctx.Request.Context(), name)
	if err != nil {
		c.logger.Error("Failed to get node metrics", zap.Error(err), zap.String("node", name))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, points)
}

func (c *MetricsController) getDeploymentMetrics(ctx *gin.Context) {
	if c.querier == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "Prometheus metrics are not configured. Enable prometheus in config.yaml to use this endpoint."})
		return
	}
	namespace := ctx.Query("namespace")
	name := ctx.Query("name")
	if namespace == "" || name == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "namespace and name are required"})
		return
	}
	points, err := c.querier.GetDeploymentMetrics(ctx.Request.Context(), namespace, name)
	if err != nil {
		c.logger.Error("Failed to get deployment metrics", zap.Error(err), zap.String("namespace", namespace), zap.String("deployment", name))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, points)
}

func (c *MetricsController) getStatefulSetMetrics(ctx *gin.Context) {
	if c.querier == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "Prometheus metrics are not configured"})
		return
	}
	namespace := ctx.Query("namespace")
	name := ctx.Query("name")
	if namespace == "" || name == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "namespace and name are required"})
		return
	}
	points, err := c.querier.GetStatefulSetMetrics(ctx.Request.Context(), namespace, name)
	if err != nil {
		c.logger.Error("Failed to get statefulset metrics", zap.Error(err), zap.String("namespace", namespace), zap.String("statefulset", name))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, points)
}

func (c *MetricsController) getDaemonSetMetrics(ctx *gin.Context) {
	if c.querier == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "Prometheus metrics are not configured"})
		return
	}
	namespace := ctx.Query("namespace")
	name := ctx.Query("name")
	if namespace == "" || name == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "namespace and name are required"})
		return
	}
	points, err := c.querier.GetDaemonSetMetrics(ctx.Request.Context(), namespace, name)
	if err != nil {
		c.logger.Error("Failed to get daemonset metrics", zap.Error(err), zap.String("namespace", namespace), zap.String("daemonset", name))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, points)
}

func (c *MetricsController) getServiceMetrics(ctx *gin.Context) {
	if c.querier == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "Prometheus metrics are not configured"})
		return
	}
	namespace := ctx.Query("namespace")
	name := ctx.Query("name")
	if namespace == "" || name == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "namespace and name are required"})
		return
	}
	points, err := c.querier.GetServiceMetrics(ctx.Request.Context(), namespace, name)
	if err != nil {
		c.logger.Error("Failed to get service metrics", zap.Error(err), zap.String("namespace", namespace), zap.String("service", name))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, points)
}
