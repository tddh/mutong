package auth

import (
	"strings"
	"sync"

	"gitee.com/tddh/mutong/models"
	"github.com/casbin/casbin/v3"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"gorm.io/gorm"
)

// RBAC policy modes.
const (
	RBACOff     = "off"
	RBACShadow  = "shadow"
	RBACEnforce = "enforce"
)

var (
	enforcer    *casbin.Enforcer
	enforcerMu  sync.RWMutex
	currentMode = RBACEnforce
)

// InitCasbin loads policies and binds every active user to their role.
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

	enforcerMu.Lock()
	enforcer = e
	enforcerMu.Unlock()

	seedPolicies(e)
	return SyncAllUserRoles(db)
}

func GetEnforcer() *casbin.Enforcer {
	enforcerMu.RLock()
	defer enforcerMu.RUnlock()
	return enforcer
}

// SetRBACMode configures enforcement. Invalid values fall back to enforce.
func SetRBACMode(mode string) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case RBACOff:
		currentMode = RBACOff
	case RBACShadow:
		currentMode = RBACShadow
	default:
		currentMode = RBACEnforce
	}
}

func GetRBACMode() string { return currentMode }

// Enforce evaluates a request. With RBAC disabled everything is allowed; with
// no enforcer loaded the safe answer is deny.
func Enforce(userID, resource, action string) (bool, error) {
	if currentMode == RBACOff {
		return true, nil
	}
	enforcerMu.RLock()
	e := enforcer
	enforcerMu.RUnlock()
	if e == nil {
		return false, nil
	}
	return e.Enforce(userID, resource, action)
}

func AssignRole(userID, role string) error {
	e := GetEnforcer()
	if e == nil || userID == "" || role == "" {
		return nil
	}
	_, err := e.AddRoleForUser(userID, role)
	return err
}

func RemoveRole(userID, role string) error {
	e := GetEnforcer()
	if e == nil {
		return nil
	}
	_, err := e.DeleteRoleForUser(userID, role)
	return err
}

func seedPolicies(e *casbin.Enforcer) {
	// admin: full control. operator: read/write/execute. viewer: read-only.
	_, _ = e.AddPolicy("admin", "/*", "(read|write|execute|admin)")
	_, _ = e.AddPolicy("operator", "/*", "(read|write|execute)")
	_, _ = e.AddPolicy("viewer", "/*", "(read)")

	// Every authenticated user must be able to manage their own credentials.
	_, _ = e.AddPolicy("viewer", "/api/auth/*", "(read|write)")
	_, _ = e.AddPolicy("operator", "/api/auth/*", "(read|write)")
	_, _ = e.AddPolicy("admin", "/api/auth/*", "(read|write)")

	// Role inheritance.
	_, _ = e.AddRoleForUser("admin", "operator")
	_, _ = e.AddRoleForUser("operator", "viewer")
}

// SyncUserRole rebinds a single user (by UUID) to their current database role.
func SyncUserRole(db *gorm.DB, userID string) error {
	e := GetEnforcer()
	if e == nil || userID == "" {
		return nil
	}
	var user models.User
	if err := db.Where("uuid = ?", userID).First(&user).Error; err != nil {
		return err
	}
	_, _ = e.DeleteRolesForUser(userID)
	return AssignRole(userID, user.Role)
}

// SyncAllUserRoles binds all active users so authorization works immediately
// after startup instead of only after the next login.
func SyncAllUserRoles(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	var users []models.User
	if err := db.Where("status = ?", "active").Find(&users).Error; err != nil {
		return err
	}
	for _, u := range users {
		if u.UUID == "" || u.Role == "" {
			continue
		}
		_ = SyncUserRole(db, u.UUID)
	}
	return nil
}
