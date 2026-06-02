package services

import (
	"encoding/json"
	"testing"

	nebula "github.com/vesoft-inc/nebula-go/v3"
	nebula0 "github.com/vesoft-inc/nebula-go/v3/nebula"
	nebulaGraph "github.com/vesoft-inc/nebula-go/v3/nebula/graph"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"gitee.com/tddh/mutong/config"
	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models"
)

func makeGoFromResultSet(dstUID string) *nebula.ResultSet {
	ds := &nebula0.DataSet{
		ColumnNames: [][]byte{[]byte("dst")},
		Rows:        []*nebula0.Row{{Values: []*nebula0.Value{{SVal: []byte(dstUID)}}}},
	}
	resp := &nebulaGraph.ExecutionResponse{
		ErrorCode:   0,
		LatencyInUs: 100,
		Data:        ds,
	}
	rs, _ := nebula.GenResultSet(resp)
	return rs
}

func newTestBlsSyncer(db interfaces.GraphDB, normConf config.AppNameNormalizationConf) *BusinessLabelSyncer {
	s := &BusinessLabelSyncer{
		logger:       &mockNebulaLogger{},
		graphDB:      db,
		cache:        &mockNebulaCache{},
		messageQueue: nil,
		stopCh:       make(chan struct{}),
		taskChan:     make(chan *interfaces.Message, blsTaskChanSize),
	}
	s.compileNormalizationRules(normConf)
	return s
}

func TestNormalizeAppName_MatchesRule(t *testing.T) {
	s := newTestBlsSyncer(nil, config.AppNameNormalizationConf{
		Enabled: true,
		Rules: []config.NormalizationRuleEntry{
			{Name: "redis", Pattern: `^(.*)-(cluster|master|slave)$`, Replacement: "$1"},
		},
	})

	got := s.normalizeAppName("redis-cluster", "default", "StatefulSet")
	if got != "redis" {
		t.Fatalf("expected 'redis', got %q", got)
	}
}

func TestNormalizeAppName_NoMatch(t *testing.T) {
	s := newTestBlsSyncer(nil, config.AppNameNormalizationConf{
		Enabled: true,
		Rules: []config.NormalizationRuleEntry{
			{Name: "redis", Pattern: `^(.*)-(cluster|master)$`, Replacement: "$1"},
		},
	})

	got := s.normalizeAppName("myapp", "default", "Deployment")
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestNormalizeAppName_DisabledConfig(t *testing.T) {
	s := newTestBlsSyncer(nil, config.AppNameNormalizationConf{
		Enabled: false,
		Rules: []config.NormalizationRuleEntry{
			{Pattern: `^(.*)-(cluster)$`, Replacement: "$1"},
		},
	})

	got := s.normalizeAppName("redis-cluster", "default", "StatefulSet")
	if got != "" {
		t.Fatalf("expected empty when disabled, got %q", got)
	}
}

func TestNormalizeAppName_InvalidPattern_Skipped(t *testing.T) {
	s := newTestBlsSyncer(nil, config.AppNameNormalizationConf{
		Enabled: true,
		Rules: []config.NormalizationRuleEntry{
			{Name: "good", Pattern: `^(.*)-svc$`, Replacement: "$1"},
			{Name: "bad", Pattern: `([incomplete`, Replacement: "$1"},
		},
	})

	got := s.normalizeAppName("myapp-svc", "default", "Deployment")
	if got != "myapp" {
		t.Fatalf("expected 'myapp', got %q", got)
	}
}

func TestNormalizeAppName_ReplacementYieldsSame_Skipped(t *testing.T) {
	s := newTestBlsSyncer(nil, config.AppNameNormalizationConf{
		Enabled: true,
		Rules: []config.NormalizationRuleEntry{
			{Name: "noop", Pattern: `^(.*)$`, Replacement: "$1"},
		},
	})

	got := s.normalizeAppName("redis", "default", "Deployment")
	if got != "" {
		t.Fatalf("expected empty when replacement equals original, got %q", got)
	}
}

func TestNormalizeAppName_NamespaceFilter(t *testing.T) {
	s := newTestBlsSyncer(nil, config.AppNameNormalizationConf{
		Enabled: true,
		Rules: []config.NormalizationRuleEntry{
			{Name: "redis-prod-only", NamespaceMatch: "prod-*", Pattern: `^(.*)-(cluster)$`, Replacement: "$1"},
		},
	})

	if got := s.normalizeAppName("redis-cluster", "dev-ns", "StatefulSet"); got != "" {
		t.Fatalf("expected empty for non-matching namespace, got %q", got)
	}

	if got := s.normalizeAppName("redis-cluster", "prod-ns", "StatefulSet"); got != "redis" {
		t.Fatalf("expected 'redis' for matching namespace, got %q", got)
	}
}

func TestNormalizeAppName_KindFilter(t *testing.T) {
	s := newTestBlsSyncer(nil, config.AppNameNormalizationConf{
		Enabled: true,
		Rules: []config.NormalizationRuleEntry{
			{Name: "redis-kind", KindMatch: []string{"StatefulSet"}, Pattern: `^(.*)-(cluster)$`, Replacement: "$1"},
		},
	})

	if got := s.normalizeAppName("redis-cluster", "default", "Deployment"); got != "" {
		t.Fatalf("expected empty for non-matching kind, got %q", got)
	}

	if got := s.normalizeAppName("redis-cluster", "default", "StatefulSet"); got != "redis" {
		t.Fatalf("expected 'redis' for matching kind, got %q", got)
	}
}

func TestNormalizeAppName_EmptyKindMatch_MatchesAll(t *testing.T) {
	s := newTestBlsSyncer(nil, config.AppNameNormalizationConf{
		Enabled: true,
		Rules: []config.NormalizationRuleEntry{
			{Name: "all-kinds", KindMatch: []string{}, Pattern: `^(.*)-(cluster)$`, Replacement: "$1"},
		},
	})

	if got := s.normalizeAppName("redis-cluster", "default", "Deployment"); got != "redis" {
		t.Fatalf("expected 'redis' for empty kindMatch, got %q", got)
	}
}

func TestSyncApp_LabelTakesPriorityOverNormalization(t *testing.T) {
	calls := make([]string, 0)
	db := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			calls = append(calls, query)
			return nil, nil
		},
	}

	s := newTestBlsSyncer(db, config.AppNameNormalizationConf{
		Enabled: true,
		Rules:   []config.NormalizationRuleEntry{{Pattern: `^(.*)-(cluster)$`, Replacement: "$1"}},
	})

	obj := makeTestUnstructured("StatefulSet", "redis-cluster", "default", "uid-norm-1")
	obj.SetLabels(map[string]string{"app.mutong.io/name": "my-redis"})

	s.syncApp(obj, "test-cluster", "", "bls:uid-norm-1", nil)
	s.flushBuffers()

	found := false
	for _, call := range calls {
		if strHas(call, `"my-redis"`) && strHas(call, `bizapp-test-cluster-default-my-redis`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("syncApp should use label 'my-redis', got calls: %v", calls)
	}
}

func TestSyncApp_UsesNormalizationWhenNoLabel(t *testing.T) {
	calls := make([]string, 0)
	db := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			calls = append(calls, query)
			return nil, nil
		},
	}

	s := newTestBlsSyncer(db, config.AppNameNormalizationConf{
		Enabled: true,
		Rules:   []config.NormalizationRuleEntry{{Pattern: `^(.*)-(cluster)$`, Replacement: "$1"}},
	})

	obj := makeTestUnstructured("StatefulSet", "redis-cluster", "default", "uid-norm-2")
	s.syncApp(obj, "test-cluster", "", "bls:uid-norm-2", nil)
	s.flushBuffers()

	for _, call := range calls {
		if strHas(call, `bizapp-test-cluster-default-redis`) {
			return
		}
	}
	t.Fatalf("syncApp should create bizUID with normalized name 'redis', got calls: %v", calls)
}

func TestRemoveApp_DeletesEdgeAndVertexWhenLastRef(t *testing.T) {
	calls := make([]string, 0)
	db := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			calls = append(calls, query)
			return nil, nil
		},
	}

	s := newTestBlsSyncer(db, config.AppNameNormalizationConf{Enabled: false})
	s.cntFn = func(bizUID string) int { return 0 }

	s.bufferMu.Lock()
	s.deleteEdgeBuffer = append(s.deleteEdgeBuffer, blsDeleteEdgeBatchItem{
		fromUID: "res-del-1",
		toUID:   "bizapp-test-cluster-default-redis",
		msg:     nil,
	})
	s.bufferMu.Unlock()
	s.flushBuffers()
	s.deleteBusinessAppVertex("bizapp-test-cluster-default-redis")

	hasDeleteEdge := false
	hasDeleteVertex := false
	for _, call := range calls {
		if strHas(call, "DELETE EDGE BelongsToApp") {
			hasDeleteEdge = true
		}
		if strHas(call, "DELETE VERTEX") {
			hasDeleteVertex = true
		}
	}
	if !hasDeleteEdge {
		t.Fatal("expected DELETE EDGE call")
	}
	if !hasDeleteVertex {
		t.Fatal("expected DELETE VERTEX call when count is 0")
	}
}

func TestRemoveApp_DeletesEdgeButNotVertexWhenOtherRefsExist(t *testing.T) {
	calls := make([]string, 0)
	db := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			calls = append(calls, query)
			return nil, nil
		},
	}

	s := newTestBlsSyncer(db, config.AppNameNormalizationConf{Enabled: false})
	s.cntFn = func(bizUID string) int { return 2 }

	s.bufferMu.Lock()
	s.deleteEdgeBuffer = append(s.deleteEdgeBuffer, blsDeleteEdgeBatchItem{
		fromUID: "res-del-2",
		toUID:   "bizapp-test-cluster-default-redis",
		msg:     nil,
	})
	s.bufferMu.Unlock()
	s.flushBuffers()

	hasDeleteEdge := false
	hasDeleteVertex := false
	for _, call := range calls {
		if strHas(call, "DELETE EDGE BelongsToApp") {
			hasDeleteEdge = true
		}
		if strHas(call, "DELETE VERTEX") {
			hasDeleteVertex = true
		}
	}
	if !hasDeleteEdge {
		t.Fatal("expected DELETE EDGE call")
	}
	if hasDeleteVertex {
		t.Fatal("should NOT delete vertex when other refs exist")
	}
}

func makeTestKafkaResourceMessage(eventType, group string, obj *unstructured.Unstructured) *interfaces.Message {
	jsonBytes, _ := json.Marshal(obj.Object)
	msgBody, _ := json.Marshal(models.KafkaResourceMessage{
		EventType: eventType,
		Group:     group,
		Object:    jsonBytes,
	})
	return &interfaces.Message{
		Key:   []byte(obj.GetUID()),
		Value: msgBody,
		Topic: "business-workloads",
	}
}

func TestProcessWorkloadMessage_DeletedEventRoutesToRemoveApp(t *testing.T) {
	calls := make([]string, 0)
	expectedBizUID := "bizapp--default-myapp"

	db := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			calls = append(calls, query)
			if strHas(query, "GO FROM") {
				return makeGoFromResultSet(expectedBizUID), nil
			}
			return nil, nil
		},
	}

	s := newTestBlsSyncer(db, config.AppNameNormalizationConf{Enabled: false})

	obj := makeTestUnstructured("Deployment", "myapp", "default", "del-uid")
	obj.SetResourceVersion("v1")

	msg := makeTestKafkaResourceMessage("Deleted", "core", obj)

	s.processWorkloadMessage(msg)
	s.flushBuffers()

	hasDeleteEdge := false
	for _, call := range calls {
		if strHas(call, "DELETE EDGE BelongsToApp") {
			hasDeleteEdge = true
		}
	}
	if !hasDeleteEdge {
		t.Fatal("expected DELETE EDGE call for Deleted event")
	}
}

func TestProcessWorkloadMessage_UnknownEventType(t *testing.T) {
	s := newTestBlsSyncer(nil, config.AppNameNormalizationConf{Enabled: false})

	obj := makeTestUnstructured("Deployment", "myapp", "default", "unknown-uid")
	obj.SetResourceVersion("v1")

	jsonBytes, _ := json.Marshal(obj.Object)
	msgBody, _ := json.Marshal(models.KafkaResourceMessage{
		EventType: "Unknown",
		Group:     "core",
		Object:    jsonBytes,
	})
	msg := &interfaces.Message{
		Key:   []byte("unknown-uid"),
		Value: msgBody,
		Topic: "business-workloads",
	}

	s.processWorkloadMessage(msg)
}

func strHas(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
