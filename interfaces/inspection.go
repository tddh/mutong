package interfaces

import (
	"context"
	"time"

	inspectionModel "gitee.com/tddh/mutong/models/inspection"
	// keep aliasing explicit to avoid name conflicts in this file
)

type InspectionRule interface {
	Name() string
	Description() string
	Severity() string
	Execute(ctx context.Context, graphDB GraphDB) ([]inspectionModel.InspectionResult, error)
}

type InspectionEngine interface {
	RegisterRule(rule InspectionRule)
	ExecuteAll(ctx context.Context) ([]inspectionModel.InspectionResult, error)
	ExecuteRule(ctx context.Context, name string) ([]inspectionModel.InspectionResult, error)
}

type InspectionReporter interface {
	GenerateReport(results []inspectionModel.InspectionResult) *inspectionModel.InspectionReport
	GetLatestReport() (*inspectionModel.InspectionReport, error)
	GetReportByID(id string) (*inspectionModel.InspectionReport, error)
	GetReportsByRange(start, end time.Time, limit, offset int) ([]*inspectionModel.InspectionReport, error)
	GetTrendData(days int) ([]inspectionModel.TrendPoint, error)
}

type InspectionScheduler interface {
	Start() error
	Stop()
}

type InspectionProcessor interface {
	Start() error
	Stop()
	ExecuteNow(ctx context.Context) ([]inspectionModel.InspectionResult, error)
	GetLatestReport() (*inspectionModel.InspectionReport, error)
	GetReportByID(id string) (*inspectionModel.InspectionReport, error)
	GetReportsByRange(start, end time.Time, limit, offset int) ([]*inspectionModel.InspectionReport, error)
	GetTrendData(days int) ([]inspectionModel.TrendPoint, error)
	CompareReports(a, b *inspectionModel.InspectionReport) *inspectionModel.ComparisonResult
}
