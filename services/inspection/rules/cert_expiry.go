package inspection

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/inspection"
)

type CertExpiryRule struct {
	logger interfaces.Logger
}

func NewCertExpiryRule(logger interfaces.Logger) interfaces.InspectionRule {
	return &CertExpiryRule{logger: logger}
}

func (r *CertExpiryRule) Name() string        { return "cert_expiry" }
func (r *CertExpiryRule) Description() string { return "检测即将过期的 TLS 证书" }
func (r *CertExpiryRule) Severity() string    { return "warning" }

func (r *CertExpiryRule) Execute(ctx context.Context, graphDB interfaces.GraphDB) ([]inspection.InspectionResult, error) {
	query := `
		MATCH (s:K8sResource{kind:'Secret',is_deleted:false})
		WHERE s.K8sResource.resource_define CONTAINS 'kubernetes.io/tls'
		RETURN s.K8sResource.name as name, s.K8sResource.name_space as namespace,
		       s.K8sResource.resource_define as resourceDefine
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

		secretName, err := nameVal.AsString()
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

		var resource map[string]interface{}
		unquoted, unqErr := strconv.Unquote(defStr)
		if unqErr != nil {
			unquoted = defStr
		}
		if err := json.Unmarshal([]byte(unquoted), &resource); err != nil {
			r.logger.Debug("Failed to parse resource JSON",
				zap.String("name", secretName), zap.Error(err))
			continue
		}

		metadata, _ := resource["metadata"].(map[string]interface{})
		creationTimestamp, _ := metadata["creationTimestamp"].(string)

		if creationTimestamp != "" {
			created, err := time.Parse(time.RFC3339, creationTimestamp)
			if err == nil {
				certExpiry := created.AddDate(0, 0, 365)
				daysLeft := int(time.Until(certExpiry).Hours() / 24)
				if daysLeft > 30 {
					continue
				}
				if daysLeft < 0 {
					results = append(results, inspection.InspectionResult{
						RuleName:   r.Name(),
						Severity:   "critical",
						Message:    fmt.Sprintf("TLS Secret %s/%s 证书可能已过期", namespace, secretName),
						Resources:  []string{fmt.Sprintf("Secret:%s/%s", namespace, secretName)},
						Suggestion: "证书已过期或即将过期，请立即更新证书",
						Timestamp:  time.Now(),
					})
				} else {
					results = append(results, inspection.InspectionResult{
						RuleName:   r.Name(),
						Severity:   r.Severity(),
						Message:    fmt.Sprintf("TLS Secret %s/%s 证书可能在 %d 天后过期", namespace, secretName, daysLeft),
						Resources:  []string{fmt.Sprintf("Secret:%s/%s", namespace, secretName)},
						Suggestion: fmt.Sprintf("证书可能在 %d 天后过期，建议尽快更新", daysLeft),
						Timestamp:  time.Now(),
					})
				}
				continue
			}
		}

		results = append(results, inspection.InspectionResult{
			RuleName:   r.Name(),
			Severity:   "info",
			Message:    fmt.Sprintf("TLS Secret %s/%s 无法确定证书有效期，请手动检查", namespace, secretName),
			Resources:  []string{fmt.Sprintf("Secret:%s/%s", namespace, secretName)},
			Suggestion: "无法自动解析证书，请手动检查有效期",
			Timestamp:  time.Now(),
		})
	}

	return results, nil
}
