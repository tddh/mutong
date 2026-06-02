package inspection

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	inspectionModel "gitee.com/tddh/mutong/models/inspection"
	"gorm.io/gorm"
)

type InspectionReporter struct {
	logger       interfaces.Logger
	latestReport *inspectionModel.InspectionReport
	mu           sync.RWMutex
	db           *gorm.DB
}

func NewInspectionReporter(logger interfaces.Logger, db *gorm.DB) interfaces.InspectionReporter {
	return &InspectionReporter{logger: logger, db: db}
}

func (r *InspectionReporter) GenerateReport(results []inspectionModel.InspectionResult) *inspectionModel.InspectionReport {
	summary := inspectionModel.ReportSummary{
		Total:      len(results),
		ByCategory: make(map[string]int),
	}

	for _, res := range results {
		switch res.Severity {
		case "critical":
			summary.Critical++
		case "warning":
			summary.Warning++
		default:
			summary.Info++
		}
		summary.ByCategory[res.RuleName]++
	}

	issues := make([]inspectionModel.InspectionResult, len(results))
	copy(issues, results)

	report := &inspectionModel.InspectionReport{
		ID:          fmt.Sprintf("RPT-%s-%03d-%04d", time.Now().Format("20060102-150405"), time.Now().Nanosecond()/1e6, rand.Intn(10000)), //nolint:gosec
		GeneratedAt: time.Now(),
		Issues:      issues,
		Summary:     summary,
	}

	r.mu.Lock()
	r.latestReport = report
	r.mu.Unlock()

	r.logger.Info("Inspection report generated",
		zap.String("id", report.ID),
		zap.Int("total", summary.Total),
		zap.Int("critical", summary.Critical),
		zap.Int("warning", summary.Warning))

	// Persist to MySQL if a DB is configured
	if r.db != nil {
		model := inspectionModel.FromReport(report)
		if model != nil {
			_ = r.db.Create(model).Error
		}
	}

	return report
}

func (r *InspectionReporter) GetLatestReport() (*inspectionModel.InspectionReport, error) {
	r.mu.RLock()
	latest := r.latestReport
	r.mu.RUnlock()

	if latest != nil {
		return latest, nil
	}

	if r.db != nil {
		var model inspectionModel.InspectionReportModel
		if err := r.db.Order("generated_at desc").First(&model).Error; err != nil {
			return nil, fmt.Errorf("no report available")
		}
		report := model.ToReport()
		r.mu.Lock()
		r.latestReport = report
		r.mu.Unlock()
		return report, nil
	}

	return nil, fmt.Errorf("no report available")
}

// GetReportByID fetches a report by its ID from MySQL (if configured) or memory as fallback.
func (r *InspectionReporter) GetReportByID(id string) (*inspectionModel.InspectionReport, error) {
	if r.db != nil {
		var model inspectionModel.InspectionReportModel
		if err := r.db.First(&model, "id = ?", id).Error; err != nil {
			return nil, err
		}
		return model.ToReport(), nil
	}
	// Fallback: return latest if no DB
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.latestReport == nil {
		return nil, fmt.Errorf("no report available")
	}
	return r.latestReport, nil
}

// GetReportsByRange returns reports within a time range.
func (r *InspectionReporter) GetReportsByRange(start, end time.Time, limit, offset int) ([]*inspectionModel.InspectionReport, error) {
	results := []*inspectionModel.InspectionReport{}
	if r.db != nil {
		var models []inspectionModel.InspectionReportModel
		query := r.db.Where("generated_at >= ? AND generated_at < ?", start, end).Order("generated_at desc")
		if limit > 0 {
			query = query.Limit(limit)
		}
		if offset > 0 {
			query = query.Offset(offset)
		}
		if err := query.Find(&models).Error; err != nil {
			return nil, err
		}
		for i := range models {
			results = append(results, models[i].ToReport())
		}
		return results, nil
	}
	// Memory fallback: return latest up to limit
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.latestReport == nil {
		return results, nil
	}
	results = append(results, r.latestReport)
	return results, nil
}

// GetTrendData returns daily issue counts for the past given days.
func (r *InspectionReporter) GetTrendData(days int) ([]inspectionModel.TrendPoint, error) {
	type point struct {
		Date  time.Time
		Count int
	}
	var pts []point
	if days <= 0 {
		days = 1
	}
	if r.db != nil {
		// Build last N days trend by summing TotalIssues per day
		for i := 0; i < days; i++ {
			day := time.Now().AddDate(0, 0, -i)
			start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
			end := start.Add(24 * time.Hour)
			var sum int
			r.db.Model(&inspectionModel.InspectionReportModel{}).Where("generated_at >= ? AND generated_at < ?", start, end).Select("COALESCE(SUM(total_issues), 0) as sum").Scan(&sum)
			pts = append(pts, point{Date: start, Count: sum})
		}
		// convert to public type below
		res := make([]inspectionModel.TrendPoint, 0, len(pts))
		for _, p := range pts {
			res = append(res, inspectionModel.TrendPoint{Date: p.Date, Count: p.Count})
		}
		return res, nil
	}
	// Memory fallback: zeros
	var res []inspectionModel.TrendPoint
	now := time.Now()
	for i := 0; i < days; i++ {
		d := time.Date(now.Year(), now.Month(), now.Day()-i, 0, 0, 0, 0, now.Location())
		res = append(res, inspectionModel.TrendPoint{Date: d, Count: 0})
	}
	return res, nil
}

func CompareReports(a, b *inspectionModel.InspectionReport) *inspectionModel.ComparisonResult {
	result := &inspectionModel.ComparisonResult{
		NewIssues:        make([]inspectionModel.InspectionResult, 0),
		ResolvedIssues:   make([]inspectionModel.InspectionResult, 0),
		PersistentIssues: make([]inspectionModel.InspectionResult, 0),
	}

	bIssues := make(map[string]bool)
	for _, issue := range b.Issues {
		bIssues[issue.RuleName+issue.Message] = true
	}

	aIssues := make(map[string]bool)
	for _, issue := range a.Issues {
		aIssues[issue.RuleName+issue.Message] = true
	}

	for _, issue := range a.Issues {
		key := issue.RuleName + issue.Message
		if bIssues[key] {
			result.PersistentIssues = append(result.PersistentIssues, issue)
		} else {
			result.ResolvedIssues = append(result.ResolvedIssues, issue)
		}
	}

	for _, issue := range b.Issues {
		key := issue.RuleName + issue.Message
		if !aIssues[key] {
			result.NewIssues = append(result.NewIssues, issue)
		}
	}

	if len(result.NewIssues) > len(result.ResolvedIssues) {
		result.Trend = "worsening"
	} else if len(result.NewIssues) < len(result.ResolvedIssues) {
		result.Trend = "improving"
	} else {
		result.Trend = "stable"
	}

	return result
}
