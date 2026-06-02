package oauth2

import "time"

type Grant struct {
	ID        uint   `gorm:"primaryKey"`
	UserID    uint   `gorm:"index:idx_user_client,unique;not null"`
	ClientID  string `gorm:"index:idx_user_client,unique;size:255;not null"`
	Scope     string `gorm:"size:1024"`
	Counter   int64  `gorm:"not null;default:0"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
