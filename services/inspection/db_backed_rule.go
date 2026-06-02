package inspection

import (
	"context"
	"encoding/json"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/inspection"
)

type DBBackedRule struct {
	model      inspection.InspectionRuleModel
	graphDB    interfaces.GraphDB
	yamlEngine *YAMLEngine
}

func NewDBBackedRule(model inspection.InspectionRuleModel, graphDB interfaces.GraphDB, logger interfaces.Logger) *DBBackedRule {
	var checkCfg CheckConfig
	_ = json.Unmarshal([]byte(model.CheckConfig), &checkCfg)

	yamlEngine := NewYAMLEngine(logger, graphDB, []Rule{{
		Name:        model.Name,
		Description: model.Description,
		Query:       model.Query,
		Check:       checkCfg,
		Severity:    model.Severity,
		Suggestion:  model.Suggestion,
	}})

	return &DBBackedRule{
		model:      model,
		graphDB:    graphDB,
		yamlEngine: yamlEngine,
	}
}

func (r *DBBackedRule) Name() string        { return r.model.Name }
func (r *DBBackedRule) Description() string { return r.model.Description }
func (r *DBBackedRule) Severity() string    { return r.model.Severity }

func (r *DBBackedRule) Execute(ctx context.Context, graphDB interfaces.GraphDB) ([]inspection.InspectionResult, error) {
	report := r.yamlEngine.Execute(ctx)
	var results []inspection.InspectionResult
	for _, ruleResult := range report.Rules {
		for _, finding := range ruleResult.Findings {
			results = append(results, inspection.InspectionResult{
				RuleName:   r.model.Name,
				Severity:   finding.Severity,
				Message:    finding.Message,
				Resources:  []string{finding.Resource},
				Suggestion: r.model.Suggestion,
				Timestamp:  report.Timestamp,
			})
		}
	}
	return results, nil
}
