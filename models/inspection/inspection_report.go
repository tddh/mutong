package inspection

import (
	"encoding/json"
	"time"
)

// InspectionReportModel is the GORM persistence model for an inspection report.
type InspectionReportModel struct {
	ID            string    `gorm:"primaryKey;type:char(36)"`
	GeneratedAt   time.Time `gorm:"column:generated_at;index;not null" json:"generatedAt"`
	TotalIssues   int       `json:"totalIssues"`
	CriticalCount int       `json:"criticalCount"`
	WarningCount  int       `json:"warningCount"`
	InfoCount     int       `json:"infoCount"`
	Summary       []byte    `json:"summary"`
	Issues        []byte    `json:"issues"`
	CreatedAt     time.Time `json:"createdAt"`
}

// TableName returns the table name for GORM.
func (InspectionReportModel) TableName() string {
	return "inspection_reports"
}

// ToReport converts a persistent model to the domain InspectionReport.
func (m *InspectionReportModel) ToReport() *InspectionReport {
	var summary ReportSummary
	var issues []InspectionResult
	_ = json.Unmarshal(m.Summary, &summary)
	_ = json.Unmarshal(m.Issues, &issues)
	return &InspectionReport{
		ID:          m.ID,
		GeneratedAt: m.GeneratedAt,
		Issues:      issues,
		Summary:     summary,
	}
}

// FromReport creates a new InspectionReportModel from a domain report.
func FromReport(report *InspectionReport) *InspectionReportModel {
	summaryBytes, _ := json.Marshal(report.Summary)
	issuesBytes, _ := json.Marshal(report.Issues)
	return &InspectionReportModel{
		ID:            report.ID,
		GeneratedAt:   report.GeneratedAt,
		TotalIssues:   report.Summary.Total,
		CriticalCount: report.Summary.Critical,
		WarningCount:  report.Summary.Warning,
		InfoCount:     report.Summary.Info,
		Summary:       summaryBytes,
		Issues:        issuesBytes,
		CreatedAt:     time.Now(),
	}
}
