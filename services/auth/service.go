package auth

import (
	"gitee.com/tddh/mutong/models"
	"gorm.io/gorm"
)

type PasswordService struct{}

func NewPasswordService() *PasswordService { return &PasswordService{} }

func (s *PasswordService) HashPassword(pw string) (string, error) { return HashPassword(pw) }
func (s *PasswordService) VerifyPassword(pw, hash string) bool    { return VerifyPassword(pw, hash) }

type TokenService struct{}

func NewTokenService() *TokenService { return &TokenService{} }

func (s *TokenService) CreateToken(db *gorm.DB, userID *uint, saID *uint, name, tokenType string, scopes []string, expiresInDays int) (*models.APIToken, string, error) {
	return CreateToken(db, userID, saID, name, tokenType, scopes, expiresInDays)
}

func (s *TokenService) ListTokens(db *gorm.DB, userID uint) ([]models.APIToken, error) {
	return ListTokens(db, userID)
}

func (s *TokenService) RevokeToken(db *gorm.DB, tokenID uint, userID uint) error {
	return RevokeToken(db, tokenID, userID)
}

func (s *TokenService) ValidateToken(db *gorm.DB, rawToken string) (*models.APIToken, error) {
	return ValidateToken(db, rawToken)
}
