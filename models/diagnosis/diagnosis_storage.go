package diagnosis

import "time"

type DiagnosisResultModel struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Fingerprint string    `gorm:"type:varchar(64);uniqueIndex;not null" json:"fingerprint"`
	ResultJSON  string    `gorm:"type:jsonb" json:"result_json"`
	Source      string    `gorm:"type:varchar(16);default:llm" json:"source"`
	Confidence  float64   `gorm:"default:0" json:"confidence"`
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (DiagnosisResultModel) TableName() string { return "diagnosis_results" }
