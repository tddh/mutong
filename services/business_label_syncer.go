package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gitee.com/tddh/mutong/config"
	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models"
	"github.com/allegro/bigcache/v3"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	labelPrefixMutong = "app.mutong.io/"
	labelPrefixK8s    = "app.kubernetes.io/"

	labelAppName    = "name"
	labelAppTeam    = "team"
	labelAppBizUnit = "business-unit"
	labelAppCrit    = "criticality"
	labelAppEnv     = "environment"

	// cacheKeyPrefixBLS isolates BusinessLabelSyncer's dedup cache from the main K8s resource cache.
	cacheKeyPrefixBLS = "bls:"
	cacheKeyPrefixV   = "v:" // vertex write dedup key prefix
	cacheKeyPrefixE   = "e:" // edge write dedup key prefix

	blsWorkerCount    = 5
	blsTaskChanSize   = 500
	blsFlushInterval  = 3 * time.Second
	blsFlushThreshold = 100
)

type blsVertexBatchItem struct {
	app        models.BusinessApp
	msg        *interfaces.Message
	baseHash   string // 基础属性去重
	ownerHash  string // owner 去重（仅 writeOwner 时有效）
	writeOwner bool   // 仅控制器级资源(Deployment/StatefulSet/DaemonSet/CronJob)为 true
}

type blsEdgeBatchItem struct {
	fromUID string
	toUID   string
	msg     *interfaces.Message
}

type blsDeleteEdgeBatchItem struct {
	fromUID string
	toUID   string
	msg     *interfaces.Message
}

type compiledNormRule struct {
	name        string
	namespaceRe *regexp.Regexp
	kindSet     map[string]bool
	patternRe   *regexp.Regexp
	replacement string
}

type BusinessLabelSyncer struct {
	logger             interfaces.Logger
	graphDB            interfaces.GraphDB
	namespaceMapper    *NamespaceMapper
	cache              interfaces.Cache
	relationCache      *bigcache.BigCache
	cluster            *config.Cluster
	messageQueue       interfaces.MessageQueue
	stopCh             chan struct{}
	normalizationRules []compiledNormRule
	cntFn              func(bizUID string) int

	taskChan chan *interfaces.Message
	wg       sync.WaitGroup
	inflight sync.Map

	vertexBuffer     []blsVertexBatchItem
	edgeBuffer       []blsEdgeBatchItem
	deleteEdgeBuffer []blsDeleteEdgeBatchItem
	bufferMu         sync.Mutex
}

func NewBusinessLabelSyncer(
	logger interfaces.Logger,
	graphDB interfaces.GraphDB,
	namespaceMapper *NamespaceMapper,
	cache interfaces.Cache,
	cluster *config.Cluster,
	messageQueue interfaces.MessageQueue,
	normConf config.AppNameNormalizationConf,
) *BusinessLabelSyncer {
	s := &BusinessLabelSyncer{
		logger:          logger,
		graphDB:         graphDB,
		namespaceMapper: namespaceMapper,
		cache:           cache,
		cluster:         cluster,
		messageQueue:    messageQueue,
		stopCh:          make(chan struct{}),
		taskChan:        make(chan *interfaces.Message, blsTaskChanSize),
	}
	s.compileNormalizationRules(normConf)
	return s
}

func (s *BusinessLabelSyncer) SetRelationCache(cache *bigcache.BigCache) {
	s.relationCache = cache
}

func (s *BusinessLabelSyncer) Start() {
	s.logger.Info("BusinessLabelSyncer starting")

	for i := 0; i < blsWorkerCount; i++ {
		s.wg.Add(1)
		go s.worker(i)
	}

	go s.consumeLoop()
	go s.flushLoop()
}

func (s *BusinessLabelSyncer) Stop() {
	close(s.stopCh)

	s.logger.Debug("Waiting for flush cycle to complete")
	s.flushBuffers()

	close(s.taskChan)
	s.wg.Wait()

	s.logger.Info("BusinessLabelSyncer stopped")
}

func (s *BusinessLabelSyncer) consumeLoop() {
	ctx := context.Background()
	s.logger.Debug("[BLS_CONSUMER] consumeLoop started")
	for {
		select {
		case <-s.stopCh:
			s.logger.Debug("[BLS_CONSUMER] stopCh received, exiting")
			return
		default:
		}

		fetches := s.messageQueue.PollFetches(ctx)
		if fetches.IsClosed() {
			s.logger.Warn("Kafka client closed, stopping BusinessLabelSyncer")
			return
		}

		fetches.EachError(func(topic string, partition int32, err error) {
			if strings.Contains(err.Error(), "lost records") {
				return
			}
			s.logger.Error("Kafka fetch error",
				zap.String("topic", topic),
				zap.Int32("partition", partition),
				zap.Error(err))
		})

		var records []*interfaces.Message
		fetches.EachRecord(func(msg *interfaces.Message) {
			records = append(records, msg)
		})

		for _, msg := range records {
			select {
			case s.taskChan <- msg:
			case <-s.stopCh:
				return
			}
		}
	}
}

func (s *BusinessLabelSyncer) worker(id int) {
	defer s.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("BLS worker panic recovered",
				zap.Int("workerID", id),
				zap.Any("panic", r))
		}
	}()
	for msg := range s.taskChan {
		s.processWorkloadMessage(msg)
		if s.messageQueue != nil {
			s.messageQueue.MarkCommit(msg)
		}
	}
	s.logger.Debug("BLS worker stopped", zap.Int("workerID", id))
}

func (s *BusinessLabelSyncer) processWorkloadMessage(msg *interfaces.Message) {
	var kafkaMsg models.KafkaResourceMessage
	if err := json.Unmarshal(msg.Value, &kafkaMsg); err != nil {
		s.logger.Error("Failed to unmarshal workload message", zap.Error(err))
		return
	}

	var unstructuredObj unstructured.Unstructured
	if err := json.Unmarshal(kafkaMsg.Object, &unstructuredObj.Object); err != nil {
		s.logger.Error("Failed to unmarshal unstructured object", zap.Error(err))
		return
	}

	uid := string(unstructuredObj.GetUID())
	resourceVersion := unstructuredObj.GetResourceVersion()

	cacheKey := cacheKeyPrefixBLS + uid
	if cachedRV, err := s.cache.Get(cacheKey); err == nil {
		if string(cachedRV) == resourceVersion {
			return
		}
	}

	if _, loaded := s.inflight.LoadOrStore(cacheKey, struct{}{}); loaded {
		s.logger.Debug("BLS message already in-flight", zap.String("uid", uid))
		return
	}
	defer s.inflight.Delete(cacheKey)

	clusterName := ""
	if s.cluster != nil && len(s.cluster.K8sClusterClient) > 0 {
		for name := range s.cluster.K8sClusterClient {
			clusterName = name
			break
		}
	}

	switch kafkaMsg.EventType {
	case "Added", "Updated":
		s.syncApp(&unstructuredObj, clusterName, resourceVersion, cacheKey, msg)
	case "Deleted":
		s.removeApp(&unstructuredObj, clusterName, cacheKey, msg)
	default:
		s.logger.Warn("Unknown event type", zap.String("eventType", kafkaMsg.EventType))
	}
}

func (s *BusinessLabelSyncer) syncApp(obj interface{}, clusterName string, resourceVersion string, cacheKey string, msg *interfaces.Message) {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return
	}

	labels := unstructuredObj.GetLabels()
	appName := s.getLabelValue(labels, labelAppName)
	if appName == "" {
		appName = s.normalizeAppName(unstructuredObj.GetName(), unstructuredObj.GetNamespace(), unstructuredObj.GetKind())
	}
	if appName == "" {
		appName = unstructuredObj.GetName()
	}
	if appName == "" {
		return
	}

	namespace := unstructuredObj.GetNamespace()
	uid := string(unstructuredObj.GetUID())

	bizAttrs := s.extractBusinessAttributes(labels, namespace)
	bizUID := s.buildBusinessAppUID(appName, namespace, clusterName)

	_ = s.cache.Delete("bls-orphan:" + bizUID)

	kind := unstructuredObj.GetKind()
	resName := unstructuredObj.GetName()
	// owner 仅由控制器级工作负载写入，避免 Pod/ReplicaSet/Job 覆盖导致归属漂移
	writeOwner := isPrimaryControllerKind(kind)
	ownerName, ownerKind := "", ""
	if writeOwner {
		ownerName, ownerKind = resName, kind
	}

	app := models.BusinessApp{
		UID:          bizUID,
		AppName:      appName,
		Namespace:    namespace,
		Criticality:  bizAttrs.Criticality,
		Environment:  bizAttrs.Environment,
		Team:         bizAttrs.Team,
		BusinessUnit: bizAttrs.BusinessUnit,
		OwnerName:    ownerName,
		OwnerKind:    ownerKind,
	}

	baseHash := s.hashBaseAttrs(appName, namespace, bizAttrs)
	baseKey := "bls-hash:" + namespace + ":" + appName
	eKey := cacheKeyPrefixE + uid + "->" + bizUID

	var cachedBase []byte
	if s.relationCache != nil {
		cachedBase, _ = s.relationCache.Get(baseKey)
	}
	needBase := cachedBase == nil || string(cachedBase) != baseHash

	ownerHash := ""
	needOwner := false
	if writeOwner {
		ownerHash = s.hashOwnerAttrs(ownerName, ownerKind)
		ownerKey := "bls-owner:" + namespace + ":" + appName
		var cachedOwner []byte
		if s.relationCache != nil {
			cachedOwner, _ = s.relationCache.Get(ownerKey)
		}
		needOwner = cachedOwner == nil || string(cachedOwner) != ownerHash
	}

	cachedEdge, _ := s.cache.Get(eKey)

	s.bufferMu.Lock()

	if cachedEdge == nil {
		s.edgeBuffer = append(s.edgeBuffer, blsEdgeBatchItem{fromUID: uid, toUID: bizUID, msg: msg})
		_ = s.cache.Set(eKey, []byte("1"))
	}

	if needBase || needOwner {
		s.vertexBuffer = append(s.vertexBuffer, blsVertexBatchItem{
			app: app, msg: msg, baseHash: baseHash, ownerHash: ownerHash, writeOwner: writeOwner,
		})
	}

	shouldFlush := len(s.vertexBuffer) >= blsFlushThreshold || len(s.edgeBuffer) >= blsFlushThreshold
	s.bufferMu.Unlock()

	if shouldFlush {
		s.flushBuffers()
	}

	_ = s.cache.Set(cacheKey, []byte(resourceVersion))
}

// isPrimaryControllerKind 判定是否为顶层工作负载控制器——只有它们才写 BusinessApp 的 owner_name/owner_kind。
// 不含 Job(常为 CronJob 子任务或临时任务)、ReplicaSet(Deployment 派生)、Pod(副本)，避免归属漂移到非主体资源。
func isPrimaryControllerKind(kind string) bool {
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet", "CronJob":
		return true
	default:
		return false
	}
}

func (s *BusinessLabelSyncer) hashBaseAttrs(appName, namespace string, bizAttrs config.NamespaceMappingEntry) string {
	h := sha256.Sum256([]byte(strings.Join([]string{
		appName, namespace,
		bizAttrs.Criticality, bizAttrs.Environment, bizAttrs.Team, bizAttrs.BusinessUnit,
	}, "|")))
	return string(h[:])
}

func (s *BusinessLabelSyncer) hashOwnerAttrs(ownerName, ownerKind string) string {
	h := sha256.Sum256([]byte(ownerName + "|" + ownerKind))
	return string(h[:])
}

func (s *BusinessLabelSyncer) hasBelongsToAppEdge(fromUID, toUID string) bool {
	query := fmt.Sprintf(`GO FROM %s OVER BelongsToApp YIELD dst(edge) AS dst`,
		strconv.Quote(fromUID))
	resultSet, err := s.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet == nil {
		return false
	}
	for i := 0; i < resultSet.GetRowSize(); i++ {
		row, _ := resultSet.GetRowValuesByIndex(i)
		val, _ := row.GetValueByColName("dst")
		if val != nil {
			if dst, e := val.AsString(); e == nil && dst == toUID {
				return true
			}
		}
	}
	return false
}

func (s *BusinessLabelSyncer) removeApp(unstructuredObj *unstructured.Unstructured, clusterName string, cacheKey string, msg *interfaces.Message) {
	if unstructuredObj == nil {
		return
	}

	resourceUID := string(unstructuredObj.GetUID())

	labels := unstructuredObj.GetLabels()
	appName := s.getLabelValue(labels, labelAppName)
	if appName == "" {
		appName = s.normalizeAppName(unstructuredObj.GetName(), unstructuredObj.GetNamespace(), unstructuredObj.GetKind())
	}
	if appName == "" {
		appName = unstructuredObj.GetName()
	}
	if appName == "" {
		return
	}

	namespace := unstructuredObj.GetNamespace()
	bizUID := s.buildBusinessAppUID(appName, namespace, clusterName)

	if !s.hasBelongsToAppEdge(resourceUID, bizUID) {
		_ = s.cache.Delete(cacheKey)
		return
	}

	s.bufferMu.Lock()
	s.deleteEdgeBuffer = append(s.deleteEdgeBuffer, blsDeleteEdgeBatchItem{fromUID: resourceUID, toUID: bizUID, msg: msg})
	shouldFlush := len(s.deleteEdgeBuffer) >= blsFlushThreshold
	s.bufferMu.Unlock()

	if shouldFlush {
		s.flushBuffers()
	}

	remainingCount := s.countBelongsToAppEdges(bizUID)

	if remainingCount == 0 {
		s.deleteBusinessAppVertex(bizUID)
		s.logger.Info("Orphan BusinessApp deleted",
			zap.String("bizapp_uid", bizUID),
			zap.String("app_name", appName),
			zap.String("namespace", namespace))
	} else {
		s.logger.Debug("BelongsToApp edge deleted (BusinessApp has other refs)",
			zap.String("bizapp_uid", bizUID),
			zap.String("app_name", appName),
			zap.Int("remaining_references", remainingCount))
	}

	_ = s.cache.Delete(cacheKey)
}

func (s *BusinessLabelSyncer) flushLoop() {
	ticker := time.NewTicker(blsFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.flushBuffers()
		}
	}
}

func (s *BusinessLabelSyncer) flushBuffers() {
	s.bufferMu.Lock()
	vertices := s.vertexBuffer
	edges := s.edgeBuffer
	deleteEdges := s.deleteEdgeBuffer
	s.vertexBuffer = nil
	s.edgeBuffer = nil
	s.deleteEdgeBuffer = nil
	s.bufferMu.Unlock()

	if len(vertices) == 0 && len(edges) == 0 && len(deleteEdges) == 0 {
		return
	}

	commitSet := make(map[*interfaces.Message]struct{})
	collectMsg := func(m *interfaces.Message) {
		if m != nil {
			commitSet[m] = struct{}{}
		}
	}

	success := true

	if len(vertices) > 0 {
		if !s.batchUpsertBusinessApp(vertices) {
			success = false
		}
		for _, v := range vertices {
			collectMsg(v.msg)
		}
	}
	if len(edges) > 0 {
		if !s.batchUpsertBelongsToAppEdge(edges) {
			success = false
		}
		for _, e := range edges {
			collectMsg(e.msg)
		}
	}
	if len(deleteEdges) > 0 {
		if !s.batchDeleteBelongsToAppEdges(deleteEdges) {
			success = false
		}
		for _, d := range deleteEdges {
			collectMsg(d.msg)
		}
	}

	if success {
		if s.messageQueue != nil {
			for m := range commitSet {
				s.messageQueue.MarkCommit(m)
			}
		}
		if len(commitSet) > 0 {
			s.logger.Debug("BLS flush committed",
				zap.Int("messages", len(commitSet)),
				zap.Int("vertices", len(vertices)),
				zap.Int("edges", len(edges)),
				zap.Int("delete_edges", len(deleteEdges)))
		}
	} else {
		s.logger.Warn("BLS flush partially failed, offsets NOT committed",
			zap.Int("messages", len(commitSet)))
	}
}

func (s *BusinessLabelSyncer) batchUpsertBusinessApp(items []blsVertexBatchItem) bool {
	// 同一 bizUID 本批次去重：控制器项优先(写 owner)，并跳过与之重复的非控制器项。
	controllerUIDs := make(map[string]bool)
	for _, item := range items {
		if item.writeOwner {
			controllerUIDs[item.app.UID] = true
		}
	}

	buildRow := func(item blsVertexBatchItem) string {
		return fmt.Sprintf(
			`%s:(%s, %s, %s, %s, %s, %s, %s, %s, %s)`,
			strconv.Quote(item.app.UID),
			strconv.Quote(item.app.UID),
			strconv.Quote(item.app.AppName),
			strconv.Quote(item.app.Namespace),
			strconv.Quote(item.app.Criticality),
			strconv.Quote(item.app.Environment),
			strconv.Quote(item.app.Team),
			strconv.Quote(item.app.BusinessUnit),
			strconv.Quote(item.app.OwnerName),
			strconv.Quote(item.app.OwnerKind),
		)
	}

	ctrlValues := make([]string, 0, len(items))
	nonCtrlValues := make([]string, 0, len(items))
	ctrlSeen := make(map[string]bool)
	nonCtrlSeen := make(map[string]bool)
	for _, item := range items {
		if item.writeOwner {
			if ctrlSeen[item.app.UID] {
				continue
			}
			ctrlSeen[item.app.UID] = true
			ctrlValues = append(ctrlValues, buildRow(item))
		} else {
			if controllerUIDs[item.app.UID] || nonCtrlSeen[item.app.UID] {
				continue
			}
			nonCtrlSeen[item.app.UID] = true
			nonCtrlValues = append(nonCtrlValues, buildRow(item))
		}
	}

	ok := true
	// 控制器级：普通 INSERT（整点覆盖，写入 owner）
	if len(ctrlValues) > 0 {
		query := fmt.Sprintf(`INSERT VERTEX BusinessApp(uid, app_name, namespace, criticality, environment, team, business_unit, owner_name, owner_kind) VALUES %s;`,
			strings.Join(ctrlValues, ", "))
		if _, err := s.graphDB.ExecuteAndCheck(query); err != nil {
			s.logger.Error("Failed to insert controller BusinessApp vertices",
				zap.Int("count", len(ctrlValues)),
				zap.String("query_preview", query[:min(len(query), 500)]),
				zap.Error(err))
			ok = false
		}
	}
	// 非控制器级：IF NOT EXISTS（仅在顶点缺失时创建，绝不覆盖已有 owner）
	if len(nonCtrlValues) > 0 {
		query := fmt.Sprintf(`INSERT VERTEX IF NOT EXISTS BusinessApp(uid, app_name, namespace, criticality, environment, team, business_unit, owner_name, owner_kind) VALUES %s;`,
			strings.Join(nonCtrlValues, ", "))
		if _, err := s.graphDB.ExecuteAndCheck(query); err != nil {
			s.logger.Error("Failed to insert non-controller BusinessApp vertices",
				zap.Int("count", len(nonCtrlValues)),
				zap.String("query_preview", query[:min(len(query), 500)]),
				zap.Error(err))
			ok = false
		}
	}
	if !ok {
		return false
	}

	if s.relationCache != nil {
		for _, item := range items {
			if item.baseHash != "" {
				baseKey := "bls-hash:" + item.app.Namespace + ":" + item.app.AppName
				_ = s.relationCache.Set(baseKey, []byte(item.baseHash))
			}
			if item.writeOwner && item.ownerHash != "" {
				ownerKey := "bls-owner:" + item.app.Namespace + ":" + item.app.AppName
				_ = s.relationCache.Set(ownerKey, []byte(item.ownerHash))
			}
		}
	}
	s.logger.Debug("Batch upserted BusinessApp vertices",
		zap.Int("controllers", len(ctrlValues)),
		zap.Int("non_controllers", len(nonCtrlValues)))
	return true
}

func (s *BusinessLabelSyncer) batchUpsertBelongsToAppEdge(items []blsEdgeBatchItem) bool {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, fmt.Sprintf(
			"%s -> %s:()",
			strconv.Quote(item.fromUID),
			strconv.Quote(item.toUID),
		))
	}
	query := fmt.Sprintf("INSERT EDGE IF NOT EXISTS BelongsToApp () VALUES %s;", strings.Join(values, ", "))
	if _, err := s.graphDB.ExecuteAndCheck(query); err != nil {
		s.logger.Error("Failed to batch upsert BelongsToApp edges",
			zap.Int("count", len(items)),
			zap.String("query_preview", query[:min(len(query), 500)]),
			zap.Error(err))
		return false
	}
	s.logger.Debug("Batch upserted BelongsToApp edges", zap.Int("count", len(items)))
	return true
}

func (s *BusinessLabelSyncer) batchDeleteBelongsToAppEdges(items []blsDeleteEdgeBatchItem) bool {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, fmt.Sprintf(
			"%s -> %s",
			strconv.Quote(item.fromUID),
			strconv.Quote(item.toUID),
		))
	}
	query := fmt.Sprintf("DELETE EDGE BelongsToApp %s;", strings.Join(values, ", "))
	if _, err := s.graphDB.ExecuteAndCheck(query); err != nil {
		s.logger.Error("Failed to batch delete BelongsToApp edges",
			zap.Int("count", len(items)),
			zap.String("query_preview", query[:min(len(query), 500)]),
			zap.Error(err))
		return false
	}
	s.logger.Debug("Batch deleted BelongsToApp edges", zap.Int("count", len(items)))
	return true
}

func (s *BusinessLabelSyncer) extractBusinessAttributes(labels map[string]string, namespace string) config.NamespaceMappingEntry {
	attrs := config.NamespaceMappingEntry{}

	attrs.Team = s.getLabelValue(labels, labelAppTeam)
	attrs.BusinessUnit = s.getLabelValue(labels, labelAppBizUnit)
	attrs.Criticality = s.getLabelValue(labels, labelAppCrit)
	attrs.Environment = s.getLabelValue(labels, labelAppEnv)

	if s.namespaceMapper != nil {
		nsMapping := s.namespaceMapper.Resolve(namespace)
		if attrs.Team == "" {
			attrs.Team = nsMapping.Team
		}
		if attrs.BusinessUnit == "" {
			attrs.BusinessUnit = nsMapping.BusinessUnit
		}
		if attrs.Criticality == "" {
			attrs.Criticality = nsMapping.Criticality
		}
		if attrs.Environment == "" {
			attrs.Environment = nsMapping.Environment
		}
	}

	if attrs.Criticality == "" {
		attrs.Criticality = "medium"
	}

	return attrs
}

func (s *BusinessLabelSyncer) compileNormalizationRules(conf config.AppNameNormalizationConf) {
	if !conf.Enabled {
		return
	}
	s.normalizationRules = make([]compiledNormRule, 0, len(conf.Rules))
	for _, r := range conf.Rules {
		patternRe, err := regexp.Compile(r.Pattern)
		if err != nil {
			s.logger.Debug("skip normalization rule", zap.String("name", r.Name), zap.Error(err))
			continue
		}

		kindSet := make(map[string]bool, len(r.KindMatch))
		for _, k := range r.KindMatch {
			kindSet[k] = true
		}

		s.normalizationRules = append(s.normalizationRules, compiledNormRule{
			name:        r.Name,
			namespaceRe: patternReFromGlob(r.NamespaceMatch),
			kindSet:     kindSet,
			patternRe:   patternRe,
			replacement: r.Replacement,
		})
	}
}

func patternReFromGlob(pattern string) *regexp.Regexp {
	if pattern == "" {
		return nil
	}
	globRe := regexp.MustCompile(`^` + globToRegex(pattern) + `$`)
	return globRe
}

func globToRegex(g string) string {
	out := ""
	for i := 0; i < len(g); i++ {
		switch g[i] {
		case '*':
			out += ".*"
		case '?':
			out += "."
		case '.', '+', '(', ')', '[', ']', '{', '}', '^', '$', '|', '\\':
			out += "\\" + string(g[i])
		default:
			out += string(g[i])
		}
	}
	return out
}

func (s *BusinessLabelSyncer) buildBusinessAppUID(appName, namespace, cluster string) string {
	return fmt.Sprintf("bizapp-%s-%s-%s", cluster, namespace, appName)
}

func (s *BusinessLabelSyncer) normalizeAppName(resourceName, namespace, kind string) string {
	for _, rule := range s.normalizationRules {
		if rule.namespaceRe != nil && !rule.namespaceRe.MatchString(namespace) {
			continue
		}
		if len(rule.kindSet) > 0 && !rule.kindSet[kind] {
			continue
		}
		if rule.patternRe.MatchString(resourceName) {
			normalized := rule.patternRe.ReplaceAllString(resourceName, rule.replacement)
			if normalized != "" && normalized != resourceName {
				return normalized
			}
		}
	}
	return ""
}

func (s *BusinessLabelSyncer) getLabelValue(labels map[string]string, key string) string {
	if val, ok := labels[labelPrefixMutong+key]; ok {
		return val
	}
	if val, ok := labels[labelPrefixK8s+key]; ok {
		return val
	}
	if val, ok := labels[key]; ok {
		return val
	}
	return ""
}

func (s *BusinessLabelSyncer) countBelongsToAppEdges(bizUID string) int {
	if s.cntFn != nil {
		return s.cntFn(bizUID)
	}
	query := fmt.Sprintf(`MATCH ()-[e:BelongsToApp]->(v:BusinessApp{uid: %s}) RETURN count(e) as cnt;`,
		strconv.Quote(bizUID))
	resultSet, err := s.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet == nil {
		return -1
	}
	if resultSet.GetRowSize() == 0 {
		return 0
	}
	row, err := resultSet.GetRowValuesByIndex(0)
	if err != nil {
		return -1
	}
	val, err := row.GetValueByColName("cnt")
	if err != nil {
		return -1
	}
	cnt, err := val.AsInt()
	if err != nil {
		return -1
	}
	return int(cnt)
}

func (s *BusinessLabelSyncer) deleteBusinessAppVertex(bizUID string) {
	query := fmt.Sprintf(`DELETE VERTEX %s;`, strconv.Quote(bizUID))
	_, err := s.graphDB.ExecuteAndCheck(query)
	if err != nil {
		s.logger.Warn("Failed to delete BusinessApp vertex",
			zap.String("bizapp_uid", bizUID),
			zap.Error(err))
	}
}
