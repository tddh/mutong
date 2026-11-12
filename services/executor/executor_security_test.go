package executor

import (
	"context"
	"testing"

	cfg "gitee.com/tddh/mutong/config"
	diagModel "gitee.com/tddh/mutong/models/diagnosis"
	ex "gitee.com/tddh/mutong/models/executor"
	audstore "gitee.com/tddh/mutong/services/audit"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// SEC-09：自动执行动作白名单——滚动重启/中风险/未知动作一律转人工提案
func TestShouldExecuteWhitelist(t *testing.T) {
	b := NewRemediationBridge()
	cases := []struct {
		name     string
		action   string
		risk     string
		fixable  bool
		autoMode bool
		want     bool
	}{
		{"pod restart low", "restart_pod", "low", true, true, true},
		{"legacy restart low", "Restart", "low", true, true, true},
		{"rollout restart low", "rollout_restart", "low", true, true, false},
		{"legacy rollback", "Rollback", "low", true, true, false},
		{"scale", "scale_deployment", "low", true, true, false},
		{"delete", "delete_pod", "low", true, true, false},
		{"image", "update_deployment_image", "low", true, true, false},
		{"medium risk restart", "restart_pod", "medium", true, true, false},
		{"high risk restart", "restart_pod", "high", true, true, false},
		{"unknown risk", "restart_pod", "", true, true, false},
		{"unknown action", "do_something", "low", true, true, false},
		{"not auto fixable", "restart_pod", "low", false, true, false},
		{"auto mode off", "restart_pod", "low", true, false, false},
	}
	for _, tc := range cases {
		rem := diagModel.RemediationSuggestion{Action: tc.action, RiskLevel: tc.risk, AutoFixable: tc.fixable}
		if got := b.ShouldExecute(rem, tc.autoMode); got != tc.want {
			t.Errorf("%s: ShouldExecute = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// SEC-09：LLM 自报低风险 + 高置信度也不能让滚动重启自动执行，但仍产出人工提案
func TestCreatePlanRolloutRestartRequiresApproval(t *testing.T) {
	b := NewRemediationBridge()
	result := &diagModel.DiagnosisResult{
		Summary:    "llm injected suggestion",
		RootCauses: []diagModel.RootCause{{ResourceType: "Deployment", ResourceName: "nginx", Namespace: "default", Confidence: 0.99}},
		Remediations: []diagModel.RemediationSuggestion{
			{Action: "rollout_restart", RiskLevel: "low", AutoFixable: true},
		},
	}
	plan, shouldAuto := b.CreatePlanFromDiagnosis(result, true)
	if plan == nil {
		t.Fatal("expected a proposal plan for rollout_restart")
	}
	if shouldAuto {
		t.Fatal("rollout_restart must never auto-execute on LLM-reported risk/confidence")
	}
}

func newSecurityTestExecutor(t *testing.T, conf *cfg.Config, objs ...runtime.Object) *K8sExecutor {
	t.Helper()
	e := &K8sExecutor{
		logger:     &testLogger{},
		k8sClient:  fake.NewSimpleClientset(objs...),
		cfg:        conf,
		auditStore: newMemoryAuditStore(),
	}
	if conf != nil {
		e.autoMode.Store(conf.GetExecutorConf().AutoMode)
	}
	return e
}

func runningPod(ns, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func executorConf(autoMode bool, actions map[string]cfg.ActionConf) *cfg.Config {
	return &cfg.Config{Executor: cfg.ExecutorConf{
		Enabled:          true,
		AutoMode:         autoMode,
		DefaultNamespace: "default",
		MaxRestartCount:  5,
		CoolDownMinutes:  5,
		GracePeriodSec:   30,
		Actions:          actions,
	}}
}

// SEC-09：执行侧门槛——白名单外的动作即使自动模式开启 + 置信度拉满也只生成待审批提案
func TestExecuteAutoModeGates(t *testing.T) {
	lowRisk := map[string]cfg.ActionConf{
		"restart_pod":     {Risk: "low", AutoThreshold: 0.7},
		"rollout_restart": {Risk: "low", AutoThreshold: 0.7},
	}

	t.Run("rollout restart denied", func(t *testing.T) {
		dep, _ := deploymentWithHistory("default", "nginx", "nginx:1.0", nil)
		e := newSecurityTestExecutor(t, executorConf(true, lowRisk), dep)
		res, err := e.Execute(context.Background(), ex.ExecutionPlan{
			ID: "p1", Action: ex.ActionRolloutRestart, Namespace: "default", ResourceName: "nginx", Confidence: 0.99,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Success {
			t.Fatal("rollout_restart must not auto-execute")
		}
		if res.Message != "pending approval: action is not allowed for auto-execution" {
			t.Errorf("unexpected message: %q", res.Message)
		}
		got, _ := e.k8sClient.AppsV1().Deployments("default").Get(context.Background(), "nginx", metav1.GetOptions{})
		if _, ok := got.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"]; ok {
			t.Error("deployment must not be restarted")
		}
	})

	t.Run("confidence below threshold denied", func(t *testing.T) {
		e := newSecurityTestExecutor(t, executorConf(true, lowRisk), runningPod("default", "pod-1"))
		res, err := e.Execute(context.Background(), ex.ExecutionPlan{
			ID: "p2", Action: ex.ActionRestartPod, Namespace: "default", ResourceName: "pod-1", Confidence: 0.5,
		})
		if err != nil || res.Success {
			t.Fatalf("expected denial, got res=%+v err=%v", res, err)
		}
		if res.Message != "pending approval: confidence below threshold" {
			t.Errorf("unexpected message: %q", res.Message)
		}
	})

	t.Run("config risk not low denied", func(t *testing.T) {
		actions := map[string]cfg.ActionConf{"restart_pod": {Risk: "medium", AutoThreshold: 0.7}}
		e := newSecurityTestExecutor(t, executorConf(true, actions), runningPod("default", "pod-1"))
		res, _ := e.Execute(context.Background(), ex.ExecutionPlan{
			ID: "p3", Action: ex.ActionRestartPod, Namespace: "default", ResourceName: "pod-1", Confidence: 0.99,
		})
		if res.Success {
			t.Fatal("LLM-reported confidence must not override configured risk")
		}
	})

	t.Run("action not configured denied", func(t *testing.T) {
		e := newSecurityTestExecutor(t, executorConf(true, map[string]cfg.ActionConf{}), runningPod("default", "pod-1"))
		res, _ := e.Execute(context.Background(), ex.ExecutionPlan{
			ID: "p4", Action: ex.ActionRestartPod, Namespace: "default", ResourceName: "pod-1", Confidence: 0.99,
		})
		if res.Success {
			t.Fatal("action without explicit low-risk config must not auto-execute")
		}
	})

	t.Run("auto mode off denied", func(t *testing.T) {
		e := newSecurityTestExecutor(t, executorConf(false, lowRisk), runningPod("default", "pod-1"))
		res, _ := e.Execute(context.Background(), ex.ExecutionPlan{
			ID: "p5", Action: ex.ActionRestartPod, Namespace: "default", ResourceName: "pod-1", Confidence: 0.99,
		})
		if res.Success || res.Message != "approval required" {
			t.Fatalf("expected approval required, got %+v", res)
		}
	})

	t.Run("whitelisted low risk executes", func(t *testing.T) {
		e := newSecurityTestExecutor(t, executorConf(true, lowRisk), runningPod("default", "pod-1"))
		res, err := e.Execute(context.Background(), ex.ExecutionPlan{
			ID: "p6", Action: ex.ActionRestartPod, Namespace: "default", ResourceName: "pod-1", Confidence: 0.9,
		})
		if err != nil || !res.Success {
			t.Fatalf("expected auto-execution of whitelisted low-risk restart, got res=%+v err=%v", res, err)
		}
	})

	t.Run("manual approval bypasses auto gate", func(t *testing.T) {
		dep, _ := deploymentWithHistory("default", "nginx", "nginx:1.0", nil)
		e := newSecurityTestExecutor(t, executorConf(false, lowRisk), dep)
		res, err := e.Execute(context.Background(), ex.ExecutionPlan{
			ID: "p7", Action: ex.ActionRolloutRestart, Namespace: "default", ResourceName: "nginx",
			Confidence: 0.1, ApprovedBy: "alice",
		})
		if err != nil || !res.Success {
			t.Fatalf("approved plan must execute, got res=%+v err=%v", res, err)
		}
	})
}

// SEC-07：构造时按配置自动接线操作审计，每个动作留痕
func TestOperationAuditWiredFromConfig(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	conf := &cfg.Config{DB: db}
	conf.Executor.AuditLog.Type = "postgres"
	conf.Executor.AuditLog.Table = "executor_audit_logs"

	e := NewK8sExecutor(&testLogger{}, nil, conf)
	defer e.Stop()

	if err := e.RecordAudit(context.Background(), ex.AuditLog{
		ID:     "audit-op-1",
		Plan:   ex.ExecutionPlan{ID: "plan-op-1", Action: ex.ActionRestartPod, Target: "default/pod-1", Namespace: "default", ResourceName: "pod-1", Reason: "oom"},
		Result: ex.ExecutionResult{Success: true, Message: "pod restarted"},
	}); err != nil {
		t.Fatalf("RecordAudit: %v", err)
	}

	store, err := audstore.NewPostgresOperationAuditStore(db, cfg.DefaultOperationAuditTable)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := store.List(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 operation audit record, got %d", len(logs))
	}
	if logs[0].Action != string(ex.ActionRestartPod) || logs[0].Namespace != "default" ||
		logs[0].ResourceName != "pod-1" || logs[0].Operator != "system" {
		t.Errorf("unexpected operation audit record: %+v", logs[0])
	}
}

// SEC-07：可选 setter 向后兼容，未配置数据库时不 panic
func TestSetOperationAuditStore(t *testing.T) {
	e := NewK8sExecutor(&testLogger{}, nil, nil)
	defer e.Stop()

	store, err := audstore.NewPostgresOperationAuditStore(nil, "")
	if err == nil || store != nil {
		t.Fatal("nil db must be rejected by the operation audit store")
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	injected, err := audstore.NewPostgresOperationAuditStore(db, "custom_op_audit")
	if err != nil {
		t.Fatal(err)
	}
	e.SetOperationAuditStore(injected)
	if err := e.RecordAudit(context.Background(), ex.AuditLog{
		ID:     "audit-op-2",
		Plan:   ex.ExecutionPlan{ID: "plan-op-2", Action: ex.ActionDeletePod, Namespace: "ns1", ResourceName: "pod-2", ApprovedBy: "bob"},
		Result: ex.ExecutionResult{Success: true, Message: "pod deleted"},
	}); err != nil {
		t.Fatalf("RecordAudit: %v", err)
	}
	logs, err := injected.List(map[string]string{})
	if err != nil || len(logs) != 1 {
		t.Fatalf("expected injected store to receive the record, got %d (%v)", len(logs), err)
	}
	if logs[0].Operator != "bob" {
		t.Errorf("approver must be recorded as operator, got %q", logs[0].Operator)
	}
}
