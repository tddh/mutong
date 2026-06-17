package controllers

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	t "gitee.com/tddh/mutong/services/terminal"
)

var wsUpgrader = websocket.Upgrader{
	HandshakeTimeout: 10 * time.Second,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		allowed := os.Getenv("MUTONG_ALLOWED_ORIGINS")
		if allowed == "" {
			return true
		}
		for _, a := range strings.Split(allowed, ",") {
			a = strings.TrimSpace(a)
			if a == "*" || origin == a {
				return true
			}
		}
		return false
	},
}

type TerminalController struct {
	Manager    *t.TerminalSessionManager
	k8sClient  *kubernetes.Clientset
	restConfig *rest.Config
	logger     *zap.Logger
}

func NewTerminalController(logger *zap.Logger, k8sClient *kubernetes.Clientset, restConfig *rest.Config) *TerminalController {
	return &TerminalController{
		Manager:    t.NewTerminalSessionManager(),
		k8sClient:  k8sClient,
		restConfig: restConfig,
		logger:     logger,
	}
}

func (c *TerminalController) RegisterRoutes(app *gin.Engine) {
	app.GET("/api/v1/terminal/namespaces", c.namespaces)
	app.GET("/api/v1/terminal/pods", c.pods)
	app.GET("/api/v1/terminal/containers", c.containers)
	app.GET("/api/v1/terminal/ws", c.ws)
}

func (c *TerminalController) namespaces(ctx *gin.Context) {
	if c.k8sClient == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "Kubernetes client not configured"})
		return
	}

	nsList, err := c.k8sClient.CoreV1().Namespaces().List(ctx.Request.Context(), metav1.ListOptions{})
	if err != nil {
		c.logger.Error("Failed to list namespaces", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	namespaces := make([]string, 0, len(nsList.Items))
	for _, ns := range nsList.Items {
		namespaces = append(namespaces, ns.Name)
	}

	ctx.JSON(http.StatusOK, gin.H{"namespaces": namespaces})
}

func (c *TerminalController) pods(ctx *gin.Context) {
	namespace := ctx.DefaultQuery("namespace", "default")

	if c.k8sClient == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "Kubernetes client not configured"})
		return
	}

	podList, err := c.k8sClient.CoreV1().Pods(namespace).List(ctx.Request.Context(), metav1.ListOptions{})
	if err != nil {
		c.logger.Error("Failed to list pods", zap.Error(err), zap.String("namespace", namespace))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	pods := make([]string, 0, len(podList.Items))
	for _, pod := range podList.Items {
		pods = append(pods, pod.Name)
	}

	ctx.JSON(http.StatusOK, gin.H{"namespace": namespace, "pods": pods})
}

func (c *TerminalController) containers(ctx *gin.Context) {
	namespace := ctx.DefaultQuery("namespace", "default")
	podName := ctx.Query("pod")

	if c.k8sClient == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "Kubernetes client not configured"})
		return
	}

	if podName == "" {
		podList, err := c.k8sClient.CoreV1().Pods(namespace).List(ctx.Request.Context(), metav1.ListOptions{})
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		pods := make([]string, 0, len(podList.Items))
		for _, pod := range podList.Items {
			pods = append(pods, pod.Name)
		}
		ctx.JSON(http.StatusOK, gin.H{"namespace": namespace, "pods": pods, "containers": []string{}})
		return
	}

	pod, err := c.k8sClient.CoreV1().Pods(namespace).Get(ctx.Request.Context(), podName, metav1.GetOptions{})
	if err != nil {
		c.logger.Error("Failed to get pod", zap.Error(err), zap.String("namespace", namespace), zap.String("pod", podName))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	containers := make([]string, 0, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		containers = append(containers, c.Name)
	}

	initContainers := make([]string, 0, len(pod.Spec.InitContainers))
	for _, c := range pod.Spec.InitContainers {
		initContainers = append(initContainers, c.Name)
	}

	ctx.JSON(http.StatusOK, gin.H{
		"namespace":      namespace,
		"pod":            podName,
		"containers":     containers,
		"initContainers": initContainers,
	})
}

func (c *TerminalController) ws(ctx *gin.Context) {
	ws, err := wsUpgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		if !ctx.Writer.Written() {
			ctx.AbortWithStatus(http.StatusInternalServerError)
		}
		return
	}
	defer ws.Close()

	const (
		writeWait      = 10 * time.Second
		pongWait       = 60 * time.Second
		pingPeriod     = 54 * time.Second
		maxMessageSize = 4096
	)
	ws.SetReadLimit(maxMessageSize)
	_ = ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error {
		_ = ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// ping ticker to keep connection alive
	pingDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = ws.SetWriteDeadline(time.Now().Add(writeWait))
				if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-pingDone:
				return
			}
		}
	}()
	defer close(pingDone)

	cluster := ctx.DefaultQuery("cluster", "default-cluster")
	namespace := ctx.DefaultQuery("namespace", "default")
	pod := ctx.DefaultQuery("pod", "")
	container := ctx.DefaultQuery("container", "")
	shell := ctx.DefaultQuery("shell", "/bin/sh")

	if pod == "" {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31mError: pod name required\x1b[0m\r\n"))
		return
	}
	if container == "" {
		container = pod
	}

	sess, _ := c.Manager.CreateSession(cluster, namespace, pod, container, ws)
	if sess != nil {
		sess.SetK8sClient(c.k8sClient, c.restConfig)
		go sess.StartK8sExec(cluster, namespace, pod, container, shell)
	}

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			break
		}
		if len(msg) > 0 {
			var payload map[string]interface{}
			if err := json.Unmarshal(msg, &payload); err == nil {
				if t, ok := payload["type"].(string); ok && t == "resize" {
					w64, _ := payload["width"].(float64)
					h64, _ := payload["height"].(float64)
					if sess != nil {
						sess.Resize(uint16(w64), uint16(h64))
					}
					continue
				}
			}
			if sess != nil {
				sess.WriteStdin(msg)
			}
		}
	}
}
