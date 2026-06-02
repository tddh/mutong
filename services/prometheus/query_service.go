package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
)

type PrometheusConfig struct {
	Enabled       bool
	URL           string
	Timeout       int
	MetricMapping map[string][]MetricQueryConfig
	// Optional cache settings for Prometheus query results
	CacheEnabled bool
	CacheTTL     int
	CacheMaxSize int
}

type QueryService struct {
	logger  interfaces.Logger
	client  *http.Client
	baseURL string
	timeout time.Duration
	mapping map[string][]MetricQueryConfig
	cache   *PrometheusCache
}

// QueryRangeResult 保存时间范围查询的结果
type QueryRangeResult struct {
	MetricName string            `json:"metricName"`
	Values     []TimeSeriesPoint `json:"values"`
	Labels     map[string]string `json:"labels"`
}

type TimeSeriesPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

type MetricQueryConfig struct {
	ResourceKind      string
	MetricName        string
	QueryTemplate     string
	LabelKey          string
	Description       string
	Unit              string
	Category          string
	ThresholdWarning  float64
	ThresholdCritical float64
	TypicalRange      string
	ForUI             bool
	ForLLM            bool
}

type QueryResult struct {
	MetricName string            `json:"metricName"`
	Value      float64           `json:"value"`
	Timestamp  time.Time         `json:"timestamp"`
	Labels     map[string]string `json:"labels"`
	Status     string            `json:"status,omitempty"` // normal, warning, critical
}

type PrometheusResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// HPAStatus 表示 HPA 的当前状态信息
type HPAStatus struct {
	CurrentReplicas int32   `json:"current_replicas"`
	DesiredReplicas int32   `json:"desired_replicas"`
	MinReplicas     int32   `json:"min_replicas"`
	MaxReplicas     int32   `json:"max_replicas"`
	CPUUtilization  float64 `json:"cpu_utilization_percent"`
	Available       bool    `json:"available"`
}

// PrometheusRangeResponse 用于解析 /api/v1/query_range 的响应
type PrometheusRangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][]interface{}   `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func NewQueryService(logger interfaces.Logger, cfg PrometheusConfig) *QueryService {
	mapping := make(map[string][]MetricQueryConfig)

	if len(cfg.MetricMapping) > 0 {
		for key, queries := range cfg.MetricMapping {
			var configs []MetricQueryConfig
			for _, q := range queries {
				configs = append(configs, MetricQueryConfig{
					ResourceKind:      q.ResourceKind,
					MetricName:        q.MetricName,
					QueryTemplate:     q.QueryTemplate,
					LabelKey:          q.LabelKey,
					Description:       q.Description,
					Unit:              q.Unit,
					Category:          q.Category,
					ThresholdWarning:  q.ThresholdWarning,
					ThresholdCritical: q.ThresholdCritical,
					TypicalRange:      q.TypicalRange,
					ForUI:             q.ForUI,
					ForLLM:            q.ForLLM,
				})
			}
			mapping[key] = configs
		}
	} else {
		mapping = getDefaultMapping()
	}

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	baseURL := ""
	if cfg.URL != "" {
		baseURL = cfg.URL
		if !strings.HasSuffix(baseURL, "/api/v1/query") {
			baseURL = strings.TrimSuffix(baseURL, "/")
			if !strings.HasSuffix(baseURL, "/api/v1") {
				baseURL = baseURL + "/api/v1"
			}
		}
	}

	svc := &QueryService{
		logger:  logger,
		client:  &http.Client{Timeout: timeout},
		baseURL: baseURL,
		timeout: timeout,
		mapping: mapping,
	}

	// Initialize cache if enabled in config
	if cfg.CacheEnabled {
		if cfg.CacheTTL <= 0 {
			cfg.CacheTTL = 60
		}
		svc.cache = NewPrometheusCache(cfg.CacheTTL, cfg.CacheMaxSize)
	}

	return svc
}

func getDefaultMapping() map[string][]MetricQueryConfig {
	return map[string][]MetricQueryConfig{
		"Pod": {
			{
				ResourceKind:      "Pod",
				MetricName:        "pod_cpu_usage_cores",
				QueryTemplate:     "sum(rate(container_cpu_usage_seconds_total{namespace=\"%s\",pod=\"%s\",container!=\"-\"}[5m])) by (pod)",
				LabelKey:          "pod",
				Description:       "Pod CPU 使用核数",
				Unit:              "核",
				Category:          "performance",
				ThresholdWarning:  0.8,
				ThresholdCritical: 0.95,
				TypicalRange:      "0.1 - 2.0",
				ForUI:             true,
				ForLLM:            true,
			},
			{
				ResourceKind:  "Pod",
				MetricName:    "pod_memory_working_set_bytes",
				QueryTemplate: "sum(container_memory_working_set_bytes{namespace=\"%s\",pod=\"%s\",container!=\"-\"}) by (pod)",
				LabelKey:      "pod",
				Description:   "Pod 工作集内存（不含缓存）",
				Unit:          "字节",
				Category:      "performance",
				TypicalRange:  "100MB - 4GB",
				ForUI:         true,
				ForLLM:        true,
			},
			{
				ResourceKind:      "Pod",
				MetricName:        "pod_memory_usage_percent",
				QueryTemplate:     "sum(container_memory_working_set_bytes{namespace=\"%s\",pod=\"%s\",container!=\"-\"}) by (pod) / sum(container_memory_usage_bytes{namespace=\"%s\",pod=\"%s\",container!=\"-\"}) by (pod) * 100",
				LabelKey:          "pod",
				Description:       "Pod 内存使用率",
				Unit:              "%",
				Category:          "performance",
				ThresholdWarning:  80,
				ThresholdCritical: 95,
				TypicalRange:      "40% - 80%",
				ForUI:             true,
				ForLLM:            true,
			},
			{
				ResourceKind:  "Pod",
				MetricName:    "pod_network_receive_bytes_per_sec",
				QueryTemplate: "sum(rate(container_network_receive_bytes_total{namespace=\"%s\",pod=\"%s\"}[5m])) by (pod)",
				LabelKey:      "pod",
				Description:   "Pod 网络接收速率",
				Unit:          "B/s",
				Category:      "network",
				ForUI:         true,
				ForLLM:        true,
			},
			{
				ResourceKind:  "Pod",
				MetricName:    "pod_network_transmit_bytes_per_sec",
				QueryTemplate: "sum(rate(container_network_transmit_bytes_total{namespace=\"%s\",pod=\"%s\"}[5m])) by (pod)",
				LabelKey:      "pod",
				Description:   "Pod 网络发送速率",
				Unit:          "B/s",
				Category:      "network",
				ForUI:         true,
				ForLLM:        true,
			},
			{
				ResourceKind:      "Pod",
				MetricName:        "kube_pod_container_status_restarts_total",
				QueryTemplate:     "sum(kube_pod_container_status_restarts_total{namespace=\"%s\",pod=\"%s\"}) by (pod)",
				LabelKey:          "pod",
				Description:       "Pod 容器重启次数",
				Unit:              "次",
				Category:          "availability",
				ThresholdWarning:  3,
				ThresholdCritical: 10,
				TypicalRange:      "0 - 2",
				ForUI:             true,
				ForLLM:            true,
			},
		},
		"Node": {
			{
				ResourceKind:      "Node",
				MetricName:        "node_cpu_usage_percent",
				QueryTemplate:     "100 - (avg(rate(node_cpu_seconds_total{instance=\"%s\",mode=\"idle\"}[5m])) * 100)",
				LabelKey:          "instance",
				Description:       "Node CPU 使用率",
				Unit:              "%",
				Category:          "performance",
				ThresholdWarning:  75,
				ThresholdCritical: 90,
				TypicalRange:      "20% - 70%",
				ForUI:             true,
				ForLLM:            true,
			},
			{
				ResourceKind:      "Node",
				MetricName:        "node_memory_usage_percent",
				QueryTemplate:     "(1 - node_memory_MemAvailable_bytes{instance=\"%s\"} / node_memory_MemTotal_bytes{instance=\"%s\"}) * 100",
				LabelKey:          "instance",
				Description:       "Node 内存使用率",
				Unit:              "%",
				Category:          "performance",
				ThresholdWarning:  80,
				ThresholdCritical: 95,
				TypicalRange:      "50% - 85%",
				ForUI:             true,
				ForLLM:            true,
			},
			{
				ResourceKind:      "Node",
				MetricName:        "node_filesystem_usage_percent",
				QueryTemplate:     "(1 - node_filesystem_avail_bytes{instance=\"%s\",mountpoint=\"/\"} / node_filesystem_size_bytes{instance=\"%s\",mountpoint=\"/\"}) * 100",
				LabelKey:          "instance",
				Description:       "Node 根磁盘使用率",
				Unit:              "%",
				Category:          "storage",
				ThresholdWarning:  80,
				ThresholdCritical: 90,
				TypicalRange:      "30% - 70%",
				ForUI:             true,
				ForLLM:            true,
			},
			{
				ResourceKind:  "Node",
				MetricName:    "node_network_receive_bytes_per_sec",
				QueryTemplate: "sum(rate(node_network_receive_bytes_total{instance=\"%s\"}[5m])) by (instance)",
				LabelKey:      "instance",
				Description:   "Node 网络接收速率",
				Unit:          "B/s",
				Category:      "network",
				ForUI:         true,
				ForLLM:        true,
			},
			{
				ResourceKind:  "Node",
				MetricName:    "node_network_transmit_bytes_per_sec",
				QueryTemplate: "sum(rate(node_network_transmit_bytes_total{instance=\"%s\"}[5m])) by (instance)",
				LabelKey:      "instance",
				Description:   "Node 网络发送速率",
				Unit:          "B/s",
				Category:      "network",
				ForUI:         true,
				ForLLM:        true,
			},
		},
		"Deployment": {
			{
				ResourceKind:  "Deployment",
				MetricName:    "kube_deployment_spec_replicas",
				QueryTemplate: "kube_deployment_spec_replicas{namespace=\"%s\",deployment=\"%s\"}",
				LabelKey:      "deployment",
				Description:   "Deployment 期望副本数",
				Unit:          "个",
				Category:      "availability",
				ForUI:         true,
				ForLLM:        true,
			},
			{
				ResourceKind:  "Deployment",
				MetricName:    "kube_deployment_status_replicas_available",
				QueryTemplate: "kube_deployment_status_replicas_available{namespace=\"%s\",deployment=\"%s\"}",
				LabelKey:      "deployment",
				Description:   "Deployment 可用副本数",
				Unit:          "个",
				Category:      "availability",
				ForUI:         true,
				ForLLM:        true,
			},
			{
				ResourceKind:      "Deployment",
				MetricName:        "kube_deployment_status_replicas_unavailable",
				QueryTemplate:     "kube_deployment_status_replicas_unavailable{namespace=\"%s\",deployment=\"%s\"}",
				LabelKey:          "deployment",
				Description:       "Deployment 不可用副本数",
				Unit:              "个",
				Category:          "availability",
				ThresholdWarning:  1,
				ThresholdCritical: 3,
				TypicalRange:      "0",
				ForUI:             true,
				ForLLM:            true,
			},
		},
		"StatefulSet": {
			{
				ResourceKind:  "StatefulSet",
				MetricName:    "kube_statefulset_status_replicas_ready",
				QueryTemplate: "kube_statefulset_status_replicas_ready{namespace=\"%s\",statefulset=\"%s\"}",
				LabelKey:      "statefulset",
				Description:   "StatefulSet 就绪副本数",
				Unit:          "个",
				Category:      "availability",
				ForUI:         true,
				ForLLM:        true,
			},
			{
				ResourceKind:     "StatefulSet",
				MetricName:       "kube_statefulset_status_replicas_current",
				QueryTemplate:    "kube_statefulset_status_replicas_current{namespace=\"%s\",statefulset=\"%s\"}",
				LabelKey:         "statefulset",
				Description:      "StatefulSet 当前副本数",
				Unit:             "个",
				Category:         "availability",
				ThresholdWarning: 0,
				ForUI:            true,
				ForLLM:           true,
			},
		},
		"DaemonSet": {
			{
				ResourceKind:  "DaemonSet",
				MetricName:    "kube_daemonset_status_number_ready",
				QueryTemplate: "kube_daemonset_status_number_ready{namespace=\"%s\",daemonset=\"%s\"}",
				LabelKey:      "daemonset",
				Description:   "DaemonSet 就绪节点数",
				Unit:          "个",
				Category:      "availability",
				ForUI:         true,
				ForLLM:        true,
			},
			{
				ResourceKind:     "DaemonSet",
				MetricName:       "kube_daemonset_status_number_available",
				QueryTemplate:    "kube_daemonset_status_number_available{namespace=\"%s\",daemonset=\"%s\"}",
				LabelKey:         "daemonset",
				Description:      "DaemonSet 可用节点数",
				Unit:             "个",
				Category:         "availability",
				ThresholdWarning: 0,
				ForUI:            true,
				ForLLM:           true,
			},
		},
		"Service": {
			{
				ResourceKind:  "Service",
				MetricName:    "kube_endpoint_address_available",
				QueryTemplate: "kube_endpoint_address_available{namespace=\"%s\",endpoint=\"%s\"}",
				LabelKey:      "endpoint",
				Description:   "Service 可用端点地址数",
				Unit:          "个",
				Category:      "availability",
				ForUI:         true,
				ForLLM:        true,
			},
			{
				ResourceKind:     "Service",
				MetricName:       "kube_endpoint_address_not_ready",
				QueryTemplate:    "kube_endpoint_address_not_ready{namespace=\"%s\",endpoint=\"%s\"}",
				LabelKey:         "endpoint",
				Description:      "Service 未就绪端点地址数",
				Unit:             "个",
				Category:         "availability",
				ThresholdWarning: 1,
				ForUI:            true,
				ForLLM:           true,
			},
		},
	}
}

func (s *QueryService) QueryMetric(ctx context.Context, resourceKind, namespace, resourceName string) ([]QueryResult, error) {
	// If baseURL is not configured, return empty results without error
	if s.baseURL == "" {
		return nil, nil
	}
	queries, ok := s.mapping[resourceKind]
	if !ok || len(queries) == 0 {
		return nil, fmt.Errorf("no metric mapping for resource kind: %s", resourceKind)
	}

	var results []QueryResult
	for _, q := range queries {
		query := fmt.Sprintf(q.QueryTemplate, namespace, resourceName)
		result, err := s.executeQuery(ctx, query, q.MetricName, q.ThresholdWarning, q.ThresholdCritical)
		if err != nil {
			s.logger.Debug("Failed to query metric",
				zap.String("metric", q.MetricName),
				zap.Error(err))
			continue
		}
		if result != nil {
			results = append(results, *result)
		}
	}

	return results, nil
}

// QueryInstant 执行 Prometheus instant query，返回单个数值结果
func (s *QueryService) QueryInstant(ctx context.Context, expr string) (float64, error) {
	if s.baseURL == "" {
		return 0, nil
	}
	u := fmt.Sprintf("%s/api/v1/query?query=%s", s.baseURL, url.QueryEscape(expr))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var apiResp struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Value []interface{} `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return 0, err
	}
	if apiResp.Status != "success" || len(apiResp.Data.Result) == 0 {
		return 0, nil
	}
	val := apiResp.Data.Result[0].Value
	if len(val) < 2 {
		return 0, nil
	}
	if str, ok := val[1].(string); ok {
		var f float64
		if _, err := fmt.Sscanf(str, "%f", &f); err == nil {
			return f, nil
		}
	}
	return 0, nil
}

// QueryRange 执行 Prometheus range query，返回时间序列数据
func (s *QueryService) QueryRange(ctx context.Context, expr string, start, end time.Time, step time.Duration) ([]QueryRangeResult, error) {
	// Try cache first
	if s.cache != nil {
		key := fmt.Sprintf("%s|%d|%d|%d", expr, start.Unix(), end.Unix(), int(step.Seconds()))
		if ifVal, ok := s.cache.Get(key); ok {
			return ifVal, nil
		}
	}
	// 如果未配置 Prometheus 地址，返回空结果且不阻塞主流程
	if !s.IsEnabled() {
		return []QueryRangeResult{}, nil
	}

	// 构建查询 URL，Prometheus 的 start/end/step 使用秒级时间
	startSec := float64(start.Unix())
	endSec := float64(end.Unix())
	stepSec := step.Seconds()

	// Encode expr 传入 query 参数，使用 QueryEscape 避免空格等问题
	url := s.baseURL + "/query_range?query=" + strings.ReplaceAll(expr, " ", "+")
	// 使用简单拼接，Prometheus 要求 start/end/step 是秒级时间
	url += "&start=" + strconv.FormatFloat(startSec, 'f', 0, 64)
	url += "&end=" + strconv.FormatFloat(endSec, 'f', 0, 64)
	url += "&step=" + strconv.FormatFloat(stepSec, 'f', -1, 64)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute range query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus range query returned status: %d", resp.StatusCode)
	}

	var rangeResp PrometheusRangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&rangeResp); err != nil {
		return nil, fmt.Errorf("failed to decode range response: %w", err)
	}

	var results []QueryRangeResult
	if rangeResp.Status != "success" || len(rangeResp.Data.Result) == 0 {
		return results, nil
	}
	for _, r := range rangeResp.Data.Result {
		// 解析时间序列点
		var values []TimeSeriesPoint
		for _, pair := range r.Values {
			if len(pair) < 2 {
				continue
			}
			// 期望第一个为时间戳（float64），第二个为字符串数值
			ts, ok := pair[0].(float64)
			if !ok {
				continue
			}
			valStr, ok := pair[1].(string)
			if !ok {
				// 允许在范围查询中解析为 float64
				switch v := pair[1].(type) {
				case float64:
					values = append(values, TimeSeriesPoint{Timestamp: time.Unix(int64(ts), 0), Value: v})
					continue
				default:
					continue
				}
			}
			var v float64
			if val, err := strconv.ParseFloat(valStr, 64); err == nil {
				v = val
			} else {
				continue
			}
			values = append(values, TimeSeriesPoint{Timestamp: time.Unix(int64(ts), 0), Value: v})
		}
		// Metric name，若存在 __name__ 字段，则作为 MetricName
		metricName := r.Metric["__name__"]
		results = append(results, QueryRangeResult{MetricName: metricName, Values: values, Labels: r.Metric})
	}
	// Cache the computed range results if caching is enabled
	if s.cache != nil {
		key := fmt.Sprintf("%s|%d|%d|%d", expr, start.Unix(), end.Unix(), int(step.Seconds()))
		s.cache.Set(key, results)
	}
	return results, nil
}

// GetNodeMetrics 获取指定节点的 CPU/内存/磁盘/网络指标
func (s *QueryService) GetNodeMetrics(ctx context.Context, nodeName string) ([]QueryResult, error) {
	return s.QueryMetric(ctx, "Node", "", nodeName)
}

// GetPodMetrics 获取指定 Pod 的容器资源使用率、重启次数
func (s *QueryService) GetPodMetrics(ctx context.Context, namespace, podName string) ([]QueryResult, error) {
	return s.QueryMetric(ctx, "Pod", namespace, podName)
}

// GetDeploymentMetrics 获取指定 Deployment 的副本数/可用数
func (s *QueryService) GetDeploymentMetrics(ctx context.Context, namespace, name string) ([]QueryResult, error) {
	return s.QueryMetric(ctx, "Deployment", namespace, name)
}

// GetStatefulSetMetrics 获取指定 StatefulSet 的副本数/可用数
func (s *QueryService) GetStatefulSetMetrics(ctx context.Context, namespace, name string) ([]QueryResult, error) {
	return s.QueryMetric(ctx, "StatefulSet", namespace, name)
}

// GetDaemonSetMetrics 获取指定 DaemonSet 的节点调度数
func (s *QueryService) GetDaemonSetMetrics(ctx context.Context, namespace, name string) ([]QueryResult, error) {
	return s.QueryMetric(ctx, "DaemonSet", namespace, name)
}

// GetServiceMetrics 获取指定 Service 的端点状态
func (s *QueryService) GetServiceMetrics(ctx context.Context, namespace, name string) ([]QueryResult, error) {
	return s.QueryMetric(ctx, "Service", namespace, name)
}

// GetDeploymentCPUUtilization 获取指定 Deployment 的 CPU 利用率（百分比）
func (s *QueryService) GetDeploymentCPUUtilization(ctx context.Context, namespace, deployment string) (float64, error) {
	results, err := s.QueryMetric(ctx, "Deployment", namespace, deployment)
	if err != nil {
		return 0, err
	}
	for _, r := range results {
		if r.MetricName == "k8s_deployment_cpu_utilization" {
			return r.Value, nil
		}
	}
	return 0, nil
}

// GetDeploymentMemoryUtilization 获取指定 Deployment 的 Memory 利用率（百分比）
func (s *QueryService) GetDeploymentMemoryUtilization(ctx context.Context, namespace, deployment string) (float64, error) {
	results, err := s.QueryMetric(ctx, "Deployment", namespace, deployment)
	if err != nil {
		return 0, err
	}
	for _, r := range results {
		if r.MetricName == "k8s_deployment_memory_utilization" {
			return r.Value, nil
		}
	}
	return 0, nil
}

func (s *QueryService) executeQuery(ctx context.Context, query, metricName string, thresholdWarning, thresholdCritical float64) (*QueryResult, error) {
	url := s.baseURL + "/query?query=" + strings.ReplaceAll(query, " ", "+")

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned status: %d", resp.StatusCode)
	}

	var promResp PrometheusResponse
	if err := json.NewDecoder(resp.Body).Decode(&promResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if promResp.Status != "success" || len(promResp.Data.Result) == 0 {
		return nil, nil
	}

	result := promResp.Data.Result[0]
	if len(result.Value) < 2 {
		return nil, nil
	}

	var value float64
	switch v := result.Value[1].(type) {
	case float64:
		value = v
	case string:
		_, _ = fmt.Sscanf(v, "%f", &value)
	}

	timestamp := time.Now()
	if len(result.Value) >= 1 {
		if ts, ok := result.Value[0].(float64); ok {
			timestamp = time.Unix(int64(ts), 0)
		}
	}

	// 判断指标状态
	status := "normal"
	if thresholdCritical > 0 && value >= thresholdCritical {
		status = "critical"
	} else if thresholdWarning > 0 && value >= thresholdWarning {
		status = "warning"
	}

	return &QueryResult{
		MetricName: metricName,
		Value:      value,
		Timestamp:  timestamp,
		Labels:     result.Metric,
		Status:     status,
	}, nil
}

func (s *QueryService) IsEnabled() bool {
	return s.baseURL != ""
}

type AlertPrometheusQuerier interface {
	QueryMetricForAlert(ctx context.Context, resourceKind, namespace, resourceName string) ([]AlertPrometheusResult, error)
}

type AlertPrometheusResult struct {
	MetricName string
	Value      float64
	Timestamp  time.Time
	Labels     map[string]string
}

func (s *QueryService) AsAlertPrometheusQuerier() AlertPrometheusQuerier {
	return s
}

func (s *QueryService) QueryMetricForAlert(ctx context.Context, resourceKind, namespace, resourceName string) ([]AlertPrometheusResult, error) {
	results, err := s.QueryMetric(ctx, resourceKind, namespace, resourceName)
	if err != nil {
		return nil, err
	}

	var alertResults []AlertPrometheusResult
	for _, r := range results {
		alertResults = append(alertResults, AlertPrometheusResult{
			MetricName: r.MetricName,
			Value:      r.Value,
			Timestamp:  r.Timestamp,
			Labels:     r.Labels,
		})
	}
	return alertResults, nil
}

// AsMetricsQuerier 返回实现 interfaces.MetricsQuerier 的适配器
func (s *QueryService) AsMetricsQuerier() interfaces.MetricsQuerier {
	return &metricsQuerierAdapter{svc: s}
}

// GetHPAStatus 查询指定 namespace 下指定 HPA 的状态信息
func (s *QueryService) GetHPAStatus(ctx context.Context, namespace, hpaName string) (*HPAStatus, error) {
	// helper function to query a single Prometheus expression and parse float64 value
	queryOne := func(expr string) (float64, error) {
		url := s.baseURL + "/query?query=" + url.QueryEscape(expr)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return 0, err
		}
		resp, err := s.client.Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return 0, fmt.Errorf("prometheus returned status: %d", resp.StatusCode)
		}
		var prom PrometheusResponse
		if err := json.NewDecoder(resp.Body).Decode(&prom); err != nil {
			return 0, err
		}
		if prom.Status != "success" || len(prom.Data.Result) == 0 {
			return 0, nil
		}
		val := prom.Data.Result[0].Value
		if len(val) < 2 {
			return 0, nil
		}
		switch v := val[1].(type) {
		case float64:
			return v, nil
		case string:
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return 0, nil
			}
			return f, nil
		default:
			return 0, nil
		}
	}

	// 关键指标：当前/期望副本数与最小/最大副本数
	current, _ := queryOne(fmt.Sprintf("kube_hpa_status_current_replicas{namespace=\"%s\",hpa=\"%s\"}", namespace, hpaName))
	desired, _ := queryOne(fmt.Sprintf("kube_hpa_status_desired_replicas{namespace=\"%s\",hpa=\"%s\"}", namespace, hpaName))
	MinR, _ := queryOne(fmt.Sprintf("kube_hpa_spec_min_replicas{namespace=\"%s\",hpa=\"%s\"}", namespace, hpaName))
	MaxR, _ := queryOne(fmt.Sprintf("kube_hpa_spec_max_replicas{namespace=\"%s\",hpa=\"%s\"}", namespace, hpaName))
	cpuUtil, _ := queryOne(fmt.Sprintf("kube_hpa_status_current_cpu_utilization_percentage{namespace=\"%s\",hpa=\"%s\"}", namespace, hpaName))

	status := &HPAStatus{
		CurrentReplicas: int32(current),
		DesiredReplicas: int32(desired),
		MinReplicas:     int32(MinR),
		MaxReplicas:     int32(MaxR),
		CPUUtilization:  cpuUtil,
		Available:       true,
	}
	// 如果没有可用数据，标记不可用
	if current == 0 && desired == 0 && MinR == 0 && MaxR == 0 {
		status.Available = false
	}
	return status, nil
}

type metricsQuerierAdapter struct {
	svc *QueryService
}

func (a *metricsQuerierAdapter) GetNodeMetrics(ctx context.Context, nodeName string) ([]interfaces.MetricDataPoint, error) {
	results, err := a.svc.GetNodeMetrics(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	return convertToMetricDataPoints(results), nil
}

func (a *metricsQuerierAdapter) GetPodMetrics(ctx context.Context, namespace, podName string) ([]interfaces.MetricDataPoint, error) {
	results, err := a.svc.GetPodMetrics(ctx, namespace, podName)
	if err != nil {
		return nil, err
	}
	return convertToMetricDataPoints(results), nil
}

func (a *metricsQuerierAdapter) GetDeploymentMetrics(ctx context.Context, namespace, name string) ([]interfaces.MetricDataPoint, error) {
	results, err := a.svc.GetDeploymentMetrics(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	return convertToMetricDataPoints(results), nil
}

func (a *metricsQuerierAdapter) GetStatefulSetMetrics(ctx context.Context, namespace, name string) ([]interfaces.MetricDataPoint, error) {
	results, err := a.svc.GetStatefulSetMetrics(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	return convertToMetricDataPoints(results), nil
}

func (a *metricsQuerierAdapter) GetDaemonSetMetrics(ctx context.Context, namespace, name string) ([]interfaces.MetricDataPoint, error) {
	results, err := a.svc.GetDaemonSetMetrics(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	return convertToMetricDataPoints(results), nil
}

func (a *metricsQuerierAdapter) GetServiceMetrics(ctx context.Context, namespace, name string) ([]interfaces.MetricDataPoint, error) {
	results, err := a.svc.GetServiceMetrics(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	return convertToMetricDataPoints(results), nil
}

func (a *metricsQuerierAdapter) QueryMetricTimeseries(ctx context.Context, expr string, start, end int64, step int64) ([]interfaces.TimeSeriesDataPoint, error) {
	startTime := time.Unix(start, 0)
	endTime := time.Unix(end, 0)
	stepDuration := time.Duration(step) * time.Second

	results, err := a.svc.QueryRange(ctx, expr, startTime, endTime, stepDuration)
	if err != nil {
		return nil, err
	}

	var timeSeries []interfaces.TimeSeriesDataPoint
	for _, r := range results {
		var values []interfaces.TimeSeriesValue
		for _, v := range r.Values {
			values = append(values, interfaces.TimeSeriesValue{
				Timestamp: v.Timestamp.Unix(),
				Value:     v.Value,
			})
		}
		timeSeries = append(timeSeries, interfaces.TimeSeriesDataPoint{
			MetricName: r.MetricName,
			Values:     values,
			Labels:     r.Labels,
		})
	}

	return timeSeries, nil
}

func (a *metricsQuerierAdapter) GetMetricCatalog(ctx context.Context, forUI, forLLM bool) ([]interfaces.MetricCatalogEntry, error) {
	var catalog []interfaces.MetricCatalogEntry

	for resourceType, queries := range a.svc.mapping {
		for _, q := range queries {
			if forUI && !q.ForUI {
				continue
			}
			if forLLM && !q.ForLLM {
				continue
			}
			catalog = append(catalog, interfaces.MetricCatalogEntry{
				Name:         q.MetricName,
				ResourceType: resourceType,
				Description:  q.Description,
				Unit:         q.Unit,
				Category:     q.Category,
				Thresholds: interfaces.MetricThresholds{
					Warning:  q.ThresholdWarning,
					Critical: q.ThresholdCritical,
				},
				TypicalRange: q.TypicalRange,
			})
		}
	}

	return catalog, nil
}

func (a *metricsQuerierAdapter) CheckHealth(ctx context.Context) error {
	if !a.svc.IsEnabled() {
		return fmt.Errorf("prometheus not configured")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", a.svc.baseURL+"/query?query=up", nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := a.svc.client.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("prometheus unhealthy: status %d", resp.StatusCode)
	}

	return nil
}

func convertToMetricDataPoints(results []QueryResult) []interfaces.MetricDataPoint {
	dps := make([]interfaces.MetricDataPoint, 0, len(results))
	for _, r := range results {
		dps = append(dps, interfaces.MetricDataPoint{
			MetricName: r.MetricName,
			Value:      r.Value,
			Labels:     r.Labels,
			Status:     r.Status,
		})
	}
	return dps
}
