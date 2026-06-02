package controllers

import (
	"net/http"

	"gitee.com/tddh/mutong/services"
	"github.com/gin-gonic/gin"
)

type BusinessTopologyController struct {
	svc *services.BusinessTopologyService
}

func NewBusinessTopologyController(svc *services.BusinessTopologyService) *BusinessTopologyController {
	return &BusinessTopologyController{svc: svc}
}

func (c *BusinessTopologyController) RegisterRoutes(app *gin.Engine) {
	app.GET("/api/v1/business-topology/apps", c.GetApps)
	app.GET("/api/v1/business-topology/calls", c.GetCalls)
	app.GET("/api/v1/business-topology/graph", c.GetGraph)
}

func (c *BusinessTopologyController) GetApps(ctx *gin.Context) {
	businessUnit := ctx.Query("businessUnit")
	team := ctx.Query("team")
	namespace := ctx.Query("namespace")

	apps, err := c.svc.GetApps(businessUnit, team, namespace)
	if err != nil {
		ctx.JSON(http.StatusOK, map[string]string{"error": err.Error()})
		ctx.Status(http.StatusInternalServerError)
		return
	}

	ctx.JSON(http.StatusOK, map[string]interface{}{"apps": apps})
	ctx.Status(http.StatusOK)
}

func (c *BusinessTopologyController) GetCalls(ctx *gin.Context) {
	edges, err := c.svc.GetCalls()
	if err != nil {
		ctx.JSON(http.StatusOK, map[string]string{"error": err.Error()})
		ctx.Status(http.StatusInternalServerError)
		return
	}

	ctx.JSON(http.StatusOK, map[string]interface{}{"edges": edges})
	ctx.Status(http.StatusOK)
}

func (c *BusinessTopologyController) GetGraph(ctx *gin.Context) {
	businessUnit := ctx.Query("businessUnit")
	team := ctx.Query("team")

	graph, err := c.svc.GetGraph(businessUnit, team)
	if err != nil {
		ctx.JSON(http.StatusOK, map[string]string{"error": err.Error()})
		ctx.Status(http.StatusInternalServerError)
		return
	}

	ctx.JSON(http.StatusOK, graph)
	ctx.Status(http.StatusOK)
}
