package services

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/tddh/mutong/config"
	"gitee.com/tddh/mutong/interfaces"
	"github.com/allegro/bigcache/v3"
	nebula "github.com/vesoft-inc/nebula-go/v3"
	v1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	traceCacheTTL       = 24 * time.Hour
	flushInterval       = 5 * time.Second
	flushThreshold      = 100
	callsFlushThreshold = 500
)

type TraceTopologySyncer struct {
	logger       interfaces.Logger
	graphDB      interfaces.GraphDB
	messageQueue interfaces.MessageQueue
	clusterName  string

	relationCache *bigcache.BigCache
	knownServices map[string]config.KnownServiceEntry
	edgeBuffer    []pendingCallEdge
	bufferMu      sync.Mutex
	stopCh        chan struct{}

	// edge drop counters (atomic, read via Metrics() or logs)
	droppedCallerMissing   int64
	droppedCalleeMissing   int64
	droppedSelfCall        int64
	droppedAmbiguousLookup int64
	droppedFlushFailed     int64
	edgesWritten           int64
}

type pendingCallEdge struct {
	from     string
	to       string
	cacheKey string
}

type businessAppRef struct {
	uid       string
	appName   string
	namespace string
}

func NewTraceTopologySyncer(
	logger interfaces.Logger,
	graphDB interfaces.GraphDB,
	messageQueue interfaces.MessageQueue,
	clusterName string,
	knownServices map[string]config.KnownServiceEntry,
) (*TraceTopologySyncer, error) {
	relationCache, err := bigcache.New(context.Background(), bigcache.Config{
		Shards:             16,
		LifeWindow:         traceCacheTTL,
		CleanWindow:        5 * time.Minute,
		MaxEntriesInWindow: 10000,
		MaxEntrySize:       128,
		HardMaxCacheSize:   16,
		Verbose:            false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create relation cache: %w", err)
	}

	return &TraceTopologySyncer{
		logger:        logger,
		graphDB:       graphDB,
		messageQueue:  messageQueue,
		clusterName:   clusterName,
		relationCache: relationCache,
		knownServices: knownServices,
		edgeBuffer:    make([]pendingCallEdge, 0, flushThreshold),
		stopCh:        make(chan struct{}),
	}, nil
}

func (s *TraceTopologySyncer) Start() {
	s.logger.Info("TraceTopologySyncer starting")

	go s.warmupBusinessAppCache()

	go s.consumeLoop()
	go s.flushLoop()
	go s.periodicCounterDump()
}

func (s *TraceTopologySyncer) periodicCounterDump() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			s.dumpCounters()
			return
		case <-ticker.C:
			s.dumpCounters()
		}
	}
}

func (s *TraceTopologySyncer) dumpCounters() {
	s.logger.Debug(
		"COUNTERS Edge processing stats",
		zap.Int64("caller_missing", atomic.LoadInt64(&s.droppedCallerMissing)),
		zap.Int64("callee_missing", atomic.LoadInt64(&s.droppedCalleeMissing)),
		zap.Int64("self_call_filtered", atomic.LoadInt64(&s.droppedSelfCall)),
		zap.Int64("ambiguous_lookup", atomic.LoadInt64(&s.droppedAmbiguousLookup)),
		zap.Int64("flush_failed", atomic.LoadInt64(&s.droppedFlushFailed)),
		zap.Int64("edges_written", atomic.LoadInt64(&s.edgesWritten)),
	)
}

func (s *TraceTopologySyncer) Stop() {
	close(s.stopCh)
	s.flushBuffer()
	s.logger.Info("TraceTopologySyncer stopped")
}

func (s *TraceTopologySyncer) RelationCache() *bigcache.BigCache {
	return s.relationCache
}

// warmupBusinessAppCache 启动时从 NebulaGraph 分批加载已有 BusinessApp 顶点到 BigCache，
// 同时为 BusinessLabelSyncer 缓存属性 hash（key: bls-hash:{ns}:{name}）。
func (s *TraceTopologySyncer) warmupBusinessAppCache() {
	const batchSize = 500
	s.logger.Info("Warming up BusinessApp cache from NebulaGraph...")

	loaded := 0
	for offset := 0; ; offset += batchSize {
		query := fmt.Sprintf(
			`MATCH (v:BusinessApp) RETURN v.BusinessApp.app_name AS app_name, v.BusinessApp.namespace AS namespace,
			 v.BusinessApp.uid AS uid, v.BusinessApp.team AS team, v.BusinessApp.business_unit AS biz_unit,
			 v.BusinessApp.criticality AS crit, v.BusinessApp.environment AS env
			 SKIP %d LIMIT %d`, offset, batchSize,
		)

		resultSet, err := s.graphDB.ExecuteAndCheck(query)
		if err != nil || resultSet == nil {
			s.logger.Warn("BusinessApp cache warmup query failed", zap.Error(err))
			break
		}

		rowCount := resultSet.GetRowSize()
		for i := 0; i < rowCount; i++ {
			row, err := resultSet.GetRowValuesByIndex(i)
			if err != nil {
				continue
			}
			appName, _ := getStrVal(row, "app_name")
			ns, _ := getStrVal(row, "namespace")
			uid, _ := getStrVal(row, "uid")
			team, _ := getStrVal(row, "team")
			bizUnit, _ := getStrVal(row, "biz_unit")
			crit, _ := getStrVal(row, "crit")
			env, _ := getStrVal(row, "env")

			if appName == "" || uid == "" {
				continue
			}

			// TraceTopologySyncer 用：app 名 → uid
			_ = s.relationCache.Set("bizapp:"+ns+":"+appName, []byte(uid))

			// BusinessLabelSyncer 用：app 属性 hash 去重
			hash := hashBusinessAppAttrs(appName, ns, team, bizUnit, crit, env)
			_ = s.relationCache.Set("bls-hash:"+ns+":"+appName, []byte(hash))
			loaded++
		}

		if rowCount < batchSize {
			break
		}
	}

	s.logger.Info("BusinessApp cache warmup completed",
		zap.Int("loaded", loaded))
}

func getStrVal(row *nebula.Record, col string) (string, error) {
	v, e := row.GetValueByColName(col)
	if e != nil {
		return "", e
	}
	return v.AsString()
}

func hashBusinessAppAttrs(appName, ns, team, bizUnit, crit, env string) string {
	h := sha256.Sum256([]byte(strings.Join([]string{appName, ns, crit, env, team, bizUnit}, "|")))
	return string(h[:])
}

func (s *TraceTopologySyncer) consumeLoop() {
	ctx := context.Background()
	s.logger.Info("consumeLoop started")

	for {
		select {
		case <-s.stopCh:
			s.logger.Info("consumeLoop: stopCh received, exiting")
			return
		default:
		}

		fetches := s.messageQueue.PollFetches(ctx)
		if fetches.IsClosed() {
			s.logger.Warn("Kafka client closed, stopping TraceTopologySyncer")
			return
		}

		fetches.EachError(func(topic string, partition int32, err error) {
			// "lost records" 是 rebalance 握手阶段的过渡状态，实际 offset 未丢失
			if strings.Contains(err.Error(), "lost records") {
				return
			}
			s.logger.Error("Kafka fetch error",
				zap.String("topic", topic),
				zap.Int32("partition", partition),
				zap.Error(err))
		})

		recordCount := 0
		fetches.EachRecord(func(msg *interfaces.Message) {
			recordCount++
			if strings.Contains(string(msg.Value), "image-proxy-swyang") {
				traceData := &v1.TracesData{}
				if err := proto.Unmarshal(msg.Value, traceData); err != nil {
					s.logger.Warn("DIAG_PROXY_RAW unmarshal failed", zap.Error(err), zap.Int("len", len(msg.Value)))
				} else {
					jsonBytes, err := protojson.Marshal(traceData)
					if err != nil {
						s.logger.Warn("DIAG_PROXY_RAW marshal to json failed", zap.Error(err))
					} else {
						s.logger.Warn("DIAG_PROXY_RAW", zap.Int("len", len(msg.Value)), zap.String("trace_json", string(jsonBytes)))
					}
				}
			}
			s.logger.Debug("consumeLoop: Received Kafka message", zap.String("topic", msg.Topic), zap.Int32("partition", msg.Partition), zap.Int("value_len", len(msg.Value)))
			if s.processSpanRecord(msg) {
				s.messageQueue.MarkCommit(msg)
				s.logger.Debug("consumeLoop: MarkCommit done", zap.String("topic", msg.Topic), zap.Int32("partition", msg.Partition), zap.Int64("offset", msg.Offset))
			} else {
				s.logger.Warn("consumeLoop: processSpanRecord returned false, NOT committing", zap.String("topic", msg.Topic), zap.Int32("partition", msg.Partition), zap.Int64("offset", msg.Offset))
			}
		})
		s.logger.Debug("consumeLoop: EachRecord completed", zap.Int("records_processed", recordCount))
	}
}

func (s *TraceTopologySyncer) processSpanRecord(msg *interfaces.Message) bool {
	s.logger.Debug("processSpanRecord: start", zap.Int("value_len", len(msg.Value)))
	traceData := &v1.TracesData{}
	if err := proto.Unmarshal(msg.Value, traceData); err != nil {
		s.logger.Error("processSpanRecord: unmarshal FAILED", zap.Error(err), zap.Int("value_len", len(msg.Value)))
		return false
	}

	resourceSpans := traceData.GetResourceSpans()
	s.logger.Debug("processSpanRecord: Parsed TracesData", zap.Int("resource_spans_count", len(resourceSpans)))
	if len(resourceSpans) == 0 {
		s.logger.Debug("processSpanRecord: no ResourceSpans, returning true")
		return true
	}

	// 预扫：构建 service.name / k8s.owner.name → k8s.namespace.name 映射
	// 用于纠正 resolvePeerBusinessApp 中 DNS 解析的 namespace
	peerNsMap := make(map[string]string)
	for _, rs := range resourceSpans {
		ns := extractResourceAttr(rs, "k8s.namespace.name")
		if ns == "" {
			continue
		}
		if svc := extractResourceAttr(rs, "service.name"); svc != "" {
			peerNsMap[svc] = ns
		}
		if owner := extractResourceAttr(rs, "k8s.owner.name"); owner != "" {
			peerNsMap[owner] = ns
		}
	}

	spanCount := 0
	for _, rs := range resourceSpans {
		caller, ok := s.resolveCallerBusinessApp(rs)
		if !ok {
			atomic.AddInt64(&s.droppedCallerMissing, 1)
			s.logger.Debug("processSpanRecord: resolveCallerBusinessApp FAILED",
				zap.String("k8s_owner_name", extractResourceAttr(rs, "k8s.owner.name")),
				zap.String("service_name", extractResourceAttr(rs, "service.name")),
				zap.String("namespace", extractResourceAttr(rs, "k8s.namespace.name")),
				zap.Int("attr_count", len(rs.GetResource().GetAttributes())))
			continue
		}
		s.logger.Debug("TRACE_PROCESS Caller resolved",
			zap.String("uid", caller.uid),
			zap.String("app_name", caller.appName),
			zap.String("namespace", caller.namespace))

		scopeSpans := rs.GetScopeSpans()
		for _, ss := range scopeSpans {
			for _, span := range ss.GetSpans() {
				if span.GetKind() == v1.Span_SPAN_KIND_INTERNAL {
					s.logger.Debug("TRACE_PROCESS INTERNAL span skipped",
						zap.String("span_name", span.GetName()),
						zap.String("caller_app", caller.appName))
					continue
				}

				callee, ok := s.resolvePeerBusinessApp(span, peerNsMap)
				if !ok {
					atomic.AddInt64(&s.droppedCalleeMissing, 1)
					s.logger.Debug("TRACE_PROCESS resolvePeerBusinessApp failed",
						zap.String("peer_service", extractSpanAttr(span, "peer.service")),
						zap.String("server_address", extractSpanAttr(span, "server.address")),
						zap.String("caller_app", caller.appName),
						zap.String("caller_namespace", caller.namespace))
					continue
				}
				s.logger.Debug("TRACE_PROCESS Callee resolved",
					zap.String("uid", callee.uid),
					zap.String("app_name", callee.appName),
					zap.String("namespace", callee.namespace))

				if caller.uid == callee.uid {
					atomic.AddInt64(&s.droppedSelfCall, 1)
					s.logger.Debug("TRACE_PROCESS Self-call filtered",
						zap.String("uid", caller.uid),
						zap.String("app_name", caller.appName))
					continue
				}

				s.upsertCallEdge(caller, callee)
				s.logger.Debug("TRACE_PROCESS Call edge buffered",
					zap.String("caller", caller.appName),
					zap.String("callee", callee.appName))
				spanCount++
			}
		}
	}

	if spanCount > 0 {
		s.logger.Debug("TRACE_PROCESS Processed trace batch",
			zap.Int("resource_spans", len(resourceSpans)),
			zap.Int("call_edges", spanCount))
	} else {
		s.logger.Debug("TRACE_PROCESS No call edges produced for this message",
			zap.Int("resource_spans", len(resourceSpans)))
	}
	return true
}

func (s *TraceTopologySyncer) resolveCallerBusinessApp(rs *v1.ResourceSpans) (businessAppRef, bool) {
	appName := extractResourceAttr(rs, "k8s.owner.name")
	s.logger.Debug("CALLER_RESOLVE Trying k8s.owner.name", zap.String("value", appName))
	if appName == "" {
		appName = extractResourceAttr(rs, "k8s.statefulset.name")
		s.logger.Debug("CALLER_RESOLVE Trying k8s.statefulset.name", zap.String("value", appName))
	}
	if appName == "" {
		appName = extractResourceAttr(rs, "k8s.deployment.name")
		s.logger.Debug("CALLER_RESOLVE Trying k8s.deployment.name", zap.String("value", appName))
	}
	if appName == "" {
		appName = extractResourceAttr(rs, "service.name")
		s.logger.Debug("CALLER_RESOLVE Trying service.name", zap.String("value", appName))
	}
	if appName == "" {
		s.logger.Debug("CALLER_RESOLVE All appName sources empty, returning false")
		return businessAppRef{}, false
	}

	namespace := extractResourceAttr(rs, "k8s.namespace.name")
	s.logger.Debug("CALLER_RESOLVE Final appName and namespace",
		zap.String("app_name", appName),
		zap.String("namespace", namespace))

	uid, ok := s.lookupOrInsertBusinessApp(appName, namespace)
	if !ok {
		s.logger.Debug("CALLER_RESOLVE lookupOrInsertBusinessApp failed",
			zap.String("app_name", appName),
			zap.String("namespace", namespace))
		return businessAppRef{}, false
	}

	s.logger.Debug("CALLER_RESOLVE Success",
		zap.String("uid", uid),
		zap.String("app_name", appName),
		zap.String("namespace", namespace))
	return businessAppRef{uid: uid, appName: appName, namespace: namespace}, true
}

func (s *TraceTopologySyncer) resolvePeerBusinessApp(span *v1.Span, peerNsMap map[string]string) (businessAppRef, bool) {
	rawPeer := extractSpanAttr(span, "peer.service")
	s.logger.Debug("PEER_RESOLVE Trying peer.service", zap.String("value", rawPeer))
	if rawPeer == "" {
		rawPeer = extractSpanAttr(span, "service.peer.name")
		s.logger.Debug("PEER_RESOLVE Trying service.peer.name", zap.String("value", rawPeer))
	}
	if rawPeer == "" {
		rawPeer = extractSpanAttr(span, "server.address")
		s.logger.Debug("PEER_RESOLVE Trying server.address", zap.String("value", rawPeer))
	}
	if rawPeer == "" {
		rawPeer = extractSpanAttr(span, "network.peer.address")
		s.logger.Debug("PEER_RESOLVE Trying network.peer.address", zap.String("value", rawPeer))
	}
	if rawPeer == "" {
		rawPeer = extractSpanAttr(span, "net.peer.name")
		s.logger.Debug("PEER_RESOLVE Trying net.peer.name", zap.String("value", rawPeer))
	}
	if rawPeer == "" {
		rawPeer = extractSpanAttr(span, "net.peer.ip")
		s.logger.Debug("PEER_RESOLVE Trying net.peer.ip", zap.String("value", rawPeer))
	}
	if rawPeer == "" {
		s.logger.Debug("PEER_RESOLVE All peer attributes empty, returning false")
		return businessAppRef{}, false
	}

	appName, namespace := s.resolvePeerFromAddress(rawPeer)
	s.logger.Debug("PEER_RESOLVE Resolved from address",
		zap.String("raw_peer", rawPeer),
		zap.String("app_name", appName),
		zap.String("namespace", namespace))
	if appName == "" {
		s.logger.Debug("PEER_RESOLVE appName empty after resolvePeerFromAddress, returning false")
		return businessAppRef{}, false
	}

	if correctNs, ok := peerNsMap[appName]; ok && correctNs != "" && namespace != correctNs {
		s.logger.Debug("PEER_RESOLVE namespace corrected by peer resource",
			zap.String("app_name", appName),
			zap.String("dns_namespace", namespace),
			zap.String("correct_namespace", correctNs))
		namespace = correctNs
	}

	// 当 peer.service 解析出的 namespace 为空时（短名无 DNS 后缀），
	// 尝试从 server.address 提取 namespace（通常包含完整 DNS 名如 kafka.base.svc.cluster.local）
	if namespace == "" {
		serverAddr := extractSpanAttr(span, "server.address")
		if serverAddr != "" && serverAddr != rawPeer {
			if _, nsFromServer := s.resolvePeerFromAddress(serverAddr); nsFromServer != "" {
				namespace = nsFromServer
				s.logger.Debug("PEER_RESOLVE Filled namespace from server.address",
					zap.String("app_name", appName),
					zap.String("namespace", namespace),
					zap.String("server_address", serverAddr))
			}
		}
	}

	uid, ok := s.lookupOrInsertBusinessApp(appName, namespace)
	if !ok {
		s.logger.Debug("PEER_RESOLVE lookupOrInsertBusinessApp failed",
			zap.String("app_name", appName),
			zap.String("namespace", namespace))
		return businessAppRef{}, false
	}

	s.logger.Debug("PEER_RESOLVE Success",
		zap.String("uid", uid),
		zap.String("app_name", appName),
		zap.String("namespace", namespace))
	return businessAppRef{uid: uid, appName: appName, namespace: namespace}, true
}

func (s *TraceTopologySyncer) resolvePeerFromAddress(rawPeer string) (appName, namespace string) {
	if entry, ok := s.knownServices[rawPeer]; ok {
		return entry.Name, entry.Namespace
	}

	host := rawPeer
	if idx := strings.LastIndex(rawPeer, ":"); idx > 0 {
		stripped := rawPeer[:idx]
		if entry, ok := s.knownServices[stripped]; ok {
			return entry.Name, entry.Namespace
		}
		if net.ParseIP(stripped) != nil {
			return "", ""
		}
		host = stripped
	}

	if net.ParseIP(host) != nil {
		if entry, ok := s.knownServices[host]; ok {
			return entry.Name, entry.Namespace
		}
		return "", ""
	}

	parts := strings.SplitN(host, ".", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "", ""
	}
	appName = parts[0]

	if len(parts) > 1 {
		dnsParts := strings.SplitN(parts[1], ".", 2)
		if len(dnsParts) > 0 && dnsParts[0] != "" {
			namespace = dnsParts[0]
		}
	}

	return appName, namespace
}

func (s *TraceTopologySyncer) lookupOrInsertBusinessApp(appName, namespace string) (string, bool) {
	cacheKey := "bizapp:" + namespace + ":" + appName
	if cached, err := s.relationCache.Get(cacheKey); err == nil {
		s.logger.Debug("BIZAPP_LOOKUP Cache hit",
			zap.String("cache_key", cacheKey),
			zap.String("uid", string(cached)))
		return string(cached), true
	}
	s.logger.Debug("BIZAPP_LOOKUP Cache miss", zap.String("cache_key", cacheKey))

	if namespace != "" {
		// Non-empty namespace: exact match (app_name + namespace)
		query := fmt.Sprintf(`MATCH (v:BusinessApp) WHERE v.BusinessApp.app_name == %s AND v.BusinessApp.namespace == %s RETURN v.BusinessApp.uid as uid LIMIT 1`,
			strconv.Quote(appName), strconv.Quote(namespace))
		s.logger.Debug("BIZAPP_LOOKUP Querying with namespace",
			zap.String("app_name", appName),
			zap.String("namespace", namespace))

		resultSet, err := s.graphDB.ExecuteAndCheck(query)
		if err == nil && resultSet != nil && resultSet.GetRowSize() > 0 {
			row, rowErr := resultSet.GetRowValuesByIndex(0)
			if rowErr == nil {
				value, valErr := row.GetValueByColName("uid")
				if valErr == nil {
					uid, uidErr := value.AsString()
					if uidErr == nil && uid != "" {
						_ = s.relationCache.Set(cacheKey, []byte(uid))
						s.logger.Debug("BIZAPP_LOOKUP Found existing BusinessApp",
							zap.String("app_name", appName),
							zap.String("namespace", namespace),
							zap.String("uid", uid))
						return uid, true
					}
				}
			}
		}
		s.logger.Debug("BIZAPP_LOOKUP No match found with namespace, falling through to auto-create",
			zap.String("app_name", appName),
			zap.String("namespace", namespace))
	} else {
		// Empty namespace: require exactly one match, otherwise fail.
		query := fmt.Sprintf(`MATCH (v:BusinessApp) WHERE v.BusinessApp.app_name == %s RETURN v.BusinessApp.app_name as app_name, v.BusinessApp.namespace as namespace, v.BusinessApp.uid as uid`,
			strconv.Quote(appName))
		s.logger.Debug("BIZAPP_LOOKUP Querying without namespace",
			zap.String("app_name", appName))

		resultSet, err := s.graphDB.ExecuteAndCheck(query)
		if err == nil && resultSet != nil {
			rowCount := resultSet.GetRowSize()
			s.logger.Debug("BIZAPP_LOOKUP Query result count (empty namespace)",
				zap.String("app_name", appName),
				zap.Int("row_count", rowCount))
			if rowCount == 1 {
				row, rowErr := resultSet.GetRowValuesByIndex(0)
				if rowErr == nil {
					uidVal, valErr := row.GetValueByColName("uid")
					if valErr == nil {
						uid, uidErr := uidVal.AsString()
						if uidErr == nil && uid != "" {
							_ = s.relationCache.Set(cacheKey, []byte(uid))
							s.logger.Debug("BIZAPP_LOOKUP Found existing BusinessApp (unique match)",
								zap.String("app_name", appName),
								zap.String("namespace", namespace),
								zap.String("uid", uid))
							return uid, true
						}
					}
				}
			} else if rowCount > 1 {
				atomic.AddInt64(&s.droppedAmbiguousLookup, 1)
				s.logger.Debug("BIZAPP_LOOKUP Ambiguous BusinessApp lookup (multiple matches), skipping edge",
					zap.String("app_name", appName), zap.Int("match_count", rowCount))
				return "", false
			}
			// rowCount == 0 → fall through to auto-create
			s.logger.Debug("BIZAPP_LOOKUP No match found (empty namespace), falling through to auto-create",
				zap.String("app_name", appName))
		}
	}

	s.logger.Debug("BIZAPP_LOOKUP Auto-creating BusinessApp vertex",
		zap.String("app_name", appName),
		zap.String("namespace", namespace))

	uid := s.buildTraceBusinessAppUID(appName, namespace)
	insertQuery := fmt.Sprintf(
		`INSERT VERTEX BusinessApp(uid, app_name, namespace, criticality, environment, team, business_unit) VALUES %s:(%s, %s, %s, %s, %s, %s, %s);`,
		strconv.Quote(uid),
		strconv.Quote(uid),
		strconv.Quote(appName),
		strconv.Quote(namespace),
		strconv.Quote("medium"),
		strconv.Quote(""),
		strconv.Quote(""),
		strconv.Quote(""),
	)

	if _, err := s.graphDB.ExecuteAndCheck(insertQuery); err != nil {
		s.logger.Error("BIZAPP_LOOKUP Failed to auto-create BusinessApp vertex",
			zap.String("app_name", appName),
			zap.String("namespace", namespace),
			zap.String("query", insertQuery),
			zap.Error(err))
		return "", false
	}

	s.logger.Debug("BIZAPP_LOOKUP Auto-created BusinessApp vertex",
		zap.String("uid", uid),
		zap.String("app_name", appName),
		zap.String("namespace", namespace))
	_ = s.relationCache.Set(cacheKey, []byte(uid))
	return uid, true
}

func (s *TraceTopologySyncer) buildTraceBusinessAppUID(appName, namespace string) string {
	return fmt.Sprintf("bizapp-%s-%s-%s", s.clusterName, namespace, appName)
}

func (s *TraceTopologySyncer) upsertCallEdge(caller, callee businessAppRef) {
	cacheKey := caller.uid + "->" + callee.uid

	if _, err := s.relationCache.Get(cacheKey); err == nil {
		s.logger.Debug("CALLS_APP Edge already cached, skipping",
			zap.String("caller", caller.appName),
			zap.String("callee", callee.appName),
			zap.String("cache_key", cacheKey))
		return
	}

	s.bufferMu.Lock()
	s.edgeBuffer = append(s.edgeBuffer, pendingCallEdge{from: caller.uid, to: callee.uid, cacheKey: cacheKey})
	shouldFlush := len(s.edgeBuffer) >= callsFlushThreshold
	s.bufferMu.Unlock()

	if shouldFlush {
		go s.flushBuffer()
	}

	s.logger.Debug("CALLS_APP Edge added to buffer",
		zap.String("caller_uid", caller.uid),
		zap.String("caller_app", caller.appName),
		zap.String("callee_uid", callee.uid),
		zap.String("callee_app", callee.appName),
		zap.Int("buffer_size", len(s.edgeBuffer)))
}

func (s *TraceTopologySyncer) flushLoop() {
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

func (s *TraceTopologySyncer) flushBuffer() {
	s.bufferMu.Lock()
	if len(s.edgeBuffer) == 0 {
		s.bufferMu.Unlock()
		return
	}

	batch := s.edgeBuffer
	s.edgeBuffer = make([]pendingCallEdge, 0, flushThreshold)
	s.bufferMu.Unlock()

	s.logger.Debug("CALLS_APP_FLUSH Starting flush", zap.Int("total_edges", len(batch)))

	const batchSize = 200
	for i := 0; i < len(batch); i += batchSize {
		end := i + batchSize
		if end > len(batch) {
			end = len(batch)
		}
		chunk := batch[i:end]

		values := make([]string, 0, len(chunk))
		for _, edge := range chunk {
			values = append(values, fmt.Sprintf(
				"%s -> %s:()",
				strconv.Quote(edge.from),
				strconv.Quote(edge.to),
			))
		}

		query := fmt.Sprintf("INSERT EDGE CallsApp () VALUES %s;", strings.Join(values, ", "))
		s.logger.Debug("CALLS_APP_FLUSH Executing batch query",
			zap.Int("batch_start", i),
			zap.Int("batch_size", len(chunk)),
			zap.String("query_preview", query[:min(len(query), 200)]))

		if _, err := s.graphDB.ExecuteAndCheck(query); err != nil {
			atomic.AddInt64(&s.droppedFlushFailed, int64(len(chunk)))
			s.logger.Error("CALLS_APP_FLUSH Failed to flush call edges batch to Nebula",
				zap.Int("batch_start", i),
				zap.Int("batch_size", len(chunk)),
				zap.String("query_preview", query[:min(len(query), 500)]),
				zap.Error(err))
			continue
		}

		for _, edge := range chunk {
			_ = s.relationCache.Set(edge.cacheKey, []byte("1"))
		}
		atomic.AddInt64(&s.edgesWritten, int64(len(chunk)))
		s.logger.Debug("CALLS_APP_FLUSH Batch flushed successfully",
			zap.Int("batch_start", i),
			zap.Int("batch_size", len(chunk)))
	}

	s.logger.Debug("CALLS_APP_FLUSH All call edges flushed to Nebula", zap.Int("total_count", len(batch)))
}

func extractResourceAttr(rs *v1.ResourceSpans, key string) string {
	if rs.GetResource() == nil {
		return ""
	}
	for _, attr := range rs.GetResource().GetAttributes() {
		if attr.GetKey() == key {
			return attr.GetValue().GetStringValue()
		}
	}
	return ""
}

func extractSpanAttr(span *v1.Span, key string) string {
	for _, attr := range span.GetAttributes() {
		if attr.GetKey() == key {
			return attr.GetValue().GetStringValue()
		}
	}
	return ""
}
