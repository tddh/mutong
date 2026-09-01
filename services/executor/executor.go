package executor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cfg "gitee.com/tddh/mutong/config"
	interfaces "gitee.com/tddh/mutong/interfaces"
	maudit "gitee.com/tddh/mutong/models/audit"
	ex "gitee.com/tddh/mutong/models/executor"
	audstore "gitee.com/tddh/mutong/services/audit"
	"github.com/google/uuid"
	"go.uber.org/zap"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type K8sExecutor struct {
	logger       interfaces.Logger
	k8sClient    *kubernetes.Clientset
	cfg          *cfg.Config
	auditStore   AuditLogStore
	opAuditStore audstore.OperationAuditStore
	autoMode     atomic.Bool
	coolDowns    sync.Map // map[string]time.Time — key: "restart:<ns>:<name>"
	stopCh       chan struct{}
}

// memory-based in-memory store implementation
type memoryAuditStore struct {
	mu   sync.RWMutex
	logs []ex.AuditLog
}

func newMemoryAuditStore() AuditLogStore {
	return &memoryAuditStore{logs: make([]ex.AuditLog, 0)}
}

func (m *memoryAuditStore) Save(log ex.AuditLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, log)
	return nil
}

func (m *memoryAuditStore) List(filters map[string]string) ([]ex.AuditLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ex.AuditLog, 0, len(m.logs))
	for _, a := range m.logs {
		if len(filters) == 0 || matchesAuditLog(a, filters) {
			out = append(out, a)
		}
	}
	return out, nil
}

func (m *memoryAuditStore) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.logs)
}

// GetByID returns an audit log by its ID from the in-memory store
func (m *memoryAuditStore) GetByID(id string) (*ex.AuditLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range m.logs {
		if m.logs[i].ID == id {
			// return a copy to avoid aliasing
			log := m.logs[i]
			return &log, nil
		}
	}
	return nil, fmt.Errorf("audit log not found: %s", id)
}

func NewK8sExecutor(logger interfaces.Logger, k8sClient *kubernetes.Clientset, cfg *cfg.Config) *K8sExecutor {
	// Initialize with in-memory default store; may be replaced with MySQL store below
	store := newMemoryAuditStore()
	if cfg != nil {
		auditCfg := cfg.GetExecutorConf().AuditLog
		if strings.ToLower(strings.TrimSpace(auditCfg.Type)) == "postgres" {
			if db := cfg.GetConfigGormDB(); db != nil {
				if s, err := NewPostgresAuditStore(db, auditCfg.Table); err == nil {
					store = s
				} else {
					logger.Warn("Failed to initialize PostgreSQL audit store, using in-memory storage", zap.Error(err))
				}
			}
		}
	}
	e := &K8sExecutor{
		logger:       logger,
		k8sClient:    k8sClient,
		cfg:          cfg,
		auditStore:   store,
		opAuditStore: nil,
		stopCh:       make(chan struct{}),
	}
	if cfg != nil {
		e.autoMode.Store(cfg.GetExecutorConf().AutoMode)
	}
	e.logger.Info("Executor initialized",
		zap.Bool("enabled", cfg.GetExecutorConf().Enabled),
		zap.Bool("autoMode", cfg.GetExecutorConf().AutoMode))

	go e.cleanCoolDowns(10*time.Minute, 10*time.Minute)

	return e
}

func (e *K8sExecutor) Execute(ctx context.Context, plan ex.ExecutionPlan) (*ex.ExecutionResult, error) {
	action := plan.Action
	if action != ex.ActionRestartPod && action != ex.ActionScaleDeployment && action != ex.ActionDeletePod &&
		action != ex.ActionCreateHPA && action != ex.ActionUpdateHPA &&
		action != ex.ActionUpdateConfigMap && action != ex.ActionUpdateSecret &&
		action != ex.ActionUpdateResourceLimits && action != ex.ActionUpdateDeploymentImage &&
		action != ex.ActionUpdateAnnotations && action != ex.ActionUpdateLabels &&
		action != ex.ActionRolloutRestart {
		return nil, fmt.Errorf("unsupported action: %s", action)
	}

	execCfg := e.getActionConf(string(action))
	threshold := execCfg.AutoThreshold

	autoMode := e.IsAutoMode()
	if plan.ApprovedBy == "" {
		// 未经人工审批：必须处于自动模式且置信度达标才执行
		if autoMode {
			if plan.Confidence < threshold {
				audit := ex.AuditLog{
					ID:           generateAuditID(),
					Plan:         plan,
					Result:       ex.ExecutionResult{Success: false, Message: "auto mode disabled: confidence below threshold", Timestamp: time.Now()},
					AutoExecuted: true,
					ApprovedBy:   "",
					Timestamp:    time.Now(),
				}
				e.appendAuditLog(audit)
				return &audit.Result, nil
			}
		} else {
			audit := ex.AuditLog{
				ID:           generateAuditID(),
				Plan:         plan,
				Result:       ex.ExecutionResult{Success: false, Message: "approval required", Timestamp: time.Now()},
				AutoExecuted: false,
				ApprovedBy:   plan.ApprovedBy,
				Timestamp:    time.Now(),
			}
			e.appendAuditLog(audit)
			return &audit.Result, nil
		}
	}

	var res *ex.ExecutionResult
	var err error
	var success bool
	var msg string
	switch action {
	case ex.ActionRestartPod:
		err = e.restartPod(ctx, plan.Namespace, plan.ResourceName)
		if err != nil {
			msg = err.Error()
		} else {
			success = true
			msg = "pod restarted (eviction triggers restart)"
		}
	case ex.ActionDeletePod:
		err = e.deletePod(ctx, plan.Namespace, plan.ResourceName)
		if err != nil {
			msg = err.Error()
		} else {
			success = true
			msg = "pod deleted"
		}
	case ex.ActionScaleDeployment:
		// Use the requested replicas from the ExecutionPlan to scale the Deployment
		// Replicas value is optional; if 0, Kubernetes will scale to 0
		err = e.scaleDeployment(ctx, plan.Namespace, plan.ResourceName, plan.Replicas)
		if err != nil {
			msg = err.Error()
		} else {
			success = true
			msg = fmt.Sprintf("scaled deployment %s/%s to %d replicas", plan.Namespace, plan.ResourceName, plan.Replicas)
		}
	case ex.ActionCreateHPA:
		// Create HorizontalPodAutoscaler for the target Deployment
		// Use defaults if not provided
		if plan.MinReplicas == 0 {
			plan.MinReplicas = 1
		}
		if plan.MaxReplicas == 0 {
			plan.MaxReplicas = 5
		}
		if plan.TargetCPU == 0 {
			plan.TargetCPU = 50
		}
		err = e.createHPA(ctx, plan)
		if err != nil {
			msg = err.Error()
		} else {
			success = true
			msg = fmt.Sprintf("created HPA for %s/%s", plan.Namespace, plan.ResourceName)
		}
	case ex.ActionUpdateHPA:
		// Update existing HPA with new configuration
		if plan.MinReplicas == 0 {
			plan.MinReplicas = 1
		}
		if plan.MaxReplicas == 0 {
			plan.MaxReplicas = 5
		}
		if plan.TargetCPU == 0 {
			plan.TargetCPU = 50
		}
		err = e.updateHPA(ctx, plan)
		if err != nil {
			msg = err.Error()
		} else {
			success = true
			msg = fmt.Sprintf("updated HPA for %s/%s", plan.Namespace, plan.ResourceName)
		}
	case ex.ActionUpdateConfigMap:
		err = e.executeUpdateConfigMap(ctx, plan)
		if err != nil {
			msg = err.Error()
		} else {
			success = true
			msg = fmt.Sprintf("updated ConfigMap %s/%s", plan.Namespace, plan.ResourceName)
		}
	case ex.ActionUpdateSecret:
		err = e.executeUpdateSecret(ctx, plan)
		if err != nil {
			msg = err.Error()
		} else {
			success = true
			msg = fmt.Sprintf("updated Secret %s/%s", plan.Namespace, plan.ResourceName)
		}
	case ex.ActionUpdateResourceLimits:
		err = e.executeUpdateResourceLimits(ctx, plan)
		if err != nil {
			msg = err.Error()
		} else {
			success = true
			msg = fmt.Sprintf("updated resource limits for %s/%s", plan.Namespace, plan.ResourceName)
		}
	case ex.ActionUpdateDeploymentImage:
		err = e.executeUpdateDeploymentImage(ctx, plan)
		if err != nil {
			msg = err.Error()
		} else {
			success = true
			msg = fmt.Sprintf("updated image for %s/%s to %s", plan.Namespace, plan.ResourceName, plan.Image)
		}
	case ex.ActionUpdateAnnotations:
		err = e.executeUpdateAnnotations(ctx, plan)
		if err != nil {
			msg = err.Error()
		} else {
			success = true
			msg = "annotations updated"
		}
	case ex.ActionUpdateLabels:
		err = e.executeUpdateLabels(ctx, plan)
		if err != nil {
			msg = err.Error()
		} else {
			success = true
			msg = "labels updated"
		}
	case ex.ActionRolloutRestart:
		err = e.rolloutRestartDeployment(ctx, plan.Namespace, plan.ResourceName)
		if err != nil {
			msg = err.Error()
		} else {
			success = true
			msg = fmt.Sprintf("rollout restart triggered for deployment %s/%s", plan.Namespace, plan.ResourceName)
		}
	}

	audit := ex.AuditLog{
		ID:           generateAuditID(),
		Plan:         plan,
		Result:       ex.ExecutionResult{Success: success, Message: msg, Timestamp: time.Now()},
		AutoExecuted: autoMode && plan.ApprovedBy == "",
		ApprovedBy:   plan.ApprovedBy,
		Timestamp:    time.Now(),
	}
	e.appendAuditLog(audit)

	e.logger.Info("Self-healing action executed",
		zap.String("action", string(plan.Action)),
		zap.Bool("success", success),
		zap.String("namespace", plan.Namespace),
		zap.String("resource", plan.ResourceName),
		zap.Bool("autoMode", autoMode))

	res = &audit.Result
	return res, err
}

func (e *K8sExecutor) IsAutoMode() bool {
	return e.autoMode.Load()
}

func (e *K8sExecutor) SetAutoMode(auto bool) {
	e.autoMode.Store(auto)
	e.logger.Info("Executor auto mode changed", zap.Bool("auto", auto))
}

func (e *K8sExecutor) GetAuditLogs(ctx context.Context, filters map[string]string) ([]ex.AuditLog, error) {
	return e.auditStore.List(filters)
}

// GetAuditLogByID 按审计记录 ID 查询单条记录
func (e *K8sExecutor) GetAuditLogByID(id string) (*ex.AuditLog, error) {
	return e.auditStore.GetByID(id)
}

func (e *K8sExecutor) appendAuditLog(a ex.AuditLog) {
	_ = e.auditStore.Save(a)
	// Also persist an OperationAuditLog if the store is configured
	if e.opAuditStore != nil {
		operator := a.Plan.ApprovedBy
		if operator == "" {
			operator = "system"
		}
		op := maudit.OperationAuditLog{
			Operator:     operator,
			Action:       string(a.Plan.Action),
			ResourceType: a.Plan.Target,
			ResourceName: a.Plan.ResourceName,
			Namespace:    a.Plan.Namespace,
			Result:       a.Result.Message,
			Reason:       a.Plan.Reason,
			Timestamp:    a.Timestamp,
			Details:      "",
		}
		_ = e.opAuditStore.Save(op)
	}
}

func (e *K8sExecutor) RecordAudit(_ context.Context, log ex.AuditLog) error {
	e.appendAuditLog(log)
	return nil
}

func (e *K8sExecutor) restartPod(ctx context.Context, namespace, name string) error {
	if namespace == "" {
		namespace = e.cfg.GetExecutorConf().DefaultNamespace
	}

	pod, err := e.k8sClient.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("获取 Pod 信息失败: %w", err)
	}

	if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodSucceeded {
		return fmt.Errorf("Pod 状态为 %s，拒绝重启", pod.Status.Phase)
	}

	for _, cs := range pod.Status.ContainerStatuses {
		maxRestart := e.cfg.GetExecutorConf().MaxRestartCount
		if int(cs.RestartCount) > maxRestart {
			return fmt.Errorf("容器 %s 重启次数 %d > %d，需人工介入", cs.Name, cs.RestartCount, maxRestart)
		}
	}

	coolKey := fmt.Sprintf("restart:%s:%s", namespace, name)
	coolDuration := time.Duration(e.cfg.GetExecutorConf().CoolDownMinutes) * time.Minute
	if last, ok := e.coolDowns.Load(coolKey); ok {
		if since := time.Since(last.(time.Time)); since < coolDuration {
			return fmt.Errorf("在冷却期内（剩余 %v）", coolDuration-since)
		}
	}

	// Use eviction API for safe pod restart (respects PDB, graceful termination)
	// If eviction fails, fall back to delete with grace period
	eviction := &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err = e.k8sClient.CoreV1().Pods(namespace).EvictV1(ctx, eviction)
	if err != nil {
		e.logger.Warn("Eviction failed, falling back to delete", zap.Error(err), zap.String("namespace", namespace), zap.String("pod", name))
		gracePeriod := int64(e.cfg.GetExecutorConf().GracePeriodSec)
		err = e.k8sClient.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{
			GracePeriodSeconds: &gracePeriod,
		})
	}
	if err == nil {
		e.coolDowns.Store(coolKey, time.Now())
	}
	return err
}

func (e *K8sExecutor) deletePod(ctx context.Context, namespace, name string) error {
	if namespace == "" {
		namespace = e.cfg.GetExecutorConf().DefaultNamespace
	}
	gracePeriod := int64(e.cfg.GetExecutorConf().DeleteGracePeriodSec)
	return e.k8sClient.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
	})
}

func (e *K8sExecutor) scaleDeployment(ctx context.Context, namespace, name string, replicas int32) error {
	if namespace == "" {
		namespace = e.cfg.GetExecutorConf().DefaultNamespace
	}
	maxReplicas := e.cfg.GetExecutorConf().MaxReplicas
	if replicas > int32(maxReplicas) { //nolint:gosec
		return fmt.Errorf("目标副本数 %d 超过上限 (%d)，拒绝扩缩", replicas, maxReplicas)
	}
	// Get current scale for the Deployment
	scale, err := e.k8sClient.AppsV1().Deployments(namespace).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	// Update replicas
	scale.Spec.Replicas = replicas
	// Apply the new scale
	_, err = e.k8sClient.AppsV1().Deployments(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
	return err
}

// rolloutRestartDeployment triggers a rolling restart by patching the pod template
// annotation, equivalent to `kubectl rollout restart deployment/<name>`
func (e *K8sExecutor) rolloutRestartDeployment(ctx context.Context, namespace, name string) error {
	if namespace == "" {
		namespace = e.cfg.GetExecutorConf().DefaultNamespace
	}
	deploy, err := e.k8sClient.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get Deployment: %w", err)
	}
	if deploy.Spec.Template.Annotations == nil {
		deploy.Spec.Template.Annotations = make(map[string]string)
	}
	deploy.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)
	_, err = e.k8sClient.AppsV1().Deployments(namespace).Update(ctx, deploy, metav1.UpdateOptions{})
	return err
}

// createHPA creates a Horizontal Pod Autoscaler for the given Deployment
func (e *K8sExecutor) createHPA(ctx context.Context, plan ex.ExecutionPlan) error {
	if e.k8sClient == nil {
		return fmt.Errorf("k8s client is nil")
	}
	hpaName := plan.ResourceName + "-hpa"
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hpaName,
			Namespace: plan.Namespace,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind:       "Deployment",
				Name:       plan.ResourceName,
				APIVersion: "apps/v1",
			},
			MinReplicas: &plan.MinReplicas,
			MaxReplicas: plan.MaxReplicas,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.MetricTargetType("Utilization"),
							AverageUtilization: &plan.TargetCPU,
						},
					},
				},
			},
		},
	}
	_, err := e.k8sClient.AutoscalingV2().HorizontalPodAutoscalers(plan.Namespace).Create(ctx, hpa, metav1.CreateOptions{})
	return err
}

// updateHPA updates an existing Horizontal Pod Autoscaler for the given Deployment
func (e *K8sExecutor) updateHPA(ctx context.Context, plan ex.ExecutionPlan) error {
	if e.k8sClient == nil {
		return fmt.Errorf("k8s client is nil")
	}
	hpaName := plan.ResourceName + "-hpa"
	existing, err := e.k8sClient.AutoscalingV2().HorizontalPodAutoscalers(plan.Namespace).Get(ctx, hpaName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	existing.Spec.MinReplicas = &plan.MinReplicas
	existing.Spec.MaxReplicas = plan.MaxReplicas
	existing.Spec.Metrics = []autoscalingv2.MetricSpec{
		{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceCPU,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.MetricTargetType("Utilization"),
					AverageUtilization: &plan.TargetCPU,
				},
			},
		},
	}
	_, err = e.k8sClient.AutoscalingV2().HorizontalPodAutoscalers(plan.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (e *K8sExecutor) getActionConf(action string) cfg.ActionConf {
	if e.cfg == nil {
		return cfg.ActionConf{Risk: "low", AutoThreshold: 0.0}
	}
	acMap := e.cfg.GetExecutorConf().Actions
	if ac, ok := acMap[action]; ok {
		if ac.Risk == "" {
			ac.Risk = "low"
		}
		if ac.AutoThreshold < 0 {
			ac.AutoThreshold = 0
		}
		return ac
	}
	return cfg.ActionConf{Risk: "low", AutoThreshold: 0.0}
}

func generateAuditID() string {
	return fmt.Sprintf("audit-%s", uuid.New().String())
}

func matchesAuditLog(a ex.AuditLog, filters map[string]string) bool {
	if len(filters) == 0 {
		return true
	}
	if v, ok := filters["action"]; ok {
		if !strings.EqualFold(string(a.Plan.Action), v) {
			return false
		}
	}
	if v, ok := filters["namespace"]; ok {
		if a.Plan.Namespace != v {
			return false
		}
	}
	if v, ok := filters["autoExecuted"]; ok {
		if (v == "true" && !a.AutoExecuted) || (v == "false" && a.AutoExecuted) {
			return false
		}
	}
	return true
}

func (e *K8sExecutor) cleanCoolDowns(interval, maxAge time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopCh:
			e.logger.Info("cleanCoolDowns stopped")
			return
		case <-ticker.C:
			now := time.Now()
			e.coolDowns.Range(func(key, value interface{}) bool {
				if now.Sub(value.(time.Time)) > maxAge {
					e.coolDowns.Delete(key)
				}
				return true
			})
		}
	}
}

func (e *K8sExecutor) Stop() {
	e.logger.Info("K8s executor stopped")
	close(e.stopCh)
}

func (e *K8sExecutor) executeUpdateConfigMap(ctx context.Context, plan ex.ExecutionPlan) error {
	ns := plan.Namespace
	if ns == "" {
		ns = e.cfg.GetExecutorConf().DefaultNamespace
	}
	cm, err := e.k8sClient.CoreV1().ConfigMaps(ns).Get(ctx, plan.ResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get ConfigMap: %w", err)
	}
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	for k, v := range plan.ConfigData {
		cm.Data[k] = v
	}
	_, err = e.k8sClient.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

func (e *K8sExecutor) executeUpdateSecret(ctx context.Context, plan ex.ExecutionPlan) error {
	ns := plan.Namespace
	if ns == "" {
		ns = e.cfg.GetExecutorConf().DefaultNamespace
	}
	s, err := e.k8sClient.CoreV1().Secrets(ns).Get(ctx, plan.ResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get Secret: %w", err)
	}
	if s.StringData == nil {
		s.StringData = make(map[string]string)
	}
	for k, v := range plan.ConfigData {
		s.StringData[k] = v
	}
	_, err = e.k8sClient.CoreV1().Secrets(ns).Update(ctx, s, metav1.UpdateOptions{})
	return err
}

func (e *K8sExecutor) executeUpdateResourceLimits(ctx context.Context, plan ex.ExecutionPlan) error {
	ns := plan.Namespace
	if ns == "" {
		ns = e.cfg.GetExecutorConf().DefaultNamespace
	}
	deploy, err := e.k8sClient.AppsV1().Deployments(ns).Get(ctx, plan.ResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get Deployment: %w", err)
	}
	idx := 0
	if plan.ContainerName != "" {
		for i, c := range deploy.Spec.Template.Spec.Containers {
			if c.Name == plan.ContainerName {
				idx = i
				break
			}
		}
	}
	if len(plan.ConfigData) > 0 {
		e.parseResourceLimitsFromConfig(plan.ConfigData, &deploy.Spec.Template.Spec.Containers[idx])
	}
	_, err = e.k8sClient.AppsV1().Deployments(ns).Update(ctx, deploy, metav1.UpdateOptions{})
	return err
}

func (e *K8sExecutor) parseResourceLimitsFromConfig(data map[string]string, c *corev1.Container) {
	if cpuLimit, ok := data["cpu_limit"]; ok {
		q, _ := resource.ParseQuantity(cpuLimit)
		if c.Resources.Limits == nil {
			c.Resources.Limits = corev1.ResourceList{}
		}
		c.Resources.Limits[corev1.ResourceCPU] = q
	}
	if memLimit, ok := data["memory_limit"]; ok {
		q, _ := resource.ParseQuantity(memLimit)
		if c.Resources.Limits == nil {
			c.Resources.Limits = corev1.ResourceList{}
		}
		c.Resources.Limits[corev1.ResourceMemory] = q
	}
	if cpuReq, ok := data["cpu_request"]; ok {
		q, _ := resource.ParseQuantity(cpuReq)
		if c.Resources.Requests == nil {
			c.Resources.Requests = corev1.ResourceList{}
		}
		c.Resources.Requests[corev1.ResourceCPU] = q
	}
	if memReq, ok := data["memory_request"]; ok {
		q, _ := resource.ParseQuantity(memReq)
		if c.Resources.Requests == nil {
			c.Resources.Requests = corev1.ResourceList{}
		}
		c.Resources.Requests[corev1.ResourceMemory] = q
	}
}

func (e *K8sExecutor) executeUpdateDeploymentImage(ctx context.Context, plan ex.ExecutionPlan) error {
	ns := plan.Namespace
	if ns == "" {
		ns = e.cfg.GetExecutorConf().DefaultNamespace
	}
	deploy, err := e.k8sClient.AppsV1().Deployments(ns).Get(ctx, plan.ResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get Deployment: %w", err)
	}
	idx := 0
	if plan.ContainerName != "" {
		for i, c := range deploy.Spec.Template.Spec.Containers {
			if c.Name == plan.ContainerName {
				idx = i
				break
			}
		}
	}
	deploy.Spec.Template.Spec.Containers[idx].Image = plan.Image
	_, err = e.k8sClient.AppsV1().Deployments(ns).Update(ctx, deploy, metav1.UpdateOptions{})
	return err
}

func (e *K8sExecutor) executeUpdateAnnotations(ctx context.Context, plan ex.ExecutionPlan) error {
	ns := plan.Namespace
	if ns == "" {
		ns = e.cfg.GetExecutorConf().DefaultNamespace
	}
	deploy, err := e.k8sClient.AppsV1().Deployments(ns).Get(ctx, plan.ResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get Deployment: %w", err)
	}
	if deploy.Annotations == nil {
		deploy.Annotations = make(map[string]string)
	}
	for k, v := range plan.Annotations {
		deploy.Annotations[k] = v
	}
	_, err = e.k8sClient.AppsV1().Deployments(ns).Update(ctx, deploy, metav1.UpdateOptions{})
	return err
}

func (e *K8sExecutor) executeUpdateLabels(ctx context.Context, plan ex.ExecutionPlan) error {
	ns := plan.Namespace
	if ns == "" {
		ns = e.cfg.GetExecutorConf().DefaultNamespace
	}
	deploy, err := e.k8sClient.AppsV1().Deployments(ns).Get(ctx, plan.ResourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get Deployment: %w", err)
	}
	if deploy.Labels == nil {
		deploy.Labels = make(map[string]string)
	}
	for k, v := range plan.Labels {
		deploy.Labels[k] = v
	}
	_, err = e.k8sClient.AppsV1().Deployments(ns).Update(ctx, deploy, metav1.UpdateOptions{})
	return err
}
