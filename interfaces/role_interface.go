package interfaces

import "gitee.com/tddh/mutong/models"

type RoleInterface interface {
	GetRoleByID(id uint) *models.Role
}
