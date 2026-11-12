package auth

import (
	"testing"
	"time"

	"gitee.com/tddh/mutong/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newGuardDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedUser(t *testing.T, db *gorm.DB, u models.User) models.User {
	t.Helper()
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func TestSessionGuardValidate(t *testing.T) {
	db := newGuardDB(t)
	user := seedUser(t, db, models.User{
		UUID: "u-1", Username: "alice", Role: "admin", Status: "active", PasswordHash: "hash-v1",
	})
	fp := PasswordFingerprint(user.PasswordHash)

	tests := []struct {
		name    string
		claims  SessionClaims
		mutate  func(*gorm.DB)
		wantErr bool
	}{
		{
			name:   "active matching session",
			claims: SessionClaims{UUID: "u-1", Role: "admin", PasswordFingerprint: fp},
		},
		{
			name:    "disabled user",
			claims:  SessionClaims{UUID: "u-1", Role: "admin", PasswordFingerprint: fp},
			mutate:  func(db *gorm.DB) { db.Model(&models.User{}).Where("uuid = ?", "u-1").Update("status", "disabled") },
			wantErr: true,
		},
		{
			name:    "role changed",
			claims:  SessionClaims{UUID: "u-1", Role: "admin", PasswordFingerprint: fp},
			mutate:  func(db *gorm.DB) { db.Model(&models.User{}).Where("uuid = ?", "u-1").Update("role", "viewer") },
			wantErr: true,
		},
		{
			name:   "password changed",
			claims: SessionClaims{UUID: "u-1", Role: "admin", PasswordFingerprint: fp},
			mutate: func(db *gorm.DB) {
				db.Model(&models.User{}).Where("uuid = ?", "u-1").Update("password_hash", "hash-v2")
			},
			wantErr: true,
		},
		{
			name:    "missing user",
			claims:  SessionClaims{UUID: "ghost", Role: "admin", PasswordFingerprint: fp},
			wantErr: true,
		},
		{
			name:    "empty uuid",
			claims:  SessionClaims{UUID: "", Role: "admin"},
			wantErr: true,
		},
		{
			name:   "legacy token without fingerprint still gets status/role check",
			claims: SessionClaims{UUID: "u-1", Role: "admin"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Fresh DB per case so mutations do not leak across cases.
			db := newGuardDB(t)
			seedUser(t, db, models.User{UUID: "u-1", Username: "alice", Role: "admin", Status: "active", PasswordHash: "hash-v1"})
			if tc.mutate != nil {
				tc.mutate(db)
			}
			guard := NewSessionGuard(db, time.Minute)
			err := guard.Validate(&tc.claims)
			if tc.wantErr && err == nil {
				t.Fatalf("expected revocation, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected valid session, got %v", err)
			}
		})
	}
}

func TestSessionGuardCacheAndInvalidate(t *testing.T) {
	db := newGuardDB(t)
	seedUser(t, db, models.User{UUID: "u-1", Username: "alice", Role: "admin", Status: "active"})
	guard := NewSessionGuard(db, time.Hour) // long TTL to exercise the cache
	claims := &SessionClaims{UUID: "u-1", Role: "admin"}

	if err := guard.Validate(claims); err != nil {
		t.Fatalf("initial validate: %v", err)
	}
	// Role changes in the DB, but the cached snapshot is still fresh.
	db.Model(&models.User{}).Where("uuid = ?", "u-1").Update("role", "viewer")
	if err := guard.Validate(claims); err != nil {
		t.Fatalf("cached snapshot should still validate within TTL, got %v", err)
	}
	// Explicit invalidation forces a re-read and revokes the session.
	guard.Invalidate("u-1")
	if err := guard.Validate(claims); err == nil {
		t.Fatal("expected revocation after Invalidate")
	}
}

func TestPasswordFingerprintStableAndDistinct(t *testing.T) {
	a := PasswordFingerprint("hash-v1")
	if a == "" || a != PasswordFingerprint("hash-v1") {
		t.Fatal("fingerprint must be non-empty and deterministic")
	}
	if a == PasswordFingerprint("hash-v2") {
		t.Fatal("different hashes must produce different fingerprints")
	}
}
