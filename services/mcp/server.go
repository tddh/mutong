package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"

	"gitee.com/tddh/mutong/interfaces"
	alert_interfaces "gitee.com/tddh/mutong/interfaces/alert"
	"gitee.com/tddh/mutong/models/diagnosis"
	"gitee.com/tddh/mutong/services"
	"gitee.com/tddh/mutong/services/alert"
	diagnosis_svc "gitee.com/tddh/mutong/services/diagnosis"
)

var (
	topologyCacheHits   = prometheus.NewCounter(prometheus.CounterOpts{Name: "mcp_topology_cache_hits_total", Help: "Total MCP topology cache hits"})
	topologyCacheMisses = prometheus.NewCounter(prometheus.CounterOpts{Name: "mcp_topology_cache_misses_total", Help: "Total MCP topology cache misses"})
)

func init() {
	prometheus.MustRegister(topologyCacheHits)
	prometheus.MustRegister(topologyCacheMisses)
}

type Server struct {
	logger          interfaces.Logger
	diagnosisEngine *diagnosis_svc.Engine
	metricsQuerier  interfaces.MetricsQuerier
	k8sClient       *kubernetes.Clientset
	logQuerier      interfaces.LogQuerier
	cache           interfaces.Cache
	tools           map[string]ToolHandler
	vectorRetriever *diagnosis_svc.VectorRetriever
	inspectionSvc   interfaces.InspectionProcessor
	informerGetter  func() dynamicinformer.DynamicSharedInformerFactory
	retroGen        RetroGenerator
}

type ToolHandler func(ctx context.Context, args map[string]string) (string, error)

type RetroGenerator func(ctx context.Context, fingerprint string) (string, error)

func (s *Server) WithRetrospectiveGenerator(gen RetroGenerator) *Server {
	s.retroGen = gen
	s.safeRegisterGenRetrospective()
	return s
}

func NewServer(logger interfaces.Logger, engine *diagnosis_svc.Engine, metricsQuerier interfaces.MetricsQuerier, k8sClient *kubernetes.Clientset, logQuerier interfaces.LogQuerier, cache interfaces.Cache, vectorRetriever *diagnosis_svc.VectorRetriever, inspectionSvc interfaces.InspectionProcessor, informerGetter func() dynamicinformer.DynamicSharedInformerFactory) *Server {
	s := &Server{
		logger:          logger,
		diagnosisEngine: engine,
		metricsQuerier:  metricsQuerier,
		k8sClient:       k8sClient,
		logQuerier:      logQuerier,
		cache:           cache,
		tools:           make(map[string]ToolHandler),
		vectorRetriever: vectorRetriever,
		inspectionSvc:   inspectionSvc,
		informerGetter:  informerGetter,
	}
	s.registerTools()
	return s
}

func (s *Server) registerTools() {
	s.tools["query_topology"] = s.handleQueryTopology
	s.tools["get_active_alerts"] = s.handleGetActiveAlerts
	s.tools["get_alert_detail"] = s.handleGetAlertDetail
	s.tools["run_diagnosis"] = s.handleRunDiagnosis
	s.tools["inspect_resource"] = s.handleInspectResource
	s.tools["get_inspection_report"] = s.handleGetInspectionReport
	s.tools["list_resources_from_graph"] = s.handleListResources
	s.tools["list_k8s_resources"] = s.handleListK8sResources
	s.tools["list_resources_from_cache"] = s.handleListResourcesFromCache
	s.tools["get_resource_metrics"] = s.handleGetResourceMetrics
	s.tools["query_metric_timeseries"] = s.handleQueryMetricTimeseries
	s.tools["get_system_health"] = s.handleGetSystemHealth
	s.tools["get_metric_catalog"] = s.handleGetMetricCatalog
	s.tools["get_pod_logs"] = s.handleGetPodLogs
	s.tools["get_pod_logs_es"] = s.handleGetPodLogsES
	s.tools["search_logs"] = s.handleSearchLogs
	s.tools["get_error_logs"] = s.handleGetErrorLogs
	s.tools["search_similar_cases"] = s.handleSearchSimilarCases
	s.tools["list_alerts"] = s.handleListAlerts
}

func (s *Server) safeRegisterGenRetrospective() {
	if s.retroGen != nil {
		s.tools["generate_retrospective"] = s.handleGenerateRetrospective
	}
}

func (s *Server) ExecuteTool(ctx context.Context, name string, args map[string]string) (string, error) {
	handler, ok := s.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	startTime := time.Now()
	s.logger.Info("MCP tool executing", zap.String("tool", name), zap.Any("args", args))
	result, err := handler(ctx, args)
	duration := time.Since(startTime)
	if err != nil {
		s.logger.Error(
			"MCP tool execution failed",
			zap.String("tool", name),
			zap.Duration("duration", duration),
			zap.Error(err),
		)
		return "", err
	}
	resultLen := len(result)
	summary := result
	if resultLen > 200 {
		summary = result[:200] + "..."
	}
	s.logger.Info(
		"MCP tool executed successfully",
		zap.String("tool", name),
		zap.Int("result_len", resultLen),
		zap.Duration("duration", duration),
		zap.String("summary", summary),
	)
	return result, nil
}

func (s *Server) ListTools() []diagnosis.ToolDefinition {
	return []diagnosis.ToolDefinition{
		{
			Name:        "query_topology",
			Description: "从 Nebula Graph 查询 K8s 资源拓扑关系，查找资源间的上下游依赖、服务关联。用于分析资源间依赖关系。",
			Parameters: []diagnosis.ParamDef{
				{Name: "resource_type", Required: true, Description: "资源类型：Pod, Node, Service, Deployment 等"},
				{Name: "resource_name", Required: true, Description: "资源名称"},
				{Name: "namespace", Required: false, Description: "Kubernetes 命名空间"},
			},
		},
		{
			Name:        "get_active_alerts",
			Description: "获取活跃告警列表，支持按命名空间、严重级别、资源类型筛选和分页。返回告警名称、资源、级别、摘要。",
			Parameters: []diagnosis.ParamDef{
				{Name: "namespace", Required: false, Description: "按命名空间筛选"},
				{Name: "severity", Required: false, Description: "按严重级别筛选：critical, error, warning, info"},
				{Name: "resource_type", Required: false, Description: "按资源类型筛选"},
				{Name: "limit", Required: false, Description: "每页条数（默认20，最大100）"},
				{Name: "offset", Required: false, Description: "偏移量（默认0）"},
			},
		},
		{
			Name:        "get_alert_detail",
			Description: "获取指定告警的详细信息，通过告警指纹查询",
			Parameters: []diagnosis.ParamDef{
				{Name: "fingerprint", Required: true, Description: "告警指纹"},
			},
		},
		{
			Name:        "run_diagnosis",
			Description: "执行 AI 辅助诊断，返回根因分析、影响评估和修复建议",
			Parameters: []diagnosis.ParamDef{
				{Name: "fingerprint", Required: false, Description: "告警指纹"},
				{Name: "namespace", Required: false, Description: "命名空间"},
				{Name: "resource", Required: false, Description: "资源类型"},
			},
		},
		{
			Name:        "inspect_resource",
			Description: "检查指定 Kubernetes 资源的实时状态（运行状态、容器状态、条件、副本数等），并附带 Prometheus 指标。直接从 K8s API 查询，不依赖缓存。用于排查单个资源的具体问题。",
			Parameters: []diagnosis.ParamDef{
				{Name: "resource_type", Required: true, Description: "资源类型：Pod/Deployment/Node/StatefulSet/DaemonSet"},
				{Name: "resource_name", Required: true, Description: "资源名称"},
				{Name: "namespace", Required: false, Description: "Kubernetes 命名空间"},
			},
		},
		{
			Name:        "get_inspection_report",
			Description: "获取最新的自动化巡检报告",
			Parameters:  []diagnosis.ParamDef{},
		},
		{
			Name:        "list_resources_from_graph",
			Description: "列出 Kubernetes 集群中的资源清单（从 NebulaGraph 图数据库查询，含拓扑关系）。用于快速浏览某类资源有哪些。",
			Parameters: []diagnosis.ParamDef{
				{Name: "resource_type", Required: true, Description: "资源类型：Pod, Node, Service, Deployment 等"},
				{Name: "namespace", Required: false, Description: "Kubernetes 命名空间"},
				{Name: "limit", Required: false, Description: "返回条数上限（默认 20，最大 200）"},
			},
		},
		{
			Name:        "list_k8s_resources",
			Description: "直接从 Kubernetes API 查询资源清单（实时数据，不含拓扑关系），按资源类型和命名空间筛选。用于确认资源是否真实存在。",
			Parameters: []diagnosis.ParamDef{
				{Name: "resource_type", Required: true, Description: "资源类型：Pod, Deployment, Service, Node, StatefulSet, DaemonSet 等"},
				{Name: "namespace", Required: false, Description: "Kubernetes 命名空间（Node 不需要，留空查所有命名空间）"},
				{Name: "limit", Required: false, Description: "返回条数上限（默认100，最大500）"},
			},
		},
		{
			Name:        "list_resources_from_cache",
			Description: "从本地 Informer 缓存查询资源清单（秒级新鲜度，不超时，支持全量查询）。比 list_k8s_resources 更快更可靠，推荐优先使用。支持 Pod/Deployment/Service/Node/StatefulSet/DaemonSet/ConfigMap/Secret/PVC/PV/Ingress/Job/CronJob 等内置资源。CRD 资源可用 api_group 参数指定 Group。",
			Parameters: []diagnosis.ParamDef{
				{Name: "resource_type", Required: true, Description: "资源类型：Pod, Deployment, Service, Node, ConfigMap, Secret, Ingress, Job, CronJob 等"},
				{Name: "namespace", Required: false, Description: "Kubernetes 命名空间（留空查所有命名空间）"},
				{Name: "limit", Required: false, Description: "返回条数上限（默认 200，最大 1000）"},
				{Name: "api_group", Required: false, Description: "CRD 资源的 API Group（如 cert-manager.io），内置资源无需传"},
			},
		},
		{
			Name:        "get_resource_metrics",
			Description: "获取指定 K8s 资源的当前指标（CPU、内存、网络、重启次数）",
			Parameters: []diagnosis.ParamDef{
				{Name: "resource_type", Required: true, Description: "资源类型：Pod, Node, Deployment"},
				{Name: "resource_name", Required: true, Description: "资源名称"},
				{Name: "namespace", Required: false, Description: "Kubernetes 命名空间"},
			},
		},
		{
			Name:        "query_metric_timeseries",
			Description: "查询指标时序数据，用于趋势分析",
			Parameters: []diagnosis.ParamDef{
				{Name: "expr", Required: true, Description: "PromQL 表达式"},
				{Name: "start", Required: false, Description: "开始时间（Unix 时间戳）"},
				{Name: "end", Required: false, Description: "结束时间（Unix 时间戳）"},
				{Name: "step", Required: false, Description: "步长（秒）"},
			},
		},
		{
			Name:        "get_system_health",
			Description: "获取系统整体健康状态，包括指标后端可用性",
			Parameters:  []diagnosis.ParamDef{},
		},
		{
			Name:        "get_metric_catalog",
			Description: "获取可用指标目录（含描述和单位），查询指标前先使用此工具了解可用指标",
			Parameters:  []diagnosis.ParamDef{},
		},
		{
			Name:        "get_pod_logs",
			Description: "从 Kubernetes API 获取 Pod 日志（可能超时，大日志建议用 get_pod_logs_es）",
			Parameters: []diagnosis.ParamDef{
				{Name: "namespace", Required: true, Description: "Kubernetes 命名空间"},
				{Name: "pod_name", Required: true, Description: "Pod 名称"},
				{Name: "container", Required: false, Description: "容器名称（可选，默认第一个容器）"},
				{Name: "tail_lines", Required: false, Description: "日志行数（默认200）"},
			},
		},
		{
			Name:        "get_pod_logs_es",
			Description: "从 Elasticsearch 获取 Pod 日志，适合查询历史日志",
			Parameters: []diagnosis.ParamDef{
				{Name: "namespace", Required: true, Description: "Kubernetes 命名空间"},
				{Name: "pod_name", Required: true, Description: "Pod 名称"},
				{Name: "container", Required: false, Description: "容器名称"},
				{Name: "since_minutes", Required: false, Description: "回溯分钟数（默认30）"},
				{Name: "tail", Required: false, Description: "最大日志条数（默认50）"},
			},
		},
		{
			Name:        "search_logs",
			Description: "按关键词搜索命名空间下所有 Pod 的日志",
			Parameters: []diagnosis.ParamDef{
				{Name: "query", Required: true, Description: "搜索关键词"},
				{Name: "namespace", Required: false, Description: "命名空间筛选"},
				{Name: "since_minutes", Required: false, Description: "回溯分钟数（默认30）"},
				{Name: "max_results", Required: false, Description: "最大结果数（默认20）"},
			},
		},
		{
			Name:        "get_error_logs",
			Description: "获取命名空间下的错误级别日志，用于快速定位问题",
			Parameters: []diagnosis.ParamDef{
				{Name: "namespace", Required: true, Description: "Kubernetes 命名空间"},
				{Name: "since_minutes", Required: false, Description: "回溯分钟数（默认30）"},
			},
		},
		{
			Name:        "search_similar_cases",
			Description: "使用向量相似度搜索历史故障案例，查找过去的事件和解决方案",
			Parameters: []diagnosis.ParamDef{
				{Name: "fingerprint", Required: false, Description: "告警指纹"},
				{Name: "query", Required: false, Description: "自然语言搜索查询"},
				{Name: "limit", Required: false, Description: "最大结果数（默认5）"},
			},
		},
		{
			Name:        "list_alerts",
			Description: "列出当前活跃告警（简化版），仅返回告警列表不含分页。如需分页和完整信息请用 get_active_alerts。",
			Parameters: []diagnosis.ParamDef{
				{Name: "namespace", Required: false, Description: "按命名空间筛选"},
				{Name: "severity", Required: false, Description: "按严重级别筛选：critical, error, warning, info"},
				{Name: "resource_type", Required: false, Description: "按资源类型筛选"},
				{Name: "limit", Required: false, Description: "返回条数（默认10，最大20）"},
			},
		},
		{
			Name:        "generate_retrospective",
			Description: "根据告警指纹（fingerprint）生成故障复盘分析报告。包含根因分析、因果链、影响评估、经验教训和改进项。通常用于告警恢复后进行复盘。",
			Parameters: []diagnosis.ParamDef{
				{Name: "fingerprint", Required: true, Description: "告警指纹（fingerprint），可从 get_active_alerts 或 get_alert_detail 获取"},
				{Name: "include_business_impact", Required: false, Description: "是否包含业务影响分析（默认 true）"},
			},
		},
		{
			Name:        "search_knowledge_base",
			Description: "搜索互联网获取实时技术信息。返回 AI 摘要、相关文章片段和来源链接。适用于排查陌生故障、查找最佳实践、获取社区解决方案，以及本地诊断工具无法提供足够上下文时使用。请勿用于查询集群内部状态（集群资源查询请用 list_resources_from_cache、get_pod_logs 等工具）。",
			Parameters: []diagnosis.ParamDef{
				{Name: "query", Required: true, Description: "搜索查询（自然语言，如 'Pod CrashLoopBackOff 排查方法'，尽量具体）"},
				{Name: "topic", Required: false, Description: "搜索类别：general（通用/技术文档）、news（最新动态）、security（安全公告），默认 general"},
			},
		},
		{
			Name:        "search_github_issues",
			Description: "搜索 GitHub Issues，查找开源项目中的已知 Bug、修复方案和社区讨论。返回 Issue 标题、编号、状态、标签和链接。适用于确认某个错误是否为已知的开源组件 Bug，或查找社区提供的 workaround。可指定仓库缩小范围。请勿用于查询集群内部数据（告警查询用 get_active_alerts，日志查询用 search_logs）。",
			Parameters: []diagnosis.ParamDef{
				{Name: "query", Required: true, Description: "搜索关键词（使用错误信息中的技术术语，如 'CrashLoopBackOff OOMKilled'）"},
				{Name: "repo", Required: false, Description: "限定仓库（格式 owner/repo，如 kubernetes/kubernetes），留空则全 GitHub 搜索"},
				{Name: "state", Required: false, Description: "Issue 状态：open（活跃）/closed（已关闭）/all（全部），默认 open"},
			},
		},
	}
}

func (s *Server) handleQueryTopology(ctx context.Context, args map[string]string) (string, error) {
	resourceType := args["resource_type"]
	resourceName := args["resource_name"]
	namespace := args["namespace"]

	// 接入 nGQL sanitizer 防止注入
	sanitizer := services.NewNGQLSanitizer()
	resourceType = sanitizer.EscapeString(resourceType)
	resourceName = sanitizer.EscapeString(resourceName)
	namespace = sanitizer.EscapeString(namespace)

	if resourceType == "" || resourceName == "" {
		return "", fmt.Errorf("resource_type and resource_name are required")
	}

	if !isValidResourceType(resourceType) {
		return "", fmt.Errorf("invalid resource type: %s", resourceType)
	}

	if !isValidResourceName(resourceName) {
		return "", fmt.Errorf("invalid resource name: %s", resourceName)
	}

	cacheKey := fmt.Sprintf("topo:%s:%s:%s", resourceType, resourceName, namespace)

	if s.cache != nil {
		if cached, err := s.cache.Get(cacheKey); err == nil {
			alert.TopologyCacheHits.Inc()
			return string(cached), nil
		}
		alert.TopologyCacheMisses.Inc()
	}

	nsClause := ""
	if namespace != "" {
		if !isValidNamespace(namespace) {
			return "", fmt.Errorf("invalid namespace: %s", namespace)
		}
		nsClause = fmt.Sprintf(`, namespace: "%s"`, namespace)
	}

	query := fmt.Sprintf(
		`MATCH p = (n:%s {name: "%s"%s})-[*1..3]-(related) RETURN p LIMIT 50`,
		resourceType, resourceName, nsClause,
	)

	result, err := s.diagnosisEngine.GetGraphDB().ExecuteAndCheck(query)
	if err != nil {
		return "", fmt.Errorf("topology query failed: %w", err)
	}

	if result == nil || result.GetRowSize() == 0 {
		return fmt.Sprintf("未找到 %s/%s 的拓扑关系", resourceType, resourceName), nil
	}

	var relations []string
	for i := 0; i < result.GetRowSize() && i < 30; i++ {
		row, err := result.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}
		relations = append(relations, fmt.Sprintf("%v", row))
	}
	resultStr := fmt.Sprintf("%s/%s 拓扑关系 (%d):\n%s", resourceType, resourceName, len(relations), strings.Join(relations, "\n"))

	if s.cache != nil {
		go func() {
			defer func() { _ = recover() }()
			_ = s.cache.Set(cacheKey, []byte(resultStr))
		}()
	}

	return resultStr, nil
}

func isValidResourceType(s string) bool {
	validTypes := map[string]bool{
		"Pod": true, "Node": true, "Service": true, "Deployment": true,
		"StatefulSet": true, "DaemonSet": true, "ReplicaSet": true,
		"ConfigMap": true, "Secret": true, "PersistentVolumeClaim": true,
		"Ingress": true, "EndpointSlice": true, "Namespace": true,
	}
	return validTypes[s]
}

func isValidResourceName(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func isValidNamespace(s string) bool {
	if len(s) == 0 || len(s) > 63 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}

func (s *Server) handleGetActiveAlerts(ctx context.Context, args map[string]string) (string, error) {
	filters := map[string]string{}
	if ns, ok := args["namespace"]; ok {
		filters["namespace"] = ns
	}
	if sev, ok := args["severity"]; ok {
		filters["severity"] = sev
	}
	if rt, ok := args["resource_type"]; ok {
		filters["resourceType"] = rt
	}

	limit := 20
	if v, ok := args["limit"]; ok {
		var l int64
		_, _ = fmt.Sscanf(v, "%d", &l)
		if l > 0 {
			limit = int(l)
			if limit > 100 {
				limit = 100
			}
		}
	}

	offset := 0
	if v, ok := args["offset"]; ok {
		var o int64
		_, _ = fmt.Sscanf(v, "%d", &o)
		if o > 0 {
			offset = int(o)
		}
	}

	alerts, err := s.diagnosisEngine.GetAlertStorage().GetActiveAlerts(ctx, filters)
	if err != nil {
		return "", fmt.Errorf("failed to get alerts: %w", err)
	}

	if len(alerts) == 0 {
		return "当前没有活跃告警", nil
	}

	total := len(alerts)
	if offset >= total {
		return fmt.Sprintf("当前共有 %d 条活跃告警，已超出分页范围", total), nil
	}

	end := offset + limit
	if end > total {
		end = total
	}
	alerts = alerts[offset:end]

	var lines []string
	for _, a := range alerts {
		lines = append(lines, fmt.Sprintf("[%s] %s/%s/%s - %s | fingerprint: %s",
			a.Routing.Severity, a.Namespace, a.ResourceType, a.ResourceName,
			a.Annotations["summary"], a.Fingerprint))
	}
	return fmt.Sprintf("活跃告警 (共%d条，当前显示%d-%d):\n%s\n\n💡 使用 get_alert_detail 查看详情时需要传 fingerprint 参数", total, offset+1, len(alerts)+offset, strings.Join(lines, "\n")), nil
}

func (s *Server) handleGetAlertDetail(ctx context.Context, args map[string]string) (string, error) {
	fingerprint := args["fingerprint"]
	if fingerprint == "" {
		return "", fmt.Errorf("fingerprint is required")
	}

	alert, err := s.diagnosisEngine.GetAlertStorage().GetAlertByFingerprint(ctx, fingerprint)
	if err != nil {
		return "", fmt.Errorf("alert not found: %w", err)
	}

	return fmt.Sprintf("[%s] %s\n资源: %s/%s/%s\n节点: %s\n摘要: %s\n业务: %s (团队:%s, 关键度:%s)",
		alert.Routing.Severity, alert.Labels["alertname"],
		alert.ResourceType, alert.ResourceName, alert.Namespace,
		alert.NodeName,
		alert.Annotations["summary"],
		alert.EnrichTags["businessApp"], alert.EnrichTags["team"], alert.EnrichTags["criticality"]), nil
}

func (s *Server) handleRunDiagnosis(ctx context.Context, args map[string]string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req := diagnosis.DiagnosisRequest{
		Fingerprint: args["fingerprint"],
		Namespace:   args["namespace"],
		Resource:    args["resource"],
	}

	result, err := s.diagnosisEngine.Diagnose(ctx, req)
	if err != nil {
		return "", fmt.Errorf("diagnosis failed: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("诊断完成 (置信度: %d%%)。\n\n", int(maxConf(result.RootCauses)*100)))
	if result.Summary != "" {
		sb.WriteString(result.Summary + "\n\n")
	}
	if len(result.Remediations) > 0 {
		sb.WriteString("建议:\n")
		for i, r := range result.Remediations {
			if i >= 3 {
				break
			}
			sb.WriteString(fmt.Sprintf("- %s\n", r.Description))
		}
	}
	return sb.String(), nil
}

func (s *Server) handleInspectResource(ctx context.Context, args map[string]string) (string, error) {
	resourceType := args["resource_type"]
	resourceName := args["resource_name"]
	namespace := args["namespace"]

	if resourceType == "" || resourceName == "" {
		return "", fmt.Errorf("resource_type and resource_name are required")
	}
	if s.k8sClient == nil {
		return "", fmt.Errorf("k8s client not configured")
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("=== %s/%s", resourceType, resourceName))
	if namespace != "" {
		result.WriteString(fmt.Sprintf(" (namespace: %s)", namespace))
	}
	result.WriteString(" ===\n\n")

	switch resourceType {
	case "Pod":
		pod, err := s.k8sClient.CoreV1().Pods(namespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("获取 Pod 失败: %w", err)
		}
		result.WriteString(fmt.Sprintf("状态: %s\n", pod.Status.Phase))
		result.WriteString(fmt.Sprintf("节点: %s\n", pod.Spec.NodeName))
		result.WriteString(fmt.Sprintf("重启策略: %s\n", pod.Spec.RestartPolicy))
		result.WriteString("\n容器状态:\n")
		for _, cs := range pod.Status.ContainerStatuses {
			ready := "NotReady"
			if cs.Ready {
				ready = "Ready"
			}
			result.WriteString(fmt.Sprintf("  - %s: %s, 镜像: %s, 重启: %d\n", cs.Name, ready, cs.Image, cs.RestartCount))
			if cs.State.Running != nil {
				result.WriteString(fmt.Sprintf("    运行中, 启动于: %s\n", cs.State.Running.StartedAt.Format(time.RFC3339)))
			}
			if cs.State.Waiting != nil {
				result.WriteString(fmt.Sprintf("    等待中: %s (原因: %s)\n", cs.State.Waiting.Reason, cs.State.Waiting.Message))
			}
			if cs.State.Terminated != nil {
				result.WriteString(fmt.Sprintf("    已终止: %s (退出码: %d)\n", cs.State.Terminated.Reason, cs.State.Terminated.ExitCode))
			}
		}
		if len(pod.Status.Conditions) > 0 {
			result.WriteString("\nPod 状态条件:\n")
			for _, cond := range pod.Status.Conditions {
				status := "True"
				if cond.Status != corev1.ConditionTrue {
					status = "False"
				}
				result.WriteString(fmt.Sprintf("  - %s: %s", cond.Type, status))
				if cond.Reason != "" {
					result.WriteString(fmt.Sprintf(" (%s)", cond.Reason))
				}
				if cond.Message != "" {
					result.WriteString(fmt.Sprintf(" - %s", cond.Message))
				}
				result.WriteString("\n")
			}
		}

	case "Deployment":
		dep, err := s.k8sClient.AppsV1().Deployments(namespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("获取 Deployment 失败: %w", err)
		}
		result.WriteString(fmt.Sprintf("期望副本: %d\n", *dep.Spec.Replicas))
		result.WriteString(fmt.Sprintf("就绪副本: %d\n", dep.Status.ReadyReplicas))
		result.WriteString(fmt.Sprintf("可用副本: %d\n", dep.Status.AvailableReplicas))
		result.WriteString(fmt.Sprintf("更新副本: %d\n", dep.Status.UpdatedReplicas))
		result.WriteString("\nDeployment 条件:\n")
		for _, cond := range dep.Status.Conditions {
			status := "True"
			if cond.Status != corev1.ConditionTrue {
				status = "False"
			}
			result.WriteString(fmt.Sprintf("  - %s: %s (%s)\n", cond.Type, status, cond.Reason))
		}

	case "Node":
		node, err := s.k8sClient.CoreV1().Nodes().Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("获取 Node 失败: %w", err)
		}
		result.WriteString("\nNode 状态:\n")
		for _, cond := range node.Status.Conditions {
			status := "True"
			if cond.Status != corev1.ConditionTrue {
				status = "False"
			}
			result.WriteString(fmt.Sprintf("  - %s: %s", cond.Type, status))
			if cond.Reason != "" {
				result.WriteString(fmt.Sprintf(" (%s)", cond.Reason))
			}
			if cond.Message != "" {
				result.WriteString(fmt.Sprintf(" - %s", cond.Message))
			}
			result.WriteString("\n")
		}
		result.WriteString(fmt.Sprintf("\n容量:\n  CPU: %s\n  内存: %s\n  Pod数: %s\n",
			node.Status.Capacity.Cpu(), node.Status.Capacity.Memory(), node.Status.Capacity.Pods()))

	case "StatefulSet":
		sts, err := s.k8sClient.AppsV1().StatefulSets(namespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("获取 StatefulSet 失败: %w", err)
		}
		result.WriteString(fmt.Sprintf("期望副本: %d\n", *sts.Spec.Replicas))
		result.WriteString(fmt.Sprintf("就绪副本: %d\n", sts.Status.ReadyReplicas))
		result.WriteString(fmt.Sprintf("当前副本: %d\n", sts.Status.CurrentReplicas))

	case "DaemonSet":
		ds, err := s.k8sClient.AppsV1().DaemonSets(namespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("获取 DaemonSet 失败: %w", err)
		}
		result.WriteString(fmt.Sprintf("期望节点数: %d\n", ds.Status.DesiredNumberScheduled))
		result.WriteString(fmt.Sprintf("就绪节点数: %d\n", ds.Status.NumberReady))
		result.WriteString(fmt.Sprintf("可用节点数: %d\n", ds.Status.NumberAvailable))

	default:
		return "", fmt.Errorf("不支持的资源类型: %s (支持 Pod/Deployment/Node/StatefulSet/DaemonSet)", resourceType)
	}

	if s.metricsQuerier != nil && resourceType != "Node" {
		result.WriteString("\n--- 指标数据 ---\n")
		metricsData, err := s.getMetricsForResource(ctx, resourceType, resourceName, namespace)
		if err == nil && metricsData != "" {
			result.WriteString(metricsData)
		} else {
			result.WriteString("(指标查询不可用)\n")
		}
	}

	return result.String(), nil
}

func (s *Server) getMetricsForResource(ctx context.Context, resourceType, resourceName, namespace string) (string, error) {
	switch resourceType {
	case "Pod":
		data, err := s.metricsQuerier.GetPodMetrics(ctx, namespace, resourceName)
		if err != nil {
			return "", err
		}
		b, _ := json.MarshalIndent(data, "", "  ")
		return string(b), nil
	case "Deployment":
		data, err := s.metricsQuerier.GetDeploymentMetrics(ctx, namespace, resourceName)
		if err != nil {
			return "", err
		}
		b, _ := json.MarshalIndent(data, "", "  ")
		return string(b), nil
	case "Node":
		data, err := s.metricsQuerier.GetNodeMetrics(ctx, resourceName)
		if err != nil {
			return "", err
		}
		b, _ := json.MarshalIndent(data, "", "  ")
		return string(b), nil
	case "StatefulSet":
		data, err := s.metricsQuerier.GetStatefulSetMetrics(ctx, namespace, resourceName)
		if err != nil {
			return "", err
		}
		b, _ := json.MarshalIndent(data, "", "  ")
		return string(b), nil
	case "DaemonSet":
		data, err := s.metricsQuerier.GetDaemonSetMetrics(ctx, namespace, resourceName)
		if err != nil {
			return "", err
		}
		b, _ := json.MarshalIndent(data, "", "  ")
		return string(b), nil
	case "Service":
		data, err := s.metricsQuerier.GetServiceMetrics(ctx, namespace, resourceName)
		if err != nil {
			return "", err
		}
		b, _ := json.MarshalIndent(data, "", "  ")
		return string(b), nil
	default:
		data, err := s.metricsQuerier.GetPodMetrics(ctx, namespace, resourceName)
		if err != nil {
			return "", err
		}
		b, _ := json.MarshalIndent(data, "", "  ")
		return string(b), nil
	}
}

func (s *Server) handleGetInspectionReport(ctx context.Context, args map[string]string) (string, error) {
	if s.inspectionSvc == nil {
		return "巡检服务未启用", nil
	}

	report, err := s.inspectionSvc.GetLatestReport()
	if err != nil {
		return "", fmt.Errorf("获取巡检报告失败: %w", err)
	}

	if report == nil {
		return "暂无巡检报告，请先执行巡检", nil
	}

	data, _ := json.MarshalIndent(report, "", "  ")
	return string(data), nil
}

func (s *Server) handleListResources(ctx context.Context, args map[string]string) (string, error) {
	resourceType := args["resource_type"]
	if resourceType == "" {
		return "", fmt.Errorf("resource_type is required")
	}

	if !isValidResourceType(resourceType) {
		return "", fmt.Errorf("invalid resource type: %s", resourceType)
	}

	namespace := args["namespace"]
	nsClause := ""
	if namespace != "" {
		if !isValidNamespace(namespace) {
			return "", fmt.Errorf("invalid namespace: %s", namespace)
		}
		nsClause = fmt.Sprintf(` AND v.K8sResource.name_space == "%s"`, namespace)
	}

	limit := 20
	if v := args["limit"]; v != "" {
		_, _ = fmt.Sscanf(v, "%d", &limit)
		if limit < 1 {
			limit = 20
		} else if limit > 200 {
			limit = 200
		}
	}

	result, err := s.diagnosisEngine.GetGraphDB().Execute(
		fmt.Sprintf(`MATCH (v:K8sResource) WHERE v.K8sResource.kind == "%s" AND v.K8sResource.is_deleted == false%s RETURN v.K8sResource.name AS name, v.K8sResource.name_space AS namespace LIMIT %d`, resourceType, nsClause, limit),
	)
	if err != nil {
		return "", fmt.Errorf("failed to list resources: %w", err)
	}

	var resources []string
	if result != nil {
		for i := 0; i < result.GetRowSize() && i < limit; i++ {
			row, err := result.GetRowValuesByIndex(i)
			if err != nil {
				continue
			}
			name, _ := row.GetValueByColName("name")
			ns, _ := row.GetValueByColName("namespace")
			nameStr := ""
			nsStr := ""
			if name != nil {
				nameStr, _ = name.AsString()
			}
			if ns != nil {
				nsStr, _ = ns.AsString()
			}
			if nsStr != "" {
				resources = append(resources, fmt.Sprintf("%s/%s", nsStr, nameStr))
			} else {
				resources = append(resources, nameStr)
			}
		}
	}

	if len(resources) == 0 {
		return fmt.Sprintf("未找到 %s 资源", resourceType), nil
	}
	return fmt.Sprintf("%s 资源列表 (共%d条):\n%s", resourceType, len(resources), strings.Join(resources, "\n")), nil
}

func (s *Server) handleListK8sResources(ctx context.Context, args map[string]string) (string, error) {
	resourceType := args["resource_type"]
	if resourceType == "" {
		return "", fmt.Errorf("resource_type is required")
	}
	if s.k8sClient == nil {
		return "", fmt.Errorf("k8s client not configured")
	}

	namespace := args["namespace"]
	limit := 100
	if v := args["limit"]; v != "" {
		_, _ = fmt.Sscanf(v, "%d", &limit)
		if limit < 1 {
			limit = 100
		} else if limit > 500 {
			limit = 500
		}
	}

	var items []string

	switch resourceType {
	case "Pod":
		pods, err := s.k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{Limit: int64(limit)})
		if err != nil {
			return "", fmt.Errorf("list pods failed: %w", err)
		}
		for _, p := range pods.Items {
			if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
				continue
			}
			items = append(items, fmt.Sprintf("%s/%s (%s)", p.Namespace, p.Name, p.Status.Phase))
		}

	case "Deployment":
		deps, err := s.k8sClient.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{Limit: int64(limit)})
		if err != nil {
			return "", fmt.Errorf("list deployments failed: %w", err)
		}
		for _, d := range deps.Items {
			items = append(items, fmt.Sprintf("%s/%s (ready:%d/%d)", d.Namespace, d.Name, d.Status.ReadyReplicas, d.Status.Replicas))
		}

	case "Service":
		svcs, err := s.k8sClient.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{Limit: int64(limit)})
		if err != nil {
			return "", fmt.Errorf("list services failed: %w", err)
		}
		for _, s := range svcs.Items {
			items = append(items, fmt.Sprintf("%s/%s (%s)", s.Namespace, s.Name, s.Spec.Type))
		}

	case "Node":
		nodes, err := s.k8sClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: int64(limit)})
		if err != nil {
			return "", fmt.Errorf("list nodes failed: %w", err)
		}
		for _, n := range nodes.Items {
			ready := "NotReady"
			for _, c := range n.Status.Conditions {
				if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
					ready = "Ready"
				}
			}
			items = append(items, fmt.Sprintf("%s (%s)", n.Name, ready))
		}

	case "StatefulSet":
		sts, err := s.k8sClient.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{Limit: int64(limit)})
		if err != nil {
			return "", fmt.Errorf("list statefulsets failed: %w", err)
		}
		for _, s := range sts.Items {
			items = append(items, fmt.Sprintf("%s/%s (ready:%d/%d)", s.Namespace, s.Name, s.Status.ReadyReplicas, s.Status.Replicas))
		}

	case "DaemonSet":
		ds, err := s.k8sClient.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{Limit: int64(limit)})
		if err != nil {
			return "", fmt.Errorf("list daemonsets failed: %w", err)
		}
		for _, d := range ds.Items {
			items = append(items, fmt.Sprintf("%s/%s (ready:%d)", d.Namespace, d.Name, d.Status.NumberReady))
		}

	default:
		return "", fmt.Errorf("unsupported resource type for K8s direct query: %s (support: Pod/Deployment/Service/Node/StatefulSet/DaemonSet)", resourceType)
	}

	if len(items) == 0 {
		return fmt.Sprintf("K8s API 未找到 %s 资源", resourceType), nil
	}
	return fmt.Sprintf("%s 资源列表 (K8s API 实时,共%d条):\n%s", resourceType, len(items), strings.Join(items, "\n")), nil
}

func (s *Server) handleListResourcesFromCache(ctx context.Context, args map[string]string) (string, error) {
	resourceType := args["resource_type"]
	if resourceType == "" {
		return "", fmt.Errorf("resource_type is required")
	}
	if s.informerGetter == nil {
		return "", fmt.Errorf("本地缓存未就绪，请稍后重试或使用 list_k8s_resources")
	}
	factory := s.informerGetter()
	if factory == nil {
		return "", fmt.Errorf("本地缓存未就绪，请稍后重试或使用 list_k8s_resources")
	}

	namespace := args["namespace"]
	limit := 200
	if v := args["limit"]; v != "" {
		_, _ = fmt.Sscanf(v, "%d", &limit)
		if limit < 1 {
			limit = 200
		} else if limit > 1000 {
			limit = 1000
		}
	}

	gvr, ok := resourceTypeToGVR(resourceType)
	if !ok {
		// CRD 资源：需要 api_group 参数
		apiGroup := args["api_group"]
		if apiGroup == "" {
			return "", fmt.Errorf("不支持从缓存查询的资源类型: %s (支持: Pod/Deployment/Service/Node/StatefulSet/DaemonSet/ConfigMap/Secret/PVC/PV/Ingress/Job/CronJob/Namespace/ReplicaSet/ServiceAccount)。CRD 请传 api_group 参数", resourceType)
		}
		resourceName := strings.ToLower(resourceType) + "s"
		gvr = schema.GroupVersionResource{Group: apiGroup, Version: "v1", Resource: resourceName}
	}

	informer := factory.ForResource(gvr).Informer()
	if !informer.HasSynced() {
		return "", fmt.Errorf("%s 缓存尚未同步完成，请稍后再试", resourceType)
	}

	store := informer.GetStore()
	allItems := store.List()

	var items []string
	activeCount := 0
	for _, obj := range allItems {
		u := obj.(*unstructured.Unstructured).DeepCopy()
		if namespace != "" && u.GetNamespace() != namespace {
			continue
		}
		// 跳过正在删除中的资源（DeletionTimestamp 不为空）
		if u.GetDeletionTimestamp() != nil {
			continue
		}
		// 对于 Pod，跳过已终止的（Succeeded/Failed/Unknown）
		if resourceType == "Pod" {
			phase, found, _ := unstructured.NestedString(u.Object, "status", "phase")
			if found && (phase == "Succeeded" || phase == "Failed" || phase == "Unknown") {
				continue
			}
		}
		activeCount++
		status := "Running"
		phase, found, _ := unstructured.NestedString(u.Object, "status", "phase")
		if found && phase != "" {
			status = phase
		}
		items = append(items, fmt.Sprintf("%s/%s (%s)", u.GetNamespace(), u.GetName(), status))
		if len(items) >= limit {
			break
		}
	}

	if len(items) == 0 {
		nsHint := ""
		if namespace != "" {
			nsHint = "（命名空间: " + namespace + "）"
		}
		return fmt.Sprintf("缓存中未找到 %s 资源%s", resourceType, nsHint), nil
	}

	totalHint := ""
	if len(items) < activeCount {
		totalHint = fmt.Sprintf("，仅显示前%d条，共%d条", limit, activeCount)
	}
	return fmt.Sprintf("%s 资源列表 (本地缓存,共%d条%s):\n%s", resourceType, activeCount, totalHint, strings.Join(items, "\n")), nil
}

func resourceTypeToGVR(resourceType string) (schema.GroupVersionResource, bool) {
	switch resourceType {
	case "Pod":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}, true
	case "Service":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, true
	case "Node":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"}, true
	case "ConfigMap":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, true
	case "Secret":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, true
	case "PersistentVolumeClaim":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"}, true
	case "PersistentVolume":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumes"}, true
	case "ServiceAccount":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"}, true
	case "Namespace":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}, true
	case "Deployment":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, true
	case "StatefulSet":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, true
	case "DaemonSet":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, true
	case "ReplicaSet":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, true
	case "Ingress":
		return schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, true
	case "Job":
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, true
	case "CronJob":
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, true
	}
	return schema.GroupVersionResource{}, false
}

func (s *Server) handleGetResourceMetrics(ctx context.Context, args map[string]string) (string, error) {
	if s.metricsQuerier == nil {
		return "", fmt.Errorf("metrics querier not configured")
	}

	resourceType := args["resource_type"]
	resourceName := args["resource_name"]
	namespace := args["namespace"]

	if resourceType == "" || resourceName == "" {
		return "", fmt.Errorf("resource_type and resource_name are required")
	}

	var results interface{}
	var err error

	switch resourceType {
	case "Pod":
		if namespace == "" {
			return "", fmt.Errorf("namespace is required for Pod")
		}
		results, err = s.metricsQuerier.GetPodMetrics(ctx, namespace, resourceName)
	case "Node":
		results, err = s.metricsQuerier.GetNodeMetrics(ctx, resourceName)
	case "Deployment":
		if namespace == "" {
			return "", fmt.Errorf("namespace is required for Deployment")
		}
		results, err = s.metricsQuerier.GetDeploymentMetrics(ctx, namespace, resourceName)
	case "StatefulSet":
		if namespace == "" {
			return "", fmt.Errorf("namespace is required for StatefulSet")
		}
		results, err = s.metricsQuerier.GetStatefulSetMetrics(ctx, namespace, resourceName)
	case "DaemonSet":
		if namespace == "" {
			return "", fmt.Errorf("namespace is required for DaemonSet")
		}
		results, err = s.metricsQuerier.GetDaemonSetMetrics(ctx, namespace, resourceName)
	case "Service":
		if namespace == "" {
			return "", fmt.Errorf("namespace is required for Service")
		}
		results, err = s.metricsQuerier.GetServiceMetrics(ctx, namespace, resourceName)
	default:
		return "", fmt.Errorf("unsupported resource type: %s", resourceType)
	}

	if err != nil {
		return "", fmt.Errorf("failed to get metrics: %w", err)
	}

	data, _ := json.MarshalIndent(results, "", "  ")
	// 限制返回长度，避免 LLM 上下文溢出
	if len(data) > 2000 {
		return string(data[:2000]) + "\n...(已截断)", nil
	}
	return string(data), nil
}

func (s *Server) handleQueryMetricTimeseries(ctx context.Context, args map[string]string) (string, error) {
	if s.metricsQuerier == nil {
		return "", fmt.Errorf("metrics querier not configured")
	}

	expr := args["expr"]
	if expr == "" {
		return "", fmt.Errorf("expr is required")
	}

	start := parseInt64(args["start"], time.Now().Add(-1*time.Hour).Unix())
	end := parseInt64(args["end"], time.Now().Unix())
	step := parseInt64(args["step"], 60)

	results, err := s.metricsQuerier.QueryMetricTimeseries(ctx, expr, start, end, step)
	if err != nil {
		return "", fmt.Errorf("failed to query timeseries: %w", err)
	}

	data, _ := json.MarshalIndent(results, "", "  ")
	if len(data) > 2000 {
		return string(data[:2000]) + "\n...(已截断)", nil
	}
	return string(data), nil
}

func (s *Server) handleGetSystemHealth(ctx context.Context, args map[string]string) (string, error) {
	type healthResult struct {
		Status  string `json:"status"`
		Message string `json:"message,omitempty"`
	}

	if s.metricsQuerier == nil {
		result := healthResult{Status: "unhealthy", Message: "metrics querier not configured"}
		data, _ := json.Marshal(result)
		return string(data), nil
	}

	err := s.metricsQuerier.CheckHealth(ctx)
	if err != nil {
		result := healthResult{Status: "unhealthy", Message: err.Error()}
		data, _ := json.Marshal(result)
		return string(data), nil
	}

	result := healthResult{Status: "healthy"}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func (s *Server) handleGetMetricCatalog(ctx context.Context, args map[string]string) (string, error) {
	if s.metricsQuerier == nil {
		return "", fmt.Errorf("metrics querier not configured")
	}

	catalog, err := s.metricsQuerier.GetMetricCatalog(ctx, false, true)
	if err != nil {
		return "", fmt.Errorf("failed to get metric catalog: %w", err)
	}

	data, _ := json.Marshal(catalog)
	if len(data) > 2000 {
		return string(data[:2000]) + "\n...(已截断，可使用 query_metric_timeseries 查询具体指标)", nil
	}
	return string(data), nil
}

func maxConf(causes []diagnosis.RootCause) float64 {
	var m float64
	for _, c := range causes {
		if c.Confidence > m {
			m = c.Confidence
		}
	}
	return m
}

func parseInt64(s string, defaultValue int64) int64 {
	if s == "" {
		return defaultValue
	}
	var result int64
	_, _ = fmt.Sscanf(s, "%d", &result)
	if result == 0 {
		return defaultValue
	}
	return result
}

func (s *Server) GetGraphDB() interfaces.GraphDB {
	return s.diagnosisEngine.GetGraphDB()
}

func (s *Server) GetAlertStorage() alert_interfaces.AlertProcessor {
	return s.diagnosisEngine.GetAlertStorage()
}

func (s *Server) handleGetPodLogs(ctx context.Context, args map[string]string) (string, error) {
	ns := args["namespace"]
	pod := args["pod_name"]
	if ns == "" || pod == "" {
		return "", fmt.Errorf("namespace and pod_name are required")
	}
	container := args["container"]
	tail := int64(200)
	if v := args["tail_lines"]; v != "" {
		var t int64
		_, _ = fmt.Sscanf(v, "%d", &t)
		if t > 0 {
			tail = t
		}
	}
	if s.k8sClient == nil {
		return "", fmt.Errorf("k8s client not configured")
	}

	// 未指定容器时，自动获取 Pod 的第一个容器名
	if container == "" {
		podObj, err := s.k8sClient.CoreV1().Pods(ns).Get(ctx, pod, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("获取 Pod 信息失败: %w", err)
		}
		if len(podObj.Spec.Containers) == 0 {
			return "", fmt.Errorf("Pod %s/%s 没有容器", ns, pod)
		}
		container = podObj.Spec.Containers[0].Name
	}

	req := s.k8sClient.CoreV1().Pods(ns).GetLogs(pod, &corev1.PodLogOptions{
		Container: container,
		TailLines: &tail,
	})
	body, err := req.DoRaw(ctx)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "NotFound") {
			return "", fmt.Errorf("未找到 Pod: %s/%s", ns, pod)
		}
		if strings.Contains(errMsg, "forbidden") || strings.Contains(errMsg, "Forbidden") || strings.Contains(errMsg, "unknown reason") {
			hint := ""
			if container == "" {
				hint = "。提示：未指定容器名可能导致此错误，请尝试指定 container 参数（如 container=kafka）"
			}
			return "", fmt.Errorf("K8s 无法获取 Pod 日志（权限不足或 API 拒绝）%s", hint)
		}
		return "", fmt.Errorf("K8s API 获取日志失败: %s", errMsg)
	}
	result := string(body)
	if len(result) > 4000 {
		result = "...(已截断)\n" + result[len(result)-4000:]
	}
	return result, nil
}

func (s *Server) handleGetPodLogsES(ctx context.Context, args map[string]string) (string, error) {
	if s.logQuerier == nil {
		return "", fmt.Errorf("log querier not configured")
	}
	ns := args["namespace"]
	pod := args["pod_name"]
	if ns == "" || pod == "" {
		return "", fmt.Errorf("namespace and pod_name are required")
	}
	sinceMin := parseInt64(args["since_minutes"], 30)
	tail := int(50)
	if v := args["tail"]; v != "" {
		var t int64
		_, _ = fmt.Sscanf(v, "%d", &t)
		if t > 0 {
			tail = int(t)
		}
	}
	entries, err := s.logQuerier.GetPodLogs(ctx, ns, pod, args["container"], int(sinceMin), tail)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "dial tcp") {
			return "", fmt.Errorf("Elasticsearch 连接失败：无法连接到配置地址。请检查 config.yaml 中 elasticsearch.addresses 配置是否正确且 ES 已启动")
		}
		if strings.Contains(errMsg, "404") || strings.Contains(errMsg, "not found") {
			return "", fmt.Errorf("ES 中未找到 %s/%s 的日志记录 (最近 %d 分钟)", ns, pod, sinceMin)
		}
		return "", fmt.Errorf("Elasticsearch 查询日志失败: %w", err)
	}
	data, _ := json.Marshal(entries)
	result := string(data)
	if len(result) > 4000 {
		result = result[:4000] + "\n...(已截断)"
	}
	return result, nil
}

func (s *Server) handleSearchLogs(ctx context.Context, args map[string]string) (string, error) {
	if s.logQuerier == nil {
		return "", fmt.Errorf("log querier not configured")
	}
	query := args["query"]
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	sinceMin := int(30)
	if v := args["since_minutes"]; v != "" {
		var t int64
		_, _ = fmt.Sscanf(v, "%d", &t)
		if t > 0 {
			sinceMin = int(t)
		}
	}
	maxResults := int(20)
	if v := args["max_results"]; v != "" {
		var t int64
		_, _ = fmt.Sscanf(v, "%d", &t)
		if t > 0 {
			maxResults = int(t)
		}
	}
	entries, err := s.logQuerier.SearchLogs(ctx, query, args["namespace"], sinceMin, maxResults)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "dial tcp") {
			return "", fmt.Errorf("Elasticsearch 连接失败：无法连接到配置地址。请检查 config.yaml 中 elasticsearch.addresses 配置是否正确且 ES 已启动")
		}
		return "", fmt.Errorf("Elasticsearch 关键词搜索失败: %w", err)
	}
	data, _ := json.Marshal(entries)
	result := string(data)
	if len(result) > 4000 {
		result = result[:4000] + "\n...(已截断)"
	}
	return result, nil
}

func (s *Server) handleGetErrorLogs(ctx context.Context, args map[string]string) (string, error) {
	if s.logQuerier == nil {
		return "", fmt.Errorf("log querier not configured")
	}
	ns := args["namespace"]
	if ns == "" {
		return "", fmt.Errorf("namespace is required")
	}
	sinceMin := int(30)
	if v := args["since_minutes"]; v != "" {
		var t int64
		_, _ = fmt.Sscanf(v, "%d", &t)
		if t > 0 {
			sinceMin = int(t)
		}
	}
	entries, err := s.logQuerier.GetErrorLogs(ctx, ns, sinceMin)
	if err != nil {
		return "", fmt.Errorf("failed to get error logs: %w", err)
	}
	data, _ := json.Marshal(entries)
	result := string(data)
	if len(result) > 4000 {
		result = result[:4000] + "\n...(已截断)"
	}
	return result, nil
}

func (s *Server) handleSearchSimilarCases(ctx context.Context, args map[string]string) (string, error) {
	if s.vectorRetriever == nil {
		return "", fmt.Errorf("vector retriever not configured (requires PostgreSQL with pgvector)")
	}

	fingerprint := args["fingerprint"]
	query := args["query"]
	limit := 5
	if v := args["limit"]; v != "" {
		var l int64
		_, _ = fmt.Sscanf(v, "%d", &l)
		if l > 0 && l <= 20 {
			limit = int(l)
		}
	}

	if fingerprint != "" {
		results, err := s.vectorRetriever.SearchSimilarByFingerprint(ctx, fingerprint, limit)
		if err != nil {
			return "", fmt.Errorf("similar case search failed: %w", err)
		}
		if len(results) == 0 {
			return "未找到相似的历史故障案例", nil
		}
		data, _ := json.Marshal(results)
		return string(data), nil
	}

	if query != "" {
		return "语义搜索需要 embedding 模型支持，请通过 fingerprint 参数查询相似案例", nil
	}

	return "", fmt.Errorf("fingerprint or query is required")
}

func (s *Server) handleListAlerts(ctx context.Context, args map[string]string) (string, error) {
	filters := map[string]string{}
	if ns, ok := args["namespace"]; ok {
		filters["namespace"] = ns
	}
	if sev, ok := args["severity"]; ok {
		filters["severity"] = sev
	}
	if rt, ok := args["resource_type"]; ok {
		filters["resourceType"] = rt
	}

	limit := 10
	if v, ok := args["limit"]; ok {
		var l int64
		_, _ = fmt.Sscanf(v, "%d", &l)
		if l > 0 && l <= 20 {
			limit = int(l)
		}
	}

	alerts, err := s.diagnosisEngine.GetAlertStorage().GetActiveAlerts(ctx, filters)
	if err != nil {
		return "", fmt.Errorf("获取告警列表失败: %w", err)
	}

	if len(alerts) == 0 {
		return "当前没有活跃告警", nil
	}

	if len(alerts) > limit {
		alerts = alerts[:limit]
	}

	var lines []string
	for _, a := range alerts {
		summary := a.Annotations["summary"]
		if summary == "" {
			summary = "无摘要"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s | 资源: %s/%s/%s | 摘要: %s | fingerprint: %s",
			a.Routing.Severity, a.Labels["alertname"],
			a.Namespace, a.ResourceType, a.ResourceName,
			summary, a.Fingerprint))
	}
	return fmt.Sprintf("活跃告警列表 (共%d条):\n%s", len(alerts), strings.Join(lines, "\n")), nil
}

func (s *Server) GetEinoToolDefsAndHandlers() map[string]diagnosis_svc.ToolDefAndHandler {
	result := make(map[string]diagnosis_svc.ToolDefAndHandler)
	for _, td := range s.ListTools() {
		if handler, ok := s.tools[td.Name]; ok {
			result[td.Name] = diagnosis_svc.ToolDefAndHandler{
				Description: td.Description,
				Params:      td.Parameters,
				Handler:     diagnosis_svc.ToolHandler(handler),
			}
		}
	}
	return result
}

func (s *Server) handleGenerateRetrospective(ctx context.Context, args map[string]string) (string, error) {
	fingerprint := args["fingerprint"]
	if fingerprint == "" {
		return "", fmt.Errorf("fingerprint is required")
	}
	if s.retroGen == nil {
		return "", fmt.Errorf("retrospective generator not configured")
	}
	return s.retroGen(ctx, fingerprint)
}
