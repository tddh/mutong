package executor

import (
	"context"
	"encoding/json"
	"testing"

	ex "gitee.com/tddh/mutong/models/executor"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

type testLogger struct{}

func (t *testLogger) Debug(msg string, fields ...zap.Field) {}
func (t *testLogger) Info(msg string, fields ...zap.Field)  {}
func (t *testLogger) Warn(msg string, fields ...zap.Field)  {}
func (t *testLogger) Error(msg string, fields ...zap.Field) {}

type rsHistory struct {
	revision string
	image    string
	replicas int32
}

func int32Ptr(v int32) *int32 { return &v }
func boolPtr(v bool) *bool    { return &v }

// deploymentWithHistory 构造 Deployment 及其 ReplicaSet 版本历史（cfg 为 nil，测试必须显式传 namespace）
func deploymentWithHistory(ns, name, currentImage string, histories []rsHistory) (*appsv1.Deployment, []*appsv1.ReplicaSet) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: ns, UID: types.UID("dep-uid"), Generation: 3,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: currentImage}},
				},
			},
		},
	}
	var rsList []*appsv1.ReplicaSet
	for _, h := range histories {
		rs := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name + "-" + h.revision,
				Namespace: ns,
				Annotations: map[string]string{
					"deployment.kubernetes.io/revision": h.revision,
				},
				OwnerReferences: []metav1.OwnerReference{
					{Kind: "Deployment", Name: name, UID: types.UID("dep-uid"), Controller: boolPtr(true)},
				},
			},
			Spec: appsv1.ReplicaSetSpec{
				Replicas: int32Ptr(h.replicas),
				Selector: dep.Spec.Selector,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "app", Image: h.image}},
					},
				},
			},
		}
		rsList = append(rsList, rs)
	}
	return dep, rsList
}

// newFakeExecutor 用 fake clientset 构造执行器（cfg 为 nil，测试必须显式传 namespace）
func newFakeExecutor(t *testing.T, objs ...runtime.Object) *K8sExecutor {
	t.Helper()
	return &K8sExecutor{
		logger:     &testLogger{},
		k8sClient:  fake.NewSimpleClientset(objs...),
		auditStore: newMemoryAuditStore(),
	}
}

func TestRolloutUndoToPreviousRevision(t *testing.T) {
	dep, rsList := deploymentWithHistory("default", "nginx", "nginx:2.0", []rsHistory{
		{"1", "nginx:0.9", 0},
		{"2", "nginx:1.0", 0},
		{"3", "nginx:2.0", 1},
	})
	objs := []runtime.Object{dep}
	for _, rs := range rsList {
		objs = append(objs, rs)
	}
	e := newFakeExecutor(t, objs...)

	rev, err := e.rolloutUndoDeployment(context.Background(), "default", "nginx", 0)
	if err != nil {
		t.Fatalf("rolloutUndoDeployment failed: %v", err)
	}
	if rev != 2 {
		t.Errorf("rolled back to revision %d, want 2", rev)
	}
	got, err := e.k8sClient.AppsV1().Deployments("default").Get(context.Background(), "nginx", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if img := got.Spec.Template.Spec.Containers[0].Image; img != "nginx:1.0" {
		t.Errorf("template image = %q, want nginx:1.0", img)
	}
	if _, ok := got.Spec.Template.Labels["pod-template-hash"]; ok {
		t.Error("pod-template-hash label should be stripped from restored template")
	}
}

func TestRolloutUndoToSpecificRevision(t *testing.T) {
	dep, rsList := deploymentWithHistory("default", "nginx", "nginx:2.0", []rsHistory{
		{"1", "nginx:0.9", 0},
		{"2", "nginx:1.0", 0},
		{"3", "nginx:2.0", 1},
	})
	objs := []runtime.Object{dep}
	for _, rs := range rsList {
		objs = append(objs, rs)
	}
	e := newFakeExecutor(t, objs...)

	rev, err := e.rolloutUndoDeployment(context.Background(), "default", "nginx", 1)
	if err != nil {
		t.Fatalf("rolloutUndoDeployment failed: %v", err)
	}
	if rev != 1 {
		t.Errorf("rolled back to revision %d, want 1", rev)
	}
	got, _ := e.k8sClient.AppsV1().Deployments("default").Get(context.Background(), "nginx", metav1.GetOptions{})
	if img := got.Spec.Template.Spec.Containers[0].Image; img != "nginx:0.9" {
		t.Errorf("template image = %q, want nginx:0.9", img)
	}
}

func TestRolloutUndoNoHistory(t *testing.T) {
	dep, rsList := deploymentWithHistory("default", "nginx", "nginx:1.0", []rsHistory{
		{"1", "nginx:1.0", 1},
	})
	objs := []runtime.Object{dep}
	for _, rs := range rsList {
		objs = append(objs, rs)
	}
	e := newFakeExecutor(t, objs...)

	if _, err := e.rolloutUndoDeployment(context.Background(), "default", "nginx", 0); err == nil {
		t.Error("expected error when no older revision exists")
	}
}

func TestRolloutUndoRevisionNotFound(t *testing.T) {
	dep, rsList := deploymentWithHistory("default", "nginx", "nginx:2.0", []rsHistory{
		{"1", "nginx:1.0", 0},
		{"2", "nginx:2.0", 1},
	})
	objs := []runtime.Object{dep}
	for _, rs := range rsList {
		objs = append(objs, rs)
	}
	e := newFakeExecutor(t, objs...)

	if _, err := e.rolloutUndoDeployment(context.Background(), "default", "nginx", 9); err == nil {
		t.Error("expected error for nonexistent revision")
	}
}

func TestRestorePodTemplate(t *testing.T) {
	dep, _ := deploymentWithHistory("default", "nginx", "nginx:1.0", nil)
	e := newFakeExecutor(t, dep)

	snapshot := e.snapshotDeploymentTemplate(context.Background(), "default", "nginx")
	if snapshot == "" {
		t.Fatal("snapshot should not be empty")
	}
	// 模拟改坏镜像
	d, _ := e.k8sClient.AppsV1().Deployments("default").Get(context.Background(), "nginx", metav1.GetOptions{})
	d.Spec.Template.Spec.Containers[0].Image = "nginx:broken"
	_, _ = e.k8sClient.AppsV1().Deployments("default").Update(context.Background(), d, metav1.UpdateOptions{})

	if err := e.restorePodTemplate(context.Background(), "default", "nginx", snapshot); err != nil {
		t.Fatalf("restorePodTemplate failed: %v", err)
	}
	got, _ := e.k8sClient.AppsV1().Deployments("default").Get(context.Background(), "nginx", metav1.GetOptions{})
	if img := got.Spec.Template.Spec.Containers[0].Image; img != "nginx:1.0" {
		t.Errorf("restored image = %q, want nginx:1.0", img)
	}
}

func TestCheckDeploymentRollout(t *testing.T) {
	dep, _ := deploymentWithHistory("default", "nginx", "nginx:1.0", nil)
	dep.Status = appsv1.DeploymentStatus{
		ObservedGeneration: 3,
		UpdatedReplicas:    1,
		AvailableReplicas:  1,
		Replicas:           1,
	}
	e := newFakeExecutor(t, dep)
	if r := e.checkDeploymentRollout(context.Background(), "default", "nginx"); !r.ok {
		t.Errorf("expected rollout complete, got: %s", r.message)
	}

	// 滚动中：可用副本不足
	dep2, _ := deploymentWithHistory("default", "busy", "busy:1.0", nil)
	dep2.Status = appsv1.DeploymentStatus{ObservedGeneration: 3, UpdatedReplicas: 1, AvailableReplicas: 0, Replicas: 2}
	e2 := newFakeExecutor(t, dep2)
	if r := e2.checkDeploymentRollout(context.Background(), "default", "busy"); r.ok {
		t.Error("expected rollout incomplete while pods unavailable")
	}

	// 控制器尚未处理最新变更
	dep3, _ := deploymentWithHistory("default", "lag", "lag:1.0", nil)
	dep3.Status = appsv1.DeploymentStatus{ObservedGeneration: 1, UpdatedReplicas: 1, AvailableReplicas: 1, Replicas: 1}
	e3 := newFakeExecutor(t, dep3)
	if r := e3.checkDeploymentRollout(context.Background(), "default", "lag"); r.ok {
		t.Error("expected incomplete while observedGeneration lags")
	}
}

func TestCheckNewPodReady(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "nginx-abc", Namespace: "default", UID: types.UID("new-uid")},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
	e := newFakeExecutor(t, pod)

	// 旧 UID 不同、新 Pod Ready → 验证通过
	r := e.checkNewPodReady(context.Background(), "default", "nginx-abc", podSnapshot{uid: types.UID("old-uid")})
	if !r.ok {
		t.Errorf("expected verified, got: %s", r.message)
	}
	// 原 Pod（同 UID）还在 → 未通过
	r = e.checkNewPodReady(context.Background(), "default", "nginx-abc", podSnapshot{uid: types.UID("new-uid")})
	if r.ok {
		t.Error("expected not verified while original pod still exists")
	}
	// Pod 不存在且无控制器快照 → 未通过并给出原因
	r = e.checkNewPodReady(context.Background(), "default", "gone", podSnapshot{})
	if r.ok {
		t.Error("expected not verified for missing pod without snapshot")
	}
}

func TestVerifyClassification(t *testing.T) {
	if !canAutoRollback(ex.ActionUpdateDeploymentImage) || !canAutoRollback(ex.ActionUpdateResourceLimits) {
		t.Error("image/limits actions should be auto-rollbackable")
	}
	if canAutoRollback(ex.ActionRestartPod) || canAutoRollback(ex.ActionScaleDeployment) ||
		canAutoRollback(ex.ActionDeletePod) || canAutoRollback(ex.ActionRolloutUndo) {
		t.Error("restart/scale/delete/undo should not auto-rollback")
	}
	if !actionNeedsVerification(ex.ActionRolloutUndo) || !actionNeedsVerification(ex.ActionDeletePod) {
		t.Error("new actions should require verification")
	}
	if actionNeedsVerification(ex.ActionUpdateConfigMap) {
		t.Error("configmap update has no verification yet")
	}
}

func TestAuditStoreUpdateResult(t *testing.T) {
	store := newMemoryAuditStore()
	planID := "plan-1"
	log := ex.AuditLog{
		ID:     "audit-x",
		Plan:   ex.ExecutionPlan{ID: planID, Action: ex.ActionDeletePod},
		Result: ex.ExecutionResult{Success: true, Message: "pod deleted"},
	}
	if err := store.Save(log); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := store.UpdateResult(planID, false, "pod deleted | 执行验证失败: 新 Pod 未就绪"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := store.GetByID("audit-x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Result.Success {
		t.Error("success should be false after verification failure")
	}
	if got.Result.Message == "pod deleted" {
		t.Error("message should be updated with verification result")
	}
	if err := store.UpdateResult("nonexistent", true, "x"); err == nil {
		t.Error("expected error for unknown plan id")
	}
}

// 序列化保障：模板快照必须能无损还原
func TestTemplateSnapshotRoundTrip(t *testing.T) {
	dep, _ := deploymentWithHistory("default", "nginx", "nginx:1.0", nil)
	b, err := json.Marshal(dep.Spec.Template)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var tmpl corev1.PodTemplateSpec
	if err := json.Unmarshal(b, &tmpl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tmpl.Spec.Containers[0].Image != "nginx:1.0" {
		t.Error("round trip lost container image")
	}
}
