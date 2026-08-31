package services

import (
	"context"
	"sync"
	"testing"

	nebula "github.com/vesoft-inc/nebula-go/v3"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	v1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"gitee.com/tddh/mutong/config"
	"gitee.com/tddh/mutong/interfaces"
)

type traceLogger struct{}

func (traceLogger) Debug(string, ...zap.Field) {}
func (traceLogger) Info(string, ...zap.Field)  {}
func (traceLogger) Warn(string, ...zap.Field)  {}
func (traceLogger) Error(string, ...zap.Field) {}

type traceGraphDB struct {
	mu     sync.Mutex
	callFn func(string) (*nebula.ResultSet, error)
	calls  []string
}

func (m *traceGraphDB) Execute(query string) (*nebula.ResultSet, error) {
	m.mu.Lock()
	m.calls = append(m.calls, query)
	m.mu.Unlock()
	if m.callFn != nil {
		return m.callFn(query)
	}
	return nil, nil
}

func (m *traceGraphDB) ExecuteAndCheck(query string) (*nebula.ResultSet, error) {
	return m.Execute(query)
}

type traceEmptyMQ struct{}

func (traceEmptyMQ) Publish(context.Context, string, string, []byte) error { return nil }
func (traceEmptyMQ) PollFetches(context.Context) interfaces.FetchResult    { return nil }
func (traceEmptyMQ) MarkCommit(*interfaces.Message)                        {}
func (traceEmptyMQ) PublishDeadLetter(context.Context, *interfaces.Message, string) error {
	return nil
}

func newTraceSyncer(db interfaces.GraphDB, clusterName string, knownServices ...map[string]config.KnownServiceEntry) *TraceTopologySyncer {
	var ks map[string]config.KnownServiceEntry
	if len(knownServices) > 0 {
		ks = knownServices[0]
	}
	s, err := NewTraceTopologySyncer(&traceLogger{}, db, &traceEmptyMQ{}, clusterName, ks)
	if err != nil {
		panic(err)
	}
	return s
}

func sa(key, value string) *commonv1.KeyValue {
	return &commonv1.KeyValue{
		Key:   key,
		Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}},
	}
}

func tpRes(attrs []*commonv1.KeyValue) *v1.ResourceSpans {
	return &v1.ResourceSpans{Resource: &resourcev1.Resource{Attributes: attrs}}
}

func tpSpan(attrs []*commonv1.KeyValue, k v1.Span_SpanKind) *v1.Span {
	return &v1.Span{Attributes: attrs, Kind: k}
}

func tpMarshal(rs *v1.ResourceSpans) []byte {
	td := &v1.TracesData{ResourceSpans: []*v1.ResourceSpans{rs}}
	b, err := proto.Marshal(td)
	if err != nil {
		panic(err)
	}
	return b
}

func assertEq(t *testing.T, want, got, msg string) {
	t.Helper()
	if want != got {
		t.Errorf("%s: want %q, got %q", msg, want, got)
	}
}

// --- parsePeerServiceName tests

func TestParsePeerServiceName_Short(t *testing.T) {
	s := newTraceSyncer(nil, "")
	app, ns := s.resolvePeerFromAddress("kafka")
	assertEq(t, "kafka", app, "appName")
	assertEq(t, "", ns, "namespace")
}

func TestParsePeerServiceName_FQDN(t *testing.T) {
	s := newTraceSyncer(nil, "")
	app, ns := s.resolvePeerFromAddress("kafka.messaging.svc.cluster.local")
	assertEq(t, "kafka", app, "appName")
	assertEq(t, "messaging", ns, "namespace")
}

func TestParsePeerServiceName_FQDNOtherNS(t *testing.T) {
	s := newTraceSyncer(nil, "")
	app, ns := s.resolvePeerFromAddress("redis.prod.svc.cluster.local")
	assertEq(t, "redis", app, "appName")
	assertEq(t, "prod", ns, "namespace")
}

func TestParsePeerServiceName_IPPort(t *testing.T) {
	s := newTraceSyncer(nil, "")
	app, ns := s.resolvePeerFromAddress("10.0.0.1:9092")
	assertEq(t, "", app, "appName")
	assertEq(t, "", ns, "namespace")
}

func TestParsePeerServiceName_PureIP(t *testing.T) {
	s := newTraceSyncer(nil, "")
	app, ns := s.resolvePeerFromAddress("10.0.0.1")
	assertEq(t, "", app, "appName")
	assertEq(t, "", ns, "namespace")
}

func TestParsePeerServiceName_WithPort(t *testing.T) {
	s := newTraceSyncer(nil, "")
	app, ns := s.resolvePeerFromAddress("kafka:9092")
	assertEq(t, "kafka", app, "appName")
	assertEq(t, "", ns, "namespace")
}

func TestParsePeerServiceName_FQDNWithPort(t *testing.T) {
	s := newTraceSyncer(nil, "")
	app, ns := s.resolvePeerFromAddress("kafka.messaging.svc.cluster.local:9092")
	assertEq(t, "kafka", app, "appName")
	assertEq(t, "messaging", ns, "namespace")
}

func TestParsePeerServiceName_Empty(t *testing.T) {
	s := newTraceSyncer(nil, "")
	app, ns := s.resolvePeerFromAddress("")
	if app != "" || ns != "" {
		t.Errorf("want empty, got app=%q ns=%q", app, ns)
	}
}

func TestParsePeerServiceName_TwoSeg(t *testing.T) {
	s := newTraceSyncer(nil, "")
	app, ns := s.resolvePeerFromAddress("mysql.staging")
	assertEq(t, "mysql", app, "appName")
	assertEq(t, "staging", ns, "namespace")
}

func TestResolvePeer_FQDNKeepsParsedNS(t *testing.T) {
	s := newTraceSyncer(newDB(func(q string) (*nebula.ResultSet, error) {
		return nil, nil
	}), "")

	sp := tpSpan([]*commonv1.KeyValue{
		sa("peer.service", "kafka.messaging.svc.cluster.local"),
	}, v1.Span_SPAN_KIND_CLIENT)

	r, ok := s.resolvePeerBusinessApp(sp, map[string]string{})
	if !ok {
		t.Fatal("ok=false")
	}
	assertEq(t, "messaging", r.namespace, "namespace")
}

func TestResolvePeer_ServerAddressFallback(t *testing.T) {
	s := newTraceSyncer(newDB(func(q string) (*nebula.ResultSet, error) {
		return nil, nil
	}), "")

	sp := tpSpan([]*commonv1.KeyValue{sa("server.address", "kafka")}, v1.Span_SPAN_KIND_CLIENT)
	r, ok := s.resolvePeerBusinessApp(sp, map[string]string{})
	if !ok {
		t.Fatal("ok=false")
	}
	assertEq(t, "kafka", r.appName, "appName from server.address")
}

func TestResolvePeer_NeitherPeerNorServer(t *testing.T) {
	s := newTraceSyncer(nil, "")
	sp := tpSpan([]*commonv1.KeyValue{sa("other.attr", "val")}, v1.Span_SPAN_KIND_CLIENT)
	_, ok := s.resolvePeerBusinessApp(sp, map[string]string{})
	if ok {
		t.Error("want fail with neither peer.service nor server.address")
	}
}

// --- resolvePeerBusinessApp tests

func newDB(callFn func(string) (*nebula.ResultSet, error)) *traceGraphDB {
	return &traceGraphDB{callFn: callFn}
}

func TestResolvePeer_PrefersPeerService(t *testing.T) {
	s := newTraceSyncer(newDB(func(q string) (*nebula.ResultSet, error) {
		return nil, nil
	}), "")

	sp := tpSpan([]*commonv1.KeyValue{
		sa("peer.service", "kafka"),
		sa("server.address", "redis"),
	}, v1.Span_SPAN_KIND_CLIENT)

	r, ok := s.resolvePeerBusinessApp(sp, map[string]string{})
	if !ok {
		t.Fatal("ok=false")
	}
	assertEq(t, "kafka", r.appName, "appName")
	if r.namespace != "" {
		t.Errorf("want empty ns for short peer in production, got %q", r.namespace)
	}
}

// --- buildTraceBusinessAppUID tests

func TestBuildUID_WithCluster(t *testing.T) {
	s := newTraceSyncer(nil, "prod-cluster-1")
	assertEq(t, "bizapp-prod-cluster-1-messaging-kafka", s.buildTraceBusinessAppUID("kafka", "messaging"), "with cluster")
}

func TestBuildUID_NoCluster(t *testing.T) {
	s := newTraceSyncer(nil, "")
	assertEq(t, "bizapp--messaging-kafka", s.buildTraceBusinessAppUID("kafka", "messaging"), "no cluster prefix")
}

func TestBuildUID_EmptyNS(t *testing.T) {
	s := newTraceSyncer(nil, "")
	assertEq(t, "bizapp---kafka", s.buildTraceBusinessAppUID("kafka", ""), "empty ns")
}

// --- resolveCallerBusinessApp tests

func TestResolveCaller_FromK8sOwner(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "")
	rs := tpRes([]*commonv1.KeyValue{
		sa("k8s.owner.name", "my-frontend"),
		sa("k8s.namespace.name", "web"),
	})
	r, ok := s.resolveCallerBusinessApp(rs)
	if !ok {
		t.Fatal("ok=false")
	}
	assertEq(t, "my-frontend", r.appName, "appName")
	assertEq(t, "web", r.namespace, "namespace")
}

func TestResolveCaller_FallbackService(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "")
	rs := tpRes([]*commonv1.KeyValue{
		sa("service.name", "fallback"),
		sa("k8s.namespace.name", "default"),
	})
	r, ok := s.resolveCallerBusinessApp(rs)
	if !ok {
		t.Fatal("ok=false")
	}
	assertEq(t, "fallback", r.appName, "appName")
}

func TestResolveCaller_NoApp(t *testing.T) {
	s := newTraceSyncer(nil, "")
	_, ok := s.resolveCallerBusinessApp(tpRes([]*commonv1.KeyValue{sa("x", "y")}))
	if ok {
		t.Error("want fail with no k8s/service attrs")
	}
}

// --- lookupOrInsertBusinessApp tests

func TestLookupCacheHit(t *testing.T) {
	s := newTraceSyncer(newDB(func(q string) (*nebula.ResultSet, error) {
		return nil, nil
	}), "c")
	u1, ok1 := s.lookupOrInsertBusinessApp("redis", "ns")
	u2, ok2 := s.lookupOrInsertBusinessApp("redis", "ns")
	if !ok1 || !ok2 {
		t.Fatalf("ok1=%v ok2=%v", ok1, ok2)
	}
	if u1 != u2 {
		t.Errorf("cache mismatch: %q vs %q", u1, u2)
	}
}

func TestLookup_QueryContainsParams(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "")
	s.lookupOrInsertBusinessApp("kafka", "messaging")
	if len(db.calls) < 1 {
		t.Fatal("no calls")
	}
	q := db.calls[0]
	if !sc(q, "kafka") {
		t.Errorf("query missing kafka: %q", q)
	}
	if !sc(q, "messaging") {
		t.Errorf("query missing messaging: %q", q)
	}
}

func TestLookup_EmptyNS_ExcludedNamespaceFromQuery(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "")
	s.lookupOrInsertBusinessApp("kafka", "")
	if len(db.calls) < 1 {
		t.Fatal("no calls")
	}
	q := db.calls[0]
	if sc(q, "namespace ==") {
		t.Errorf("empty ns query should not contain namespace condition: %q", q)
	}
}

func TestLookup_NONEmptyNS_QueryHasNamespace(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "")
	s.lookupOrInsertBusinessApp("redis", "cache")
	if len(db.calls) < 1 {
		t.Fatal("no calls")
	}
	q := db.calls[0]
	if !sc(q, "app_name ==") || !sc(q, "namespace ==") {
		t.Errorf("non-empty ns query should have app+ns: %q", q)
	}
}

func TestLookup_EmptyNS_QueryHasNoLIMIT(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "")
	s.lookupOrInsertBusinessApp("kafka", "")
	q := db.calls[0]
	if sc(q, "LIMIT") {
		t.Errorf("empty ns query should not have LIMIT (ambiguous-detection): %q", q)
	}
}

func TestLookup_UIDWithCluster(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "prod")
	u, ok := s.lookupOrInsertBusinessApp("kafka", "messaging")
	if !ok {
		t.Fatal("ok=false")
	}
	assertEq(t, "bizapp-prod-messaging-kafka", u, "uid")
}

func TestLookup_EmptyNS_AutoCreateEmptyNS(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "cl")
	u, ok := s.lookupOrInsertBusinessApp("kafka", "")
	if !ok {
		t.Fatal("ok=false")
	}
	assertEq(t, "bizapp-cl--kafka", u, "uid with empty ns")
}

// --- processSpanRecord tests

func TestProcessSpan_SamePodSelfCallFiltered(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "")
	rs := tpRes([]*commonv1.KeyValue{
		sa("k8s.owner.name", "kafka"),
		sa("k8s.namespace.name", "messaging"),
		sa("k8s.pod.name", "kafka-0"),
	})
	rs.ScopeSpans = []*v1.ScopeSpans{{Spans: []*v1.Span{
		tpSpan([]*commonv1.KeyValue{sa("peer.service", "kafka-0.messaging.svc.cluster.local")}, v1.Span_SPAN_KIND_CLIENT),
	}}}
	s.processSpanRecord(&interfaces.Message{Value: tpMarshal(rs)})
	s.bufferMu.Lock()
	n := len(s.edgeBuffer)
	s.bufferMu.Unlock()
	if n != 0 {
		t.Errorf("same-pod self-call should be filtered, got %d edges", n)
	}
}

func TestProcessSpan_CrossReplicaSameWorkloadKept(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "")
	rs := tpRes([]*commonv1.KeyValue{
		sa("k8s.owner.name", "kafka"),
		sa("k8s.namespace.name", "messaging"),
		sa("k8s.pod.name", "kafka-0"),
	})
	rs.ScopeSpans = []*v1.ScopeSpans{{Spans: []*v1.Span{
		tpSpan([]*commonv1.KeyValue{sa("peer.service", "kafka-1.messaging.svc.cluster.local")}, v1.Span_SPAN_KIND_CLIENT),
	}}}
	s.processSpanRecord(&interfaces.Message{Value: tpMarshal(rs)})
	s.bufferMu.Lock()
	n := len(s.edgeBuffer)
	s.bufferMu.Unlock()
	if n != 1 {
		t.Fatalf("cross-replica same-workload should keep self-loop edge, got %d", n)
	}
}

func TestProcessSpan_CrossCall(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "")
	rs := tpRes([]*commonv1.KeyValue{
		sa("k8s.owner.name", "frontend"),
		sa("k8s.namespace.name", "web"),
	})
	rs.ScopeSpans = []*v1.ScopeSpans{{Spans: []*v1.Span{
		tpSpan([]*commonv1.KeyValue{sa("peer.service", "kafka.messaging.svc.cluster.local")}, v1.Span_SPAN_KIND_CLIENT),
	}}}
	s.processSpanRecord(&interfaces.Message{Value: tpMarshal(rs)})
	s.bufferMu.Lock()
	n := len(s.edgeBuffer)
	s.bufferMu.Unlock()
	if n != 1 {
		t.Fatalf("want 1 edge, got %d", n)
	}
	if s.edgeBuffer[0].from == s.edgeBuffer[0].to {
		t.Error("caller/callee should differ")
	}
}

func TestProcessSpan_INTERNALSkipped(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "")
	rs := tpRes([]*commonv1.KeyValue{
		sa("k8s.owner.name", "frontend"),
		sa("k8s.namespace.name", "web"),
	})
	rs.ScopeSpans = []*v1.ScopeSpans{{Spans: []*v1.Span{
		tpSpan([]*commonv1.KeyValue{sa("peer.service", "redis.web.svc")}, v1.Span_SPAN_KIND_INTERNAL),
	}}}
	s.processSpanRecord(&interfaces.Message{Value: tpMarshal(rs)})
	s.bufferMu.Lock()
	n := len(s.edgeBuffer)
	s.bufferMu.Unlock()
	if n != 0 {
		t.Errorf("INTERNAL span should be skipped, got %d edges", n)
	}
}

// --- ambiguous (empty-namespace multi-match) detection via query analysis ---

func TestEmptyNSQuery_NoLIMIT_ForAmbiguityDetection(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "")
	s.lookupOrInsertBusinessApp("kafka", "")
	if len(db.calls) < 1 {
		t.Fatal("no calls")
	}
	q := db.calls[0]
	if !sc(q, "MATCH") {
		t.Errorf("should be MATCH: %q", q)
	}
	if !sc(q, "app_name ==") {
		t.Errorf("should filter by app_name: %q", q)
	}
	if sc(q, "LIMIT") {
		t.Errorf("empty ns query MUST NOT have LIMIT (to detect ambiguity): %q", q)
	}
	if sc(q, "namespace ==") {
		t.Errorf("empty ns should not add namespace filter: %q", q)
	}
}

func TestNonEmptyNSQuery_HasLIMIT(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "")
	s.lookupOrInsertBusinessApp("kafka", "default")
	if len(db.calls) < 1 {
		t.Fatal("no calls")
	}
	q := db.calls[0]
	if !sc(q, "LIMIT 1") {
		t.Errorf("non-empty ns query should have LIMIT 1: %q", q)
	}
	if !sc(q, "namespace ==") {
		t.Errorf("non-empty ns query should filter by namespace: %q", q)
	}
}

func sc(s, p string) bool { return len(s) >= len(p) && _sc(s, p) }
func _sc(s, p string) bool {
	for i := 0; i <= len(s)-len(p); i++ {
		if s[i:i+len(p)] == p {
			return true
		}
	}
	return false
}
