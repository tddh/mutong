package services

import (
	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models"
)

type RoleService struct{}

func NewRoleService() interfaces.RoleInterface {
	return &RoleService{}
}

func (s *RoleService) GetRoleByID(id uint) *models.Role {
	return &models.Role{ID: id, Name: "Default Role", Permissions: `["read"]`}
}
