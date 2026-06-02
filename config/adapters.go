package config

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/allegro/bigcache/v3"
	"github.com/twmb/franz-go/pkg/kgo"
	nebula "github.com/vesoft-inc/nebula-go/v3"
	"go.uber.org/zap"
	"gorm.io/gorm/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	"gitee.com/tddh/mutong/interfaces"
)

// ZapLoggerAdapter 将zap.Logger适配为Logger接口
// 用途：封装zap.Logger，使其符合interfaces.Logger接口定义
// 好处：服务层通过接口使用日志，不依赖具体的zap实现
// 使用方式：通过NewZapLoggerAdapter(logger)创建
type ZapLoggerAdapter struct {
	logger *zap.Logger
}

func NewZapLoggerAdapter(logger *zap.Logger) *ZapLoggerAdapter {
	return &ZapLoggerAdapter{logger: logger}
}

func (a *ZapLoggerAdapter) Debug(msg string, fields ...zap.Field) {
	a.logger.Debug(msg, fields...)
}

func (a *ZapLoggerAdapter) Info(msg string, fields ...zap.Field) {
	a.logger.Info(msg, fields...)
}

func (a *ZapLoggerAdapter) Warn(msg string, fields ...zap.Field) {
	a.logger.Warn(msg, fields...)
}

func (a *ZapLoggerAdapter) Error(msg string, fields ...zap.Field) {
	a.logger.Error(msg, fields...)
}

// NebulaGraphAdapter 将Nebula SessionPool适配为GraphDB接口
// 用途：封装Nebula Graph客户端，使其符合interfaces.GraphDB接口定义
// 好处：服务层通过接口使用图数据库，不依赖具体的Nebula实现
// 使用方式：通过NewNebulaGraphAdapter(pool)创建
type NebulaGraphAdapter struct {
	pool *nebula.SessionPool
}

func NewNebulaGraphAdapter(pool *nebula.SessionPool) *NebulaGraphAdapter {
	return &NebulaGraphAdapter{pool: pool}
}

func (a *NebulaGraphAdapter) Execute(query string) (*nebula.ResultSet, error) {
	return a.pool.Execute(query)
}

func (a *NebulaGraphAdapter) ExecuteAndCheck(query string) (*nebula.ResultSet, error) {
	return a.pool.ExecuteAndCheck(query)
}

func (a *NebulaGraphAdapter) Close() error {
	if a.pool != nil {
		a.pool.Close()
	}
	return nil
}

// KafkaAdapter 将Kafka客户端适配为MessageQueue接口
// 用途：封装Kafka客户端，使其符合interfaces.MessageQueue接口定义
// 好处：服务层通过接口使用消息队列，不依赖具体的Kafka实现
// 使用方式：通过NewKafkaAdapter(client)创建
type KafkaAdapter struct {
	client *kgo.Client
	logger *zap.Logger
}

func NewKafkaAdapter(client *kgo.Client, logger *zap.Logger) *KafkaAdapter {
	return &KafkaAdapter{client: client, logger: logger}
}

// Publish 同步发布消息到Kafka
func (a *KafkaAdapter) Publish(ctx context.Context, topic string, key string, message []byte) error {
	record := &kgo.Record{
		Topic: topic,
		Key:   []byte(key),
		Value: message,
	}
	return a.client.ProduceSync(ctx, record).FirstErr()
}

// PollFetches 轮询拉取Kafka消息
func (a *KafkaAdapter) PollFetches(ctx context.Context) interfaces.FetchResult {
	fetches := a.client.PollFetches(ctx)
	numRecords := fetches.NumRecords()
	return &KafkaFetchResult{fetches: fetches, recordCount: numRecords}
}

// MarkCommit 标记 Kafka 消息的 offset，等待异步提交
func (a *KafkaAdapter) MarkCommit(msg *interfaces.Message) {
	record := &kgo.Record{
		Topic:       msg.Topic,
		Partition:   msg.Partition,
		Offset:      msg.Offset,
		LeaderEpoch: msg.LeaderEpoch,
	}
	a.client.MarkCommitRecords(record)
}

func (a *KafkaAdapter) Close() error {
	if a.client != nil {
		a.client.Close()
	}
	return nil
}

// deadLetterMessage 死信消息结构
type deadLetterMessage struct {
	OriginalKey   []byte `json:"original_key"`
	OriginalValue []byte `json:"original_value"`
	OriginalTopic string `json:"original_topic"`
	ErrorMessage  string `json:"error_message"`
	RetryCount    int    `json:"retry_count"`
	FailedAt      string `json:"failed_at"`
}

// PublishDeadLetter 发送消息到死信队列
func (a *KafkaAdapter) PublishDeadLetter(ctx context.Context, originalMsg *interfaces.Message, errMsg string) error {
	dlMsg := deadLetterMessage{
		OriginalKey:   originalMsg.Key,
		OriginalValue: originalMsg.Value,
		OriginalTopic: originalMsg.Topic,
		ErrorMessage:  errMsg,
		FailedAt:      time.Now().Format(time.RFC3339),
	}

	dlBytes, err := json.Marshal(dlMsg)
	if err != nil {
		return err
	}

	dlqTopic := originalMsg.Topic + "_dlq"
	record := &kgo.Record{
		Topic: dlqTopic,
		Key:   originalMsg.Key,
		Value: dlBytes,
	}
	return a.client.ProduceSync(ctx, record).FirstErr()
}

// KafkaFetchResult Kafka拉取结果实现
type KafkaFetchResult struct {
	fetches     kgo.Fetches
	recordCount int
}

func (r *KafkaFetchResult) IsClosed() bool {
	return r.fetches.IsClientClosed()
}

func (r *KafkaFetchResult) EachError(fn func(topic string, partition int32, err error)) {
	r.fetches.EachError(fn)
}

func (r *KafkaFetchResult) EachRecord(fn func(msg *interfaces.Message)) {
	r.fetches.EachRecord(func(record *kgo.Record) {
		fn(&interfaces.Message{
			Key:         record.Key,
			Value:       record.Value,
			Topic:       record.Topic,
			Partition:   record.Partition,
			Offset:      record.Offset,
			LeaderEpoch: record.LeaderEpoch,
		})
	})
}

func (r *KafkaFetchResult) NumRecords() int {
	return r.recordCount
}

// BigCacheAdapter 将BigCache适配为Cache接口
// 用途：封装BigCache客户端，使其符合interfaces.Cache接口定义
// 好处：服务层通过接口使用缓存，不依赖具体的BigCache实现
// 使用方式：通过NewBigCacheAdapter(cache)创建
type BigCacheAdapter struct {
	cache *bigcache.BigCache
}

func NewBigCacheAdapter(cache *bigcache.BigCache) *BigCacheAdapter {
	return &BigCacheAdapter{cache: cache}
}

func (a *BigCacheAdapter) Get(key string) ([]byte, error) {
	return a.cache.Get(key)
}

func (a *BigCacheAdapter) Set(key string, value []byte) error {
	return a.cache.Set(key, value)
}

func (a *BigCacheAdapter) Delete(key string) error {
	return a.cache.Delete(key)
}

// Stats 获取缓存统计信息
// 用途：监控缓存性能，了解命中率、冲突率等指标
func (a *BigCacheAdapter) Stats() interfaces.CacheStats {
	s := a.cache.Stats()
	return interfaces.CacheStats{
		Hits:       s.Hits,
		Misses:     s.Misses,
		DelHits:    s.DelHits,
		DelMisses:  s.DelMisses,
		Collisions: s.Collisions,
	}
}

func (a *BigCacheAdapter) Close() error {
	if a.cache != nil {
		return a.cache.Close()
	}
	return nil
}

// K8sClientAdapter 将Kubernetes dynamic client适配为KubernetesClient接口
// 用途：封装K8s客户端，使其符合interfaces.KubernetesClient接口定义
// 好处：服务层通过接口使用K8s客户端，不依赖具体的client-go实现
// 使用方式：通过NewK8sClientAdapter(client)创建
type K8sClientAdapter struct {
	dynamicClient dynamic.Interface
}

func NewK8sClientAdapter(client dynamic.Interface) *K8sClientAdapter {
	return &K8sClientAdapter{dynamicClient: client}
}

// ListResources 列出指定命名空间的K8s资源
func (a *K8sClientAdapter) ListResources(gvr schema.GroupVersionResource, namespace string, opts interface{}) ([]unstructured.Unstructured, error) {
	if a.dynamicClient == nil {
		return nil, fmt.Errorf("dynamic client is not initialized")
	}

	listOpts := metav1.ListOptions{}
	if opts != nil {
		// 尝试将 opts 转换为 ListOptions
		if lo, ok := opts.(metav1.ListOptions); ok {
			listOpts = lo
		}
	}

	client := a.dynamicClient.Resource(gvr)
	var list *unstructured.UnstructuredList
	var err error
	if namespace != "" {
		list, err = client.Namespace(namespace).List(context.TODO(), listOpts)
	} else {
		list, err = client.List(context.TODO(), listOpts)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list resources %s: %w", gvr.String(), err)
	}

	return list.Items, nil
}

// WatchResources 监听指定命名空间的K8s资源变更事件
func (a *K8sClientAdapter) WatchResources(gvr schema.GroupVersionResource, namespace string, handler cache.ResourceEventHandler) error {
	if a.dynamicClient == nil {
		return fmt.Errorf("dynamic client is not initialized")
	}

	client := a.dynamicClient.Resource(gvr)
	watcher, err := client.Namespace(namespace).Watch(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to watch resources %s: %w", gvr.String(), err)
	}

	go func() {
		for event := range watcher.ResultChan() {
			obj, ok := event.Object.(*unstructured.Unstructured)
			if !ok {
				continue
			}
			switch event.Type {
			case watch.Added:
				handler.OnAdd(obj, false)
			case watch.Modified:
				handler.OnUpdate(obj, obj)
			case watch.Deleted:
				handler.OnDelete(obj)
			}
		}
	}()

	return nil
}

// GetAPIResources 获取K8s集群支持的API资源类型列表
// 注意：此方法需要 discovery client，此处使用 dynamic client 的 ServerGroupsAndResources
// 如果无法获取，返回空列表而不是错误
func (a *K8sClientAdapter) GetAPIResources() ([]schema.GroupVersionResource, error) {
	// dynamic.Interface 不直接提供 API discovery 能力，
	// 需要通过 discovery client 实现。
	// 此处返回空列表，调用方应使用 K8sClusterClient.RootDiscoveryClient 替代。
	return nil, nil
}

// GormZapLogger 将 GORM 日志适配为 zap 输出
type GormZapLogger struct {
	logger        *zap.Logger
	level         logger.LogLevel
	slowThreshold time.Duration
}

func NewGormZapLogger(zapLogger *zap.Logger, level logger.LogLevel, slowThreshold time.Duration) *GormZapLogger {
	return &GormZapLogger{
		logger:        zapLogger,
		level:         level,
		slowThreshold: slowThreshold,
	}
}

func (l *GormZapLogger) LogMode(level logger.LogLevel) logger.Interface {
	newLogger := *l
	newLogger.level = level
	return &newLogger
}

func (l *GormZapLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if l.level >= logger.Info {
		l.logger.Info(fmt.Sprintf(msg, data...))
	}
}

func (l *GormZapLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.level >= logger.Warn {
		l.logger.Warn(fmt.Sprintf(msg, data...))
	}
}

func (l *GormZapLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.level >= logger.Error {
		l.logger.Error(fmt.Sprintf(msg, data...))
	}
}

func (l *GormZapLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if l.level <= logger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	fields := []zap.Field{
		zap.Duration("elapsed", elapsed),
		zap.Int64("rows", rows),
		zap.String("sql", sql),
	}

	switch {
	case err != nil && l.level >= logger.Error:
		fields = append(fields, zap.Error(err))
		l.logger.Error("gorm", fields...)
	case l.slowThreshold != 0 && elapsed > l.slowThreshold && l.level >= logger.Warn:
		l.logger.Warn("slow sql", fields...)
	case l.level >= logger.Info:
		l.logger.Debug("sql", fields...)
	}
}

var _ logger.Interface = (*GormZapLogger)(nil)
