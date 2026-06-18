package inspection

import (
	"context"
	"time"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/inspection"
)

// YAMLRuleAdapter 将 YAML 声明的 Rule 包装为 InspectionRule 接口
// 用于将 configs/rules/inspection/*.yml 加载的规则注册到 InspectionEngine
type YAMLRuleAdapter struct {
	rule   Rule
	engine *YAMLEngine
}

// NewYAMLRuleAdapter 创建一个 YAML 规则适配器
func NewYAMLRuleAdapter(rule Rule, logger interfaces.Logger, graphDB interfaces.GraphDB) *YAMLRuleAdapter {
	engine := NewYAMLEngine(logger, graphDB, []Rule{rule})
	return &YAMLRuleAdapter{rule: rule, engine: engine}
}

func (a *YAMLRuleAdapter) Name() string        { return a.rule.Name }
func (a *YAMLRuleAdapter) Description() string { return a.rule.Description }
func (a *YAMLRuleAdapter) Severity() string    { return a.rule.Severity }

func (a *YAMLRuleAdapter) Execute(ctx context.Context, graphDB interfaces.GraphDB) ([]inspection.InspectionResult, error) {
	report := a.engine.Execute(ctx)
	var results []inspection.InspectionResult
	for _, ruleResult := range report.Rules {
		for _, finding := range ruleResult.Findings {
			results = append(results, inspection.InspectionResult{
				RuleName:   a.rule.Name,
				Severity:   finding.Severity,
				Message:    finding.Message,
				Resources:  []string{finding.Resource},
				Suggestion: a.rule.Suggestion,
				Timestamp:  time.Now(),
			})
		}
	}
	return results, nil
}
