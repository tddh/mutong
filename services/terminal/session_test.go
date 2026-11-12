package terminal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestSessionLifecycle(t *testing.T) {
	m := NewTerminalSessionManager()
	if got := m.SessionCount(); got != 0 {
		t.Fatalf("new manager should be empty, got %d", got)
	}

	sess, err := m.CreateSession("c", "ns", "pod", "ctr", nil)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID == "" {
		t.Fatal("session ID must not be empty")
	}
	if got := m.SessionCount(); got != 1 {
		t.Fatalf("expected 1 session, got %d", got)
	}

	if err := m.CloseSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	if got := m.SessionCount(); got != 0 {
		t.Fatalf("expected 0 sessions after close, got %d", got)
	}
	// Closing twice must be safe.
	if err := m.CloseSession(sess.ID); err != nil {
		t.Fatalf("second close should be a no-op, got %v", err)
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	m := NewTerminalSessionManager()
	sess, _ := m.CreateSession("c", "ns", "pod", "ctr", nil)
	sess.createdAt = time.Now().Add(-time.Hour)

	m.CleanupExpired(time.Minute)
	if got := m.SessionCount(); got != 0 {
		t.Fatalf("expired session should be removed, got %d", got)
	}
}

func TestWriteAfterCloseDoesNotPanic(t *testing.T) {
	m := NewTerminalSessionManager()
	sess, _ := m.CreateSession("c", "ns", "pod", "ctr", nil)
	sess.startWriter()
	_ = m.CloseSession(sess.ID)
	sess.writeText("late message")
}

// SEC-06：默认拒绝系统命名空间，配置白名单后仅允许列表内
func TestNamespacePolicy(t *testing.T) {
	m := NewTerminalSessionManager()
	for _, ns := range []string{"kube-system", "kube-public", "kube-node-lease"} {
		if m.IsNamespaceAllowed(ns) {
			t.Errorf("system namespace %q must be denied by default", ns)
		}
		if _, err := m.CreateSession("c", ns, "pod", "ctr", nil); err == nil {
			t.Errorf("CreateSession must reject system namespace %q", ns)
		}
	}
	if m.SessionCount() != 0 {
		t.Fatalf("rejected sessions must not be registered, got %d", m.SessionCount())
	}
	for _, ns := range []string{"default", "team-a", "ns"} {
		if !m.IsNamespaceAllowed(ns) {
			t.Errorf("namespace %q should be allowed by default", ns)
		}
	}
	// 非法命名字符集一律拒绝
	for _, ns := range []string{"", "Kube-System", "ns;drop", `ns"`, "ns\\", "ns\n", "ns_1"} {
		if m.IsNamespaceAllowed(ns) {
			t.Errorf("invalid namespace %q must be rejected", ns)
		}
	}

	m.SetAllowedNamespaces([]string{"team-a"})
	if !m.IsNamespaceAllowed("team-a") {
		t.Error("whitelisted namespace must be allowed")
	}
	for _, ns := range []string{"default", "kube-system", "team-b"} {
		if m.IsNamespaceAllowed(ns) {
			t.Errorf("namespace %q outside whitelist must be denied", ns)
		}
	}
}

// 清理协程可幂等停止
func TestStopCleanupIdempotent(t *testing.T) {
	m := NewTerminalSessionManager()
	m.StartCleanup(time.Millisecond, time.Minute)
	m.StopCleanup()
	m.StopCleanup()
	m.Stop()
}

// SEC-06：exec 细节（Exec URL / API 地址）不得回显客户端，且必须留审计
func TestStartK8sExecNoURLLeakAndAudit(t *testing.T) {
	var mu sync.Mutex
	var received []string
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			mu.Lock()
			received = append(received, string(msg))
			mu.Unlock()
		}
	}))
	defer wsSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "exec unsupported", http.StatusBadRequest)
	}))
	defer apiSrv.Close()

	client, err := kubernetes.NewForConfig(&rest.Config{Host: apiSrv.URL})
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(wsSrv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	m := NewTerminalSessionManager()
	sess, err := m.CreateSession("c1", "default", "pod-1", "ctr", conn)
	if err != nil {
		t.Fatal(err)
	}
	sess.SetK8sClient(client, &rest.Config{Host: apiSrv.URL})
	var events []AuditEvent
	sess.SetAuditHook(func(ev AuditEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})

	sess.StartK8sExec("c1", "default", "pod-1", "ctr", "/bin/sh")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 {
		t.Fatal("expected status messages on the websocket")
	}
	apiHost := strings.TrimPrefix(apiSrv.URL, "http://")
	for _, msg := range received {
		if strings.Contains(msg, "Exec URL") || strings.Contains(msg, apiHost) || strings.Contains(msg, "K8s API Host") {
			t.Errorf("exec endpoint leaked to client: %q", msg)
		}
		if strings.Contains(msg, "exec unsupported") {
			t.Errorf("backend error detail leaked to client: %q", msg)
		}
	}
	if len(events) == 0 {
		t.Fatal("expected terminal exec audit events")
	}
	for _, ev := range events {
		if ev.Action != "terminal_exec" || ev.Namespace != "default" || ev.Pod != "pod-1" || ev.SessionID != sess.ID {
			t.Errorf("unexpected audit event: %+v", ev)
		}
	}
}

// K8s 客户端缺失时同样留痕，且只回显通用错误
func TestStartK8sExecWithoutClientAuditsFailure(t *testing.T) {
	m := NewTerminalSessionManager()
	sess, err := m.CreateSession("c1", "default", "pod-1", "ctr", nil)
	if err != nil {
		t.Fatal(err)
	}
	var events []AuditEvent
	sess.SetAuditHook(func(ev AuditEvent) { events = append(events, ev) })
	sess.StartK8sExec("c1", "default", "pod-1", "ctr", "/bin/sh")
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	if events[0].Action != "terminal_exec" || !strings.Contains(events[0].Result, "failed") {
		t.Errorf("unexpected audit event: %+v", events[0])
	}
	if events[0].Shell != "/bin/sh" {
		t.Errorf("audit event should carry shell, got %+v", events[0])
	}
}
