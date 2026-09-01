package mcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/diagnosis"
	ex "gitee.com/tddh/mutong/models/executor"
)

// DiagnosisResultReader 按告警指纹读取诊断结果，用于给执行提议提供置信度依据
type DiagnosisResultReader interface {
	GetCachedDiagnosisResult(ctx context.Context, fingerprint string) (*diagnosis.DiagnosisResult, error)
}

// WithSelfHealing 注入自愈执行依赖并注册执行类工具（安全组）。
// 未注入时执行工具不会出现在工具列表中。
func (s *Server) WithSelfHealing(exec interfaces.Executor, reader DiagnosisResultReader) *Server {
	s.executor = exec
	s.diagReader = reader
	if exec != nil {
		s.tools["restart_pod_safe"] = s.handleRestartPodSafe
		s.tools["rollout_restart"] = s.handleRolloutRestart
		s.tools["scale_deployment"] = s.handleScaleDeployment
		s.tools["update_deployment_image"] = s.handleUpdateDeploymentImage
		s.tools["adjust_resource_limits"] = s.handleAdjustResourceLimits
	}
	return s
}

// execToolDefs 执行类工具定义。安全组（重启/滚动重启/扩缩容）走 autoMode+审批门禁；
// 提案组（改镜像/改资源限制）永远只生成待审批提议，绝不自动执行。
func execToolDefs() []diagnosis.ToolDefinition {
	return []diagnosis.ToolDefinition{
		{
			Name:        "restart_pod_safe",
			Description: "【执行动作】提议安全重启指定 Pod（使用驱逐方式，尊重 PDB 和优雅终止）。调用不等于立即执行：是否执行由系统自动模式与审批机制决定，可在执行记录中查看结果。仅在已确认根因且重启是合理修复手段时使用。",
			Parameters: []diagnosis.ParamDef{
				{Name: "namespace", Required: true, Description: "Pod 所在命名空间"},
				{Name: "pod_name", Required: true, Description: "Pod 名称"},
				{Name: "fingerprint", Required: true, Description: "关联告警指纹（必填，提供诊断置信度依据）"},
				{Name: "reason", Required: false, Description: "执行理由（简述为什么重启能解决问题）"},
			},
		},
		{
			Name:        "rollout_restart",
			Description: "【执行动作】提议滚动重启指定 Deployment（等价 kubectl rollout restart，逐个替换 Pod，业务不中断）。调用不等于立即执行：是否执行由系统自动模式与审批机制决定。仅在已确认根因且需要重建全部 Pod 时使用（如 OOM、内存泄漏、配置需生效）。",
			Parameters: []diagnosis.ParamDef{
				{Name: "namespace", Required: true, Description: "Deployment 所在命名空间"},
				{Name: "deployment", Required: true, Description: "Deployment 名称"},
				{Name: "fingerprint", Required: true, Description: "关联告警指纹（必填，提供诊断置信度依据）"},
				{Name: "reason", Required: false, Description: "执行理由"},
			},
		},
		{
			Name:        "scale_deployment",
			Description: "【执行动作】提议扩缩容指定 Deployment 的副本数（受系统副本上限约束）。调用不等于立即执行：是否执行由系统自动模式与审批机制决定。仅在负载类问题（如 CPU 高、请求排队）且扩容是合理手段时使用。",
			Parameters: []diagnosis.ParamDef{
				{Name: "namespace", Required: true, Description: "Deployment 所在命名空间"},
				{Name: "deployment", Required: true, Description: "Deployment 名称"},
				{Name: "replicas", Required: true, Description: "目标副本数（整数）"},
				{Name: "fingerprint", Required: true, Description: "关联告警指纹（必填，提供诊断置信度依据）"},
				{Name: "reason", Required: false, Description: "执行理由"},
			},
		},
		{
			Name:        "update_deployment_image",
			Description: "【提案动作·危险】提议修改指定 Deployment 容器的镜像（如镜像名/标签写错、需要换版本）。此动作只会生成待审批提议，永远不会自动执行，必须由人工在执行记录中批准后才生效。仅在已确认根因就是镜像问题时使用。",
			Parameters: []diagnosis.ParamDef{
				{Name: "namespace", Required: true, Description: "Deployment 所在命名空间"},
				{Name: "deployment", Required: true, Description: "Deployment 名称"},
				{Name: "image", Required: true, Description: "目标镜像完整引用（如 harbor.example.com/app/name:tag）"},
				{Name: "container", Required: false, Description: "容器名（多容器时必填，默认第一个容器）"},
				{Name: "fingerprint", Required: true, Description: "关联告警指纹（必填，提供诊断置信度依据）"},
				{Name: "reason", Required: false, Description: "提议理由"},
			},
		},
		{
			Name:        "adjust_resource_limits",
			Description: "【提案动作·危险】提议调整指定 Deployment 容器的 CPU/内存 requests/limits（OOM、CPU 限流的根治手段）。此动作只会生成待审批提议，永远不会自动执行，必须由人工在执行记录中批准后才生效。至少提供一个资源值参数。",
			Parameters: []diagnosis.ParamDef{
				{Name: "namespace", Required: true, Description: "Deployment 所在命名空间"},
				{Name: "deployment", Required: true, Description: "Deployment 名称"},
				{Name: "cpu_limit", Required: false, Description: "CPU 上限（如 500m、2）"},
				{Name: "memory_limit", Required: false, Description: "内存上限（如 512Mi、2Gi）"},
				{Name: "cpu_request", Required: false, Description: "CPU 请求（如 250m）"},
				{Name: "memory_request", Required: false, Description: "内存请求（如 256Mi）"},
				{Name: "container", Required: false, Description: "容器名（多容器时必填，默认第一个容器）"},
				{Name: "fingerprint", Required: true, Description: "关联告警指纹（必填，提供诊断置信度依据）"},
				{Name: "reason", Required: false, Description: "提议理由"},
			},
		},
	}
}

// planAndSubmit 构造执行计划并交给 executor 的门禁链路（autoMode + 置信度阈值 + 审计）。
// 没有诊断结果支撑的调用直接拒绝，防止凭空执行。
func (s *Server) planAndSubmit(ctx context.Context, action ex.ActionType, risk ex.RiskLevel, namespace, resourceName, fingerprint, reason string, apply func(p *ex.ExecutionPlan)) (string, error) {
	if s.executor == nil {
		return "", fmt.Errorf("自愈执行器未启用（executor 未配置）")
	}

	// 同指纹同动作已有待审批计划时去重，避免审批列表堆重复项
	if pendingID := s.findPendingPlan(ctx, fingerprint, action); pendingID != "" {
		return fmt.Sprintf("该告警已有一个待审批的执行计划（审计ID: %s，动作: %s），请直接到执行记录中审批，不重复创建", pendingID, action), nil
	}

	var confidence float64
	if s.diagReader != nil && fingerprint != "" {
		if res, err := s.diagReader.GetCachedDiagnosisResult(ctx, fingerprint); err == nil && res != nil {
			confidence = maxConf(res.RootCauses)
			if reason == "" {
				reason = res.Summary
			}
		}
	}
	if confidence <= 0 {
		return "", fmt.Errorf("未找到指纹 %s 的诊断结果（可能已过期），无法为执行提议提供置信度依据，拒绝执行", fingerprint)
	}
	if reason == "" {
		reason = "AI 诊断建议执行 " + string(action)
	}

	target := resourceName
	if namespace != "" {
		target = namespace + "/" + resourceName
	}
	plan := &ex.ExecutionPlan{
		ID:           uuid.New().String(),
		Action:       action,
		Target:       target,
		Namespace:    namespace,
		ResourceName: resourceName,
		Reason:       reason,
		Confidence:   confidence,
		Risk:         risk,
		Fingerprint:  fingerprint,
		CreatedAt:    time.Now(),
	}
	if apply != nil {
		apply(plan)
	}

	result, err := s.executor.Execute(ctx, *plan)
	if err != nil {
		return "", fmt.Errorf("执行失败: %w", err)
	}
	if result == nil {
		return "", fmt.Errorf("执行器未返回结果")
	}
	if result.Success {
		return fmt.Sprintf("执行成功（计划ID: %s）: %s", plan.ID, result.Message), nil
	}
	return fmt.Sprintf("执行提议已提交但未执行（计划ID: %s）: %s。若提示需要审批，请在前端执行记录中人工审批。", plan.ID, result.Message), nil
}

// proposalAndRecord 危险动作专用：构造计划后只记录为"待审批"审计，绝不执行（不走 autoMode 门禁）。
// 与安全组相同的准入约束：必须有诊断结果支撑、同指纹同动作去重。
func (s *Server) proposalAndRecord(ctx context.Context, action ex.ActionType, risk ex.RiskLevel, namespace, resourceName, fingerprint, reason string, apply func(p *ex.ExecutionPlan)) (string, error) {
	if s.executor == nil {
		return "", fmt.Errorf("自愈执行器未启用（executor 未配置）")
	}

	if pendingID := s.findPendingPlan(ctx, fingerprint, action); pendingID != "" {
		return fmt.Sprintf("该告警已有一个待审批的执行计划（审计ID: %s，动作: %s），请直接到执行记录中审批，不重复创建", pendingID, action), nil
	}

	var confidence float64
	if s.diagReader != nil && fingerprint != "" {
		if res, err := s.diagReader.GetCachedDiagnosisResult(ctx, fingerprint); err == nil && res != nil {
			confidence = maxConf(res.RootCauses)
			if reason == "" {
				reason = res.Summary
			}
		}
	}
	if confidence <= 0 {
		return "", fmt.Errorf("未找到指纹 %s 的诊断结果（可能已过期），无法为执行提议提供置信度依据，拒绝执行", fingerprint)
	}
	if reason == "" {
		reason = "AI 诊断建议执行 " + string(action)
	}

	target := resourceName
	if namespace != "" {
		target = namespace + "/" + resourceName
	}
	plan := ex.ExecutionPlan{
		ID:           uuid.New().String(),
		Action:       action,
		Target:       target,
		Namespace:    namespace,
		ResourceName: resourceName,
		Reason:       reason,
		Confidence:   confidence,
		Risk:         risk,
		Fingerprint:  fingerprint,
		CreatedAt:    time.Now(),
	}
	if apply != nil {
		apply(&plan)
	}

	audit := ex.AuditLog{
		ID:           fmt.Sprintf("audit-%s", uuid.New().String()),
		Plan:         plan,
		Result:       ex.ExecutionResult{Success: false, Message: "pending approval (not auto-executed)", Timestamp: time.Now()},
		AutoExecuted: false,
		ApprovedBy:   "",
		Timestamp:    time.Now(),
	}
	if err := s.executor.RecordAudit(ctx, audit); err != nil {
		return "", fmt.Errorf("记录待审批计划失败: %w", err)
	}
	return fmt.Sprintf("已生成待审批提议（计划ID: %s，动作: %s，目标: %s）。该动作风险较高，不会自动执行，请提醒用户到前端执行记录中审批。", plan.ID, action, target), nil
}

func (s *Server) handleUpdateDeploymentImage(ctx context.Context, args map[string]string) (string, error) {
	namespace := args["namespace"]
	deployment := args["deployment"]
	image := strings.TrimSpace(args["image"])
	fingerprint := args["fingerprint"]
	if namespace == "" || deployment == "" || image == "" || fingerprint == "" {
		return "", fmt.Errorf("namespace、deployment、image、fingerprint 均为必填")
	}
	if !isValidNamespace(namespace) {
		return "", fmt.Errorf("invalid namespace: %s", namespace)
	}
	if !isValidResourceName(deployment) {
		return "", fmt.Errorf("invalid deployment name: %s", deployment)
	}
	return s.proposalAndRecord(ctx, ex.ActionUpdateDeploymentImage, ex.RiskHigh, namespace, deployment, fingerprint, args["reason"], func(p *ex.ExecutionPlan) {
		p.Image = image
		p.ContainerName = args["container"]
	})
}

func (s *Server) handleAdjustResourceLimits(ctx context.Context, args map[string]string) (string, error) {
	namespace := args["namespace"]
	deployment := args["deployment"]
	fingerprint := args["fingerprint"]
	if namespace == "" || deployment == "" || fingerprint == "" {
		return "", fmt.Errorf("namespace、deployment、fingerprint 均为必填")
	}
	configData := map[string]string{}
	for _, k := range []string{"cpu_limit", "memory_limit", "cpu_request", "memory_request"} {
		if v := strings.TrimSpace(args[k]); v != "" {
			configData[k] = v
		}
	}
	if len(configData) == 0 {
		return "", fmt.Errorf("至少提供一个资源值参数：cpu_limit / memory_limit / cpu_request / memory_request")
	}
	if !isValidNamespace(namespace) {
		return "", fmt.Errorf("invalid namespace: %s", namespace)
	}
	if !isValidResourceName(deployment) {
		return "", fmt.Errorf("invalid deployment name: %s", deployment)
	}
	return s.proposalAndRecord(ctx, ex.ActionUpdateResourceLimits, ex.RiskHigh, namespace, deployment, fingerprint, args["reason"], func(p *ex.ExecutionPlan) {
		p.ConfigData = configData
		p.ContainerName = args["container"]
	})
}

// findPendingPlan 查找同指纹同动作的待审批计划，返回其审计记录 ID（无则返回空）
func (s *Server) findPendingPlan(ctx context.Context, fingerprint string, action ex.ActionType) string {
	logs, err := s.executor.GetAuditLogs(ctx, map[string]string{"fingerprint": fingerprint})
	if err != nil {
		return ""
	}
	for _, l := range logs {
		if l.Plan.Action != action || l.Result.Success || l.ApprovedBy != "" {
			continue
		}
		if strings.Contains(l.Result.Message, "pending approval") || strings.Contains(l.Result.Message, "approval required") {
			return l.ID
		}
	}
	return ""
}

func (s *Server) handleRestartPodSafe(ctx context.Context, args map[string]string) (string, error) {
	namespace := args["namespace"]
	podName := args["pod_name"]
	fingerprint := args["fingerprint"]
	if namespace == "" || podName == "" || fingerprint == "" {
		return "", fmt.Errorf("namespace、pod_name、fingerprint 均为必填")
	}
	if !isValidNamespace(namespace) {
		return "", fmt.Errorf("invalid namespace: %s", namespace)
	}
	if !isValidResourceName(podName) {
		return "", fmt.Errorf("invalid pod name: %s", podName)
	}
	return s.planAndSubmit(ctx, ex.ActionRestartPod, ex.RiskLow, namespace, podName, fingerprint, args["reason"], nil)
}

func (s *Server) handleRolloutRestart(ctx context.Context, args map[string]string) (string, error) {
	namespace := args["namespace"]
	deployment := args["deployment"]
	fingerprint := args["fingerprint"]
	if namespace == "" || deployment == "" || fingerprint == "" {
		return "", fmt.Errorf("namespace、deployment、fingerprint 均为必填")
	}
	if !isValidNamespace(namespace) {
		return "", fmt.Errorf("invalid namespace: %s", namespace)
	}
	if !isValidResourceName(deployment) {
		return "", fmt.Errorf("invalid deployment name: %s", deployment)
	}
	return s.planAndSubmit(ctx, ex.ActionRolloutRestart, ex.RiskLow, namespace, deployment, fingerprint, args["reason"], nil)
}

func (s *Server) handleScaleDeployment(ctx context.Context, args map[string]string) (string, error) {
	namespace := args["namespace"]
	deployment := args["deployment"]
	fingerprint := args["fingerprint"]
	if namespace == "" || deployment == "" || fingerprint == "" || args["replicas"] == "" {
		return "", fmt.Errorf("namespace、deployment、replicas、fingerprint 均为必填")
	}
	if !isValidNamespace(namespace) {
		return "", fmt.Errorf("invalid namespace: %s", namespace)
	}
	if !isValidResourceName(deployment) {
		return "", fmt.Errorf("invalid deployment name: %s", deployment)
	}
	replicas, err := strconv.Atoi(args["replicas"])
	if err != nil || replicas < 0 {
		return "", fmt.Errorf("replicas 必须为非负整数: %s", args["replicas"])
	}
	return s.planAndSubmit(ctx, ex.ActionScaleDeployment, ex.RiskLow, namespace, deployment, fingerprint, args["reason"], func(p *ex.ExecutionPlan) {
		p.Replicas = int32(replicas)
	})
}
