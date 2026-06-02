package models

import (
	"time"
)

type APIToken struct {
	ID               uint       `gorm:"primaryKey"`
	UserID           *uint      `gorm:"index"`
	ServiceAccountID *uint      `gorm:"index"`
	Name             string     `gorm:"size:255;not null"`
	TokenPrefix      string     `gorm:"size:12;not null"`
	TokenHash        string     `gorm:"uniqueIndex;size:64;not null"`
	TokenType        string     `gorm:"size:4;not null"`
	Scopes           string     `gorm:"type:text;not null"`
	ExpiresAt        *time.Time `gorm:"index"`
	LastUsedAt       *time.Time
	RevokedAt        *time.Time `gorm:"index"`
	RotatedFromID    *uint
	CreatedAt        time.Time
}
