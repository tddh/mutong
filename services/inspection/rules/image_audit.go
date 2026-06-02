package inspection

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/inspection"
)

type ImageAuditRule struct {
	logger interfaces.Logger
}

func NewImageAuditRule(logger interfaces.Logger) interfaces.InspectionRule {
	return &ImageAuditRule{logger: logger}
}

func (r *ImageAuditRule) Name() string { return "image_audit" }
func (r *ImageAuditRule) Description() string {
	return "检测使用 latest 标签或过时镜像的 Pod"
}
func (r *ImageAuditRule) Severity() string { return "warning" }

func (r *ImageAuditRule) Execute(ctx context.Context, graphDB interfaces.GraphDB) ([]inspection.InspectionResult, error) {
	query := `
		MATCH (p:K8sResource{kind:'Pod',is_deleted:false})
		RETURN p.K8sResource.name as name, p.K8sResource.name_space as namespace,
		       p.K8sResource.resource_define as resourceDefine
		LIMIT 200
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
		defVal, err := row.GetValueByColName("resourceDefine")
		if err != nil {
			r.logger.Debug("Failed to get resourceDefine", zap.Int("row", i), zap.Error(err))
			continue
		}

		podName, err := nameVal.AsString()
		if err != nil {
			r.logger.Debug("Failed to convert name to string", zap.Int("row", i), zap.Error(err))
			continue
		}
		namespace, err := nsVal.AsString()
		if err != nil {
			r.logger.Debug("Failed to convert namespace to string", zap.Int("row", i), zap.Error(err))
			continue
		}
		defStr, err := defVal.AsString()
		if err != nil {
			r.logger.Debug("Failed to convert resourceDefine to string", zap.Int("row", i), zap.Error(err))
			continue
		}

		if strings.Contains(defStr, ":latest") {
			results = append(results, inspection.InspectionResult{
				RuleName:   r.Name(),
				Severity:   r.Severity(),
				Message:    fmt.Sprintf("Pod %s/%s 使用 latest 标签镜像", namespace, podName),
				Resources:  []string{fmt.Sprintf("Pod:%s/%s", namespace, podName)},
				Suggestion: "避免使用 latest 标签，使用固定版本标签确保可重现性",
				Timestamp:  time.Now(),
			})
		}
	}

	return results, nil
}
