package interfaces

import (
	"context"

	ex "gitee.com/tddh/mutong/models/executor"
)

// Executor defines the remediation executor interface
type Executor interface {
	// Execute runs the given execution plan and returns the result
	Execute(ctx context.Context, plan ex.ExecutionPlan) (*ex.ExecutionResult, error)
	// IsAutoMode returns true if executor is running in autonomous (auto) mode
	IsAutoMode() bool
	// GetAuditLogs retrieves audit logs filtered by provided criteria
	GetAuditLogs(ctx context.Context, filters map[string]string) ([]ex.AuditLog, error)
	// RecordAudit writes an audit log entry without executing, used to record plans generated but not executed
	RecordAudit(ctx context.Context, log ex.AuditLog) error
}
