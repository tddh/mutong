package services

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models"
)

type UserService struct {
	db *gorm.DB
}

func NewUserService(db *gorm.DB) interfaces.UserInterface {
	return &UserService{db: db}
}

func (s *UserService) GetUserByID(id uint) *models.User {
	var user models.User
	if err := s.db.First(&user, id).Error; err != nil {
		return nil
	}
	return &user
}

func (s *UserService) GetUserByZitadelID(zid string) (*models.User, error) {
	var user models.User
	if err := s.db.Where("zitadel_user_id = ?", zid).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *UserService) CreateUserFromZitadel(subject, username, email, displayName string) (*models.User, error) {
	now := time.Now()
	user := &models.User{
		UUID:          uuid.New().String(),
		Username:      username,
		Email:         email,
		DisplayName:   displayName,
		ZitadelUserID: &subject,
		AuthProvider:  "zitadel",
		Role:          "viewer",
		Status:        "active",
		LastLoginAt:   &now,
	}
	if err := s.db.Create(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}
