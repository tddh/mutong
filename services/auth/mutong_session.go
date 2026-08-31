package auth

import (
	"time"

	"gitee.com/tddh/mutong/interfaces"
	"github.com/ory/fosite"
	foauth2 "github.com/ory/fosite/handler/oauth2"
	fopenid "github.com/ory/fosite/handler/openid"
	"github.com/ory/fosite/token/jwt"
	"github.com/tiendc/go-deepcopy"
	"go.uber.org/zap"
)

// MutongSession satisfies both JWTSessionContainer (for JWT access tokens) and
// openid.Session (for OpenID Connect id_token support) by embedding
// oauth2.JWTSession and adding IDToken fields.
var sessionLogger interfaces.Logger

func SetSessionLogger(l interfaces.Logger) {
	sessionLogger = l
}

type MutongSession struct {
	*foauth2.JWTSession
	Claims  *jwt.IDTokenClaims
	Headers *jwt.Headers
}

func NewMutongSession(subject string) *MutongSession {
	return &MutongSession{
		JWTSession: &foauth2.JWTSession{
			Subject: subject,
		},
		Claims:  &jwt.IDTokenClaims{},
		Headers: &jwt.Headers{},
	}
}

func (s *MutongSession) IDTokenClaims() *jwt.IDTokenClaims {
	if s.Claims == nil {
		s.Claims = &jwt.IDTokenClaims{}
	}
	return s.Claims
}

func (s *MutongSession) IDTokenHeaders() *jwt.Headers {
	if s.Headers == nil {
		s.Headers = &jwt.Headers{}
	}
	return s.Headers
}

func (s *MutongSession) GetJWTClaims() jwt.JWTClaimsContainer {
	return s.JWTSession.GetJWTClaims()
}

func (s *MutongSession) GetJWTHeader() *jwt.Headers {
	return s.JWTSession.GetJWTHeader()
}

func (s *MutongSession) SetExpiresAt(key fosite.TokenType, exp time.Time) {
	s.JWTSession.SetExpiresAt(key, exp)
}

func (s *MutongSession) GetExpiresAt(key fosite.TokenType) time.Time {
	return s.JWTSession.GetExpiresAt(key)
}

func (s *MutongSession) GetUsername() string {
	return s.JWTSession.GetUsername()
}

func (s *MutongSession) SetSubject(subject string) {
	s.JWTSession.SetSubject(subject)
}

func (s *MutongSession) GetSubject() string {
	return s.JWTSession.GetSubject()
}

func (s *MutongSession) Clone() fosite.Session {
	if s == nil {
		return nil
	}
	clone := &MutongSession{}
	if err := deepcopy.Copy(clone, s); err != nil {
		if sessionLogger != nil {
			sessionLogger.Error("MutongSession.Clone deepcopy failed, falling back to shallow copy", zap.Error(err))
		}
		clone.JWTSession = s.JWTSession
		clone.Claims = s.Claims
		clone.Headers = s.Headers
	}
	return clone
}

var (
	_ foauth2.JWTSessionContainer = (*MutongSession)(nil)
	_ fopenid.Session             = (*MutongSession)(nil)
)
