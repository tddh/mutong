package executor

import (
	"context"
	"testing"

	diagModel "gitee.com/tddh/mutong/models/diagnosis"
	appsv1 "k8s.io/api/apps/v1"
)

func TestPickRollbackTarget(t *testing.T) {
	e := newFakeExecutor(t)

	// 既无已知健康版本也无快照 → 空
	if tgt, src := e.pickRollbackTarget("default", "nginx", ""); tgt != "" || src != "" {
		t.Errorf("expected empty target, got %q/%q", tgt, src)
	}
	// 只有快照 → 用快照
	if tgt, src := e.pickRollbackTarget("default", "nginx", "SNAP"); tgt != "SNAP" || src != "变更前快照" {
		t.Errorf("expected snapshot fallback, got %q/%q", tgt, src)
	}
	// 有已知健康版本 → 优先它（即便快照存在）
	e.knownGood.Store("default/nginx", "GOOD")
	if tgt, src := e.pickRollbackTarget("default", "nginx", "SNAP"); tgt != "GOOD" || src != "最近已知健康版本" {
		t.Errorf("expected knownGood preferred over snapshot, got %q/%q", tgt, src)
	}
}

func TestSnapshotRecordsKnownGoodOnlyWhenHealthy(t *testing.T) {
	// 健康 Deployment：快照应记录为已知健康版本
	dep, _ := deploymentWithHistory("default", "nginx", "nginx:1.0", nil)
	dep.Status = appsv1.DeploymentStatus{ObservedGeneration: 3, UpdatedReplicas: 1, AvailableReplicas: 1, Replicas: 1}
	e := newFakeExecutor(t, dep)
	if snap := e.snapshotDeploymentTemplate(context.Background(), "default", "nginx"); snap == "" {
		t.Fatal("snapshot should not be empty")
	}
	if _, ok := e.knownGood.Load("default/nginx"); !ok {
		t.Error("healthy deployment should be recorded as known-good")
	}

	// 不健康 Deployment（rollout 卡住）：不应记录，避免把坏状态当成回滚目标
	dep2, _ := deploymentWithHistory("default", "busy", "busy:bad", nil)
	dep2.Status = appsv1.DeploymentStatus{ObservedGeneration: 1, UpdatedReplicas: 0, AvailableReplicas: 0, Replicas: 1}
	e2 := newFakeExecutor(t, dep2)
	e2.snapshotDeploymentTemplate(context.Background(), "default", "busy")
	if _, ok := e2.knownGood.Load("default/busy"); ok {
		t.Error("unhealthy deployment must NOT be recorded as known-good")
	}
}

func TestDeploymentRolloutStatus(t *testing.T) {
	healthy, _ := deploymentWithHistory("default", "h", "h:1", nil)
	healthy.Status = appsv1.DeploymentStatus{ObservedGeneration: 3, UpdatedReplicas: 1, AvailableReplicas: 1, Replicas: 1}
	if !deploymentRolloutStatus(healthy).ok {
		t.Error("expected healthy rollout")
	}
	// observedGeneration 落后 → 不健康
	lag, _ := deploymentWithHistory("default", "l", "l:1", nil)
	lag.Status = appsv1.DeploymentStatus{ObservedGeneration: 1, UpdatedReplicas: 1, AvailableReplicas: 1, Replicas: 1}
	if deploymentRolloutStatus(lag).ok {
		t.Error("lagging observedGeneration should be unhealthy")
	}
}

// bridge 不应为"无法完整参数化"的动作生成计划（批准必失败）
func TestBridgeSkipsUnparameterizableActions(t *testing.T) {
	b := NewRemediationBridge()
	mk := func(action, resType string) *diagModel.DiagnosisResult {
		return &diagModel.DiagnosisResult{
			Summary:    "test",
			RootCauses: []diagModel.RootCause{{ResourceType: resType, ResourceName: "res", Namespace: "default", Confidence: 0.9}},
			Remediations: []diagModel.RemediationSuggestion{
				{Action: action, RiskLevel: "high", AutoFixable: true},
			},
		}
	}

	// 改镜像：诊断不含目标镜像 → 不生成计划（这正是之前的缺陷）
	if p, _ := b.CreatePlanFromDiagnosis(mk("update_deployment_image", "Deployment"), false); p != nil {
		t.Error("update_deployment_image should NOT produce a plan without a concrete image")
	}
	// 调资源：诊断不含具体资源值 → 不生成
	if p, _ := b.CreatePlanFromDiagnosis(mk("update_resource_limits", "Deployment"), false); p != nil {
		t.Error("update_resource_limits should NOT produce a plan without configData")
	}
	// 扩缩容：诊断不含副本数 → 不生成（否则会误缩到 0）
	if p, _ := b.CreatePlanFromDiagnosis(mk("scale_deployment", "Deployment"), false); p != nil {
		t.Error("scale_deployment should NOT produce a plan without replicas")
	}
	// Deployment 级动作但根因是 Pod → target 会错 → 不生成
	if p, _ := b.CreatePlanFromDiagnosis(mk("rollout_restart", "Pod"), false); p != nil {
		t.Error("rollout_restart on a Pod root cause should NOT produce a plan")
	}

	// 正常路径仍要生成：restart_pod（Pod 级、无需额外参数）
	if p, _ := b.CreatePlanFromDiagnosis(mk("restart_pod", "Pod"), false); p == nil {
		t.Error("restart_pod should still produce a plan")
	}
	// rollout_restart + Deployment 根因 → 生成
	if p, _ := b.CreatePlanFromDiagnosis(mk("rollout_restart", "Deployment"), false); p == nil {
		t.Error("rollout_restart on a Deployment root cause should produce a plan")
	}
}
