package inspection

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/inspection"
)

type MonitoringBlindspotRule struct {
	logger interfaces.Logger
}

func NewMonitoringBlindspotRule(logger interfaces.Logger) interfaces.InspectionRule {
	return &MonitoringBlindspotRule{logger: logger}
}

func (r *MonitoringBlindspotRule) Name() string { return "monitoring_blindspot" }
func (r *MonitoringBlindspotRule) Description() string {
	return "检测未配置监控告警的关键资源"
}
func (r *MonitoringBlindspotRule) Severity() string { return "info" }

func (r *MonitoringBlindspotRule) Execute(ctx context.Context, graphDB interfaces.GraphDB) ([]inspection.InspectionResult, error) {
	nodeQuery := `MATCH (n:K8sResource{kind:'Node',is_deleted:false}) RETURN count(n) as nodeCount`
	resultSet, err := graphDB.ExecuteAndCheck(nodeQuery)
	if err != nil {
		return nil, fmt.Errorf("node count query failed: %w", err)
	}

	var nodeCount int64
	if resultSet.GetRowSize() > 0 {
		row, err := resultSet.GetRowValuesByIndex(0)
		if err != nil {
			return nil, fmt.Errorf("failed to read row: %w", err)
		}
		countVal, err := row.GetValueByColName("nodeCount")
		if err != nil {
			r.logger.Debug("Failed to get nodeCount column", zap.Error(err))
			return nil, nil
		}
		nodeCount, _ = countVal.AsInt()
	}

	if nodeCount == 0 {
		return []inspection.InspectionResult{{
			RuleName:   r.Name(),
			Severity:   "info",
			Message:    "集群未发现 Node 资源，无需检查监控覆盖",
			Suggestion: "确认集群节点已正确注册",
			Timestamp:  time.Now(),
		}}, nil
	}

	promoQuery := `MATCH (p:K8sResource{kind:'Deployment',is_deleted:false})
		WHERE toLower(p.K8sResource.name) CONTAINS 'prometheus'
		RETURN count(p) as count`

	var promoCount int64
	resultSet2, err2 := graphDB.ExecuteAndCheck(promoQuery)
	if err2 == nil && resultSet2.GetRowSize() > 0 {
		row, err := resultSet2.GetRowValuesByIndex(0)
		if err == nil {
			countVal, err := row.GetValueByColName("count")
			if err == nil {
				promoCount, _ = countVal.AsInt()
			}
		}
	}

	var results []inspection.InspectionResult
	if promoCount == 0 {
		results = append(results, inspection.InspectionResult{
			RuleName:   r.Name(),
			Severity:   "warning",
			Message:    fmt.Sprintf("集群共有 %d 个节点，但未检测到 Prometheus 部署", nodeCount),
			Resources:  []string{},
			Suggestion: "集群缺少 Prometheus 监控，建议部署 Prometheus 或类似监控系统",
			Timestamp:  time.Now(),
		})
	}

	results = append(results, inspection.InspectionResult{
		RuleName:   r.Name(),
		Severity:   "info",
		Message:    fmt.Sprintf("集群共有 %d 个节点，Prometheus 部署数: %d", nodeCount, promoCount),
		Resources:  []string{},
		Suggestion: "确认所有节点都配置了 Prometheus 监控和告警规则",
		Timestamp:  time.Now(),
	})

	return results, nil
}
