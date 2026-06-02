package diagnosis

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"gitee.com/tddh/mutong/interfaces"
)

type HTTPLLMProvider struct {
	provider         string
	model            string
	embeddingModel   string
	embeddingAPIKey  string
	embeddingBaseURL string
	apiKey           string
	endpoint         string
	baseURL          string
	timeout          time.Duration
	client           *http.Client
	maxTokens        int
	contextWindow    int
}

const llmSystemPromptBusinessImpact = " In addition to root cause analysis, you MUST analyze business impact. Review the BUSINESS IMPACT CONTEXT section to identify directly and indirectly affected business applications. DIRECT impacts are apps owning the alerted Kubernetes resource. INDIRECT impacts are downstream apps accessed via the CallsApp dependency chain (max 2 hops). Consider each app's criticality when assessing risk level. Output your ENTIRE response in Chinese (中文), including root_cause, evidence, remediation, reasoning, and summary fields. ALL text values must be in Chinese."

func NewHTTPLLMProvider(provider, model, apiKey, endpoint string, timeoutSec int) *HTTPLLMProvider {
	return newHTTPLLMProvider(provider, model, apiKey, endpoint, "", timeoutSec, 0, 0)
}

func NewHTTPLLMProviderWithBaseURL(provider, model, apiKey, endpoint, baseURL string, timeoutSec int) *HTTPLLMProvider {
	return newHTTPLLMProvider(provider, model, apiKey, endpoint, baseURL, timeoutSec, 0, 0)
}

func NewHTTPLLMProviderWithTokens(provider, model, apiKey, endpoint, baseURL string, timeoutSec, maxTokens, contextWindow int) *HTTPLLMProvider {
	return newHTTPLLMProvider(provider, model, apiKey, endpoint, baseURL, timeoutSec, maxTokens, contextWindow)
}

func newHTTPLLMProvider(provider, model, apiKey, endpoint, baseURL string, timeoutSec, maxTokens, contextWindow int) *HTTPLLMProvider {
	apiKey = resolveEnvVar(apiKey)

	mt := maxTokens
	if mt <= 0 {
		mt = 16384
	}

	return &HTTPLLMProvider{
		provider:       provider,
		model:          model,
		embeddingModel: "text-embedding-3-small",
		apiKey:         apiKey,
		endpoint:       endpoint,
		baseURL:        baseURL,
		timeout:        time.Duration(timeoutSec) * time.Second,
		client: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		},
		maxTokens:     mt,
		contextWindow: contextWindow,
	}
}

func (p *HTTPLLMProvider) WithEmbeddingConfig(model, apiKey, baseURL string) *HTTPLLMProvider {
	if model != "" {
		p.embeddingModel = model
	}
	if apiKey != "" {
		p.embeddingAPIKey = apiKey
	}
	if baseURL != "" {
		p.embeddingBaseURL = baseURL
	}
	return p
}

func (p *HTTPLLMProvider) resolveEndpoint(defaultURL string) string {
	if p.endpoint != "" {
		return p.endpoint
	}
	if p.baseURL != "" {
		base := strings.TrimRight(p.baseURL, "/")
		if strings.HasPrefix(defaultURL, "http") {
			if u, err := url.Parse(defaultURL); err == nil {
				return base + u.Path
			}
		}
		return base + defaultURL
	}
	return defaultURL
}

func resolveEnvVar(apiKey string) string {
	if apiKey == "" {
		return ""
	}

	var varName string
	switch {
	case len(apiKey) > 2 && apiKey[0] == '$' && apiKey[1] == '{' && apiKey[len(apiKey)-1] == '}':
		varName = apiKey[2 : len(apiKey)-1]
	case apiKey[0] == '$':
		varName = apiKey[1:]
	default:
		return apiKey
	}

	val := os.Getenv(varName)
	if val == "" {
		return apiKey
	}
	return val
}

func (p *HTTPLLMProvider) Diagnose(ctx context.Context, prompt interfaces.DiagnosisPrompt) (*interfaces.LLMDiagnosisResult, error) {
	switch p.provider {
	case "anthropic":
		return p.diagnoseAnthropic(ctx, prompt)
	case "openai":
		return p.diagnoseOpenAI(ctx, prompt)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", p.provider)
	}
}

func (p *HTTPLLMProvider) DiagnoseStream(ctx context.Context, prompt interfaces.DiagnosisPrompt, history []ChatMessage, callback func(token string) error) error {
	switch p.provider {
	case "anthropic":
		return p.diagnoseAnthropicStream(ctx, prompt, history, callback)
	case "openai":
		return p.diagnoseOpenAIStream(ctx, prompt, history, callback)
	default:
		return fmt.Errorf("unsupported LLM provider: %s", p.provider)
	}
}

func (p *HTTPLLMProvider) diagnoseAnthropic(ctx context.Context, prompt interfaces.DiagnosisPrompt) (*interfaces.LLMDiagnosisResult, error) {
	systemPrompt := "You are a Kubernetes AIOps diagnosis engine." + llmSystemPromptBusinessImpact + " Analyze the provided alert and topology data to determine the root cause. Return a JSON object with: root_cause (string), confidence (0.0-1.0), evidence (array of strings), remediation (object with action, description, steps, risk_level, auto_fixable), and optionally businessImpact (object with directImpacts, indirectImpacts, summary, riskLevel)."

	messages := []map[string]string{
		{"role": "user", "content": p.buildPromptText(prompt)},
	}

	body := map[string]interface{}{
		"model":      p.model,
		"max_tokens": p.maxTokens,
		"system":     systemPrompt,
		"messages":   messages,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := p.resolveEndpoint("https://api.anthropic.com/v1/messages")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("empty response from LLM")
	}

	return p.parseJSONResponse(apiResp.Content[0].Text)
}

func (p *HTTPLLMProvider) diagnoseOpenAI(ctx context.Context, prompt interfaces.DiagnosisPrompt) (*interfaces.LLMDiagnosisResult, error) {
	messages := []map[string]string{
		{"role": "system", "content": "You are a Kubernetes AIOps diagnosis engine. Return ONLY valid JSON (no markdown, no extra text). The JSON must contain: root_cause, confidence, evidence, remediation." + llmSystemPromptBusinessImpact},
		{"role": "user", "content": p.buildPromptText(prompt)},
	}

	body := map[string]interface{}{
		"model":       p.model,
		"max_tokens":  p.maxTokens,
		"temperature": 0.1,
		"messages":    messages,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := p.resolveEndpoint("https://api.openai.com/v1/chat/completions")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from LLM")
	}

	return p.parseJSONResponse(apiResp.Choices[0].Message.Content)
}

func (p *HTTPLLMProvider) parseJSONResponse(text string) (*interfaces.LLMDiagnosisResult, error) {
	cleaned := strings.TrimSpace(text)
	if strings.HasPrefix(cleaned, "```") {
		if start := strings.Index(cleaned, "\n"); start > -1 {
			cleaned = cleaned[start+1:]
		}
		if cleaned = strings.TrimSpace(cleaned); strings.HasSuffix(cleaned, "```") {
			cleaned = cleaned[:len(cleaned)-3]
		}
		cleaned = strings.TrimSpace(cleaned)
	}

	// 尝试直接解析
	var result interfaces.LLMDiagnosisResult
	if err := json.Unmarshal([]byte(cleaned), &result); err == nil {
		// 兼容 LLM 返回 rootCause (驼峰) 而非 root_cause (下划线)
		if result.RootCause == "" && result.RootCauseAlt != "" {
			result.RootCause = result.RootCauseAlt
		}
		if result.RootCause == "" {
			return nil, fmt.Errorf("LLM returned empty root cause, raw: %s", text[:min(300, len(text))])
		}
		if result.Confidence < 0 || result.Confidence > 1 {
			result.Confidence = 0.5
		}
		return &result, nil
	}

	// 如果直接解析失败，尝试从文本中提取 JSON
	startIdx := strings.Index(cleaned, "{")
	endIdx := strings.LastIndex(cleaned, "}")
	if startIdx != -1 && endIdx > startIdx {
		jsonStr := cleaned[startIdx : endIdx+1]
		if err := json.Unmarshal([]byte(jsonStr), &result); err == nil {
			if result.RootCause == "" && result.RootCauseAlt != "" {
				result.RootCause = result.RootCauseAlt
			}
			if result.RootCause == "" {
				return nil, fmt.Errorf("LLM returned empty root cause, raw: %s", text[:min(300, len(text))])
			}
			if result.Confidence < 0 || result.Confidence > 1 {
				result.Confidence = 0.5
			}
			return &result, nil
		}
	}

	// 所有尝试都失败
	preview := text
	if len(preview) > 500 {
		preview = preview[:500]
	}
	return nil, fmt.Errorf("failed to parse LLM JSON, raw: %s", preview)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (p *HTTPLLMProvider) buildPromptText(prompt interfaces.DiagnosisPrompt) string {
	var buf bytes.Buffer

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
		fmt.Fprintf(&buf, "IMPACT: severity=%s, blast_radius=%d, user_facing=%t\n",
			prompt.ImpactAssessment.Severity,
			prompt.ImpactAssessment.BlastRadius,
			prompt.ImpactAssessment.UserFacingImpact)
		if len(prompt.ImpactAssessment.AffectedServices) > 0 {
			fmt.Fprintf(&buf, "  Affected services: %v\n", prompt.ImpactAssessment.AffectedServices)
		}
		if prompt.ImpactAssessment.BusinessContext != "" {
			fmt.Fprintf(&buf, "  Business context: %s\n", prompt.ImpactAssessment.BusinessContext)
		}
		if prompt.ImpactAssessment.BusinessImpactNote != "" {
			fmt.Fprintf(&buf, "  Business impact: %s\n", prompt.ImpactAssessment.BusinessImpactNote)
		}
		if len(prompt.ImpactAssessment.AffectedAppNames) > 0 {
			fmt.Fprintf(&buf, "  Affected apps: %v\n", prompt.ImpactAssessment.AffectedAppNames)
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

	if prompt.BusinessAppCalls != nil {
		c := prompt.BusinessAppCalls
		fmt.Fprintf(&buf, "\nBUSINESS DEPENDENCIES: app=%s (namespace=%s)\n", c.AppName, c.Namespace)
		if len(c.Upstreams) > 0 {
			fmt.Fprintf(&buf, "  Upstream callers (%d):", len(c.Upstreams))
			for _, u := range c.Upstreams {
				fmt.Fprintf(&buf, " %s(team=%s,crit=%s)", u.AppName, u.Team, u.Criticality)
			}
			fmt.Fprintln(&buf)
		}
		if len(c.Downstreams) > 0 {
			fmt.Fprintf(&buf, "  Downstream deps (%d):", len(c.Downstreams))
			for _, d := range c.Downstreams {
				fmt.Fprintf(&buf, " %s(team=%s,crit=%s)", d.AppName, d.Team, d.Criticality)
			}
			fmt.Fprintln(&buf)
		}
	}

	// 业务影响上下文及分析（注入更完整的拓扑数据 + 调用关系）
	if prompt.BusinessImpactContext != nil {
		if bizCtx, ok := prompt.BusinessImpactContext.(*BusinessImpactContext); ok && bizCtx != nil {
			fmt.Fprintln(&buf, "\n## BUSINESS IMPACT CONTEXT")

			fmt.Fprintf(&buf, "The alerted resource belongs to the following business application(s):")
			for _, app := range bizCtx.DirectBusinessApps {
				fmt.Fprintf(&buf, "  - %s (namespace: %s, criticality: %s, team: %s)",
					app.AppName, app.Namespace, app.Criticality, app.Team)
				if app.BusinessUnit != "" {
					fmt.Fprintf(&buf, ", business_unit: %s", app.BusinessUnit)
				}
				fmt.Fprintln(&buf)
			}
			fmt.Fprintln(&buf)

			// 注入业务上下游调用链信息
			if prompt.BusinessAppCalls != nil {
				c := prompt.BusinessAppCalls
				fmt.Fprintln(&buf, "Business Dependencies (Call Chain):")

				if len(c.Upstreams) > 0 {
					fmt.Fprintf(&buf, "  Upstream callers (calling %s):\n", c.AppName)
					for _, u := range c.Upstreams {
						fmt.Fprintf(&buf, "    - %s (team: %s, criticality: %s)\n", u.AppName, u.Team, u.Criticality)
					}
				}
				if len(c.Downstreams) > 0 {
					fmt.Fprintf(&buf, "  Downstream dependencies (called by %s):\n", c.AppName)
					for _, d := range c.Downstreams {
						fmt.Fprintf(&buf, "    - %s (team: %s, criticality: %s)\n", d.AppName, d.Team, d.Criticality)
					}
				}
				if len(c.Upstreams) == 0 && len(c.Downstreams) == 0 {
					fmt.Fprintln(&buf, "  No direct business dependencies found.")
				}
				fmt.Fprintln(&buf)
			}

			if len(bizCtx.DownstreamCallChain) > 0 {
				fmt.Fprintln(&buf, "Downstream call chain (2 hops max):")
				for _, call := range bizCtx.DownstreamCallChain {
					fmt.Fprintf(&buf, "  %s -> %s (hop: %d)\n", call.Caller, call.Callee, call.HopDistance)
				}
			}

			// 要求 LLM 输出完整的结构化影响分析（作为整个诊断 JSON 的一部分）
			fmt.Fprintln(&buf, "\n**IMPORTANT: Your ENTIRE response must be a SINGLE JSON object containing ALL of these fields:**")
			fmt.Fprintln(&buf, `{
  "root_cause": "string - the root cause analysis",
  "confidence": 0.0-1.0,
  "evidence": ["array of strings"],
  "remediation": ["array of strings"],
  "businessImpact": {
    "directImpacts": [
      {"appName": "...", "namespace": "...", "team": "...", "criticality": "...", "businessUnit": "...", "impactType": "direct", "hopDistance": 0, "impactPath": "...", "reasoning": "..." }
    ],
    "indirectImpacts": [
      {"appName": "...", "namespace": "...", "team": "...", "criticality": "...", "businessUnit": "...", "impactType": "indirect", "hopDistance": 1, "impactPath": "appA -> appB", "reasoning": "..." }
    ],
    "summary": "整体影响评估摘要（中文）",
    "riskLevel": "critical|high|medium|low"
  }
}`)
			fmt.Fprintln(&buf, "\nGuidelines:")
			fmt.Fprintln(&buf, "1. DIRECT impacts: Apps that OWN the alerted resource, AND apps that have DIRECT business dependencies (upstream callers and downstream deps).")
			fmt.Fprintln(&buf, "2. INDIRECT impacts: Other downstream apps reached via further hops in the call chain (2 hops max). If there are downstream apps in the call chain, you MUST include them in indirectImpacts.")
			fmt.Fprintln(&buf, "3. Include team and businessUnit in every impacted item if available.")
			fmt.Fprintln(&buf, "4. riskLevel: assess overall risk based on criticality and number of affected apps.")
			fmt.Fprintln(&buf, "5. Output ONLY valid JSON, no markdown code fences around the JSON")
			fmt.Fprintln(&buf, "6. DO NOT output only the businessImpact part - output the COMPLETE JSON with root_cause, confidence, evidence, remediation, AND businessImpact")
			fmt.Fprintln(&buf, "7. ALL text fields (root_cause, evidence, reasoning, summary) MUST be in Chinese (中文)")
		}
	}

	return buf.String()
}

func (p *HTTPLLMProvider) diagnoseOpenAIStream(ctx context.Context, prompt interfaces.DiagnosisPrompt, history []ChatMessage, callback func(token string) error) error {
	userQuery := GetFirstUserMessage(history)
	if userQuery == "" {
		userQuery = p.buildPromptText(prompt)
	}
	messages := []map[string]string{
		{"role": "system", "content": appendToolExamplesToString("You are a Kubernetes AIOps diagnosis assistant."+llmSystemPromptBusinessImpact+" Provide diagnostic guidance in Chinese.", userQuery)},
		{"role": "user", "content": p.buildPromptText(prompt)},
	}

	for _, msg := range history {
		messages = append(messages, map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	body := map[string]interface{}{
		"model":       p.model,
		"max_tokens":  p.maxTokens,
		"temperature": 0.1,
		"messages":    messages,
		"stream":      true,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := p.resolveEndpoint("https://api.openai.com/v1/chat/completions")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return p.parseOpenAISSE(resp.Body, callback)
}

func (p *HTTPLLMProvider) parseOpenAISSE(body io.Reader, callback func(token string) error) error {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return nil
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			if err := callback(chunk.Choices[0].Delta.Content); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func (p *HTTPLLMProvider) diagnoseAnthropicStream(ctx context.Context, prompt interfaces.DiagnosisPrompt, history []ChatMessage, callback func(token string) error) error {
	userQuery := GetFirstUserMessage(history)
	if userQuery == "" {
		userQuery = p.buildPromptText(prompt)
	}
	systemPrompt := appendToolExamplesToString("You are a Kubernetes AIOps diagnosis assistant."+llmSystemPromptBusinessImpact+" Provide diagnostic guidance in Chinese.", userQuery)

	messages := []map[string]string{
		{"role": "user", "content": p.buildPromptText(prompt)},
	}

	for _, msg := range history {
		messages = append(messages, map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	body := map[string]interface{}{
		"model":      p.model,
		"max_tokens": p.maxTokens,
		"system":     systemPrompt,
		"messages":   messages,
		"stream":     true,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := p.resolveEndpoint("https://api.anthropic.com/v1/messages")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return p.parseAnthropicSSE(resp.Body, callback)
}

func (p *HTTPLLMProvider) parseAnthropicSSE(body io.Reader, callback func(token string) error) error {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: content_block_delta") {
			scanner.Scan()
			dataLine := scanner.Text()
			if !strings.HasPrefix(dataLine, "data: ") {
				continue
			}

			data := strings.TrimPrefix(dataLine, "data: ")
			var chunk struct {
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			if chunk.Delta.Text != "" {
				if err := callback(chunk.Delta.Text); err != nil {
					return err
				}
			}
		}
	}
	return scanner.Err()
}

// ChatResponse 聊天响应（含工具调用）
type ChatResponse struct {
	FinishReason string
	Content      string
	ToolCalls    []ToolCall
}

// DiagnoseChatWithTools 支持 OpenAI function calling 的聊天诊断方法
func (p *HTTPLLMProvider) DiagnoseChatWithTools(ctx context.Context, messages []ChatMessage, tools []map[string]interface{}) (*ChatResponse, error) {
	openaiMessages := []map[string]interface{}{}
	for _, msg := range messages {
		m := map[string]interface{}{"role": msg.Role}
		if msg.Content != "" {
			m["content"] = msg.Content
		}
		if len(msg.ToolCalls) > 0 {
			toolCalls := []map[string]interface{}{}
			for _, tc := range msg.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Arguments)
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      tc.Name,
						"arguments": string(argsJSON),
					},
				})
			}
			m["tool_calls"] = toolCalls
		}
		if msg.ToolCallID != "" {
			m["tool_call_id"] = msg.ToolCallID
		}
		openaiMessages = append(openaiMessages, m)
	}

	body := map[string]interface{}{
		"model":       p.model,
		"max_tokens":  p.maxTokens,
		"temperature": 0.1,
		"messages":    openaiMessages,
		"tools":       tools,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := p.resolveEndpoint("https://api.openai.com/v1/chat/completions")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from LLM")
	}

	choice := apiResp.Choices[0]
	result := &ChatResponse{
		FinishReason: choice.FinishReason,
		Content:      choice.Message.Content,
	}
	for _, tc := range choice.Message.ToolCalls {
		var args map[string]string
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}
	return result, nil
}

// StreamCallback 流式输出回调
type StreamCallback struct {
	OnToken    func(token string)
	OnToolCall func(name string, args map[string]string)
}

// toolCallAccumulator 用于累积流式 tool_call 片段
type toolCallAccumulator struct {
	ID        string
	Name      string
	Args      string
	Started   bool
	Completed bool
}

// DiagnoseChatWithToolsStream 支持流式和工具调用的聊天诊断方法
func (p *HTTPLLMProvider) DiagnoseChatWithToolsStream(
	ctx context.Context,
	messages []ChatMessage,
	tools []map[string]interface{},
	cb StreamCallback,
) (*ChatResponse, error) {
	openaiMessages := []map[string]interface{}{}
	for _, msg := range messages {
		m := map[string]interface{}{"role": msg.Role}
		if msg.Content != "" {
			m["content"] = msg.Content
		}
		if len(msg.ToolCalls) > 0 {
			toolCalls := []map[string]interface{}{}
			for _, tc := range msg.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Arguments)
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      tc.Name,
						"arguments": string(argsJSON),
					},
				})
			}
			m["tool_calls"] = toolCalls
		}
		if msg.ToolCallID != "" {
			m["tool_call_id"] = msg.ToolCallID
		}
		openaiMessages = append(openaiMessages, m)
	}

	body := map[string]interface{}{
		"model":       p.model,
		"max_tokens":  p.maxTokens,
		"temperature": 0.1,
		"messages":    openaiMessages,
		"tools":       tools,
		"stream":      true,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := p.resolveEndpoint("https://api.openai.com/v1/chat/completions")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return p.parseOpenAISSEWithTools(resp.Body, cb)
}

// parseOpenAISSEWithTools 解析带工具调用的 OpenAI SSE 流
func (p *HTTPLLMProvider) parseOpenAISSEWithTools(body io.Reader, cb StreamCallback) (*ChatResponse, error) {
	scanner := bufio.NewScanner(body)
	var content string
	var toolCalls []ToolCall
	toolAccums := make(map[int]*toolCallAccumulator)
	var finishReason string

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		finishReason = chunk.Choices[0].FinishReason

		// 处理文本内容
		if delta.Content != "" && cb.OnToken != nil {
			content += delta.Content
			cb.OnToken(delta.Content)
		}

		// Detect pending tool_call completion: when a new index appears, all lower indices are done
		if len(delta.ToolCalls) > 0 {
			newIdx := delta.ToolCalls[0].Index
			for i, acc := range toolAccums {
				if i < newIdx && !acc.Completed {
					acc.Completed = true
					var args map[string]string
					_ = json.Unmarshal([]byte(acc.Args), &args)
					toolCalls = append(toolCalls, ToolCall{
						ID:        acc.ID,
						Name:      acc.Name,
						Arguments: args,
					})
					if cb.OnToolCall != nil && acc.Started {
						cb.OnToolCall(acc.Name, args)
					}
				}
			}
		}

		// 处理 tool_call delta
		for _, tcDelta := range delta.ToolCalls {
			idx := tcDelta.Index
			acc, exists := toolAccums[idx]
			if !exists {
				acc = &toolCallAccumulator{}
				toolAccums[idx] = acc
			}

			if tcDelta.ID != "" {
				acc.ID = tcDelta.ID
			}
			if tcDelta.Function.Name != "" {
				acc.Name = tcDelta.Function.Name
				acc.Started = true
				if cb.OnToolCall != nil {
					cb.OnToolCall(acc.Name, nil)
				}
			}
			if tcDelta.Function.Arguments != "" {
				acc.Args += tcDelta.Function.Arguments
			}
		}

		// finish_reason 为 "tool_calls" 时，处理最后一个 tool_call
		if finishReason == "tool_calls" {
			for _, acc := range toolAccums {
				if acc.Completed {
					continue
				}
				acc.Completed = true
				var args map[string]string
				_ = json.Unmarshal([]byte(acc.Args), &args)
				toolCalls = append(toolCalls, ToolCall{
					ID:        acc.ID,
					Name:      acc.Name,
					Arguments: args,
				})
				if cb.OnToolCall != nil && acc.Started {
					cb.OnToolCall(acc.Name, args)
				}
			}
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("SSE stream error: %w", err)
	}

	return &ChatResponse{
		FinishReason: finishReason,
		Content:      content,
		ToolCalls:    toolCalls,
	}, nil
}

func (p *HTTPLLMProvider) GeneratePostmortemInsights(ctx context.Context, prompt interfaces.PostmortemInsightsPrompt) (*interfaces.PostmortemInsightsResult, error) {
	messages := []map[string]string{
		{"role": "system", "content": `你是一个 Kubernetes 故障复盘分析专家。请根据提供的故障上下文，生成复盘报告的核心内容。返回严格的 JSON 格式（不要 markdown，不要额外文字）。

JSON 格式：
{
  "lessons_learned": ["经验教训1", "经验教训2"],
  "action_items": [{"description": "改进项", "owner": "负责人", "priority": "high/medium/low"}],
  "resolution": "解决措施总结"
}

要求：
- lessons_learned: 至少2条，针对具体故障类型给出可落地的改进建议
- action_items: 至少2条，包含明确的负责人和优先级
- resolution: 用1-2句话总结故障的解决措施或当前状态
- 所有内容使用中文`},
		{"role": "user", "content": fmt.Sprintf(
			"故障标题: %s\n严重级别: %s\n持续时间: %s\n根因: %s\n\n时间线:\n%s\n\n告警名称: %s\n资源: %s/%s\n命名空间: %s",
			prompt.IncidentTitle, prompt.Severity, prompt.Duration, prompt.RootCause,
			prompt.Timeline, prompt.AlertName, prompt.ResourceKind, prompt.ResourceName, prompt.Namespace,
		)},
	}

	body := map[string]interface{}{
		"model":       p.model,
		"max_tokens":  1024,
		"temperature": 0.3,
		"messages":    messages,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := p.resolveEndpoint("https://api.openai.com/v1/chat/completions")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from LLM")
	}

	return p.parsePostmortemJSON(apiResp.Choices[0].Message.Content)
}

func (p *HTTPLLMProvider) parsePostmortemJSON(text string) (*interfaces.PostmortemInsightsResult, error) {
	cleaned := strings.TrimSpace(text)
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
		return nil, fmt.Errorf("failed to parse JSON: %w, raw: %s", err, text[:min(300, len(text))])
	}
	return &result, nil
}

func (p *HTTPLLMProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	type embeddingRequest struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}
	type embeddingResponse struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}

	model := p.embeddingModel
	if model == "" {
		model = "text-embedding-3-small"
	}
	reqBody := embeddingRequest{Model: model, Input: text}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal embedding request: %w", err)
	}

	apiKey := p.embeddingAPIKey
	if apiKey == "" {
		apiKey = p.apiKey
	}
	baseURL := p.embeddingBaseURL
	if baseURL == "" {
		baseURL = p.baseURL
	}

	var endpoint string
	if baseURL != "" {
		endpoint = strings.TrimRight(baseURL, "/") + "/embeddings"
	} else {
		endpoint = p.resolveEndpoint("https://api.openai.com/v1/embeddings")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding API error %d: %s", resp.StatusCode, string(respBody))
	}

	var embResp embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, fmt.Errorf("failed to decode embedding response: %w", err)
	}

	if len(embResp.Data) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}

	return embResp.Data[0].Embedding, nil
}
