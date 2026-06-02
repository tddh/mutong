package inspection

import "time"

type InspectionResult struct {
	RuleName   string    `json:"ruleName"`
	Severity   string    `json:"severity"`
	Message    string    `json:"message"`
	Resources  []string  `json:"resources"`
	Suggestion string    `json:"suggestion"`
	Timestamp  time.Time `json:"timestamp"`
}

type InspectionReport struct {
	ID          string             `json:"id"`
	GeneratedAt time.Time          `json:"generatedAt"`
	Issues      []InspectionResult `json:"issues"`
	Summary     ReportSummary      `json:"summary"`
}

type ReportSummary struct {
	Total      int            `json:"total"`
	Critical   int            `json:"critical"`
	Warning    int            `json:"warning"`
	Info       int            `json:"info"`
	ByCategory map[string]int `json:"byCategory"`
}

type ComparisonResult struct {
	NewIssues        []InspectionResult `json:"newIssues"`
	ResolvedIssues   []InspectionResult `json:"resolvedIssues"`
	PersistentIssues []InspectionResult `json:"persistentIssues"`
	Trend            string             `json:"trend"`
}
