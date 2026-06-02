package diagnosis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/services/prompt"
)

type EinoLLMProvider struct {
	chatModel model.BaseChatModel
	timeout   time.Duration
	provider  string
	model     string
	endpoint  string
	promptMgr *prompt.Manager
}

func NewEinoLLMProvider(chatModel model.BaseChatModel, timeout time.Duration, provider, mdl, endpoint string) interfaces.LLMProvider {
	return &EinoLLMProvider{
		chatModel: chatModel,
		timeout:   timeout,
		provider:  provider,
		model:     mdl,
		endpoint:  endpoint,
	}
}

func (p *EinoLLMProvider) GetChatModel() model.BaseChatModel {
	return p.chatModel
}

func (p *EinoLLMProvider) SetPromptManager(mgr *prompt.Manager) {
	p.promptMgr = mgr
}

func (p *EinoLLMProvider) Diagnose(ctx context.Context, prompt interfaces.DiagnosisPrompt) (*interfaces.LLMDiagnosisResult, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	messages := []*schema.Message{
		schema.SystemMessage(buildDiagnosisSystemPrompt(prompt, p.promptMgr)),
	}

	resp, err := p.chatModel.Generate(ctx, messages)
	if err != nil {
		return nil, err
	}

	return parseLLMResponse(resp.Content)
}

func (p *EinoLLMProvider) GeneratePostmortemInsights(ctx context.Context, prompt interfaces.PostmortemInsightsPrompt) (*interfaces.PostmortemInsightsResult, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	messages := []*schema.Message{
		schema.SystemMessage(buildPostmortemSystemPrompt(prompt)),
	}

	resp, err := p.chatModel.Generate(ctx, messages)
	if err != nil {
		return nil, err
	}

	return parsePostmortemResponse(resp.Content)
}

func buildDiagnosisSystemPrompt(prompt interfaces.DiagnosisPrompt, promptMgr *prompt.Manager) string {
	var buf bytes.Buffer

	if promptMgr != nil {
		buf.WriteString(promptMgr.Render("default"))
		buf.WriteString("\n\n")
	}

	buf.WriteString("You are a Kubernetes AIOps diagnosis engine. Analyze the provided alert and topology data to determine the root cause.\n\n")
	buf.WriteString("In addition to root cause analysis, you MUST analyze business impact. Review the BUSINESS IMPACT CONTEXT section to identify directly and indirectly affected business applications. DIRECT impacts are apps owning the alerted Kubernetes resource. INDIRECT impacts are downstream apps accessed via the CallsApp dependency chain (max 2 hops). Consider each app's criticality when assessing risk level. Output your ENTIRE response in Chinese (中文).\n\n")
	buf.WriteString("Your ENTIRE response must be a SINGLE JSON object containing ALL of these fields:\n")
	buf.WriteString(`{
  "root_cause": "string - the root cause analysis in Chinese",
  "confidence": 0.0-1.0,
  "evidence": ["array of strings in Chinese"],
  "remediation": {
    "action": "string",
    "description": "string",
    "steps": ["array of strings"],
    "risk_level": "low|medium|high",
    "auto_fixable": false
  },
  "businessImpact": {
    "directImpacts": [
      {"appName": "...", "namespace": "...", "team": "...", "criticality": "...", "businessUnit": "...", "impactType": "direct", "hopDistance": 0, "impactPath": "...", "reasoning": "..."}
    ],
    "indirectImpacts": [
      {"appName": "...", "namespace": "...", "team": "...", "criticality": "...", "businessUnit": "...", "impactType": "indirect", "hopDistance": 1, "impactPath": "appA -> appB", "reasoning": "..."}
    ],
    "summary": "整体影响评估摘要（中文）",
    "riskLevel": "critical|high|medium|low"
  }
}`)
	buf.WriteString("\n\nGuidelines:\n- businessImpact.directImpacts and indirectImpacts MUST be arrays, NOT strings\n- If no business impact data is available, use empty arrays []\n- ALL text values (root_cause, evidence, descriptions, summary) MUST be in Chinese\n\n")

	fmt.Fprintf(&buf, "ALERT: %+v\n\n", prompt.Alert)

	if prompt.TopologySnapshot != nil {
		fmt.Fprintf(&buf, "TOPOLOGY (%d nodes, %d edges):\n", len(prompt.TopologySnapshot.Nodes), len(prompt.TopologySnapshot.Edges))
		for _, n := range prompt.TopologySnapshot.Nodes {
			fmt.Fprintf(&buf, "  Node: %s/%s (uid: %s, deleted: %t)\n", n.Kind, n.Name, n.UID, n.IsDeleted)
		}
		for _, e := range prompt.TopologySnapshot.Edges {
			fmt.Fprintf(&buf, "  Edge: %s -[%s]-> %s\n", e.From, e.Type, e.To)
		}
		fmt.Fprintln(&buf)
	}

	if prompt.ImpactAssessment != nil {
		fmt.Fprintf(&buf, "IMPACT: severity=%s, blast_radius=%d\n",
			prompt.ImpactAssessment.Severity,
			prompt.ImpactAssessment.BlastRadius)
		if prompt.ImpactAssessment.BusinessContext != "" {
			fmt.Fprintf(&buf, "  Business context: %s\n", prompt.ImpactAssessment.BusinessContext)
		}
		fmt.Fprintln(&buf)
	}

	if len(prompt.KnowledgeMatches) > 0 {
		fmt.Fprintln(&buf, "KNOWLEDGE BASE MATCHES:")
		for _, m := range prompt.KnowledgeMatches {
			fmt.Fprintf(&buf, "  - %s: %s\n", m.Action, m.Description)
		}
		fmt.Fprintln(&buf)
	}

	if len(prompt.RelatedAlerts) > 0 {
		fmt.Fprintf(&buf, "RELATED ALERTS: %v\n", prompt.RelatedAlerts)
	}

	if prompt.BusinessImpactContext != nil {
		if biCtx, ok := prompt.BusinessImpactContext.(*BusinessImpactContext); ok && biCtx != nil {
			fmt.Fprintln(&buf, "BUSINESS IMPACT CONTEXT:")
			for _, app := range biCtx.DirectBusinessApps {
				fmt.Fprintf(&buf, "  Direct: app=%s team=%s criticality=%s\n", app.AppName, app.Team, app.Criticality)
			}
			if len(biCtx.DownstreamCallChain) > 0 {
				fmt.Fprintln(&buf, "  Call chains (who depends on this app):")
				for _, call := range biCtx.DownstreamCallChain {
					fmt.Fprintf(&buf, "    %s/%s -> %s/%s (hop=%d)\n",
						call.Caller, call.CallerNS, call.Callee, call.CalleeNS, call.HopDistance)
				}
			}
			fmt.Fprintln(&buf)
		}
	}
	if prompt.BusinessAppCalls != nil {
		fmt.Fprintln(&buf, "SERVICE CALL CHAINS:")
		for _, up := range prompt.BusinessAppCalls.Upstreams {
			fmt.Fprintf(&buf, "  Upstream: %s\n", up.AppName)
		}
		for _, down := range prompt.BusinessAppCalls.Downstreams {
			fmt.Fprintf(&buf, "  Downstream: %s\n", down.AppName)
		}
		fmt.Fprintln(&buf)
	}

	return buf.String()
}

// buildDiagnosisSystemPromptFromPrompt is an alias for buildDiagnosisSystemPrompt,
// used by the pipeline-based diagnosis path.
func buildDiagnosisSystemPromptFromPrompt(prompt interfaces.DiagnosisPrompt) string {
	return buildDiagnosisSystemPrompt(prompt, nil)
}

func buildPostmortemSystemPrompt(prompt interfaces.PostmortemInsightsPrompt) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`你是一个 Kubernetes 故障复盘分析专家。请根据提供的故障上下文，生成复盘报告的核心内容。返回严格的 JSON 格式。

JSON 格式：
{
  "lessons_learned": ["经验教训1", "经验教训2"],
  "what_went_well": ["做得好的方面1"],
  "what_went_wrong": ["可以改进的方面1"],
  "contributing_factors": ["促成因素1", "促成因素2"],
  "action_items": [{"description": "改进项", "owner": "负责人", "priority": "high/medium/low", "exit_criteria": "验收标准", "category": "prevent/detect/mitigate"}],
  "resolution": "解决措施总结"
}

故障信息：
故障标题: %s
严重级别: %s
持续时间: %s
根因: %s
时间线: %s
告警名称: %s
资源: %s/%s
命名空间: %s
`, prompt.IncidentTitle, prompt.Severity, prompt.Duration, prompt.RootCause,
		prompt.Timeline, prompt.AlertName, prompt.ResourceKind, prompt.ResourceName, prompt.Namespace))

	if prompt.CausalChain != "" {
		sb.WriteString(fmt.Sprintf("因果链: %s\n", prompt.CausalChain))
	}
	if prompt.ImpactSummary != "" {
		sb.WriteString(fmt.Sprintf("影响摘要: %s\n", prompt.ImpactSummary))
	}
	if prompt.DiagnosisSummary != "" {
		sb.WriteString(fmt.Sprintf("AI诊断摘要: %s\n", prompt.DiagnosisSummary))
	}
	if len(prompt.AffectedServices) > 0 {
		sb.WriteString(fmt.Sprintf("受影响服务: %s\n", strings.Join(prompt.AffectedServices, ", ")))
	}
	if prompt.MetricsSummary != "" {
		sb.WriteString(fmt.Sprintf("关键指标: %s\n", prompt.MetricsSummary))
	}
	if prompt.LogsHighlights != "" {
		sb.WriteString(fmt.Sprintf("关键日志: %s\n", prompt.LogsHighlights))
	}

	sb.WriteString(`
要求：
- lessons_learned: 至少2条，针对具体故障类型给出可落地的改进建议
- what_went_well: 故障处理过程中做得好的方面（如快速响应、有效协作等），至少1条
- what_went_wrong: 故障处理过程中可以改进的方面（如告警延迟、文档缺失等），至少2条
- contributing_factors: 除了根因外，哪些系统/流程/组织条件促成了故障发生，至少2条
- action_items: 至少2条，包含明确的负责人、优先级、验收标准和分类
  - category 取值为 prevent（预防）/ detect（检测）/ mitigate（缓解）
  - exit_criteria 描述该项完成的可验证标准
- resolution: 用1-2句话总结故障的解决措施或当前状态
- 所有内容使用中文`)

	return sb.String()
}

func parseLLMResponse(content string) (*interfaces.LLMDiagnosisResult, error) {
	cleaned := strings.TrimSpace(content)
	if strings.HasPrefix(cleaned, "```") {
		if start := strings.Index(cleaned, "\n"); start > -1 {
			cleaned = cleaned[start+1:]
		}
		cleaned = strings.TrimSpace(cleaned)
		cleaned = strings.TrimSuffix(cleaned, "```")
		cleaned = strings.TrimSpace(cleaned)
	}

	var result interfaces.LLMDiagnosisResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response JSON: %w", err)
	}
	return &result, nil
}

func parsePostmortemResponse(content string) (*interfaces.PostmortemInsightsResult, error) {
	cleaned := strings.TrimSpace(content)
	if strings.HasPrefix(cleaned, "```") {
		if start := strings.Index(cleaned, "\n"); start > -1 {
			cleaned = cleaned[start+1:]
		}
		cleaned = strings.TrimSpace(cleaned)
		cleaned = strings.TrimSuffix(cleaned, "```")
		cleaned = strings.TrimSpace(cleaned)
	}

	var result interfaces.PostmortemInsightsResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("failed to parse postmortem JSON: %w", err)
	}
	return &result, nil
}

func (p *EinoLLMProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	return nil, fmt.Errorf("embedding not supported via Eino provider; use HTTPLLMProvider")
}
