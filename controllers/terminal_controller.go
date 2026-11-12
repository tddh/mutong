package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	maudit "gitee.com/tddh/mutong/models/audit"
	audstore "gitee.com/tddh/mutong/services/audit"
	t "gitee.com/tddh/mutong/services/terminal"
)

var wsUpgrader = websocket.Upgrader{
	HandshakeTimeout: 10 * time.Second,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Non-browser client (no Origin header).
			return true
		}
		allowed := os.Getenv("MUTONG_ALLOWED_ORIGINS")
		if allowed == "" {
			// Default to same-origin instead of allowing every site.
			u, err := url.Parse(origin)
			return err == nil && u.Host == r.Host
		}
		originHost := origin
		if u, err := url.Parse(origin); err == nil {
			originHost = u.Host
		}
		for _, a := range strings.Split(allowed, ",") {
			a = strings.TrimSpace(a)
			if a == "" || a == "*" {
				continue
			}
			allowedHost := a
			if u, err := url.Parse(a); err == nil && u.Host != "" {
				allowedHost = u.Host
			}
			if origin == a || originHost == allowedHost {
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
	auditStore audstore.OperationAuditStore
}

func NewTerminalController(logger *zap.Logger, k8sClient *kubernetes.Clientset, restConfig *rest.Config) *TerminalController {
	manager := t.NewTerminalSessionManager()
	manager.SetLogger(logger)
	return &TerminalController{
		Manager:    manager,
		k8sClient:  k8sClient,
		restConfig: restConfig,
		logger:     logger,
	}
}

// SetAllowedNamespaces 注入终端命名空间白名单（可选，空=默认拒绝系统命名空间）
func (c *TerminalController) SetAllowedNamespaces(namespaces []string) {
	c.Manager.SetAllowedNamespaces(namespaces)
}

// SetAuditStore 注入操作审计存储（可选，未注入时仍写服务端日志）
func (c *TerminalController) SetAuditStore(store audstore.OperationAuditStore) {
	c.auditStore = store
}

// terminalOperator 解析当前请求的操作者标识
func terminalOperator(ctx *gin.Context) string {
	if u := ctx.GetString("username"); u != "" {
		return u
	}
	if u := GetUserUUID(ctx); u != "" {
		return u
	}
	if uid, ok := GetUserID(ctx); ok {
		return fmt.Sprintf("user-%d", uid)
	}
	return "unknown"
}

// recordTerminalAudit 终端会话/执行留痕：服务端日志 + 操作审计表
func (c *TerminalController) recordTerminalAudit(operator string, ev t.AuditEvent) {
	c.logger.Info("Terminal audit",
		zap.String("operator", operator),
		zap.String("action", ev.Action),
		zap.String("cluster", ev.Cluster),
		zap.String("namespace", ev.Namespace),
		zap.String("pod", ev.Pod),
		zap.String("container", ev.Container),
		zap.String("shell", ev.Shell),
		zap.String("session", ev.SessionID),
		zap.String("result", ev.Result))
	if c.auditStore == nil {
		return
	}
	if err := c.auditStore.Save(maudit.OperationAuditLog{
		Operator:     operator,
		Action:       ev.Action,
		ResourceType: "Pod",
		ResourceName: ev.Pod,
		Namespace:    ev.Namespace,
		Result:       ev.Result,
		Reason:       "web terminal",
		Timestamp:    time.Now(),
		Details:      fmt.Sprintf("cluster=%s container=%s shell=%s session=%s", ev.Cluster, ev.Container, ev.Shell, ev.SessionID),
	}); err != nil {
		c.logger.Error("Failed to persist terminal audit log", zap.Error(err), zap.String("session", ev.SessionID))
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
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list namespaces"})
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
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list pods"})
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
			c.logger.Error("Failed to list pods", zap.Error(err), zap.String("namespace", namespace))
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list pods"})
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
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get pod containers"})
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
	defer func() { _ = ws.Close() }()

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
	switch shell {
	case "/bin/sh", "/bin/bash", "/bin/ash", "/bin/zsh", "/bin/dash":
	default:
		shell = "/bin/sh"
	}

	if pod == "" {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31mError: pod name required\x1b[0m\r\n"))
		return
	}
	if container == "" {
		container = pod
	}

	operator := terminalOperator(ctx)
	// exec 前校验命名空间白名单，禁止进入系统命名空间
	if !c.Manager.IsNamespaceAllowed(namespace) {
		c.logger.Warn("Terminal namespace denied",
			zap.String("operator", operator),
			zap.String("cluster", cluster),
			zap.String("namespace", namespace),
			zap.String("pod", pod))
		c.recordTerminalAudit(operator, t.AuditEvent{
			Cluster: cluster, Namespace: namespace, Pod: pod, Container: container,
			Action: "terminal_session_denied", Result: "namespace not allowed",
		})
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31mError: namespace not allowed for terminal access\x1b[0m\r\n"))
		return
	}

	if c.Manager.SessionCount() >= 100 {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31mError: too many concurrent terminal sessions\x1b[0m\r\n"))
		return
	}
	sess, err := c.Manager.CreateSession(cluster, namespace, pod, container, ws)
	if err != nil {
		c.logger.Warn("Terminal session rejected", zap.Error(err), zap.String("namespace", namespace), zap.String("pod", pod))
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\n\x1b[31mError: terminal session not permitted\x1b[0m\r\n"))
		return
	}
	defer func() { _ = c.Manager.CloseSession(sess.ID) }()
	sess.SetK8sClient(c.k8sClient, c.restConfig)
	sess.SetAuditHook(func(ev t.AuditEvent) { c.recordTerminalAudit(operator, ev) })
	c.recordTerminalAudit(operator, t.AuditEvent{
		SessionID: sess.ID, Cluster: cluster, Namespace: namespace, Pod: pod, Container: container,
		Action: "terminal_session_open", Result: "opened",
	})
	go sess.StartK8sExec(cluster, namespace, pod, container, shell)

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
