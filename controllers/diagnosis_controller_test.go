package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	diagnosis_svc "gitee.com/tddh/mutong/services/diagnosis"
)

func newSessionTestController(t *testing.T, ownerID uint) (*DiagnosisController, string) {
	t.Helper()
	mgr := diagnosis_svc.NewChatSessionManager(zap.NewNop(), nil, nil, nil, nil)
	session, _, err := mgr.Create(context.Background(), ownerID, diagnosis_svc.ChatStartRequest{})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &DiagnosisController{logger: zap.NewNop(), chatManager: mgr}, session.ID
}

func newSessionTestEngine(ctrl *DiagnosisController, userID interface{}, role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		if userID != nil {
			c.Set("user_id", userID)
		}
		if role != "" {
			c.Set("role", role)
		}
	})
	engine.GET("/api/v1/diagnosis/chat/sessions/:id", ctrl.getSession)
	engine.POST("/api/v1/diagnosis/run", ctrl.runDiagnosis)
	return engine
}

// SEC-11：无法确认属主（user_id 缺失/为 0）时必须拒绝，管理员保留跨用户语义
func TestGetSessionOwnership(t *testing.T) {
	ctrl, sessionID := newSessionTestController(t, 7)
	path := "/api/v1/diagnosis/chat/sessions/" + sessionID

	cases := []struct {
		name   string
		userID interface{}
		role   string
		want   int
	}{
		{"owner", uint(7), "operator", http.StatusOK},
		{"other user", uint(8), "operator", http.StatusForbidden},
		{"missing identity", nil, "operator", http.StatusForbidden},
		{"zero identity", uint(0), "operator", http.StatusForbidden},
		{"uuid only identity", "zitadel-uuid", "operator", http.StatusForbidden},
		{"admin without numeric id", nil, "admin", http.StatusOK},
		{"admin other user", uint(8), "admin", http.StatusOK},
	}
	for _, tc := range cases {
		engine := newSessionTestEngine(ctrl, tc.userID, tc.role)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != tc.want {
			t.Errorf("%s: expected %d, got %d (%s)", tc.name, tc.want, w.Code, w.Body.String())
		}
	}
}

// 无属主的历史会话（UserID=0）也不得被普通用户读取
func TestGetSessionOwnerlessSessionDenied(t *testing.T) {
	ctrl, sessionID := newSessionTestController(t, 0)
	engine := newSessionTestEngine(ctrl, uint(7), "operator")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/diagnosis/chat/sessions/"+sessionID, nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("ownerless session must not be readable by a normal user, got %d (%s)", w.Code, w.Body.String())
	}
}

// SEC-17：请求体解析失败不得回显内部错误细节
func TestRunDiagnosisInvalidPayloadGenericError(t *testing.T) {
	ctrl := &DiagnosisController{logger: zap.NewNop()}
	engine := newSessionTestEngine(ctrl, uint(7), "operator")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/diagnosis/run", strings.NewReader(`{"fingerprint":`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid request payload") {
		t.Fatalf("expected generic error message, got %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "json:") || strings.Contains(w.Body.String(), "offset") {
		t.Fatalf("decoder detail leaked to client: %s", w.Body.String())
	}
}
