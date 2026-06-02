package controllers

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/config"
	diagModel "gitee.com/tddh/mutong/models/diagnosis"
	exModel "gitee.com/tddh/mutong/models/executor"
	diagEngine "gitee.com/tddh/mutong/services/diagnosis"
	executorpkg "gitee.com/tddh/mutong/services/executor"
)

// ExecutorController exposes endpoints to drive the K8s executor
type ExecutorController struct {
	logger *zap.Logger
	exec   *executorpkg.K8sExecutor
	cfg    *config.Config
	engine *diagEngine.Engine
}

func NewExecutorController(logger *zap.Logger, exec *executorpkg.K8sExecutor, cfg *config.Config, diagEng *diagEngine.Engine) *ExecutorController {
	return &ExecutorController{logger: logger, exec: exec, cfg: cfg, engine: diagEng}
}

// SetDiagnosisEngine wires the diagnosis engine after initialization
func (c *ExecutorController) SetDiagnosisEngine(engine *diagEngine.Engine) {
	c.engine = engine
}

func (c *ExecutorController) RegisterRoutes(app *gin.Engine) {
	api := app.Group("/api/v1/executor")
	api.GET("/status", c.status)
	api.GET("/audit", c.auditLogs)
	api.POST("/execute", c.execute)
	api.POST("/toggle", c.toggleAutoMode)
	api.POST("/diagnose-and-execute", c.diagnoseAndExecute)
}

// requireAdminKey checks for admin-level authorization for dangerous operations.
// Admin key is the same as MUTONG_API_KEY or a separate MUTONG_ADMIN_KEY if set.
// This provides defense-in-depth: even with a valid API key, the admin key
// must be present for destructive operations.
func (c *ExecutorController) requireAdminKey(ctx *gin.Context) bool {
	adminKey := os.Getenv("MUTONG_ADMIN_KEY")
	if adminKey == "" {
		return true
	}
	token := ctx.GetHeader("X-Admin-Key")
	if token == adminKey {
		return true
	}
	ctx.JSON(http.StatusForbidden, gin.H{"error": "admin key required for this operation"})
	return false
}

// execute submits an ExecutionPlan for the executor
func (c *ExecutorController) execute(ctx *gin.Context) {
	if !c.requireAdminKey(ctx) {
		return
	}
	var plan exModel.ExecutionPlan
	if err := ctx.ShouldBindJSON(&plan); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := c.exec.Execute(ctx.Request.Context(), plan)
	if err != nil {
		c.logger.Error("Executor execution failed", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, res)
}

// status returns executor status information
func (c *ExecutorController) status(ctx *gin.Context) {
	enabled := false
	auditLogType := "memory"
	if c.cfg != nil {
		enabled = c.cfg.GetExecutorConf().Enabled
		auditLogType = c.cfg.GetExecutorConf().AuditLog.Type
		if auditLogType == "" {
			auditLogType = "memory"
		}
	}
	autoMode := c.exec.IsAutoMode()
	logs, _ := c.exec.GetAuditLogs(ctx.Request.Context(), map[string]string{})
	ctx.JSON(http.StatusOK, gin.H{
		"enabled":       enabled,
		"autoMode":      autoMode,
		"auditLogCount": len(logs),
		"auditLogType":  auditLogType,
	})
}

// auditLogs returns audit logs with optional filters
func (c *ExecutorController) auditLogs(ctx *gin.Context) {
	filters := map[string]string{}
	if v := ctx.Query("action"); v != "" {
		filters["action"] = v
	}
	if v := ctx.Query("namespace"); v != "" {
		filters["namespace"] = v
	}
	if v := ctx.Query("autoExecuted"); v != "" {
		filters["autoExecuted"] = v
	}
	logs, err := c.exec.GetAuditLogs(ctx.Request.Context(), filters)
	if err != nil {
		c.logger.Error("Failed to fetch audit logs", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, logs)
}

// toggleAutoMode toggles the executor's auto mode
func (c *ExecutorController) toggleAutoMode(ctx *gin.Context) {
	if !c.requireAdminKey(ctx) {
		return
	}
	var payload struct {
		AutoMode bool `json:"autoMode"`
	}
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.exec.SetAutoMode(payload.AutoMode)
	ctx.JSON(http.StatusOK, gin.H{"autoMode": payload.AutoMode})
}

// diagnoseAndExecute runs a diagnosis request, creates an execution plan and
// optionally executes it via the embedded executor.
func (c *ExecutorController) diagnoseAndExecute(ctx *gin.Context) {
	if !c.requireAdminKey(ctx) {
		return
	}
	if !c.cfg.GetExecutorConf().Enabled {
		ctx.JSON(http.StatusForbidden, gin.H{"error": "executor is not enabled"})
		return
	}

	var req diagModel.DiagnosisRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// ensure we have a diagnosis engine
	if c.engine == nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "diagnosis engine not configured"})
		return
	}

	// Run diagnosis
	result, err := c.engine.Diagnose(ctx.Request.Context(), req)
	if err != nil {
		c.logger.Error("Diagnosis failed", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	bridge := executorpkg.NewRemediationBridge()
	plan, shouldAuto := bridge.CreatePlanFromDiagnosis(result, c.exec.IsAutoMode())
	var execRes *exModel.ExecutionResult
	if plan != nil && shouldAuto {
		execRes, err = bridge.ExecuteRemediation(ctx.Request.Context(), plan, c.exec)
		if err != nil {
			c.logger.Error("Auto remediation execution failed", zap.Error(err))
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "diagnosis": result})
			return
		}
	}

	ctx.JSON(http.StatusOK, gin.H{
		"diagnosis": result,
		"execution": execRes,
	})
}
