package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
)

// LogController provides Elasticsearch/Pod log related endpoints
type LogController struct {
	logger     *zap.Logger
	logQuerier interfaces.LogQuerier
}

func NewLogController(logger *zap.Logger, logQuerier interfaces.LogQuerier) *LogController {
	return &LogController{logger: logger, logQuerier: logQuerier}
}

func (c *LogController) RegisterRoutes(app *gin.Engine) {
	api := app.Group("/api/v1")
	api.GET("/logs/pod", c.getPodLogs)
	api.GET("/logs/search", c.searchLogs)
	api.GET("/logs/errors", c.getErrorLogs)
	api.GET("/logs/warn", c.getWarnLogs)
	api.GET("/logs/info", c.getInfoLogs)
}

// getPodLogs returns logs for a specific pod
func (c *LogController) getPodLogs(ctx *gin.Context) {
	ns := ctx.Query("namespace")
	name := ctx.Query("name")
	if ns == "" || name == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "namespace and name are required"})
		return
	}
	container := ctx.Query("container")
	since := QueryIntDefault(ctx, "since", 15)
	tail := QueryIntDefault(ctx, "tail", 50)
	logs, err := c.logQuerier.GetPodLogs(ctx.Request.Context(), ns, name, container, since, tail)
	if err != nil {
		c.logger.Error("Failed to fetch pod logs", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, logs)
}

// searchLogs searches across logs
func (c *LogController) searchLogs(ctx *gin.Context) {
	ns := ctx.Query("namespace")
	query := ctx.Query("query")
	if query == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "query parameter is required"})
		return
	}
	since := QueryIntDefault(ctx, "since", 15)
	max := QueryIntDefault(ctx, "max", 50)
	logs, err := c.logQuerier.SearchLogs(ctx.Request.Context(), query, ns, since, max)
	if err != nil {
		c.logger.Error("Failed to search logs", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, logs)
}

// getErrorLogs returns error logs
func (c *LogController) getErrorLogs(ctx *gin.Context) {
	ns := ctx.Query("namespace")
	since := QueryIntDefault(ctx, "since", 15)
	max := QueryIntDefault(ctx, "max", 50)
	logs, err := c.logQuerier.GetErrorLogs(ctx.Request.Context(), ns, since)
	if err != nil {
		c.logger.Error("Failed to fetch error logs", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_ = max // the interface uses only since in current method; keep compatibility
	ctx.JSON(http.StatusOK, logs)
}

// getWarnLogs returns warning logs
func (c *LogController) getWarnLogs(ctx *gin.Context) {
	ns := ctx.Query("namespace")
	since := QueryIntDefault(ctx, "since", 15)
	logs, err := c.logQuerier.GetWarnLogs(ctx.Request.Context(), ns, since)
	if err != nil {
		c.logger.Error("Failed to fetch warn logs", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, logs)
}

// getInfoLogs returns info logs
func (c *LogController) getInfoLogs(ctx *gin.Context) {
	ns := ctx.Query("namespace")
	since := QueryIntDefault(ctx, "since", 15)
	logs, err := c.logQuerier.GetInfoLogs(ctx.Request.Context(), ns, since)
	if err != nil {
		c.logger.Error("Failed to fetch info logs", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, logs)
}
