package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/allegro/bigcache/v3"
	"go.uber.org/zap"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	eventsv1 "k8s.io/api/events/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	scv1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func (d *K8sResoureService) unmarshalResourceDefine(resourceDefine string) (*unstructured.Unstructured, error) {
	resForJson, err := strconv.Unquote(resourceDefine)
	if err != nil {
		return nil, err
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(resForJson), &obj); err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: obj}, nil
}

func generateLabelUID(key, value string) string {
	return fmt.Sprintf("label-%s-%s", key, value)
}

func mapToKeyValuePairs(m map[string]string) string {
	pairs := make([]string, 0, len(m))

	for k, v := range m {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}

	return strings.Join(pairs, ",")
}

func (d *K8sResoureService) processLabelsRelationship(unstructuredObj *unstructured.Unstructured) {
	labels := unstructuredObj.GetLabels()
	if len(labels) == 0 {
		return
	}

	k8sResourceUID := string(unstructuredObj.GetUID())

	for key, labelValue := range labels {
		labelUID := generateLabelUID(key, labelValue)

		cacheValue, err := d.cache.Get("ExcludeLabels-" + key)
		if err != nil && err != bigcache.ErrEntryNotFound {
			d.logger.Error("processLabelsRelationship", zap.Error(err))
		} else if err == bigcache.ErrEntryNotFound {
			d.logger.Debug("ExcludeLabels", zap.String("key", key), zap.String("value", string(cacheValue)))
		} else if string(cacheValue) == "" || bytes.Equal(cacheValue, []byte(labelValue)) {
			d.logger.Debug("ExcludeLabels", zap.String("key", key), zap.String("value", string(cacheValue)))
			continue
		}

		labelQuery := fmt.Sprintf(`INSERT VERTEX IF NOT EXISTS Label(uid, key, value) VALUES "%s":("%s", "%s", "%s");`,
			labelUID,
			labelUID,
			key,
			labelValue)
		d.logger.Debug("processLabelsRelationship", zap.String("nGQL", labelQuery))
		_, err = d.graphDB.Execute(labelQuery)
		if err != nil {
			d.logger.Error("processLabelsRelationship", zap.Error(err))
			continue
		}

		_ = d.insertEdge("BelongsToLabel", k8sResourceUID, labelUID)
	}
}

// processWebhookConfigurationRelationship 统一处理
// MutatingWebhookConfiguration 和 ValidatingWebhookConfiguration
// 遍历所有 webhooks，建立 WebhookRefSvc 边指向 clientConfig.service
func (d *K8sResoureService) processWebhookConfigurationRelationship(unstructuredObj *unstructured.Unstructured) {
	obj := unstructuredObj.Object
	webhooksRaw, ok := obj["webhooks"]
	if !ok {
		return
	}
	webhooks, ok := webhooksRaw.([]interface{})
	if !ok || len(webhooks) == 0 {
		return
	}

	d.CleanupOutgoingEdgesByType(string(unstructuredObj.GetUID()), "WebhookRefSvc")

	defaultNS := unstructuredObj.GetNamespace()

	for _, whRaw := range webhooks {
		wh, ok := whRaw.(map[string]interface{})
		if !ok {
			continue
		}

		whName, _ := wh["name"].(string)

		ccRaw, ok := wh["clientConfig"].(map[string]interface{})
		if !ok {
			continue
		}

		svcRaw, ok := ccRaw["service"].(map[string]interface{})
		if !ok {
			continue // url 模式, 不是 service 引用
		}

		svcName, _ := svcRaw["name"].(string)
		if svcName == "" {
			continue
		}

		svcNS, _ := svcRaw["namespace"].(string)
		if svcNS == "" {
			svcNS = defaultNS
		}

		svcUID, found := d.lookupUID("Service", svcNS, svcName)
		if !found {
			d.logger.Debug("processWebhookConfigurationRelationship: Service not found",
				zap.String("webhook", whName),
				zap.String("serviceNS", svcNS),
				zap.String("serviceName", svcName))
			continue
		}

		path := ""
		if p, ok := svcRaw["path"].(string); ok {
			path = p
		}
		port := int32(0)
		if p, ok := svcRaw["port"].(float64); ok {
			port = int32(p)
		}

		query := fmt.Sprintf(
			"INSERT EDGE WebhookRefSvc(webhook_name, path, port) VALUES %s -> %s:(%s, %s, %d);",
			strconv.Quote(string(unstructuredObj.GetUID())),
			strconv.Quote(svcUID),
			strconv.Quote(whName),
			strconv.Quote(path),
			port,
		)
		d.logger.Debug("processWebhookConfigurationRelationship",
			zap.String("nGQL", query))
		_, err := d.graphDB.Execute(query)
		if err != nil {
			d.logger.Error("Failed to insert WebhookRefSvc edge", zap.Error(err))
		}
	}
}

func (d *K8sResoureService) processMutatingWebhookConfigurationRelationship(unstructuredObj *unstructured.Unstructured) {
	d.processWebhookConfigurationRelationship(unstructuredObj)
}

func (d *K8sResoureService) processValidatingWebhookConfigurationRelationship(unstructuredObj *unstructured.Unstructured) {
	d.processWebhookConfigurationRelationship(unstructuredObj)
}

func (d *K8sResoureService) processNetworkPolicyRelationship(unstructuredObj *unstructured.Unstructured) {
	var np networkingv1.NetworkPolicy
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &np); err != nil {
		d.logger.Error("processNetworkPolicyRelationship", zap.Error(err))
		return
	}

	npUID := string(unstructuredObj.GetUID())

	d.CleanupOutgoingEdgesByType(npUID, "NpSelectsByLabel")
	d.CleanupOutgoingEdgesByType(npUID, "NpSelectsNs")

	for k, v := range np.Spec.PodSelector.MatchLabels {
		labelUID := generateLabelUID(k, v)
		_ = d.insertEdge("NpSelectsByLabel", npUID, labelUID)
	}

	for _, rule := range np.Spec.Ingress {
		for _, peer := range rule.From {
			d.processNetworkPolicyPeer(npUID, peer)
		}
	}

	for _, rule := range np.Spec.Egress {
		for _, peer := range rule.To {
			d.processNetworkPolicyPeer(npUID, peer)
		}
	}
}

func (d *K8sResoureService) processNetworkPolicyPeer(npUID string, peer networkingv1.NetworkPolicyPeer) {
	if peer.PodSelector != nil {
		for k, v := range peer.PodSelector.MatchLabels {
			labelUID := generateLabelUID(k, v)
			_ = d.insertEdge("NpSelectsByLabel", npUID, labelUID)
		}
	}

	if peer.NamespaceSelector != nil {
		for nsKey, nsValue := range peer.NamespaceSelector.MatchLabels {
			d.addNpNamespaceEdge(npUID, nsKey, nsValue)
		}
	}
}

func (d *K8sResoureService) addNpNamespaceEdge(npUID, labelKey, labelValue string) {
	labelUID := generateLabelUID(labelKey, labelValue)

	query := fmt.Sprintf(
		"GO FROM %s OVER BelongsToLabel REVERSELY YIELD dst(edge) AS resource_uid LIMIT 100;",
		strconv.Quote(labelUID),
	)

	rows, err := d.executenGQL(query)
	if err != nil {
		d.logger.Error("addNpNamespaceEdge: query failed",
			zap.String("label", fmt.Sprintf("%s=%s", labelKey, labelValue)),
			zap.Error(err))
		return
	}

	for _, row := range rows {
		resourceUID := string(row.Values[0].GetSVal())

		verifyQuery := fmt.Sprintf(
			"FETCH PROP ON K8sResource %s YIELD properties(vertex).kind AS kind;",
			strconv.Quote(resourceUID),
		)
		verifyRows, err := d.executenGQL(verifyQuery)
		if err != nil || len(verifyRows) == 0 {
			continue
		}
		if string(verifyRows[0].Values[0].GetSVal()) == "Namespace" {
			_ = d.insertEdge("NpSelectsNs", npUID, resourceUID)
		}
	}
}

func (d *K8sResoureService) processPDBRelationship(unstructuredObj *unstructured.Unstructured) {
	var obj policyv1.PodDisruptionBudget

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &obj); err != nil {
		d.logger.Error("processPDBRelationship", zap.Error(err))
		return
	}

	// 通过 BelongsToLabel 边反查匹配的 Pod（仅处理 MatchLabels）
	matchedPodUIDs := make(map[string]bool)

	for key, value := range obj.Spec.Selector.MatchLabels {
		labelUID := generateLabelUID(key, value)

		query := fmt.Sprintf("GO FROM %s OVER BelongsToLabel REVERSELY YIELD dst(edge) AS resource_uid;",
			strconv.Quote(labelUID))

		rows, err := d.executenGQL(query)
		if err != nil {
			d.logger.Error("processPDBRelationship: failed to query BelongsToLabel",
				zap.String("label", fmt.Sprintf("%s=%s", key, value)), zap.Error(err))
			return
		}

		for _, row := range rows {
			resourceUID := string(row.Values[0].GetSVal())
			if matchedPodUIDs[resourceUID] {
				continue
			}
			matchedPodUIDs[resourceUID] = true
		}
	}

	if len(matchedPodUIDs) == 0 {
		d.logger.Debug("processPDBRelationship: no pods matched",
			zap.String("pdb", obj.Name),
			zap.Any("matchLabels", obj.Spec.Selector.MatchLabels))
		return
	}

	// 过滤：只保留 namespace 匹配的 Pod
	var podUIDs []string
	for resourceUID := range matchedPodUIDs {
		verifyQuery := fmt.Sprintf(
			"FETCH PROP ON K8sResource %s YIELD properties(vertex).kind AS kind, properties(vertex).name_space AS ns;",
			strconv.Quote(resourceUID),
		)
		verifyRows, err := d.executenGQL(verifyQuery)
		if err != nil || len(verifyRows) == 0 {
			continue
		}
		kind := string(verifyRows[0].Values[0].GetSVal())
		ns := string(verifyRows[0].Values[1].GetSVal())
		if kind == "Pod" && ns == obj.Namespace {
			podUIDs = append(podUIDs, resourceUID)
		}
	}

	if len(podUIDs) == 0 {
		d.logger.Debug("processPDBRelationship: no pods matched in namespace",
			zap.String("pdb", obj.Name),
			zap.String("namespace", obj.Namespace))
		return
	}

	// 先清理旧边，再建新边
	d.CleanupOutgoingEdgesByType(string(unstructuredObj.GetUID()), "PdbToPod")

	var edgeInserts []string
	for _, podUID := range podUIDs {
		edgeInserts = append(edgeInserts, fmt.Sprintf("%s->%s:()",
			strconv.Quote(string(unstructuredObj.GetUID())),
			strconv.Quote(podUID)))
	}

	insertQuery := fmt.Sprintf("INSERT EDGE PdbToPod () VALUES %s;", strings.Join(edgeInserts, ", "))
	d.logger.Debug("processPDBRelationship", zap.String("pdb", obj.Name))
	_, err := d.graphDB.Execute(insertQuery)
	if err != nil {
		d.logger.Error("Failed to insert PdbToPod edges", zap.Error(err))
	}
}

func (d *K8sResoureService) processHPARelationship(unstructuredObj *unstructured.Unstructured) {
	var hpa autoscalingv2.HorizontalPodAutoscaler

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &hpa); err != nil {
		d.logger.Error("processHPARelationship", zap.Error(err))
		return
	}

	scaleTarget := hpa.Spec.ScaleTargetRef

	targetUID, found := d.lookupUID(scaleTarget.Kind, hpa.Namespace, scaleTarget.Name)
	if !found {
		d.logger.Debug("processHPARelationship: target not found",
			zap.String("hpa", hpa.Name),
			zap.String("targetKind", scaleTarget.Kind),
			zap.String("targetName", scaleTarget.Name))
		return
	}

	d.CleanupOutgoingEdgesByType(string(unstructuredObj.GetUID()), "AutoScales")

	minReplicas := int32(0)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}

	query := fmt.Sprintf(
		"INSERT EDGE AutoScales(scale_target_kind, scale_target_name, min_replicas, max_replicas, current_replicas) VALUES %s -> %s:(%s, %s, %d, %d, %d);",
		strconv.Quote(string(unstructuredObj.GetUID())),
		strconv.Quote(targetUID),
		strconv.Quote(scaleTarget.Kind),
		strconv.Quote(scaleTarget.Name),
		minReplicas,
		hpa.Spec.MaxReplicas,
		hpa.Status.CurrentReplicas,
	)
	d.logger.Debug("processHPARelationship", zap.String("hpa", hpa.Name))
	_, err := d.graphDB.Execute(query)
	if err != nil {
		d.logger.Error("Failed to insert AutoScales edge", zap.Error(err))
	}
}

func (d *K8sResoureService) processSecretTokenRelationship(unstructuredObj *unstructured.Unstructured) {
	var secret corev1.Secret

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &secret); err != nil {
		d.logger.Error("processSecretTokenRelationship", zap.Error(err))
		return
	}

	// 只处理 kubernetes.io/service-account-token 类型的 Secret
	if secret.Type != "kubernetes.io/service-account-token" {
		return
	}

	saName, ok := secret.Annotations["kubernetes.io/service-account.name"]
	if !ok || saName == "" {
		return
	}

	// 通过 Nebula 查找 ServiceAccount 的 UID
	saUID, found := d.lookupUID("ServiceAccount", secret.Namespace, saName)
	if !found {
		d.logger.Debug("processSecretTokenRelationship: SA not found",
			zap.String("secret", secret.Name),
			zap.String("saName", saName))
		return
	}

	// 复用 MountsSecret 边：SA → Secret
	query := fmt.Sprintf("INSERT EDGE MountsSecret () VALUES %s -> %s:();",
		strconv.Quote(saUID),
		strconv.Quote(string(unstructuredObj.GetUID())))
	d.logger.Debug("processSecretTokenRelationship",
		zap.String("sa", saName),
		zap.String("secret", secret.Name))
	_, err := d.graphDB.Execute(query)
	if err != nil {
		d.logger.Error("Failed to insert MountsSecret edge for SA token", zap.Error(err))
	}
}

func (d *K8sResoureService) processClusterRoleRelationship(unstructuredObj *unstructured.Unstructured) {
	var obj rbacv1.ClusterRoleBinding

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &obj); err != nil {
		d.logger.Error("processClusterRoleRelationship", zap.Error(err))
		return
	}

	if clusterRoleUID, found := d.lookupUID("ClusterRole", "", obj.Name); found {
		_ = d.insertEdge("BelongsToClusterRole", string(unstructuredObj.GetUID()), clusterRoleUID)
	}

	for _, subject := range obj.Subjects {
		switch subject.Kind {
		case "User":
			if uid, found := d.lookupUID("User", subject.Namespace, subject.Name); found {
				_ = d.insertEdge("BelongsToUser", string(unstructuredObj.GetUID()), uid)
			}
		case "Group":
			if uid, found := d.lookupUID("Group", subject.Namespace, subject.Name); found {
				_ = d.insertEdge("BelongsToGroup", string(unstructuredObj.GetUID()), uid)
			}
		case "ServiceAccount":
			if uid, found := d.lookupUID("ServiceAccount", subject.Namespace, subject.Name); found {
				_ = d.insertEdge("BelongsToServiceAccount", string(unstructuredObj.GetUID()), uid)
			}
		}
	}
}

func (d *K8sResoureService) processRoleBindingRelationship(unstructuredObj *unstructured.Unstructured) {
	var obj rbacv1.RoleBinding

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &obj); err != nil {
		d.logger.Error("processRoleBindingRelationship", zap.Error(err))
		return
	}

	if uid, found := d.lookupUID("Role", obj.Namespace, obj.RoleRef.Name); found {
		_ = d.insertEdge("BelongsToRole", string(unstructuredObj.GetUID()), uid)
	}

	for _, subject := range obj.Subjects {
		switch subject.Kind {
		case "User":
			if uid, found := d.lookupUID("User", subject.Namespace, subject.Name); found {
				_ = d.insertEdge("BelongsToUser", string(unstructuredObj.GetUID()), uid)
			}
		case "Group":
			if uid, found := d.lookupUID("Group", subject.Namespace, subject.Name); found {
				_ = d.insertEdge("BelongsToGroup", string(unstructuredObj.GetUID()), uid)
			}
		case "ServiceAccount":
			if uid, found := d.lookupUID("ServiceAccount", subject.Namespace, subject.Name); found {
				_ = d.insertEdge("BelongsToServiceAccount", string(unstructuredObj.GetUID()), uid)
			}
		}
	}
}

func (d *K8sResoureService) processEventV1Relationship(unstructuredObj *unstructured.Unstructured) {
	var obj corev1.Event

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &obj); err != nil {
		d.logger.Error("processEventV1Relationship", zap.Error(err))
		return
	}

	query := fmt.Sprintf("INSERT EDGE Events(eventTime, note, reason, reportingController, type) VALUES %s -> %s:(timestamp(%s), %s, %s, %s, %s, %s);",
		strconv.Quote(string(unstructuredObj.GetUID())),
		strconv.Quote(string(obj.InvolvedObject.UID)),
		strconv.FormatInt(d.StringToInt64(obj.ResourceVersion), 10),
		strconv.Quote(string(obj.GetCreationTimestamp().Format(time.DateTime))),
		strconv.Quote(string(obj.Message)),
		strconv.Quote(string(obj.Reason)),
		strconv.Quote(string(obj.ReportingController)),
		strconv.Quote(string(obj.Type)))

	d.logger.Debug("processEventV1Relationship", zap.String("nGQL", query))
	_, err := d.graphDB.ExecuteAndCheck(query)
	if err != nil {
		d.logger.Error("processEventV1Relationship", zap.Error(err))
	}
}

func (d *K8sResoureService) processEventIoV1Relationship(unstructuredObj *unstructured.Unstructured) {
	var obj eventsv1.Event

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &obj); err != nil {
		d.logger.Error("processEventIoV1Relationship", zap.Error(err))
		return
	}

	query := fmt.Sprintf("INSERT EDGE Events(eventTime, note, reason, reportingController, type) VALUES %s -> %s:(timestamp(%s), %s, %s, %s,%s);",
		strconv.Quote(string(unstructuredObj.GetUID())),
		strconv.Quote(string(obj.Regarding.UID)),
		// d.StringToInt64(unstructuredObj.GetResourceVersion()),
		strconv.Quote(string(obj.GetCreationTimestamp().Format(time.DateTime))),
		strconv.Quote(string(obj.Note)),
		strconv.Quote(string(obj.Reason)),
		strconv.Quote(string(obj.ReportingController)),
		strconv.Quote(string(obj.Type)))

	d.logger.Debug("processEventIoV1Relationship", zap.String("nGQL", query))
	_, err := d.graphDB.ExecuteAndCheck(query)
	if err != nil {
		d.logger.Error("processEventIoV1Relationship", zap.Error(err))
	}
}

func (d *K8sResoureService) processIngressToServiceAndSecretRelationship(unstructuredObj *unstructured.Unstructured) {
	var obj networkingv1.Ingress

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &obj); err != nil {
		d.logger.Error("processIngressToServiceAndSecretRelationship", zap.Error(err))
		return
	}

	if obj.Spec.IngressClassName != nil {
		if uid, found := d.lookupUID("IngressClass", "", *obj.Spec.IngressClassName); found {
			_ = d.insertEdge("BelongsToIngressClass", string(unstructuredObj.GetUID()), uid)
		}
	}

	for _, rule := range obj.Spec.Rules {
		for _, path := range rule.HTTP.Paths {
			if uid, found := d.lookupUID("Service", obj.Namespace, path.Backend.Service.Name); found {
				_ = d.insertEdge("RoutesToSvc", string(unstructuredObj.GetUID()), uid)
			}
		}
	}

	for _, tls := range obj.Spec.TLS {
		if uid, found := d.lookupUID("Secret", obj.Namespace, tls.SecretName); found {
			_ = d.insertEdge("MountsSecret", string(unstructuredObj.GetUID()), uid)
		}
	}
}

func (d *K8sResoureService) processCSINodeToNodeRelationship(unstructuredObj *unstructured.Unstructured) {
	var obj scv1.CSINode

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &obj); err != nil {
		d.logger.Error("processCSINodeToNodeRelationship", zap.Error(err))
	}

	for _, driver := range obj.Spec.Drivers {
		if uid, found := d.lookupUID("CSIDriver", "", driver.Name); found {
			_ = d.insertEdge("RegisteredON", string(unstructuredObj.GetUID()), uid)
		}
	}
}

func (d *K8sResoureService) processOwnerReferences(unstructuredObj *unstructured.Unstructured) {
	ownerReferences := unstructuredObj.GetOwnerReferences()
	if len(ownerReferences) == 0 {
		return
	}

	var values []string
	for _, ownerReference := range ownerReferences {
		values = append(values, fmt.Sprintf("%s -> %s:(%s, %s, %s, %t, %s)",
			strconv.Quote(string(unstructuredObj.GetUID())),
			strconv.Quote(string(ownerReference.UID)),
			strconv.Quote(ownerReference.Kind),
			strconv.Quote(string(ownerReference.UID)),
			strconv.Quote(ownerReference.APIVersion),
			convertPointerToBool(ownerReference.Controller),
			strconv.Quote(ownerReference.Name)))
	}

	query := fmt.Sprintf("INSERT EDGE OwnedBy(owner_kind, owner_uid, api_version, controller_type, owner_name) VALUES %s;",
		strings.Join(values, ", "))
	d.logger.Debug("processOwnerReferences", zap.String("nGQL", query))
	_, err := d.graphDB.Execute(query)
	if err != nil {
		d.logger.Error("processOwnerReferences", zap.Error(err))
	}
}

func (d *K8sResoureService) processNamespaceRelationship(unstructuredObj *unstructured.Unstructured) {
	if unstructuredObj.GetNamespace() != "" {
		if uid, found := d.lookupUID("Namespace", "", unstructuredObj.GetNamespace()); found {
			query := fmt.Sprintf("INSERT EDGE BelongsTo () VALUES %s -> %s:();",
				strconv.Quote(string(unstructuredObj.GetUID())),
				strconv.Quote(uid))
			d.logger.Debug("processNamespaceRelationship", zap.String("nGQL", query))
			_, err := d.graphDB.Execute(query)
			if err != nil {
				d.logger.Error("processNamespaceRelationship", zap.Error(err))
			}
		} else {
			d.logger.Warn("processNamespaceRelationship", zap.String("namespace", unstructuredObj.GetNamespace()), zap.String("msg", "namespace node not found, skipping edge creation"))
		}
	}
}

func (d *K8sResoureService) processPodRelationships(unstructuredObj *unstructured.Unstructured) {
	var pod corev1.Pod
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &pod); err != nil {
		d.logger.Error("Error converting to Pod", zap.Error(err))
		return
	}

	d.processServiceAccountRelationship(unstructuredObj, &pod)
	d.processNodeRelationship(unstructuredObj, &pod)
	d.processVolumeRelationships(&pod)
	d.processPriorityClassRelationship(&pod)
	d.processRuntimeClassRelationship(&pod)
}

func (d *K8sResoureService) processPriorityClassRelationship(pod *corev1.Pod) {
	if pod.Spec.PriorityClassName == "" {
		return
	}
	if uid, found := d.lookupUID("PriorityClass", "", pod.Spec.PriorityClassName); found {
		_ = d.insertEdge("PodPrioClass", string(pod.UID), uid)
	}
}

func (d *K8sResoureService) processRuntimeClassRelationship(pod *corev1.Pod) {
	if pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName == "" {
		return
	}
	if uid, found := d.lookupUID("RuntimeClass", "", *pod.Spec.RuntimeClassName); found {
		_ = d.insertEdge("PodRuntimeClass", string(pod.UID), uid)
	}
}

func (d *K8sResoureService) processServiceAccountRelationship(unstructuredObj *unstructured.Unstructured, pod *corev1.Pod) {
	if uid, found := d.lookupUID("ServiceAccount", pod.Namespace, pod.Spec.ServiceAccountName); found {
		query := fmt.Sprintf("INSERT EDGE ServiceAccount () VALUES %s -> %s:();",
			strconv.Quote(string(unstructuredObj.GetUID())),
			strconv.Quote(uid))
		_, err := d.graphDB.Execute(query)
		if err != nil {
			d.logger.Error("processServiceAccountRelationship", zap.Error(err))
		}
	}
}

func (d *K8sResoureService) processNodeRelationship(unstructuredObj *unstructured.Unstructured, pod *corev1.Pod) {
	if pod.Spec.NodeName != "" {
		if uid, found := d.lookupUID("Node", "", pod.Spec.NodeName); found {
			query := fmt.Sprintf("INSERT EDGE RunsOn () VALUES %s -> %s:();",
				strconv.Quote(string(unstructuredObj.GetUID())),
				strconv.Quote(uid))
			_, err := d.graphDB.Execute(query)
			if err != nil {
				d.logger.Error("processNodeRelationship", zap.Error(err))
			}
		}
	}
}

func (d *K8sResoureService) processVolumeRelationships(pod *corev1.Pod) {
	var pvcNames []string
	for _, vol := range pod.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil {
			pvcNames = append(pvcNames, vol.PersistentVolumeClaim.ClaimName)
		}
	}
	if len(pvcNames) > 0 {
		pvcUIDMap := make(map[string]string)
		for _, name := range pvcNames {
			if uid, found := d.lookupUID("PersistentVolumeClaim", pod.Namespace, name); found {
				pvcUIDMap[name] = uid
			}
		}
		for _, vol := range pod.Spec.Volumes {
			if vol.PersistentVolumeClaim != nil {
				if uid, ok := pvcUIDMap[vol.PersistentVolumeClaim.ClaimName]; ok {
					_ = d.insertEdge("MountsPVC", string(pod.UID), uid)
				}
			}
		}
	}

	for i := range pod.Spec.Volumes {
		source := &pod.Spec.Volumes[i].VolumeSource
		d.processProjectedVolumes(source, pod)
		d.processConfigMapVolumes(source, pod)
		d.processSecretVolumes(source, pod)
	}

	for _, container := range pod.Spec.Containers {
		d.processContainerEnvFrom(container, pod)
		d.processContainerEnv(container, pod)
		d.processImagePullSecrets(container, pod)
	}

	for _, container := range pod.Spec.InitContainers {
		d.processContainerEnvFrom(container, pod)
		d.processContainerEnv(container, pod)
		d.processImagePullSecrets(container, pod)
	}

	for _, container := range pod.Spec.EphemeralContainers {
		d.processEphemeralContainerEnvFrom(container, pod)
		d.processEphemeralContainerEnv(container, pod)
		d.processEphemeralImagePullSecrets(container, pod)
	}
}

func (d *K8sResoureService) processProjectedVolumes(source *corev1.VolumeSource, pod *corev1.Pod) {
	if source.Projected != nil {
		var configMapNames []string
		var secretNames []string

		for j := range source.Projected.Sources {
			if source.Projected.Sources[j].ConfigMap != nil {
				d.logger.Debug("processProjectedVolumes", zap.String("name", source.Projected.Sources[j].ConfigMap.Name))
				configMapNames = append(configMapNames, source.Projected.Sources[j].ConfigMap.Name)
			}
			if source.Projected.Sources[j].Secret != nil {
				d.logger.Debug("processProjectedVolumes", zap.String("name", source.Projected.Sources[j].Secret.Name))
				secretNames = append(secretNames, source.Projected.Sources[j].Secret.Name)
			}
		}

		if len(configMapNames) == 0 && len(secretNames) == 0 {
			return
		}

		if len(configMapNames) > 0 {
			var edgeInserts []string
			for _, name := range configMapNames {
				if uid, found := d.lookupUID("ConfigMap", pod.Namespace, name); found {
					edgeInserts = append(edgeInserts, fmt.Sprintf("%s->%s:()",
						strconv.Quote(string(pod.UID)),
						strconv.Quote(uid)))
				}
			}
			if len(edgeInserts) > 0 {
				insertQuery := fmt.Sprintf("INSERT EDGE MountsConfig () VALUES %s;",
					strings.Join(edgeInserts, ", "))
				d.logger.Debug("processProjectedVolumes", zap.String("nGQL", insertQuery))
				_, err := d.graphDB.Execute(insertQuery)
				if err != nil {
					d.logger.Error("processProjectedVolumes", zap.Error(err))
				}
			}
		}

		if len(secretNames) > 0 {
			var edgeInserts []string
			for _, name := range secretNames {
				if uid, found := d.lookupUID("Secret", pod.Namespace, name); found {
					edgeInserts = append(edgeInserts, fmt.Sprintf("%s->%s:()",
						strconv.Quote(string(pod.UID)),
						strconv.Quote(uid)))
				}
			}
			if len(edgeInserts) > 0 {
				insertQuery := fmt.Sprintf("INSERT EDGE MountsSecret () VALUES %s;",
					strings.Join(edgeInserts, ", "))
				d.logger.Debug("processProjectedVolumes", zap.String("nGQL", insertQuery))
				_, err := d.graphDB.Execute(insertQuery)
				if err != nil {
					d.logger.Error("processProjectedVolumes", zap.Error(err))
				}
			}
		}
	}
}

func (d *K8sResoureService) processConfigMapVolumes(source *corev1.VolumeSource, pod *corev1.Pod) {
	if source.ConfigMap != nil {
		d.logger.Debug("processConfigMapVolumes", zap.String("name", source.ConfigMap.Name))
		if uid, found := d.lookupUID("ConfigMap", pod.Namespace, source.ConfigMap.Name); found {
			query := fmt.Sprintf("INSERT EDGE MountsConfig () VALUES %s -> %s:();",
				strconv.Quote(string(pod.UID)),
				strconv.Quote(uid))
			d.logger.Debug("processConfigMapVolumes", zap.String("nGQL", query))
			_, err := d.graphDB.Execute(query)
			if err != nil {
				d.logger.Error("processConfigMapVolumes", zap.Error(err))
			}
		}
	}
}

func (d *K8sResoureService) processSecretVolumes(source *corev1.VolumeSource, pod *corev1.Pod) {
	if source.Secret != nil {
		d.logger.Debug("processSecretVolumes", zap.String("name", source.Secret.SecretName))
		if uid, found := d.lookupUID("Secret", pod.Namespace, source.Secret.SecretName); found {
			query := fmt.Sprintf("INSERT EDGE MountsSecret () VALUES %s -> %s:();",
				strconv.Quote(string(pod.UID)),
				strconv.Quote(uid))
			d.logger.Debug("processSecretVolumes", zap.String("nGQL", query))
			_, err := d.graphDB.Execute(query)
			if err != nil {
				d.logger.Error("processSecretVolumes", zap.Error(err))
			}
		}
	}
}

func (d *K8sResoureService) processContainerEnvFrom(container corev1.Container, pod *corev1.Pod) {
	var secretNames []string
	var configMapNames []string

	for _, env := range container.EnvFrom {
		if env.SecretRef != nil {
			secretNames = append(secretNames, env.SecretRef.Name)
		}
		if env.ConfigMapRef != nil {
			configMapNames = append(configMapNames, env.ConfigMapRef.Name)
		}
	}

	if len(secretNames) == 0 && len(configMapNames) == 0 {
		return
	}

	if len(secretNames) > 0 {
		var edgeInserts []string
		for _, name := range secretNames {
			if uid, found := d.lookupUID("Secret", pod.Namespace, name); found {
				edgeInserts = append(edgeInserts, fmt.Sprintf("%s->%s:()",
					strconv.Quote(string(pod.UID)), strconv.Quote(uid)))
			}
		}
		if len(edgeInserts) > 0 {
			insertQuery := fmt.Sprintf("INSERT EDGE MountsSecret () VALUES %s;", strings.Join(edgeInserts, ", "))
			d.logger.Debug("processContainerEnvFrom", zap.String("nGQL", insertQuery))
			_, err := d.executenGQL(insertQuery)
			if err != nil {
				d.logger.Error("processContainerEnvFrom", zap.Error(err))
			}
		}
	}

	if len(configMapNames) > 0 {
		var edgeInserts []string
		for _, name := range configMapNames {
			if uid, found := d.lookupUID("ConfigMap", pod.Namespace, name); found {
				edgeInserts = append(edgeInserts, fmt.Sprintf("%s->%s:()",
					strconv.Quote(string(pod.UID)),
					strconv.Quote(uid)))
			}
		}
		if len(edgeInserts) > 0 {
			insertQuery := fmt.Sprintf("INSERT EDGE MountsConfig () VALUES %s;", strings.Join(edgeInserts, ", "))
			d.logger.Debug("processContainerEnvFrom", zap.String("nGQL", insertQuery))
			_, err := d.executenGQL(insertQuery)
			if err != nil {
				d.logger.Error("processContainerEnvFrom", zap.Error(err))
			}
		}
	}
}

func (d *K8sResoureService) processContainerEnv(container corev1.Container, pod *corev1.Pod) {
	var secretNames []string
	var configMapNames []string

	for _, envVar := range container.Env {
		if envVar.ValueFrom != nil && envVar.ValueFrom.SecretKeyRef != nil {
			secretNames = append(secretNames, envVar.ValueFrom.SecretKeyRef.Name)
		}
		if envVar.ValueFrom != nil && envVar.ValueFrom.ConfigMapKeyRef != nil {
			configMapNames = append(configMapNames, envVar.ValueFrom.ConfigMapKeyRef.Name)
		}
	}

	if len(secretNames) > 0 {
		var edgeInserts []string
		for _, name := range secretNames {
			if uid, found := d.lookupUID("Secret", pod.Namespace, name); found {
				edgeInserts = append(edgeInserts, fmt.Sprintf("%s->%s:()",
					strconv.Quote(string(pod.UID)), strconv.Quote(uid)))
			}
		}
		if len(edgeInserts) > 0 {
			insertQuery := fmt.Sprintf("INSERT EDGE MountsSecret () VALUES %s;", strings.Join(edgeInserts, ", "))
			d.logger.Debug("processContainerEnv", zap.String("nGQL", insertQuery))
			_, err := d.executenGQL(insertQuery)
			if err != nil {
				d.logger.Error("processContainerEnv", zap.Error(err))
			}
		}
	}

	if len(configMapNames) > 0 {
		var edgeInserts []string
		for _, name := range configMapNames {
			if uid, found := d.lookupUID("ConfigMap", pod.Namespace, name); found {
				edgeInserts = append(edgeInserts, fmt.Sprintf("%s->%s:()",
					strconv.Quote(string(pod.UID)),
					strconv.Quote(uid)))
			}
		}
		if len(edgeInserts) > 0 {
			insertQuery := fmt.Sprintf("INSERT EDGE MountsConfig () VALUES %s;", strings.Join(edgeInserts, ", "))
			d.logger.Debug("processContainerEnv", zap.String("nGQL", insertQuery))
			_, err := d.executenGQL(insertQuery)
			if err != nil {
				d.logger.Error("processContainerEnv", zap.Error(err))
			}
		}
	}
}

func (d *K8sResoureService) processImagePullSecrets(container corev1.Container, pod *corev1.Pod) {
	var secretNames []string

	for _, reference := range pod.Spec.ImagePullSecrets {
		secretNames = append(secretNames, reference.Name)
	}

	if len(secretNames) > 0 {
		var edgeInserts []string
		for _, name := range secretNames {
			if uid, found := d.lookupUID("Secret", pod.Namespace, name); found {
				edgeInserts = append(edgeInserts, fmt.Sprintf("%s->%s:()",
					strconv.Quote(string(pod.UID)), strconv.Quote(uid)))
			}
		}
		if len(edgeInserts) > 0 {
			insertQuery := fmt.Sprintf("INSERT EDGE MountsSecret () VALUES %s;", strings.Join(edgeInserts, ", "))
			d.logger.Debug("processImagePullSecrets", zap.String("nGQL", insertQuery))
			_, err := d.executenGQL(insertQuery)
			if err != nil {
				d.logger.Error("processImagePullSecrets", zap.Error(err))
			}
		}
	}
}

func (d *K8sResoureService) processEphemeralContainerEnvFrom(container corev1.EphemeralContainer, pod *corev1.Pod) {
	var secretNames []string
	var configMapNames []string

	for _, env := range container.EnvFrom {
		if env.SecretRef != nil {
			secretNames = append(secretNames, env.SecretRef.Name)
		}
		if env.ConfigMapRef != nil {
			configMapNames = append(configMapNames, env.ConfigMapRef.Name)
		}
	}

	if len(secretNames) == 0 && len(configMapNames) == 0 {
		return
	}

	if len(secretNames) > 0 {
		var edgeInserts []string
		for _, name := range secretNames {
			if uid, found := d.lookupUID("Secret", pod.Namespace, name); found {
				edgeInserts = append(edgeInserts, fmt.Sprintf("%s->%s:()",
					strconv.Quote(string(pod.UID)), strconv.Quote(uid)))
			}
		}
		if len(edgeInserts) > 0 {
			insertQuery := fmt.Sprintf("INSERT EDGE MountsSecret () VALUES %s;", strings.Join(edgeInserts, ", "))
			d.logger.Debug("processEphemeralContainerEnvFrom", zap.String("nGQL", insertQuery))
			_, err := d.executenGQL(insertQuery)
			if err != nil {
				d.logger.Error("Failed to insert MountsSecret edges",
					zap.Error(err),
					zap.String("pod", string(pod.UID)))
			}
		}
	}

	if len(configMapNames) > 0 {
		var edgeInserts []string
		for _, name := range configMapNames {
			if uid, found := d.lookupUID("ConfigMap", pod.Namespace, name); found {
				edgeInserts = append(edgeInserts, fmt.Sprintf("%s->%s:()",
					strconv.Quote(string(pod.UID)),
					strconv.Quote(uid)))
			}
		}
		if len(edgeInserts) > 0 {
			insertQuery := fmt.Sprintf("INSERT EDGE MountsConfig () VALUES %s;", strings.Join(edgeInserts, ", "))
			d.logger.Debug("processEphemeralContainerEnvFrom", zap.String("nGQL", insertQuery))
			_, err := d.executenGQL(insertQuery)
			if err != nil {
				d.logger.Error("Failed to insert MountsConfig edges",
					zap.Error(err),
					zap.String("pod", string(pod.UID)))
			}
		}
	}
}

func (d *K8sResoureService) processEphemeralContainerEnv(container corev1.EphemeralContainer, pod *corev1.Pod) {
	var secretNames []string
	var configMapNames []string

	for _, envVar := range container.Env {
		if envVar.ValueFrom != nil && envVar.ValueFrom.SecretKeyRef != nil {
			secretNames = append(secretNames, envVar.ValueFrom.SecretKeyRef.Name)
		}
		if envVar.ValueFrom != nil && envVar.ValueFrom.ConfigMapKeyRef != nil {
			configMapNames = append(configMapNames, envVar.ValueFrom.ConfigMapKeyRef.Name)
		}
	}

	if len(secretNames) > 0 {
		var edgeInserts []string
		for _, name := range secretNames {
			if uid, found := d.lookupUID("Secret", pod.Namespace, name); found {
				edgeInserts = append(edgeInserts, fmt.Sprintf("%s->%s:()",
					strconv.Quote(string(pod.UID)), strconv.Quote(uid)))
			}
		}
		if len(edgeInserts) > 0 {
			insertQuery := fmt.Sprintf("INSERT EDGE MountsSecret () VALUES %s;", strings.Join(edgeInserts, ", "))
			d.logger.Debug("processEphemeralContainerEnv", zap.String("nGQL", insertQuery))
			_, err := d.executenGQL(insertQuery)
			if err != nil {
				d.logger.Error("processEphemeralContainerEnv", zap.Error(err))
			}
		}
	}

	if len(configMapNames) > 0 {
		var edgeInserts []string
		for _, name := range configMapNames {
			if uid, found := d.lookupUID("ConfigMap", pod.Namespace, name); found {
				edgeInserts = append(edgeInserts, fmt.Sprintf("%s->%s:()",
					strconv.Quote(string(pod.UID)),
					strconv.Quote(uid)))
			}
		}
		if len(edgeInserts) > 0 {
			insertQuery := fmt.Sprintf("INSERT EDGE MountsConfig () VALUES %s;", strings.Join(edgeInserts, ", "))
			d.logger.Debug("processEphemeralContainerEnv", zap.String("nGQL", insertQuery))
			_, err := d.executenGQL(insertQuery)
			if err != nil {
				d.logger.Error("processEphemeralContainerEnv", zap.Error(err))
			}
		}
	}
}

func (d *K8sResoureService) processEphemeralImagePullSecrets(container corev1.EphemeralContainer, pod *corev1.Pod) {
	var secretNames []string

	for _, reference := range pod.Spec.ImagePullSecrets {
		secretNames = append(secretNames, reference.Name)
	}

	if len(secretNames) > 0 {
		var edgeInserts []string
		for _, name := range secretNames {
			if uid, found := d.lookupUID("Secret", pod.Namespace, name); found {
				edgeInserts = append(edgeInserts, fmt.Sprintf("%s->%s:()",
					strconv.Quote(string(pod.UID)), strconv.Quote(uid)))
			}
		}
		if len(edgeInserts) > 0 {
			insertQuery := fmt.Sprintf("INSERT EDGE MountsSecret () VALUES %s;", strings.Join(edgeInserts, ", "))
			d.logger.Debug("processEphemeralImagePullSecrets", zap.String("nGQL", insertQuery))
			_, err := d.executenGQL(insertQuery)
			if err != nil {
				d.logger.Error("processEphemeralImagePullSecrets", zap.Error(err))
			}
		}
	}
}

func (d *K8sResoureService) processPVCToPVToSCRelationship(unstructuredObj *unstructured.Unstructured) {
	var pv corev1.PersistentVolume

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &pv); err != nil {
		d.logger.Error("processPVCToPVToSCRelationship", zap.Error(err))
	}

	if pv.Spec.ClaimRef != nil {
		_ = d.insertEdge("BoundToPV", string(pv.Spec.ClaimRef.UID), string(pv.UID))
	}

	if pv.Spec.StorageClassName != "" {
		if uid, found := d.lookupUID("StorageClass", "", pv.Spec.StorageClassName); found {
			_ = d.insertEdge("BelongsToStorageClass", string(pv.UID), uid)
		}
	}
}

func (d *K8sResoureService) processServiceToEndpointToPodRelationship(unstructuredObj *unstructured.Unstructured) {
	var svc corev1.Service

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &svc); err != nil {
		d.logger.Error("processServiceToEndpointToPodRelationship", zap.Error(err))
	}

	if endpointUID, rd, found := d.lookupResourceDefine("Endpoints", svc.Namespace, svc.Name); found {
		_ = d.insertEdge("SvcToEp", string(unstructuredObj.GetUID()), endpointUID)

		epunstructred, err := d.unmarshalResourceDefine(rd)
		if err != nil {
			d.logger.Error("processServiceToEndpointToPodRelationship", zap.Error(err))
			return
		}
		var ep corev1.Endpoints //nolint:staticcheck

		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(epunstructred.Object, &ep); err != nil {
			d.logger.Error("processServiceToEndpointToPodRelationship", zap.Error(err))
		}

		for _, subset := range ep.Subsets {
			for _, address := range subset.Addresses {
				if address.TargetRef != nil {
					_ = d.insertEdge("EpToPods", endpointUID, string(address.TargetRef.UID))
					_ = d.insertEdge("SvcToPods", string(unstructuredObj.GetUID()), string(address.TargetRef.UID))
				}
			}
		}
	}
}

func (d *K8sResoureService) processEndpointSliceToServiceToEndpointPodRelationship(unstructuredObj *unstructured.Unstructured) {
	var epSlice discoveryv1.EndpointSlice

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &epSlice); err != nil {
		d.logger.Error("processEndpointSliceToServiceToEndpointPodRelationship", zap.Error(err))
		return
	}

	svc := unstructuredObj.GetOwnerReferences()
	if len(svc) != 0 {
		d.CleanupOutgoingEdgesByType(string(svc[0].UID), "SvcToPods")
	}

	for _, pods := range epSlice.Endpoints {
		if pods.TargetRef != nil {
			_ = d.insertEdge("EpSliceToPods", string(epSlice.UID), string(pods.TargetRef.UID))
			_ = d.insertEdge("SvcToPods", string(svc[0].UID), string(pods.TargetRef.UID))
		}
	}
}

func (d *K8sResoureService) processClassStorageToCSIDriverRelationship(unstructuredObj *unstructured.Unstructured) {
	var sc scv1.StorageClass

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &sc); err != nil {
		d.logger.Error("processClassStorageToCSIDriverRelationship", zap.Error(err))
	}

	if uid, found := d.lookupUID("CSIDriver", "", sc.Provisioner); found {
		_ = d.insertEdge("ScRefCSIDriver", string(unstructuredObj.GetUID()), uid)
	}
}

func (d *K8sResoureService) processVolumeAttachmentRelationship(unstructuredObj *unstructured.Unstructured) {
	var va scv1.VolumeAttachment
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &va); err != nil {
		d.logger.Error("processVolumeAttachmentRelationship", zap.Error(err))
		return
	}

	vaUID := string(unstructuredObj.GetUID())

	d.CleanupOutgoingEdgesByType(vaUID, "VolAttachToNode")
	d.CleanupOutgoingEdgesByType(vaUID, "VolAttachToPV")

	if va.Spec.NodeName != "" {
		if uid, found := d.lookupUID("Node", "", va.Spec.NodeName); found {
			_ = d.insertEdge("VolAttachToNode", vaUID, uid)
		}
	}

	if va.Spec.Source.PersistentVolumeName != nil && *va.Spec.Source.PersistentVolumeName != "" {
		if uid, found := d.lookupUID("PersistentVolume", "", *va.Spec.Source.PersistentVolumeName); found {
			_ = d.insertEdge("VolAttachToPV", vaUID, uid)
		}
	}
}

// lookupUID queries the UID of a K8sResource by kind, namespace and name.
// Uses BigCache (key: "uid:{kind}:{ns}:{name}") for fast path; fallback to LOOKUP ON.
func (d *K8sResoureService) lookupUID(kind, ns, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	cacheKey := "uid:" + kind + ":" + ns + ":" + name
	if v, err := d.cache.Get(cacheKey); err == nil {
		return string(v), true
	}

	// Cache miss: query Nebula with LOOKUP ON
	query := "LOOKUP ON K8sResource WHERE K8sResource.kind == '" + kind + "' AND K8sResource.name == " + strconv.Quote(name)
	if ns != "" {
		query += " AND K8sResource.name_space == " + strconv.Quote(ns)
	}
	query += " YIELD properties(vertex).uid AS uid;"

	rows, err := d.executenGQL(query)
	if err != nil || len(rows) == 0 {
		return "", false
	}
	uid := string(rows[0].Values[0].GetSVal())
	_ = d.cache.Set(cacheKey, []byte(uid))
	return uid, true
}

// lookupResourceDefine returns uid and resource_define for a K8sResource.
// Uses BigCache (key: "rd:{kind}:{ns}:{name}") with fallback to LOOKUP ON.
func (d *K8sResoureService) lookupResourceDefine(kind, ns, name string) (uid, resourceDefine string, found bool) {
	if name == "" {
		return "", "", false
	}
	rdKey := "rd:" + kind + ":" + ns + ":" + name
	if v, err := d.cache.Get(rdKey); err == nil {
		// uid is stored in the separate uid cache
		uidKey := "uid:" + kind + ":" + ns + ":" + name
		if u, err2 := d.cache.Get(uidKey); err2 == nil {
			return string(u), string(v), true
		}
	}

	query := "LOOKUP ON K8sResource WHERE K8sResource.kind == '" + kind + "' AND K8sResource.name == " + strconv.Quote(name)
	if ns != "" {
		query += " AND K8sResource.name_space == " + strconv.Quote(ns)
	}
	query += " YIELD properties(vertex).uid AS uid, properties(vertex).resource_define AS resource_define;"

	rows, err := d.executenGQL(query)
	if err != nil || len(rows) == 0 {
		return "", "", false
	}
	uid2 := string(rows[0].Values[0].GetSVal())
	rd := string(rows[0].Values[1].GetSVal())
	_ = d.cache.Set(rdKey, []byte(rd))
	_ = d.cache.Set("uid:"+kind+":"+ns+":"+name, []byte(uid2))
	return uid2, rd, true
}
