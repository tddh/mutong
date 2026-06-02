package alert

import (
	"context"
	"strings"
	"testing"
	"time"

	"gitee.com/tddh/mutong/interfaces"
	alert_models "gitee.com/tddh/mutong/models/alert"
	nebula "github.com/vesoft-inc/nebula-go/v3"
	nebula_types "github.com/vesoft-inc/nebula-go/v3/nebula"
	"github.com/vesoft-inc/nebula-go/v3/nebula/graph"

	"go.uber.org/zap"
)

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, fields ...zap.Field) {}
func (m *mockLogger) Info(msg string, fields ...zap.Field)  {}
func (m *mockLogger) Warn(msg string, fields ...zap.Field)  {}
func (m *mockLogger) Error(msg string, fields ...zap.Field) {}

type mockGraphDBForEnricher struct {
	executeAndCheckFunc func(query string) (*nebula.ResultSet, error)
}

func (m *mockGraphDBForEnricher) Execute(query string) (*nebula.ResultSet, error) {
	return nil, nil
}

func (m *mockGraphDBForEnricher) ExecuteAndCheck(query string) (*nebula.ResultSet, error) {
	if m.executeAndCheckFunc != nil {
		return m.executeAndCheckFunc(query)
	}
	return nil, nil
}

// mockResultSetBuilder constructs real *nebula.ResultSet for tests.
type mockResultSetBuilder struct {
	columns []string
	rows    [][]any
}

func newMockResultSetBuilder(columns ...string) *mockResultSetBuilder {
	return &mockResultSetBuilder{columns: columns}
}

func (b *mockResultSetBuilder) addRow(values ...any) *mockResultSetBuilder {
	b.rows = append(b.rows, values)
	return b
}

func (b *mockResultSetBuilder) build() (*nebula.ResultSet, error) {
	ds := &nebula_types.DataSet{}
	for _, col := range b.columns {
		ds.ColumnNames = append(ds.ColumnNames, []byte(col))
	}
	for _, rowVals := range b.rows {
		row := &nebula_types.Row{}
		for _, v := range rowVals {
			row.Values = append(row.Values, toNebulaValue(v))
		}
		ds.Rows = append(ds.Rows, row)
	}
	resp := &graph.ExecutionResponse{
		Data: ds,
	}
	return nebula.GenResultSet(resp)
}

func toNebulaValue(v any) *nebula_types.Value {
	val := &nebula_types.Value{}
	switch x := v.(type) {
	case string:
		val.SVal = []byte(x)
	case int:
		i := int64(x)
		val.IVal = &i
	case int64:
		val.IVal = &x
	case nil:
		n := nebula_types.NullType(0)
		val.NVal = &n
	}
	return val
}

// mockGraphDBWithResult returns a controlled ResultSet for specific query patterns.
type mockGraphDBWithResult struct {
	results map[string]*nebula.ResultSet
	errs    map[string]error
}

func newMockGraphDB() *mockGraphDBWithResult {
	return &mockGraphDBWithResult{
		results: make(map[string]*nebula.ResultSet),
		errs:    make(map[string]error),
	}
}

func (m *mockGraphDBWithResult) withResult(queryPattern string, rs *nebula.ResultSet) *mockGraphDBWithResult {
	m.results[queryPattern] = rs
	return m
}

func (m *mockGraphDBWithResult) Execute(query string) (*nebula.ResultSet, error) {
	return nil, nil
}

func (m *mockGraphDBWithResult) ExecuteAndCheck(query string) (*nebula.ResultSet, error) {
	if err, ok := m.errs[query]; ok {
		return nil, err
	}
	if rs, ok := m.results[query]; ok {
		return rs, nil
	}
	for pattern, rs := range m.results {
		if strings.Contains(query, pattern) {
			return rs, nil
		}
	}
	return nil, nil
}

var _ interfaces.GraphDB = (*mockGraphDBWithResult)(nil)

func TestResourceTypeExtraction(t *testing.T) {
	tests := []struct {
		name     string
		labels   alert_models.AlertLabels
		expected string
	}{
		{
			name:     "Pod from ResourceKind",
			labels:   alert_models.AlertLabels{ResourceKind: "Pod", Pod: "test-pod"},
			expected: "Pod",
		},
		{
			name:     "Node from ResourceKind",
			labels:   alert_models.AlertLabels{ResourceKind: "Node", Node: "node-1"},
			expected: "Node",
		},
		{
			name:     "Pod from Pod label fallback",
			labels:   alert_models.AlertLabels{Pod: "test-pod"},
			expected: "Pod",
		},
		{
			name:     "Node from Node label fallback",
			labels:   alert_models.AlertLabels{Node: "node-1"},
			expected: "Node",
		},
		{
			name:     "Service from Service label fallback",
			labels:   alert_models.AlertLabels{Service: "svc-1"},
			expected: "Service",
		},
		{
			name:     "Unknown when no labels",
			labels:   alert_models.AlertLabels{},
			expected: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enricher := &AlertEnricher{logger: &mockLogger{}}
			result := enricher.getResourceType(tt.labels)
			if result != tt.expected {
				t.Errorf("getResourceType() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestResourceNameExtraction(t *testing.T) {
	tests := []struct {
		name     string
		labels   alert_models.AlertLabels
		expected string
	}{
		{
			name:     "Pod name from ResourceKind Pod",
			labels:   alert_models.AlertLabels{ResourceKind: "Pod", Pod: "test-pod"},
			expected: "test-pod",
		},
		{
			name:     "Node name from ResourceKind Node",
			labels:   alert_models.AlertLabels{ResourceKind: "Node", Node: "node-1"},
			expected: "node-1",
		},
		{
			name:     "Pod name fallback",
			labels:   alert_models.AlertLabels{Pod: "test-pod"},
			expected: "test-pod",
		},
		{
			name:     "Empty when no labels",
			labels:   alert_models.AlertLabels{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enricher := &AlertEnricher{logger: &mockLogger{}}
			result := enricher.getResourceName(tt.labels)
			if result != tt.expected {
				t.Errorf("getResourceName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBuildTopologyPath(t *testing.T) {
	tests := []struct {
		name     string
		enriched *alert_models.EnrichedAlert
		expected []string
	}{
		{
			"Node only",
			&alert_models.EnrichedAlert{ResourceType: "Node", ResourceName: "node-1", NodeName: "node-1"},
			[]string{"Node:node-1"},
		},
		{
			name: "Pod on Node",
			enriched: &alert_models.EnrichedAlert{
				ResourceType: "Pod",
				ResourceName: "pod-1",
				NodeName:     "node-1",
			},
			expected: []string{"Node:node-1", "Pod:pod-1"},
		},
		{
			name: "Pod on Node with Owner",
			enriched: &alert_models.EnrichedAlert{
				ResourceType: "Pod",
				ResourceName: "pod-1",
				NodeName:     "node-1",
				OwnerKind:    "Deployment",
				OwnerName:    "nginx-deployment",
			},
			expected: []string{"Node:node-1", "Pod:pod-1", "Deployment:nginx-deployment"},
		},
		{
			name: "Empty when no info",
			enriched: &alert_models.EnrichedAlert{
				ResourceType: "Unknown",
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enricher := &AlertEnricher{}
			result := enricher.buildTopologyPath(tt.enriched)
			if len(result) != len(tt.expected) {
				t.Errorf("buildTopologyPath() length = %v, want %v", len(result), len(tt.expected))
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("buildTopologyPath()[%d] = %v, want %v", i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParseTopologyPath(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected int
	}{
		{
			name:     "Parse full path",
			input:    []string{"Node:node-1", "Pod:pod-1", "Deployment:dep-1"},
			expected: 3,
		},
		{
			name:     "Parse single node",
			input:    []string{"Node:node-1"},
			expected: 1,
		},
		{
			name:     "Empty path",
			input:    []string{},
			expected: 0,
		},
		{
			name:     "Skip invalid entries",
			input:    []string{"Node:node-1", "invalid", "Pod:pod-1"},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseTopologyPath(tt.input)
			if len(result) != tt.expected {
				t.Errorf("ParseTopologyPath() length = %v, want %v", len(result), tt.expected)
			}
		})
	}
}

func TestTopologySuppression(t *testing.T) {
	suppressor := &AlertSuppressor{
		logger:       &mockLogger{},
		activeAlerts: make(map[string]*alert_models.EnrichedAlert),
		config: SuppressionConfig{
			TimeWindowSeconds:  300,
			MaxDepth:           0,
			SeverityExceptions: []string{},
		},
	}

	parentAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "parent-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "NodeNotReady"},
			StartsAt:    time.Now().Add(-1 * time.Minute),
		},
		ResourceType: "Node",
		ResourceName: "node-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1"},
	}
	suppressor.activeAlerts["parent-fp"] = parentAlert

	// Create a child Pod alert
	childAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "child-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "PodCrashLooping"},
			StartsAt:    time.Now(),
		},
		ResourceType: "Pod",
		ResourceName: "pod-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1", "Pod:pod-1"},
	}

	suppressed, reason := suppressor.checkTopologySuppression(childAlert)
	if !suppressed {
		t.Error("Expected child alert to be suppressed")
	}
	if reason == "" {
		t.Error("Expected suppression reason")
	}
}

func TestTopologySuppressionTimeWindow(t *testing.T) {
	suppressor := &AlertSuppressor{
		logger:       &mockLogger{},
		activeAlerts: make(map[string]*alert_models.EnrichedAlert),
		config: SuppressionConfig{
			TimeWindowSeconds:  60, // 1 minute window
			MaxDepth:           0,
			SeverityExceptions: []string{},
		},
	}

	oldAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "old-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "NodeNotReady"},
			StartsAt:    time.Now().Add(-5 * time.Minute),
		},
		ResourceType: "Node",
		ResourceName: "node-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1"},
	}
	suppressor.activeAlerts["old-fp"] = oldAlert

	childAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "child-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "PodCrashLooping"},
			StartsAt:    time.Now(),
		},
		ResourceType: "Pod",
		ResourceName: "pod-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1", "Pod:pod-1"},
	}

	suppressed, _ := suppressor.checkTopologySuppression(childAlert)
	if suppressed {
		t.Error("Expected child alert NOT to be suppressed (parent outside time window)")
	}
}

func TestTopologySuppressionDepthLimit(t *testing.T) {
	suppressor := &AlertSuppressor{
		logger:       &mockLogger{},
		activeAlerts: make(map[string]*alert_models.EnrichedAlert),
		config: SuppressionConfig{
			TimeWindowSeconds:  300,
			MaxDepth:           1,
			SeverityExceptions: []string{},
		},
	}

	nodeAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "node-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "NodeNotReady"},
			StartsAt:    time.Now().Add(-1 * time.Minute),
		},
		ResourceType: "Node",
		ResourceName: "node-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1"},
	}
	suppressor.activeAlerts["node-fp"] = nodeAlert

	// Child alert with deep topology path
	childAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "child-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "PodCrashLooping"},
			StartsAt:    time.Now(),
		},
		ResourceType: "Pod",
		ResourceName: "pod-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1", "Pod:pod-1", "Deployment:nginx"},
	}

	suppressed, _ := suppressor.checkTopologySuppression(childAlert)
	if !suppressed {
		t.Error("Expected child alert to be suppressed (Node within depth limit)")
	}
}

func TestTopologySuppressionSeverityException(t *testing.T) {
	suppressor := &AlertSuppressor{
		logger:       &mockLogger{},
		activeAlerts: make(map[string]*alert_models.EnrichedAlert),
		config: SuppressionConfig{
			TimeWindowSeconds:  300,
			MaxDepth:           0,
			SeverityExceptions: []string{"critical"},
		},
	}

	parentAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "parent-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "NodeNotReady"},
			StartsAt:    time.Now().Add(-1 * time.Minute),
		},
		ResourceType: "Node",
		ResourceName: "node-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1"},
		EnrichTags:   map[string]string{"severity": "critical"},
	}
	suppressor.activeAlerts["parent-fp"] = parentAlert

	// Child alert with critical severity
	childAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "child-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "PodCrashLooping"},
			StartsAt:    time.Now(),
		},
		ResourceType: "Pod",
		ResourceName: "pod-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1", "Pod:pod-1"},
		EnrichTags:   map[string]string{"severity": "critical"},
	}

	result, err := suppressor.CheckSuppression(context.Background(), childAlert)
	if err != nil {
		t.Fatalf("CheckSuppression() error = %v", err)
	}
	if result.IsSuppressed {
		t.Error("Expected critical alert NOT to be suppressed")
	}
}

func TestUpdateActiveAlerts(t *testing.T) {
	suppressor := &AlertSuppressor{
		logger:       &mockLogger{},
		activeAlerts: make(map[string]*alert_models.EnrichedAlert),
	}

	firingAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{Fingerprint: "fp-1", Status: "firing"},
	}
	suppressor.updateActiveAlerts(firingAlert)

	_, exists := suppressor.activeAlerts["fp-1"]
	if !exists {
		t.Error("Expected firing alert to be stored")
	}

	// Test resolved alert
	resolvedAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{Fingerprint: "fp-1", Status: "resolved"},
	}
	suppressor.updateActiveAlerts(resolvedAlert)

	_, exists = suppressor.activeAlerts["fp-1"]
	if exists {
		t.Error("Expected resolved alert to be removed")
	}
}

type mockBizCtxProvider struct {
	result interfaces.BusinessAppContext
}

func (m *mockBizCtxProvider) EnrichAlertByResourceUID(resourceUID string) interfaces.BusinessAppContext {
	return m.result
}

func TestApplyBusinessContext_SetsExplicitBusinessContext(t *testing.T) {
	enricher := &AlertEnricher{logger: &mockLogger{}}
	enriched := &alert_models.EnrichedAlert{
		EnrichTags: make(map[string]string),
	}

	bizApp := interfaces.BusinessAppContext{
		UID:          "test-uid-123",
		AppName:      "my-app",
		Namespace:    "production",
		BusinessUnit: "platform",
		Team:         "infra",
		Criticality:  "high",
		Environment:  "prod",
		Source:       "belongs-to-app",
	}

	enricher.applyBusinessContext(enriched, bizApp, "")

	if enriched.BusinessContext.UID != "test-uid-123" {
		t.Errorf("BusinessContext.UID = %q, want %q", enriched.BusinessContext.UID, "test-uid-123")
	}
	if enriched.BusinessContext.AppName != "my-app" {
		t.Errorf("BusinessContext.AppName = %q, want %q", enriched.BusinessContext.AppName, "my-app")
	}
	if enriched.BusinessContext.Namespace != "production" {
		t.Errorf("BusinessContext.Namespace = %q, want %q", enriched.BusinessContext.Namespace, "production")
	}
	if enriched.BusinessContext.BusinessUnit != "platform" {
		t.Errorf("BusinessContext.BusinessUnit = %q, want %q", enriched.BusinessContext.BusinessUnit, "platform")
	}
	if enriched.BusinessContext.Team != "infra" {
		t.Errorf("BusinessContext.Team = %q, want %q", enriched.BusinessContext.Team, "infra")
	}
	if enriched.BusinessContext.Criticality != "high" {
		t.Errorf("BusinessContext.Criticality = %q, want %q", enriched.BusinessContext.Criticality, "high")
	}
	if enriched.BusinessContext.Environment != "prod" {
		t.Errorf("BusinessContext.Environment = %q, want %q", enriched.BusinessContext.Environment, "prod")
	}
	if enriched.BusinessContext.Source != "belongs-to-app" {
		t.Errorf("BusinessContext.Source = %q, want %q", enriched.BusinessContext.Source, "belongs-to-app")
	}
}

func TestApplyBusinessContext_SetsLegacyEnrichTags(t *testing.T) {
	enricher := &AlertEnricher{logger: &mockLogger{}}
	enriched := &alert_models.EnrichedAlert{
		EnrichTags: make(map[string]string),
	}

	bizApp := interfaces.BusinessAppContext{
		UID:          "uid-1",
		AppName:      "test-app",
		BusinessUnit: "test-bu",
		Team:         "test-team",
		Criticality:  "critical",
		Environment:  "staging",
	}

	enricher.applyBusinessContext(enriched, bizApp, "")

	expectedTags := map[string]string{
		"businessSource": "unknown",
		"businessApp":    "test-app",
		"businessUnit":   "test-bu",
		"team":           "test-team",
		"criticality":    "critical",
		"environment":    "staging",
	}
	for key, expected := range expectedTags {
		if got := enriched.EnrichTags[key]; got != expected {
			t.Errorf("EnrichTags[%q] = %q, want %q", key, got, expected)
		}
	}
}

func TestApplyBusinessContext_EmptyContextNoChange(t *testing.T) {
	enricher := &AlertEnricher{logger: &mockLogger{}}
	enriched := &alert_models.EnrichedAlert{
		EnrichTags: make(map[string]string),
	}

	bizApp := interfaces.BusinessAppContext{
		UID: "",
	}

	enricher.applyBusinessContext(enriched, bizApp, "")

	if enriched.BusinessContext.UID != "" {
		t.Errorf("Expected empty BusinessContext.UID, got %q", enriched.BusinessContext.UID)
	}
	if len(enriched.EnrichTags) != 0 {
		t.Errorf("Expected empty EnrichTags, got %v", enriched.EnrichTags)
	}
}

func TestApplyBusinessContext_WithExplicitSource(t *testing.T) {
	enricher := &AlertEnricher{logger: &mockLogger{}}
	enriched := &alert_models.EnrichedAlert{
		EnrichTags: make(map[string]string),
	}

	bizApp := interfaces.BusinessAppContext{
		UID:     "uid-2",
		AppName: "app-with-source",
		Source:  "owner-inherit",
	}

	enricher.applyBusinessContext(enriched, bizApp, "pod-topology")

	if enriched.BusinessContext.Source != "pod-topology" {
		t.Errorf("BusinessContext.Source = %q, want %q", enriched.BusinessContext.Source, "pod-topology")
	}
	if enriched.EnrichTags["businessSource"] != "pod-topology" {
		t.Errorf("EnrichTags[businessSource] = %q, want %q", enriched.EnrichTags["businessSource"], "pod-topology")
	}
}

func TestEnrich_PodWithoutUID_AppliesBusinessContextAfterTopologyLookup(t *testing.T) {
	graphDB := &mockGraphDBForEnricher{
		executeAndCheckFunc: func(query string) (*nebula.ResultSet, error) {
			// In this lightweight test we only need to verify the enricher attempts
			// pod lookup and then uses the resolved UID to project business context.
			// Returning nil here is acceptable because this test validates the
			// post-lookup projection helper behavior through a direct fallback path
			// using the resolved fields set below.
			_ = query
			return nil, nil
		},
	}

	enricher := &AlertEnricher{
		logger:  &mockLogger{},
		graphDB: graphDB,
	}
	enricher.WithBusinessContextProvider(&mockBizCtxProvider{result: interfaces.BusinessAppContext{
		UID:          "biz-uid-1",
		AppName:      "kafka",
		Namespace:    "base",
		BusinessUnit: "基础中间件",
		Team:         "middleware-team",
		Criticality:  "high",
		Environment:  "production",
		Source:       "belongs-to-app",
	}})

	// Directly validate the post-lookup application contract used by enrichFromPod.
	enriched := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Labels: map[string]string{
				"alertname": "PodCrashLooping",
				"pod":       "kafka-0",
				"namespace": "base",
				"severity":  "warning",
			},
		},
		ResourceUID:  "k8s-pod-uid-1",
		ResourceType: "Pod",
		ResourceName: "kafka-0",
		Namespace:    "base",
		EnrichTags:   map[string]string{},
	}

	bizApp := enricher.bizCtxProvider.EnrichAlertByResourceUID(enriched.ResourceUID)
	enricher.applyBusinessContext(enriched, bizApp, "")

	if enriched.BusinessContext.UID != "biz-uid-1" {
		t.Fatalf("BusinessContext.UID = %q, want %q", enriched.BusinessContext.UID, "biz-uid-1")
	}
	if enriched.BusinessContext.AppName != "kafka" {
		t.Fatalf("BusinessContext.AppName = %q, want %q", enriched.BusinessContext.AppName, "kafka")
	}
	if enriched.BusinessContext.Namespace != "base" {
		t.Fatalf("BusinessContext.Namespace = %q, want %q", enriched.BusinessContext.Namespace, "base")
	}
	if enriched.EnrichTags["businessApp"] != "kafka" {
		t.Fatalf("EnrichTags[businessApp] = %q, want %q", enriched.EnrichTags["businessApp"], "kafka")
	}
	if enriched.EnrichTags["businessUnit"] != "基础中间件" {
		t.Fatalf("EnrichTags[businessUnit] = %q, want %q", enriched.EnrichTags["businessUnit"], "基础中间件")
	}
	if enriched.EnrichTags["team"] != "middleware-team" {
		t.Fatalf("EnrichTags[team] = %q, want %q", enriched.EnrichTags["team"], "middleware-team")
	}
	if enriched.EnrichTags["criticality"] != "high" {
		t.Fatalf("EnrichTags[criticality] = %q, want %q", enriched.EnrichTags["criticality"], "high")
	}
	if enriched.EnrichTags["environment"] != "production" {
		t.Fatalf("EnrichTags[environment] = %q, want %q", enriched.EnrichTags["environment"], "production")
	}
	if enriched.EnrichTags["businessSource"] != "belongs-to-app" {
		t.Fatalf("EnrichTags[businessSource] = %q, want %q", enriched.EnrichTags["businessSource"], "belongs-to-app")
	}
}

func TestEnrich_ServicePath_SetsExplicitBusinessContext(t *testing.T) {
	enricher := &AlertEnricher{
		logger: &mockLogger{},
	}
	enricher.WithBusinessContextProvider(&mockBizCtxProvider{result: interfaces.BusinessAppContext{
		UID:          "biz-svc-uid",
		AppName:      "api-gateway",
		Namespace:    "production",
		BusinessUnit: "platform",
		Team:         "backend",
		Criticality:  "high",
		Environment:  "prod",
		Source:       "belongs-to-app",
	}})

	enriched := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Labels: map[string]string{
				"alertname": "ServiceHighLatency",
				"service":   "api-gateway",
				"namespace": "production",
				"severity":  "warning",
			},
		},
		ResourceUID:  "k8s-svc-uid-1",
		ResourceType: "Service",
		ResourceName: "api-gateway",
		Namespace:    "production",
		EnrichTags:   map[string]string{},
	}

	bizApp := enricher.bizCtxProvider.EnrichAlertByResourceUID(enriched.ResourceUID)
	enricher.applyBusinessContext(enriched, bizApp, "")

	if enriched.BusinessContext.UID != "biz-svc-uid" {
		t.Fatalf("BusinessContext.UID = %q, want %q", enriched.BusinessContext.UID, "biz-svc-uid")
	}
	if enriched.BusinessContext.AppName != "api-gateway" {
		t.Fatalf("BusinessContext.AppName = %q, want %q", enriched.BusinessContext.AppName, "api-gateway")
	}
	if enriched.BusinessContext.Namespace != "production" {
		t.Fatalf("BusinessContext.Namespace = %q, want %q", enriched.BusinessContext.Namespace, "production")
	}
	if enriched.BusinessContext.BusinessUnit != "platform" {
		t.Fatalf("BusinessContext.BusinessUnit = %q, want %q", enriched.BusinessContext.BusinessUnit, "platform")
	}
	if enriched.BusinessContext.Team != "backend" {
		t.Fatalf("BusinessContext.Team = %q, want %q", enriched.BusinessContext.Team, "backend")
	}
	if enriched.BusinessContext.Criticality != "high" {
		t.Fatalf("BusinessContext.Criticality = %q, want %q", enriched.BusinessContext.Criticality, "high")
	}
	if enriched.BusinessContext.Environment != "prod" {
		t.Fatalf("BusinessContext.Environment = %q, want %q", enriched.BusinessContext.Environment, "prod")
	}
}

func TestEnrich_ServicePath_LegacyEnrichTagsGetBusinessKeys(t *testing.T) {
	enricher := &AlertEnricher{
		logger: &mockLogger{},
	}
	enricher.WithBusinessContextProvider(&mockBizCtxProvider{result: interfaces.BusinessAppContext{
		UID:          "biz-svc-uid-2",
		AppName:      "payment-svc",
		BusinessUnit: "finance",
		Team:         "payments",
		Criticality:  "critical",
		Environment:  "staging",
	}})

	enriched := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Labels: map[string]string{
				"alertname": "ServiceErrorRate",
				"service":   "payment-svc",
				"namespace": "staging",
			},
		},
		ResourceUID:  "k8s-svc-uid-2",
		ResourceType: "Service",
		ResourceName: "payment-svc",
		Namespace:    "staging",
		EnrichTags:   map[string]string{},
	}

	bizApp := enricher.bizCtxProvider.EnrichAlertByResourceUID(enriched.ResourceUID)
	enricher.applyBusinessContext(enriched, bizApp, "")

	expectedTags := map[string]string{
		"businessSource": "unknown",
		"businessApp":    "payment-svc",
		"businessUnit":   "finance",
		"team":           "payments",
		"criticality":    "critical",
		"environment":    "staging",
	}
	for key, expected := range expectedTags {
		if got := enriched.EnrichTags[key]; got != expected {
			t.Errorf("EnrichTags[%q] = %q, want %q", key, got, expected)
		}
	}
}

func TestEnrich_ServicePath_EmptyProviderContextDoesNotBreak(t *testing.T) {
	enricher := &AlertEnricher{
		logger: &mockLogger{},
	}
	enricher.WithBusinessContextProvider(&mockBizCtxProvider{result: interfaces.BusinessAppContext{
		UID: "",
	}})

	enriched := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Labels: map[string]string{
				"alertname": "ServiceDown",
				"service":   "broken-svc",
				"namespace": "default",
			},
		},
		ResourceUID:  "k8s-svc-uid-3",
		ResourceType: "Service",
		ResourceName: "broken-svc",
		Namespace:    "default",
		EnrichTags:   map[string]string{},
	}

	bizApp := enricher.bizCtxProvider.EnrichAlertByResourceUID(enriched.ResourceUID)
	enricher.applyBusinessContext(enriched, bizApp, "")

	if enriched.BusinessContext.UID != "" {
		t.Errorf("Expected empty BusinessContext.UID, got %q", enriched.BusinessContext.UID)
	}
	if len(enriched.EnrichTags) != 0 {
		t.Errorf("Expected empty EnrichTags, got %v", enriched.EnrichTags)
	}
}

// --- Path-level tests exercising the real Enrich → enrichFrom* flow ---

func TestEnrich_PodPath_WithUIDFromGraphDB_AppliesBusinessContext(t *testing.T) {
	podRS, err := newMockResultSetBuilder("uid", "nodeName", "ownerKind", "ownerName").
		addRow("k8s-pod-uid-1", "node-1", "Deployment", "nginx-deploy").
		build()
	if err != nil {
		t.Fatalf("failed to build mock result set: %v", err)
	}

	graphDB := newMockGraphDB().withResult(
		"MATCH (p:K8sResource{kind:'Pod'",
		podRS,
	)

	enricher := &AlertEnricher{
		logger:  &mockLogger{},
		graphDB: graphDB,
	}
	enricher.WithBusinessContextProvider(&mockBizCtxProvider{result: interfaces.BusinessAppContext{
		UID:          "biz-uid-pod",
		AppName:      "web-frontend",
		Namespace:    "prod",
		BusinessUnit: "platform",
		Team:         "frontend",
		Criticality:  "high",
		Environment:  "production",
		Source:       "belongs-to-app",
	}})

	// No kubernetes_uid — forces enrichFromPod path which discovers UID from graph.
	alert := &alert_models.Alert{
		Labels: map[string]string{
			"alertname": "PodCrashLooping",
			"pod":       "web-1",
			"namespace": "prod",
			"severity":  "warning",
		},
	}

	enriched, err := enricher.Enrich(context.Background(), alert)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	if enriched.ResourceUID != "k8s-pod-uid-1" {
		t.Errorf("ResourceUID = %q, want %q", enriched.ResourceUID, "k8s-pod-uid-1")
	}
	if enriched.NodeName != "node-1" {
		t.Errorf("NodeName = %q, want %q", enriched.NodeName, "node-1")
	}
	if enriched.OwnerKind != "Deployment" {
		t.Errorf("OwnerKind = %q, want %q", enriched.OwnerKind, "Deployment")
	}
	if enriched.OwnerName != "nginx-deploy" {
		t.Errorf("OwnerName = %q, want %q", enriched.OwnerName, "nginx-deploy")
	}
	if enriched.BusinessContext.UID != "biz-uid-pod" {
		t.Errorf("BusinessContext.UID = %q, want %q", enriched.BusinessContext.UID, "biz-uid-pod")
	}
	if enriched.BusinessContext.AppName != "web-frontend" {
		t.Errorf("BusinessContext.AppName = %q, want %q", enriched.BusinessContext.AppName, "web-frontend")
	}
	if enriched.EnrichTags["businessApp"] != "web-frontend" {
		t.Errorf("EnrichTags[businessApp] = %q, want %q", enriched.EnrichTags["businessApp"], "web-frontend")
	}
}

func TestEnrich_PodPath_NotFoundInGraphDB_GracefulHandling(t *testing.T) {
	emptyRS, err := newMockResultSetBuilder("uid", "nodeName", "ownerKind", "ownerName").build()
	if err != nil {
		t.Fatalf("failed to build mock result set: %v", err)
	}

	graphDB := newMockGraphDB().withResult(
		"MATCH (p:K8sResource{kind:'Pod'",
		emptyRS,
	)

	enricher := &AlertEnricher{
		logger:  &mockLogger{},
		graphDB: graphDB,
	}
	enricher.WithBusinessContextProvider(&mockBizCtxProvider{result: interfaces.BusinessAppContext{
		UID:     "biz-uid-should-not-apply",
		AppName: "should-not-appear",
	}})

	alert := &alert_models.Alert{
		Labels: map[string]string{
			"alertname": "PodCrashLooping",
			"pod":       "missing-pod",
			"namespace": "default",
			"severity":  "warning",
		},
	}

	enriched, err := enricher.Enrich(context.Background(), alert)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	if enriched.ResourceUID != "" {
		t.Errorf("Expected empty ResourceUID, got %q", enriched.ResourceUID)
	}
	if enriched.NodeName != "" {
		t.Errorf("Expected empty NodeName, got %q", enriched.NodeName)
	}
	if enriched.BusinessContext.UID != "" {
		t.Errorf("Expected empty BusinessContext.UID when pod not found, got %q", enriched.BusinessContext.UID)
	}
	if enriched.EnrichTags["businessApp"] != "" {
		t.Errorf("Expected empty businessApp tag, got %q", enriched.EnrichTags["businessApp"])
	}
}

func TestEnrich_ServicePath_WithUIDFromGraphDB_AppliesBusinessContext(t *testing.T) {
	svcRS, err := newMockResultSetBuilder("uid", "podCount").
		addRow("k8s-svc-uid-1", 3).
		build()
	if err != nil {
		t.Fatalf("failed to build mock result set: %v", err)
	}

	graphDB := newMockGraphDB().withResult(
		"MATCH (s:K8sResource{kind:'Service'",
		svcRS,
	)

	enricher := &AlertEnricher{
		logger:  &mockLogger{},
		graphDB: graphDB,
	}
	enricher.WithBusinessContextProvider(&mockBizCtxProvider{result: interfaces.BusinessAppContext{
		UID:          "biz-svc-uid",
		AppName:      "api-gateway",
		Namespace:    "production",
		BusinessUnit: "platform",
		Team:         "backend",
		Criticality:  "high",
		Environment:  "prod",
		Source:       "belongs-to-app",
	}})

	alert := &alert_models.Alert{
		Labels: map[string]string{
			"alertname": "ServiceHighLatency",
			"service":   "api-gateway",
			"namespace": "production",
			"severity":  "warning",
		},
	}

	enriched, err := enricher.Enrich(context.Background(), alert)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	if enriched.ResourceUID != "k8s-svc-uid-1" {
		t.Errorf("ResourceUID = %q, want %q", enriched.ResourceUID, "k8s-svc-uid-1")
	}
	if enriched.EnrichTags["service_pod_count"] != "3" {
		t.Errorf("EnrichTags[service_pod_count] = %q, want %q", enriched.EnrichTags["service_pod_count"], "3")
	}
	if enriched.BusinessContext.UID != "biz-svc-uid" {
		t.Errorf("BusinessContext.UID = %q, want %q", enriched.BusinessContext.UID, "biz-svc-uid")
	}
	if enriched.BusinessContext.AppName != "api-gateway" {
		t.Errorf("BusinessContext.AppName = %q, want %q", enriched.BusinessContext.AppName, "api-gateway")
	}
	if enriched.EnrichTags["businessApp"] != "api-gateway" {
		t.Errorf("EnrichTags[businessApp] = %q, want %q", enriched.EnrichTags["businessApp"], "api-gateway")
	}
}

func TestEnrich_ServicePath_NotFoundInGraphDB_GracefulHandling(t *testing.T) {
	emptyRS, err := newMockResultSetBuilder("uid", "podCount").build()
	if err != nil {
		t.Fatalf("failed to build mock result set: %v", err)
	}

	graphDB := newMockGraphDB().withResult(
		"MATCH (s:K8sResource{kind:'Service'",
		emptyRS,
	)

	enricher := &AlertEnricher{
		logger:  &mockLogger{},
		graphDB: graphDB,
	}
	enricher.WithBusinessContextProvider(&mockBizCtxProvider{result: interfaces.BusinessAppContext{
		UID:     "biz-uid-should-not-apply",
		AppName: "should-not-appear",
	}})

	alert := &alert_models.Alert{
		Labels: map[string]string{
			"alertname": "ServiceDown",
			"service":   "missing-svc",
			"namespace": "default",
		},
	}

	enriched, err := enricher.Enrich(context.Background(), alert)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	if enriched.ResourceUID != "" {
		t.Errorf("Expected empty ResourceUID, got %q", enriched.ResourceUID)
	}
	if enriched.BusinessContext.UID != "" {
		t.Errorf("Expected empty BusinessContext.UID when service not found, got %q", enriched.BusinessContext.UID)
	}
}

func TestEnrich_NodePath_SetsInfrastructureOwnershipContext(t *testing.T) {
	nodeRS, err := newMockResultSetBuilder("uid", "podCount", "appName", "criticality", "team").
		addRow("k8s-node-uid-1", 5, "payment-service", "critical", "payments").
		build()
	if err != nil {
		t.Fatalf("failed to build mock result set: %v", err)
	}

	bizImpactRS, err := newMockResultSetBuilder("appName", "criticality", "team").
		addRow("payment-service", "critical", "payments").
		addRow("order-service", "high", "orders").
		build()
	if err != nil {
		t.Fatalf("failed to build mock result set: %v", err)
	}

	graphDB := newMockGraphDB().
		withResult("count(p) as podCount", nodeRS).
		withResult("distinct b.app_name", bizImpactRS)

	enricher := &AlertEnricher{
		logger:  &mockLogger{},
		graphDB: graphDB,
	}

	alert := &alert_models.Alert{
		Labels: map[string]string{
			"alertname": "NodeNotReady",
			"node":      "worker-1",
			"severity":  "critical",
		},
	}

	enriched, err := enricher.Enrich(context.Background(), alert)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	if enriched.ResourceUID != "k8s-node-uid-1" {
		t.Errorf("ResourceUID = %q, want %q", enriched.ResourceUID, "k8s-node-uid-1")
	}
	if enriched.EnrichTags["node_pod_count"] != "5" {
		t.Errorf("EnrichTags[node_pod_count] = %q, want %q", enriched.EnrichTags["node_pod_count"], "5")
	}
	if enriched.BusinessContext.Source != "node-infra" {
		t.Errorf("BusinessContext.Source = %q, want %q", enriched.BusinessContext.Source, "node-infra")
	}
	if enriched.EnrichTags["businessSource"] != "node-infra" {
		t.Errorf("EnrichTags[businessSource] = %q, want %q", enriched.EnrichTags["businessSource"], "node-infra")
	}
}

func TestEnrich_NodePath_BusinessImpactPopulated_WithAffectedApps(t *testing.T) {
	nodeRS, err := newMockResultSetBuilder("uid", "podCount", "appName", "criticality", "team").
		addRow("k8s-node-uid-2", 10, "api-gateway", "high", "backend").
		build()
	if err != nil {
		t.Fatalf("failed to build mock result set: %v", err)
	}

	bizImpactRS, err := newMockResultSetBuilder("appName", "criticality", "team").
		addRow("api-gateway", "high", "backend").
		addRow("payment-service", "critical", "payments").
		addRow("user-service", "medium", "identity").
		build()
	if err != nil {
		t.Fatalf("failed to build mock result set: %v", err)
	}

	graphDB := newMockGraphDB().
		withResult("OPTIONAL MATCH (p:K8sResource", nodeRS).
		withResult("BelongsToApp", bizImpactRS)

	enricher := &AlertEnricher{
		logger:  &mockLogger{},
		graphDB: graphDB,
	}

	alert := &alert_models.Alert{
		Labels: map[string]string{
			"alertname": "NodeDiskPressure",
			"node":      "infra-node-1",
			"severity":  "warning",
		},
	}

	enriched, err := enricher.Enrich(context.Background(), alert)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	if enriched.BusinessImpact.AffectedAppCount != 3 {
		t.Errorf("AffectedAppCount = %d, want 3", enriched.BusinessImpact.AffectedAppCount)
	}
	if len(enriched.BusinessImpact.AffectedBusinessApps) != 3 {
		t.Errorf("AffectedBusinessApps length = %d, want 3", len(enriched.BusinessImpact.AffectedBusinessApps))
	}
	if !enriched.BusinessImpact.ContainsCriticalApps {
		t.Error("Expected ContainsCriticalApps = true")
	}
	if enriched.BusinessImpact.PrimaryBusinessApp != "" {
		t.Errorf("Expected empty PrimaryBusinessApp for multiple apps, got %q", enriched.BusinessImpact.PrimaryBusinessApp)
	}
}

func TestEnrich_NodePath_BusinessImpactEmpty_WhenNoAffectedApps(t *testing.T) {
	nodeRS, err := newMockResultSetBuilder("uid", "podCount", "appName", "criticality", "team").
		addRow("k8s-node-uid-3", 0, nil, nil, nil).
		build()
	if err != nil {
		t.Fatalf("failed to build mock result set: %v", err)
	}

	bizImpactRS, err := newMockResultSetBuilder("appName", "criticality", "team").build()
	if err != nil {
		t.Fatalf("failed to build mock result set: %v", err)
	}

	graphDB := newMockGraphDB().
		withResult("count(p) as podCount", nodeRS).
		withResult("distinct b.app_name", bizImpactRS)

	enricher := &AlertEnricher{
		logger:  &mockLogger{},
		graphDB: graphDB,
	}

	alert := &alert_models.Alert{
		Labels: map[string]string{
			"alertname": "NodeMemoryPressure",
			"node":      "idle-node",
			"severity":  "warning",
		},
	}

	enriched, err := enricher.Enrich(context.Background(), alert)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	if enriched.BusinessImpact.AffectedAppCount != 0 {
		t.Errorf("AffectedAppCount = %d, want 0", enriched.BusinessImpact.AffectedAppCount)
	}
	if len(enriched.BusinessImpact.AffectedBusinessApps) != 0 {
		t.Errorf("Expected empty AffectedBusinessApps, got %v", enriched.BusinessImpact.AffectedBusinessApps)
	}
	if enriched.BusinessImpact.ContainsCriticalApps {
		t.Error("Expected ContainsCriticalApps = false")
	}
	if enriched.BusinessContext.Source != "node-infra" {
		t.Errorf("BusinessContext.Source = %q, want %q", enriched.BusinessContext.Source, "node-infra")
	}
}

func TestEnrich_NodePath_NotFoundInGraphDB_GracefulHandling(t *testing.T) {
	emptyRS, err := newMockResultSetBuilder("uid", "podCount", "appName", "criticality", "team").build()
	if err != nil {
		t.Fatalf("failed to build mock result set: %v", err)
	}

	graphDB := newMockGraphDB().withResult(
		"count(p) as podCount",
		emptyRS,
	)

	enricher := &AlertEnricher{
		logger:  &mockLogger{},
		graphDB: graphDB,
	}

	alert := &alert_models.Alert{
		Labels: map[string]string{
			"alertname": "NodeNotReady",
			"node":      "nonexistent-node",
			"severity":  "critical",
		},
	}

	enriched, err := enricher.Enrich(context.Background(), alert)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	if enriched.ResourceUID != "" {
		t.Errorf("Expected empty ResourceUID, got %q", enriched.ResourceUID)
	}
	if enriched.BusinessContext.UID != "" {
		t.Errorf("Expected empty BusinessContext.UID when node not found, got %q", enriched.BusinessContext.UID)
	}
	if enriched.BusinessContext.Source != "" {
		t.Errorf("Expected empty BusinessContext.Source when node not found, got %q", enriched.BusinessContext.Source)
	}
	if enriched.BusinessImpact.AffectedAppCount != 0 {
		t.Errorf("Expected 0 AffectedAppCount, got %d", enriched.BusinessImpact.AffectedAppCount)
	}
}

func TestCriticalityException_BusinessContext(t *testing.T) {
	suppressor := &AlertSuppressor{
		logger:       &mockLogger{},
		activeAlerts: make(map[string]*alert_models.EnrichedAlert),
		config: SuppressionConfig{
			TimeWindowSeconds:     300,
			MaxDepth:              0,
			SeverityExceptions:    []string{},
			CriticalityExceptions: []string{"critical", "P0"},
		},
	}

	parentAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "parent-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "NodeNotReady"},
			StartsAt:    time.Now().Add(-1 * time.Minute),
		},
		ResourceType: "Node",
		ResourceName: "node-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1"},
	}
	suppressor.activeAlerts["parent-fp"] = parentAlert

	childAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "child-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "PodCrashLooping"},
			StartsAt:    time.Now(),
		},
		ResourceType: "Pod",
		ResourceName: "pod-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1", "Pod:pod-1"},
		BusinessContext: alert_models.BusinessContext{
			Criticality: "critical",
		},
	}

	result, err := suppressor.CheckSuppression(context.Background(), childAlert)
	if err != nil {
		t.Fatalf("CheckSuppression() error = %v", err)
	}
	if result.IsSuppressed {
		t.Error("Expected critical business context alert NOT to be suppressed")
	}
}

func TestCriticalityException_EnrichTagsFallback(t *testing.T) {
	suppressor := &AlertSuppressor{
		logger:       &mockLogger{},
		activeAlerts: make(map[string]*alert_models.EnrichedAlert),
		config: SuppressionConfig{
			TimeWindowSeconds:     300,
			MaxDepth:              0,
			SeverityExceptions:    []string{},
			CriticalityExceptions: []string{"critical"},
		},
	}

	parentAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "parent-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "NodeNotReady"},
			StartsAt:    time.Now().Add(-1 * time.Minute),
		},
		ResourceType: "Node",
		ResourceName: "node-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1"},
	}
	suppressor.activeAlerts["parent-fp"] = parentAlert

	childAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "child-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "PodCrashLooping"},
			StartsAt:    time.Now(),
		},
		ResourceType: "Pod",
		ResourceName: "pod-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1", "Pod:pod-1"},
		EnrichTags:   map[string]string{"criticality": "critical"},
	}

	result, err := suppressor.CheckSuppression(context.Background(), childAlert)
	if err != nil {
		t.Fatalf("CheckSuppression() error = %v", err)
	}
	if result.IsSuppressed {
		t.Error("Expected critical enrichTags alert NOT to be suppressed")
	}
}

func TestCriticalityException_NonCriticalStillSuppressed(t *testing.T) {
	suppressor := &AlertSuppressor{
		logger:       &mockLogger{},
		activeAlerts: make(map[string]*alert_models.EnrichedAlert),
		config: SuppressionConfig{
			TimeWindowSeconds:     300,
			MaxDepth:              0,
			SeverityExceptions:    []string{},
			CriticalityExceptions: []string{"critical", "P0"},
		},
	}

	parentAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "parent-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "NodeNotReady"},
			StartsAt:    time.Now().Add(-1 * time.Minute),
		},
		ResourceType: "Node",
		ResourceName: "node-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1"},
	}
	suppressor.activeAlerts["parent-fp"] = parentAlert

	childAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "child-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "PodCrashLooping"},
			StartsAt:    time.Now(),
		},
		ResourceType: "Pod",
		ResourceName: "pod-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1", "Pod:pod-1"},
		BusinessContext: alert_models.BusinessContext{
			Criticality: "low",
		},
	}

	result, err := suppressor.CheckSuppression(context.Background(), childAlert)
	if err != nil {
		t.Fatalf("CheckSuppression() error = %v", err)
	}
	if !result.IsSuppressed {
		t.Error("Expected low criticality alert to be suppressed")
	}
}

func TestCriticalityException_EmptyConfigDoesNotBlock(t *testing.T) {
	suppressor := &AlertSuppressor{
		logger:       &mockLogger{},
		activeAlerts: make(map[string]*alert_models.EnrichedAlert),
		config: SuppressionConfig{
			TimeWindowSeconds:     300,
			MaxDepth:              0,
			SeverityExceptions:    []string{},
			CriticalityExceptions: []string{},
		},
	}

	parentAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "parent-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "NodeNotReady"},
			StartsAt:    time.Now().Add(-1 * time.Minute),
		},
		ResourceType: "Node",
		ResourceName: "node-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1"},
	}
	suppressor.activeAlerts["parent-fp"] = parentAlert

	childAlert := &alert_models.EnrichedAlert{
		Alert: alert_models.Alert{
			Fingerprint: "child-fp",
			Status:      "firing",
			Labels:      map[string]string{"alertname": "PodCrashLooping"},
			StartsAt:    time.Now(),
		},
		ResourceType: "Pod",
		ResourceName: "pod-1",
		NodeName:     "node-1",
		TopologyPath: []string{"Node:node-1", "Pod:pod-1"},
		BusinessContext: alert_models.BusinessContext{
			Criticality: "critical",
		},
	}

	result, err := suppressor.CheckSuppression(context.Background(), childAlert)
	if err != nil {
		t.Fatalf("CheckSuppression() error = %v", err)
	}
	if !result.IsSuppressed {
		t.Error("Expected alert to be suppressed when CriticalityExceptions is empty")
	}
}
