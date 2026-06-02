package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"gitee.com/tddh/mutong/config"
)

// AccessTokenClaims holds validated OIDC access token claims.
type AccessTokenClaims struct {
	Subject string
}

// AccessTokenValidator validates an OIDC access token and returns its claims.
type AccessTokenValidator interface {
	ValidateAccessToken(ctx context.Context, rawToken string) (*AccessTokenClaims, error)
}

// ZitadelUserInfo represents the user information returned by Zitadel OIDC.
type ZitadelUserInfo struct {
	Subject       string `json:"sub"`
	PreferredName string `json:"preferred_username"`
	Email         string `json:"email"`
	Name          string `json:"name"`
}

// ZitadelOIDCClient handles Zitadel OIDC authentication flows.
type ZitadelOIDCClient struct {
	issuer       string
	clientID     string
	clientSecret string
	redirectURI  string
	provider     *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	oauth2Cfg    oauth2.Config
}

// NewZitadelOIDCClient creates a ZitadelOIDCClient from the given configuration.
// It discovers the OIDC provider and sets up the OAuth2 config and ID token verifier.
func NewZitadelOIDCClient(ctx context.Context, cfg config.ZitadelConfig) (*ZitadelOIDCClient, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider for %s: %w", cfg.Issuer, err)
	}
	oauthCfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  cfg.RedirectURI,
		Scopes:       cfg.Scopes,
	}
	return &ZitadelOIDCClient{
		issuer:       cfg.Issuer,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		redirectURI:  cfg.RedirectURI,
		provider:     provider,
		verifier:     provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth2Cfg:    oauthCfg,
	}, nil
}

// BuildAuthURL constructs the Zitadel authorization URL with PKCE challenge and random state.
func (c *ZitadelOIDCClient) BuildAuthURL() (authURL, state, verifier string) {
	state = uuid.New().String()
	verifier = oauth2.GenerateVerifier()
	authURL = c.oauth2Cfg.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(verifier),
	)
	return
}

// Exchange exchanges an authorization code for an OAuth2 token and user information using PKCE.
func (c *ZitadelOIDCClient) Exchange(ctx context.Context, code, verifier string) (*oauth2.Token, *ZitadelUserInfo, error) {
	oauth2Token, err := c.oauth2Cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to exchange code: %w", err)
	}
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return oauth2Token, nil, fmt.Errorf("no id_token in token response")
	}
	idToken, err := c.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return oauth2Token, nil, fmt.Errorf("failed to verify id_token: %w", err)
	}
	var userInfo ZitadelUserInfo
	if err := idToken.Claims(&userInfo); err != nil {
		return oauth2Token, nil, fmt.Errorf("failed to parse id_token claims: %w", err)
	}
	return oauth2Token, &userInfo, nil
}

// ValidateAccessToken verifies an OIDC access token by calling the UserInfo endpoint.
func (c *ZitadelOIDCClient) ValidateAccessToken(ctx context.Context, rawToken string) (*AccessTokenClaims, error) {
	token := &oauth2.Token{AccessToken: rawToken, TokenType: "Bearer"}
	userInfo, err := c.provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err != nil {
		return nil, fmt.Errorf("failed to validate access token: %w", err)
	}
	return &AccessTokenClaims{Subject: userInfo.Subject}, nil
}
