package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitee.com/tddh/mutong/models"
	"github.com/gin-gonic/gin"
)

type stubUserSvc struct{ user *models.User }

func (s *stubUserSvc) GetUserByID(uint) *models.User { return s.user }
func (s *stubUserSvc) GetUserByZitadelID(string) (*models.User, error) {
	return nil, nil
}
func (s *stubUserSvc) CreateUserFromZitadel(string, string, string, string) (*models.User, error) {
	return nil, nil
}

type stubRoleSvc struct{ role *models.Role }

func (s *stubRoleSvc) GetRoleByID(uint) *models.Role { return s.role }

func TestUserControllerGetUserByID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Found.
	r := gin.New()
	NewUserController(&stubUserSvc{user: &models.User{UUID: "u-1", Username: "alice"}}).RegisterRoutes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var u models.User
	if err := json.Unmarshal(w.Body.Bytes(), &u); err != nil {
		t.Fatal(err)
	}
	if u.UUID != "u-1" || u.Username != "alice" {
		t.Fatalf("unexpected user: %+v", u)
	}

	// Missing.
	r = gin.New()
	NewUserController(&stubUserSvc{}).RegisterRoutes(r)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing user: expected 404, got %d", w.Code)
	}
}

func TestRoleControllerGet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewRoleController(&stubRoleSvc{role: &models.Role{Name: "admin"}}).RegisterRoutes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/roles", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var role models.Role
	if err := json.Unmarshal(w.Body.Bytes(), &role); err != nil {
		t.Fatal(err)
	}
	if role.Name != "admin" {
		t.Fatalf("unexpected role: %+v", role)
	}
}

func TestControllersRegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctrls := NewControllers(&stubRoleSvc{}, &stubUserSvc{}, nil)
	r := gin.New()
	ctrls.RegisterRoutes(r)

	routes := r.Routes()
	want := map[string]bool{
		"GET /api/v1/users": false,
		"GET /api/v1/roles": false,
	}
	for _, ri := range routes {
		key := ri.Method + " " + ri.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for key, found := range want {
		if !found {
			t.Errorf("route %s not registered", key)
		}
	}
}
