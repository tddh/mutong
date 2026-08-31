package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"gitee.com/tddh/mutong/interfaces"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	mtr "go.opentelemetry.io/proto/otlp/metrics/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

type FlowTopologySyncer struct {
	logger       interfaces.Logger
	graphDB      interfaces.GraphDB
	messageQueue interfaces.MessageQueue
	// 复用 TraceTopologySyncer 的 owner_name 别名解析 + 写边能力
	resolver   *TraceTopologySyncer
	stopCh     chan struct{}
	edgeBuffer []pendingCallEdge
	bufferMu   sync.Mutex
}

var flowMetricNames = map[string]bool{
	"beyla.network.flow.bytes":       true,
	"beyla_network_flow_bytes_total": true,
}

func NewFlowTopologySyncer(
	logger interfaces.Logger,
	graphDB interfaces.GraphDB,
	messageQueue interfaces.MessageQueue,
	resolver *TraceTopologySyncer,
) *FlowTopologySyncer {
	return &FlowTopologySyncer{
		logger:       logger,
		graphDB:      graphDB,
		messageQueue: messageQueue,
		resolver:     resolver,
		stopCh:       make(chan struct{}),
	}
}

func (s *FlowTopologySyncer) Start() {
	s.logger.Info("FlowTopologySyncer starting")
	go s.consumeLoop()
	go s.flushLoop()
}

func (s *FlowTopologySyncer) Stop() {
	close(s.stopCh)
	s.flushBuffer()
	s.logger.Info("FlowTopologySyncer stopped")
}

func (s *FlowTopologySyncer) consumeLoop() {
	ctx := context.Background()
	for {
		select {
		case <-s.stopCh:
			return
		default:
		}
		fetches := s.messageQueue.PollFetches(ctx)
		if fetches.IsClosed() {
			s.logger.Warn("Flow Kafka client closed, stopping FlowTopologySyncer")
			return
		}
		fetches.EachRecord(func(msg *interfaces.Message) {
			if s.processMetricsRecord(msg) {
				s.messageQueue.MarkCommit(msg)
			}
		})
	}
}

func (s *FlowTopologySyncer) processMetricsRecord(msg *interfaces.Message) bool {
	data := &mtr.MetricsData{}
	if err := proto.Unmarshal(msg.Value, data); err != nil {
		s.logger.Error("flow: unmarshal MetricsData failed", zapError(err))
		return false
	}
	for _, rm := range data.GetResourceMetrics() {
		for _, sm := range rm.GetScopeMetrics() {
			for _, metric := range sm.GetMetrics() {
				if !flowMetricNames[metric.GetName()] {
					continue
				}
				for _, dp := range metric.GetSum().GetDataPoints() {
					s.handleFlowPoint(dp.GetAttributes())
				}
			}
		}
	}
	return true
}

func (s *FlowTopologySyncer) handleFlowPoint(attrs []*commonv1.KeyValue) {
	srcOwner := flowAttr(attrs, "k8s.src.owner.name", "source.workload.name")
	srcNs := flowAttr(attrs, "k8s.src.namespace", "source.namespace")
	dstOwner := flowAttr(attrs, "k8s.dst.owner.name", "destination.workload.name")
	dstNs := flowAttr(attrs, "k8s.dst.namespace", "destination.namespace")

	caller, ok1 := s.resolver.lookupByOwnerName(srcOwner, srcNs)
	callee, ok2 := s.resolver.lookupByOwnerName(dstOwner, dstNs)
	if !ok1 || !ok2 {
		return
	}

	s.bufferMu.Lock()
	s.edgeBuffer = append(s.edgeBuffer, pendingCallEdge{from: caller.uid, to: callee.uid, cacheKey: caller.uid + "->" + callee.uid})
	shouldFlush := len(s.edgeBuffer) >= callsFlushThreshold
	s.bufferMu.Unlock()

	if shouldFlush {
		go s.flushBuffer()
	}
}

func (s *FlowTopologySyncer) flushLoop() {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.flushBuffer()
		}
	}
}

func (s *FlowTopologySyncer) flushBuffer() {
	s.bufferMu.Lock()
	if len(s.edgeBuffer) == 0 {
		s.bufferMu.Unlock()
		return
	}
	batch := s.edgeBuffer
	s.edgeBuffer = nil
	s.bufferMu.Unlock()

	const batchSize = 200
	values := make([]string, 0, len(batch))
	for _, edge := range batch {
		values = append(values, fmt.Sprintf(
			"%s -> %s:()",
			strconv.Quote(edge.from),
			strconv.Quote(edge.to),
		))
		if len(values) >= batchSize {
			s.flushChunk(values)
			values = values[:0]
		}
	}
	if len(values) > 0 {
		s.flushChunk(values)
	}
}

func (s *FlowTopologySyncer) flushChunk(values []string) {
	query := fmt.Sprintf("INSERT EDGE CallsApp () VALUES %s;", strings.Join(values, ", "))
	if _, err := s.graphDB.ExecuteAndCheck(query); err != nil {
		s.logger.Error("flow: flush CallsApp edges failed", zapError(err))
	}
}

func flowAttr(attrs []*commonv1.KeyValue, keys ...string) string {
	for _, a := range attrs {
		for _, k := range keys {
			if a.GetKey() == k {
				return a.GetValue().GetStringValue()
			}
		}
	}
	return ""
}

func zapError(err error) zap.Field {
	return zap.Error(err)
}
