package services

import (
	"context"

	"gitee.com/tddh/mutong/interfaces"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	mtr "go.opentelemetry.io/proto/otlp/metrics/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

type FlowTopologySyncer struct {
	logger       interfaces.Logger
	messageQueue interfaces.MessageQueue
	resolver     *TraceTopologySyncer
	stopCh       chan struct{}
}

var flowMetricNames = map[string]bool{
	"beyla.network.flow.bytes":       true,
	"beyla_network_flow_bytes_total": true,
}

func NewFlowTopologySyncer(
	logger interfaces.Logger,
	messageQueue interfaces.MessageQueue,
	resolver *TraceTopologySyncer,
) *FlowTopologySyncer {
	return &FlowTopologySyncer{
		logger:       logger,
		messageQueue: messageQueue,
		resolver:     resolver,
		stopCh:       make(chan struct{}),
	}
}

func (s *FlowTopologySyncer) Start() {
	s.logger.Info("FlowTopologySyncer starting")
	go s.consumeLoop()
}

func (s *FlowTopologySyncer) Stop() {
	close(s.stopCh)
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

	s.resolver.upsertCallEdge(caller, callee)
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
