package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	ex "gitee.com/tddh/mutong/models/executor"
	"gorm.io/gorm"
)

// AuditLogStore defines persistence operations for executor audit logs
type AuditLogStore interface {
	Save(log ex.AuditLog) error
	List(filters map[string]string) ([]ex.AuditLog, error)
	GetByID(id string) (*ex.AuditLog, error)
	// UpdateResult 按计划 ID 回写执行结果（执行后验证完成时更新消息与成功标志）
	UpdateResult(planID string, success bool, message string) error
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

func (m *MemoryAuditLogStore) UpdateResult(planID string, success bool, message string) error {
	return m.inner.UpdateResult(planID, success, message)
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
	Success      bool
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
	backfillSuccessFlag(db)
	return &PostgresAuditStore{db: db, table: table}, nil
}

// backfillSuccessFlag 回填历史审计记录的 success 标志（旧版表无此列）。
// 只匹配 Execute 已知的成功消息模式，幂等，不会把失败/待审批记录误标为成功。
// 注意：AutoMigrate 为存量行填的是 NULL（不是 false），需先归一化，否则 "success = false" 匹配不到。
func backfillSuccessFlag(db *gorm.DB) {
	if err := db.Model(&auditLogModel{}).Where("success IS NULL").Update("success", false).Error; err != nil {
		fmt.Printf("[executor] normalize null success flag failed: %v\n", err)
	}

	patterns := []string{
		"pod restarted%",
		"pod deleted",
		"scaled deployment%",
		"created HPA%",
		"updated HPA%",
		"updated ConfigMap%",
		"updated Secret%",
		"updated resource limits%",
		"updated image for%",
		"rollout restart triggered%",
		"rolled back deployment%",
		"annotations updated",
		"labels updated",
	}
	conds := make([]string, 0, len(patterns))
	args := make([]interface{}, 0, len(patterns)+1)
	args = append(args, false)
	for _, p := range patterns {
		conds = append(conds, "result LIKE ?")
		args = append(args, p)
	}
	query := "success = ? AND (" + strings.Join(conds, " OR ") + ")"
	tx := db.Model(&auditLogModel{}).Where(query, args...).Update("success", true)
	if tx.Error != nil {
		fmt.Printf("[executor] backfill audit success flag failed: %v\n", tx.Error)
		return
	}
	if tx.RowsAffected > 0 {
		fmt.Printf("[executor] backfill audit success flag: rows affected = %d\n", tx.RowsAffected)
	}
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
		Success:      log.Result.Success,
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
		res := ex.ExecutionResult{Message: r.Result, Success: r.Success, Timestamp: r.Timestamp}
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
	res := ex.ExecutionResult{Message: r.Result, Success: r.Success, Timestamp: r.Timestamp}
	a := ex.AuditLog{ID: fmt.Sprintf("audit-%d", r.ID), Plan: plan, Result: res, AutoExecuted: r.AutoExecuted, ApprovedBy: r.ApprovedBy, Timestamp: r.Timestamp}
	return &a, nil
}

func (s *PostgresAuditStore) UpdateResult(planID string, success bool, message string) error {
	if planID == "" {
		return fmt.Errorf("empty plan id")
	}
	return s.db.Model(&auditLogModel{}).Where("plan_id = ?", planID).
		Updates(map[string]interface{}{"success": success, "result": message}).Error
}
