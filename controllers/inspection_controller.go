package controllers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	insp_model "gitee.com/tddh/mutong/models/inspection"
	insp_svc "gitee.com/tddh/mutong/services/inspection"
)

type InspectionController struct {
	logger    *zap.Logger
	processor interfaces.InspectionProcessor
	ruleStore *insp_svc.RuleStore
}

func NewInspectionController(logger *zap.Logger, processor interfaces.InspectionProcessor) *InspectionController {
	return &InspectionController{
		logger:    logger,
		processor: processor,
	}
}

func (c *InspectionController) SetRuleStore(store *insp_svc.RuleStore) {
	c.ruleStore = store
}

func (c *InspectionController) ExecuteInspection(ctx *gin.Context) {
	if c.processor == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "inspection service not configured"})
		return
	}

	results, err := c.processor.ExecuteNow(ctx.Request.Context())
	if err != nil {
		c.logger.Error("Failed to execute inspection", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "inspection failed", "detail": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"total":   len(results),
		"results": results,
	})
}

func (c *InspectionController) GetReport(ctx *gin.Context) {
	if c.processor == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "inspection service not configured"})
		return
	}

	report, err := c.processor.GetLatestReport()
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "no report available"})
		return
	}

	ctx.JSON(http.StatusOK, report)
}

func (c *InspectionController) RegisterRoutes(app *gin.Engine) {
	insp := app.Group("/api/v1/inspection")
	{
		insp.POST("/execute", c.ExecuteInspection)
		insp.GET("/report", c.GetReport)
		insp.GET("/reports", c.GetReportHistory)
		insp.GET("/reports/:id", c.GetReportDetail)
		insp.GET("/reports/:id/compare/:other_id", c.CompareReports)
		insp.GET("/trend", c.GetTrend)
	}
	rules := app.Group("/api/v1/inspection/rules")
	{
		rules.GET("", c.listRules)
		rules.POST("", c.createRule)
		rules.PUT("/:id", c.updateRule)
		rules.DELETE("/:id", c.deleteRule)
		rules.POST("/:id/toggle", c.toggleRule)
	}
}

func (c *InspectionController) GetReportHistory(ctx *gin.Context) {
	startStr := ctx.Query("start")
	endStr := ctx.Query("end")
	limit := QueryIntDefault(ctx, "limit", 20)
	offset := QueryIntDefault(ctx, "offset", 0)

	start, _ := time.Parse(time.RFC3339, startStr)
	end, _ := time.Parse(time.RFC3339, endStr)

	if start.IsZero() || end.IsZero() {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "start and end parameters required (RFC3339 format)"})
		return
	}

	reports, err := c.processor.GetReportsByRange(start, end, limit, offset)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"reports": reports,
		"count":   len(reports),
	})
}

func (c *InspectionController) GetReportDetail(ctx *gin.Context) {
	id := ctx.Param("id")
	if id == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	report, err := c.processor.GetReportByID(id)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "report not found"})
		return
	}

	ctx.JSON(http.StatusOK, report)
}

func (c *InspectionController) CompareReports(ctx *gin.Context) {
	id := ctx.Param("id")
	otherID := ctx.Param("other_id")

	if id == "" || otherID == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "both report IDs are required"})
		return
	}

	a, errA := c.processor.GetReportByID(id)
	b, errB := c.processor.GetReportByID(otherID)

	if errA != nil || errB != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "one or both reports not found"})
		return
	}

	comparison := insp_svc.CompareReports(a, b)
	ctx.JSON(http.StatusOK, comparison)
}

func (c *InspectionController) GetTrend(ctx *gin.Context) {
	days := QueryIntDefault(ctx, "days", 30)

	trend, err := c.processor.GetTrendData(days)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"trend": trend,
		"days":  days,
	})
}

func (c *InspectionController) listRules(ctx *gin.Context) {
	if c.ruleStore == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "rule store not available"})
		return
	}
	rules, err := c.ruleStore.List(false)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, rules)
}

func (c *InspectionController) createRule(ctx *gin.Context) {
	if c.ruleStore == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "rule store not available"})
		return
	}
	var rule insp_model.InspectionRuleModel
	if err := ctx.ShouldBindJSON(&rule); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.ruleStore.Create(&rule); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusCreated, rule)
}

func (c *InspectionController) updateRule(ctx *gin.Context) {
	if c.ruleStore == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "rule store not available"})
		return
	}
	id, err := parseUintParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	existing, err := c.ruleStore.Get(id)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "rule not found"})
		return
	}
	if err := ctx.ShouldBindJSON(existing); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.ruleStore.Update(existing); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, existing)
}

func (c *InspectionController) deleteRule(ctx *gin.Context) {
	if c.ruleStore == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "rule store not available"})
		return
	}
	id, err := parseUintParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := c.ruleStore.Delete(id); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (c *InspectionController) toggleRule(ctx *gin.Context) {
	if c.ruleStore == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "rule store not available"})
		return
	}
	id, err := parseUintParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.ruleStore.Toggle(id, body.Enabled); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"status": "toggled", "enabled": body.Enabled})
}

func parseUintParam(ctx *gin.Context, name string) (uint, error) {
	idStr := ctx.Param(name)
	id64, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(id64), nil
}
