package inspection

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/inspection"
)

type CMDBDataSiloRule struct {
	logger interfaces.Logger
}

func NewCMDBDataSiloRule(logger interfaces.Logger) interfaces.InspectionRule {
	return &CMDBDataSiloRule{logger: logger}
}

func (r *CMDBDataSiloRule) Name() string        { return "cmdb_data_silo" }
func (r *CMDBDataSiloRule) Description() string { return "检测图谱中断开连接的孤立节点" }
func (r *CMDBDataSiloRule) Severity() string    { return "info" }

func (r *CMDBDataSiloRule) Execute(ctx context.Context, graphDB interfaces.GraphDB) ([]inspection.InspectionResult, error) {
	query := `
		MATCH (v:K8sResource{is_deleted:false})
		WHERE NOT (v)-[]-()
		RETURN v.K8sResource.kind as kind, v.K8sResource.name as name, v.K8sResource.name_space as namespace
		LIMIT 100
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
		kindVal, err := row.GetValueByColName("kind")
		if err != nil {
			r.logger.Debug("Failed to get kind", zap.Int("row", i), zap.Error(err))
			continue
		}
		nameVal, err := row.GetValueByColName("name")
		if err != nil {
			r.logger.Debug("Failed to get name", zap.Int("row", i), zap.Error(err))
			continue
		}
		nsVal, err := row.GetValueByColName("namespace")
		if err != nil {
			r.logger.Debug("Failed to get namespace", zap.Int("row", i), zap.Error(err))
			continue
		}

		ks, err := kindVal.AsString()
		if err != nil {
			r.logger.Debug("Failed to convert kind to string", zap.Int("row", i), zap.Error(err))
			continue
		}
		na, err := nameVal.AsString()
		if err != nil {
			r.logger.Debug("Failed to convert name to string", zap.Int("row", i), zap.Error(err))
			continue
		}
		ns, err := nsVal.AsString()
		if err != nil {
			r.logger.Debug("Failed to convert namespace to string", zap.Int("row", i), zap.Error(err))
			continue
		}

		results = append(results, inspection.InspectionResult{
			RuleName:   r.Name(),
			Severity:   r.Severity(),
			Message:    fmt.Sprintf("孤立资源: %s/%s (namespace: %s) 无拓扑连接", ks, na, ns),
			Resources:  []string{fmt.Sprintf("%s:%s", ks, na)},
			Suggestion: "检查资源采集逻辑，确保拓扑关系完整",
			Timestamp:  time.Now(),
		})
	}

	return results, nil
}
