package inspection

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/inspection"
)

type SinglePointFailureRule struct {
	logger interfaces.Logger
}

func NewSinglePointFailureRule(logger interfaces.Logger) interfaces.InspectionRule {
	return &SinglePointFailureRule{logger: logger}
}

func (r *SinglePointFailureRule) Name() string { return "single_point_failure" }
func (r *SinglePointFailureRule) Description() string {
	return "检测单节点 Pod 密度过高，节点故障影响范围过大"
}
func (r *SinglePointFailureRule) Severity() string { return "warning" }

func (r *SinglePointFailureRule) Execute(ctx context.Context, graphDB interfaces.GraphDB) ([]inspection.InspectionResult, error) {
	query := `
		MATCH (n:K8sResource{kind:'Node',is_deleted:false})
		MATCH (p:K8sResource{kind:'Pod',is_deleted:false})-[:RunsOn]->(n)
		WITH n, collect(p) as pods
		WHERE size(pods) > 30
		RETURN n.K8sResource.name as nodeName, size(pods) as podCount
	`

	resultSet, err := graphDB.ExecuteAndCheck(query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	var results []inspection.InspectionResult
	for i := 0; i < resultSet.GetRowSize(); i++ {
		row, err := resultSet.GetRowValuesByIndex(i)
		if err != nil {
			r.logger.Debug("Failed to get row value", zap.Int("row", i), zap.Error(err))
			continue
		}
		nodeNameVal, err := row.GetValueByColName("nodeName")
		if err != nil {
			r.logger.Debug("Failed to get nodeName", zap.Int("row", i), zap.Error(err))
			continue
		}
		podCountVal, err := row.GetValueByColName("podCount")
		if err != nil {
			r.logger.Debug("Failed to get podCount", zap.Int("row", i), zap.Error(err))
			continue
		}

		ns, err := nodeNameVal.AsString()
		if err != nil {
			r.logger.Debug("Failed to convert nodeName to string", zap.Int("row", i), zap.Error(err))
			continue
		}
		pc, err := podCountVal.AsInt()
		if err != nil {
			r.logger.Debug("Failed to convert podCount to int", zap.Int("row", i), zap.Error(err))
			continue
		}

		results = append(results, inspection.InspectionResult{
			RuleName:   r.Name(),
			Severity:   r.Severity(),
			Message:    fmt.Sprintf("节点 %s 运行 %d 个 Pod，存在单点故障风险", ns, pc),
			Resources:  []string{fmt.Sprintf("Node:%s", ns)},
			Suggestion: "建议分散 Pod 到其他节点，或添加反亲和性规则",
			Timestamp:  time.Now(),
		})
	}

	return results, nil
}
