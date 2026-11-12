package auth

import (
	"errors"
	"sync"
	"time"

	"gitee.com/tddh/mutong/models"
	"gorm.io/gorm"
)

// ErrSessionRevoked signals that an otherwise-valid session token no longer
// maps to an active, unchanged user identity.
var ErrSessionRevoked = errors.New("session revoked")

// DefaultSessionGuardTTL bounds how long a user snapshot is cached. It trades
// immediate revocation for fewer DB round-trips on cookie-authenticated
// requests: a disable/role/password change takes effect within at most this TTL.
const DefaultSessionGuardTTL = 30 * time.Second

type userSnapshot struct {
	exists         bool
	status         string
	role           string
	pwdFingerprint string
	fetchedAt      time.Time
}

// SessionGuard revalidates session claims against the current user row so that
// disabling a user, changing their role, or changing their password revokes
// outstanding sessions before their 24h expiry.
type SessionGuard struct {
	db  *gorm.DB
	ttl time.Duration

	mu    sync.Mutex
	cache map[string]userSnapshot
}

// NewSessionGuard creates a guard backed by db. A non-positive ttl falls back to
// DefaultSessionGuardTTL.
func NewSessionGuard(db *gorm.DB, ttl time.Duration) *SessionGuard {
	if ttl <= 0 {
		ttl = DefaultSessionGuardTTL
	}
	return &SessionGuard{db: db, ttl: ttl, cache: make(map[string]userSnapshot)}
}

// Validate returns nil when the session still maps to an active user with the
// same role and password, and ErrSessionRevoked otherwise.
func (g *SessionGuard) Validate(claims *SessionClaims) error {
	if g == nil || g.db == nil || claims == nil || claims.UUID == "" {
		return ErrSessionRevoked
	}
	snap := g.snapshot(claims.UUID)
	if !snap.exists || snap.status != "active" {
		return ErrSessionRevoked
	}
	if snap.role != claims.Role {
		return ErrSessionRevoked
	}
	// Tokens issued before password binding carry no fingerprint; they still get
	// status/role revocation above.
	if claims.PasswordFingerprint != "" && snap.pwdFingerprint != claims.PasswordFingerprint {
		return ErrSessionRevoked
	}
	return nil
}

// Invalidate drops the cached snapshot for a user so the next Validate re-reads
// the database. Call it after changing a user's status, role or password for
// immediate revocation instead of waiting out the TTL.
func (g *SessionGuard) Invalidate(uuid string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	delete(g.cache, uuid)
	g.mu.Unlock()
}

func (g *SessionGuard) snapshot(uuid string) userSnapshot {
	now := time.Now()
	g.mu.Lock()
	snap, ok := g.cache[uuid]
	g.mu.Unlock()
	if ok && now.Sub(snap.fetchedAt) < g.ttl {
		return snap
	}

	var user models.User
	err := g.db.Where("uuid = ?", uuid).First(&user).Error
	snap = userSnapshot{fetchedAt: now}
	if err == nil {
		snap.exists = true
		snap.status = user.Status
		snap.role = user.Role
		snap.pwdFingerprint = PasswordFingerprint(user.PasswordHash)
	}

	g.mu.Lock()
	g.cache[uuid] = snap
	g.mu.Unlock()
	return snap
}
