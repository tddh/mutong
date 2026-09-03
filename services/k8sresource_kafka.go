package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func humanBytes(b int) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

type bizPublishTask struct {
	jsonBytes []byte
	uid       string
	kind      string
	name      string
	eventType string
	group     string
}

func (d *K8sResoureService) startBizPublishWorker() {
	go func() {
		defer close(d.bizPublishDone)
		for task := range d.bizPublishChan {
			d.publishToBusinessWorkloadTopic(task.jsonBytes, task.uid, task.kind, task.name, task.eventType, task.group)
		}
	}()
}

func (d *K8sResoureService) seedToKafka(key string, obj []byte, group string, eventType string, cluster string) {
	if d.messageQueue == nil {
		d.logger.Error("Message queue not initialized")
		return
	}

	msg := models.KafkaResourceMessage{
		EventType: eventType,
		Group:     group,
		Cluster:   cluster,
		Object:    obj,
	}

	jsonBytes, err := json.Marshal(msg)
	if err != nil {
		d.logger.Error("Failed to marshal message to JSON", zap.Error(err))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(d.cfg.Kafka.PublishTimeoutSec)*time.Second)
	defer cancel()

	msgSize := len(jsonBytes)
	objSize := len(obj)
	err = d.messageQueue.Publish(ctx, d.kafkaTopic, key, jsonBytes)
	if err != nil {
		d.logger.Error("Failed to produce message to Kafka",
			zap.String("key", key),
			zap.String("topic", d.kafkaTopic),
			zap.String("eventType", eventType),
			zap.String("group", group),
			zap.Int("messageSizeBytes", msgSize),
			zap.String("messageSizeHuman", humanBytes(msgSize)),
			zap.Int("rawObjectSizeBytes", objSize),
			zap.String("rawObjectSizeHuman", humanBytes(objSize)),
			zap.Float64("messageSizeMB", float64(msgSize)/1024/1024),
			zap.Error(err))
	} else {
		d.logger.Debug("Successfully produced message to Kafka",
			zap.String("key", key), zap.String("eventType", eventType))
	}

	d.logger.Debug("seedToKafka completed", zap.String("key", key), zap.String("eventType", eventType))
}

func (d *K8sResoureService) ConsumeKafkaMessages() {
	ctx := context.Background()
	d.ConsumeKafkaMessagesWithContext(ctx)
}

func (d *K8sResoureService) ConsumeKafkaMessagesWithContext(ctx context.Context) {
	d.logger.Info("Starting Kafka message consumer")

	if d.messageQueue == nil {
		d.logger.Error("Message queue not initialized")
		return
	}

	workerPoolSize := d.cfg.Kafka.WorkerPoolSize
	taskChan := make(chan *interfaces.Message, d.cfg.Kafka.TaskChanBuffer)

	var wg sync.WaitGroup

	defer func() {
		close(taskChan)
		wg.Wait()
		d.logger.Info("Kafka message consumer fully stopped")
	}()

	for i := 0; i < workerPoolSize; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					d.logger.Error("Worker goroutine panic recovered",
						zap.Int("workerID", workerID),
						zap.Any("panic", r))
				}
			}()
			d.logger.Debug("Starting worker", zap.Int("workerID", workerID))
			for msg := range taskChan {
				retryCount := 0
				success := false
				for {
					b := d.processKafkaMessageFromInterface(msg)
					if b {
						success = true
						break
					}

					retryCount++
					if retryCount >= d.maxRetries {
						dlErr := d.messageQueue.PublishDeadLetter(ctx, msg, fmt.Sprintf("max retries (%d) exceeded", retryCount))
						if dlErr != nil {
							d.logger.Error("Failed to send to dead letter queue, will NOT commit - message can be retried",
								zap.String("uid", string(msg.Key)),
								zap.Error(dlErr))
						} else {
							success = true
						}
						break
					}

					backoff := time.Duration(1<<uint(retryCount-1)) * d.retryBackoff //nolint:gosec
					maxBackoff := time.Duration(d.cfg.Kafka.MaxBackoffMs) * time.Millisecond
					if backoff > maxBackoff {
						backoff = maxBackoff
					}
					d.logger.Warn("Message processing failed, retrying",
						zap.String("uid", string(msg.Key)),
						zap.Int("retryCount", retryCount),
						zap.Duration("backoff", backoff))
					select {
					case <-ctx.Done():
						d.logger.Warn("Context cancelled, abandoning message",
							zap.String("uid", string(msg.Key)))
						return
					case <-time.After(backoff):
					}
				}
				if success {
					d.messageQueue.MarkCommit(msg)
				}
			}
			d.logger.Debug("Worker stopped", zap.Int("workerID", workerID))
		}(i)

	}

	for {
		select {
		case <-ctx.Done():
			d.logger.Debug("Context cancelled, stopping fetcher")
			return
		default:
			d.logger.Debug("[KAFKA_CONSUMER] Calling PollFetches")
			fetches := d.messageQueue.PollFetches(ctx)
			if fetches.IsClosed() {
				d.logger.Debug("Kafka client closed, stopping fetcher")
				return
			}

			fetches.EachError(func(topic string, partition int32, err error) {
				// "lost records" 是 rebalance 握手阶段的过渡状态（Redpanda offset 同步差异），
				// 实际消费从上次中断位置继续，offset 未丢失。静默跳过。
				if strings.Contains(err.Error(), "lost records") {
					return
				}
				d.logger.Error("Error fetching from Kafka",
					zap.String("topic", topic),
					zap.Int32("partition", partition),
					zap.Error(err))
			})

			d.logger.Debug("[KAFKA_CONSUMER] PollFetches returned, iterating records")
			fetches.EachRecord(func(msg *interfaces.Message) {
				d.logger.Debug("[KAFKA_CONSUMER] Got record from PollFetches",
					zap.String("topic", msg.Topic),
					zap.Int32("partition", msg.Partition),
					zap.Int("keyLen", len(msg.Key)),
					zap.Int("valueLen", len(msg.Value)))
				select {
				case taskChan <- msg:
					d.logger.Debug("Enqueued message",
						zap.String("key", string(msg.Key)),
						zap.String("topic", msg.Topic))
				case <-ctx.Done():
					return
				}
			})
		}
	}
}

func (d *K8sResoureService) processKafkaMessageFromInterface(msg *interfaces.Message) bool {
	d.logger.Debug("Processing Kafka record",
		zap.String("key", string(msg.Key)),
		zap.String("topic", msg.Topic))

	var kafkaMsg models.KafkaResourceMessage
	err := json.Unmarshal(msg.Value, &kafkaMsg)
	if err != nil {
		d.logger.Error("Failed to unmarshal Kafka message",
			zap.String("key", string(msg.Key)),
			zap.Error(err))
		return false
	}

	var unstructuredObj unstructured.Unstructured
	err = unstructuredObj.UnmarshalJSON(kafkaMsg.Object)
	if err != nil {
		d.logger.Error("Failed to unmarshal object data",
			zap.String("key", string(msg.Key)),
			zap.Error(err))
		return false
	}

	return d.processKafkaMessage(&unstructuredObj, kafkaMsg.EventType, kafkaMsg.Group, kafkaMsg.Cluster)
}

func (d *K8sResoureService) processKafkaMessage(unstructuredObj *unstructured.Unstructured, eventType string, group string, cluster string) bool {
	d.logger.Debug("Processing Kafka message",
		zap.String("eventType", eventType),
		zap.String("name", unstructuredObj.GetName()),
		zap.String("namespace", unstructuredObj.GetNamespace()),
		zap.String("kind", unstructuredObj.GetKind()),
		zap.String("apiVersion", unstructuredObj.GetAPIVersion()))

	jsonTxt, err := unstructuredObj.MarshalJSON()
	if err != nil {
		d.logger.Error("Failed to marshal object to JSON", zap.Error(err))
		return false
	}

	switch eventType {
	case "Added":

		resource := models.K8sResource{
			Uid:            string(unstructuredObj.GetUID()),
			Cluster:        cluster,
			Name:           unstructuredObj.GetName(),
			Group:          group,
			NameSpace:      unstructuredObj.GetNamespace(),
			Labels:         mapToKeyValuePairs(unstructuredObj.GetLabels()),
			ResourceDefine: string(jsonTxt),
			Kind:           unstructuredObj.GetKind(),
			APIVersion:     unstructuredObj.GetAPIVersion(),
			IsDeleted:      false,
		}

		insertQuery := insertK8sResourceToNebula(resource)
		_, err := d.graphDB.ExecuteAndCheck(insertQuery)
		if err != nil {
			d.logger.Error("Failed to insert resource to Nebula", zap.String("query", insertQuery), zap.Error(err))
			return false
		} else {
			d.logger.Debug("Successfully inserted resource to Nebula",
				zap.String("uid", resource.Uid), zap.String("name", resource.Name))
		}

		checkQuery := fmt.Sprintf(`GO FROM %s OVER * LIMIT 1`, strconv.Quote(string(unstructuredObj.GetUID())))
		checkResult, err := d.graphDB.Execute(checkQuery)
		if err != nil || checkResult == nil || len(checkResult.GetRows()) == 0 {
			d.logger.Debug("CleanupAllOutgoingEdgesExcept - Skipped (no outgoing edges)",
				zap.String("name", unstructuredObj.GetName()),
				zap.String("kind", unstructuredObj.GetKind()))
		} else {
			cleanupStart := time.Now()
			d.CleanupAllOutgoingEdgesExcept(string(unstructuredObj.GetUID()), "BelongsToApp")
			d.logger.Debug("CleanupAllOutgoingEdgesExcept completed",
				zap.String("name", unstructuredObj.GetName()),
				zap.String("kind", unstructuredObj.GetKind()),
				zap.Duration("duration", time.Since(cleanupStart)))
		}

		relStart := time.Now()
		d.Relationship(string(unstructuredObj.GetUID()))
		d.logger.Debug("Relationship completed",
			zap.String("name", unstructuredObj.GetName()),
			zap.String("kind", unstructuredObj.GetKind()),
			zap.Duration("duration", time.Since(relStart)))

	case "Updated":
		if oldRes, err := d.cache.Get(string(unstructuredObj.GetUID())); err != nil {
			d.logger.Debug("Failed to get resource from BigCache", zap.Error(err))
			return true
		} else {
			if string(oldRes) != unstructuredObj.GetResourceVersion() {
				d.logger.Debug("Resource version not match",
					zap.String("uid", string(unstructuredObj.GetUID())),
					zap.String("oldVersion", string(oldRes)),
					zap.String("newVersion", unstructuredObj.GetResourceVersion()))
				return true
			}
		}
		resource := models.K8sResource{
			Uid:            string(unstructuredObj.GetUID()),
			Cluster:        cluster,
			Name:           unstructuredObj.GetName(),
			Group:          group,
			NameSpace:      unstructuredObj.GetNamespace(),
			Labels:         mapToKeyValuePairs(unstructuredObj.GetLabels()),
			ResourceDefine: string(jsonTxt),
			Kind:           unstructuredObj.GetKind(),
			APIVersion:     unstructuredObj.GetAPIVersion(),
			IsDeleted:      false,
		}

		insertQuery := insertK8sResourceToNebula(resource)
		_, err := d.graphDB.ExecuteAndCheck(insertQuery)
		if err != nil {
			d.logger.Error("Failed to update resource in Nebula", zap.String("query", insertQuery), zap.Error(err))
			return false
		} else {
			d.logger.Debug("Successfully updated resource in Nebula",
				zap.String("uid", resource.Uid), zap.String("name", resource.Name))
		}

		d.CleanupAllOutgoingEdgesExcept(string(unstructuredObj.GetUID()), "BelongsToApp")
		d.Relationship(string(unstructuredObj.GetUID()))

	case "Deleted":
		uid := string(unstructuredObj.GetUID())
		if d.isAlreadyDeleted(uid) {
			d.logger.Debug("Skip replay deleted resource", zap.String("uid", uid))
			return true
		}

		resource := models.K8sResource{
			Uid:            string(unstructuredObj.GetUID()),
			Cluster:        cluster,
			Name:           unstructuredObj.GetName(),
			Group:          group,
			NameSpace:      unstructuredObj.GetNamespace(),
			Labels:         mapToKeyValuePairs(unstructuredObj.GetLabels()),
			ResourceDefine: string(jsonTxt),
			Kind:           unstructuredObj.GetKind(),
			APIVersion:     unstructuredObj.GetAPIVersion(),
			IsDeleted:      true,
			DeletedAt:      time.Now().Unix(),
		}

		insertQuery := insertK8sResourceToNebula(resource)
		_, err = d.graphDB.ExecuteAndCheck(insertQuery)
		if err != nil {
			d.logger.Error("Failed to update resource in Nebula", zap.String("query", insertQuery), zap.Error(err))
			return false
		} else {
			d.logger.Debug("Successfully marked resource deleted in Nebula",
				zap.String("uid", resource.Uid), zap.String("name", resource.Name))
		}

		d.CleanupAllOutgoingEdges(string(unstructuredObj.GetUID()))
	default:
		d.logger.Warn("Unknown event type", zap.String("eventType", eventType))
	}

	if d.businessWorkloadProducer != nil && d.businessWorkloadTopic != "" {
		if d.workloadKinds[unstructuredObj.GetKind()] {
			jsonBytes, err := unstructuredObj.MarshalJSON()
			if err != nil {
				d.logger.Error("Failed to marshal for business topic", zap.Error(err))
			} else {
				select {
				case d.bizPublishChan <- bizPublishTask{
					jsonBytes: jsonBytes,
					uid:       string(unstructuredObj.GetUID()),
					kind:      unstructuredObj.GetKind(),
					name:      unstructuredObj.GetName(),
					eventType: eventType,
					group:     group,
				}:
				default:
					d.logger.Warn("Business publish channel full, dropping message",
						zap.String("name", unstructuredObj.GetName()),
						zap.String("kind", unstructuredObj.GetKind()))
				}
			}
		}
	}

	d.invalidateMetadataCache()
	return true
}
