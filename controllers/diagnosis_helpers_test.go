package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	diag_model "gitee.com/tddh/mutong/models/diagnosis"
	diagnosis_svc "gitee.com/tddh/mutong/services/diagnosis"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestJoinStrings(t *testing.T) {
	tests := []struct {
		parts []string
		sep   string
		want  string
	}{
		{nil, ",", ""},
		{[]string{"a"}, ",", "a"},
		{[]string{"a", "b", "c"}, ", ", "a, b, c"},
		{[]string{"", ""}, "-", "-"},
	}
	for _, tc := range tests {
		if got := joinStrings(tc.parts, tc.sep); got != tc.want {
			t.Errorf("joinStrings(%v, %q) = %q, want %q", tc.parts, tc.sep, got, tc.want)
		}
	}
}

func TestTruncateStringRuneSafe(t *testing.T) {
	tests := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"exact", 5, "exact"},
		{"abcdef", 3, "abc..."},
		{"告警诊断报告", 2, "告警..."},
		{"", 3, ""},
	}
	for _, tc := range tests {
		if got := truncateString(tc.in, tc.n); got != tc.want {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}

func TestFirstUserMessage(t *testing.T) {
	msgs := []diagnosis_svc.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "assistant", Content: "hi"},
		{Role: "user", Content: "first question"},
		{Role: "user", Content: "second"},
	}
	if got := firstUserMessage(msgs); got != "first question" {
		t.Fatalf("got %q", got)
	}
	if got := firstUserMessage(nil); got != "" {
		t.Fatalf("empty messages must yield empty string, got %q", got)
	}
	if got := firstUserMessage([]diagnosis_svc.ChatMessage{{Role: "system", Content: "x"}}); got != "" {
		t.Fatalf("no user message must yield empty string, got %q", got)
	}
}

func TestParseDeepDiagnosisJSON(t *testing.T) {
	valid := `{"summary":"s","root_cause":"disk full","confidence":0.9,"evidence":["e1"],"remediation":{"action":"restart_pod","description":"d","risk_level":"low","auto_fixable":true}}`

	// Plain and markdown-fenced output both parse.
	for _, content := range []string{valid, "思考中...\n```json\n" + valid + "\n```"} {
		out, err := parseDeepDiagnosisJSON(content)
		if err != nil {
			t.Fatalf("parse failed: %v", err)
		}
		if out.RootCause != "disk full" || out.Remediation.Action != "restart_pod" || out.Confidence != 0.9 {
			t.Fatalf("unexpected parse result: %+v", out)
		}
	}

	// When several objects appear, the last complete one wins.
	early := `{"summary":"partial","root_cause":"","remediation":{"action":""}}`
	out, err := parseDeepDiagnosisJSON(early + "\n" + valid)
	if err != nil {
		t.Fatal(err)
	}
	if out.RootCause != "disk full" {
		t.Fatalf("last valid object must win: %+v", out)
	}

	// Invalid inputs are rejected.
	for _, content := range []string{
		"no json at all",
		`{"summary":"x"}`,                      // missing root_cause and action
		`{"root_cause":"rc","remediation":{}}`, // missing action
	} {
		if _, err := parseDeepDiagnosisJSON(content); err == nil {
			t.Errorf("expected error for %q", content)
		}
	}
}

func TestSSEWriteLine(t *testing.T) {
	ctrl := &DiagnosisController{logger: zap.NewNop(), sseWriteTimeout: time.Second}
	w := httptest.NewRecorder()
	write := ctrl.sseWriteLine(w, w)
	write("data: hello")
	write("data: world")
	if got := w.Body.String(); got != "data: hello\ndata: world\n" {
		t.Fatalf("unexpected SSE output: %q", got)
	}
	if !w.Flushed {
		t.Fatal("SSE lines must be flushed immediately")
	}
}

func newDiagnosisHelperController(t *testing.T, withPG bool) *DiagnosisController {
	t.Helper()
	var pgStore *diagnosis_svc.PostgresStorage
	if withPG {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.AutoMigrate(&diag_model.ChatSessionModel{}, &diag_model.ChatMessageModel{}); err != nil {
			t.Fatal(err)
		}
		pgStore = diagnosis_svc.NewPostgresStorage(db, zap.NewNop())
	}
	mgr := diagnosis_svc.NewChatSessionManager(zap.NewNop(), nil, nil, nil, pgStore)
	t.Cleanup(mgr.Stop)
	engine := diagnosis_svc.NewEngine(zap.NewNop(), nil, nil)
	return NewDiagnosisController(zap.NewNop(), engine, nil, mgr, time.Minute, nil, nil, nil, nil, nil, 0)
}

func TestDiagnosisResultAndRerunValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctrl := newDiagnosisHelperController(t, false)
	r := gin.New()
	r.GET("/api/v1/diagnosis/result/:id", ctrl.getDiagnosisResult)
	r.POST("/api/v1/diagnosis/rerun", ctrl.rerunDiagnosis)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/diagnosis/result/diag-1", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "diag-1") {
		t.Fatalf("result endpoint: got %d %s", w.Code, w.Body.String())
	}

	if w := doJSON(r, http.MethodPost, "/api/v1/diagnosis/rerun", `{bad`); w.Code != http.StatusBadRequest {
		t.Fatalf("rerun bad payload: got %d", w.Code)
	}
	w = doJSON(r, http.MethodPost, "/api/v1/diagnosis/rerun", `{}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "fingerprint") {
		t.Fatalf("rerun without fingerprint: got %d %s", w.Code, w.Body.String())
	}
}

func TestListSessionsPaginationAndStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Without persistent storage the endpoint reports failure, not empty data.
	ctrl := newDiagnosisHelperController(t, false)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", uint(7)) })
	r.GET("/sessions", ctrl.listSessions)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sessions", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("list without pgStore: got %d", w.Code)
	}

	// With storage: empty list and clamped pagination.
	ctrl = newDiagnosisHelperController(t, true)
	r = gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", uint(7)) })
	r.GET("/sessions", ctrl.listSessions)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sessions?limit=999&offset=-5", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Total  int64 `json:"total"`
		Limit  int   `json:"limit"`
		Offset int   `json:"offset"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 0 || resp.Limit != 20 || resp.Offset != 0 {
		t.Fatalf("pagination must clamp to defaults, got %+v", resp)
	}
}

func TestDeleteSessionOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctrl := newDiagnosisHelperController(t, false)
	session, _, err := ctrl.chatManager.Create(context.Background(), 7, diagnosis_svc.ChatStartRequest{})
	if err != nil {
		t.Fatal(err)
	}

	build := func(uid uint) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set("user_id", uid) })
		r.DELETE("/sessions/:id", ctrl.deleteSession)
		return r
	}

	// Non-owner is rejected and the session survives.
	w := httptest.NewRecorder()
	build(8).ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/sessions/"+session.ID, nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-owner delete: got %d", w.Code)
	}
	if _, err := ctrl.chatManager.LoadSession(context.Background(), session.ID); err != nil {
		t.Fatal("session must survive a rejected delete")
	}

	// Missing session -> 404.
	w = httptest.NewRecorder()
	build(7).ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/sessions/ghost", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing session: got %d", w.Code)
	}

	// Owner deletes successfully.
	w = httptest.NewRecorder()
	build(7).ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/sessions/"+session.ID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("owner delete: got %d: %s", w.Code, w.Body.String())
	}
	if _, err := ctrl.chatManager.LoadSession(context.Background(), session.ID); err == nil {
		t.Fatal("session must be gone after delete")
	}
}

// SEC: state-changing MCP tools are admin-only through the debug endpoint.
func TestExecuteMCPToolAdminGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctrl := newDiagnosisHelperController(t, false)

	build := func(role string) *gin.Engine {
		r := gin.New()
		if role != "" {
			r.Use(func(c *gin.Context) { c.Set("role", role) })
		}
		r.POST("/mcp/tool/:name", ctrl.executeMCPTool)
		r.GET("/tools", ctrl.listMCPTools)
		return r
	}

	for _, role := range []string{"", "operator", "viewer"} {
		w := doJSON(build(role), http.MethodPost, "/mcp/tool/restart_pod_safe", `{}`)
		if w.Code != http.StatusForbidden {
			t.Errorf("role %q calling exec tool: got %d, want 403", role, w.Code)
		}
	}

	// Tool catalog is readable and non-empty.
	w := httptest.NewRecorder()
	build("viewer").ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tools", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list tools: got %d", w.Code)
	}
	var tools []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) == 0 {
		t.Fatal("tool catalog must not be empty")
	}

	// Unknown tool -> 500 generic error (no internal detail).
	w = doJSON(build("admin"), http.MethodPost, "/mcp/tool/no_such_tool", `{}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unknown tool: got %d", w.Code)
	}
}

func TestNewDeepDiagnoseFuncWithoutToolCallingProvider(t *testing.T) {
	ctrl := newDiagnosisHelperController(t, false)
	// The chat manager has no LLM provider, so deep diagnosis must be disabled
	// rather than returning a half-wired function.
	if fn := ctrl.NewDeepDiagnoseFunc(); fn != nil {
		t.Fatal("deep diagnosis must be unavailable without an Eino provider")
	}
}
