package diagnosis

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"gitee.com/tddh/mutong/models/diagnosis"
)

type VectorRetriever struct {
	db *gorm.DB
}

func NewVectorRetriever(db *gorm.DB) *VectorRetriever {
	return &VectorRetriever{db: db}
}

func formatVector(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%f", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func (r *VectorRetriever) SearchSimilarCases(ctx context.Context, embedding []float32, limit int, threshold float64) ([]diagnosis.FaultReportVector, error) {
	if limit <= 0 {
		limit = 5
	}
	if threshold <= 0 {
		threshold = 0.8
	}

	var results []diagnosis.FaultReportVector
	vecStr := formatVector(embedding)
	err := r.db.WithContext(ctx).
		Raw(
			"SELECT * FROM fault_report_vectors WHERE embedding <=> ?::vector < ? ORDER BY embedding <=> ?::vector ASC LIMIT ?",
			vecStr, threshold, vecStr, limit,
		).
		Scan(&results).Error
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}
	return results, nil
}

func (r *VectorRetriever) SearchSimilarByFingerprint(ctx context.Context, fingerprint string, limit int) ([]diagnosis.FaultReportVector, error) {
	if limit <= 0 {
		limit = 5
	}

	var source diagnosis.FaultReportVector
	err := r.db.WithContext(ctx).
		Where("alert_fingerprint = ?", fingerprint).
		Order("created_at DESC").
		First(&source).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("find source vector by fingerprint: %w", err)
	}

	if len(source.Embedding) == 0 {
		return nil, nil
	}

	return r.SearchSimilarCases(ctx, source.Embedding, limit, 0.7)
}
