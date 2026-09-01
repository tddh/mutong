package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	ID           uint   `gorm:"primaryKey"`
	PlanID       string `gorm:"index"`
	Action       string
	Target       string
	Namespace    string
	Resource     string
	Reason       string
	Risk         string
	Result       string
	Fingerprint  string `gorm:"index"`
	AutoExecuted bool
	ApprovedBy   string
	Timestamp    time.Time `gorm:"index"`
	// Details 存放完整计划 JSON（镜像、容器、资源值、副本数等），审批执行时还原
	Details string `gorm:"type:text"`
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
	details, _ := json.Marshal(log.Plan)
	m := auditLogModel{
		PlanID:       log.Plan.ID,
		Action:       string(log.Plan.Action),
		Target:       log.Plan.Target,
		Namespace:    log.Plan.Namespace,
		Resource:     log.Plan.ResourceName,
		Reason:       log.Plan.Reason,
		Risk:         string(log.Plan.Risk),
		Result:       log.Result.Message,
		Fingerprint:  log.Plan.Fingerprint,
		AutoExecuted: log.AutoExecuted,
		ApprovedBy:   log.ApprovedBy,
		Timestamp:    log.Timestamp,
		Details:      string(details),
	}
	return s.db.Create(&m).Error
}

// planFromRow 从审计行还原执行计划：优先用 Details 里的完整计划，旧数据回退到列字段拼装
func planFromRow(r auditLogModel) ex.ExecutionPlan {
	if r.Details != "" {
		var p ex.ExecutionPlan
		if err := json.Unmarshal([]byte(r.Details), &p); err == nil {
			if p.ID == "" {
				p.ID = r.PlanID
			}
			if p.Action == "" {
				p.Action = ex.ActionType(r.Action)
			}
			if p.Namespace == "" {
				p.Namespace = r.Namespace
			}
			if p.ResourceName == "" {
				p.ResourceName = r.Resource
			}
			if p.Fingerprint == "" {
				p.Fingerprint = r.Fingerprint
			}
			return p
		}
	}
	return ex.ExecutionPlan{
		ID:           r.PlanID,
		Action:       ex.ActionType(r.Action),
		Target:       r.Target,
		Namespace:    r.Namespace,
		ResourceName: r.Resource,
		Reason:       r.Reason,
		Risk:         ex.RiskLevel(r.Risk),
		Fingerprint:  r.Fingerprint,
		CreatedAt:    r.Timestamp,
	}
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
	if v, ok := filters["fingerprint"]; ok {
		q = q.Where("fingerprint = ?", v)
	}
	if err := q.Order("timestamp desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	// Convert DB rows back to ex.AuditLog objects
	out := make([]ex.AuditLog, 0, len(rows))
	for _, r := range rows {
		plan := planFromRow(r)
		res := ex.ExecutionResult{Message: r.Result, Timestamp: r.Timestamp}
		a := ex.AuditLog{
			ID:           fmt.Sprintf("audit-%d", r.ID),
			Plan:         plan,
			Result:       res,
			AutoExecuted: r.AutoExecuted,
			ApprovedBy:   r.ApprovedBy,
			Timestamp:    r.Timestamp,
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *PostgresAuditStore) GetByID(id string) (*ex.AuditLog, error) {
	numID, err := strconv.Atoi(id)
	if err != nil {
		return nil, fmt.Errorf("invalid audit id: %s", id)
	}
	var r auditLogModel
	if err := s.db.First(&r, numID).Error; err != nil {
		return nil, err
	}
	plan := planFromRow(r)
	res := ex.ExecutionResult{Message: r.Result, Timestamp: r.Timestamp}
	a := ex.AuditLog{ID: fmt.Sprintf("audit-%d", r.ID), Plan: plan, Result: res, AutoExecuted: r.AutoExecuted, ApprovedBy: r.ApprovedBy, Timestamp: r.Timestamp}
	return &a, nil
}
