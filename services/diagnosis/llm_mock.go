package diagnosis

import (
	"context"
	"fmt"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/diagnosis"
)

type MockLLMProvider struct {
	Result    *interfaces.LLMDiagnosisResult
	Error     error
	CallCount int
}

func NewMockLLMProvider(result *interfaces.LLMDiagnosisResult, err error) *MockLLMProvider {
	return &MockLLMProvider{Result: result, Error: err}
}

func (m *MockLLMProvider) Diagnose(ctx context.Context, prompt interfaces.DiagnosisPrompt) (*interfaces.LLMDiagnosisResult, error) {
	m.CallCount++
	return m.Result, m.Error
}

func (m *MockLLMProvider) GeneratePostmortemInsights(ctx context.Context, prompt interfaces.PostmortemInsightsPrompt) (*interfaces.PostmortemInsightsResult, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	return nil, fmt.Errorf("not implemented in mock")
}

func (m *MockLLMProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	return make([]float32, 1024), nil
}

func NewMockLLMProviderWithDefaultResult() *MockLLMProvider {
	m := &MockLLMProvider{
		Result: &interfaces.LLMDiagnosisResult{
			RootCause:  "Pod 内存不足导致 OOMKilled",
			Confidence: 0.85,
			Evidence: []string{
				"Pod 内存限制为 256Mi",
				"实际使用超过限制",
			},
		},
	}
	m.Result.SetRemediation(diagnosis.RemediationSuggestion{
		Action:      "增加内存限制",
		Description: "Pod 因内存不足被杀死，建议增加内存限制",
		Steps: []string{
			"检查当前内存使用：kubectl top pod",
			"增加 deployment 中的内存限制",
			"重新部署：kubectl rollout restart deployment/<name>",
		},
		RiskLevel:   "low",
		AutoFixable: true,
	})
	return m
}

func NewMockLLMProviderWithError(err error) *MockLLMProvider {
	return &MockLLMProvider{Error: err}
}

func NewMockLLMProviderWithBusinessImpact() *MockLLMProvider {
	m := &MockLLMProvider{
		Result: &interfaces.LLMDiagnosisResult{
			RootCause:  "Pod 内存不足导致 OOMKilled，影响了订单服务",
			Confidence: 0.85,
			Evidence:   []string{"Pod 内存限制为 256Mi", "实际使用超过限制"},
			BusinessImpact: &diagnosis.BusinessImpactAnalysis{
				DirectImpacts: []diagnosis.BusinessImpactEntry{
					{AppName: "order-service", Namespace: "production", Team: "交易平台组", Criticality: "critical", ImpactType: "direct", HopDistance: 0, ImpactPath: "order-service", Reasoning: "告警 Pod 直接归属于 order-service"},
				},
				IndirectImpacts: []diagnosis.BusinessImpactEntry{
					{AppName: "payment-service", Namespace: "production", Team: "支付组", Criticality: "high", ImpactType: "indirect", HopDistance: 1, ImpactPath: "order-service → payment-service", Reasoning: "payment-service 是 order-service 的直接下游调用方"},
				},
				Summary:   "订单服务故障直接影响下游支付服务，影响面涉及交易核心链路",
				RiskLevel: "critical",
			},
		},
	}
	m.Result.SetRemediation(diagnosis.RemediationSuggestion{
		Action:      "增加内存限制",
		Description: "Pod 因内存不足被杀死，建议增加内存限制",
		Steps:       []string{"检查当前内存使用", "增加 deployment 中的内存限制"},
		RiskLevel:   "low",
		AutoFixable: true,
	})
	return m
}

var _ interfaces.LLMProvider = (*MockLLMProvider)(nil)
