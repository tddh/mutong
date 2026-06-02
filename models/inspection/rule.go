package inspection

import (
	"time"

	"gorm.io/gorm"
)

type InspectionRuleModel struct {
	ID          uint           `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	Name        string         `gorm:"uniqueIndex;size:128;not null" json:"name"`
	Description string         `json:"description"`
	Query       string         `gorm:"type:text;not null" json:"query"`
	CheckType   string         `gorm:"size:32;not null" json:"checkType"`
	CheckConfig string         `gorm:"type:text;not null" json:"checkConfig"`
	Severity    string         `gorm:"size:16;not null;default:warning" json:"severity"`
	Suggestion  string         `json:"suggestion"`
	Enabled     bool           `gorm:"default:true" json:"enabled"`
}

func (InspectionRuleModel) TableName() string {
	return "inspection_rules"
}
