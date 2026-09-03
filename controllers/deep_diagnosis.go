package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	alert_models "gitee.com/tddh/mutong/models/alert"
	diagModel "gitee.com/tddh/mutong/models/diagnosis"
	diagnosis_svc "gitee.com/tddh/mutong/services/diagnosis"
	"gitee.com/tddh/mutong/services/executor"
)

const deepDiagnosisSystemPrompt = `你是 Kubernetes 故障排查 Agent。集群中出现了告警，请使用可用工具逐步排查（查看资源状态、日志、指标、拓扑关系等），找出根因并给出修复建议。

要求：
1. 必须先调用工具获取证据再下结论，不要凭空猜测。
2. 排查完成后只输出一个严格的 JSON 对象（不要 markdown，不要其他任何文字）：
{"summary":"简要结论","root_cause":"根因描述","confidence":0.0-1.0,"evidence":["证据1","证据2"],"remediation":{"action":"动作名","description":"修复说明","risk_level":"low|medium|high","auto_fixable":true}}
3. action 只能是以下值之一：restart_pod（重启 Pod）、rollout_restart（滚动重启 Deployment）、scale_deployment（扩缩容）、update_deployment_image（修改镜像）、adjust_resource_limits（调整 CPU/内存限制）、delete_pod（删除 Pod 由控制器重建）、rollout_undo（回滚 Deployment 到历史版本）
4. update_deployment_image、adjust_resource_limits、delete_pod、rollout_undo 的 risk_level 必须为 high
5. confidence 要保守：证据充分才给高值，宁可低不可虚高`

// NewDeepDiagnoseFunc 构造"调工具深度排查"函数，供自动诊断管道在置信度不足时使用。
// 当前 LLM 提供者不支持 Eino 工具调用时返回 nil。
func (c *DiagnosisController) NewDeepDiagnoseFunc() executor.DeepDiagnoseFunc {
	provider, ok := c.chatManager.GetLLMProvider().(*diagnosis_svc.EinoLLMProvider)
	if !ok {
		c.logger.Warn("Deep diagnosis unavailable: LLM provider does not support tool calling")
		return nil
	}
	agent := diagnosis_svc.NewDiagnosisAgent(
		provider.GetChatModel(),
		diagnosis_svc.ConvertToEinoTools(c.mcpSrv.GetEinoToolDefsAndHandlers(), c.logger),
		c.logger,
	)

	return func(ctx context.Context, alert *alert_models.ProcessedAlert, base *diagModel.DiagnosisResult) (*diagModel.DiagnosisResult, error) {
		userMsg := fmt.Sprintf("告警: %s\n资源: %s/%s/%s\n节点: %s\n摘要: %s\n请排查并给出该故障的根因结论。",
			alert.Labels["alertname"], alert.ResourceType, alert.Namespace, alert.ResourceName,
			alert.NodeName, alert.Annotations["summary"])

		dctx, cancel := context.WithTimeout(ctx, 150*time.Second)
		defer cancel()
		result, err := agent.Run(dctx, userMsg, deepDiagnosisSystemPrompt)
		if err != nil {
			return nil, fmt.Errorf("deep diagnosis agent failed: %w", err)
		}

		parsed, err := parseDeepDiagnosisJSON(result.Content)
		if err != nil {
			c.logger.Warn("Deep diagnosis output not parseable, keeping base result",
				zap.String("fingerprint", alert.Fingerprint), zap.Error(err))
			return nil, err
		}

		res := &diagModel.DiagnosisResult{
			ID:        base.ID,
			Timestamp: time.Now(),
			Request:   base.Request,
			Summary:   parsed.Summary,
			RootCauses: []diagModel.RootCause{{
				ResourceType: alert.ResourceType,
				ResourceName: alert.ResourceName,
				Namespace:    alert.Namespace,
				Confidence:   parsed.Confidence,
				Evidence:     parsed.Evidence,
			}},
			Remediations: []diagModel.RemediationSuggestion{{
				Action:      parsed.Remediation.Action,
				Description: parsed.Remediation.Description,
				RiskLevel:   parsed.Remediation.RiskLevel,
				AutoFixable: parsed.Remediation.AutoFixable,
			}},
		}
		return res, nil
	}
}

type deepDiagnosisOutput struct {
	Summary     string   `json:"summary"`
	RootCause   string   `json:"root_cause"`
	Confidence  float64  `json:"confidence"`
	Evidence    []string `json:"evidence"`
	Remediation struct {
		Action      string `json:"action"`
		Description string `json:"description"`
		RiskLevel   string `json:"risk_level"`
		AutoFixable bool   `json:"auto_fixable"`
	} `json:"remediation"`
}

func parseDeepDiagnosisJSON(content string) (*deepDiagnosisOutput, error) {
	end := strings.LastIndex(content, "}")
	if end < 0 {
		return nil, fmt.Errorf("no JSON object found in agent output")
	}
	candidate := content[:end+1]
	var found *deepDiagnosisOutput
	// 遍历每个 '{' 起点，取最后一个包含必需字段的合法 JSON（最终回答通常在末尾）
	for {
		start := strings.Index(candidate, "{")
		if start < 0 {
			break
		}
		var out deepDiagnosisOutput
		if err := json.Unmarshal([]byte(candidate[start:]), &out); err == nil &&
			out.RootCause != "" && out.Remediation.Action != "" {
			found = &out
		}
		candidate = candidate[start+1:]
	}
	if found == nil {
		return nil, fmt.Errorf("no valid deep-diagnosis JSON in agent output")
	}
	return found, nil
}
