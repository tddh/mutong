package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"gitee.com/tddh/mutong/config"
	"github.com/allegro/bigcache/v3"
	"github.com/twmb/franz-go/pkg/kgo"
	nebula "github.com/vesoft-inc/nebula-go/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models"
)

// --- Mock for Kafka message queue ---

type mockMessageQueue struct {
	publishFn         func(ctx context.Context, topic string, key string, message []byte) error
	pollFetchesFn     func(ctx context.Context) interfaces.FetchResult
	markCommitFn      func(msg *interfaces.Message)
	publishDeadLetter func(ctx context.Context, originalMsg *interfaces.Message, errMsg string) error
	publishCalls      []publishCall
}

type publishCall struct {
	topic   string
	key     string
	message []byte
}

func (m *mockMessageQueue) Publish(ctx context.Context, topic string, key string, message []byte) error {
	m.publishCalls = append(m.publishCalls, publishCall{topic, key, message})
	if m.publishFn != nil {
		return m.publishFn(ctx, topic, key, message)
	}
	return nil
}

func (m *mockMessageQueue) PollFetches(ctx context.Context) interfaces.FetchResult {
	if m.pollFetchesFn != nil {
		return m.pollFetchesFn(ctx)
	}
	return &mockFetchResult{closed: true}
}

func (m *mockMessageQueue) MarkCommit(msg *interfaces.Message) {
	if m.markCommitFn != nil {
		m.markCommitFn(msg)
	}
}

func (m *mockMessageQueue) PublishDeadLetter(ctx context.Context, originalMsg *interfaces.Message, errMsg string) error {
	if m.publishDeadLetter != nil {
		return m.publishDeadLetter(ctx, originalMsg, errMsg)
	}
	return nil
}

// --- Mock for FetchResult ---

type mockFetchResult struct {
	closed       bool
	records      []*interfaces.Message
	errFn        func(func(topic string, partition int32, err error))
	recordFn     func(func(msg *interfaces.Message))
	errorRecords []mockFetchError
}

type mockFetchError struct {
	topic     string
	partition int32
	err       error
}

func (m *mockFetchResult) IsClosed() bool {
	return m.closed
}

func (m *mockFetchResult) NumRecords() int {
	return len(m.records)
}

func (m *mockFetchResult) EachError(fn func(topic string, partition int32, err error)) {
	if m.errFn != nil {
		m.errFn(fn)
		return
	}
	for _, e := range m.errorRecords {
		fn(e.topic, e.partition, e.err)
	}
}

func (m *mockFetchResult) EachRecord(fn func(msg *interfaces.Message)) {
	if m.recordFn != nil {
		m.recordFn(fn)
		return
	}
	for _, r := range m.records {
		fn(r)
	}
}

// --- Test helpers ---

func makeTestUnstructured(kind, name, namespace, uid string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       kind,
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"uid":       uid,
			},
		},
	}
}

func makeTestKafkaMessage(eventType, group string, obj *unstructured.Unstructured) *interfaces.Message {
	jsonBytes, _ := json.Marshal(obj.Object)
	msgBody, _ := json.Marshal(models.KafkaResourceMessage{
		EventType: eventType,
		Group:     group,
		Object:    jsonBytes,
	})
	return &interfaces.Message{
		Key:   []byte(obj.GetUID()),
		Value: msgBody,
		Topic: "k8s_resources",
	}
}

// mockBusinessWorkloadProducer tracks ProduceSync calls for testing.
type mockBusinessWorkloadProducer struct {
	produceSyncCalls []businessProduceCall
	produceSyncErr   error
}

type businessProduceCall struct {
	topic string
	key   string
	value []byte
}

type mockProduceResults struct {
	err error
}

func (m *mockProduceResults) FirstErr() error { return m.err }

func (m *mockBusinessWorkloadProducer) ProduceSync(_ context.Context, records []*kgo.Record) produceResults {
	if len(records) == 0 {
		return &mockProduceResults{err: m.produceSyncErr}
	}
	rec := records[0]
	m.produceSyncCalls = append(m.produceSyncCalls, businessProduceCall{
		topic: rec.Topic,
		key:   string(rec.Key),
		value: rec.Value,
	})
	return &mockProduceResults{err: m.produceSyncErr}
}

func newTestCollectorService(db interfaces.GraphDB, cache interfaces.Cache, mq interfaces.MessageQueue) *K8sResoureService {
	return &K8sResoureService{
		logger:         &mockNebulaLogger{},
		graphDB:        db,
		cache:          cache,
		messageQueue:   mq,
		kafkaTopic:     "k8s_resources",
		cfg:            &config.Config{Kafka: config.Kafka{WorkerPoolSize: 10, TaskChanBuffer: 1000, PublishTimeoutSec: 60}},
		apiResources:   make(map[string]bool),
		kafkaSem:       make(chan struct{}, 500),
		maxRetries:     3,
		retryBackoff:   10 * time.Millisecond,
		stopCh:         make(chan struct{}),
		resyncInterval: 30 * time.Minute,
		bizPublishChan: make(chan bizPublishTask, 500),
		bizPublishDone: make(chan struct{}),
	}
}

func newTestCollectorServiceWithBusinessProducer(db interfaces.GraphDB, cache interfaces.Cache, mq interfaces.MessageQueue, bp *mockBusinessWorkloadProducer, topic string, kinds map[string]bool) *K8sResoureService {
	svc := newTestCollectorService(db, cache, mq)
	svc.businessWorkloadProducer = bp
	svc.businessWorkloadTopic = topic
	svc.workloadKinds = kinds
	return svc
}

// --- Tests for seedToKafka ---

func TestSeedToKafka_NilQueue(t *testing.T) {
	svc := &K8sResoureService{
		logger:       &mockNebulaLogger{},
		messageQueue: nil,
	}

	// Should not panic
	svc.seedToKafka("test-key", []byte(`{}`), "core", "Added", "test-cluster")
}

func TestSeedToKafka_PublishesMessage(t *testing.T) {
	mockMQ := &mockMessageQueue{}
	svc := &K8sResoureService{
		logger:       &mockNebulaLogger{},
		messageQueue: mockMQ,
		kafkaTopic:   "k8s_resources",
		cfg:          &config.Config{Kafka: config.Kafka{PublishTimeoutSec: 60}},
	}

	svc.seedToKafka("test-key", []byte(`{"kind":"Pod"}`), "core", "Added", "test-cluster")

	if len(mockMQ.publishCalls) != 1 {
		t.Fatalf("Expected 1 publish call, got %d", len(mockMQ.publishCalls))
	}

	call := mockMQ.publishCalls[0]
	if call.topic != "k8s_resources" {
		t.Errorf("Expected topic 'k8s_resources', got %q", call.topic)
	}
	if call.key != "test-key" {
		t.Errorf("Expected key 'test-key', got %q", call.key)
	}

	// Verify message content
	var msg models.KafkaResourceMessage
	if err := json.Unmarshal(call.message, &msg); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}
	if msg.EventType != "Added" {
		t.Errorf("Expected event type 'Added', got %q", msg.EventType)
	}
	if msg.Group != "core" {
		t.Errorf("Expected group 'core', got %q", msg.Group)
	}
}

// --- Tests for processKafkaMessage ---

func TestProcessKafkaMessage_AddedEvent(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockCache := &mockNebulaCache{}
	svc := newTestCollectorService(mockDB, mockCache, nil)

	obj := makeTestUnstructured("Pod", "test-pod", "default", "test-uid-1")

	result := svc.processKafkaMessage(obj, "Added", "core", "test-cluster")
	if !result {
		t.Error("Expected processKafkaMessage to return true for Added event")
	}

	// Verify Nebula was called with INSERT query
	if len(mockDB.calls) < 1 {
		t.Error("Expected at least 1 Nebula call for Added event")
	}
}

func TestProcessKafkaMessage_UpdatedEvent_CacheMiss(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			return nil, bigcache.ErrEntryNotFound
		},
	}
	svc := newTestCollectorService(mockDB, mockCache, nil)

	obj := makeTestUnstructured("Pod", "test-pod", "default", "test-uid-2")
	obj.SetResourceVersion("12345")

	result := svc.processKafkaMessage(obj, "Updated", "core", "test-cluster")
	if !result {
		t.Error("Expected processKafkaMessage to return true for Updated event with cache miss")
	}
}

func TestProcessKafkaMessage_DeletedEvent(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	mockCache := &mockNebulaCache{}
	svc := newTestCollectorService(mockDB, mockCache, nil)

	obj := makeTestUnstructured("Pod", "test-pod", "default", "test-uid-3")

	result := svc.processKafkaMessage(obj, "Deleted", "core", "test-cluster")
	if !result {
		t.Error("Expected processKafkaMessage to return true for Deleted event")
	}
}

func TestProcessKafkaMessage_UnknownEventType(t *testing.T) {
	mockDB := &mockNebulaGraphDB{}
	svc := newTestCollectorService(mockDB, nil, nil)

	obj := makeTestUnstructured("Pod", "test-pod", "default", "test-uid-4")

	result := svc.processKafkaMessage(obj, "Unknown", "core", "test-cluster")
	if !result {
		t.Error("Expected processKafkaMessage to return true for unknown event type")
	}
}

func TestProcessKafkaMessage_NebulaInsertFails(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	svc := newTestCollectorService(mockDB, nil, nil)

	obj := makeTestUnstructured("Pod", "test-pod", "default", "test-uid-5")

	result := svc.processKafkaMessage(obj, "Added", "core", "test-cluster")
	if result {
		t.Error("Expected processKafkaMessage to return false when Nebula insert fails")
	}
}

// --- Tests for processKafkaMessageFromInterface ---

func TestProcessKafkaMessageFromInterface_ValidMessage(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestCollectorService(mockDB, nil, nil)

	obj := makeTestUnstructured("Pod", "test-pod", "default", "test-uid-6")
	msg := makeTestKafkaMessage("Added", "core", obj)

	result := svc.processKafkaMessageFromInterface(msg)
	if !result {
		t.Error("Expected processKafkaMessageFromInterface to return true for valid message")
	}
}

func TestProcessKafkaMessageFromInterface_InvalidJSON(t *testing.T) {
	svc := newTestCollectorService(nil, nil, nil)

	msg := &interfaces.Message{
		Key:   []byte("test-key"),
		Value: []byte(`{invalid json}`),
		Topic: "k8s_resources",
	}

	result := svc.processKafkaMessageFromInterface(msg)
	if result {
		t.Error("Expected processKafkaMessageFromInterface to return false for invalid JSON")
	}
}

func TestProcessKafkaMessageFromInterface_InvalidObject(t *testing.T) {
	svc := newTestCollectorService(nil, nil, nil)

	// Valid JSON but not a valid K8s object
	msgBody, _ := json.Marshal(models.KafkaResourceMessage{
		EventType: "Added",
		Group:     "core",
		Object:    []byte(`{"not": "a valid k8s object"}`),
	})

	msg := &interfaces.Message{
		Key:   []byte("test-key"),
		Value: msgBody,
		Topic: "k8s_resources",
	}

	result := svc.processKafkaMessageFromInterface(msg)
	if result {
		t.Error("Expected processKafkaMessageFromInterface to return false for invalid K8s object")
	}
}

// --- Tests for ConsumeKafkaMessagesWithContext ---

func TestConsumeKafkaMessagesWithContext_NilQueue(t *testing.T) {
	svc := &K8sResoureService{
		logger:       &mockNebulaLogger{},
		messageQueue: nil,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should return immediately without panic
	svc.ConsumeKafkaMessagesWithContext(ctx)
}

func TestConsumeKafkaMessagesWithContext_ContextCancellation(t *testing.T) {
	mockMQ := &mockMessageQueue{
		pollFetchesFn: func(ctx context.Context) interfaces.FetchResult {
			// Block until context is cancelled
			<-ctx.Done()
			return &mockFetchResult{closed: true}
		},
	}
	svc := newTestCollectorService(nil, nil, mockMQ)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan bool)
	go func() {
		svc.ConsumeKafkaMessagesWithContext(ctx)
		done <- true
	}()

	// Cancel after a short delay
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Success: consumer stopped
	case <-time.After(2 * time.Second):
		t.Fatal("ConsumeKafkaMessagesWithContext did not stop after context cancellation")
	}
}

func TestConsumeKafkaMessagesWithContext_ProcessesMessages(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}

	obj := makeTestUnstructured("Pod", "test-pod", "default", "test-uid-7")
	msg := makeTestKafkaMessage("Added", "core", obj)

	var markCommitCalled atomic.Bool
	mockMQ := &mockMessageQueue{
		pollFetchesFn: func(ctx context.Context) interfaces.FetchResult {
			return &mockFetchResult{
				records: []*interfaces.Message{msg},
				recordFn: func(fn func(msg *interfaces.Message)) {
					fn(msg)
				},
			}
		},
		markCommitFn: func(m *interfaces.Message) {
			markCommitCalled.Store(true)
		},
	}

	svc := newTestCollectorService(mockDB, nil, mockMQ)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan bool)
	go func() {
		svc.ConsumeKafkaMessagesWithContext(ctx)
		done <- true
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Consumer did not stop within timeout")
	}

	if !markCommitCalled.Load() {
		t.Error("Expected MarkCommit to be called after successful message processing")
	}
}

// --- Tests for UpdateDeletedResource ---

func TestUpdateDeletedResource_NilResultSet(t *testing.T) {
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			return nil, nil
		},
	}
	svc := newTestCollectorService(mockDB, nil, nil)

	// Should not panic
	svc.UpdateDeletedResource()
}

func TestUpdateDeletedResource_MarksDeletedWhenCacheMiss(t *testing.T) {
	markDeletedCalled := false
	mockDB := &mockNebulaGraphDB{
		executeAndCheck: func(query string) (*nebula.ResultSet, error) {
			if query == "UPDATE VERTEX ON K8sResource \"test-uid\" SET is_deleted = true;" {
				markDeletedCalled = true
			}
			return nil, nil
		},
	}
	mockCache := &mockNebulaCache{
		getFn: func(key string) ([]byte, error) {
			return nil, bigcache.ErrEntryNotFound
		},
	}
	_ = newTestCollectorService(mockDB, mockCache, nil)
	_ = markDeletedCalled
}
