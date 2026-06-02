package logsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gitee.com/tddh/mutong/interfaces"

	"github.com/elastic/go-elasticsearch/v8"
	esapi "github.com/elastic/go-elasticsearch/v8/esapi"
)

type ESQueryService struct {
	logger         interfaces.Logger
	client         *elasticsearch.Client
	indexPattern   string
	serviceToIndex map[string]string
	timeout        time.Duration
}

func NewESQueryService(logger interfaces.Logger, addresses []string, indexPattern string, serviceToIndex map[string]string, username, password string, timeoutSec int) (*ESQueryService, error) {
	if len(addresses) == 0 {
		return &ESQueryService{logger: logger}, nil
	}

	timeout := time.Duration(timeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	esCfg := elasticsearch.Config{
		Addresses: addresses,
		Username:  username,
		Password:  password,
	}

	client, err := elasticsearch.NewClient(esCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create ES client: %w", err)
	}

	if indexPattern == "" {
		indexPattern = "k8s-logs-*"
	}
	if serviceToIndex == nil {
		serviceToIndex = make(map[string]string)
	}

	return &ESQueryService{
		logger:         logger,
		client:         client,
		indexPattern:   indexPattern,
		serviceToIndex: serviceToIndex,
		timeout:        timeout,
	}, nil
}

func (s *ESQueryService) IsEnabled() bool {
	return s.client != nil
}

func (s *ESQueryService) GetPodLogs(ctx context.Context, namespace, podName, container string, sinceMinutes int, tail int) ([]interfaces.LogEntry, error) {
	if !s.IsEnabled() {
		return nil, nil
	}

	if tail <= 0 {
		tail = 100
	}
	if sinceMinutes <= 0 {
		sinceMinutes = 15
	}

	filters := []interface{}{
		map[string]interface{}{"term": map[string]interface{}{"kubernetes.pod_name": podName}},
		map[string]interface{}{"term": map[string]interface{}{"kubernetes.namespace_name": namespace}},
	}

	if container != "" {
		filters = append(filters, map[string]interface{}{"term": map[string]interface{}{"kubernetes.container_name": container}})
	}

	sinceTime := time.Now().Add(-time.Duration(sinceMinutes) * time.Minute).Format(time.RFC3339)
	filters = append(filters, map[string]interface{}{"range": map[string]interface{}{"@timestamp": map[string]interface{}{"gte": sinceTime}}})

	query := map[string]interface{}{
		"query": map[string]interface{}{"bool": map[string]interface{}{"filter": filters}},
		"sort":  []interface{}{map[string]interface{}{"@timestamp": map[string]interface{}{"order": "desc"}}},
		"size":  tail,
	}

	return s.searchLogs(ctx, query, "")
}

func (s *ESQueryService) SearchLogs(ctx context.Context, query, namespace string, sinceMinutes int, maxResults int) ([]interfaces.LogEntry, error) {
	if !s.IsEnabled() {
		return nil, nil
	}

	if maxResults <= 0 {
		maxResults = 100
	}
	if sinceMinutes <= 0 {
		sinceMinutes = 15
	}

	filters := []interface{}{
		map[string]interface{}{"query_string": map[string]interface{}{"query": query}},
	}

	if namespace != "" {
		filters = append(filters, map[string]interface{}{"term": map[string]interface{}{"kubernetes.namespace_name": namespace}})
	}

	sinceTime := time.Now().Add(-time.Duration(sinceMinutes) * time.Minute).Format(time.RFC3339)
	filters = append(filters, map[string]interface{}{"range": map[string]interface{}{"@timestamp": map[string]interface{}{"gte": sinceTime}}})

	esQuery := map[string]interface{}{
		"query": map[string]interface{}{"bool": map[string]interface{}{"filter": filters}},
		"sort":  []interface{}{map[string]interface{}{"@timestamp": map[string]interface{}{"order": "desc"}}},
		"size":  maxResults,
	}

	return s.searchLogs(ctx, esQuery, "")
}

func (s *ESQueryService) GetErrorLogs(ctx context.Context, namespace string, sinceMinutes int) ([]interfaces.LogEntry, error) {
	if !s.IsEnabled() {
		return nil, nil
	}

	if sinceMinutes <= 0 {
		sinceMinutes = 15
	}

	filters := []interface{}{
		map[string]interface{}{
			"terms": map[string]interface{}{
				"level": []string{"ERROR", "FATAL", "PANIC", "error", "fatal", "panic"},
			},
		},
	}

	if namespace != "" {
		filters = append(filters, map[string]interface{}{"term": map[string]interface{}{"kubernetes.namespace_name": namespace}})
	}

	sinceTime := time.Now().Add(-time.Duration(sinceMinutes) * time.Minute).Format(time.RFC3339)
	filters = append(filters, map[string]interface{}{"range": map[string]interface{}{"@timestamp": map[string]interface{}{"gte": sinceTime}}})

	query := map[string]interface{}{
		"query": map[string]interface{}{"bool": map[string]interface{}{"filter": filters}},
		"sort":  []interface{}{map[string]interface{}{"@timestamp": map[string]interface{}{"order": "desc"}}},
		"size":  50,
	}

	return s.searchLogs(ctx, query, "")
}

func (s *ESQueryService) GetWarnLogs(ctx context.Context, namespace string, sinceMinutes int) ([]interfaces.LogEntry, error) {
	if !s.IsEnabled() {
		return nil, nil
	}

	if sinceMinutes <= 0 {
		sinceMinutes = 15
	}

	filters := []interface{}{
		map[string]interface{}{
			"terms": map[string]interface{}{
				"level": []string{"WARN", "WARNING", "warn", "warning"},
			},
		},
	}

	if namespace != "" {
		filters = append(filters, map[string]interface{}{"term": map[string]interface{}{"kubernetes.namespace_name": namespace}})
	}

	sinceTime := time.Now().Add(-time.Duration(sinceMinutes) * time.Minute).Format(time.RFC3339)
	filters = append(filters, map[string]interface{}{"range": map[string]interface{}{"@timestamp": map[string]interface{}{"gte": sinceTime}}})

	query := map[string]interface{}{
		"query": map[string]interface{}{"bool": map[string]interface{}{"filter": filters}},
		"sort":  []interface{}{map[string]interface{}{"@timestamp": map[string]interface{}{"order": "desc"}}},
		"size":  50,
	}

	return s.searchLogs(ctx, query, "")
}

func (s *ESQueryService) GetInfoLogs(ctx context.Context, namespace string, sinceMinutes int) ([]interfaces.LogEntry, error) {
	if !s.IsEnabled() {
		return nil, nil
	}

	if sinceMinutes <= 0 {
		sinceMinutes = 15
	}

	filters := []interface{}{
		map[string]interface{}{
			"terms": map[string]interface{}{
				"level": []string{"INFO", "info", "DEBUG", "debug", "TRACE", "trace"},
			},
		},
	}

	if namespace != "" {
		filters = append(filters, map[string]interface{}{"term": map[string]interface{}{"kubernetes.namespace_name": namespace}})
	}

	sinceTime := time.Now().Add(-time.Duration(sinceMinutes) * time.Minute).Format(time.RFC3339)
	filters = append(filters, map[string]interface{}{"range": map[string]interface{}{"@timestamp": map[string]interface{}{"gte": sinceTime}}})

	query := map[string]interface{}{
		"query": map[string]interface{}{"bool": map[string]interface{}{"filter": filters}},
		"sort":  []interface{}{map[string]interface{}{"@timestamp": map[string]interface{}{"order": "desc"}}},
		"size":  50,
	}

	return s.searchLogs(ctx, query, "")
}

type esSearchResponse struct {
	Hits struct {
		Hits []struct {
			Source map[string]interface{} `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

func (s *ESQueryService) searchLogs(ctx context.Context, query map[string]interface{}, serviceName string) ([]interfaces.LogEntry, error) {
	index := s.resolveIndex(serviceName)
	body, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	req := esapi.SearchRequest{
		Index: []string{index},
		Body:  bytes.NewReader(body),
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	resp, err := req.Do(ctx, s.client)
	if err != nil {
		return nil, fmt.Errorf("failed to search ES: %w", err)
	}
	defer resp.Body.Close()

	if resp.IsError() {
		return nil, fmt.Errorf("ES search error: %s", resp.Status())
	}

	var searchResp esSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to decode ES response: %w", err)
	}

	var entries []interfaces.LogEntry
	for _, hit := range searchResp.Hits.Hits {
		entry := parseLogEntry(hit.Source)
		entries = append(entries, entry)
	}

	return entries, nil
}

func parseLogEntry(source map[string]interface{}) interfaces.LogEntry {
	entry := interfaces.LogEntry{
		Labels: make(map[string]string),
	}

	if ts, ok := source["@timestamp"].(string); ok {
		entry.Timestamp = ts
	}
	if level, ok := source["level"].(string); ok {
		entry.Level = level
	}
	if msg, ok := source["message"].(string); ok {
		entry.Message = msg
	} else if msg, ok := source["log"].(string); ok {
		entry.Message = msg
	}

	if k8s, ok := source["kubernetes"].(map[string]interface{}); ok {
		if ns, ok := k8s["namespace_name"].(string); ok {
			entry.Namespace = ns
		}
		if pod, ok := k8s["pod_name"].(string); ok {
			entry.Pod = pod
		}
		if cont, ok := k8s["container_name"].(string); ok {
			entry.Container = cont
		}
	}

	if entry.Level == "" {
		msgUpper := strings.ToUpper(entry.Message)
		if strings.Contains(msgUpper, "ERROR") || strings.Contains(msgUpper, "FATAL") {
			entry.Level = "error"
		} else if strings.Contains(msgUpper, "WARN") {
			entry.Level = "warning"
		} else {
			entry.Level = "info"
		}
	}

	return entry
}

func (s *ESQueryService) AsLogQuerier() interfaces.LogQuerier {
	return s
}

func (s *ESQueryService) resolveIndex(serviceName string) string {
	if serviceName == "" || s.serviceToIndex == nil {
		return s.indexPattern
	}
	if idx, ok := s.serviceToIndex[serviceName]; ok && idx != "" {
		return idx
	}
	return s.indexPattern
}

func (s *ESQueryService) SearchLogsByBusiness(ctx context.Context, namespaces []string, keyword string, sinceMinutes int, maxResults int) ([]interfaces.LogEntry, error) {
	if !s.IsEnabled() {
		return nil, nil
	}
	if maxResults <= 0 {
		maxResults = 100
	}
	if sinceMinutes <= 0 {
		sinceMinutes = 15
	}
	filters := []interface{}{
		map[string]interface{}{"query_string": map[string]interface{}{"query": keyword}},
	}
	if len(namespaces) > 0 {
		filters = append(filters, map[string]interface{}{
			"terms": map[string]interface{}{"kubernetes.namespace_name": namespaces},
		})
	}
	sinceTime := time.Now().Add(-time.Duration(sinceMinutes) * time.Minute).Format(time.RFC3339)
	filters = append(filters, map[string]interface{}{
		"range": map[string]interface{}{"@timestamp": map[string]interface{}{"gte": sinceTime}},
	})
	query := map[string]interface{}{
		"query": map[string]interface{}{"bool": map[string]interface{}{"filter": filters}},
		"sort":  []interface{}{map[string]interface{}{"@timestamp": map[string]interface{}{"order": "desc"}}},
		"size":  maxResults,
	}
	return s.searchLogs(ctx, query, "")
}
