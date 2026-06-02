package services

import (
	"errors"
	"sync"
	"testing"
	"time"

	nebula "github.com/vesoft-inc/nebula-go/v3"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models"
)

// --- Mock implementations ---

type mockNebulaLogger struct{}

func (m *mockNebulaLogger) Debug(msg string, fields ...zap.Field) {}
func (m *mockNebulaLogger) Info(msg string, fields ...zap.Field)  {}
func (m *mockNebulaLogger) Warn(msg string, fields ...zap.Field)  {}
func (m *mockNebulaLogger) Error(msg string, fields ...zap.Field) {}

type mockNebulaGraphDB struct {
	mu              sync.Mutex
	executeFn       func(query string) (*nebula.ResultSet, error)
	executeAndCheck func(query string) (*nebula.ResultSet, error)
	calls           []string
}

func (m *mockNebulaGraphDB) Execute(query string) (*nebula.ResultSet, error) {
	m.mu.Lock()
	m.calls = append(m.calls, query)
	m.mu.Unlock()
	if m.executeFn != nil {
		return m.executeFn(query)
	}
	return nil, nil
}

func (m *mockNebulaGraphDB) ExecuteAndCheck(query string) (*nebula.ResultSet, error) {
	m.mu.Lock()
	m.calls = append(m.calls, query)
	m.mu.Unlock()
	if m.executeAndCheck != nil {
		return m.executeAndCheck(query)
	}
	return nil, nil
}

type mockNebulaCache struct {
	getFn    func(key string) ([]byte, error)
	setFn    func(key string, value []byte) error
	deleteFn func(key string) error
	statsFn  func() interfaces.CacheStats
}

func (m *mockNebulaCache) Get(key string) ([]byte, error) {
	if m.getFn != nil {
		return m.getFn(key)
	}
	return nil, errors.New("not found")
}

func (m *mockNebulaCache) Set(key string, value []byte) error {
	if m.setFn != nil {
		return m.setFn(key, value)
	}
	return nil
}

func (m *mockNebulaCache) Delete(key string) error {
	if m.deleteFn != nil {
		return m.deleteFn(key)
	}
	return nil
}

func (m *mockNebulaCache) Stats() interfaces.CacheStats {
	if m.statsFn != nil {
		return m.statsFn()
	}
	return interfaces.CacheStats{}
}

// --- Test helper ---

func newTestK8sServiceWithMocks(db interfaces.GraphDB, cache interfaces.Cache) *K8sResoureService {
	return &K8sResoureService{
		logger:         &mockNebulaLogger{},
		graphDB:        db,
		cache:          cache,
		apiResources:   make(map[string]bool),
		kafkaSem:       make(chan struct{}, 500),
		maxRetries:     5,
		retryBackoff:   100,
		resyncInterval: 30 * time.Minute,
	}
}

// --- Tests for insertK8sResourceToNebula ---

func TestInsertK8sResourceToNebula_GeneratesValidQuery(t *testing.T) {
	resource := models.K8sResource{
		Uid:            "test-uid-123",
		Name:           "test-pod",
		Group:          "core",
		NameSpace:      "default",
		Kind:           "Pod",
		APIVersion:     "v1",
		IsDeleted:      false,
		Labels:         "app=test,version=v1",
		ResourceDefine: `{"kind":"Pod"}`,
	}

	query := insertK8sResourceToNebula(resource)

	if query == "" {
		t.Fatal("Expected non-empty query")
	}

	// Verify key parts of the generated query
	if !containsStr(query, "INSERT VERTEX K8sResource") {
		t.Error("Expected INSERT VERTEX K8sResource in query")
	}
	if !containsStr(query, "test-uid-123") {
		t.Error("Expected UID in query")
	}
	if !containsStr(query, "test-pod") {
		t.Error("Expected name in query")
	}
	if !containsStr(query, "Pod") {
		t.Error("Expected kind in query")
	}
	if !containsStr(query, "default") {
		t.Error("Expected namespace in query")
	}
}

func TestInsertK8sResourceToNebula_MarksDeleted(t *testing.T) {
	resource := models.K8sResource{
		Uid:            "deleted-uid",
		Name:           "deleted-pod",
		Group:          "core",
		NameSpace:      "default",
		Kind:           "Pod",
		APIVersion:     "v1",
		IsDeleted:      true,
		Labels:         "",
		ResourceDefine: "{}",
	}

	query := insertK8sResourceToNebula(resource)

	if !containsStr(query, "true") {
		t.Error("Expected is_deleted = true in query")
	}
}

// --- Tests for quoteJSONString ---

func TestQuoteJSONString_ValidString(t *testing.T) {
	result := quoteJSONString("hello world")
	if result != `"hello world"` {
		t.Errorf("Expected quoted string, got %q", result)
	}
}

func TestQuoteJSONString_EmptyString(t *testing.T) {
	result := quoteJSONString("")
	if result != `""` {
		t.Errorf("Expected empty quoted string, got %q", result)
	}
}

func TestQuoteJSONString_SpecialChars(t *testing.T) {
	result := quoteJSONString("hello\tworld\n")
	// fastjson should properly escape special characters
	if result == "" {
		t.Error("Expected non-empty result for string with special chars")
	}
}

// --- Tests for contains ---

func TestContains_Found(t *testing.T) {
	slice := []string{"a", "b", "c"}
	if !contains(slice, "b") {
		t.Error("Expected to find 'b' in slice")
	}
}

func TestContains_NotFound(t *testing.T) {
	slice := []string{"a", "b", "c"}
	if contains(slice, "d") {
		t.Error("Expected not to find 'd' in slice")
	}
}

func TestContains_EmptySlice(t *testing.T) {
	slice := []string{}
	if contains(slice, "a") {
		t.Error("Expected not to find 'a' in empty slice")
	}
}

// --- Tests for insertEdge ---

func TestInsertEdge_GeneratesCorrectQuery(t *testing.T) {
	mockDB := &mockNebulaGraphDB{}
	svc := newTestK8sServiceWithMocks(mockDB, nil)

	err := svc.insertEdge("BelongsTo", "uid-a", "uid-b")
	if err != nil {
		t.Fatalf("insertEdge() error = %v", err)
	}

	if len(mockDB.calls) != 1 {
		t.Fatalf("Expected 1 call, got %d", len(mockDB.calls))
	}

	expected := `INSERT EDGE BelongsTo () VALUES "uid-a" -> "uid-b":();`
	if mockDB.calls[0] != expected {
		t.Errorf("Expected query %q, got %q", expected, mockDB.calls[0])
	}
}

func TestInsertEdge_PropagatesError(t *testing.T) {
	expectedErr := errors.New("db error")
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, expectedErr
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, nil)

	err := svc.insertEdge("BelongsTo", "uid-a", "uid-b")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if err.Error() != expectedErr.Error() {
		t.Errorf("Expected error %q, got %q", expectedErr, err)
	}
}

// --- Tests for InsertEdgeWithCleanup ---

func TestInsertEdgeWithCleanup_CallsCleanupThenInsert(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, nil)

	err := svc.InsertEdgeWithCleanup("BelongsTo", "uid-a", "uid-b", 1, 3)
	if err != nil {
		t.Fatalf("InsertEdgeWithCleanup() error = %v", err)
	}

	// Should have 2 calls: cleanup query + insert query
	if len(mockDB.calls) != 2 {
		t.Errorf("Expected 2 calls (cleanup + insert), got %d", len(mockDB.calls))
	}
}

func TestInsertEdgeWithCleanup_ContinuesOnCleanupFailure(t *testing.T) {
	callCount := 0
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			callCount++
			// First call (cleanup) fails, second call (insert) succeeds
			if callCount == 1 {
				return nil, errors.New("cleanup failed")
			}
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, nil)

	err := svc.InsertEdgeWithCleanup("BelongsTo", "uid-a", "uid-b", 1, 3)
	if err != nil {
		t.Fatalf("InsertEdgeWithCleanup() should continue on cleanup failure, got error = %v", err)
	}

	// Should still have 2 calls despite cleanup failure
	if len(mockDB.calls) != 2 {
		t.Errorf("Expected 2 calls despite cleanup failure, got %d", len(mockDB.calls))
	}
}

// --- Tests for CleanupOldEdges ---

func TestCleanupOldEdges_NoCleanupNeeded(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			// Return empty result set (no existing edges)
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, nil)

	err := svc.CleanupOldEdges("BelongsTo", "uid-a", "uid-b", 3)
	if err != nil {
		t.Fatalf("CleanupOldEdges() error = %v", err)
	}
}

// --- Tests for CleanupOutgoingEdgesByType ---

func TestCleanupOutgoingEdgesByType_EmptyResult(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, nil)

	// Should not panic or error on empty result
	svc.CleanupOutgoingEdgesByType("uid-a", "BelongsTo")
}

func TestCleanupOutgoingEdgesByType_ErrorOnQuery(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, errors.New("query failed")
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, nil)

	// Should not panic, just log error and return
	svc.CleanupOutgoingEdgesByType("uid-a", "BelongsTo")
}

// --- Tests for CleanupAllOutgoingEdges ---

func TestCleanupAllOutgoingEdges_EmptyResult(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, nil)

	// Should not panic or error on empty result
	svc.CleanupAllOutgoingEdges("uid-a")
}

func TestCleanupAllOutgoingEdges_ExcludesBelongsToApp(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeFn: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, nil)

	svc.CleanupAllOutgoingEdgesExcept("uid-a", "BelongsToApp")
}

// --- Tests for executenGQL ---

func TestExecutenGQL_ReturnsRows(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, nil)

	rows, err := svc.executenGQL("MATCH (v:K8sResource) RETURN v")
	if err != nil {
		t.Fatalf("executenGQL() error = %v", err)
	}
	if rows != nil {
		t.Error("Expected nil rows when ResultSet is nil")
	}
}

func TestExecutenGQL_PropagatesError(t *testing.T) {
	expectedErr := errors.New("query failed")
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, expectedErr
		},
	}
	svc := newTestK8sServiceWithMocks(mockDB, nil)

	_, err := svc.executenGQL("INVALID QUERY")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}

// --- Helper function ---

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
