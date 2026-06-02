package services

import (
	"testing"

	"go.uber.org/zap"
)

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, fields ...zap.Field) {}
func (m *mockLogger) Info(msg string, fields ...zap.Field)  {}
func (m *mockLogger) Warn(msg string, fields ...zap.Field)  {}
func (m *mockLogger) Error(msg string, fields ...zap.Field) {}

func TestResourceCache_GetSet(t *testing.T) {
	logger := &mockLogger{}
	cache, err := NewResourceCache(logger, 10, 10, 100, true)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	testData := map[string]interface{}{
		"nodes": []map[string]interface{}{
			{"id": "test-uid", "label": "test-pod"},
		},
		"totalCount": 1,
	}

	err = cache.Set("test-key", testData)
	if err != nil {
		t.Fatalf("Failed to set cache: %v", err)
	}

	got, ok := cache.Get("test-key")
	if !ok {
		t.Fatal("Expected cache hit, got miss")
	}

	gotMap, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected map[string]interface{}, got %T", got)
	}

	if gotMap["totalCount"] != float64(1) {
		t.Errorf("Expected totalCount=1, got %v", gotMap["totalCount"])
	}
}

func TestResourceCache_GetNilCache(t *testing.T) {
	logger := &mockLogger{}
	cache := &ResourceCache{logger: logger}

	_, ok := cache.Get("test-key")
	if ok {
		t.Error("Expected cache miss for nil cache")
	}
}

func TestResourceCache_SetNilCache(t *testing.T) {
	logger := &mockLogger{}
	cache := &ResourceCache{logger: logger, tracked: make(map[string]bool)}

	err := cache.Set("test-key", "test-data")
	if err != nil {
		t.Errorf("Expected no error for nil cache set, got %v", err)
	}
}

func TestResourceCache_Delete(t *testing.T) {
	logger := &mockLogger{}
	cache, err := NewResourceCache(logger, 10, 10, 100, true)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	err = cache.Set("test-key", "test-data")
	if err != nil {
		t.Fatalf("Failed to set cache: %v", err)
	}

	err = cache.Delete("test-key")
	if err != nil {
		t.Fatalf("Failed to delete cache: %v", err)
	}

	_, ok := cache.Get("test-key")
	if ok {
		t.Error("Expected cache miss after delete")
	}
}

func TestResourceCache_ClearAll(t *testing.T) {
	logger := &mockLogger{}
	cache, err := NewResourceCache(logger, 10, 10, 100, true)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	cache.Set("key1", "data1")
	cache.Set("key2", "data2")
	cache.Set("key3", "data3")

	cache.ClearAll()

	if len(cache.tracked) != 0 {
		t.Errorf("Expected tracked to be empty, got %d items", len(cache.tracked))
	}

	_, ok := cache.Get("key1")
	if ok {
		t.Error("Expected key1 to be cleared")
	}
}

func TestResourceCache_Disabled(t *testing.T) {
	logger := &mockLogger{}
	cache, err := NewResourceCache(logger, 10, 10, 100, false)
	if err != nil {
		t.Fatalf("Expected no error for disabled cache, got %v", err)
	}

	if cache.cache != nil {
		t.Error("Expected cache to be nil when disabled")
	}

	err = cache.Set("test-key", "test-data")
	if err != nil {
		t.Errorf("Expected no error for disabled cache set, got %v", err)
	}

	_, ok := cache.Get("test-key")
	if ok {
		t.Error("Expected cache miss for disabled cache")
	}
}

func TestResourceCache_InvalidJSON(t *testing.T) {
	logger := &mockLogger{}
	cache, err := NewResourceCache(logger, 10, 10, 100, true)
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	chanData := make(chan int)
	err = cache.Set("chan-key", chanData)
	if err == nil {
		t.Error("Expected error for unmarshalable type, got nil")
	}
}
