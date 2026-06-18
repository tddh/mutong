package inspection

import (
	"context"
	"time"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/inspection"
)

// YAMLRuleAdapter 将 YAML 声明的 Rule 包装为 InspectionRule 接口
type YAMLRuleAdapter struct {
	rule   Rule
	engine *YAMLEngine
}

// NewYAMLRuleAdapter 创建适配器（自动创建独立 YAMLEngine）
func NewYAMLRuleAdapter(rule Rule, logger interfaces.Logger, graphDB interfaces.GraphDB) *YAMLRuleAdapter {
	return &YAMLRuleAdapter{rule: rule, engine: NewYAMLEngine(logger, graphDB, nil)}
}

// NewYAMLRuleAdapterWithEngine 创建适配器并共享已有 YAMLEngine（推荐）
func NewYAMLRuleAdapterWithEngine(rule Rule, engine *YAMLEngine) *YAMLRuleAdapter {
	return &YAMLRuleAdapter{rule: rule, engine: engine}
}

func (a *YAMLRuleAdapter) Name() string        { return a.rule.Name }
func (a *YAMLRuleAdapter) Description() string { return a.rule.Description }
func (a *YAMLRuleAdapter) Severity() string    { return a.rule.Severity }

func (a *YAMLRuleAdapter) Execute(ctx context.Context, graphDB interfaces.GraphDB) ([]inspection.InspectionResult, error) {
	findings, err := a.engine.CheckRule(a.rule)
	if err != nil {
		return nil, err
	}
	var results []inspection.InspectionResult
	for _, f := range findings {
		results = append(results, inspection.InspectionResult{
			RuleName:   a.rule.Name,
			Severity:   f.Severity,
			Message:    f.Message,
			Resources:  []string{f.Resource},
			Suggestion: a.rule.Suggestion,
			Timestamp:  time.Now(),
		})
	}
	return results, nil
}
