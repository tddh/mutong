package controllers

import (
	"net/http"
	"strconv"

	"gitee.com/tddh/mutong/interfaces"
	"github.com/gin-gonic/gin"
)

// TraceController exposes API endpoints for tracing data
type TraceController struct {
	traceQuerier interfaces.TraceQuerier
}

func NewTraceController(tq interfaces.TraceQuerier) *TraceController {
	return &TraceController{traceQuerier: tq}
}

// RegisterRoutes registers the HTTP endpoints for tracing
func (c *TraceController) RegisterRoutes(app *gin.Engine) {
	// Base path is /api/v1/trace
	g := app.Group("/api/v1/trace")
	g.GET("/query", c.Query)
	g.GET("/spans", c.Spans)
	g.GET("/services", c.Services)
}

// Query handles GET /api/v1/trace/query?serviceName=&operationName=&traceId=
func (c *TraceController) Query(ctx *gin.Context) {
	serviceName := ctx.Query("serviceName")
	operationName := ctx.Query("operationName")
	traceId := ctx.Query("traceId")
	// Limit can be specified, default to 100
	limit := 100
	if l := ctx.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}

	traces, err := c.traceQuerier.QueryTraces(serviceName, operationName, traceId, limit)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, traces)
}

// Spans handles GET /api/v1/trace/spans?traceId=
func (c *TraceController) Spans(ctx *gin.Context) {
	traceId := ctx.Query("traceId")
	spans, err := c.traceQuerier.GetSpans(traceId)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, spans)
}

// Services handles GET /api/v1/trace/services
func (c *TraceController) Services(ctx *gin.Context) {
	services, err := c.traceQuerier.GetServices()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, services)
}
