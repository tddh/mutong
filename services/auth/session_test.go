package auth

import (
	"testing"
	"time"
)

func TestSessionManagerIssueAndVerify(t *testing.T) {
	mgr := NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 24*time.Hour)
	raw, err := mgr.Issue(SessionClaims{
		UserID:       7,
		UUID:         "u-1",
		Role:         "admin",
		AuthProvider: "zitadel",
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := mgr.Verify(raw)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Role != "admin" || claims.UUID != "u-1" {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestSessionManagerVerifyRejectsTamperedToken(t *testing.T) {
	mgr := NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), 24*time.Hour)
	raw, err := mgr.Issue(SessionClaims{
		UserID: 7,
		UUID:   "u-1",
		Role:   "admin",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with the payload
	tampered := raw[:len(raw)-1] + "X"
	_, err = mgr.Verify(tampered)
	if err == nil {
		t.Fatal("expected error for tampered token")
	}
}

func TestSessionManagerVerifyRejectsExpiredToken(t *testing.T) {
	mgr := NewSessionManager([]byte("0123456789abcdef0123456789abcdef"), -1*time.Hour)
	raw, err := mgr.Issue(SessionClaims{
		UserID: 7,
		UUID:   "u-1",
		Role:   "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.Verify(raw)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}
