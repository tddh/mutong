package controllers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	alert_svc "gitee.com/tddh/mutong/services/alert"
	diagnosis_svc "gitee.com/tddh/mutong/services/diagnosis"
)

type StatusController struct {
	logger       *zap.Logger
	diagEngine   *diagnosis_svc.Engine
	alertService *alert_svc.AlertService
	graphDB      interfaces.GraphDB
}

func NewStatusController(
	logger *zap.Logger,
	diagEngine *diagnosis_svc.Engine,
	alertService *alert_svc.AlertService,
	graphDB interfaces.GraphDB,
) *StatusController {
	return &StatusController{
		logger:       logger,
		diagEngine:   diagEngine,
		alertService: alertService,
		graphDB:      graphDB,
	}
}

func (c *StatusController) RegisterRoutes(app *gin.Engine) {
	api := app.Group("/api/v1")
	api.GET("/diagnosis/status", c.GetDiagnosisStatus)
	api.GET("/alerts/suppression/status", c.GetSuppressionStatus)
	api.GET("/system/status", c.GetSystemStatus)
}

func (c *StatusController) GetDiagnosisStatus(ctx *gin.Context) {
	if c.diagEngine == nil {
		ctx.JSON(http.StatusOK, gin.H{
			"llm":       gin.H{"configured": false, "available": false},
			"diagnosis": gin.H{"totalDiagnoses": 0, "llmCalls": 0, "ruleOnlyCalls": 0},
		})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"llm":       c.diagEngine.GetLLMStatus(),
		"diagnosis": c.diagEngine.GetDiagnosisStats(),
	})
}

func (c *StatusController) GetSuppressionStatus(ctx *gin.Context) {
	if c.alertService == nil {
		ctx.JSON(http.StatusOK, gin.H{"ruleEngine": gin.H{"enabled": false}})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"ruleEngine": c.alertService.GetSuppressionStatus(),
	})
}

func (c *StatusController) GetSystemStatus(ctx *gin.Context) {
	result := gin.H{}

	if c.diagEngine != nil {
		result["diagnosis"] = gin.H{
			"llm":       c.diagEngine.GetLLMStatus(),
			"diagnosis": c.diagEngine.GetDiagnosisStats(),
		}
	} else {
		result["diagnosis"] = gin.H{
			"llm":       gin.H{"configured": false, "available": false},
			"diagnosis": gin.H{"totalDiagnoses": 0, "llmCalls": 0, "ruleOnlyCalls": 0},
		}
	}

	if c.alertService != nil {
		alertStatus := c.alertService.GetStatus()
		supStatus := c.alertService.GetSuppressionStatus()
		result["alerts"] = gin.H{
			"metrics":     alertStatus.Metrics,
			"suppression": supStatus,
		}
	} else {
		result["alerts"] = gin.H{
			"metrics":     gin.H{"receivedTotal": 0, "suppressedTotal": 0, "notifiedTotal": 0, "activeFiring": 0, "activeResolved": 0},
			"suppression": gin.H{"ruleEngineEnabled": false},
		}
	}

	if c.graphDB != nil {
		_, cancel := context.WithTimeout(ctx.Request.Context(), 3*time.Second)
		defer cancel()
		_, err := c.graphDB.Execute("SHOW SPACES;")
		result["infrastructure"] = gin.H{"nebula": gin.H{"available": err == nil}}
	} else {
		result["infrastructure"] = gin.H{"nebula": gin.H{"available": false}}
	}

	ctx.JSON(http.StatusOK, result)
}
