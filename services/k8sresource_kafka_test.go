package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"gitee.com/tddh/mutong/config"
	nebula "github.com/vesoft-inc/nebula-go/v3"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models"
)

// --- Tests for Kafka retry and dead letter queue behavior ---

func TestProcessKafkaMessage_UpdateVersionMismatch(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			return []byte("old-version"), nil
		},
	}
	svc := newTestCollectorService(mockDB, mockCache, nil)

	obj := makeTestUnstructured("Pod", "test-pod", "default", "test-uid-update")
	obj.SetResourceVersion("new-version")

	result := svc.processKafkaMessage(obj, "Updated", "core", "test-cluster")
	if !result {
		t.Error("Expected processKafkaMessage to return true when version mismatch")
	}

	if len(mockDB.calls) != 0 {
		t.Errorf("Expected no Nebula calls for version mismatch, got %d", len(mockDB.calls))
	}
}

func TestProcessKafkaMessage_UpdateVersionMatch(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			return []byte("v123"), nil
		},
	}
	svc := newTestCollectorService(mockDB, mockCache, nil)

	obj := makeTestUnstructured("Pod", "test-pod", "default", "test-uid-match")
	obj.SetResourceVersion("v123")

	result := svc.processKafkaMessage(obj, "Updated", "core", "test-cluster")
	if !result {
		t.Error("Expected processKafkaMessage to return true for matching version")
	}

	if len(mockDB.calls) < 1 {
		t.Error("Expected Nebula call for matching version")
	}
}

// --- Tests for seedToKafka error handling ---

func TestSeedToKafka_PublishError(t *testing.T) {
	expectedErr := errors.New("kafka publish failed")
	mockMQ := &mockMessageQueue{
		publishFn: func(ctx context.Context, topic string, key string, message []byte) error {
			return expectedErr
		},
	}
	svc := &K8sResoureService{
		logger:       &mockNebulaLogger{},
		messageQueue: mockMQ,
		kafkaTopic:   "k8s_resources",
		cfg:          &config.Config{Kafka: config.Kafka{PublishTimeoutSec: 60}},
	}

	svc.seedToKafka("test-key", []byte(`{}`), "core", "Added", "test-cluster")

	if len(mockMQ.publishCalls) != 1 {
		t.Errorf("Expected 1 publish attempt, got %d", len(mockMQ.publishCalls))
	}
}

func TestSeedToKafka_ValidMessage(t *testing.T) {
	mockMQ := &mockMessageQueue{}
	svc := &K8sResoureService{
		logger:       &mockNebulaLogger{},
		messageQueue: mockMQ,
		kafkaTopic:   "k8s_resources",
		cfg:          &config.Config{Kafka: config.Kafka{PublishTimeoutSec: 60}},
	}

	svc.seedToKafka("test-key", []byte(`{"kind":"Pod"}`), "core", "Added", "test-cluster")

	if len(mockMQ.publishCalls) != 1 {
		t.Errorf("Expected 1 publish call, got %d", len(mockMQ.publishCalls))
	}

	call := mockMQ.publishCalls[0]
	var msg models.KafkaResourceMessage
	if err := json.Unmarshal(call.message, &msg); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}
	if msg.EventType != "Added" {
		t.Errorf("Expected event type 'Added', got %q", msg.EventType)
	}
}

// --- Tests for Kafka message marshaling ---

func TestKafkaResourceMessage_MarshalUnmarshal(t *testing.T) {
	obj := makeTestUnstructured("Pod", "test-pod", "default", "test-uid-msg")
	objJSON, _ := json.Marshal(obj.Object)

	msg := models.KafkaResourceMessage{
		EventType: "Added",
		Group:     "core",
		Object:    objJSON,
	}

	jsonBytes, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded models.KafkaResourceMessage
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.EventType != "Added" {
		t.Errorf("Expected event type 'Added', got %q", decoded.EventType)
	}
	if decoded.Group != "core" {
		t.Errorf("Expected group 'core', got %q", decoded.Group)
	}
}

// --- Tests for ConsumeKafkaMessagesWithContext with closed client ---

func TestConsumeKafkaMessagesWithContext_ClosedClient(t *testing.T) {
	mockMQ := &mockMessageQueue{
		pollFetchesFn: func(ctx context.Context) interfaces.FetchResult {
			return &mockFetchResult{closed: true}
		},
	}
	svc := newTestCollectorService(nil, nil, mockMQ)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan bool)
	go func() {
		svc.ConsumeKafkaMessagesWithContext(ctx)
		done <- true
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Consumer did not stop when client closed")
	}
}

// --- Tests for ConsumeKafkaMessagesWithContext with fetch errors ---

func TestConsumeKafkaMessagesWithContext_FetchErrors(t *testing.T) {
	callCount := 0

	mockMQ := &mockMessageQueue{
		pollFetchesFn: func(ctx context.Context) interfaces.FetchResult {
			callCount++
			if callCount > 2 {
				return &mockFetchResult{closed: true}
			}
			return &mockFetchResult{
				errorRecords: []mockFetchError{
					{topic: "k8s_resources", partition: 0, err: errors.New("fetch error")},
				},
			}
		},
	}
	svc := newTestCollectorService(nil, nil, mockMQ)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	done := make(chan bool)
	go func() {
		svc.ConsumeKafkaMessagesWithContext(ctx)
		done <- true
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Consumer did not stop within timeout")
	}
}

// --- Tests for ConsumeKafkaMessagesWithContext DLQ behavior ---

func TestConsumeKafkaMessagesWithContext_DLQOnMaxRetries(t *testing.T) {
	invalidMsg := &interfaces.Message{
		Key:   []byte("invalid-key"),
		Value: []byte(`{invalid json}`),
		Topic: "k8s_resources",
	}

	dlqCalled := false
	var mu sync.Mutex

	mockMQ := &mockMessageQueue{
		pollFetchesFn: func(ctx context.Context) interfaces.FetchResult {
			return &mockFetchResult{
				records: []*interfaces.Message{invalidMsg},
				recordFn: func(fn func(msg *interfaces.Message)) {
					fn(invalidMsg)
				},
			}
		},
		publishDeadLetter: func(ctx context.Context, originalMsg *interfaces.Message, errMsg string) error {
			mu.Lock()
			dlqCalled = true
			mu.Unlock()
			return nil
		},
		markCommitFn: func(m *interfaces.Message) {},
	}

	svc := &K8sResoureService{
		logger:       &mockNebulaLogger{},
		messageQueue: mockMQ,
		kafkaTopic:   "k8s_resources",
		cfg:          &config.Config{Kafka: config.Kafka{WorkerPoolSize: 10, TaskChanBuffer: 1000}},
		apiResources: make(map[string]bool),
		kafkaSem:     make(chan struct{}, 500),
		maxRetries:   2,
		retryBackoff: 5 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	done := make(chan bool)
	go func() {
		svc.ConsumeKafkaMessagesWithContext(ctx)
		done <- true
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Consumer did not stop within timeout")
	}

	if !dlqCalled {
		t.Error("Expected dead letter queue to be called after max retries")
	}
}

// --- Tests for ConsumeKafkaMessagesWithContext with DLQ failure ---

func TestConsumeKafkaMessagesWithContext_DLQFailure(t *testing.T) {
	invalidMsg := &interfaces.Message{
		Key:   []byte("invalid-key"),
		Value: []byte(`{invalid json}`),
		Topic: "k8s_resources",
	}

	dlqCalled := false
	markCommitCalled := false
	var mu sync.Mutex

	mockMQ := &mockMessageQueue{
		pollFetchesFn: func(ctx context.Context) interfaces.FetchResult {
			return &mockFetchResult{
				records: []*interfaces.Message{invalidMsg},
				recordFn: func(fn func(msg *interfaces.Message)) {
					fn(invalidMsg)
				},
			}
		},
		publishDeadLetter: func(ctx context.Context, originalMsg *interfaces.Message, errMsg string) error {
			mu.Lock()
			dlqCalled = true
			mu.Unlock()
			return errors.New("DLQ failed")
		},
		markCommitFn: func(m *interfaces.Message) {
			mu.Lock()
			markCommitCalled = true
			mu.Unlock()
		},
	}

	svc := &K8sResoureService{
		logger:       &mockNebulaLogger{},
		messageQueue: mockMQ,
		kafkaTopic:   "k8s_resources",
		cfg:          &config.Config{Kafka: config.Kafka{WorkerPoolSize: 10, TaskChanBuffer: 1000}},
		apiResources: make(map[string]bool),
		kafkaSem:     make(chan struct{}, 500),
		maxRetries:   2,
		retryBackoff: 5 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	done := make(chan bool)
	go func() {
		svc.ConsumeKafkaMessagesWithContext(ctx)
		done <- true
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Consumer did not stop within timeout")
	}

	if !dlqCalled {
		t.Error("Expected dead letter queue to be called")
	}

	if markCommitCalled {
		t.Error("Expected MarkCommit to NOT be called when DLQ fails")
	}
}

// --- Tests for business workload topic publishing from processKafkaMessage ---

func TestProcessKafkaMessage_AddedWorkloadPublishesAfterSuccess(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockBP := &mockBusinessWorkloadProducer{}
	svc := newTestCollectorServiceWithBusinessProducer(
		mockDB, nil, nil, mockBP, "biz-workloads",
		map[string]bool{"Deployment": true, "Pod": true, "StatefulSet": true, "DaemonSet": true},
	)

	obj := makeTestUnstructured("Deployment", "test-deploy", "default", "test-uid-added")

	result := svc.processKafkaMessage(obj, "Added", "apps", "test-cluster")
	if !result {
		t.Fatal("Expected processKafkaMessage to return true")
	}

	// Drain async publish channel for synchronous test assertion
	for len(svc.bizPublishChan) > 0 {
		task := <-svc.bizPublishChan
		svc.publishToBusinessWorkloadTopic(task.jsonBytes, task.uid, task.kind, task.name, task.eventType, task.group)
	}

	if len(mockBP.produceSyncCalls) != 1 {
		t.Fatalf("Expected 1 business topic publish call, got %d", len(mockBP.produceSyncCalls))
	}
	call := mockBP.produceSyncCalls[0]
	if call.topic != "biz-workloads" {
		t.Errorf("Expected topic 'biz-workloads', got %q", call.topic)
	}
	if call.key != "test-uid-added" {
		t.Errorf("Expected key 'test-uid-added', got %q", call.key)
	}

	var msg models.KafkaResourceMessage
	if err := json.Unmarshal(call.value, &msg); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}
	if msg.EventType != "Added" {
		t.Errorf("Expected event type 'Added', got %q", msg.EventType)
	}
}

func TestProcessKafkaMessage_DeletedWorkloadPublishesAfterSuccess(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockBP := &mockBusinessWorkloadProducer{}
	svc := newTestCollectorServiceWithBusinessProducer(
		mockDB, nil, nil, mockBP, "biz-workloads",
		map[string]bool{"Deployment": true, "Pod": true, "StatefulSet": true, "DaemonSet": true},
	)

	obj := makeTestUnstructured("Pod", "test-pod", "default", "test-uid-deleted")

	result := svc.processKafkaMessage(obj, "Deleted", "core", "test-cluster")
	if !result {
		t.Fatal("Expected processKafkaMessage to return true")
	}

	// Drain async publish channel for synchronous test assertion
	for len(svc.bizPublishChan) > 0 {
		task := <-svc.bizPublishChan
		svc.publishToBusinessWorkloadTopic(task.jsonBytes, task.uid, task.kind, task.name, task.eventType, task.group)
	}

	if len(mockBP.produceSyncCalls) != 1 {
		t.Fatalf("Expected 1 business topic publish call, got %d", len(mockBP.produceSyncCalls))
	}
	call := mockBP.produceSyncCalls[0]
	if call.key != "test-uid-deleted" {
		t.Errorf("Expected key 'test-uid-deleted', got %q", call.key)
	}

	var msg models.KafkaResourceMessage
	if err := json.Unmarshal(call.value, &msg); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}
	if msg.EventType != "Deleted" {
		t.Errorf("Expected event type 'Deleted', got %q", msg.EventType)
	}
}

func TestProcessKafkaMessage_UpdateVersionMismatchDoesNotPublish(t *testing.T) {
	mockDB := &mockNebulaGraphDB{}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			return []byte("old-version"), nil
		},
	}
	mockBP := &mockBusinessWorkloadProducer{}
	svc := newTestCollectorServiceWithBusinessProducer(
		mockDB, mockCache, nil, mockBP, "biz-workloads",
		map[string]bool{"Deployment": true},
	)

	obj := makeTestUnstructured("Deployment", "test-deploy", "default", "test-uid-mismatch")
	obj.SetResourceVersion("new-version")

	result := svc.processKafkaMessage(obj, "Updated", "apps", "test-cluster")
	if !result {
		t.Fatal("Expected processKafkaMessage to return true on version mismatch")
	}

	if len(mockBP.produceSyncCalls) != 0 {
		t.Errorf("Expected no business topic publish on version mismatch, got %d calls", len(mockBP.produceSyncCalls))
	}
}

func TestProcessKafkaMessage_PersistenceFailureDoesNotPublish(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, fmt.Errorf("nebula write failed")
		},
	}
	mockBP := &mockBusinessWorkloadProducer{}
	svc := newTestCollectorServiceWithBusinessProducer(
		mockDB, nil, nil, mockBP, "biz-workloads",
		map[string]bool{"Pod": true},
	)

	obj := makeTestUnstructured("Pod", "test-pod", "default", "test-uid-fail")

	result := svc.processKafkaMessage(obj, "Added", "core", "test-cluster")
	if result {
		t.Fatal("Expected processKafkaMessage to return false on persistence failure")
	}

	if len(mockBP.produceSyncCalls) != 0 {
		t.Errorf("Expected no business topic publish on persistence failure, got %d calls", len(mockBP.produceSyncCalls))
	}
}

func TestProcessKafkaMessage_NonWorkloadKindDoesNotPublish(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockBP := &mockBusinessWorkloadProducer{}
	svc := newTestCollectorServiceWithBusinessProducer(
		mockDB, nil, nil, mockBP, "biz-workloads",
		map[string]bool{"Deployment": true},
	)

	obj := makeTestUnstructured("ConfigMap", "test-cm", "default", "test-uid-cm")

	result := svc.processKafkaMessage(obj, "Added", "core", "test-cluster")
	if !result {
		t.Fatal("Expected processKafkaMessage to return true")
	}

	if len(mockBP.produceSyncCalls) != 0 {
		t.Errorf("Expected no business topic publish for non-workload kind, got %d calls", len(mockBP.produceSyncCalls))
	}
}

func TestProcessKafkaMessage_NilProducerDoesNotPublish(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestCollectorService(mockDB, nil, nil)
	svc.businessWorkloadTopic = "biz-workloads"
	svc.workloadKinds = map[string]bool{"Pod": true}

	obj := makeTestUnstructured("Pod", "test-pod", "default", "test-uid-nil")

	result := svc.processKafkaMessage(obj, "Added", "core", "test-cluster")
	if !result {
		t.Fatal("Expected processKafkaMessage to return true")
	}
}
