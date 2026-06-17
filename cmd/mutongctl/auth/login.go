package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
	"gitee.com/tddh/mutong/services/httpclient"
)

const (
	defaultClientID = "mutongctl"
	tokenFileMode   = 0o600
)

// tokenEntry mirrors the JSON structure of ~/.mutong/tokens.json entries.
type tokenEntry struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

type loginOptions struct {
	IO            *iostreams.IOStreams
	ServerURL     string
	HTTPClient    *http.Client
	NoBrowserFlag bool
}

func newCmdLogin(f *Factory) *cobra.Command {
	opts := &loginOptions{
		IO:         f.IO,
		HTTPClient: httpclient.New(30 * time.Second),
	}

	cmd := &cobra.Command{
		Use:   "login [-s server-url]",
		Short: "Login to Mutong server via OAuth 2.0 Authorization Code Grant",
		Long: `Authenticate with the Mutong server using a browser-based login flow.

A local HTTP server is started to receive the OAuth2 callback from your browser.
1. Local server starts on a random port
2. Default browser opens the login/authorization page
3. After logging in, the browser redirects back to the local callback
4. The CLI exchanges the authorization code for a JWT access token
5. Token is saved to ~/.mutong/tokens.json
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.ServerURL == "" {
				opts.ServerURL = "http://localhost:8888"
			}
			return runLogin(opts)
		},
	}

	cmd.Flags().BoolVar(&opts.NoBrowserFlag, "no-browser", false, "Print the URL instead of opening the browser")

	return cmd
}

func runLogin(opt *loginOptions) error {
	// Step 1: Start local callback server on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("[ERROR] failed to start local callback server (exit_code=1)\n[CONTEXT] %v\n[HINT] check port availability", err)
	}
	localPort := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", localPort)

	// Step 2: Generate state and PKCE code verifier
	state := generateRandomToken()
	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		return fmt.Errorf("[ERROR] failed to generate PKCE verifier (exit_code=1)\n[CONTEXT] %v", err)
	}
	codeChallenge := computeCodeChallenge(codeVerifier)

	// Step 3: Build authorization URL
	authURL := fmt.Sprintf("%s/oauth2/authorize?client_id=%s&redirect_uri=%s&response_type=code&state=%s&code_challenge=%s&code_challenge_method=S256",
		opt.ServerURL, defaultClientID, url.QueryEscape(redirectURI), state, codeChallenge)

	fmt.Fprintf(opt.IO.Out, "Authenticate with Mutong server\n\n")

	// Step 4: Open browser or print URL
	if opt.NoBrowserFlag {
		fmt.Fprintf(opt.IO.Out, "➡ Visit this URL to login:\n   %s\n\n", authURL)
	} else if err := openBrowser(authURL); err != nil {
		fmt.Fprintf(opt.IO.ErrOut, "Could not open browser: %v\n", err)
		fmt.Fprintf(opt.IO.Out, "➡ Visit this URL to login:\n   %s\n\n", authURL)
	} else {
		fmt.Fprintf(opt.IO.Out, "➡ A browser window should open. If not, visit:\n   %s\n\n", authURL)
	}

	// Step 5: Wait for callback
	fmt.Fprintf(opt.IO.Out, "Waiting for authorization... (press Ctrl+C to cancel)\n")

	var authCode string
	var returnedState string
	var callbackErr error
	var once sync.Once

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			authCode = r.URL.Query().Get("code")
			returnedState = r.URL.Query().Get("state")
			if errParam := r.URL.Query().Get("error"); errParam != "" {
				callbackErr = fmt.Errorf("authorization failed: %s (%s)", errParam, r.URL.Query().Get("error_description"))
				http.Error(w, "Authorization failed. You can close this page.", http.StatusBadRequest)
				return
			}
			if authCode == "" {
				callbackErr = fmt.Errorf("no authorization code received")
				http.Error(w, "No authorization code. You can close this page.", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "<html><body style=\"display:flex;align-items:center;justify-content:center;height:100vh;font-family:sans-serif;background:#f0f5ff;color:#1890ff\"><h1>✅ Login successful! You can close this page.</h1></body></html>")
		})
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(listener) }()

	for authCode == "" && callbackErr == nil {
		time.Sleep(100 * time.Millisecond)
	}
	_ = srv.Shutdown(context.Background())

	if callbackErr != nil {
		return fmt.Errorf("[ERROR] 授权失败 (exit_code=3)\n[CONTEXT] %v\n[HINT] try again or check server logs", callbackErr)
	}

	if returnedState != state {
		return fmt.Errorf("[ERROR] state mismatch (possible CSRF) (exit_code=3)\n[HINT] try again")
	}

	// Step 6: Exchange code for token
	token, err := exchangeCodeForToken(opt, authCode, redirectURI, codeVerifier)
	if err != nil {
		return fmt.Errorf("[ERROR] 获取 token 失败 (exit_code=3)\n[CONTEXT] %v\n[HINT] check server logs at /oauth2/token", err)
	}

	// Step 7: Save token
	if err := saveToken(opt.ServerURL, token); err != nil {
		return fmt.Errorf("[ERROR] 保存 token 失败 (exit_code=2)\n[CONTEXT] %v", err)
	}

	fmt.Fprintln(opt.IO.Out)
	fmt.Fprintf(opt.IO.Out, "✓ Logged in successfully!\n\n")
	fmt.Fprintf(opt.IO.Out, "  Server:    %s\n", opt.ServerURL)
	fmt.Fprintf(opt.IO.Out, "  Token:     %s\n", maskSecret(token.AccessToken))
	fmt.Fprintf(opt.IO.Out, "  Expires:   %s\n", token.Expiry.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(opt.IO.Out, "  Token file: %s\n", tokensFilePath())
	return nil
}

func generateRandomToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func computeCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// exchangeCodeForToken exchanges the authorization code for an access token.
func exchangeCodeForToken(opts *loginOptions, code, redirectURI, codeVerifier string) (*tokenEntry, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", defaultClientID)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", codeVerifier)

	resp, err := opts.HTTPClient.PostForm(opts.ServerURL+"/oauth2/token", form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("server returned %d: %s (%s)", resp.StatusCode, errResp.Error, errResp.Description)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &tokenEntry{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		TokenType:    result.TokenType,
		Expiry:       time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
	}, nil
}

func saveToken(server string, token *tokenEntry) error {
	home, _ := os.UserHomeDir()
	tokensFile := filepath.Join(home, ".mutong", "tokens.json")

	if err := os.MkdirAll(filepath.Dir(tokensFile), 0o700); err != nil {
		return err
	}

	var tokens map[string]tokenEntry
	if data, err := os.ReadFile(tokensFile); err == nil {
		_ = json.Unmarshal(data, &tokens)
	}
	if tokens == nil {
		tokens = make(map[string]tokenEntry)
	}

	tokens[server] = *token

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(tokensFile, data, tokenFileMode)
}

func tokensFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".mutong", "tokens.json")
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

func maskSecret(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return token[:6] + "..." + token[len(token)-4:]
}
