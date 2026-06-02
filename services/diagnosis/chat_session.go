package diagnosis

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	alert_models "gitee.com/tddh/mutong/models/alert"
	"gitee.com/tddh/mutong/models/diagnosis"
)

// ChatStartRequest 创建聊天会话的请求
type ChatStartRequest struct {
	AlertFingerprint string `json:"alert_fingerprint"`
	ResourceKind     string `json:"resource_kind"`
	ResourceName     string `json:"resource_name"`
	Namespace        string `json:"namespace"`
	Description      string `json:"description"`
}

// ChatMessage 聊天消息
type ChatMessage struct {
	ID         string     `json:"id"`
	Role       string     `json:"role"` // "user" | "assistant" | "system" | "tool_call"
	Content    string     `json:"content"`
	Timestamp  time.Time  `json:"timestamp"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall 工具调用
type ToolCall struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments"`
}

// DiagnosisContext 诊断上下文
type DiagnosisContext struct {
	ResourceKind          string                       `json:"resource_kind"`
	ResourceName          string                       `json:"resource_name"`
	Namespace             string                       `json:"namespace"`
	NodeName              string                       `json:"node_name,omitempty"`
	OwnerKind             string                       `json:"owner_kind,omitempty"`
	OwnerName             string                       `json:"owner_name,omitempty"`
	AlertName             string                       `json:"alert_name,omitempty"`
	Severity              string                       `json:"severity,omitempty"`
	Summary               string                       `json:"summary,omitempty"`
	TopologyPath          []string                     `json:"topology_path,omitempty"`
	EnrichTags            map[string]string            `json:"enrich_tags,omitempty"`
	Alert                 *alert_models.ProcessedAlert `json:"alert,omitempty"`
	Topology              *diagnosis.TopologySnapshot  `json:"topology,omitempty"`
	Metrics               []diagnosis.MetricEntry      `json:"metrics,omitempty"`
	Logs                  []diagnosis.LogEntry         `json:"logs,omitempty"`
	Impact                *diagnosis.ImpactAssessment  `json:"impact,omitempty"`
	RelatedAlerts         []string                     `json:"related_alerts,omitempty"`
	BusinessAppCalls      interface{}                  `json:"business_app_calls,omitempty"`
	BusinessImpactContext interface{}                  `json:"business_impact_context,omitempty"`
}

// SSEEvent SSE 事件
type SSEEvent struct {
	Type    string `json:"type"` // "token" | "done" | "error" | "tool_call"
	Content string `json:"content"`
	Data    string `json:"data,omitempty"`
}

// ChatSession 聊天会话
type ChatSession struct {
	ID         string
	CreatedAt  time.Time
	LastActive time.Time
	mu         sync.RWMutex
	Messages   []ChatMessage
	Context    *DiagnosisContext
	SSEChan    chan *SSEEvent
	closed     bool
}

// ChatSessionManager 聊天会话管理器
type ChatSessionManager struct {
	sessions    sync.Map
	redis       *RedisStorage
	pgStore     *PostgresStorage
	logger      interfaces.Logger
	llmProvider interfaces.LLMProvider
	engine      *Engine
	stopCh      chan struct{}
	stopOnce    sync.Once
}

// NewChatSessionManager 创建聊天会话管理器
func NewChatSessionManager(logger interfaces.Logger, llmProvider interfaces.LLMProvider, engine *Engine, redis *RedisStorage, pgStore *PostgresStorage) *ChatSessionManager {
	if redis != nil && pgStore != nil {
		redis.WithPostgresStorage(pgStore)
	}
	mgr := &ChatSessionManager{
		logger:      logger,
		llmProvider: llmProvider,
		engine:      engine,
		redis:       redis,
		pgStore:     pgStore,
		stopCh:      make(chan struct{}),
	}
	go mgr.startCleanup(5*time.Minute, 30*time.Minute)
	return mgr
}

// Create 创建新会话
func (m *ChatSessionManager) Create(ctx context.Context, req ChatStartRequest) (*ChatSession, *diagnosis.DiagnosisResult, error) {
	session := &ChatSession{
		ID:         "sess_" + uuid.New().String()[:8],
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
		Context: &DiagnosisContext{
			ResourceKind: req.ResourceKind,
			ResourceName: req.ResourceName,
			Namespace:    req.Namespace,
		},
		SSEChan: make(chan *SSEEvent, 500),
	}

	var initialResult *diagnosis.DiagnosisResult
	if req.AlertFingerprint != "" {
		// 从告警页面进入：优先加载缓存诊断结果，其次使用告警富化数据
		fingerprint := req.AlertFingerprint

		// 1. 尝试加载缓存的诊断结果
		if m.redis != nil {
			cachedResult, err := m.redis.GetCachedDiagnosisResult(ctx, fingerprint)
			if err == nil && cachedResult != nil {
				initialResult = cachedResult
			}
		}

		// 2. 加载告警数据填充上下文
		if storage := m.engine.GetAlertStorage(); storage != nil {
			alert, err := storage.GetAlertByFingerprint(ctx, fingerprint)
			if err != nil {
				m.logger.Warn("Failed to fetch alert for chat context",
					zap.String("fingerprint", fingerprint),
					zap.Error(err))
			} else {
				session.Context.Alert = alert
			}
		}

		// 3. 从告警/诊断结果填充上下文
		if initialResult != nil {
			// 有缓存诊断结果：优先使用诊断数据
			if len(initialResult.RootCauses) > 0 {
				rc := initialResult.RootCauses[0]
				session.Context.ResourceKind = rc.ResourceType
				session.Context.ResourceName = rc.ResourceName
				session.Context.Namespace = rc.Namespace
			}
			session.Context.Impact = &initialResult.Impact
			session.Context.RelatedAlerts = initialResult.RelatedAlerts
			session.Context.Metrics = initialResult.Metrics
			session.Context.Logs = initialResult.RecentLogs
			session.Context.BusinessAppCalls = initialResult.BusinessAppCalls
			session.Context.BusinessImpactContext = initialResult.BusinessImpactContext

			// 展示诊断摘要
			content := initialResult.Summary
			if content == "" && len(initialResult.Remediations) > 0 {
				content = "根因分析已完成，共发现 " + fmt.Sprintf("%d", len(initialResult.RootCauses)) + " 个可能原因。"
			}
			session.Messages = append(session.Messages, ChatMessage{
				ID:        "msg_initial",
				Role:      "assistant",
				Content:   content,
				Timestamp: time.Now(),
			})
		} else {
			// 无缓存诊断结果：使用告警富化数据
			if session.Context.Alert != nil {
				a := session.Context.Alert
				session.Context.ResourceKind = a.ResourceType
				session.Context.ResourceName = a.ResourceName
				session.Context.Namespace = a.Namespace
				session.Context.NodeName = a.NodeName
				session.Context.OwnerKind = a.OwnerKind
				session.Context.OwnerName = a.OwnerName
				if a.RelatedAlerts != nil {
					session.Context.RelatedAlerts = a.RelatedAlerts
				}
				session.Context.AlertName = a.Labels["alertname"]
				session.Context.Severity = a.Routing.Severity
				session.Context.Summary = a.Annotations["summary"]
				session.Context.TopologyPath = a.TopologyPath
				session.Context.EnrichTags = a.EnrichTags
				session.Context.BusinessAppCalls = a.BusinessCalls
			}

			// 构建上下文消息展示
			var ctxParts []string
			if session.Context.AlertName != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("告警名称: %s", session.Context.AlertName))
			}
			if session.Context.Summary != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("摘要: %s", session.Context.Summary))
			}
			if session.Context.Severity != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("严重级别: %s", session.Context.Severity))
			}
			if session.Context.ResourceKind != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("资源类型: %s", session.Context.ResourceKind))
			}
			if session.Context.ResourceName != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("资源名称: %s", session.Context.ResourceName))
			}
			if session.Context.Namespace != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("命名空间: %s", session.Context.Namespace))
			}
			if session.Context.NodeName != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("所在节点: %s", session.Context.NodeName))
			}
			if session.Context.OwnerKind != "" {
				ctxParts = append(ctxParts, fmt.Sprintf("所属资源: %s/%s", session.Context.OwnerKind, session.Context.OwnerName))
			}
			if len(session.Context.TopologyPath) > 0 {
				ctxParts = append(ctxParts, fmt.Sprintf("拓扑链路: %s", strings.Join(session.Context.TopologyPath, " → ")))
			}
			if session.Context.EnrichTags != nil {
				if app := session.Context.EnrichTags["businessApp"]; app != "" {
					ctxParts = append(ctxParts, fmt.Sprintf("业务应用: %s", app))
				}
				if team := session.Context.EnrichTags["team"]; team != "" {
					ctxParts = append(ctxParts, fmt.Sprintf("所属团队: %s", team))
				}
				if crit := session.Context.EnrichTags["criticality"]; crit != "" {
					ctxParts = append(ctxParts, fmt.Sprintf("关键度: %s", crit))
				}
				if env := session.Context.EnrichTags["environment"]; env != "" {
					ctxParts = append(ctxParts, fmt.Sprintf("环境: %s", env))
				}
			}
			session.Messages = append(session.Messages, ChatMessage{
				ID:        "msg_context",
				Role:      "assistant",
				Content:   "📋 当前诊断上下文：\n\n" + strings.Join(ctxParts, "\n") + "\n\n你可以基于以上信息提问，我会结合上下文进行分析。",
				Timestamp: time.Now(),
			})
		}

		session.Messages = append(session.Messages, ChatMessage{
			ID:        "msg_system",
			Role:      "system",
			Content:   fmt.Sprintf("已加载告警: %s", fingerprint),
			Timestamp: time.Now(),
		})
	} else if req.ResourceKind != "" {
		diagReq := diagnosis.DiagnosisRequest{
			ResourceKind: req.ResourceKind,
			ResourceName: req.ResourceName,
			Namespace:    req.Namespace,
			AlertName:    req.Description,
			Severity:     "warning",
		}
		result, err := m.engine.Diagnose(ctx, diagReq)
		if err != nil {
			return nil, nil, fmt.Errorf("initial diagnosis failed: %w", err)
		}
		initialResult = result

		session.Context.Topology = result.TopologySnapshot
		session.Context.Metrics = result.Metrics
		session.Context.Logs = result.RecentLogs
		session.Context.Impact = &result.Impact
		session.Context.RelatedAlerts = result.RelatedAlerts
		session.Context.BusinessAppCalls = result.BusinessAppCalls
		session.Context.BusinessImpactContext = result.BusinessImpactContext

		session.Messages = append(session.Messages, ChatMessage{
			ID:        "msg_initial",
			Role:      "assistant",
			Content:   result.Summary,
			Timestamp: time.Now(),
		})
	} else {
		session.Messages = append(session.Messages, ChatMessage{
			ID:        "msg_welcome",
			Role:      "assistant",
			Content:   "👋 你好，我是 AI 诊断助手。请描述你要排查的问题，或选择快捷诊断按钮开始。",
			Timestamp: time.Now(),
		})
	}

	m.sessions.Store(session.ID, session)

	// 持久化使用独立 context，避免 LLM 超时取消 request context 导致保存失败
	bgCtx := context.Background()
	if m.redis != nil {
		if err := m.redis.CreateSession(bgCtx, session); err != nil {
			m.logger.Warn("Redis CreateSession failed", zap.Error(err))
		}
	}

	if m.pgStore != nil {
		go func() {
			if err := m.pgStore.ArchiveSession(bgCtx, session); err != nil {
				m.logger.Warn("ArchiveSession failed", zap.Error(err))
			}
		}()
	}

	m.logger.Info("Chat session created", zap.String("session_id", session.ID))

	return session, initialResult, nil
}

// Get 获取会话
func (m *ChatSessionManager) Get(id string) (*ChatSession, bool) {
	val, ok := m.sessions.Load(id)
	if ok {
		session := val.(*ChatSession)
		session.mu.Lock()
		session.LastActive = time.Now()
		session.mu.Unlock()
		return session, true
	}

	if m.redis != nil {
		session, err := m.redis.GetSession(context.Background(), id)
		if err == nil {
			m.sessions.Store(session.ID, session)
			return session, true
		}
	}

	return nil, false
}

// GetByAlertFingerprint 通过告警指纹获取会话
func (m *ChatSessionManager) GetByAlertFingerprint(fingerprint string) (*ChatSession, bool) {
	if m.redis != nil {
		session, err := m.redis.GetSessionByAlert(context.Background(), fingerprint)
		if err == nil {
			m.sessions.Store(session.ID, session)
			return session, true
		}
	}

	if m.pgStore != nil {
		session, err := m.pgStore.GetSessionByAlert(context.Background(), fingerprint)
		if err == nil {
			m.sessions.Store(session.ID, session)
			if m.redis != nil {
				go func() {
					if err := m.redis.CreateSession(context.Background(), session); err != nil {
						m.logger.Warn("CreateSession failed", zap.Error(err))
					}
				}()
			}
			return session, true
		}
	}

	return nil, false
}

// Delete 删除会话
func (m *ChatSessionManager) Delete(id string) {
	val, ok := m.sessions.Load(id)
	if !ok {
		return
	}
	session := val.(*ChatSession)
	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.closed {
		session.closed = true
		close(session.SSEChan)
	}
	m.sessions.Delete(id)

	if m.redis != nil {
		go func() {
			if err := m.redis.DeleteSession(context.Background(), id); err != nil {
				m.logger.Warn("DeleteSession failed", zap.Error(err))
			}
		}()
	}

	if m.pgStore != nil {
		go func() {
			if err := m.pgStore.UpdateSessionStatus(context.Background(), id, 2); err != nil {
				m.logger.Warn("UpdateSessionStatus failed", zap.Error(err))
			}
		}()
	}

	m.logger.Info("Chat session deleted", zap.String("session_id", id))
}

// AddMessage 添加消息到会话
func (s *ChatSession) AddMessage(msg ChatMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = append(s.Messages, msg)
	s.LastActive = time.Now()
}

// AddMessageAndSync 添加消息并同步到存储
func (m *ChatSessionManager) AddMessageAndSync(ctx context.Context, sessionID string, msg ChatMessage) error {
	val, ok := m.sessions.Load(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	session := val.(*ChatSession)
	session.AddMessage(msg)

	if m.redis != nil {
		if err := m.redis.AppendMessage(ctx, sessionID, msg); err != nil {
			m.logger.Warn("Redis AppendMessage failed", zap.Error(err))
		}
	}

	return nil
}

// GetMessages 获取会话消息列表
func (s *ChatSession) GetMessages() []ChatMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]ChatMessage, len(s.Messages))
	copy(result, s.Messages)
	return result
}

// SendSSE 发送 SSE 事件
func (s *ChatSession) SendSSE(event *SSEEvent) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return fmt.Errorf("session closed")
	}
	select {
	case s.SSEChan <- event:
		return nil
	default:
		return fmt.Errorf("SSE channel full")
	}
}

// startCleanup 定时清理过期会话
func (m *ChatSessionManager) startCleanup(interval, ttl time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			m.logger.Info("Chat session cleanup stopped")
			return
		case <-ticker.C:
			m.cleanup(ttl)
		}
	}
}

func (m *ChatSessionManager) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
}

// cleanup 清理过期会话
func (m *ChatSessionManager) cleanup(ttl time.Duration) {
	now := time.Now()
	var cleaned int

	m.sessions.Range(func(key, value interface{}) bool {
		session := value.(*ChatSession)
		session.mu.RLock()
		expired := now.Sub(session.LastActive) > ttl
		session.mu.RUnlock()

		if expired {
			m.Delete(key.(string))
			cleaned++
		}
		return true
	})

	if m.pgStore != nil {
		go func() {
			if err := m.pgStore.CleanupExpired(context.Background(), ttl); err != nil {
				m.logger.Warn("CleanupExpired failed", zap.Error(err))
			}
		}()
	}

	if cleaned > 0 {
		m.logger.Info("Cleaned up expired chat sessions", zap.Int("count", cleaned))
	}
}

func (m *ChatSessionManager) GetLLMProvider() interfaces.LLMProvider {
	return m.llmProvider
}

func (m *ChatSessionManager) GetRedisStorage() *RedisStorage {
	return m.redis
}

func (m *ChatSessionManager) GetPostgresStorage() *PostgresStorage {
	return m.pgStore
}
