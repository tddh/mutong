package diagnosis

import "time"

type StatsModel struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	StatKey   string    `gorm:"type:varchar(50);not null;uniqueIndex:uk_stat_key;comment:统计项标识" json:"stat_key"`
	StatValue int64     `gorm:"not null;default:0;comment:统计值" json:"stat_value"`
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime;comment:更新时间" json:"updated_at"`
}

func (StatsModel) TableName() string {
	return "diagnosis_stats"
}
