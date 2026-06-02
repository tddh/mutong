package interfaces

import (
	"context"

	nebula "github.com/vesoft-inc/nebula-go/v3"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

// Logger 日志接口
// 用途：抽象日志功能，使服务层不依赖具体的日志实现（如zap）
// 好处：可以在测试中替换为Mock Logger，实现解耦
// 实现者：config.ZapLoggerAdapter
type Logger interface {
	Debug(msg string, fields ...zap.Field)
	Info(msg string, fields ...zap.Field)
	Warn(msg string, fields ...zap.Field)
	Error(msg string, fields ...zap.Field)
}

// GraphDB 图数据库接口
// 用途：抽象图数据库操作，使服务层不依赖Nebula Graph具体实现
// 好处：可以替换为其他图数据库（如Neo4j），便于测试
// 实现者：config.NebulaGraphAdapter
type GraphDB interface {
	// Execute 执行nGQL查询，返回原始结果集
	Execute(query string) (*nebula.ResultSet, error)
	// ExecuteAndCheck 执行nGQL查询并检查结果，返回结果集
	ExecuteAndCheck(query string) (*nebula.ResultSet, error)
}

// Message 消息结构
// 用途：统一消息格式，屏蔽底层消息队列的差异
type Message struct {
	Key         []byte
	Value       []byte
	Topic       string
	Partition   int32
	Offset      int64
	LeaderEpoch int32
}

// FetchResult 拉取结果
// 用途：抽象消息拉取结果，支持错误处理和消息遍历
type FetchResult interface {
	// IsClosed 检查客户端是否已关闭
	IsClosed() bool
	// EachError 遍历错误
	EachError(fn func(topic string, partition int32, err error))
	// EachRecord 遍历消息记录
	EachRecord(fn func(msg *Message))
	// NumRecords 返回消息数量
	NumRecords() int
}

// MessageQueue 消息队列接口
// 用途：抽象消息队列功能，使服务层不依赖Kafka具体实现
// 好处：可以替换为其他消息队列（如RabbitMQ、Redis Stream），便于测试
// 实现者：config.KafkaAdapter
type MessageQueue interface {
	// Publish 同步发布消息到指定主题
	Publish(ctx context.Context, topic string, key string, message []byte) error
	// PollFetches 轮询拉取消息
	PollFetches(ctx context.Context) FetchResult
	// MarkCommit 标记消息为已提交
	MarkCommit(msg *Message)
	// PublishDeadLetter 发送消息到死信队列
	// 参数：originalMsg - 原始消息；errMsg - 错误信息
	// 返回值：发送错误，nil 表示成功
	PublishDeadLetter(ctx context.Context, originalMsg *Message, errMsg string) error
}

// CacheStats 缓存统计信息
// 用途：统一缓存统计格式，屏蔽底层缓存实现的差异
type CacheStats struct {
	Hits       int64
	Misses     int64
	DelHits    int64
	DelMisses  int64
	Collisions int64
}

// Cache 缓存接口
// 用途：抽象缓存功能，使服务层不依赖BigCache具体实现
// 好处：可以替换为Redis、Memcached等其他缓存实现，便于测试
// 实现者：config.BigCacheAdapter
type Cache interface {
	// Get 从缓存中获取值
	// 返回值：[]byte - 缓存的值；error - 键不存在或其他错误
	Get(key string) ([]byte, error)
	// Set 设置缓存值
	// 参数：key - 缓存键；value - 缓存值
	Set(key string, value []byte) error
	// Delete 删除缓存键
	// 返回值：error - 删除失败时的错误信息
	Delete(key string) error
	// Stats 获取缓存统计信息
	// 用途：监控缓存性能，了解命中率、冲突率等指标
	// 返回值：CacheStats - 缓存统计信息
	Stats() CacheStats
}

// Closer 可关闭资源接口
// 用途：统一资源关闭逻辑，确保连接正确释放
type Closer interface {
	Close() error
}

// KubernetesClient Kubernetes客户端接口
// 用途：抽象K8s客户端操作，使服务层不依赖client-go具体实现
// 好处：可以在测试中Mock K8s资源，不依赖真实集群
// 实现者：config.K8sClientAdapter
type KubernetesClient interface {
	ListResources(gvr schema.GroupVersionResource, namespace string, opts interface{}) ([]unstructured.Unstructured, error)
	WatchResources(gvr schema.GroupVersionResource, namespace string, handler cache.ResourceEventHandler) error
	GetAPIResources() ([]schema.GroupVersionResource, error)
}

// TraceIDProvider TraceID提供者接口
// 用途：抽象TraceID获取逻辑，用于分布式追踪
// 好处：可以在不同上下文（HTTP、gRPC）中提供统一的TraceID获取方式
type TraceIDProvider interface {
	GetTraceID() string
}

// BusinessContextProvider 业务上下文提供者接口
// 用途：为告警富化提供业务归属信息
type BusinessContextProvider interface {
	EnrichAlertByResourceUID(resourceUID string) BusinessAppContext
}

// BusinessAppContext 业务应用上下文
type BusinessAppContext struct {
	UID          string
	AppName      string
	Namespace    string
	Criticality  string
	Environment  string
	Team         string
	BusinessUnit string
	Source       string // 数据来源标识（如 "label-direct", "belongs-to-app", "owner-inherit", "namespace-mapping", "trace-derived", "node-infra", "unknown"）
}
