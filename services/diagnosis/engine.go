package diagnosis

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gitee.com/tddh/mutong/interfaces"
	alert_interfaces "gitee.com/tddh/mutong/interfaces/alert"
	alert_models "gitee.com/tddh/mutong/models/alert"
	"gitee.com/tddh/mutong/models/diagnosis"
)

type Engine struct {
	logger              interfaces.Logger
	graphDB             interfaces.GraphDB
	alertStorage        alert_interfaces.AlertProcessor
	knowledgeBase       *KnowledgeBase
	llmProvider         interfaces.LLMProvider
	metricsQuerier      interfaces.MetricsQuerier
	logQuerier          interfaces.LogQuerier
	confidenceThreshold float64
	querier             *TopologyQuerier
	assessor            *ImpactAssessor
	diagnoseCount       atomic.Int64
	llmCallCount        atomic.Int64
	ruleCallCount       atomic.Int64
	db                  *gorm.DB
	metrics             *DiagnosisMetrics
	agent               *DiagnosisAgent
	vectorRetriever     *VectorRetriever
	hybridRetriever     *HybridRetriever
	collector           *ContextCollector
	stopCh              chan struct{}
	stopOnce            sync.Once
}

// LLMStatusInfo 表示LLM提供者状态（不暴露apiKey）
type LLMStatusInfo struct {
	Configured bool   `json:"configured"`
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
	Available  bool   `json:"available"`
}

// DiagnosisStats 表示诊断统计
type DiagnosisStats struct {
	TotalDiagnoses int64 `json:"totalDiagnoses"`
	LLMCalls       int64 `json:"llmCalls"`
	RuleOnlyCalls  int64 `json:"ruleOnlyCalls"`
}

// GetLLMStatus returns LLM provider status (no api keys exposed)
func (e *Engine) GetLLMStatus() LLMStatusInfo {
	info := LLMStatusInfo{}
	if e.llmProvider == nil {
		return info
	}
	if p, ok := e.llmProvider.(*HTTPLLMProvider); ok {
		info.Configured = true
		info.Provider = p.provider
		info.Model = p.model
		info.Endpoint = p.resolveEndpoint("default")
		info.Available = true
	} else if p, ok := e.llmProvider.(*EinoLLMProvider); ok {
		info.Configured = true
		info.Provider = p.provider
		info.Model = p.model
		info.Endpoint = p.endpoint
		info.Available = true
	} else {
		info.Configured = true
		info.Provider = "custom"
		info.Available = true
	}
	return info
}

// GetDiagnosisStats 返回诊断统计信息
func (e *Engine) GetDiagnosisStats() DiagnosisStats {
	return DiagnosisStats{
		TotalDiagnoses: e.diagnoseCount.Load(),
		LLMCalls:       e.llmCallCount.Load(),
		RuleOnlyCalls:  e.ruleCallCount.Load(),
	}
}

func NewEngine(logger interfaces.Logger, graphDB interfaces.GraphDB, alertStorage alert_interfaces.AlertProcessor) *Engine {
	querier := NewTopologyQuerier(logger, graphDB)
	assessor := NewImpactAssessor(logger, querier)

	return &Engine{
		logger:              logger,
		graphDB:             graphDB,
		alertStorage:        alertStorage,
		knowledgeBase:       NewKnowledgeBase(),
		confidenceThreshold: 0.8,
		querier:             querier,
		assessor:            assessor,
	}
}

func (e *Engine) WithLLMProvider(provider interfaces.LLMProvider) *Engine {
	e.llmProvider = provider
	return e
}

func (e *Engine) WithConfidenceThreshold(threshold float64) *Engine {
	e.confidenceThreshold = threshold
	return e
}

func (e *Engine) WithMetricsQuerier(querier interfaces.MetricsQuerier) *Engine {
	e.metricsQuerier = querier
	return e
}

func (e *Engine) WithLogQuerier(querier interfaces.LogQuerier) *Engine {
	e.logQuerier = querier
	return e
}

func (e *Engine) WithDB(db *gorm.DB) *Engine {
	e.db = db
	return e
}

func (e *Engine) WithMetrics(m *DiagnosisMetrics) *Engine {
	e.metrics = m
	return e
}

func (e *Engine) WithEinoAgent(agent *DiagnosisAgent) *Engine {
	e.agent = agent
	return e
}

func (e *Engine) WithCollector(c *ContextCollector) *Engine {
	e.collector = c
	return e
}

func (e *Engine) GetTopologyQuerier() *TopologyQuerier { return e.querier }
func (e *Engine) GetImpactAssessor() *ImpactAssessor   { return e.assessor }
func (e *Engine) GetKnowledgeBase() *KnowledgeBase     { return e.knowledgeBase }

func (e *Engine) GetVectorRetriever() *VectorRetriever {
	if e.vectorRetriever == nil && e.db != nil {
		e.vectorRetriever = NewVectorRetriever(e.db)
	}
	return e.vectorRetriever
}

func (e *Engine) GetHybridRetriever() *HybridRetriever {
	if e.hybridRetriever == nil && e.db != nil && e.querier != nil {
		vr := e.GetVectorRetriever()
		if vr != nil {
			e.hybridRetriever = NewHybridRetriever(e.logger, vr, e.querier, e.db)
		}
	}
	return e.hybridRetriever
}

func (e *Engine) loadStatsFromDB() {
	if e.db == nil {
		return
	}

	var stats []diagnosis.StatsModel
	if err := e.db.Find(&stats).Error; err != nil {
		e.logger.Warn("Failed to load diagnosis stats from DB", zap.Error(err))
		return
	}

	for _, s := range stats {
		switch s.StatKey {
		case "total_diagnoses":
			e.diagnoseCount.Add(s.StatValue)
		case "llm_calls":
			e.llmCallCount.Add(s.StatValue)
		case "rule_only_calls":
			e.ruleCallCount.Add(s.StatValue)
		}
	}
}

func (e *Engine) syncStatsToDB() {
	if e.db == nil {
		return
	}

	stats := []diagnosis.StatsModel{
		{StatKey: "total_diagnoses", StatValue: e.diagnoseCount.Load()},
		{StatKey: "llm_calls", StatValue: e.llmCallCount.Load()},
		{StatKey: "rule_only_calls", StatValue: e.ruleCallCount.Load()},
	}

	for _, s := range stats {
		if err := e.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "stat_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"stat_value", "updated_at"}),
		}).Create(&s).Error; err != nil {
			e.logger.Warn("Failed to sync diagnosis stats to DB", zap.String("key", s.StatKey), zap.Error(err))
		}
	}
}

func (e *Engine) StartStatsSync(interval time.Duration) {
	e.loadStatsFromDB()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-e.stopCh:
				return
			case <-ticker.C:
				e.syncStatsToDB()
			}
		}
	}()
}

func (e *Engine) Stop() {
	e.stopOnce.Do(func() {
		close(e.stopCh)
	})
}

func (e *Engine) Diagnose(ctx context.Context, req diagnosis.DiagnosisRequest) (*diagnosis.DiagnosisResult, error) {
	return e.DiagnoseWithTopology(ctx, req, nil)
}

func (e *Engine) DiagnoseWithTopology(ctx context.Context, req diagnosis.DiagnosisRequest, preloadedTopology *diagnosis.TopologySnapshot) (*diagnosis.DiagnosisResult, error) {
	startTime := time.Now()
	diagnosisPath := "rule-only"

	var targetAlert *alert_models.ProcessedAlert
	var err error

	if req.Fingerprint != "" {
		targetAlert, err = e.alertStorage.GetAlertByFingerprint(ctx, req.Fingerprint)
		if err != nil {
			return nil, fmt.Errorf("failed to get alert: %w", err)
		}
	} else if req.Namespace != "" && req.Resource != "" {
		activeAlerts, err := e.alertStorage.GetActiveAlerts(ctx, map[string]string{
			"namespace":    req.Namespace,
			"resourceName": req.Resource,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get alerts: %w", err)
		}
		if len(activeAlerts) > 0 {
			targetAlert = activeAlerts[0]
		}
	} else if req.ResourceKind != "" && req.ResourceName != "" {
		// 直接诊断模式：不依赖已有告警，按 kind+name 构建临时 alert
		uid := e.querier.ResolveUID(req.ResourceKind, req.ResourceName, req.Namespace)
		targetAlert = &alert_models.ProcessedAlert{
			EnrichedAlert: alert_models.EnrichedAlert{
				ResourceType: req.ResourceKind,
				ResourceName: req.ResourceName,
				Namespace:    req.Namespace,
				ResourceUID:  uid,
				Alert: alert_models.Alert{
					Labels: map[string]string{
						"alertname": req.AlertName,
					},
				},
				EnrichTags: map[string]string{},
			},
			Routing: alert_models.RoutingResult{
				Severity: req.Severity,
			},
		}
	}

	if targetAlert == nil {
		return nil, fmt.Errorf("no matching alert found")
	}

	quickCauses := e.analyzeRootCause(ctx, targetAlert)

	ruleConfidence := e.getMaxConfidence(quickCauses)

	// Use pipeline collector when available; fallback for UID resolution.
	var pipelineCtx *PipelineContext
	if e.collector != nil {
		pipelineCtx = e.collector.Collect(ctx, targetAlert)
	} else {
		pipelineCtx = e.collectDiagnosisContextFallback(ctx, targetAlert, preloadedTopology)
	}

	if pipelineCtx.Topology != nil && e.metrics != nil {
		e.metrics.RecordTopologySnapshot(len(pipelineCtx.Topology.Nodes), len(pipelineCtx.Topology.Edges))
	}

	e.diagnoseCount.Add(1)

	// 多维度加权置信度：规则诊断分 + 环境佐证分
	compositeConfidence := computeDiagnosisConfidence(targetAlert, pipelineCtx, ruleConfidence)

	if compositeConfidence < e.confidenceThreshold && (e.agent != nil || e.llmProvider != nil) {
		var evidence []string
		if len(quickCauses) > 0 {
			evidence = quickCauses[0].Evidence
		}
		// 知识库匹配已在管道中完成，此处仅作为兜底
		if len(pipelineCtx.KnowledgeMatches) == 0 {
			pipelineCtx.KnowledgeMatches = e.knowledgeBase.GetSuggestions(targetAlert.ResourceType, evidence)
		}

		diagnosisPrompt := pipelineCtx.ToDiagnosisPrompt()

		var llmResult *interfaces.LLMDiagnosisResult
		var llmErr error

		if e.agent != nil {
			sysPrompt := buildDiagnosisSystemPromptFromPrompt(diagnosisPrompt)
			userMsg := "请基于以上告警信息进行根因诊断，返回严格的 JSON 格式结果。"
			agentResult, aerr := e.agent.Run(ctx, userMsg, sysPrompt)
			if aerr != nil {
				e.logger.Warn("Eino agent diagnosis failed, falling back to HTTP provider", zap.Error(aerr))
			} else if agentResult != nil {
				llmResult, llmErr = parseLLMResponse(agentResult.Content)
				if llmErr != nil {
					e.logger.Warn("Failed to parse agent response", zap.Error(llmErr))
				}
			}
		}

		if llmResult == nil && e.llmProvider != nil {
			llmResult, llmErr = e.llmProvider.Diagnose(ctx, diagnosisPrompt)
		}

		e.llmCallCount.Add(1)
		if llmErr != nil || llmResult == nil {
			e.logger.Error("LLM diagnosis failed, falling back to rule-only path", zap.Error(llmErr))
			if e.metrics != nil {
				e.metrics.RecordLLMFallback(false)
			}
		} else {
			// 注入 LLM 返回的业务影响分析
			if llmResult.BusinessImpact != nil {
				pipelineCtx.Impact.BusinessImpact = llmResult.BusinessImpact
			}
			if e.metrics != nil {
				e.metrics.RecordLLMFallback(true)
			}
			result := e.buildResultFromLLM(llmResult, pipelineCtx.Impact, pipelineCtx.Topology, targetAlert, pipelineCtx.RelatedAlerts)
			result.BusinessAppCalls = pipelineCtx.BusinessCalls
			result.BusinessImpactContext = pipelineCtx.BizImpactCtx
			diagnosisPath = "llm"
			if e.metrics != nil {
				e.metrics.RecordDiagnosis(diagnosisPath, time.Since(startTime), float64(llmResult.Confidence))
			}
			return result, nil
		}
	}

	e.ruleCallCount.Add(1)
	result := e.buildResult(ctx, quickCauses, pipelineCtx.Impact, pipelineCtx.Topology, targetAlert)
	result.BusinessAppCalls = pipelineCtx.BusinessCalls
	result.BusinessImpactContext = pipelineCtx.BizImpactCtx
	if e.metrics != nil {
		e.metrics.RecordDiagnosis(diagnosisPath, time.Since(startTime), e.getMaxConfidence(quickCauses))
	}
	return result, nil
}

func (e *Engine) getMaxConfidence(causes []diagnosis.RootCause) float64 {
	max := 0.0
	for _, cause := range causes {
		if cause.Confidence > max {
			max = cause.Confidence
		}
	}
	return max
}

// computeDiagnosisConfidence 综合规则诊断分与多维度环境佐证，得出最终置信度。
// 维度包括：annotation 质量、关联告警数、业务关键度。
// 置信度足够时跳过 LLM，节省 Token。
func computeDiagnosisConfidence(
	alert *alert_models.ProcessedAlert,
	pipelineCtx *PipelineContext,
	ruleConfidence float64,
) float64 {
	score := ruleConfidence

	if pipelineCtx == nil {
		return score
	}

	// 关联告警：同一根因的衍生告警越多，级联证据越充分
	if len(pipelineCtx.RelatedAlerts) >= 3 {
		score += 0.25
	} else if len(pipelineCtx.RelatedAlerts) >= 1 {
		score += 0.15
	}

	// 业务关键度
	if alert.BusinessContext.Criticality == "critical" || alert.BusinessContext.Criticality == "P0" {
		score += 0.15
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}

func (e *Engine) buildResultFromLLM(
	llmResult *interfaces.LLMDiagnosisResult,
	impact diagnosis.ImpactAssessment,
	topology *diagnosis.TopologySnapshot,
	alert *alert_models.ProcessedAlert,
	relatedAlerts []string,
) *diagnosis.DiagnosisResult {
	var metrics []diagnosis.MetricEntry
	if e.metricsQuerier != nil && alert != nil {
		metrics = e.collectMetrics(context.Background(), alert.ResourceType, alert.Namespace, alert.ResourceName, alert.NodeName)
	}
	var logs []diagnosis.LogEntry
	if e.logQuerier != nil && alert != nil {
		logs = e.collectLogs(context.Background(), alert.ResourceType, alert.Namespace, alert.ResourceName)
	}

	summary := fmt.Sprintf("LLM 诊断：%s (置信度：%.0f%%)", llmResult.RootCause, llmResult.Confidence*100)
	if impact.BusinessContext != "" {
		summary += " | " + impact.BusinessContext
	}

	return &diagnosis.DiagnosisResult{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		RootCauses: []diagnosis.RootCause{{
			ResourceType: alert.ResourceType,
			ResourceName: alert.ResourceName,
			Namespace:    alert.Namespace,
			Confidence:   float64(llmResult.Confidence),
			Evidence:     llmResult.Evidence,
		}},
		Impact:           impact,
		TopologySnapshot: topology,
		Remediations:     []diagnosis.RemediationSuggestion{llmResult.Remediation.First()},
		RelatedAlerts:    relatedAlerts,
		Summary:          summary,
		Metrics:          metrics,
		RecentLogs:       logs,
	}
}

func (e *Engine) buildResult(
	ctx context.Context,
	causes []diagnosis.RootCause,
	impact diagnosis.ImpactAssessment,
	topology *diagnosis.TopologySnapshot,
	alert *alert_models.ProcessedAlert,
) *diagnosis.DiagnosisResult {
	var metrics []diagnosis.MetricEntry
	if e.metricsQuerier != nil {
		metrics = e.collectMetrics(ctx, alert.ResourceType, alert.Namespace, alert.ResourceName, alert.NodeName)
	}
	var logs []diagnosis.LogEntry
	if e.logQuerier != nil {
		logs = e.collectLogs(ctx, alert.ResourceType, alert.Namespace, alert.ResourceName)
	}
	return &diagnosis.DiagnosisResult{
		ID:               uuid.New().String(),
		Timestamp:        time.Now(),
		RootCauses:       causes,
		Impact:           impact,
		TopologySnapshot: topology,
		Remediations:     e.generateRemediations(ctx, causes, impact),
		RelatedAlerts:    e.findRelatedAlerts(ctx, alert),
		Summary:          e.generateSummary(&diagnosis.DiagnosisResult{RootCauses: causes, Impact: impact}),
		Metrics:          metrics,
		RecentLogs:       logs,
	}
}

func (e *Engine) analyzeRootCause(ctx context.Context, alert *alert_models.ProcessedAlert) []diagnosis.RootCause {
	var causes []diagnosis.RootCause

	resourceType := alert.ResourceType
	resourceName := alert.ResourceName
	namespace := alert.Namespace

	switch resourceType {
	case "Pod":
		causes = e.analyzePodRootCause(ctx, namespace, resourceName, alert)
	case "Node":
		causes = e.analyzeNodeRootCause(ctx, resourceName, alert)
	case "Deployment":
		causes = e.analyzeDeploymentRootCause(ctx, namespace, resourceName, alert)
	case "Service":
		causes = e.analyzeServiceRootCause(ctx, namespace, resourceName, alert)
	default:
		causes = append(causes, diagnosis.RootCause{
			ResourceType: resourceType,
			ResourceName: resourceName,
			Namespace:    namespace,
			Confidence:   0.3,
			Evidence:     []string{fmt.Sprintf("告警在 %s/%s 触发", resourceType, resourceName)},
		})
	}

	return causes
}

func (e *Engine) analyzePodRootCause(ctx context.Context, namespace, podName string, alert *alert_models.ProcessedAlert) []diagnosis.RootCause {
	var causes []diagnosis.RootCause

	alertName := alert.Labels["alertname"]
	annotations := alert.Annotations

	if isInfrastructurePod(podName, namespace) {
		causes = append(causes, diagnosis.RootCause{
			ResourceType: "Pod",
			ResourceName: podName,
			Namespace:    namespace,
			Confidence:   0.7,
			Evidence: []string{
				fmt.Sprintf("基础设施组件 %s/%s 受影响", namespace, podName),
				fmt.Sprintf("告警: %s", alertName),
				"可能影响集群中所有服务",
			},
		})
		if summary, ok := annotations["summary"]; ok {
			causes[0].Evidence = append(causes[0].Evidence, summary)
		}

		if len(causes) > 0 && e.querier != nil {
			wc := e.querier.QueryWorkloadContext(podName, namespace)
			if wc != nil {
				causes[0].Evidence = append(causes[0].Evidence,
					fmt.Sprintf("%s %s: %d/%d 副本健康",
						wc.ControllerKind, wc.ControllerName, wc.HealthyPods, wc.TotalPods))
			}
		}

		return causes
	}
	switch {
	case strings.Contains(alertName, "OOMKilled") || strings.Contains(alertName, "Memory"):
		causes = append(causes, diagnosis.RootCause{
			ResourceType: "Pod",
			ResourceName: podName,
			Namespace:    namespace,
			Confidence:   0.9,
			Evidence: []string{
				"Pod 内存使用超过限制",
				fmt.Sprintf("告警: %s", alertName),
			},
		})
	case strings.Contains(alertName, "CPUThrottling") || strings.Contains(alertName, "HighCpu"):
		causes = append(causes, diagnosis.RootCause{
			ResourceType: "Pod",
			ResourceName: podName,
			Namespace:    namespace,
			Confidence:   0.85,
			Evidence: []string{
				"Pod CPU 使用超过阈值",
				fmt.Sprintf("告警: %s", alertName),
			},
		})
	case strings.Contains(alertName, "CrashLoop") || strings.Contains(alertName, "Restart"):
		causes = append(causes, diagnosis.RootCause{
			ResourceType: "Pod",
			ResourceName: podName,
			Namespace:    namespace,
			Confidence:   0.8,
			Evidence: []string{
				"Pod 处于 CrashLoopBackOff 状态",
				fmt.Sprintf("告警: %s", alertName),
			},
		})
	case strings.Contains(alertName, "Pending"):
		causes = append(causes, diagnosis.RootCause{
			ResourceType: "Node",
			ResourceName: alert.NodeName,
			Namespace:    namespace,
			Confidence:   0.7,
			Evidence: []string{
				"Pod 卡在 Pending 状态 — 可能节点资源耗尽",
				fmt.Sprintf("节点: %s", alert.NodeName),
			},
		})
	default:
		if summary, ok := annotations["summary"]; ok {
			causes = append(causes, diagnosis.RootCause{
				ResourceType: "Pod",
				ResourceName: podName,
				Namespace:    namespace,
				Confidence:   0.5,
				Evidence:     []string{summary},
			})
		}
	}

	return causes
}

func (e *Engine) analyzeNodeRootCause(ctx context.Context, nodeName string, alert *alert_models.ProcessedAlert) []diagnosis.RootCause {
	var causes []diagnosis.RootCause

	alertName := alert.Labels["alertname"]

	switch {
	case strings.Contains(alertName, "NotReady"):
		causes = append(causes, diagnosis.RootCause{
			ResourceType: "Node",
			ResourceName: nodeName,
			Confidence:   0.95,
			Evidence: []string{
				"节点处于 NotReady 状态",
				"该节点上所有 Pod 均受影响",
			},
		})
	case strings.Contains(alertName, "DiskPressure") || strings.Contains(alertName, "MemoryPressure"):
		causes = append(causes, diagnosis.RootCause{
			ResourceType: "Node",
			ResourceName: nodeName,
			Confidence:   0.6,
			Evidence: []string{
				fmt.Sprintf("节点资源压力: %s", alertName),
			},
		})
	default:
		causes = append(causes, diagnosis.RootCause{
			ResourceType: "Node",
			ResourceName: nodeName,
			Confidence:   0.5,
			Evidence:     []string{fmt.Sprintf("节点告警: %s", alertName)},
		})
	}

	return causes
}

func (e *Engine) analyzeDeploymentRootCause(ctx context.Context, namespace, deployName string, alert *alert_models.ProcessedAlert) []diagnosis.RootCause {
	// 基本根因：Deployment 层面
	causes := []diagnosis.RootCause{{
		ResourceType: "Deployment",
		ResourceName: deployName,
		Namespace:    namespace,
		Confidence:   0.7,
		Evidence: []string{
			fmt.Sprintf("Deployment %s/%s 存在异常", namespace, deployName),
			"检查 Pod 状态和发布历史",
		},
	}}

	// 通过 Metrics Querier 提升诊断：获取 CPU/内存利用率指标
	if e.metricsQuerier != nil {
		if dms, err := e.metricsQuerier.GetDeploymentMetrics(ctx, namespace, deployName); err == nil {
			for _, m := range dms {
				switch m.MetricName {
				case "k8s_deployment_cpu_utilization":
					if m.Value > 80 {
						causes = append(causes, diagnosis.RootCause{
							ResourceType: "Deployment",
							ResourceName: deployName,
							Namespace:    namespace,
							Confidence:   0.85,
							Evidence:     []string{fmt.Sprintf("Deployment CPU 使用率过高: %.2f%%", m.Value)},
						})
					}
				case "k8s_deployment_memory_utilization":
					if m.Value > 80 {
						causes = append(causes, diagnosis.RootCause{
							ResourceType: "Deployment",
							ResourceName: deployName,
							Namespace:    namespace,
							Confidence:   0.85,
							Evidence:     []string{fmt.Sprintf("Deployment 内存使用率过高: %.2f%%", m.Value)},
						})
					}
				}
			}
		}
	}

	return causes
}

func (e *Engine) analyzeServiceRootCause(ctx context.Context, namespace, svcName string, alert *alert_models.ProcessedAlert) []diagnosis.RootCause {
	return []diagnosis.RootCause{{
		ResourceType: "Service",
		ResourceName: svcName,
		Namespace:    namespace,
		Confidence:   0.6,
		Evidence: []string{
			fmt.Sprintf("Service %s/%s 端点异常", namespace, svcName),
			"检查后端 Pod 是否健康",
		},
	}}
}

func (e *Engine) findRelatedAlerts(ctx context.Context, alert *alert_models.ProcessedAlert) []string {
	seen := map[string]bool{alert.Fingerprint: true}

	if e.querier != nil && alert.ResourceUID != "" {
		flags := buildExpandFlags(alert, nil)
		neighbors := e.querier.ExpandRelatedResourceUIDs(
			alert.ResourceUID, alert.NodeName, alert.Namespace,
			alert.OwnerKind, alert.OwnerName,
			alert.BusinessContext.AppName, alert.BusinessContext.Namespace,
			20, flags,
		)
		for _, n := range neighbors {
			if n.Name == "" || n.Namespace == "" {
				continue
			}
			alerts, err := e.alertStorage.GetActiveAlerts(ctx, map[string]string{
				"resourceName": n.Name, "namespace": n.Namespace,
			})
			if err != nil {
				continue
			}
			for _, a := range alerts {
				seen[a.Fingerprint] = true
			}
		}
	}

	fallbackAlerts, _ := e.alertStorage.GetActiveAlerts(ctx, map[string]string{
		"namespace": alert.Namespace, "resourceType": alert.ResourceType,
	})
	for _, a := range fallbackAlerts {
		seen[a.Fingerprint] = true
	}

	fingerprints := make([]string, 0, len(seen))
	for fp := range seen {
		if fp != alert.Fingerprint {
			fingerprints = append(fingerprints, fp)
		}
	}
	if len(fingerprints) > 50 {
		fingerprints = fingerprints[:50]
	}
	return fingerprints
}

func (e *Engine) generateRemediations(ctx context.Context, causes []diagnosis.RootCause, impact diagnosis.ImpactAssessment) []diagnosis.RemediationSuggestion {
	var remediations []diagnosis.RemediationSuggestion

	for _, cause := range causes {
		kbSuggestions := e.knowledgeBase.GetSuggestions(cause.ResourceType, cause.Evidence)
		remediations = append(remediations, kbSuggestions...)
	}

	if len(remediations) == 0 && len(causes) > 0 {
		cause := causes[0]
		remediations = append(remediations, diagnosis.RemediationSuggestion{
			Action:      "人工排查",
			Description: fmt.Sprintf("需要人工排查 %s/%s", cause.ResourceType, cause.ResourceName),
			Steps: []string{
				"使用 kubectl describe 检查资源状态",
				"查看最近的事件和日志",
				"检查最近的部署或配置变更",
			},
			RiskLevel:   "low",
			AutoFixable: false,
		})
	}

	return remediations
}

func (e *Engine) generateSummary(result *diagnosis.DiagnosisResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("诊断 ID: %s\n", result.ID))
	sb.WriteString(fmt.Sprintf("时间: %s\n\n", result.Timestamp.Format(time.RFC3339)))

	if len(result.RootCauses) > 0 {
		sb.WriteString("根因:\n")
		for i, cause := range result.RootCauses {
			sb.WriteString(fmt.Sprintf("  %d. %s/%s (置信度: %.0f%%)\n", i+1, cause.ResourceType, cause.ResourceName, cause.Confidence*100))
			for _, ev := range cause.Evidence {
				sb.WriteString(fmt.Sprintf("     - %s\n", ev))
			}
		}
	}

	sb.WriteString(fmt.Sprintf("\n影响范围: %d 个资源受影响, 严重级别=%s\n", result.Impact.BlastRadius, result.Impact.Severity))

	if result.Impact.BusinessContext != "" {
		sb.WriteString(fmt.Sprintf("业务上下文: %s\n", result.Impact.BusinessContext))
	}
	if result.Impact.BusinessImpactNote != "" {
		sb.WriteString(fmt.Sprintf("业务影响: %s\n", result.Impact.BusinessImpactNote))
	}
	if len(result.Impact.AffectedAppNames) > 0 {
		sb.WriteString(fmt.Sprintf("受影响应用: %s\n", strings.Join(result.Impact.AffectedAppNames, ", ")))
	}

	if len(result.Remediations) > 0 {
		sb.WriteString("\n修复建议:\n")
		for i, r := range result.Remediations {
			sb.WriteString(fmt.Sprintf("  %d. [%s] %s\n", i+1, r.RiskLevel, r.Action))
		}
	}

	return sb.String()
}

func (e *Engine) GetGraphDB() interfaces.GraphDB {
	return e.graphDB
}

func (e *Engine) GetAlertStorage() alert_interfaces.AlertProcessor {
	return e.alertStorage
}

func (e *Engine) collectMetrics(ctx context.Context, resourceType, namespace, resourceName string, nodeName string) []diagnosis.MetricEntry {
	if e.metricsQuerier == nil {
		return nil
	}

	var metrics []diagnosis.MetricEntry

	switch resourceType {
	case "Pod":
		points, err := e.metricsQuerier.GetPodMetrics(ctx, namespace, resourceName)
		if err != nil {
			e.logger.Debug("Failed to get pod metrics", zap.Error(err))
		} else {
			for _, p := range points {
				metrics = append(metrics, diagnosis.MetricEntry{
					ResourceKind: "Pod",
					ResourceName: resourceName,
					MetricName:   p.MetricName,
					Value:        p.Value,
					Status:       p.Status,
				})
			}
		}
		if nodeName != "" {
			nodePoints, err := e.metricsQuerier.GetNodeMetrics(ctx, nodeName)
			if err != nil {
				e.logger.Debug("Failed to get node metrics", zap.Error(err))
			} else {
				for _, p := range nodePoints {
					metrics = append(metrics, diagnosis.MetricEntry{
						ResourceKind: "Node",
						ResourceName: nodeName,
						MetricName:   p.MetricName,
						Value:        p.Value,
						Status:       p.Status,
					})
				}
			}
		}
	case "Node":
		points, err := e.metricsQuerier.GetNodeMetrics(ctx, resourceName)
		if err != nil {
			e.logger.Debug("Failed to get node metrics", zap.Error(err))
		} else {
			for _, p := range points {
				metrics = append(metrics, diagnosis.MetricEntry{
					ResourceKind: "Node",
					ResourceName: resourceName,
					MetricName:   p.MetricName,
					Value:        p.Value,
					Status:       p.Status,
				})
			}
		}
	case "Deployment":
		points, err := e.metricsQuerier.GetDeploymentMetrics(ctx, namespace, resourceName)
		if err != nil {
			e.logger.Debug("Failed to get deployment metrics", zap.Error(err))
		} else {
			for _, p := range points {
				metrics = append(metrics, diagnosis.MetricEntry{
					ResourceKind: "Deployment",
					ResourceName: resourceName,
					MetricName:   p.MetricName,
					Value:        p.Value,
					Status:       p.Status,
				})
			}
		}
	}

	return metrics
}

func (e *Engine) collectLogs(ctx context.Context, resourceType, namespace, resourceName string) []diagnosis.LogEntry {
	if e.logQuerier == nil {
		return nil
	}

	switch resourceType {
	case "Pod":
		entries, err := e.logQuerier.GetPodLogs(ctx, namespace, resourceName, "", 15, 20)
		if err != nil {
			e.logger.Debug("Failed to get pod logs", zap.Error(err))
			return nil
		}
		var logs []diagnosis.LogEntry
		for _, entry := range entries {
			logs = append(logs, diagnosis.LogEntry{
				Timestamp: entry.Timestamp,
				Level:     entry.Level,
				Message:   entry.Message,
				Pod:       entry.Pod,
				Namespace: entry.Namespace,
				Container: entry.Container,
			})
		}
		return logs
	default:
		entries, err := e.logQuerier.GetErrorLogs(ctx, namespace, 15)
		if err != nil {
			e.logger.Debug("Failed to get error logs", zap.Error(err))
			return nil
		}
		var logs []diagnosis.LogEntry
		for _, entry := range entries {
			logs = append(logs, diagnosis.LogEntry{
				Timestamp: entry.Timestamp,
				Level:     entry.Level,
				Message:   entry.Message,
				Pod:       entry.Pod,
				Namespace: entry.Namespace,
				Container: entry.Container,
			})
		}
		return logs
	}
}

// GetProviderInfo exposes provider info for HTTPLLMProvider without exposing API keys
func (p *HTTPLLMProvider) GetProviderInfo() (provider, model, endpoint string) {
	if p == nil {
		return "", "", ""
	}
	return p.provider, p.model, p.resolveEndpoint("default")
}

func isInfrastructurePod(podName, namespace string) bool {
	if namespace != "kube-system" {
		return false
	}
	infraPrefixes := []string{
		"coredns", "kube-proxy", "etcd", "calico", "cilium",
		"kube-apiserver", "kube-scheduler", "kube-controller",
	}
	for _, prefix := range infraPrefixes {
		if strings.HasPrefix(podName, prefix) {
			return true
		}
	}
	return false
}

// collectDiagnosisContextFallback 当 ContextCollector 不可用时，回退到串行收集逻辑
func (e *Engine) collectDiagnosisContextFallback(ctx context.Context, alert *alert_models.ProcessedAlert, preloadedTopology *diagnosis.TopologySnapshot) *PipelineContext {
	pc := &PipelineContext{Alert: alert}

	uid := alert.ResourceUID
	if uid == "" && alert.ResourceType != "" && alert.ResourceName != "" {
		uid = e.querier.ResolveUID(alert.ResourceType, alert.ResourceName, alert.Namespace)
		alert.ResourceUID = uid
	}
	if uid != "" {
		if alert.EnrichTags == nil {
			alert.EnrichTags = make(map[string]string)
		}
		alert.EnrichTags["_resolvedUID"] = uid
	}

	pc.Impact = e.assessor.AssessImpactWithBusiness(
		uid, alert.ResourceType, alert.ResourceName, alert.Namespace,
		alert.Routing.Severity, alert.BusinessContext, alert.BusinessImpact,
	)

	if alert.BusinessContext.AppName != "" {
		pc.BusinessCalls = e.querier.QueryBusinessAppCalls(alert.BusinessContext.AppName, alert.BusinessContext.Namespace)
	}

	if uid != "" || alert.BusinessContext.AppName != "" {
		pc.BizImpactCtx = e.querier.BuildBusinessImpactContext(uid, alert.BusinessContext)
	}

	if preloadedTopology != nil {
		pc.Topology = preloadedTopology
	} else if uid != "" {
		pc.Topology = e.querier.GetTopologySnapshot(uid)
	}

	pc.RelatedAlerts = e.findRelatedAlerts(ctx, alert)

	return pc
}
