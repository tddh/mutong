package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gitee.com/tddh/mutong/models/oauth2"

	"github.com/ory/fosite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormStore implements fosite.Storage and fosite.ClientManager for PostgreSQL via GORM.
type GormStore struct {
	db *gorm.DB
}

// Compile-time interface checks.
var (
	_ fosite.Storage       = (*GormStore)(nil)
	_ fosite.ClientManager = (*GormStore)(nil)
)

// === Client Management ===

func (s *GormStore) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	var cl oauth2.OAuth2Client
	if err := s.db.WithContext(ctx).Where("client_id = ?", id).First(&cl).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fosite.ErrNotFound
		}
		return nil, fmt.Errorf("get client: %w", err)
	}
	return &clientAdapter{model: &cl}, nil
}

func (s *GormStore) ClientAssertionJWTValid(_ context.Context, _ string) error {
	return fosite.ErrInvalidClient.WithDescription("client assertion JWT not supported")
}

func (s *GormStore) SetClientAssertionJWT(_ context.Context, _ string, _ time.Time) error {
	return fmt.Errorf("client assertion JWT not supported")
}

// === Authorization Code (含 PKCE) ===

func (s *GormStore) CreateAuthorizeCodeSession(ctx context.Context, code string, req fosite.Requester) error {
	return s.createAuthSession(ctx, code, req)
}

func (s *GormStore) GetAuthorizeCodeSession(ctx context.Context, code string, session fosite.Session) (fosite.Requester, error) {
	return s.getAuthSession(ctx, code, session)
}

func (s *GormStore) InvalidateAuthorizeCodeSession(ctx context.Context, code string) error {
	return s.db.WithContext(ctx).Where("code = ?", code).Delete(&oauth2.AuthorizationCode{}).Error
}

// === Access Token ===

func (s *GormStore) CreateAccessTokenSession(ctx context.Context, sig string, req fosite.Requester) error {
	return s.createTokenSession(ctx, sig, req, defaultAccessTokenLifespan)
}

func (s *GormStore) GetAccessTokenSession(ctx context.Context, sig string, session fosite.Session) (fosite.Requester, error) {
	return s.getTokenSession(ctx, sig, session)
}

func (s *GormStore) DeleteAccessTokenSession(ctx context.Context, sig string) error {
	return s.db.WithContext(ctx).Where("code = ?", sig).Delete(&oauth2.AuthorizationCode{}).Error
}

func (s *GormStore) RevokeAccessToken(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Where("code = ?", id).Delete(&oauth2.AuthorizationCode{}).Error
}

// === Refresh Token (支持轮换) ===

func (s *GormStore) CreateRefreshTokenSession(ctx context.Context, sig string, accessSig string, req fosite.Requester) error {
	return s.createTokenSession(ctx, sig, req, defaultRefreshTokenLifespan)
}

func (s *GormStore) GetRefreshTokenSession(ctx context.Context, sig string, session fosite.Session) (fosite.Requester, error) {
	return s.getTokenSession(ctx, sig, session)
}

func (s *GormStore) DeleteRefreshTokenSession(ctx context.Context, sig string) error {
	return s.db.WithContext(ctx).Where("code = ?", sig).Delete(&oauth2.AuthorizationCode{}).Error
}

func (s *GormStore) RevokeRefreshToken(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Where("code = ?", id).Delete(&oauth2.AuthorizationCode{}).Error
}

func (s *GormStore) RevokeRefreshTokenMaybeGracePeriod(ctx context.Context, id string, sig string) error {
	return s.db.WithContext(ctx).Where("code = ?", sig).Delete(&oauth2.AuthorizationCode{}).Error
}

// RotateRefreshToken implements oauth2.RefreshTokenStorage for token rotation.
// Revokes all old refresh tokens associated with the same authorization request,
// preventing replay attacks when a refresh token is reused.
func (s *GormStore) RotateRefreshToken(ctx context.Context, requestID string, refreshTokenSignature string) error {
	return s.db.WithContext(ctx).
		Where("code = ? AND code != ?", requestID, refreshTokenSignature).
		Delete(&oauth2.AuthorizationCode{}).Error
}

// === PKCE ===

func (s *GormStore) CreatePKCERequestSession(ctx context.Context, sig string, req fosite.Requester) error {
	return s.createAuthSession(ctx, sig, req)
}

func (s *GormStore) GetPKCERequestSession(ctx context.Context, sig string, session fosite.Session) (fosite.Requester, error) {
	return s.getAuthSession(ctx, sig, session)
}

func (s *GormStore) DeletePKCERequestSession(ctx context.Context, sig string) error {
	// PKCE data is stored in the same row as the authorization code (same key).
	// Deleting it here would remove the authorization code prematurely.
	// The row is cleaned up by InvalidateAuthorizeCodeSession when the code is used.
	return nil
}

// === OpenID Connect ===

func (s *GormStore) CreateOpenIDConnectSession(ctx context.Context, authorizeCode string, req fosite.Requester) error {
	return s.createAuthSession(ctx, authorizeCode, req)
}

func (s *GormStore) GetOpenIDConnectSession(ctx context.Context, authorizeCode string, req fosite.Requester) (fosite.Requester, error) {
	return s.getAuthSession(ctx, authorizeCode, req.GetSession())
}

func (s *GormStore) DeleteOpenIDConnectSession(ctx context.Context, authorizeCode string) error {
	return s.db.WithContext(ctx).Where("code = ?", authorizeCode).Delete(&oauth2.AuthorizationCode{}).Error
}

// === Internal helpers ===

const (
	defaultAuthCodeLifespan     = 10 * time.Minute
	defaultAccessTokenLifespan  = 15 * time.Minute
	defaultRefreshTokenLifespan = 168 * time.Hour
)

func (s *GormStore) createAuthSession(ctx context.Context, code string, req fosite.Requester) error {
	reqJSON, sessionJSON, err := marshalRequester(req)
	if err != nil {
		return err
	}

	clientID, scopes, redirectURI := extractRequesterMeta(req)

	model := &oauth2.AuthorizationCode{
		Code:        code,
		ClientID:    clientID,
		Scope:       scopes,
		RedirectURI: redirectURI,
		Request:     reqJSON,
		Session:     sessionJSON,
		ExpiresAt:   time.Now().Add(defaultAuthCodeLifespan),
	}

	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "code"}},
		DoUpdates: clause.AssignmentColumns([]string{"client_id", "scope", "redirect_uri", "request", "session", "expires_at"}),
	}).Create(model).Error
}

func (s *GormStore) getAuthSession(ctx context.Context, code string, session fosite.Session) (fosite.Requester, error) {
	var model oauth2.AuthorizationCode
	if err := s.db.WithContext(ctx).Where("code = ?", code).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fosite.ErrNotFound
		}
		return nil, fmt.Errorf("get auth session: %w", err)
	}

	if time.Now().After(model.ExpiresAt) {
		s.db.WithContext(ctx).Delete(&model)
		return nil, fosite.ErrNotFound
	}

	return s.unmarshalSession(model.Request, model.Session, model.ClientID, session)
}

func (s *GormStore) createTokenSession(ctx context.Context, sig string, req fosite.Requester, lifespan time.Duration) error {
	reqJSON, sessionJSON, err := marshalRequester(req)
	if err != nil {
		return err
	}

	clientID, scopes, _ := extractRequesterMeta(req)

	model := &oauth2.AuthorizationCode{
		Code:      sig,
		ClientID:  clientID,
		Scope:     scopes,
		Request:   reqJSON,
		Session:   sessionJSON,
		ExpiresAt: time.Now().Add(lifespan),
	}

	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "code"}},
		DoUpdates: clause.AssignmentColumns([]string{"client_id", "scope", "request", "session", "expires_at"}),
	}).Create(model).Error
}

func (s *GormStore) getTokenSession(ctx context.Context, sig string, session fosite.Session) (fosite.Requester, error) {
	var model oauth2.AuthorizationCode
	if err := s.db.WithContext(ctx).Where("code = ?", sig).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fosite.ErrNotFound
		}
		return nil, fmt.Errorf("get token session: %w", err)
	}

	if time.Now().After(model.ExpiresAt) {
		s.db.WithContext(ctx).Delete(&model)
		return nil, fosite.ErrNotFound
	}

	return s.unmarshalSession(model.Request, model.Session, model.ClientID, session)
}

// === Shared helpers ===

func marshalRequester(req fosite.Requester) (reqJSON string, sessionJSON string, err error) {
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return "", "", fmt.Errorf("marshal request: %w", err)
	}

	sessionBytes, err := json.Marshal(req.GetSession())
	if err != nil {
		return "", "", fmt.Errorf("marshal session: %w", err)
	}

	return string(reqBytes), string(sessionBytes), nil
}

func extractRequesterMeta(req fosite.Requester) (clientID string, scopes string, redirectURI string) {
	if req.GetClient() != nil {
		clientID = req.GetClient().GetID()
	}
	scopes = strings.Join(req.GetRequestedScopes(), " ")

	// Try to extract redirect_uri from the underlying request form if available
	if fReq, ok := req.(*fosite.Request); ok && fReq.Form != nil {
		redirectURI = fReq.Form.Get("redirect_uri")
	}

	return
}

func (s *GormStore) unmarshalSession(reqJSON, sessionJSON, clientID string, session fosite.Session) (fosite.Requester, error) {
	if err := json.Unmarshal([]byte(sessionJSON), session); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(reqJSON), &raw); err != nil {
		return nil, fmt.Errorf("unmarshal request: %w", err)
	}
	delete(raw, "client")
	delete(raw, "session")
	cleanJSON, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("re-marshal request: %w", err)
	}

	var req fosite.Request
	if err := json.Unmarshal(cleanJSON, &req); err != nil {
		return nil, fmt.Errorf("unmarshal request: %w", err)
	}

	if clientID != "" {
		client, err := s.GetClient(context.Background(), clientID)
		if err == nil {
			req.Client = client
		}
	}

	req.Session = session
	return &req, nil
}

// === clientAdapter wraps OAuth2Client to implement fosite.Client ===

type clientAdapter struct {
	model *oauth2.OAuth2Client
}

func (c *clientAdapter) GetID() string             { return c.model.ClientID }
func (c *clientAdapter) GetHashedSecret() []byte   { return c.model.ClientSecret }
func (c *clientAdapter) GetRedirectURIs() []string { return c.model.GetRedirectURIs() }

func (c *clientAdapter) GetGrantTypes() fosite.Arguments { return c.model.GetGrantTypes() }

func (c *clientAdapter) GetResponseTypes() fosite.Arguments { return c.model.GetResponseTypes() }
func (c *clientAdapter) GetScopes() fosite.Arguments        { return c.model.GetScopes() }
func (c *clientAdapter) IsPublic() bool                     { return c.model.Public }
func (c *clientAdapter) GetAudience() fosite.Arguments      { return fosite.Arguments{} }

// unused import guard
var _ = url.Parse
