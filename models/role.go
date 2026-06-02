package models

import "time"

type Role struct {
	ID          uint   `gorm:"primaryKey"`
	Name        string `gorm:"uniqueIndex;size:255;not null"`
	Permissions string `gorm:"type:text;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
