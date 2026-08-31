package auth

import (
	"testing"
	"time"

	"github.com/ory/fosite"
	foauth2 "github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/token/jwt"
)

func TestMutongSessionCloneDeepCopy(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	original := NewMutongSession("user-123")
	original.JWTSession = &foauth2.JWTSession{
		JWTClaims: &jwt.JWTClaims{
			Subject:  "user-123",
			Issuer:   "https://issuer.example.com",
			Audience: []string{"aud-1", "aud-2"},
			Scope:    []string{"openid", "profile"},
			Extra:    map[string]interface{}{"k": "v"},
		},
		JWTHeader: &jwt.Headers{
			Extra: map[string]interface{}{"kid": "jwt-key"},
		},
		ExpiresAt: map[fosite.TokenType]time.Time{
			fosite.AccessToken:  now.Add(1 * time.Hour),
			fosite.RefreshToken: now.Add(24 * time.Hour),
		},
		Username: "testuser",
		Subject:  "user-123",
	}
	original.Claims = &jwt.IDTokenClaims{
		JTI:      "jti-1",
		Issuer:   "https://issuer.example.com",
		Subject:  "user-123",
		Audience: []string{"aud-1", "aud-2"},
		Nonce:    "nonce-abc",
		Extra:    map[string]interface{}{"custom": "value", "num": 42},
	}
	original.Headers = &jwt.Headers{
		Extra: map[string]interface{}{"kid": "key-1"},
	}

	clone := original.Clone()
	if clone == nil {
		t.Fatal("Clone returned nil for non-nil session")
	}

	cs, ok := clone.(*MutongSession)
	if !ok {
		t.Fatalf("Clone returned wrong type: %T, expected *MutongSession", clone)
	}

	if cs.GetSubject() != original.GetSubject() {
		t.Errorf("Subject mismatch: got %s, want %s", cs.GetSubject(), original.GetSubject())
	}
	if cs.GetUsername() != original.GetUsername() {
		t.Errorf("Username mismatch: got %s, want %s", cs.GetUsername(), original.GetUsername())
	}
	if cs.Claims.JTI != original.Claims.JTI {
		t.Errorf("Claims.JTI mismatch: got %s, want %s", cs.Claims.JTI, original.Claims.JTI)
	}

	t.Run("pointer_independence", func(t *testing.T) {
		if cs.JWTSession == original.JWTSession {
			t.Error("clone shares JWTSession pointer with original")
		}
		if cs.Claims == original.Claims {
			t.Error("clone shares Claims pointer with original")
		}
		if cs.Headers == original.Headers {
			t.Error("clone shares Headers pointer with original")
		}
		if cs.JWTSession.JWTClaims == original.JWTSession.JWTClaims {
			t.Error("clone shares JWTClaims pointer with original")
		}
		if cs.JWTSession.JWTHeader == original.JWTSession.JWTHeader {
			t.Error("clone shares JWTHeader pointer with original")
		}
	})

	t.Run("map_independence", func(t *testing.T) {
		cs.JWTSession.ExpiresAt[fosite.AccessToken] = time.Time{}
		if original.JWTSession.ExpiresAt[fosite.AccessToken].IsZero() {
			t.Error("modifying clone's ExpiresAt map affected original")
		}

		cs.Claims.Extra["custom"] = "modified"
		if original.Claims.Extra["custom"] == "modified" {
			t.Error("modifying clone's Claims.Extra map affected original")
		}

		cs.JWTSession.JWTClaims.Extra["k"] = "modified"
		if original.JWTSession.JWTClaims.Extra["k"] == "modified" {
			t.Error("modifying clone's JWTClaims.Extra map affected original")
		}
	})

	t.Run("slice_independence", func(t *testing.T) {
		if len(cs.Claims.Audience) == 0 {
			t.Skip("clone Claims.Audience is empty")
		}
		cs.Claims.Audience[0] = "modified-aud"
		if original.Claims.Audience[0] == "modified-aud" {
			t.Error("modifying clone's Claims.Audience slice affected original")
		}

		if len(cs.JWTSession.JWTClaims.Scope) == 0 {
			t.Skip("clone JWTClaims.Scope is empty")
		}
		cs.JWTSession.JWTClaims.Scope[0] = "modified-scope"
		if original.JWTSession.JWTClaims.Scope[0] == "modified-scope" {
			t.Error("modifying clone's JWTClaims.Scope slice affected original")
		}
	})

	t.Run("field_modification_isolation", func(t *testing.T) {
		cs.JWTSession.Subject = "modified"
		if original.JWTSession.Subject == "modified" {
			t.Error("modifying clone's JWTSession.Subject affected original")
		}

		cs.Claims.JTI = "modified-jti"
		if original.Claims.JTI == "modified-jti" {
			t.Error("modifying clone's Claims.JTI affected original")
		}

		cs.JWTSession.Username = "modified-user"
		if original.JWTSession.Username == "modified-user" {
			t.Error("modifying clone's JWTSession.Username affected original")
		}
	})
}

func TestMutongSessionCloneNil(t *testing.T) {
	var nilSession *MutongSession
	if nilSession.Clone() != nil {
		t.Error("Clone of nil session should return nil")
	}
}

func TestMutongSessionClonePreservesExpiresAt(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	s := NewMutongSession("user-1")
	s.SetExpiresAt(fosite.AccessToken, now.Add(1*time.Hour))
	s.SetExpiresAt(fosite.RefreshToken, now.Add(24*time.Hour))

	clone := s.Clone()
	cs := clone.(*MutongSession)

	atOrig := s.GetExpiresAt(fosite.AccessToken)
	atClone := cs.GetExpiresAt(fosite.AccessToken)
	if !atOrig.Equal(atClone) {
		t.Errorf("AccessToken ExpiresAt mismatch: orig=%v clone=%v", atOrig, atClone)
	}

	rtOrig := s.GetExpiresAt(fosite.RefreshToken)
	rtClone := cs.GetExpiresAt(fosite.RefreshToken)
	if !rtOrig.Equal(rtClone) {
		t.Errorf("RefreshToken ExpiresAt mismatch: orig=%v clone=%v", rtOrig, rtClone)
	}

	cs.SetExpiresAt(fosite.AccessToken, time.Time{})
	if s.GetExpiresAt(fosite.AccessToken).IsZero() {
		t.Error("modifying clone's ExpiresAt affected original")
	}
}
