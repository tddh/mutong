package controllers

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	maudit "gitee.com/tddh/mutong/models/audit"
)

// fakeOpAuditStore 记录操作审计写入，用于断言终端留痕
type fakeOpAuditStore struct {
	mu   sync.Mutex
	logs []maudit.OperationAuditLog
}

func (f *fakeOpAuditStore) Save(l maudit.OperationAuditLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, l)
	return nil
}

func (f *fakeOpAuditStore) List(_ map[string]string) ([]maudit.OperationAuditLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]maudit.OperationAuditLog(nil), f.logs...), nil
}

func (f *fakeOpAuditStore) GetByID(_ string) (*maudit.OperationAuditLog, error) {
	return nil, nil
}

func (f *fakeOpAuditStore) snapshot() []maudit.OperationAuditLog {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]maudit.OperationAuditLog(nil), f.logs...)
}

func newTerminalTestServer(t *testing.T, store *fakeOpAuditStore, allowed []string) (*httptest.Server, *TerminalController) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("username", "alice") })
	ctrl := NewTerminalController(zap.NewNop(), nil, nil)
	if store != nil {
		ctrl.SetAuditStore(store)
	}
	if allowed != nil {
		ctrl.SetAllowedNamespaces(allowed)
	}
	ctrl.RegisterRoutes(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return srv, ctrl
}

func dialTerminalWS(t *testing.T, srv *httptest.Server, query string) string {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/api/v1/terminal/ws?"+query, nil)
	if err != nil {
		t.Fatalf("dial terminal ws: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var sb strings.Builder
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		sb.Write(msg)
	}
	return sb.String()
}

// SEC-06：系统命名空间禁止建立终端会话，并留下审计
func TestTerminalWSRejectsSystemNamespace(t *testing.T) {
	store := &fakeOpAuditStore{}
	srv, _ := newTerminalTestServer(t, store, nil)

	out := dialTerminalWS(t, srv, "namespace=kube-system&pod=coredns-1")
	if !strings.Contains(out, "namespace not allowed") {
		t.Fatalf("expected namespace rejection message, got %q", out)
	}
	logs := store.snapshot()
	if len(logs) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(logs))
	}
	if logs[0].Action != "terminal_session_denied" || logs[0].Operator != "alice" ||
		logs[0].Namespace != "kube-system" || logs[0].ResourceName != "coredns-1" {
		t.Errorf("unexpected audit record: %+v", logs[0])
	}
}

// 配置白名单后仅允许列表内命名空间
func TestTerminalWSHonorsNamespaceWhitelist(t *testing.T) {
	store := &fakeOpAuditStore{}
	srv, _ := newTerminalTestServer(t, store, []string{"team-a"})

	if out := dialTerminalWS(t, srv, "namespace=default&pod=p1"); !strings.Contains(out, "namespace not allowed") {
		t.Fatalf("namespace outside whitelist must be rejected, got %q", out)
	}
	if out := dialTerminalWS(t, srv, "namespace=team-a&pod=p1"); strings.Contains(out, "namespace not allowed") {
		t.Fatalf("whitelisted namespace must be accepted, got %q", out)
	}
}

// SEC-06：会话建立与 exec 均写审计，且不回显 Exec URL / 后端错误细节
func TestTerminalWSAuditsSessionAndExec(t *testing.T) {
	store := &fakeOpAuditStore{}
	srv, ctrl := newTerminalTestServer(t, store, nil)

	out := dialTerminalWS(t, srv, "namespace=default&pod=p1&container=app")
	if strings.Contains(out, "Exec URL") || strings.Contains(out, "K8s API Host") {
		t.Fatalf("exec endpoint leaked to client: %q", out)
	}

	deadline := time.Now().Add(3 * time.Second)
	var logs []maudit.OperationAuditLog
	for time.Now().Before(deadline) {
		logs = store.snapshot()
		if len(logs) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(logs) < 2 {
		t.Fatalf("expected session open + exec audit records, got %+v", logs)
	}
	if logs[0].Action != "terminal_session_open" || logs[0].Operator != "alice" || logs[0].Namespace != "default" {
		t.Errorf("unexpected session audit record: %+v", logs[0])
	}
	if logs[1].Action != "terminal_exec" || !strings.Contains(logs[1].Result, "failed") {
		t.Errorf("unexpected exec audit record: %+v", logs[1])
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && ctrl.Manager.SessionCount() != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if ctrl.Manager.SessionCount() != 0 {
		t.Errorf("session should be closed after the websocket disconnects, got %d", ctrl.Manager.SessionCount())
	}
}
