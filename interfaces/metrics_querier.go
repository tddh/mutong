package interfaces

import "context"

type MetricsQuerier interface {
	GetNodeMetrics(ctx context.Context, nodeName string) ([]MetricDataPoint, error)
	GetPodMetrics(ctx context.Context, namespace, podName string) ([]MetricDataPoint, error)
	GetDeploymentMetrics(ctx context.Context, namespace, name string) ([]MetricDataPoint, error)
	GetStatefulSetMetrics(ctx context.Context, namespace, name string) ([]MetricDataPoint, error)
	GetDaemonSetMetrics(ctx context.Context, namespace, name string) ([]MetricDataPoint, error)
	GetServiceMetrics(ctx context.Context, namespace, name string) ([]MetricDataPoint, error)
	QueryMetricTimeseries(ctx context.Context, expr string, start, end int64, step int64) ([]TimeSeriesDataPoint, error)
	GetMetricCatalog(ctx context.Context, forUI, forLLM bool) ([]MetricCatalogEntry, error)
	CheckHealth(ctx context.Context) error
}

type MetricDataPoint struct {
	MetricName string            `json:"metricName"`
	Value      float64           `json:"value"`
	Labels     map[string]string `json:"labels"`
	Status     string            `json:"status,omitempty"`
}

type TimeSeriesDataPoint struct {
	MetricName string            `json:"metricName"`
	Values     []TimeSeriesValue `json:"values"`
	Labels     map[string]string `json:"labels"`
}

type TimeSeriesValue struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}

type MetricCatalogEntry struct {
	Name         string           `json:"name"`
	ResourceType string           `json:"resourceType"`
	Description  string           `json:"description,omitempty"`
	Unit         string           `json:"unit,omitempty"`
	Category     string           `json:"category,omitempty"`
	Thresholds   MetricThresholds `json:"thresholds,omitempty"`
	TypicalRange string           `json:"typicalRange,omitempty"`
}

type MetricThresholds struct {
	Warning  float64 `json:"warning"`
	Critical float64 `json:"critical"`
}
