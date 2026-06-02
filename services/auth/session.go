package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SessionClaims represents the claims stored in a local session token.
type SessionClaims struct {
	UserID       uint   `json:"user_id"`
	UUID         string `json:"uuid"`
	Role         string `json:"role"`
	AuthProvider string `json:"auth_provider"`
	ExpiresAt    int64  `json:"exp"`
}

// SessionManager issues and verifies local HMAC-signed session tokens.
type SessionManager struct {
	secret []byte
	ttl    time.Duration
}

// NewSessionManager creates a SessionManager with the given secret and TTL.
func NewSessionManager(secret []byte, ttl time.Duration) *SessionManager {
	return &SessionManager{secret: secret, ttl: ttl}
}

// TTL returns the session token lifetime.
func (m *SessionManager) TTL() time.Duration {
	return m.ttl
}

// Issue signs the claims and returns a base64-encoded token.
func (m *SessionManager) Issue(claims SessionClaims) (string, error) {
	claims.ExpiresAt = time.Now().Add(m.ttl).Unix()
	b, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, m.secret)
	mac.Write(b)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// Verify validates the token signature and expiry, returning the claims.
func (m *SessionManager) Verify(raw string) (*SessionClaims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid session token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, m.secret)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, fmt.Errorf("invalid session signature")
	}
	var claims SessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	if time.Now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("session expired")
	}
	return &claims, nil
}
