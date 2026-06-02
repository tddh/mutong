package interfaces

import "gitee.com/tddh/mutong/models"

type UserInterface interface {
	GetUserByID(id uint) *models.User
	GetUserByZitadelID(zitadelID string) (*models.User, error)
	CreateUserFromZitadel(subject, username, email, displayName string) (*models.User, error)
}
