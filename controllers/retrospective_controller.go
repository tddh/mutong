package controllers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/services/retrospective"
)

type RetrospectiveController struct {
	logger  interfaces.Logger
	service *retrospective.Service
}

func NewRetrospectiveController(logger interfaces.Logger, service *retrospective.Service) *RetrospectiveController {
	return &RetrospectiveController{
		logger:  logger,
		service: service,
	}
}

func (c *RetrospectiveController) RegisterRoutes(app *gin.Engine) {
	api := app.Group("/api/v1/retrospective")
	api.GET("/list", c.listPostmortems)
	api.GET("/timeline/:fingerprint", c.getTimeline)
	api.GET("/causal-chain/:fingerprint", c.getCausalChain)
	api.POST("/postmortem/:fingerprint", c.generatePostmortem)
	api.PUT("/postmortem/:fingerprint", c.updatePostmortem)
	api.GET("/postmortem/:fingerprint/text", c.getPostmortemText)
	api.GET("/history/:fingerprint", c.getPostmortemHistory)
	api.GET("/knowledge/search", c.searchKnowledge)
	api.GET("/resource/:resourceUID/postmortems", c.getPostmortemsByResource)
}

func (c *RetrospectiveController) getTimeline(ctx *gin.Context) {
	fingerprint := ctx.Param("fingerprint")
	if fingerprint == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "fingerprint is required"})
		return
	}

	timeline, err := c.service.BuildTimeline(ctx.Request.Context(), fingerprint)
	if err != nil {
		c.logger.Error("Failed to build timeline", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, timeline)
}

func (c *RetrospectiveController) getCausalChain(ctx *gin.Context) {
	fingerprint := ctx.Param("fingerprint")
	if fingerprint == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "fingerprint is required"})
		return
	}

	chain, err := c.service.AnalyzeCausalChain(ctx.Request.Context(), fingerprint)
	if err != nil {
		c.logger.Error("Failed to analyze causal chain", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, chain)
}

func (c *RetrospectiveController) generatePostmortem(ctx *gin.Context) {
	fingerprint := ctx.Param("fingerprint")
	if fingerprint == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "fingerprint is required"})
		return
	}

	report, err := c.service.GeneratePostmortem(ctx.Request.Context(), fingerprint)
	if err != nil {
		c.logger.Error("Failed to generate postmortem", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rc := report.RootCause.Final
	if len(rc) > 80 {
		rc = rc[:80]
	}
	res := report.Resolution.Final
	if len(res) > 80 {
		res = res[:80]
	}
	c.logger.Info("Postmortem generated",
		zap.String("fingerprint", fingerprint),
		zap.String("root_cause", rc),
		zap.String("resolution", res))
	ctx.JSON(http.StatusOK, report)
}

func (c *RetrospectiveController) getPostmortemText(ctx *gin.Context) {
	fingerprint := ctx.Param("fingerprint")
	if fingerprint == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "fingerprint is required"})
		return
	}

	report, err := c.service.GeneratePostmortem(ctx.Request.Context(), fingerprint)
	if err != nil {
		c.logger.Error("Failed to generate postmortem", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.Status(http.StatusOK)
	ctx.Header("Content-Type", "text/markdown")
	ctx.String(http.StatusOK, c.service.FormatReport(report))
}

func (c *RetrospectiveController) getPostmortemHistory(ctx *gin.Context) {
	fingerprint := ctx.Param("fingerprint")
	if fingerprint == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "fingerprint is required"})
		return
	}

	model, err := c.service.GetPostmortemByFingerprint(ctx.Request.Context(), fingerprint)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "postmortem not found"})
		return
	}
	ctx.JSON(http.StatusOK, model)
}

func (c *RetrospectiveController) searchKnowledge(ctx *gin.Context) {
	query := ctx.Query("q")
	faultType := ctx.Query("fault_type")
	resourceKind := ctx.Query("resource_kind")
	resourceUID := ctx.Query("resource_uid")
	namespace := ctx.Query("namespace")
	limit := QueryIntDefault(ctx, "limit", 20)

	results, err := c.service.SearchKnowledge(ctx.Request.Context(), query, faultType, resourceKind, resourceUID, namespace, limit)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"results": results, "count": len(results)})
}

func (c *RetrospectiveController) getPostmortemsByResource(ctx *gin.Context) {
	resourceUID := ctx.Param("resourceUID")
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "10"))

	reports, err := c.service.GetPostmortemsByResourceUID(ctx.Request.Context(), resourceUID, limit)
	if err != nil {
		c.logger.Error("Failed to get postmortems by resource", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"reports": reports, "count": len(reports)})
}

func (c *RetrospectiveController) updatePostmortem(ctx *gin.Context) {
	fingerprint := ctx.Param("fingerprint")
	if fingerprint == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "fingerprint is required"})
		return
	}

	var req retrospective.UpdatePostmortemRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	report, err := c.service.UpdatePostmortem(ctx.Request.Context(), fingerprint, req)
	if err != nil {
		c.logger.Error("Failed to update postmortem", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, report)
}

func (c *RetrospectiveController) listPostmortems(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	if pageSize > 100 {
		pageSize = 100
	}
	if page < 1 {
		page = 1
	}

	filter := retrospective.ListFilter{
		Severity:     ctx.Query("severity"),
		ResourceKind: ctx.Query("resource_kind"),
		ResourceName: ctx.Query("resource_name"),
		Namespace:    ctx.Query("namespace"),
		Keyword:      ctx.Query("keyword"),
		StartTime:    ctx.Query("start_time"),
		EndTime:      ctx.Query("end_time"),
		FaultType:    ctx.Query("fault_type"),
	}

	result, err := c.service.ListPostmortems(ctx.Request.Context(), filter, page, pageSize)
	if err != nil {
		c.logger.Error("Failed to list postmortems", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, result)
}
