package executor

import (
	"errors"
	"fmt"
	"time"

	ex "gitee.com/tddh/mutong/models/executor"
	"gorm.io/gorm"
)

// AuditLogStore defines persistence operations for executor audit logs
type AuditLogStore interface {
	Save(log ex.AuditLog) error
	List(filters map[string]string) ([]ex.AuditLog, error)
	GetByID(id string) (*ex.AuditLog, error)
}

// memoryAuditLogStore wrapper to reuse existing in-memory store implementation
type MemoryAuditLogStore struct {
	inner *memoryAuditStore
}

// NewMemoryAuditLogStore wraps the existing in-memory store
func NewMemoryAuditLogStore() AuditLogStore {
	inner := &memoryAuditStore{logs: make([]ex.AuditLog, 0)}
	return &MemoryAuditLogStore{inner: inner}
}

func (m *MemoryAuditLogStore) Save(log ex.AuditLog) error {
	return m.inner.Save(log)
}

func (m *MemoryAuditLogStore) List(filters map[string]string) ([]ex.AuditLog, error) {
	return m.inner.List(filters)
}

func (m *MemoryAuditLogStore) GetByID(id string) (*ex.AuditLog, error) {
	// Search in-memory logs by ID
	m.inner.mu.RLock()
	defer m.inner.mu.RUnlock()
	for i := range m.inner.logs {
		l := m.inner.logs[i]
		if l.ID == id {
			// return a copy to avoid aliasing issues
			copy := l
			return &copy, nil
		}
	}
	return nil, errors.New("audit log not found")
}

// postgresAuditStore implements AuditLogStore using PostgreSQL via GORM
type PostgresAuditStore struct {
	db    *gorm.DB
	table string
}

// auditLogModel is the GORM model backing the MySQL audit log table
type auditLogModel struct {
	ID        uint   `gorm:"primaryKey"`
	PlanID    string `gorm:"index"`
	Action    string
	Target    string
	Namespace string
	Resource  string
	Reason    string
	Risk      string
	Result    string
	Timestamp time.Time `gorm:"index"`
}

// TableName returns the target PostgreSQL table name for audit logs
func (auditLogModel) TableName() string { // dynamic via package-level var
	return mysqlAuditTableName
}

// global table name (configured at initialization)
var mysqlAuditTableName string

// NewPostgresAuditStore creates a PostgreSQL-backed audit log store
func NewPostgresAuditStore(db *gorm.DB, table string) (AuditLogStore, error) {
	if db == nil {
		return nil, fmt.Errorf("nil db provided for PostgreSQL audit store")
	}
	mysqlAuditTableName = table
	// Ensure table exists
	if err := db.AutoMigrate(&auditLogModel{}); err != nil {
		return nil, err
	}
	return &PostgresAuditStore{db: db, table: table}, nil
}

func (s *PostgresAuditStore) Save(log ex.AuditLog) error {
	// Translate the in-memory AuditLog into a DB row
	m := auditLogModel{
		PlanID:    log.Plan.ID,
		Action:    string(log.Plan.Action),
		Target:    log.Plan.Target,
		Namespace: log.Plan.Namespace,
		Resource:  log.Plan.ResourceName,
		Reason:    log.Plan.Reason,
		Risk:      string(log.Plan.Risk),
		Result:    log.Result.Message,
		Timestamp: log.Timestamp,
	}
	return s.db.Create(&m).Error
}

func (s *PostgresAuditStore) List(filters map[string]string) ([]ex.AuditLog, error) {
	// Build query with optional filters
	var rows []auditLogModel
	q := s.db
	if v, ok := filters["action"]; ok {
		q = q.Where("action = ?", v)
	}
	if v, ok := filters["namespace"]; ok {
		q = q.Where("namespace = ?", v)
	}
	if err := q.Order("timestamp desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	// Convert DB rows back to ex.AuditLog objects
	out := make([]ex.AuditLog, 0, len(rows))
	for _, r := range rows {
		plan := ex.ExecutionPlan{
			ID:           r.PlanID,
			Action:       ex.ActionType(r.Action),
			Target:       r.Target,
			Namespace:    r.Namespace,
			ResourceName: r.Resource,
			Reason:       r.Reason,
			Risk:         ex.RiskLevel(r.Risk),
			CreatedAt:    r.Timestamp,
		}
		res := ex.ExecutionResult{Message: r.Result, Timestamp: r.Timestamp}
		a := ex.AuditLog{
			ID:           fmt.Sprintf("audit-%d", r.ID),
			Plan:         plan,
			Result:       res,
			AutoExecuted: false,
			ApprovedBy:   "",
			Timestamp:    r.Timestamp,
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *PostgresAuditStore) GetByID(id string) (*ex.AuditLog, error) {
	var r auditLogModel
	if err := s.db.First(&r, "id = ?", id).Error; err != nil {
		return nil, err
	}
	plan := ex.ExecutionPlan{
		ID:           r.PlanID,
		Action:       ex.ActionType(r.Action),
		Target:       r.Target,
		Namespace:    r.Namespace,
		ResourceName: r.Resource,
		Reason:       r.Reason,
		Risk:         ex.RiskLevel(r.Risk),
		CreatedAt:    r.Timestamp,
	}
	res := ex.ExecutionResult{Message: r.Result, Timestamp: r.Timestamp}
	a := ex.AuditLog{ID: fmt.Sprintf("audit-%d", r.ID), Plan: plan, Result: res, AutoExecuted: false, ApprovedBy: "", Timestamp: r.Timestamp}
	return &a, nil
}
