package models

import (
	"time"

	"gorm.io/gorm"
)

type ServiceAccount struct {
	ID          uint   `gorm:"primaryKey"`
	Name        string `gorm:"size:128;not null"`
	Description string `gorm:"size:512"`
	Status      string `gorm:"size:16;not null;default:active"`
	CreatedBy   *uint  `gorm:"index"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}
