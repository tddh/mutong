package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"gitee.com/tddh/mutong/config"
	"gitee.com/tddh/mutong/interfaces"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"
)

type K8sResoureService struct {
	logger         interfaces.Logger
	graphDB        interfaces.GraphDB
	cache          interfaces.Cache
	cfg            *config.Config
	resourceCache  *ResourceCache
	cluster        *config.Cluster
	messageQueue   interfaces.MessageQueue
	kafkaTopic     string
	apiResources   map[string]bool
	apiResourcesMu sync.RWMutex
	kafkaWg        sync.WaitGroup
	kafkaSem       chan struct{}
	maxRetries     int
	retryBackoff   time.Duration
	stopCh         chan struct{}
	resyncInterval time.Duration

	businessWorkloadProducer businessWorkloadProducer
	businessWorkloadTopic    string
	workloadKinds            map[string]bool
	bizPublishChan           chan bizPublishTask
	bizPublishDone           chan struct{}

	readyOnce sync.Once
	readyMu   sync.RWMutex
	ready     bool
	readyChan chan struct{}

	informerFactoryMu sync.RWMutex
	informerFactory   dynamicinformer.DynamicSharedInformerFactory
	factoryReady      chan struct{}
	factoryReadyOnce  sync.Once
}

func (d *K8sResoureService) markResourceReady() {
	d.readyOnce.Do(func() {
		d.logger.Info("K8s resource service ready")
		d.readyMu.Lock()
		d.ready = true
		d.readyChan = make(chan struct{})
		close(d.readyChan)
		d.readyMu.Unlock()
	})
}

func (d *K8sResoureService) WaitForResourceReady(ctx context.Context) error {
	d.readyMu.RLock()
	ch := d.readyChan
	ready := d.ready
	d.readyMu.RUnlock()

	if ready && ch != nil {
		select {
		case <-ch:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			d.readyMu.RLock()
			ch = d.readyChan
			ready = d.ready
			d.readyMu.RUnlock()
			if ready && ch != nil {
				return nil
			}
		}
	}
}

func NewK8sResourceService(
	logger interfaces.Logger,
	graphDB interfaces.GraphDB,
	cache interfaces.Cache,
	cluster *config.Cluster,
	messageQueue interfaces.MessageQueue,
	kafkaTopic string,
	_ interfaces.RoleInterface,
	_ interfaces.UserInterface,
	maxRetries int,
	retryBackoffMs int,
	cacheConfig *config.Cache,
	cfg *config.Config,
	bwProducer *kgo.Client,
	businessWorkloadTopic string,
	workloadKinds []string,
) interfaces.K8sResourceInterface {
	if maxRetries <= 0 {
		maxRetries = 5
	}
	retryBackoff := time.Duration(retryBackoffMs) * time.Millisecond
	if retryBackoff <= 0 {
		retryBackoff = 100 * time.Millisecond
	}

	resyncInterval := 30 * time.Minute

	var resourceCache *ResourceCache
	if cacheConfig != nil {
		if cacheConfig.ResyncInterval > 0 {
			resyncInterval = time.Duration(cacheConfig.ResyncInterval) * time.Minute
		}
		if cacheConfig.Enable {
			var err error
			resourceCache, err = NewResourceCache(
				logger,
				cacheConfig.LifeTime,
				cacheConfig.CleanWindow,
				cacheConfig.HardMaxCacheSize,
				true,
			)
			if err != nil {
				logger.Warn("Failed to initialize resource cache", zap.Error(err))
			}
		}
	}

	workloadKindsMap := make(map[string]bool)
	for _, kind := range workloadKinds {
		workloadKindsMap[kind] = true
	}

	var bwProducerIface businessWorkloadProducer
	if bwProducer != nil {
		bwProducerIface = &kgoProducer{client: bwProducer}
	}

	d := &K8sResoureService{
		logger:                   logger,
		graphDB:                  graphDB,
		cache:                    cache,
		cfg:                      cfg,
		resourceCache:            resourceCache,
		cluster:                  cluster,
		messageQueue:             messageQueue,
		kafkaTopic:               kafkaTopic,
		apiResources:             make(map[string]bool),
		kafkaSem:                 make(chan struct{}, cfg.Kafka.KafkaSemaphore),
		maxRetries:               maxRetries,
		retryBackoff:             retryBackoff,
		resyncInterval:           resyncInterval,
		businessWorkloadProducer: bwProducerIface,
		businessWorkloadTopic:    businessWorkloadTopic,
		workloadKinds:            workloadKindsMap,
		factoryReady:             make(chan struct{}),
		bizPublishChan:           make(chan bizPublishTask, cfg.Kafka.BizPublishChanBuffer),
		bizPublishDone:           make(chan struct{}),
	}
	d.startBizPublishWorker()

	return d
}

// hasAPIResource 线程安全地检查 apiResources 中是否存在指定 key
// 使用读锁保护并发读取
func (d *K8sResoureService) hasAPIResource(key string) bool {
	d.apiResourcesMu.RLock()
	defer d.apiResourcesMu.RUnlock()
	return d.apiResources[key]
}

// setAPIResource 线程安全地设置 apiResources 中的值
// 使用写锁保护并发写入
func (d *K8sResoureService) setAPIResource(key string, value bool) {
	d.apiResourcesMu.Lock()
	defer d.apiResourcesMu.Unlock()
	d.apiResources[key] = value
}

// initAPIResources 线程安全地初始化 apiResources
// 使用写锁保护并发写入
func (d *K8sResoureService) initAPIResources() {
	d.apiResourcesMu.Lock()
	defer d.apiResourcesMu.Unlock()
	if d.apiResources == nil {
		d.apiResources = make(map[string]bool)
	}
}

// WaitForKafkaSend 等待所有 Kafka 消息发送完成
// timeout: 最大等待时间，超时后放弃等待
// 此方法应在应用关闭前调用，确保消息不丢失
func (d *K8sResoureService) WaitForKafkaSend(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		d.kafkaWg.Wait()
		close(done)
	}()
	select {
	case <-done:
		d.logger.Debug("All Kafka messages sent successfully")
	case <-time.After(timeout):
		d.logger.Warn("Timeout waiting for Kafka messages, some may be lost",
			zap.Duration("timeout", timeout))
		// Goroutine will eventually exit when kafkaWg reaches zero.
		// No leak: the spawned goroutine is bounded by kafkaWg completion.
	}
}

func (d *K8sResoureService) Stop() {
	if d.stopCh != nil {
		close(d.stopCh)
		d.logger.Debug("K8s informer stop channel closed")
	}
	d.WaitForKafkaSend(10 * time.Second)
	if d.bizPublishChan != nil {
		close(d.bizPublishChan)
		<-d.bizPublishDone
		d.logger.Debug("Business publish worker drained and stopped")
	}
	d.logger.Debug("K8s resource service stopped gracefully")
}

func (d *K8sResoureService) mapToJson(m interface{}) []byte {
	jsonTxt, err := json.Marshal(m)
	if err != nil {
		d.logger.Error("map convert json error: %s ", zap.Error(err))
	}
	return jsonTxt
}

func (d *K8sResoureService) StringToInt64(s string) int64 {
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		d.logger.Error("StringToInt64", zap.Error(err))
		return 0
	}
	return i
}

func convertPointerToBool(ptr *bool) bool {
	if ptr == nil {
		return false
	}
	return *ptr
}

func FromUnstructured(unstr *unstructured.Unstructured, obj runtime.Object) error {
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstr.UnstructuredContent(), obj)
	if err != nil {
		return fmt.Errorf("failed to convert %T to %T: %v", unstr, obj, err)
	}
	return nil
}

func (d *K8sResoureService) SetInformerFactory(f dynamicinformer.DynamicSharedInformerFactory) {
	d.informerFactoryMu.Lock()
	d.informerFactory = f
	d.informerFactoryMu.Unlock()
	d.factoryReadyOnce.Do(func() { close(d.factoryReady) })
}

func (d *K8sResoureService) GetInformerFactory() dynamicinformer.DynamicSharedInformerFactory {
	select {
	case <-d.factoryReady:
	case <-time.After(30 * time.Second):
		return nil
	}
	d.informerFactoryMu.RLock()
	defer d.informerFactoryMu.RUnlock()
	return d.informerFactory
}
