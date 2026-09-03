package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"gitee.com/tddh/mutong/config"
	"gitee.com/tddh/mutong/models"
	"github.com/allegro/bigcache/v3"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

// businessWorkloadProducer is the interface for producing messages to the
// business workload topic, enabling testability.
type businessWorkloadProducer interface {
	ProduceSync(ctx context.Context, records []*kgo.Record) produceResults
}

// produceResults wraps the result of a ProduceSync call.
type produceResults interface {
	FirstErr() error
}

// kgoProducer wraps *kgo.Client to implement businessWorkloadProducer.
type kgoProducer struct {
	client *kgo.Client
}

func (p *kgoProducer) ProduceSync(ctx context.Context, records []*kgo.Record) produceResults {
	return p.client.ProduceSync(ctx, records...)
}

func (d *K8sResoureService) Relationship(uid string) {
	query := `match (v:K8sResource) RETURN v.K8sResource.api_version as api_version,v.K8sResource.api_group as group ,v.K8sResource.labels as labels,v.K8sResource.name as name ,v.K8sResource.name_space as name_space,v.K8sResource.resource_define as resource_define ,v.K8sResource.kind as kind ,v.K8sResource.uid as uid ;`

	if uid != "" {
		// 使用 FETCH PROP ON 按 VID 直查存储层，不依赖 TAG INDEX，
		// 避免 INSERT 后因索引异步更新导致 MATCH {uid:xxx} 返回空结果。
		query = fmt.Sprintf(`FETCH PROP ON K8sResource %s YIELD properties(vertex).uid AS uid, properties(vertex).name AS name, properties(vertex).api_group AS `+"group"+`, properties(vertex).labels AS labels, properties(vertex).name_space AS name_space, properties(vertex).resource_define AS resource_define, properties(vertex).kind AS kind, properties(vertex).api_version AS api_version;`, strconv.Quote(uid))
	}

	d.logger.Debug("Relationship - Starting relationship rebuild",
		zap.String("uid", uid))
	resultSet, err := d.graphDB.Execute(query)
	if err != nil || resultSet == nil {
		d.logger.Error("Relationship", zap.Error(err))
		return
	}
	var k8sResources []models.K8sResource
	_ = resultSet.Scan(&k8sResources)

	d.logger.Debug("Relationship - Query completed",
		zap.Int("resourceCount", len(k8sResources)),
		zap.String("uid", uid))

	if len(k8sResources) == 0 && uid != "" {
		d.logger.Warn("Relationship", zap.String("uid", uid), zap.String("msg", "Resource not found immediately after write, retrying..."))
		maxRetries := 5
		retryInterval := 200 * time.Millisecond

		for i := 0; i < maxRetries; i++ {
			time.Sleep(retryInterval)
			resultSet, err = d.graphDB.Execute(query)
			if err != nil || resultSet == nil {
				d.logger.Error("Relationship retry", zap.Error(err), zap.Int("attempt", i+1))
				continue
			}
			k8sResources = []models.K8sResource{}
			_ = resultSet.Scan(&k8sResources)
			if len(k8sResources) > 0 {
				d.logger.Debug("Relationship retry success", zap.String("uid", uid), zap.Int("attempt", i+1),
					zap.Duration("total_wait_time", time.Duration(i+1)*retryInterval))
				break
			}
		}

		if len(k8sResources) == 0 {
			d.logger.Warn("Relationship retry failed", zap.String("uid", uid),
				zap.Int("max_retries", maxRetries),
				zap.Duration("retry_interval", retryInterval))
		}
	}

	for _, k8sResource := range k8sResources {
		unstructuredObj, err := d.unmarshalResourceDefine(k8sResource.ResourceDefine)
		if err != nil {
			d.logger.Error("Relationship", zap.Error(err))
			continue
		}
		d.processLabelsRelationship(unstructuredObj)
		d.processOwnerReferences(unstructuredObj)
		d.processNamespaceRelationship(unstructuredObj)

		if k8sResource.Kind == "Pod" {
			d.processPodRelationships(unstructuredObj)
		}
		if k8sResource.Kind == "PersistentVolume" {
			d.processPVCToPVToSCRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "Service" {
			d.processServiceToEndpointToPodRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "EndpointSlice" {
			d.processEndpointSliceToServiceToEndpointPodRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "StorageClass" {
			d.processClassStorageToCSIDriverRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "CSINode" {
			d.processCSINodeToNodeRelationship(unstructuredObj)
		}

		if k8sResource.Kind == "Event" && k8sResource.APIVersion == "events.k8s.io/v1" {
			d.processEventIoV1Relationship(unstructuredObj)
		}
		if k8sResource.Kind == "Event" && k8sResource.APIVersion == "core/v1" {
			d.processEventV1Relationship(unstructuredObj)
		}
		if k8sResource.Kind == "RoleBinding" {
			d.processRoleBindingRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "ClusterRoleBinding" {
			d.processClusterRoleRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "Ingress" {
			d.processIngressToServiceAndSecretRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "PodDisruptionBudget" {
			d.processPDBRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "HorizontalPodAutoscaler" {
			d.processHPARelationship(unstructuredObj)
		}
		if k8sResource.Kind == "Secret" {
			d.processSecretTokenRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "MutatingWebhookConfiguration" {
			d.processMutatingWebhookConfigurationRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "ValidatingWebhookConfiguration" {
			d.processValidatingWebhookConfigurationRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "NetworkPolicy" {
			d.processNetworkPolicyRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "VolumeAttachment" {
			d.processVolumeAttachmentRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "FlowSchema" {
			d.processFlowSchemaRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "CiliumEndpoint" {
			d.processCiliumEndpointRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "CustomResourceDefinition" {
			d.processCustomResourceDefinitionRelationship(unstructuredObj)
		}
		if k8sResource.Kind == "APIService" {
			d.processAPIServiceRelationship(unstructuredObj)
		}
	}

	if len(k8sResources) > 0 {
		d.logger.Debug("Relationship - Completed",
			zap.String("uid", uid),
			zap.String("kind", k8sResources[0].Kind),
			zap.String("name", k8sResources[0].Name))
	} else {
		d.logger.Debug("Relationship - Completed (no resources)",
			zap.String("uid", uid))
	}
}

// BackfillRelationshipsByKinds 对指定 kind 的所有存活顶点重跑 Relationship，
// 用于一次性回填新增的关系边（如 FlowSchema/CiliumEndpoint/CRD）。
// 幂等：各 process 函数重建前先清理同类型出边。
func (d *K8sResoureService) BackfillRelationshipsByKinds(kinds []string) {
	for _, kind := range kinds {
		query := "LOOKUP ON K8sResource WHERE K8sResource.kind == " + strconv.Quote(kind) +
			" AND K8sResource.is_deleted == false YIELD id(vertex) AS uid;"
		rows, err := d.executenGQL(query)
		if err != nil {
			d.logger.Error("BackfillRelationshipsByKinds: lookup failed",
				zap.String("kind", kind), zap.Error(err))
			continue
		}
		n := 0
		for _, row := range rows {
			uid := string(row.Values[0].GetSVal())
			if uid == "" {
				continue
			}
			d.Relationship(uid)
			n++
		}
		d.logger.Info("BackfillRelationshipsByKinds done",
			zap.String("kind", kind), zap.Int("count", n))
	}
}

func (d *K8sResoureService) Collect(name string) {
	d.initAPIResources()

	n := ""
	if name != "" {
		n = name
	} else {
		n = d.cluster.Context
	}

	if n == "" || d.cluster.K8sClusterClient[n] == nil {
		d.logger.Warn("Collect: no cluster configured, skipping")
		return
	}

	dynamicClient, err := dynamic.NewForConfig(d.cluster.K8sClusterClient[n].RootRestConfig)
	if err != nil {
		d.logger.Error("Collect", zap.Error(err))
		return
	}

	factory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, d.resyncInterval)
	stopCh := make(chan struct{})
	d.stopCh = stopCh

	d.startResourceUpdater(factory, n, stopCh)
}

func (d *K8sResoureService) CollectAllClusters(ctx context.Context) {
	d.initAPIResources()

	for name, client := range d.cluster.K8sClusterClient {
		go func(clusterName string, cc *config.K8sClusterClient) {
			d.collectSingleCluster(ctx, clusterName, cc)
		}(name, client)
	}
}

func (d *K8sResoureService) collectSingleCluster(ctx context.Context, clusterName string, cc *config.K8sClusterClient) {
	d.logger.Debug("Starting collection for cluster", zap.String("cluster", clusterName))

	dynamicClient, err := dynamic.NewForConfig(cc.RootRestConfig)
	if err != nil {
		d.logger.Error("Failed to create dynamic client for cluster",
			zap.String("cluster", clusterName), zap.Error(err))
		return
	}

	factory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, d.resyncInterval)
	stopCh := make(chan struct{})

	d.startResourceUpdater(factory, clusterName, stopCh)
}

func (d *K8sResoureService) CollectWithContext(ctx context.Context, name string) {
	d.logger.Info("CollectWithContext starting", zap.String("cluster", name))

	d.initAPIResources()

	n := ""
	if name != "" {
		n = name
	} else {
		n = d.cluster.Context
	}

	if n == "" || d.cluster.K8sClusterClient[n] == nil {
		d.logger.Warn("CollectWithContext: no cluster configured, skipping")
		return
	}

	dynamicClient, err := dynamic.NewForConfig(d.cluster.K8sClusterClient[n].RootRestConfig)
	if err != nil {
		d.logger.Error("Collect: failed to create dynamic client", zap.String("cluster", n), zap.Error(err))
		return
	}

	factory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, d.resyncInterval)
	stopCh := make(chan struct{})
	d.stopCh = stopCh

	d.logger.Info("CollectWithContext: entering startResourceUpdater", zap.String("cluster", n))

	go func() {
		defer func() {
			if r := recover(); r != nil {
				d.logger.Error("Context listener goroutine panic recovered",
					zap.Any("panic", r))
			}
		}()
		<-ctx.Done()
		d.logger.Debug("Context cancelled, closing stopCh for graceful shutdown")
		close(stopCh)
	}()

	d.startResourceUpdater(factory, n, stopCh)
}

func (d *K8sResoureService) startResourceUpdater(factory dynamicinformer.DynamicSharedInformerFactory, n string, stopCh chan struct{}) {
	d.logger.Debug("startResourceUpdater launched", zap.String("cluster", n))

	go func() {
		d.logger.Debug("Starting asynchronous initial full resource relationship processing")
		startTime := time.Now()
		d.Relationship("")
		duration := time.Since(startTime)
		d.logger.Debug("Completed initial full resource relationship processing",
			zap.Duration("duration", duration))
		d.markResourceReady()
	}()

	for {
		rlist, err := d.cluster.K8sClusterClient[n].RootDiscoveryClient.ServerPreferredResources()
		if err != nil {
			d.logger.Error("ServerPreferredResources failed", zap.String("cluster", n), zap.Error(err))
			time.Sleep(5 * time.Second)
			continue
		}
		d.logger.Info("ServerPreferredResources succeeded", zap.String("cluster", n), zap.Int("resourceCount", len(rlist)))
		d.updateAPIResourcesAndWait(factory, rlist, stopCh, n)
		time.Sleep(5 * time.Minute)
	}
}

func (d *K8sResoureService) updateAPIResourcesAndWait(factory dynamicinformer.DynamicSharedInformerFactory, rlist []*metav1.APIResourceList, stopCh chan struct{}, clusterName string) bool {
	var informersSynced []cache.InformerSynced
	isAdded := false

	for _, r := range rlist {
		gv, err := schema.ParseGroupVersion(r.GroupVersion)
		if err != nil {
			d.logger.Error("Collect", zap.Error(err))
			continue
		}
		for _, api := range r.APIResources {
			if contains(api.Verbs, "watch") {
				gvr := gv.WithResource(api.Name)
				resourceKey := gvr.String()

				if d.hasAPIResource(resourceKey) {
					continue
				}

				isAdded = true
				informer := factory.ForResource(gvr).Informer()
				d.logger.Debug("Registering informer",
					zap.String("gvr", resourceKey),
					zap.String("cluster", clusterName))

				d.addEventHandlers(informer, gv.Group, clusterName)

				d.setAPIResource(resourceKey, true)

				informersSynced = append(informersSynced, informer.HasSynced)
			}
		}
	}

	if isAdded {
		factory.Start(stopCh)
		if !cache.WaitForCacheSync(stopCh, informersSynced...) {
			d.logger.Error("Failed to sync informer cache")
			return false
		}
		d.SetInformerFactory(factory)
		d.logger.Info("updateAPIResourcesAndWait completed", zap.String("cluster", clusterName), zap.Bool("isAdded", isAdded), zap.Int("gvrCount", len(informersSynced)))
		return true
	}
	d.logger.Info("updateAPIResourcesAndWait: no new GVRs to register", zap.String("cluster", clusterName))
	return len(informersSynced) > 0
}

func (d *K8sResoureService) addEventHandlers(informer cache.SharedIndexInformer, group string, clusterName string) {
	_, _ = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			d.handleResourceEvent(obj, "Added", group, clusterName)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			unstructuredObjOld, _ := oldObj.(*unstructured.Unstructured)
			unstructuredObjNew, _ := newObj.(*unstructured.Unstructured)

			// DeepCopy before comparing — the original objects are owned by
			// the Informer's cache and MUST NOT be mutated. Other goroutines
			// (e.g. MCP listResourcesFromCache) may read them concurrently.
			oldCopy := unstructuredObjOld.DeepCopy()
			newCopy := unstructuredObjNew.DeepCopy()
			delete(oldCopy.Object, "status")
			delete(newCopy.Object, "status")

			if unstructuredObjNew.GetKind() != "Lease" && !equality.Semantic.DeepEqual(newCopy.Object, oldCopy.Object) {
				d.handleResourceEvent(newObj, "Updated", group, clusterName)
			}
		},
		DeleteFunc: func(obj interface{}) {
			d.handleResourceEvent(obj, "Deleted", group, clusterName)
		},
	})
}

func (d *K8sResoureService) Test(name string) {
	d.logger.Debug("ControllerService test", zap.String("name", name))

	if d.cluster.Context == "" || d.cluster.K8sClusterClient[d.cluster.Context] == nil {
		d.logger.Warn("Test: no cluster configured, skipping")
		return
	}

	podList, err := d.cluster.K8sClusterClient[d.cluster.Context].RootKubeClientSet.CoreV1().Pods("default").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		d.logger.Error("Failed to get pods", zap.Error(err))
		return
	}

	for _, pod := range podList.Items {
		for _, owner := range pod.OwnerReferences {
			d.logger.Debug("Pod owner reference",
				zap.String("pod", pod.Name),
				zap.String("owner", owner.Name),
				zap.String("kind", owner.Kind))
			p := pod.DeepCopy()
			p.Labels["owner"] = owner.Name
			_, _ = d.cluster.K8sClusterClient[d.cluster.Context].RootKubeClientSet.CoreV1().Pods("default").Update(context.TODO(), p, metav1.UpdateOptions{})
		}
	}

	rlist, _ := d.cluster.K8sClusterClient[d.cluster.Context].RootDiscoveryClient.ServerPreferredResources()
	for _, r := range rlist {
		d.logger.Debug("API resource list",
			zap.String("kind", r.Kind),
			zap.String("groupVersion", r.GroupVersion))
		for _, api := range r.APIResources {
			d.logger.Debug("API resource",
				zap.String("name", api.Name),
				zap.String("kind", api.Kind),
				zap.Bool("namespaced", api.Namespaced))
		}
	}
}

func (d *K8sResoureService) handleResourceEvent(obj any, eventType string, group string, clusterName string) {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok || unstructuredObj == nil {
		d.logger.Error("Failed to convert obj to *unstructured.Unstructured or obj is nil",
			zap.String("eventType", eventType),
			zap.String("group", group))
		return
	}

	jsonTxt, err := unstructuredObj.MarshalJSON()
	if err != nil {
		d.logger.Error("handleResourceEvent", zap.Error(err))
	}
	if eventType == "Added" || eventType == "Updated" {
		err = d.cache.Set(string(unstructuredObj.GetUID()), []byte(unstructuredObj.GetResourceVersion()))
		if err != nil {
			d.logger.Error("Failed to set resource in BigCache",
				zap.String("uid", string(unstructuredObj.GetUID())), zap.Error(err))
		}
		// UID cache: kind:ns:name -> uid
		cacheKey := fmt.Sprintf("uid:%s:%s:%s", unstructuredObj.GetKind(), unstructuredObj.GetNamespace(), unstructuredObj.GetName())
		if err := d.cache.Set(cacheKey, []byte(string(unstructuredObj.GetUID()))); err != nil {
			d.logger.Debug("Failed to set UID cache", zap.String("key", cacheKey), zap.Error(err))
		}
		// resource_define cache
		rdKey := "rd:" + cacheKey[4:] // same as uid: prefix → rd: prefix
		if err := d.cache.Set(rdKey, []byte(strconv.Quote(string(jsonTxt)))); err != nil {
			d.logger.Debug("Failed to set RD cache", zap.String("key", rdKey), zap.Error(err))
		}
		d.logger.Debug("Added or Updated resource",
			zap.String("uid", string(unstructuredObj.GetUID())),
			zap.String("name", unstructuredObj.GetName()),
			zap.String("group", group),
			zap.String("kind", unstructuredObj.GetKind()))
	} else {
		err = d.cache.Delete(string(unstructuredObj.GetUID()))
		if err != nil && err != bigcache.ErrEntryNotFound {
			d.logger.Error("Failed to delete resource from BigCache",
				zap.String("uid", string(unstructuredObj.GetUID())), zap.Error(err))
		}
		cacheKey := fmt.Sprintf("uid:%s:%s:%s", unstructuredObj.GetKind(), unstructuredObj.GetNamespace(), unstructuredObj.GetName())
		_ = d.cache.Delete(cacheKey)
		d.logger.Debug("Deleted resource",
			zap.String("uid", string(unstructuredObj.GetUID())),
			zap.String("name", unstructuredObj.GetName()),
			zap.String("group", group),
			zap.String("kind", unstructuredObj.GetKind()))
	}
	// Pre-compute uid before spawning goroutine — unstructuredObj is shared
	// by the K8s Informer and MUST NOT be accessed asynchronously.
	uid := string(unstructuredObj.GetUID())

	d.kafkaWg.Add(1)
	go func() {
		defer d.kafkaWg.Done()
		select {
		case d.kafkaSem <- struct{}{}:
		default:
			d.logger.Debug("Kafka semaphore saturated, waiting for capacity",
				zap.String("uid", uid))
			d.kafkaSem <- struct{}{}
		}
		defer func() { <-d.kafkaSem }()
		defer func() {
			if r := recover(); r != nil {
				d.logger.Error("seedToKafka goroutine panic recovered",
					zap.String("uid", uid),
					zap.Any("panic", r))
			}
		}()
		d.seedToKafka(uid, jsonTxt, group, eventType, clusterName)
	}()
}

func (d *K8sResoureService) publishToBusinessWorkloadTopic(jsonBytes []byte, uid, kind, name, eventType, group string) {
	d.logger.Debug("entered publishToBusinessWorkloadTopic",
		zap.String("kind", kind),
		zap.String("name", name),
		zap.String("uid", uid),
		zap.String("topic", d.businessWorkloadTopic),
		zap.String("eventType", eventType),
		zap.String("group", group))

	msg := models.KafkaResourceMessage{
		EventType: eventType,
		Group:     group,
		Object:    jsonBytes,
	}
	msgBytes, _ := json.Marshal(msg)

	record := &kgo.Record{
		Topic: d.businessWorkloadTopic,
		Key:   []byte(uid),
		Value: msgBytes,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	d.logger.Debug("about to call ProduceSync",
		zap.String("kind", kind),
		zap.String("name", name),
		zap.String("uid", uid),
		zap.String("topic", d.businessWorkloadTopic))

	if err := d.businessWorkloadProducer.ProduceSync(ctx, []*kgo.Record{record}).FirstErr(); err != nil {
		d.logger.Error("Failed to publish to business workload topic",
			zap.String("kind", kind),
			zap.String("name", name),
			zap.String("uid", uid),
			zap.String("topic", d.businessWorkloadTopic),
			zap.Error(err))
	} else {
		d.logger.Debug("Published to business workload topic",
			zap.String("kind", kind),
			zap.String("name", name),
			zap.String("uid", uid),
			zap.String("topic", d.businessWorkloadTopic))
	}
}
