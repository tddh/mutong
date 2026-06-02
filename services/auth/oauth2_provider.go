package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"github.com/ory/fosite/token/jwt"
	"gorm.io/gorm"
)

// OAuth2Config holds configuration for the OAuth 2.0 provider.
type OAuth2Config struct {
	IssuerURL            string
	AccessTokenLifespan  time.Duration
	RefreshTokenLifespan time.Duration
	AuthCodeLifespan     time.Duration
	DeviceCodeLifespan   time.Duration // reserved for Phase 3
	GlobalSecret         []byte
	RSAKeyGetter         func(context.Context) (interface{}, error)
}

// NewOAuth2Provider assembles a fosite OAuth2 provider with all supported grant types.
// Supports: Authorization Code + PKCE, Client Credentials, Refresh Token, Token Introspection/Revocation, OIDC.
func NewOAuth2Provider(db *gorm.DB, cfg *OAuth2Config) fosite.OAuth2Provider {
	store := &GormStore{db: db}

	secret := cfg.GlobalSecret
	if len(secret) == 0 {
		if envSecret := os.Getenv("MUTONG_AUTH_SECRET"); envSecret != "" {
			if decoded, err := base64.StdEncoding.DecodeString(envSecret); err == nil {
				secret = decoded
			}
		}
	}
	if len(secret) < 32 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			panic("crypto/rand failed: " + err.Error())
		}
		fmt.Fprintf(os.Stderr, "[WARNING] MUTONG_AUTH_SECRET not set, using random secret. All tokens will be invalidated on restart. Set MUTONG_AUTH_SECRET in production.\n")
	}

	fc := &fosite.Config{
		AccessTokenLifespan:            cfg.AccessTokenLifespan,
		RefreshTokenLifespan:           cfg.RefreshTokenLifespan,
		AuthorizeCodeLifespan:          cfg.AuthCodeLifespan,
		GlobalSecret:                   secret,
		EnforcePKCE:                    true,
		EnforcePKCEForPublicClients:    true,
		EnablePKCEPlainChallengeMethod: false,
		SendDebugMessagesToClients:     false,
		TokenURL:                       cfg.IssuerURL + "/oauth2/token",
		MinParameterEntropy:            32,
	}

	keyFn := cfg.RSAKeyGetter
	if keyFn == nil {
		keyFn = func(_ context.Context) (interface{}, error) { return nil, nil }
	}

	strategy := &compose.CommonStrategy{
		CoreStrategy:               compose.NewOAuth2HMACStrategy(fc),
		OpenIDConnectTokenStrategy: compose.NewOpenIDConnectStrategy(keyFn, fc),
		Signer:                     &jwt.DefaultSigner{GetPrivateKey: keyFn},
	}

	return compose.Compose(
		fc,
		store,
		strategy,
		// Authorization Code Grant
		compose.OAuth2AuthorizeExplicitFactory,
		// Client Credentials Grant
		compose.OAuth2ClientCredentialsGrantFactory,
		// Refresh Token Grant
		compose.OAuth2RefreshTokenGrantFactory,
		// PKCE (RFC 7636)
		compose.OAuth2PKCEFactory,
		// Token Introspection (RFC 7662)
		compose.OAuth2TokenIntrospectionFactory,
		// Token Revocation (RFC 7009)
		compose.OAuth2TokenRevocationFactory,
		// OpenID Connect
		compose.OpenIDConnectExplicitFactory,
		compose.OpenIDConnectRefreshFactory,
	)
}
