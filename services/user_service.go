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

// FindUserByUsername loads a user by username. It returns gorm.ErrRecordNotFound
// when no row matches.
func FindUserByUsername(db *gorm.DB, username string) (*models.User, error) {
	var user models.User
	if err := db.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// FindUserByUUID loads a user by UUID.
func FindUserByUUID(db *gorm.DB, uuid string) (*models.User, error) {
	var user models.User
	if err := db.Where("uuid = ?", uuid).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// FindUserByID loads a user by primary key.
func FindUserByID(db *gorm.DB, id uint) (*models.User, error) {
	var user models.User
	if err := db.First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// EnsureInitialAdmin creates the bootstrap admin only when the username does not
// already exist. It never resets an existing password, so a leaked env var
// cannot become a permanent backdoor.
func EnsureInitialAdmin(db *gorm.DB, username, passwordHash string) error {
	var existing models.User
	if err := db.Where("username = ?", username).First(&existing).Error; err == nil {
		return nil
	}
	return db.Create(&models.User{
		UUID:         uuid.New().String(),
		Username:     username,
		PasswordHash: passwordHash,
		Email:        username + "@mutong.local",
		Role:         "admin",
		Status:       "active",
	}).Error
}
