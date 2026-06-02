package audit

import "time"

// OperationAuditLog is a dedicated audit record for execution operations.
// It is a lightweight, operation-focused audit log stored in MySQL.
type OperationAuditLog struct {
	ID           uint   `gorm:"primaryKey"`
	Operator     string `gorm:"index"`
	Action       string
	ResourceType string
	ResourceName string
	Namespace    string
	Result       string
	Reason       string
	Timestamp    time.Time `gorm:"index"`
	Details      string
}
