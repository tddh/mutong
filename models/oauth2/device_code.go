package oauth2

import "time"

type DeviceCode struct {
	ID         uint      `gorm:"primaryKey"`
	DeviceCode string    `gorm:"uniqueIndex;size:255;not null"`
	UserCode   string    `gorm:"uniqueIndex;size:32;not null"`
	ClientID   string    `gorm:"index;size:255"`
	Scope      string    `gorm:"size:1024"`
	Status     string    `gorm:"size:20;not null;default:pending"`
	UserID     *uint     `gorm:"index"`
	Request    string    `gorm:"type:text"`
	Session    string    `gorm:"type:text"`
	ExpiresAt  time.Time `gorm:"index;not null"`
	CreatedAt  time.Time
}
