package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"gitee.com/tddh/mutong/models"
	"gorm.io/gorm"
)

func GenerateAPIToken(tokenType string) (raw, prefix, hash string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	hexStr := hex.EncodeToString(b)

	prefixTyp := "mtp_"
	if tokenType == "sat" {
		prefixTyp = "mts_"
	}

	raw = prefixTyp + hexStr
	prefix = raw[:12]

	h := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(h[:])
	return
}

func CreateToken(db *gorm.DB, userID *uint, saID *uint, name, tokenType string, scopes []string, expiresInDays int) (*models.APIToken, string, error) {
	raw, prefix, hash := GenerateAPIToken(tokenType)

	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return nil, "", fmt.Errorf("marshal scopes: %w", err)
	}

	var expiresAt *time.Time
	if expiresInDays > 0 {
		t := time.Now().Add(time.Duration(expiresInDays) * 24 * time.Hour)
		expiresAt = &t
	}

	token := &models.APIToken{
		UserID:           userID,
		ServiceAccountID: saID,
		Name:             name,
		TokenPrefix:      prefix,
		TokenHash:        hash,
		TokenType:        tokenType,
		Scopes:           string(scopesJSON),
		ExpiresAt:        expiresAt,
	}

	if err := db.Create(token).Error; err != nil {
		return nil, "", fmt.Errorf("create token: %w", err)
	}

	return token, raw, nil
}

func ListTokens(db *gorm.DB, userID uint) ([]models.APIToken, error) {
	var tokens []models.APIToken
	if err := db.Where("user_id = ? AND revoked_at IS NULL", userID).
		Order("created_at DESC").
		Find(&tokens).Error; err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	return tokens, nil
}

func RevokeToken(db *gorm.DB, tokenID uint, userID uint) error {
	now := time.Now()
	result := db.Model(&models.APIToken{}).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL", tokenID, userID).
		Update("revoked_at", &now)
	if result.Error != nil {
		return fmt.Errorf("revoke token: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("token not found or already revoked")
	}
	return nil
}

func ValidateToken(db *gorm.DB, rawToken string) (*models.APIToken, error) {
	h := sha256.Sum256([]byte(rawToken))
	hash := hex.EncodeToString(h[:])

	var token models.APIToken
	if err := db.Where("token_hash = ? AND revoked_at IS NULL", hash).
		Where("expires_at IS NULL OR expires_at > ?", time.Now()).
		First(&token).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("invalid or expired token")
		}
		return nil, fmt.Errorf("validate token: %w", err)
	}

	now := time.Now()
	db.Model(&token).Update("last_used_at", &now)

	return &token, nil
}
