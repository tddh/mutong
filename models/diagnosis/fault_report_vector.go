package diagnosis

import "time"

// FaultReportVector 故障复盘报告的向量化存储（基于 pgvector）
// 同时作为图增强混合检索的数据源——向量语义搜索 + 结构化字段过滤
type FaultReportVector struct {
	ID               uint      `gorm:"primaryKey;autoIncrement"`
	ReportID         string    `gorm:"uniqueIndex;size:64;not null"`
	AlertFingerprint string    `gorm:"index;size:64"`
	Summary          string    `gorm:"type:text;not null"`
	Embedding        []float32 `gorm:"type:vector(1024)"`

	ResourceKind string `gorm:"type:varchar(64);index" json:"resource_kind"`
	ResourceName string `gorm:"type:varchar(256);index" json:"resource_name"`
	Namespace    string `gorm:"type:varchar(256)" json:"namespace"`
	FaultType    string `gorm:"type:varchar(64);index" json:"fault_type"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
}

func (FaultReportVector) TableName() string {
	return "fault_report_vectors"
}
