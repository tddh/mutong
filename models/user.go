package models

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID            uint           `gorm:"primaryKey" json:"id"`
	UUID          string         `gorm:"size:36;uniqueIndex;not null" json:"uuid"`
	Username      string         `gorm:"size:64;uniqueIndex;not null" json:"username"`
	PasswordHash  string         `gorm:"size:255" json:"-"`
	Email         string         `gorm:"size:255" json:"email"`
	DisplayName   string         `gorm:"size:255" json:"display_name"`
	ZitadelUserID *string        `gorm:"size:255;uniqueIndex" json:"zitadel_user_id,omitempty"`
	AuthProvider  string         `gorm:"size:32;not null;default:local" json:"auth_provider"`
	Role          string         `gorm:"size:32;not null;default:viewer" json:"role"`
	Status        string         `gorm:"size:32;not null;default:active" json:"status"`
	LastLoginAt   *time.Time     `json:"last_login_at,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}
