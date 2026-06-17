package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gitee.com/tddh/mutong/services/httpclient"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
	authSource string
	verbose    bool
	adminKey   string // X-Admin-Key for privileged operations (executor)
}

// NewClient creates a client. authSource identifies token origin for error diagnostics (e.g. "env:MUTONG_TOKEN", "flag:--token", "config file").
func NewClient(serverURL, token, authSource string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(serverURL, "/"),
		token:      token,
		authSource: authSource,
		httpClient: httpclient.New(30 * time.Second),
	}
}

// SetVerbose enables verbose request logging.
func (c *Client) SetVerbose(v bool) { c.verbose = v }

// SetAdminKey sets the X-Admin-Key header value for privileged executor operations.
func (c *Client) SetAdminKey(key string) { c.adminKey = key }

// BaseURL returns the server URL.
func (c *Client) BaseURL() string { return c.baseURL }

// Do sends an HTTP request with the default timeout (30s). Errors use [ERROR]/[CONTEXT]/[HINT] format.
func (c *Client) Do(ctx context.Context, method, path string, body any) ([]byte, error) {
	return c.doRequest(ctx, method, path, body, c.httpClient)
}

// DoLongRunning sends an HTTP request with a 5-minute timeout, for slow endpoints like diagnosis.
func (c *Client) DoLongRunning(ctx context.Context, method, path string, body any) ([]byte, error) {
	client := httpclient.New(5 * time.Minute)
	return c.doRequest(ctx, method, path, body, client)
}

func (c *Client) doRequest(ctx context.Context, method, path string, body any, httpClient *http.Client) ([]byte, error) {
	fullURL := c.baseURL + path

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("request serialization failed: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("[ERROR] request creation failed (exit_code=1)\n[CONTEXT] method=%s url=%s\n[HINT] check server address format", method, fullURL)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.adminKey != "" {
		req.Header.Set("X-Admin-Key", c.adminKey)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf(
			"[ERROR] 网络连接失败 (exit_code=4)\n[CONTEXT] server=%s, timeout=30s, url=%s %s\n[HINT] verify server is reachable: curl %s/api/v1/system/status",
			c.baseURL, method, fullURL, c.baseURL,
		)
	}
	defer resp.Body.Close()

	// Limit response body to 10MB to prevent OOM
	const maxBodySize = 10 << 20
	limitedReader := io.LimitReader(resp.Body, maxBodySize)
	data, readErr := io.ReadAll(limitedReader)
	if readErr != nil {
		return nil, fmt.Errorf(
			"[ERROR] failed to read response body (exit_code=1)\n[CONTEXT] server=%s, url=%s %s\n[DETAIL] %v\n[HINT] response may be too large or connection interrupted",
			c.baseURL, method, fullURL, readErr,
		)
	}
	if len(data) >= maxBodySize {
		return nil, fmt.Errorf(
			"[ERROR] response body too large (exit_code=1)\n[CONTEXT] server=%s, url=%s %s, limit=%d\n[HINT] use more specific query parameters to narrow results",
			c.baseURL, method, fullURL, maxBodySize,
		)
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return data, nil
	case http.StatusUnauthorized:
		return nil, fmt.Errorf(
			"[ERROR] 认证失败 (exit_code=3)\n[CONTEXT] server=%s, auth_source=%s\n[HINT] run 'mutongctl config set token <your-token>' or check MUTONG_TOKEN env var",
			c.baseURL, c.authSource,
		)
	case http.StatusNotFound:
		return nil, fmt.Errorf(
			"[ERROR] 资源不存在 (exit_code=5)\n[CONTEXT] server=%s, url=%s %s\n[HINT] check resource name or verify it still exists",
			c.baseURL, method, fullURL,
		)
	default:
		return nil, fmt.Errorf(
			"[ERROR] API error (exit_code=1)\n[CONTEXT] server=%s, url=%s %s, status=%d\n[DETAIL] %s\n[HINT] use '-vv' flag for full request details",
			c.baseURL, method, fullURL, resp.StatusCode, string(data),
		)
	}
}

// tokenEntry mirrors the JSON structure of ~/.mutong/tokens.json entries.
type tokenEntry struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

// NewClientWithOAuth creates a client that auto-detects OAuth tokens from ~/.mutong/tokens.json.
// Falls back to the provided fallbackToken if no OAuth token is found.
func NewClientWithOAuth(serverURL, fallbackToken string) *Client {
	token, err := getOAuthToken(serverURL)
	if err == nil && token != "" {
		return NewClient(serverURL, token, "oauth")
	}
	return NewClient(serverURL, fallbackToken, "flag:--token (or default)")
}

func getOAuthToken(server string) (string, error) {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".mutong", "tokens.json"))
	if err != nil {
		return "", err
	}
	var tokens map[string]tokenEntry
	if err := json.Unmarshal(data, &tokens); err != nil {
		return "", err
	}
	t, ok := tokens[server]
	if !ok {
		return "", fmt.Errorf("no token for server %s", server)
	}
	if time.Now().Before(t.Expiry) {
		return t.AccessToken, nil
	}
	if t.RefreshToken != "" {
		newToken, expiresIn, err := refreshOAuthToken(server, t.RefreshToken)
		if err != nil {
			return "", err
		}
		if err := saveOAuthToken(server, newToken, t.RefreshToken, expiresIn); err != nil {
			return "", fmt.Errorf("saving refreshed token: %w", err)
		}
		return newToken, nil
	}
	return "", fmt.Errorf("token expired and no refresh token available")
}

func refreshOAuthToken(server, refreshToken string) (string, int, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", "mutongctl")
	form.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(context.Background(), "POST", server+"/oauth/v2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := httpclient.New(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, err
	}
	if result.AccessToken == "" {
		return "", 0, fmt.Errorf("refresh token response missing access_token")
	}
	return result.AccessToken, result.ExpiresIn, nil
}

func saveOAuthToken(server, accessToken, refreshToken string, expiresIn int) error {
	home, _ := os.UserHomeDir()
	tokensFile := filepath.Join(home, ".mutong", "tokens.json")

	data, err := os.ReadFile(tokensFile)
	if err != nil {
		return fmt.Errorf("reading tokens file: %w", err)
	}
	var tokens map[string]tokenEntry
	if err := json.Unmarshal(data, &tokens); err != nil {
		return fmt.Errorf("parsing tokens file: %w", err)
	}

	t, ok := tokens[server]
	if !ok {
		return fmt.Errorf("no token entry for server %s", server)
	}

	t.AccessToken = accessToken
	if expiresIn > 0 {
		t.Expiry = time.Now().Add(time.Duration(expiresIn) * time.Second)
	}
	t.RefreshToken = refreshToken
	tokens[server] = t
	tokenData, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing tokens: %w", err)
	}
	if err := os.WriteFile(tokensFile, tokenData, 0o600); err != nil {
		return fmt.Errorf("writing tokens file: %w", err)
	}
	return nil
}
