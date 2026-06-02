package retrospective

import "time"

type AutoRetrospectiveTask struct {
	ID           uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Fingerprint  string     `gorm:"type:varchar(64);not null" json:"fingerprint"`
	Status       string     `gorm:"type:varchar(16);default:pending" json:"status"`
	ResolvedAt   time.Time  `gorm:"not null" json:"resolved_at"`
	ScheduledAt  time.Time  `gorm:"not null" json:"scheduled_at"`
	ExecutedAt   *time.Time `json:"executed_at,omitempty"`
	ErrorMessage string     `gorm:"type:text" json:"error_message,omitempty"`
	CreatedAt    time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

func (AutoRetrospectiveTask) TableName() string { return "auto_retrospective_tasks" }
