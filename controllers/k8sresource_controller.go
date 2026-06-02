package controllers

import (
	"context"
	"net/http"

	"gitee.com/tddh/mutong/interfaces"
	"github.com/gin-gonic/gin"
)

type K8sResourceController struct {
	K8sResourceSvc interfaces.K8sResourceInterface
}

func NewK8sResourceController(K8sResourceSvc interfaces.K8sResourceInterface) *K8sResourceController {
	return &K8sResourceController{K8sResourceSvc: K8sResourceSvc}
}

func (ct *K8sResourceController) RegisterK8sResourceRoutes(app *gin.Engine) {
	// 具体路径必须在通配符之前注册，否则会被覆盖
	app.GET("/k8s/resources/graph/nodes", ct.GetAllResources)
	app.GET("/k8s/resources/graph/edges", ct.GetAllRelationships)
	app.GET("/k8s/resources/graph/metadata", ct.GetKindsAndNamespaces)
	app.GET("/k8s/resources/graph/search", ct.SearchResourceRelationship)
	app.GET("/k8s/resources/graph/suggest", ct.SuggestResources)
	app.GET("/k8s/resources/graph/resource-define", ct.GetResourceDefine)
	// 通配符路由放最后
	app.GET("/k8s/resources/:name", ct.GetAll)
}

func (ct *K8sResourceController) reqCtx(c *gin.Context) interfaces.RequestContext {
	return interfaces.RequestContext{
		TraceID:     c.GetHeader("X-Trace-ID"),
		QueryParams: extractQueryParams(c),
	}
}

func (ct *K8sResourceController) GetAll(c *gin.Context) {
	result, err := ct.K8sResourceSvc.Get(ct.reqCtx(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (ct *K8sResourceController) Collect(clusterName string) {
	ct.K8sResourceSvc.Collect(clusterName)
}

func (ct *K8sResourceController) CollectWithContext(ctx context.Context, clusterName string) {
	ct.K8sResourceSvc.CollectWithContext(ctx, clusterName)
}

func (ct *K8sResourceController) ConsumeKafkaMessages() {
	ct.K8sResourceSvc.ConsumeKafkaMessages()
}

func (ct *K8sResourceController) ConsumeKafkaMessagesWithContext(ctx context.Context) {
	ct.K8sResourceSvc.ConsumeKafkaMessagesWithContext(ctx)
}

func (ct *K8sResourceController) Relationship(clusterName string) {
	ct.K8sResourceSvc.Relationship("")
}

func (ct *K8sResourceController) GetAllResources(c *gin.Context) {
	result, err := ct.K8sResourceSvc.GetAllResources(ct.reqCtx(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (ct *K8sResourceController) GetAllRelationships(c *gin.Context) {
	result, err := ct.K8sResourceSvc.GetAllRelationships(ct.reqCtx(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (ct *K8sResourceController) SearchResourceRelationship(c *gin.Context) {
	result, err := ct.K8sResourceSvc.SearchResourceRelationship(ct.reqCtx(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (ct *K8sResourceController) GetResourceDefine(c *gin.Context) {
	result, err := ct.K8sResourceSvc.GetResourceDefine(ct.reqCtx(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (ct *K8sResourceController) SuggestResources(c *gin.Context) {
	result, err := ct.K8sResourceSvc.SuggestResources(ct.reqCtx(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (ct *K8sResourceController) GetKindsAndNamespaces(c *gin.Context) {
	result, err := ct.K8sResourceSvc.GetKindsAndNamespaces(ct.reqCtx(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
