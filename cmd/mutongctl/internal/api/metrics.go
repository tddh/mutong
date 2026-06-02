package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
)

// MetricsAPI provides typed access to the Mutong Metrics API.
type MetricsAPI struct {
	client *client.Client
}

// NewMetricsAPI creates a new MetricsAPI.
func NewMetricsAPI(c *client.Client) *MetricsAPI {
	return &MetricsAPI{client: c}
}

// MetricDataPoint represents a single metric value for a resource.
type MetricDataPoint struct {
	MetricName string            `json:"metricName"`
	Value      float64           `json:"value"`
	Labels     map[string]string `json:"labels"`
	Status     string            `json:"status,omitempty"`
}

// TimeseriesDataPoint represents a timeseries metric result.
type TimeseriesDataPoint struct {
	MetricName string            `json:"metricName"`
	Values     []TimeseriesValue `json:"values"`
	Labels     map[string]string `json:"labels"`
}

// TimeseriesValue represents a single data point in a timeseries.
type TimeseriesValue struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}

// GetResourceMetrics retrieves metrics for a specific resource by kind, name, and namespace.
// kind must be one of: Pod, Node, Deployment, StatefulSet, DaemonSet, Service.
func (a *MetricsAPI) GetResourceMetrics(kind, name, namespace string) ([]MetricDataPoint, error) {
	path := "/api/v1/monitoring/metrics/" + kind
	var params []string
	if name != "" {
		params = append(params, "name="+name)
	}
	if namespace != "" {
		params = append(params, "namespace="+namespace)
	}
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}

	data, err := a.client.Do(context.Background(), "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("getting resource metrics: %w", err)
	}
	var results []MetricDataPoint
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// GetTimeseries queries metric timeseries data with a PromQL expression.
func (a *MetricsAPI) GetTimeseries(expr, start, end, step string) ([]TimeseriesDataPoint, error) {
	path := "/api/v1/monitoring/timeseries?expr=" + expr
	if start != "" {
		path += "&start=" + start
	}
	if end != "" {
		path += "&end=" + end
	}
	if step != "" {
		path += "&step=" + step
	}

	data, err := a.client.Do(context.Background(), "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("querying timeseries: %w", err)
	}
	var results []TimeseriesDataPoint
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}
	return results, nil
}
