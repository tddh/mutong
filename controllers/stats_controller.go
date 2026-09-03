package controllers

import (
	"fmt"

	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/services"
)

type StatsController struct {
	logger    *zap.Logger
	graphDB   interfaces.GraphDB
	svc       *services.K8sResoureService
	sanitizer *services.NGQLSanitizer
}

func NewStatsController(logger *zap.Logger, db interfaces.GraphDB, svc *services.K8sResoureService) *StatsController {
	return &StatsController{
		logger:    logger,
		graphDB:   db,
		svc:       svc,
		sanitizer: services.NewNGQLSanitizer(),
	}
}

func (c *StatsController) GetOverview(ctx *gin.Context) {
	stats := make(map[string]interface{})
	stats["resources"] = c.getResourceCounts()
	stats["alerts"] = map[string]int64{"active": 0}
	stats["inspection"] = map[string]int64{"issues": 0}
	ctx.JSON(http.StatusOK, stats)
}

func (c *StatsController) getResourceCounts() map[string]int {
	counts := make(map[string]int)
	total := 0

	queryKinds := []struct {
		kind string
		key  string
	}{
		{"Pod", "pods"},
		{"Node", "nodes"},
		{"Service", "services"},
		{"Deployment", "deployments"},
	}

	for _, m := range queryKinds {
		safeKind, ok := services.WhitelistKind(m.kind)
		if !ok {
			c.logger.Warn("Stats: rejected invalid kind", zap.String("kind", m.kind))
			continue
		}

		query := fmt.Sprintf(
			"MATCH (v:K8sResource{kind:%s,is_deleted:false}) RETURN count(v) as count",
			c.sanitizer.QuoteString(safeKind),
		)

		res, err := c.graphDB.Execute(query)
		if err != nil {
			c.logger.Warn("Stats query error", zap.Error(err), zap.String("kind", m.kind))
			continue
		}
		if res.GetRowSize() > 0 {
			row, _ := res.GetRowValuesByIndex(0)
			val, _ := row.GetValueByColName("count")
			if i, err := val.AsInt(); err == nil {
				count := int(i)
				counts[m.key] = count
				total += count
			}
		}
	}

	counts["total"] = total
	return counts
}

func (c *StatsController) SyncResources(ctx *gin.Context) {
	c.logger.Info("Manual DB sync triggered")
	if c.svc != nil {
		go c.svc.ForceSyncResources()
		ctx.JSON(http.StatusOK, map[string]string{"status": "sync started"})
	} else {
		ctx.JSON(http.StatusOK, map[string]string{"status": "error"})
	}
}

func (c *StatsController) BackfillRelationships(ctx *gin.Context) {
	c.logger.Info("Manual relationship backfill triggered")
	if c.svc != nil {
		go c.svc.BackfillRelationshipsByKinds([]string{"FlowSchema", "CiliumEndpoint", "CustomResourceDefinition", "APIService"})
		ctx.JSON(http.StatusOK, map[string]string{"status": "backfill started"})
	} else {
		ctx.JSON(http.StatusOK, map[string]string{"status": "error"})
	}
}

func (c *StatsController) GetResourceTotal(ctx *gin.Context) {
	query := "LOOKUP ON K8sResource WHERE K8sResource.is_deleted == false YIELD id(vertex) AS vid"
	res, err := c.graphDB.Execute(query)
	if err != nil {
		c.logger.Warn("Resource total query error", zap.Error(err))
		ctx.JSON(http.StatusOK, map[string]int{"total": 0})
		return
	}
	ctx.JSON(http.StatusOK, map[string]int{"total": res.GetRowSize()})
}

func (c *StatsController) RegisterRoutes(app *gin.Engine) {
	stats := app.Group("/api/v1/stats")
	stats.GET("/overview", c.GetOverview)
	stats.GET("/resource-total", c.GetResourceTotal)
	stats.POST("/sync", c.SyncResources)
	stats.POST("/backfill-relationships", c.BackfillRelationships)
}
