package diagnosis

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"gitee.com/tddh/mutong/interfaces"
	alert_interfaces "gitee.com/tddh/mutong/interfaces/alert"
	alert_models "gitee.com/tddh/mutong/models/alert"
	"gitee.com/tddh/mutong/models/diagnosis"
)

type PipelineContext struct {
	Alert            *alert_models.ProcessedAlert
	Topology         *diagnosis.TopologySnapshot
	Impact           diagnosis.ImpactAssessment
	Metrics          interface{}
	PodLogs          string
	EsLogs           []interfaces.LogEntry
	ErrorLogs        []interfaces.LogEntry
	BusinessCalls    *diagnosis.BusinessAppCalls
	BizImpactCtx     *BusinessImpactContext
	KnowledgeMatches []diagnosis.RemediationSuggestion
	SimilarCases     []SimilarCaseResult
	RelatedAlerts    []string
	Errors           []ContextError
}

type SimilarCaseResult struct {
	Fingerprint string  `json:"fingerprint"`
	Similarity  float64 `json:"similarity"`
	Summary     string  `json:"summary"`
}

type ContextError struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}

type ContextCollector struct {
	logger          interfaces.Logger
	alertStorage    alert_interfaces.AlertProcessor
	metricsQuerier  interfaces.MetricsQuerier
	logQuerier      interfaces.LogQuerier
	k8sClient       *kubernetes.Clientset
	querier         *TopologyQuerier
	assessor        *ImpactAssessor
	knowledgeBase   *KnowledgeBase
	hybridRetriever *HybridRetriever
	semaphore       chan struct{}
}

func NewContextCollector(
	logger interfaces.Logger,
	alertStorage alert_interfaces.AlertProcessor,
	metricsQuerier interfaces.MetricsQuerier,
	logQuerier interfaces.LogQuerier,
	k8sClient *kubernetes.Clientset,
	querier *TopologyQuerier,
	assessor *ImpactAssessor,
	knowledgeBase *KnowledgeBase,
	hybridRetriever *HybridRetriever,
	maxConcurrent int,
) *ContextCollector {
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	return &ContextCollector{
		logger:          logger,
		alertStorage:    alertStorage,
		metricsQuerier:  metricsQuerier,
		logQuerier:      logQuerier,
		k8sClient:       k8sClient,
		querier:         querier,
		assessor:        assessor,
		knowledgeBase:   knowledgeBase,
		hybridRetriever: hybridRetriever,
		semaphore:       make(chan struct{}, maxConcurrent),
	}
}

func (c *ContextCollector) Collect(ctx context.Context, alert *alert_models.ProcessedAlert) *PipelineContext {
	select {
	case c.semaphore <- struct{}{}:
		defer func() { <-c.semaphore }()
	case <-ctx.Done():
		return &PipelineContext{
			Alert:  alert,
			Errors: []ContextError{{Source: "collector", Message: "pipeline cancelled"}},
		}
	default:
		c.logger.Warn(
			"Diagnosis pipeline saturated, request rejected",
			zap.String("fingerprint", alert.Fingerprint),
		)
		return &PipelineContext{
			Alert:  alert,
			Errors: []ContextError{{Source: "collector", Message: "pipeline saturated"}},
		}
	}

	startTime := time.Now()
	c.logger.Info(
		"Diagnosis pipeline started",
		zap.String("fingerprint", alert.Fingerprint),
		zap.String("resource", alert.ResourceType+"/"+alert.ResourceName),
	)

	result := &PipelineContext{Alert: alert}
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(5)

	go func() {
		defer wg.Done()
		uid := alert.ResourceUID
		if uid == "" && alert.ResourceType != "" && alert.ResourceName != "" {
			uid = c.querier.ResolveUID(alert.ResourceType, alert.ResourceName, alert.Namespace)
			if uid != "" {
				alert.ResourceUID = uid
			}
		}
		if uid == "" {
			c.logger.Warn("Topology snapshot skipped: no resource UID",
				zap.String("resource", alert.ResourceType+"/"+alert.ResourceName))
			return
		}
		gStart := time.Now()
		c.logger.Info("Collecting topology snapshot", zap.String("uid", uid))
		topo := c.querier.GetTopologySnapshot(uid)
		mu.Lock()
		result.Topology = topo
		mu.Unlock()
		if topo == nil || len(topo.Nodes) == 0 {
			c.logger.Warn(
				"Topology snapshot empty",
				zap.String("uid", uid),
				zap.Duration("duration", time.Since(gStart)),
			)
		} else {
			c.logger.Info(
				"Topology snapshot collected",
				zap.Int("nodes", len(topo.Nodes)),
				zap.Int("edges", len(topo.Edges)),
				zap.Duration("duration", time.Since(gStart)),
			)
		}
	}()

	go func() {
		defer wg.Done()
		if c.metricsQuerier == nil || alert.ResourceType == "" || alert.ResourceName == "" {
			return
		}
		gStart := time.Now()
		c.logger.Info(
			"Collecting resource metrics",
			zap.String("type", alert.ResourceType),
			zap.String("name", alert.ResourceName),
		)
		metrics, err := c.collectMetrics(ctx, alert)
		mu.Lock()
		if err != nil {
			result.Errors = append(result.Errors, ContextError{Source: "metrics", Message: err.Error()})
			c.logger.Warn(
				"Failed to collect metrics",
				zap.String("resource", alert.ResourceType+"/"+alert.ResourceName),
				zap.Error(err),
				zap.Duration("duration", time.Since(gStart)),
			)
		} else {
			result.Metrics = metrics
			c.logger.Info(
				"Resource metrics collected",
				zap.Duration("duration", time.Since(gStart)),
			)
		}
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		if alert.ResourceType != "Pod" || alert.ResourceName == "" || alert.Namespace == "" {
			return
		}
		var logWg sync.WaitGroup
		logWg.Add(2)

		go func() {
			defer logWg.Done()
			if c.k8sClient == nil {
				return
			}
			gStart := time.Now()
			c.logger.Info(
				"Collecting K8s pod logs",
				zap.String("namespace", alert.Namespace),
				zap.String("pod", alert.ResourceName),
			)
			podLogs, err := c.collectK8sPodLogs(ctx, alert.Namespace, alert.ResourceName)
			mu.Lock()
			if err != nil {
				result.Errors = append(result.Errors, ContextError{Source: "k8s_logs", Message: err.Error()})
				c.logger.Warn("Failed to collect K8s pod logs", zap.Error(err))
			} else {
				result.PodLogs = podLogs
				c.logger.Info(
					"K8s pod logs collected",
					zap.Int("chars", len(podLogs)),
					zap.Duration("duration", time.Since(gStart)),
				)
			}
			mu.Unlock()
		}()

		go func() {
			defer logWg.Done()
			if c.logQuerier == nil {
				return
			}
			gStart := time.Now()
			c.logger.Info(
				"Collecting ES pod logs",
				zap.String("namespace", alert.Namespace),
				zap.String("pod", alert.ResourceName),
			)
			esLogs, err := c.logQuerier.GetPodLogs(ctx, alert.Namespace, alert.ResourceName, "", 30, 50)
			mu.Lock()
			if err != nil {
				result.Errors = append(result.Errors, ContextError{Source: "es_logs", Message: err.Error()})
				c.logger.Warn("Failed to collect ES pod logs", zap.Error(err))
			} else {
				result.EsLogs = esLogs
				c.logger.Info(
					"ES pod logs collected",
					zap.Int("entries", len(esLogs)),
					zap.Duration("duration", time.Since(gStart)),
				)
			}
			mu.Unlock()
		}()
		logWg.Wait()

		if c.logQuerier != nil {
			gStart := time.Now()
			errorLogs, err := c.logQuerier.GetErrorLogs(ctx, alert.Namespace, 30)
			mu.Lock()
			if err != nil {
				result.Errors = append(result.Errors, ContextError{Source: "error_logs", Message: err.Error()})
				c.logger.Warn("Failed to collect ES error logs", zap.Error(err))
			} else {
				result.ErrorLogs = errorLogs
				c.logger.Info(
					"ES error logs collected",
					zap.Int("entries", len(errorLogs)),
					zap.Duration("duration", time.Since(gStart)),
				)
			}
			mu.Unlock()
		}
	}()

	go func() {
		defer wg.Done()
		gStart := time.Now()
		c.logger.Info("Collecting impact assessment and business context")

		impact := c.assessor.AssessImpactWithBusiness(
			alert.ResourceUID,
			alert.ResourceType,
			alert.ResourceName,
			alert.Namespace,
			alert.Routing.Severity,
			alert.BusinessContext,
			alert.BusinessImpact,
		)

		var businessCalls *diagnosis.BusinessAppCalls
		if alert.BusinessContext.AppName != "" {
			businessCalls = c.querier.QueryBusinessAppCalls(
				alert.BusinessContext.AppName,
				alert.BusinessContext.Namespace,
			)
		}

		var bizImpactCtx *BusinessImpactContext
		if alert.ResourceUID != "" || alert.BusinessContext.AppName != "" {
			bizImpactCtx = c.querier.BuildBusinessImpactContext(alert.ResourceUID, alert.BusinessContext)
		}

		mu.Lock()
		result.Impact = impact
		result.BusinessCalls = businessCalls
		result.BizImpactCtx = bizImpactCtx
		mu.Unlock()

		c.logger.Info(
			"Impact assessment collected",
			zap.String("severity", impact.Severity),
			zap.Int("blast_radius", impact.BlastRadius),
			zap.Duration("duration", time.Since(gStart)),
		)
	}()

	go func() {
		defer wg.Done()
		gStart := time.Now()

		knowledgeMatches := c.knowledgeBase.GetSuggestions(alert.ResourceType, nil)

		var similarCases []SimilarCaseResult
		if c.hybridRetriever != nil && alert.Fingerprint != "" {
			c.logger.Info("Hybrid searching similar cases",
				zap.String("fingerprint", alert.Fingerprint),
				zap.String("uid", alert.ResourceUID))

			req := diagnosis.HybridSearchRequest{
				AlertFingerprint: alert.Fingerprint,
				ResourceUID:      alert.ResourceUID,
				ResourceKind:     alert.ResourceType,
				ResourceName:     alert.ResourceName,
				Namespace:        alert.Namespace,
				Limit:            5,
				SemanticWeight:   0.6,
			}
			hybridResults, err := c.hybridRetriever.Search(ctx, req)
			if err != nil {
				c.logger.Warn("Hybrid search failed", zap.Error(err))
			} else {
				for _, r := range hybridResults {
					similarCases = append(similarCases, SimilarCaseResult{
						Fingerprint: r.Fingerprint,
						Similarity:  r.CombinedScore,
						Summary:     r.Summary,
					})
				}
				c.logger.Info(
					"Hybrid search completed",
					zap.Int("count", len(similarCases)),
					zap.Duration("duration", time.Since(gStart)),
				)
			}
		}

		mu.Lock()
		result.KnowledgeMatches = knowledgeMatches
		result.SimilarCases = similarCases
		mu.Unlock()
	}()

	wg.Wait()

	if len(alert.Labels) > 0 {
		result.RelatedAlerts = c.findRelatedAlerts(ctx, alert)
	}

	c.logger.Info(
		"Diagnosis pipeline completed",
		zap.String("fingerprint", alert.Fingerprint),
		zap.Int("errors", len(result.Errors)),
		zap.Duration("total_duration", time.Since(startTime)),
	)
	return result
}

func (c *ContextCollector) collectMetrics(ctx context.Context, alert *alert_models.ProcessedAlert) (interface{}, error) {
	switch alert.ResourceType {
	case "Pod":
		return c.metricsQuerier.GetPodMetrics(ctx, alert.Namespace, alert.ResourceName)
	case "Node":
		return c.metricsQuerier.GetNodeMetrics(ctx, alert.ResourceName)
	case "Deployment":
		return c.metricsQuerier.GetDeploymentMetrics(ctx, alert.Namespace, alert.ResourceName)
	default:
		return nil, fmt.Errorf("unsupported resource type for metrics: %s", alert.ResourceType)
	}
}

func (c *ContextCollector) collectK8sPodLogs(ctx context.Context, namespace, podName string) (string, error) {
	podObj, err := c.k8sClient.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get pod info: %w", err)
	}
	if len(podObj.Spec.Containers) == 0 {
		return "", fmt.Errorf("pod %s/%s has no containers", namespace, podName)
	}
	containerName := podObj.Spec.Containers[0].Name

	tailLines := int64(100)
	req := c.k8sClient.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tailLines,
	})
	body, err := req.DoRaw(ctx)
	if err != nil {
		return "", fmt.Errorf("k8s API get logs: %w", err)
	}

	result := string(body)
	const maxChars = 4000
	if len(result) > maxChars {
		result = "...(已截断)\n" + result[len(result)-maxChars:]
	}
	return result, nil
}

func (c *ContextCollector) findRelatedAlerts(ctx context.Context, alert *alert_models.ProcessedAlert) []string {
	if c.alertStorage == nil {
		return nil
	}
	seen := map[string]bool{alert.Fingerprint: true}

	if c.querier != nil && alert.ResourceUID != "" {
		flags := buildExpandFlags(alert, nil)
		neighbors := c.querier.ExpandRelatedResourceUIDs(
			alert.ResourceUID, alert.NodeName, alert.Namespace,
			alert.OwnerKind, alert.OwnerName,
			alert.BusinessContext.AppName, alert.BusinessContext.Namespace,
			20, flags,
		)
		for _, n := range neighbors {
			if n.Name == "" || n.Namespace == "" {
				continue
			}
			alerts, err := c.alertStorage.GetActiveAlerts(ctx, map[string]string{
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

	fallbackAlerts, _ := c.alertStorage.GetActiveAlerts(ctx, map[string]string{
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

func (pc *PipelineContext) ToDiagnosisPrompt() interfaces.DiagnosisPrompt {
	return interfaces.DiagnosisPrompt{
		Alert:                 pc.Alert,
		TopologySnapshot:      pc.Topology,
		ImpactAssessment:      &pc.Impact,
		KnowledgeMatches:      pc.KnowledgeMatches,
		RelatedAlerts:         pc.RelatedAlerts,
		BusinessAppCalls:      pc.BusinessCalls,
		BusinessImpactContext: pc.BizImpactCtx,
	}
}
