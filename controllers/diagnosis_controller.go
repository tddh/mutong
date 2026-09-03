package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/diagnosis"
	diagnosis_svc "gitee.com/tddh/mutong/services/diagnosis"
	"gitee.com/tddh/mutong/services/mcp"
	"gitee.com/tddh/mutong/services/search"
)

type DiagnosisController struct {
	logger          interfaces.Logger
	engine          *diagnosis_svc.Engine
	mcpSrv          *mcp.Server
	metricsQuerier  interfaces.MetricsQuerier
	chatManager     *diagnosis_svc.ChatSessionManager
	cacheTTL        time.Duration
	k8sClient       *kubernetes.Clientset
	logQuerier      interfaces.LogQuerier
	cache           interfaces.Cache
	inspectionSvc   interfaces.InspectionProcessor
	sseWriteTimeout time.Duration
}

func NewDiagnosisController(logger interfaces.Logger, engine *diagnosis_svc.Engine, metricsQuerier interfaces.MetricsQuerier, chatManager *diagnosis_svc.ChatSessionManager, cacheTTL time.Duration, k8sClient *kubernetes.Clientset, logQuerier interfaces.LogQuerier, cache interfaces.Cache, inspectionSvc interfaces.InspectionProcessor, informerGetter func() dynamicinformer.DynamicSharedInformerFactory, sseWriteTimeout time.Duration) *DiagnosisController {
	return &DiagnosisController{
		logger:          logger,
		engine:          engine,
		metricsQuerier:  metricsQuerier,
		mcpSrv:          mcp.NewServer(logger, engine, metricsQuerier, k8sClient, logQuerier, cache, engine.GetVectorRetriever(), inspectionSvc, informerGetter),
		chatManager:     chatManager,
		cacheTTL:        cacheTTL,
		k8sClient:       k8sClient,
		logQuerier:      logQuerier,
		cache:           cache,
		inspectionSvc:   inspectionSvc,
		sseWriteTimeout: sseWriteTimeout,
	}
}

func (c *DiagnosisController) SetRetrospectiveGenerator(gen func(ctx context.Context, fingerprint string) (string, error)) {
	c.mcpSrv.WithRetrospectiveGenerator(mcp.RetroGenerator(gen))
}

func (c *DiagnosisController) SetExternalSearch(tavily *search.TavilyClient, github *search.GitHubClient, sanitizer *diagnosis_svc.Sanitizer, auditFn mcp.AuditLogFunc) {
	c.mcpSrv.WithExternalSearch(tavily, github, sanitizer, auditFn)
}

// SetSelfHealing 注入自愈执行器与诊断结果读取器，启用执行类 MCP 工具（安全组）
func (c *DiagnosisController) SetSelfHealing(exec interfaces.Executor, reader mcp.DiagnosisResultReader) {
	c.mcpSrv.WithSelfHealing(exec, reader)
}

func (c *DiagnosisController) sseWriteLine(w http.ResponseWriter, flusher http.Flusher) func(string) {
	return func(line string) {
		fmt.Fprint(w, line+"\n")
		flusher.Flush()
		if c.sseWriteTimeout > 0 {
			rc := http.NewResponseController(w)
			_ = rc.SetWriteDeadline(time.Now().Add(c.sseWriteTimeout))
		}
	}
}

func (c *DiagnosisController) RegisterRoutes(app *gin.Engine) {
	api := app.Group("/api/v1/diagnosis")
	api.POST("/run", c.runDiagnosis)
	api.POST("/rerun", c.rerunDiagnosis)
	api.GET("/result/:id", c.getDiagnosisResult)
	api.GET("/tools", c.listMCPTools)
	api.POST("/mcp/tool/:name", c.executeMCPTool)

	// Chat endpoints（首次消息自动创建用户会话，续接消息追加到同一会话）
	api.POST("/chat/context", c.chatContext)
	api.POST("/chat/ask", c.askChat)

	chat := api.Group("/chat")
	chat.GET("/sessions", c.listSessions)
	chat.GET("/sessions/:id", c.getSession)
	chat.DELETE("/sessions/:id", c.deleteSession)

	if c.chatManager != nil {
		// getSessionByAlert 保留（非 deprecated，有实际用途）
		api.GET("/chat/session/by-alert", c.getSessionByAlert)
		api.DELETE("/chat/:sessionId", c.closeChat)
		// 以下端点已废弃，使用 /chat/context + /chat/ask 替代
		// api.POST("/chat/start", c.startChat)
		// api.GET("/chat/:sessionId/stream", c.streamChat)
		// api.POST("/chat/:sessionId/message", c.sendChatMessage)
	}
}

func (c *DiagnosisController) runDiagnosis(ctx *gin.Context) {
	var req diagnosis.DiagnosisRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Fingerprint != "" && c.chatManager != nil && c.chatManager.GetRedisStorage() != nil {
		cached, err := c.chatManager.GetRedisStorage().GetCachedDiagnosisResult(ctx.Request.Context(), req.Fingerprint)
		if err != nil {
			c.logger.Warn("Failed to get cached diagnosis, will run fresh", zap.Error(err))
		}
		if cached != nil {
			c.logger.Debug("Diagnosis cache hit", zap.String("fingerprint", req.Fingerprint))
			ctx.JSON(http.StatusOK, cached)
			return
		}
	}

	result, err := c.engine.Diagnose(ctx.Request.Context(), req)
	if err != nil {
		c.logger.Error("Diagnosis failed", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if req.Fingerprint != "" && c.chatManager != nil && c.chatManager.GetRedisStorage() != nil {
		if err := c.chatManager.GetRedisStorage().CacheDiagnosisResult(ctx.Request.Context(), req.Fingerprint, result, c.cacheTTL); err != nil {
			c.logger.Warn("Failed to cache diagnosis result", zap.Error(err))
		} else {
			c.logger.Info("Diagnosis result cached for postmortem reuse",
				zap.String("fingerprint", req.Fingerprint),
				zap.Duration("ttl", c.cacheTTL))
		}
		// 异步持久化到 PostgreSQL
		if pgStore := c.chatManager.GetPostgresStorage(); pgStore != nil {
			go func() {
				bgCtx := context.Background()
				if err := pgStore.SaveDiagnosisResult(bgCtx, req.Fingerprint, result, ""); err != nil {
					c.logger.Warn("Failed to persist diagnosis result to PG", zap.Error(err))
				}
			}()
		}
	} else {
		c.logger.Warn("Diagnosis result not cached",
			zap.Bool("has_fingerprint", req.Fingerprint != ""),
			zap.Bool("has_chat_manager", c.chatManager != nil),
			zap.Bool("has_redis", c.chatManager != nil && c.chatManager.GetRedisStorage() != nil))
	}

	ctx.JSON(http.StatusOK, result)
}

func (c *DiagnosisController) rerunDiagnosis(ctx *gin.Context) {
	var req diagnosis.DiagnosisRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Fingerprint == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "fingerprint is required for rerun"})
		return
	}

	if c.chatManager != nil && c.chatManager.GetRedisStorage() != nil {
		if err := c.chatManager.GetRedisStorage().InvalidateDiagnosis(ctx.Request.Context(), req.Fingerprint); err != nil {
			c.logger.Warn("Failed to invalidate diagnosis cache", zap.Error(err))
		}
	}

	result, err := c.engine.Diagnose(ctx.Request.Context(), req)
	if err != nil {
		c.logger.Error("Rerun diagnosis failed", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if c.chatManager != nil && c.chatManager.GetRedisStorage() != nil {
		if err := c.chatManager.GetRedisStorage().CacheDiagnosisResult(ctx.Request.Context(), req.Fingerprint, result, c.cacheTTL); err != nil {
			c.logger.Warn("Failed to cache rerun diagnosis result", zap.Error(err))
		}
		if pgStore := c.chatManager.GetPostgresStorage(); pgStore != nil {
			go func() {
				bgCtx := context.Background()
				if err := pgStore.SaveDiagnosisResult(bgCtx, req.Fingerprint, result, ""); err != nil {
					c.logger.Warn("Failed to persist rerun diagnosis result to PG", zap.Error(err))
				}
			}()
		}
	}

	ctx.JSON(http.StatusOK, result)
}

func (c *DiagnosisController) getDiagnosisResult(ctx *gin.Context) {
	id := ctx.Param("id")
	if id == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Diagnosis results are returned inline in the run response", "id": id})
}

func (c *DiagnosisController) listMCPTools(ctx *gin.Context) {
	tools := c.mcpSrv.ListTools()
	ctx.JSON(http.StatusOK, tools)
}

func (c *DiagnosisController) executeMCPTool(ctx *gin.Context) {
	name := ctx.Param("name")

	var args map[string]string
	if err := ctx.ShouldBindJSON(&args); err != nil {
		args = make(map[string]string)
	}

	result, err := c.mcpSrv.ExecuteTool(ctx.Request.Context(), name, args)
	if err != nil {
		c.logger.Error("MCP tool execution failed",
			zap.String("tool", name),
			zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var jsonData interface{}
	if err := json.Unmarshal([]byte(result), &jsonData); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"result": result})
		return
	}

	ctx.JSON(http.StatusOK, jsonData)
}

//nolint:unused
func (c *DiagnosisController) startChat(ctx *gin.Context) {
	var req diagnosis_svc.ChatStartRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, _ := GetUserID(ctx)
	session, initialResult, err := c.chatManager.Create(ctx.Request.Context(), userID, req)
	if err != nil {
		c.logger.Error("Failed to create chat session", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"session_id":        session.ID,
		"context":           session.Context,
		"messages":          session.GetMessages(),
		"initial_diagnosis": initialResult,
	})
}

func (c *DiagnosisController) getSessionByAlert(ctx *gin.Context) {
	fingerprint := ctx.Query("alert_fingerprint")
	if fingerprint == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "alert_fingerprint is required"})
		return
	}

	session, ok := c.chatManager.GetByAlertFingerprint(fingerprint)
	if !ok {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "no active session found for this alert"})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"session_id": session.ID,
		"context":    session.Context,
		"messages":   session.GetMessages(),
	})
}

//nolint:unused
func (c *DiagnosisController) streamChat(ctx *gin.Context) {
	sessionID := ctx.Param("sessionId")
	session, ok := c.chatManager.Get(sessionID)
	if !ok {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	ctx.Header("Content-Type", "text/event-stream")
	ctx.Header("Cache-Control", "no-cache")
	ctx.Header("Connection", "keep-alive")
	ctx.Header("Access-Control-Allow-Origin", "*")

	flusher, ok := ctx.Writer.(http.Flusher)
	if !ok {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	for event := range session.SSEChan {
		data, _ := json.Marshal(event)
		fmt.Fprintf(ctx.Writer, "event: %s\ndata: %s\n\n", event.Type, string(data))
		flusher.Flush()
	}
}

//nolint:unused
func (c *DiagnosisController) sendChatMessage(ctx *gin.Context) {
	sessionID := ctx.Param("sessionId")
	session, ok := c.chatManager.Get(sessionID)
	if !ok {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Content == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "content is required"})
		return
	}

	ctx2 := ctx.Request.Context()
	_ = c.chatManager.AddMessageAndSync(ctx2, sessionID, diagnosis_svc.ChatMessage{
		ID:        "msg_user_" + sessionID,
		Role:      "user",
		Content:   req.Content,
		Timestamp: time.Now(),
	})

	go c.streamLLMResponse(session, req.Content)

	ctx.JSON(http.StatusOK, gin.H{"status": "streaming"})
}

func (c *DiagnosisController) closeChat(ctx *gin.Context) {
	sessionID := ctx.Param("sessionId")
	c.chatManager.Delete(sessionID)

	ctx.JSON(http.StatusOK, gin.H{"status": "closed"})
}

//nolint:unused
func (c *DiagnosisController) streamLLMResponse(session *diagnosis_svc.ChatSession, userMessage string) {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("streamLLMResponse panic recovered", zap.Any("panic", r))
			_ = session.SendSSE(&diagnosis_svc.SSEEvent{Type: "error", Content: "内部错误，请重试"})
		}
		// 确保 done 可靠送达：如果 channel 满了就阻塞等待 3 秒
		for i := 0; i < 10; i++ {
			if err := session.SendSSE(&diagnosis_svc.SSEEvent{Type: "done"}); err == nil {
				return
			}
			time.Sleep(300 * time.Millisecond)
		}
		_ = session.SendSSE(&diagnosis_svc.SSEEvent{Type: "error", Content: "响应超时"})
	}()

	httpProvider, ok := c.chatManager.GetLLMProvider().(*diagnosis_svc.HTTPLLMProvider)
	if !ok {
		c.logger.Warn("LLM provider does not support function calling, falling back to plain chat")
		_ = session.SendSSE(&diagnosis_svc.SSEEvent{Type: "error", Content: "当前 LLM 不支持工具调用"})
		return
	}

	allTools := c.mcpSrv.ListTools()
	selectedTools := diagnosis_svc.SelectTools(diagnosis_svc.DiagnosisScenes, userMessage, allTools)
	tools := diagnosis_svc.ConvertMCPToolsToOpenAI(selectedTools)

	messages := session.GetMessages()

	ctx2 := session.Context
	if ctx2 != nil {
		var ctxInfo []string
		if ctx2.AlertName != "" {
			ctxInfo = append(ctxInfo, "告警: "+ctx2.AlertName)
		}
		if ctx2.Severity != "" {
			ctxInfo = append(ctxInfo, "严重级别: "+ctx2.Severity)
		}
		if ctx2.ResourceKind != "" && ctx2.ResourceName != "" {
			ctxInfo = append(ctxInfo, "资源: "+ctx2.ResourceKind+"/"+ctx2.ResourceName)
		}
		if ctx2.Namespace != "" {
			ctxInfo = append(ctxInfo, "命名空间: "+ctx2.Namespace)
		}
		sysMsg := `你是一个 Kubernetes AIOps 运维助手。

## 工作流程
1. 分析用户问题，确定需要什么信息
2. 调用 1-3 个最相关的工具获取数据（尽量并行调用）
3. 基于工具返回的数据给出分析和建议

## 工具调用规则
- 同一轮中如果多个工具互不依赖，请一次性并行调用
- 最多 3 轮工具调用，如果已有足够信息直接给出结论
- 工具调用失败或超时不要重试同一个工具，换用备选或直接告诉用户
- 不要为了"全面"而调用所有工具
- 资源列表查询：优先使用 list_resources_from_cache（本地缓存，速度快不超时），备选 list_k8s_resources（实时 API）
- 常用工具：get_active_alerts（告警，列表含 fingerprint 字段）、get_alert_detail（需要传 fingerprint）、get_resource_metrics（指标）、list_resources_from_cache（资源列表）、inspect_resource（资源实时状态）
- 日志工具：get_pod_logs（K8s直接拉取，优先使用）、get_pod_logs_es（ES日志，历史日志）、search_logs（关键词搜索）、get_error_logs（错误日志）
- 用户要求查看日志时，优先使用 get_pod_logs 或 get_pod_logs_es
- 诊断工具：run_diagnosis（运行AI诊断）
- 外部搜索：search_knowledge_base（Tavily全网搜索，排查方法/最佳实践/技术文档）和 search_github_issues（GitHub Issues搜索，已知Bug/修复方案/社区讨论）。遇到陌生错误、不熟悉的组件、需要参考外部资料时主动使用

## 执行类工具（自愈提议）规则
- 可选工具：restart_pod_safe（安全重启 Pod）、rollout_restart（滚动重启 Deployment）、scale_deployment（扩缩容）
- 提案式工具（高风险，只生成待审批提议、绝不自动执行）：update_deployment_image（改镜像）、adjust_resource_limits（调 CPU/内存 requests/limits）、delete_pod（删除 Pod 由控制器重建）、rollout_undo（回滚 Deployment 到历史版本）
- 调用它们只是"提交执行提议"，是否真正执行由系统自动模式/审批机制决定，结果会记录在执行审计中
- 仅在已通过诊断或日志/指标确认根因、且该动作确实是合理修复手段时才调用；不要未确认根因就执行
- 必须携带告警的 fingerprint 参数（来自 get_active_alerts / get_alert_detail / 当前上下文）
- delete_pod 仅可用于有控制器管理（Deployment/StatefulSet/DaemonSet）的 Pod；修改 ConfigMap/Secret 等其它危险操作不可用；高风险动作只能用上述提案式工具，生成提议后提醒用户到执行记录中审批
- 未经用户同意或未明确根因时，不要主动调用执行类工具

## 回答要求
- 基于查询结果给出具体分析，不要泛泛而谈
- 用中文回复`

		if len(ctxInfo) > 0 {
			sysMsg += "\n## 当前上下文\n" + joinStrings(ctxInfo, "；")
		}
		messages = append([]diagnosis_svc.ChatMessage{{Role: "system", Content: sysMsg}}, messages...)
	}

	_ = session.SendSSE(&diagnosis_svc.SSEEvent{Type: "token", Content: ""})

	outerCtx, outerCancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer outerCancel()

	for round := 0; round < 10; round++ {
		roundCtx, roundCancel := context.WithTimeout(outerCtx, 60*time.Second)
		resp, err := httpProvider.DiagnoseChatWithTools(roundCtx, messages, tools)
		roundCancel()
		if err != nil {
			c.logger.Error("Function calling failed", zap.Error(err), zap.Int("round", round))
			_ = session.SendSSE(&diagnosis_svc.SSEEvent{Type: "error", Content: "AI 分析出错: " + err.Error()})
			return
		}

		if resp.FinishReason == "tool_calls" && len(resp.ToolCalls) > 0 {
			// 如果 LLM 在调用工具前有文本输出，先流式展示
			if resp.Content != "" {
				runes := []rune(resp.Content)
				for i := 0; i < len(runes); i += 3 {
					end := i + 3
					if end > len(runes) {
						end = len(runes)
					}
					_ = session.SendSSE(&diagnosis_svc.SSEEvent{Type: "token", Content: string(runes[i:end])})
				}
			}
			for _, tc := range resp.ToolCalls {
				if tc.Name == "" {
					continue
				}
				toolData, _ := json.Marshal(map[string]interface{}{"args": tc.Arguments})
				_ = session.SendSSE(&diagnosis_svc.SSEEvent{
					Type:    "tool_start",
					Content: tc.Name,
					Data:    string(toolData),
				})

				toolCtx, toolCancel := context.WithTimeout(outerCtx, 30*time.Second)
				result, execErr := c.mcpSrv.ExecuteTool(toolCtx, tc.Name, tc.Arguments)
				toolCancel()

				resultStr := result
				if execErr != nil {
					resultStr = "错误: " + execErr.Error()
					c.logger.Error("Tool execution failed (streamLLM)",
						zap.String("tool", tc.Name),
						zap.Any("args", tc.Arguments),
						zap.Error(execErr))
				}

				resultData, _ := json.Marshal(map[string]interface{}{"result": resultStr})
				_ = session.SendSSE(&diagnosis_svc.SSEEvent{
					Type:    "tool_result",
					Content: tc.Name,
					Data:    string(resultData),
				})

				messages = append(
					messages,
					diagnosis_svc.ChatMessage{Role: "assistant", ToolCalls: []diagnosis_svc.ToolCall{{
						ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments,
					}}},
					diagnosis_svc.ChatMessage{Role: "tool", Content: resultStr, ToolCallID: tc.ID},
				)
			}
		} else {
			content := resp.Content
			if content == "" {
				content = "抱歉，我无法回答这个问题。请尝试换一种方式提问。"
			}
			// 分批流式：每批 3 个字符，减少 SSE 事件量（500 字 → ~170 个事件）
			runes := []rune(content)
			for i := 0; i < len(runes); i += 3 {
				end := i + 3
				if end > len(runes) {
					end = len(runes)
				}
				_ = session.SendSSE(&diagnosis_svc.SSEEvent{Type: "token", Content: string(runes[i:end])})
			}
			return
		}
	}

	_ = session.SendSSE(&diagnosis_svc.SSEEvent{Type: "error", Content: "分析步骤过多，已暂停。请尝试简化问题。"})
}

func joinStrings(parts []string, sep string) string {
	var result string
	for i, s := range parts {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

type chatContextRequest struct {
	AlertFingerprint string `json:"alert_fingerprint"`
	ResourceKind     string `json:"resource_kind"`
	ResourceName     string `json:"resource_name"`
	Namespace        string `json:"namespace"`
	Description      string `json:"description"`
}

//nolint:unused
type chatContextResponse struct {
	Context         interface{}   `json:"context"`
	InitialMessages []interface{} `json:"initial_messages"`
}

func (c *DiagnosisController) chatContext(ginCtx *gin.Context) {
	var req chatContextRequest
	if err := ginCtx.ShouldBindJSON(&req); err != nil {
		ginCtx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.AlertFingerprint == "" && req.ResourceKind == "" && req.ResourceName == "" && req.Description == "" {
		ginCtx.JSON(http.StatusBadRequest, gin.H{"error": "alert_fingerprint, resource_kind+resource_name, or description is required"})
		return
	}

	ctx := context.Background()
	dCtx := &diagnosis_svc.DiagnosisContext{
		ResourceKind: req.ResourceKind,
		ResourceName: req.ResourceName,
		Namespace:    req.Namespace,
	}
	var msgs []diagnosis_svc.ChatMessage

	if req.AlertFingerprint != "" {
		fingerprint := req.AlertFingerprint

		if c.chatManager != nil && c.chatManager.GetRedisStorage() != nil {
			cached, err := c.chatManager.GetRedisStorage().GetCachedDiagnosisResult(ctx, fingerprint)
			if err == nil && cached != nil {
				if len(cached.RootCauses) > 0 {
					rc := cached.RootCauses[0]
					if dCtx.ResourceKind == "" {
						dCtx.ResourceKind = rc.ResourceType
					}
					if dCtx.ResourceName == "" {
						dCtx.ResourceName = rc.ResourceName
					}
					if dCtx.Namespace == "" {
						dCtx.Namespace = rc.Namespace
					}
				}
				dCtx.Impact = &cached.Impact
				dCtx.RelatedAlerts = cached.RelatedAlerts
				dCtx.Metrics = cached.Metrics
				dCtx.Logs = cached.RecentLogs
				dCtx.BusinessAppCalls = cached.BusinessAppCalls
				dCtx.BusinessImpactContext = cached.BusinessImpactContext
				dCtx.Topology = cached.TopologySnapshot
				dCtx.AlertName = cached.Request.AlertName
				dCtx.Severity = cached.Impact.Severity
				dCtx.Summary = cached.Summary

				content := buildDiagnosisContextContent(cached)
				if content == "" && len(cached.Remediations) > 0 {
					content = fmt.Sprintf("根因分析已完成，共发现 %d 个可能原因。", len(cached.RootCauses))
				}
				msgs = append(msgs, diagnosis_svc.ChatMessage{
					ID: "msg_initial", Role: "assistant", Content: content, Timestamp: time.Now(),
				})
			}
		}

		if storage := c.engine.GetAlertStorage(); storage != nil {
			alert, err := storage.GetAlertByFingerprint(ctx, fingerprint)
			if err != nil {
				c.logger.Warn("Failed to fetch alert for chat context",
					zap.String("fingerprint", fingerprint), zap.Error(err))
			} else {
				dCtx.Alert = alert
				if dCtx.ResourceKind == "" {
					dCtx.ResourceKind = alert.ResourceType
					dCtx.ResourceName = alert.ResourceName
					dCtx.Namespace = alert.Namespace
				}
				dCtx.NodeName = alert.NodeName
				dCtx.OwnerKind = alert.OwnerKind
				dCtx.OwnerName = alert.OwnerName
				if alert.RelatedAlerts != nil {
					dCtx.RelatedAlerts = alert.RelatedAlerts
				}
				dCtx.AlertName = alert.Labels["alertname"]
				dCtx.Severity = alert.Routing.Severity
				dCtx.Summary = alert.Annotations["summary"]
				dCtx.TopologyPath = alert.TopologyPath
				dCtx.EnrichTags = alert.EnrichTags
				dCtx.BusinessAppCalls = alert.BusinessCalls
			}
		}

		if len(msgs) == 0 {
			var ctxParts []string
			if dCtx.AlertName != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("告警名称: %s", dCtx.AlertName))
			}
			if dCtx.Summary != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("摘要: %s", dCtx.Summary))
			}
			if dCtx.Severity != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("严重级别: %s", dCtx.Severity))
			}
			if dCtx.ResourceKind != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("资源类型: %s", dCtx.ResourceKind))
			}
			if dCtx.ResourceName != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("资源名称: %s", dCtx.ResourceName))
			}
			if dCtx.Namespace != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("命名空间: %s", dCtx.Namespace))
			}
			if dCtx.NodeName != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("所在节点: %s", dCtx.NodeName))
			}
			if dCtx.OwnerKind != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("所属资源: %s/%s", dCtx.OwnerKind, dCtx.OwnerName))
			}
			if len(dCtx.TopologyPath) > 0 {
				ctxParts = append(ctxParts, fmt.Sprintf("拓扑链路: %s", joinStrings(dCtx.TopologyPath, " → ")))
			}
			if dCtx.EnrichTags != nil {
				if app := dCtx.EnrichTags["businessApp"]; app != "" {
					ctxParts = append(ctxParts, fmt.Sprintf("业务应用: %s", app))
				}
				if team := dCtx.EnrichTags["team"]; team != "" {
					ctxParts = append(ctxParts, fmt.Sprintf("所属团队: %s", team))
				}
				if crit := dCtx.EnrichTags["criticality"]; crit != "" {
					ctxParts = append(ctxParts, fmt.Sprintf("关键度: %s", crit))
				}
			}
			msgs = append(msgs, diagnosis_svc.ChatMessage{
				ID: "msg_context", Role: "assistant",
				Content:   "📋 当前诊断上下文：\n\n" + joinStrings(ctxParts, "\n") + "\n\n你可以基于以上信息提问，我会结合上下文进行分析。",
				Timestamp: time.Now(),
			})
		}

		msgs = append(msgs, diagnosis_svc.ChatMessage{
			ID: "msg_system", Role: "system",
			Content:   fmt.Sprintf("已加载告警: %s", fingerprint),
			Timestamp: time.Now(),
		})
	} else if req.ResourceKind != "" && req.ResourceName != "" {
		diagReq := diagnosis.DiagnosisRequest{
			ResourceKind: req.ResourceKind,
			ResourceName: req.ResourceName,
			Namespace:    req.Namespace,
			AlertName:    req.Description,
			Severity:     "warning",
		}
		result, err := c.engine.Diagnose(ctx, diagReq)
		if err != nil {
			c.logger.Error("Initial diagnosis failed", zap.Error(err))
			ginCtx.JSON(http.StatusInternalServerError, gin.H{"error": "initial diagnosis failed: " + err.Error()})
			return
		}
		dCtx.Topology = result.TopologySnapshot
		dCtx.Metrics = result.Metrics
		dCtx.Logs = result.RecentLogs
		dCtx.Impact = &result.Impact
		dCtx.RelatedAlerts = result.RelatedAlerts
		dCtx.BusinessAppCalls = result.BusinessAppCalls
		dCtx.BusinessImpactContext = result.BusinessImpactContext

		msgs = append(msgs, diagnosis_svc.ChatMessage{
			ID: "msg_initial", Role: "assistant",
			Content:   result.Summary,
			Timestamp: time.Now(),
		})
	} else if req.ResourceKind != "" {
		msgs = append(msgs, diagnosis_svc.ChatMessage{
			ID: "msg_welcome", Role: "assistant",
			Content:   fmt.Sprintf("👋 你好！我已准备好帮你诊断 **%s** 相关问题。请输入要排查的具体资源名称和命名空间。", req.ResourceKind),
			Timestamp: time.Now(),
		})
	} else {
		msgs = append(msgs, diagnosis_svc.ChatMessage{
			ID: "msg_welcome", Role: "assistant",
			Content:   "👋 你好，我是 AI 诊断助手。请描述你要排查的问题。",
			Timestamp: time.Now(),
		})
	}

	respContext := gin.H{
		"resource_kind":  dCtx.ResourceKind,
		"resource_name":  dCtx.ResourceName,
		"namespace":      dCtx.Namespace,
		"node_name":      dCtx.NodeName,
		"owner_kind":     dCtx.OwnerKind,
		"owner_name":     dCtx.OwnerName,
		"alert_name":     dCtx.AlertName,
		"severity":       dCtx.Severity,
		"summary":        dCtx.Summary,
		"topology_path":  dCtx.TopologyPath,
		"enrich_tags":    dCtx.EnrichTags,
		"related_alerts": dCtx.RelatedAlerts,
		"impact":         dCtx.Impact,
		"alert":          dCtx.Alert,
	}

	initialMsgs := make([]interface{}, 0, len(msgs))
	for _, m := range msgs {
		initialMsgs = append(initialMsgs, gin.H{
			"role":      m.Role,
			"content":   m.Content,
			"timestamp": m.Timestamp,
		})
	}

	ginCtx.JSON(http.StatusOK, gin.H{
		"context":          respContext,
		"initial_messages": initialMsgs,
	})
}

type askChatRequest struct {
	SessionID string                      `json:"session_id"`
	Messages  []diagnosis_svc.ChatMessage `json:"messages"`
	Context   struct {
		ResourceKind string `json:"resource_kind"`
		ResourceName string `json:"resource_name"`
		Namespace    string `json:"namespace"`
	} `json:"context"`
}

//nolint:unused
type askChatResponse struct {
	Content   string                      `json:"content"`
	Messages  []diagnosis_svc.ChatMessage `json:"messages"`
	ToolsUsed []string                    `json:"tools_used"`
}

func (c *DiagnosisController) askChat(ginCtx *gin.Context) {
	var req askChatRequest
	if err := ginCtx.ShouldBindJSON(&req); err != nil {
		ginCtx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Messages) == 0 {
		ginCtx.JSON(http.StatusBadRequest, gin.H{"error": "messages is required"})
		return
	}

	llmProvider := c.chatManager.GetLLMProvider()
	if einoProvider, ok := llmProvider.(*diagnosis_svc.EinoLLMProvider); ok {
		c.askChatWithEino(ginCtx, req, einoProvider)
		return
	}

	httpProvider, ok := llmProvider.(*diagnosis_svc.HTTPLLMProvider)
	if !ok {
		ginCtx.JSON(http.StatusInternalServerError, gin.H{"error": "当前 LLM 不支持工具调用"})
		return
	}

	allTools := c.mcpSrv.ListTools()
	userQuery := diagnosis_svc.GetFirstUserMessage(req.Messages)
	selectedTools := diagnosis_svc.SelectTools(diagnosis_svc.DiagnosisScenes, userQuery, allTools)
	tools := diagnosis_svc.ConvertMCPToolsToOpenAI(selectedTools)
	messages := make([]diagnosis_svc.ChatMessage, len(req.Messages))
	copy(messages, req.Messages)

	sysMsg := `你是一个 Kubernetes AIOps 运维助手。

## 工作流程
1. 分析用户问题，确定需要什么信息
2. 调用 1-3 个最相关的工具获取数据（尽量并行调用）
3. 基于工具返回的数据给出分析和建议

## 工具调用规则
- 同一轮中如果多个工具互不依赖，请一次性并行调用
- 最多 3 轮工具调用，如果已有足够信息直接给出结论
- 工具调用失败或超时不要重试同一个工具，换用备选或直接告诉用户
- 不要为了"全面"而调用所有工具
- 资源列表查询：优先使用 list_resources_from_cache（本地缓存，速度快不超时），备选 list_k8s_resources（实时 API）
- 常用工具：get_active_alerts（告警，列表含 fingerprint 字段）、get_alert_detail（需要传 fingerprint）、get_resource_metrics（指标）、list_resources_from_cache（资源列表）、inspect_resource（资源实时状态）
- 日志工具：get_pod_logs（K8s直接拉取，优先使用）、get_pod_logs_es（ES日志，历史日志）、search_logs（关键词搜索）、get_error_logs（错误日志）
- 用户要求查看日志时，优先使用 get_pod_logs 或 get_pod_logs_es
- 诊断工具：run_diagnosis（运行AI诊断）

## 执行类工具（自愈提议）规则
- 可选工具：restart_pod_safe（安全重启 Pod）、rollout_restart（滚动重启 Deployment）、scale_deployment（扩缩容）
- 提案式工具（高风险，只生成待审批提议、绝不自动执行）：update_deployment_image（改镜像）、adjust_resource_limits（调 CPU/内存 requests/limits）、delete_pod（删除 Pod 由控制器重建）、rollout_undo（回滚 Deployment 到历史版本）
- 调用它们只是"提交执行提议"，是否真正执行由系统自动模式/审批机制决定，结果会记录在执行审计中
- 仅在已通过诊断或日志/指标确认根因、且该动作确实是合理修复手段时才调用；不要未确认根因就执行
- 必须携带告警的 fingerprint 参数（来自 get_active_alerts / get_alert_detail / 当前上下文）
- delete_pod 仅可用于有控制器管理（Deployment/StatefulSet/DaemonSet）的 Pod；修改 ConfigMap/Secret 等其它危险操作不可用；高风险动作只能用上述提案式工具，生成提议后提醒用户到执行记录中审批
- 未经用户同意或未明确根因时，不要主动调用执行类工具

## 回答要求
- 基于查询结果给出具体分析，不要泛泛而谈
- 用中文回复`

	if req.Context.ResourceKind != "" && req.Context.ResourceName != "" {
		ctxInfo := fmt.Sprintf("\n## 当前上下文\n资源: %s/%s", req.Context.ResourceKind, req.Context.ResourceName)
		if req.Context.Namespace != "" {
			ctxInfo += fmt.Sprintf(" 命名空间: %s", req.Context.Namespace)
		}
		sysMsg += ctxInfo
	}

	messages = append([]diagnosis_svc.ChatMessage{{Role: "system", Content: sysMsg}}, messages...)

	ginCtx.Header("Content-Type", "text/plain")
	ginCtx.Header("Cache-Control", "no-cache")
	ginCtx.Header("Connection", "keep-alive")
	ginCtx.Header("Access-Control-Allow-Origin", "*")

	flusher, ok := ginCtx.Writer.(http.Flusher)
	if !ok {
		ginCtx.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	writeLine := c.sseWriteLine(ginCtx.Writer, flusher)

	outerCtx, outerCancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer outerCancel()

	toolsUsed := make(map[string]gin.H)

	for round := 0; round < 5; round++ {
		var roundMu sync.Mutex

		roundCB := diagnosis_svc.StreamCallback{
			OnToken: func(token string) {
				content, _ := json.Marshal(token)
				writeLine(fmt.Sprintf(`{"type":"token","content":%s}`, string(content)))
			},
			OnToolCall: func(name string, args map[string]string) {
				if name == "" {
					return
				}
				if args == nil {
					writeLine(fmt.Sprintf(`{"type":"tool_start","name":%q}`, name))
				} else {
					argsJSON, _ := json.Marshal(args)
					roundMu.Lock()
					toolsUsed[name] = gin.H{"name": name, "args": args}
					roundMu.Unlock()
					writeLine(fmt.Sprintf(`{"type":"tool_args","name":%q,"args":%s}`, name, string(argsJSON)))
				}
			},
		}

		roundCtx, roundCancel := context.WithTimeout(outerCtx, 60*time.Second)
		c.logger.Info("LLM round start", zap.Int("round", round), zap.Int("msgCount", len(messages)))
		resp, err := httpProvider.DiagnoseChatWithToolsStream(roundCtx, messages, tools, roundCB)
		roundCancel()
		if err != nil {
			c.logger.Error("LLM round failed", zap.Error(err), zap.Int("round", round))
			writeLine(`{"type":"error","message":"AI 分析出错"}`)
			writeLine(`{"type":"done"}`)
			return
		}
		c.logger.Info("LLM round done", zap.Int("round", round), zap.String("finishReason", resp.FinishReason), zap.Int("toolCallCount", len(resp.ToolCalls)))

		if resp.FinishReason == "tool_calls" && len(resp.ToolCalls) > 0 {
			type toolResult struct {
				tc     diagnosis_svc.ToolCall
				result string
				err    error
			}
			var wg sync.WaitGroup
			results := make([]toolResult, len(resp.ToolCalls))

			for i, tc := range resp.ToolCalls {
				roundMu.Lock()
				toolsUsed[tc.Name] = gin.H{"name": tc.Name}
				roundMu.Unlock()

				wg.Add(1)
				go func(idx int, tc diagnosis_svc.ToolCall) {
					defer wg.Done()
					toolCtx, toolCancel := context.WithTimeout(outerCtx, 30*time.Second)
					defer toolCancel()
					c.logger.Info("Executing tool", zap.String("tool", tc.Name), zap.Any("args", tc.Arguments))
					result, execErr := c.mcpSrv.ExecuteTool(toolCtx, tc.Name, tc.Arguments)
					results[idx] = toolResult{tc: tc, result: result, err: execErr}
				}(i, tc)
			}
			wg.Wait()

			for _, r := range results {
				tc := r.tc
				resultStr := r.result
				status := "done"
				if r.err != nil {
					resultStr = "错误: " + r.err.Error()
					status = "error"
					c.logger.Error("Tool execution failed",
						zap.String("tool", tc.Name),
						zap.Any("args", tc.Arguments),
						zap.Error(r.err))
				} else {
					c.logger.Info("Tool executed successfully", zap.String("tool", tc.Name), zap.Int("resultLen", len(r.result)))
				}
				summary := resultStr
				if len(summary) > 200 {
					summary = summary[:200] + "..."
				}

				roundMu.Lock()
				if t, exists := toolsUsed[tc.Name]; exists {
					t["status"] = status
					t["result_summary"] = summary
				}
				roundMu.Unlock()

				writeLine(fmt.Sprintf(`{"type":"tool_result","name":%q,"status":%q,"summary":%q}`, tc.Name, status, summary))

				messages = append(
					messages,
					diagnosis_svc.ChatMessage{Role: "assistant", ToolCalls: []diagnosis_svc.ToolCall{{
						ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments,
					}}},
					diagnosis_svc.ChatMessage{Role: "tool", Content: resultStr, ToolCallID: tc.ID},
				)
			}
		} else {
			writeLine(`{"type":"done"}`)
			return
		}
	}

	writeLine(`{"type":"error","message":"分析步骤过多，已暂停。请尝试简化问题。"}`)
	writeLine(`{"type":"done"}`)
}

func buildDiagnosisContextContent(cached *diagnosis.DiagnosisResult) string {
	if cached == nil {
		return ""
	}
	var buf bytes.Buffer
	t := cached.Timestamp
	if !t.IsZero() {
		fmt.Fprintf(&buf, "诊断时间: %s\n", t.Format("2006-01-02 15:04:05"))
	}
	if cached.ID != "" {
		fmt.Fprintf(&buf, "诊断 ID: %s\n", cached.ID)
	}
	for i, rc := range cached.RootCauses {
		if i >= 3 {
			break
		}
		fmt.Fprintf(&buf, "根因 %d: %s/%s (%.0f%%)\n", i+1, rc.ResourceType, rc.ResourceName, rc.Confidence*100)
		if len(rc.Evidence) > 0 {
			fmt.Fprintf(&buf, "  证据: %s\n", rc.Evidence[0])
		}
	}
	if len(cached.Remediations) > 0 {
		fmt.Fprintln(&buf, "修复建议:")
		for i, r := range cached.Remediations {
			if i >= 3 {
				break
			}
			fmt.Fprintf(&buf, "  %d. %s (风险: %s)\n", i+1, r.Action, r.RiskLevel)
		}
	}
	if cached.Impact.BusinessImpactNote != "" {
		fmt.Fprintf(&buf, "业务影响: %s\n", cached.Impact.BusinessImpactNote)
	}
	if buf.Len() == 0 && cached.Summary != "" {
		return cached.Summary
	}
	return buf.String()
}

func (c *DiagnosisController) askChatWithEino(ginCtx *gin.Context, req askChatRequest, provider *diagnosis_svc.EinoLLMProvider) {
	sysMsg := `你是一个 Kubernetes AIOps 运维助手。

## 工作流程
1. 分析用户问题，确定需要什么信息
2. 调用 1-3 个最相关的工具获取数据（尽量并行调用）
3. 基于工具返回的数据给出分析和建议

## 工具调用规则
- 同一轮中如果多个工具互不依赖，请一次性并行调用
- 最多 3 轮工具调用，如果已有足够信息直接给出结论
- 工具调用失败或超时不要重试同一个工具，换用备选或直接告诉用户
- 不要为了"全面"而调用所有工具
- 资源列表查询：优先使用 list_resources_from_cache（本地缓存，速度快不超时），备选 list_k8s_resources（实时 API）
- 常用工具：get_active_alerts（告警，列表含 fingerprint 字段）、get_alert_detail（需要传 fingerprint）、get_resource_metrics（指标）、list_resources_from_cache（资源列表）、inspect_resource（资源实时状态）
- 日志工具：get_pod_logs（K8s直接拉取，优先使用）、get_pod_logs_es（ES日志，历史日志）、search_logs（关键词搜索）、get_error_logs（错误日志）
- 用户要求查看日志时，优先使用 get_pod_logs 或 get_pod_logs_es
- 诊断工具：run_diagnosis（运行AI诊断）

## 执行类工具（自愈提议）规则
- 可选工具：restart_pod_safe（安全重启 Pod）、rollout_restart（滚动重启 Deployment）、scale_deployment（扩缩容）
- 提案式工具（高风险，只生成待审批提议、绝不自动执行）：update_deployment_image（改镜像）、adjust_resource_limits（调 CPU/内存 requests/limits）、delete_pod（删除 Pod 由控制器重建）、rollout_undo（回滚 Deployment 到历史版本）
- 调用它们只是"提交执行提议"，是否真正执行由系统自动模式/审批机制决定，结果会记录在执行审计中
- 仅在已通过诊断或日志/指标确认根因、且该动作确实是合理修复手段时才调用；不要未确认根因就执行
- 必须携带告警的 fingerprint 参数（来自 get_active_alerts / get_alert_detail / 当前上下文）
- delete_pod 仅可用于有控制器管理（Deployment/StatefulSet/DaemonSet）的 Pod；修改 ConfigMap/Secret 等其它危险操作不可用；高风险动作只能用上述提案式工具，生成提议后提醒用户到执行记录中审批
- 未经用户同意或未明确根因时，不要主动调用执行类工具

## 回答要求
- 基于查询结果给出具体分析，不要泛泛而谈
- 用中文回复`

	if req.Context.ResourceKind != "" && req.Context.ResourceName != "" {
		ctxInfo := fmt.Sprintf("\n## 当前上下文\n资源: %s/%s", req.Context.ResourceKind, req.Context.ResourceName)
		if req.Context.Namespace != "" {
			ctxInfo += fmt.Sprintf(" 命名空间: %s", req.Context.Namespace)
		}
		sysMsg += ctxInfo
	}

	var history []*schema.Message
	for _, m := range req.Messages {
		switch m.Role {
		case "user":
			history = append(history, schema.UserMessage(m.Content))
		case "assistant":
			history = append(history, schema.AssistantMessage(m.Content, nil))
		}
	}

	ginCtx.Header("Content-Type", "text/event-stream")
	ginCtx.Header("Cache-Control", "no-cache")
	ginCtx.Header("Connection", "keep-alive")
	ginCtx.Header("Access-Control-Allow-Origin", "*")
	ginCtx.Header("X-Accel-Buffering", "no")
	ginCtx.Status(200)

	flusher, ok := ginCtx.Writer.(http.Flusher)
	if !ok {
		ginCtx.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	// trigger initial flush
	fmt.Fprint(ginCtx.Writer, ":\n\n")
	flusher.Flush()

	writeLineRaw := c.sseWriteLine(ginCtx.Writer, flusher)
	var writeMu sync.Mutex
	writeLine := func(line string) {
		writeMu.Lock()
		writeLineRaw(line)
		writeMu.Unlock()
	}

	allToolDefsAndHandlers := c.mcpSrv.GetEinoToolDefsAndHandlers()
	einoTools := diagnosis_svc.ConvertToEinoTools(allToolDefsAndHandlers, c.logger)

	chatModel := provider.GetChatModel()
	agent := diagnosis_svc.NewDiagnosisAgent(chatModel, einoTools, c.logger)

	// 阶段1：首个问题立即创建会话（侧栏立即可见）
	var sessionID string
	if req.SessionID == "" {
		firstMsg := firstUserMessage(req.Messages)
		title := truncateString(firstMsg, 50)
		if title == "" {
			title = fmt.Sprintf("新对话 %s", time.Now().Format("15:04"))
		}
		userID, _ := GetUserID(ginCtx)
		c.logger.Info("creating session before SSE", zap.String("title", title), zap.Int("msg_count", len(req.Messages)), zap.Uint("user_id", userID))
		chatReq := diagnosis_svc.ChatStartRequest{
			ResourceKind: req.Context.ResourceKind,
			ResourceName: req.Context.ResourceName,
			Namespace:    req.Context.Namespace,
			Description:  title,
		}
		session, _, createErr := c.chatManager.Create(context.Background(), userID, chatReq)
		if createErr == nil {
			sessionID = session.ID
			c.logger.Info("session created OK", zap.String("session_id", sessionID), zap.Uint("user_id", userID))
		} else {
			c.logger.Error("FAILED to create session", zap.Error(createErr))
		}
	} else {
		sessionID = req.SessionID
		c.logger.Info("skipping session creation", zap.String("req_session_id", req.SessionID))
	}

	outerCtx, outerCancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer outerCancel()

	c.logger.Info(
		"Eino agent streaming started",
		zap.Int("history_msgs", len(history)),
		zap.Int("tools", len(einoTools)),
	)

	toolCallCount := 0
	startTime := time.Now()
	var assistantText strings.Builder

	// 保活：首步 LLM 可能静默 1 分钟以上，前端有 15s 首 token / 60s 空闲超时会误断连。
	// 启动即发、每 5s 发一次（远小于两个超时阈值），前端把 keepalive 视为连接活跃
	heartbeatDone := make(chan struct{})
	go func() {
		writeLine(`{"type":"keepalive"}`)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-ticker.C:
				writeLine(`{"type":"keepalive"}`)
			}
		}
	}()

	err := agent.RunStreamWithMessages(outerCtx, history, sysMsg, func(chunk string, toolCall *diagnosis_svc.ToolCallInfo) error {
		if toolCall != nil {
			if toolCall.Name == "" {
				return nil
			}
			toolCallCount++
			c.logger.Info(
				"Eino agent tool call",
				zap.Int("call_num", toolCallCount),
				zap.String("tool", toolCall.Name),
				zap.String("args", toolCall.Arguments),
			)
			writeLine(fmt.Sprintf(`{"type":"tool_call","name":%q}`, toolCall.Name))
			return nil
		}
		if chunk != "" {
			assistantText.WriteString(chunk)
			content, _ := json.Marshal(chunk)
			writeLine(fmt.Sprintf(`{"type":"stream_chunk","content":%s}`, string(content)))
		}
		return nil
	})
	close(heartbeatDone)
	if err != nil {
		c.logger.Error(
			"Eino agent streaming failed",
			zap.Error(err),
			zap.Int("tool_calls", toolCallCount),
			zap.Duration("duration", time.Since(startTime)),
		)
		writeLine(`{"type":"error","message":"AI 分析出错"}`)
	} else {
		c.logger.Info(
			"Eino agent streaming completed",
			zap.Int("tool_calls", toolCallCount),
			zap.Duration("duration", time.Since(startTime)),
		)
	}

	// 阶段2：保存消息 + 触发 LLM 标题生成
	if sessionID != "" {
		allMessages := make([]diagnosis_svc.ChatMessage, len(req.Messages))
		copy(allMessages, req.Messages)
		answer := assistantText.String()
		if answer != "" {
			allMessages = append(allMessages, diagnosis_svc.ChatMessage{
				Role:    "assistant",
				Content: answer,
			})
		}
		if pgStore := c.chatManager.GetPostgresStorage(); pgStore != nil {
			if err := pgStore.ArchiveMessages(context.Background(), sessionID, allMessages); err != nil {
				c.logger.Error("failed to archive messages", zap.Error(err))
			}
		}
		firstMsg := firstUserMessage(req.Messages)
		if firstMsg != "" && answer != "" {
			go c.chatManager.GenerateTitle(sessionID, firstMsg, answer)
		}
	}

	exitEvent := `{"type":"action","action_type":"exit"`
	if sessionID != "" {
		exitEvent += `,"session_id":"` + sessionID + `"`
	}
	exitEvent += "}"
	writeLine(exitEvent)
	c.logger.Info("exit event sent", zap.String("session_id", sessionID), zap.Int("msg_count", len(req.Messages)))
}

func firstUserMessage(msgs []diagnosis_svc.ChatMessage) string {
	for _, m := range msgs {
		if m.Role == "user" {
			return m.Content
		}
	}
	return ""
}

func truncateString(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

func (c *DiagnosisController) listSessions(ginCtx *gin.Context) {
	userID, _ := GetUserID(ginCtx)
	limit := 20
	offset := 0
	if v, err := strconv.Atoi(ginCtx.DefaultQuery("limit", "20")); err == nil && v > 0 && v <= 50 {
		limit = v
	}
	if v, err := strconv.Atoi(ginCtx.DefaultQuery("offset", "0")); err == nil && v >= 0 {
		offset = v
	}

	sessions, total, err := c.chatManager.ListSessions(ginCtx.Request.Context(), userID, limit, offset)
	if err != nil {
		ginCtx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.logger.Info("listSessions result",
		zap.Uint("user_id", userID),
		zap.Int64("total", total),
		zap.Int("returned", len(sessions)))

	ginCtx.JSON(http.StatusOK, gin.H{
		"sessions": sessions,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

func (c *DiagnosisController) getSession(ginCtx *gin.Context) {
	sessionID := ginCtx.Param("id")
	session, err := c.chatManager.LoadSession(ginCtx.Request.Context(), sessionID)
	if err != nil {
		ginCtx.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	userID, _ := GetUserID(ginCtx)
	if session.UserID != 0 && userID != 0 && session.UserID != userID {
		ginCtx.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		return
	}

	ginCtx.JSON(http.StatusOK, gin.H{
		"session": gin.H{
			"id":         session.ID,
			"messages":   session.GetMessages(),
			"context":    session.Context,
			"created_at": session.CreatedAt,
		},
	})
}

func (c *DiagnosisController) deleteSession(ginCtx *gin.Context) {
	sessionID := ginCtx.Param("id")
	userID, _ := GetUserID(ginCtx)

	session, err := c.chatManager.LoadSession(ginCtx.Request.Context(), sessionID)
	if err != nil {
		ginCtx.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	if session.UserID != userID {
		ginCtx.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
		return
	}
	c.chatManager.Delete(sessionID)
	ginCtx.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
