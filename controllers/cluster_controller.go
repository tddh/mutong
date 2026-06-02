package controllers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gitee.com/tddh/mutong/config"
	"gitee.com/tddh/mutong/interfaces"
)

type ClusterController struct {
	logger *zap.Logger
	cfg    *config.Cluster
	db     interfaces.GraphDB
}

func NewClusterController(logger *zap.Logger, cfg *config.Cluster, db interfaces.GraphDB) *ClusterController {
	return &ClusterController{
		logger: logger,
		cfg:    cfg,
		db:     db,
	}
}

func (c *ClusterController) RegisterRoutes(app *gin.Engine) {
	clusters := app.Group("/api/v1/clusters")
	{
		clusters.GET("", c.ListClusters)
		clusters.GET("/:name/health", c.CheckHealth)
		clusters.GET("/:name/stats", c.GetStats)
	}
}

func (c *ClusterController) ListClusters(ctx *gin.Context) {
	if c.cfg == nil {
		ctx.JSON(http.StatusOK, gin.H{"clusters": []interface{}{}})
		return
	}

	type ClusterInfo struct {
		Name            string    `json:"name"`
		Region          string    `json:"region"`
		Context         string    `json:"context"`
		Endpoint        string    `json:"endpoint"`
		Status          string    `json:"status"`
		Enable          bool      `json:"enable"`
		LastHealthCheck time.Time `json:"last_health_check"`
	}

	clusters := make([]ClusterInfo, 0)
	for name, client := range c.cfg.K8sClusterClient {
		info := ClusterInfo{
			Name:            name,
			Region:          c.cfg.Region,
			Context:         client.Name,
			Status:          client.Status,
			Enable:          c.cfg.Enable,
			LastHealthCheck: client.LastHealthCheck,
		}
		if client.Status == "" {
			info.Status = "unknown"
		}
		if client.RootRestConfig != nil {
			info.Endpoint = client.RootRestConfig.Host
		}
		clusters = append(clusters, info)
	}

	ctx.JSON(http.StatusOK, gin.H{
		"clusters": clusters,
		"count":    len(clusters),
	})
}

func (c *ClusterController) CheckHealth(ctx *gin.Context) {
	name := ctx.Param("name")
	if name == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	client := c.cfg.K8sClusterClient[name]
	if client == nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "cluster not found"})
		return
	}

	ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.RootKubeClientSet.CoreV1().Namespaces().List(ctx2, metav1.ListOptions{Limit: 1})
	if err != nil {
		client.Status = "unhealthy"
		client.LastHealthCheck = time.Now()
		ctx.JSON(http.StatusOK, gin.H{
			"name":   name,
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}

	client.Status = "healthy"
	client.LastHealthCheck = time.Now()
	ctx.JSON(http.StatusOK, gin.H{
		"name":   name,
		"status": "healthy",
	})
}

func (c *ClusterController) GetStats(ctx *gin.Context) {
	name := ctx.Param("name")
	if name == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	client := c.cfg.K8sClusterClient[name]
	if client == nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "cluster not found"})
		return
	}

	stats := make(map[string]int)

	ctx2, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if pods, err := client.RootKubeClientSet.CoreV1().Pods("").List(ctx2, metav1.ListOptions{}); err == nil {
		stats["pods"] = len(pods.Items)
	}
	if nodes, err := client.RootKubeClientSet.CoreV1().Nodes().List(ctx2, metav1.ListOptions{}); err == nil {
		stats["nodes"] = len(nodes.Items)
	}
	if svcs, err := client.RootKubeClientSet.CoreV1().Services("").List(ctx2, metav1.ListOptions{}); err == nil {
		stats["services"] = len(svcs.Items)
	}
	if deps, err := client.RootKubeClientSet.AppsV1().Deployments("").List(ctx2, metav1.ListOptions{}); err == nil {
		stats["deployments"] = len(deps.Items)
	}

	ctx.JSON(http.StatusOK, gin.H{
		"cluster": name,
		"stats":   stats,
	})
}
