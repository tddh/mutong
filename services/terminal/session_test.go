package terminal

import (
	"testing"
	"time"
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
