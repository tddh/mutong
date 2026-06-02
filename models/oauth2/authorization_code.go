package oauth2

import "time"

type AuthorizationCode struct {
	ID                  uint      `gorm:"primaryKey"`
	Code                string    `gorm:"uniqueIndex;size:255;not null"`
	CodeChallenge       string    `gorm:"size:128"`
	CodeChallengeMethod string    `gorm:"size:10;default:S256"`
	ClientID            string    `gorm:"index;size:255"`
	UserID              uint      `gorm:"index"`
	Scope               string    `gorm:"size:1024"`
	RedirectURI         string    `gorm:"size:2048"`
	Request             string    `gorm:"type:text"`
	Session             string    `gorm:"type:text"`
	ExpiresAt           time.Time `gorm:"index;not null"`
	CreatedAt           time.Time
}
