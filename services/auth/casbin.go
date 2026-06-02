package auth

import (
	"gitee.com/tddh/mutong/models"
	"github.com/casbin/casbin/v3"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"gorm.io/gorm"
)

var enforcer *casbin.Enforcer

func InitCasbin(db *gorm.DB) error {
	adapter, err := gormadapter.NewAdapterByDB(db)
	if err != nil {
		return err
	}
	e, err := casbin.NewEnforcer("configs/casbin_model.conf", adapter)
	if err != nil {
		return err
	}
	e.EnableAutoSave(true)
	enforcer = e

	seedPolicies(e)
	return nil
}

func GetEnforcer() *casbin.Enforcer { return enforcer }

func Enforce(userID, resource, action string) (bool, error) {
	if enforcer == nil {
		return false, nil
	}
	return enforcer.Enforce(userID, resource, action)
}

func AssignRole(userID, role string) error {
	if enforcer == nil {
		return nil
	}
	_, err := enforcer.AddRoleForUser(userID, role)
	return err
}

func RemoveRole(userID, role string) error {
	if enforcer == nil {
		return nil
	}
	_, err := enforcer.DeleteRoleForUser(userID, role)
	return err
}

func seedPolicies(e *casbin.Enforcer) {
	_, _ = e.AddPolicy("admin", "/*", "(read|write|execute|admin)")
	_, _ = e.AddPolicy("operator", "/*", "(read|execute)")
	_, _ = e.AddPolicy("viewer", "/*", "(read)")

	_, _ = e.AddRoleForUser("admin", "operator")
	_, _ = e.AddRoleForUser("operator", "viewer")
}

func SyncUserRole(db *gorm.DB, userID string) error {
	var user models.User
	if err := db.Where("uuid = ?", userID).First(&user).Error; err != nil {
		return err
	}
	_, _ = enforcer.DeleteRolesForUser(userID)
	return AssignRole(userID, user.Role)
}
