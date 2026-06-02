package audit

import (
	"fmt"
	"time"

	maudit "gitee.com/tddh/mutong/models/audit"
	"gorm.io/gorm"
)

// OperationAuditStore defines persistence operations for operation audit logs
type OperationAuditStore interface {
	Save(log maudit.OperationAuditLog) error
	List(filters map[string]string) ([]maudit.OperationAuditLog, error)
	GetByID(id string) (*maudit.OperationAuditLog, error)
}

// PostgresOperationAuditStore implements OperationAuditStore using PostgreSQL via GORM
type PostgresOperationAuditStore struct {
	db    *gorm.DB
	table string
}

// operationAuditModel backs the operation audit logs table
type operationAuditModel struct {
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

// TableName allows custom table naming
func (operationAuditModel) TableName() string { // dynamic via package-level var
	return mysqlOperationAuditTableName
}

// global table name (configured at initialization)
var mysqlOperationAuditTableName string

// NewPostgresOperationAuditStore creates a PostgreSQL-backed operation audit store
func NewPostgresOperationAuditStore(db *gorm.DB, table string) (OperationAuditStore, error) {
	if db == nil {
		return nil, fmt.Errorf("nil db provided for PostgreSQL operation audit store")
	}
	mysqlOperationAuditTableName = table
	if err := db.AutoMigrate(&operationAuditModel{}); err != nil {
		return nil, err
	}
	return &PostgresOperationAuditStore{db: db, table: table}, nil
}

func (s *PostgresOperationAuditStore) Save(log maudit.OperationAuditLog) error {
	m := operationAuditModel{
		Operator:     log.Operator,
		Action:       log.Action,
		ResourceType: log.ResourceType,
		ResourceName: log.ResourceName,
		Namespace:    log.Namespace,
		Result:       log.Result,
		Reason:       log.Reason,
		Timestamp:    log.Timestamp,
		Details:      log.Details,
	}
	return s.db.Create(&m).Error
}

func (s *PostgresOperationAuditStore) List(filters map[string]string) ([]maudit.OperationAuditLog, error) {
	var rows []operationAuditModel
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
	out := make([]maudit.OperationAuditLog, 0, len(rows))
	for _, r := range rows {
		o := maudit.OperationAuditLog{
			ID:           r.ID,
			Operator:     r.Operator,
			Action:       r.Action,
			ResourceType: r.ResourceType,
			ResourceName: r.ResourceName,
			Namespace:    r.Namespace,
			Result:       r.Result,
			Reason:       r.Reason,
			Timestamp:    r.Timestamp,
			Details:      r.Details,
		}
		out = append(out, o)
	}
	return out, nil
}

func (s *PostgresOperationAuditStore) GetByID(id string) (*maudit.OperationAuditLog, error) {
	var r operationAuditModel
	if err := s.db.First(&r, "id = ?", id).Error; err != nil {
		return nil, err
	}
	o := maudit.OperationAuditLog{
		ID:           r.ID,
		Operator:     r.Operator,
		Action:       r.Action,
		ResourceType: r.ResourceType,
		ResourceName: r.ResourceName,
		Namespace:    r.Namespace,
		Result:       r.Result,
		Reason:       r.Reason,
		Timestamp:    r.Timestamp,
		Details:      r.Details,
	}
	return &o, nil
}
