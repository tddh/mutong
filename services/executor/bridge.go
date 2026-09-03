package executor

import (
	"context"
	"time"

	"gitee.com/tddh/mutong/interfaces"
	diagModel "gitee.com/tddh/mutong/models/diagnosis"
	exModel "gitee.com/tddh/mutong/models/executor"
	uuid "github.com/google/uuid"
)

// RemediationBridge connects Diagnosis results to Execution plans and execution
// logic. It implements the Phase 1 design:
// DiagnosisResult -> RemediationSuggestion -> ShouldExecute? -> ExecutionPlan -> Executor
type RemediationBridge struct{}

// NewRemediationBridge creates a new bridge instance
func NewRemediationBridge() *RemediationBridge {
	return &RemediationBridge{}
}

// ShouldExecute decides whether to auto-execute a remediation based on
// remediation risk and autoMode. This implementation follows a conservative rule:
// - AutoFixable must be true
// - 危险动作（改镜像、删除、改配置/Secret、改资源限制）永远不可自动执行，无论风险标注如何
// - If RiskLevel == "low" or "medium" and autoMode is ON, allow auto-execution
// - If RiskLevel == "high" or unknown, do not auto-execute
func (b *RemediationBridge) ShouldExecute(remediation diagModel.RemediationSuggestion, autoMode bool) bool {
	if !remediation.AutoFixable {
		return false
	}
	switch remediation.Action {
	case "update_deployment_image", "delete_pod", "update_configmap", "update_secret",
		"update_resource_limits", "rollout_undo", "Rollback", "Delete", "UpdateConfig", "UpdateSecret", "AdjustLimits":
		// 危险动作：只能走人工审批
		return false
	}
	switch remediation.RiskLevel {
	case "low":
		return autoMode
	case "medium":
		return autoMode
	default:
		// high or unknown, require manual approval
		return false
	}
}

// CreatePlanFromDiagnosis converts the diagnosis result into an ExecutionPlan.
// It returns the plan and a boolean indicating whether the plan should be auto-executed
// based on the autoMode flag and remediation risk.
func (b *RemediationBridge) CreatePlanFromDiagnosis(result *diagModel.DiagnosisResult, autoMode bool) (*exModel.ExecutionPlan, bool) {
	if result == nil {
		return nil, false
	}
	if len(result.RootCauses) == 0 || len(result.Remediations) == 0 {
		return nil, false
	}

	root := result.RootCauses[0]
	rem := result.Remediations[0]

	// Map remediation action to executor ActionType with conservative defaults
	var action exModel.ActionType
	switch rem.Action {
	// 标准枚举直通（知识库与执行工具统一使用该命名）
	case string(exModel.ActionRestartPod):
		action = exModel.ActionRestartPod
	case string(exModel.ActionScaleDeployment):
		action = exModel.ActionScaleDeployment
	case string(exModel.ActionDeletePod):
		action = exModel.ActionDeletePod
	case string(exModel.ActionCreateHPA):
		action = exModel.ActionCreateHPA
	case string(exModel.ActionUpdateHPA):
		action = exModel.ActionUpdateHPA
	case string(exModel.ActionUpdateConfigMap):
		action = exModel.ActionUpdateConfigMap
	case string(exModel.ActionUpdateSecret):
		action = exModel.ActionUpdateSecret
	case string(exModel.ActionUpdateResourceLimits):
		action = exModel.ActionUpdateResourceLimits
	case string(exModel.ActionUpdateDeploymentImage):
		action = exModel.ActionUpdateDeploymentImage
	case string(exModel.ActionUpdateAnnotations):
		action = exModel.ActionUpdateAnnotations
	case string(exModel.ActionUpdateLabels):
		action = exModel.ActionUpdateLabels
	case string(exModel.ActionRolloutRestart):
		action = exModel.ActionRolloutRestart
	case string(exModel.ActionRolloutUndo):
		action = exModel.ActionRolloutUndo
	// 兼容 LLM 诊断输出的旧命名
	case "Restart":
		action = exModel.ActionRestartPod
	case "Scale":
		action = exModel.ActionScaleDeployment
	case "Delete":
		action = exModel.ActionDeletePod
	case "CreateHPA":
		action = exModel.ActionCreateHPA
	case "UpdateHPA":
		action = exModel.ActionUpdateHPA
	case "UpdateConfig":
		action = exModel.ActionUpdateConfigMap
	case "UpdateSecret":
		action = exModel.ActionUpdateSecret
	case "AdjustLimits":
		action = exModel.ActionUpdateResourceLimits
	case "Rollback":
		action = exModel.ActionRolloutUndo
	case "UpdateAnnotation":
		action = exModel.ActionUpdateAnnotations
	case "UpdateLabel":
		action = exModel.ActionUpdateLabels
	default:
		switch root.ResourceType {
		case "Pod":
			action = exModel.ActionRestartPod
		case "Deployment":
			action = exModel.ActionScaleDeployment
		default:
			// Node 等类型没有安全的自动执行动作，不生成计划（仅保留诊断结果供人工处理）
			return nil, false
		}
	}

	// bridge 只能生成"可被完整参数化"的计划。诊断结果（规则或 LLM remediation）不携带
	// 目标镜像、资源值、明确副本数，也无法保证根因资源类型与动作层级匹配。这类动作若照样
	// 产出计划，批准后必然执行失败：改镜像/调资源缺参数、对 Pod 名执行 Deployment 级动作、
	// 扩缩容缺副本数会误缩到 0。因此这里直接不生成，改由 MCP 执行工具（AI 显式带上
	// image/configData/replicas、且已定位到正确目标）发起提议。
	switch action {
	case exModel.ActionUpdateDeploymentImage, exModel.ActionUpdateResourceLimits:
		// 诊断不产出目标镜像 / 具体资源值
		return nil, false
	case exModel.ActionScaleDeployment:
		// 诊断不产出明确副本数，避免缩容到 0
		return nil, false
	case exModel.ActionRolloutRestart, exModel.ActionRolloutUndo:
		// Deployment 级动作：根因资源必须是 Deployment，否则 target 会是 Pod 名而失败
		if root.ResourceType != "Deployment" {
			return nil, false
		}
	}

	// Build a reasonable Target identifier
	target := ""
	if root.Namespace != "" {
		target = root.Namespace + "/" + root.ResourceName
	} else {
		target = root.ResourceName
	}

	plan := &exModel.ExecutionPlan{
		ID:           uuid.New().String(),
		Action:       action,
		Target:       target,
		Namespace:    root.Namespace,
		ResourceName: root.ResourceName,
		Reason:       result.Summary,
		Confidence:   root.Confidence,
		Risk:         exModel.RiskLevel(rem.RiskLevel),
		ApprovedBy:   "",
		Fingerprint:  result.Request.Fingerprint,
		CreatedAt:    time.Now(),
	}

	shouldAuto := b.ShouldExecute(rem, autoMode)
	return plan, shouldAuto
}

// ExecuteRemediation delegates the execution of the given plan to the provided executor.
func (b *RemediationBridge) ExecuteRemediation(ctx context.Context, plan *exModel.ExecutionPlan, executor interfaces.Executor) (*exModel.ExecutionResult, error) {
	if plan == nil {
		return nil, nil
	}
	// Execute expects a value, so dereference the pointer
	res, err := executor.Execute(ctx, *plan)
	return res, err
}
