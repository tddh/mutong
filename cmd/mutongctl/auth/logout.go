package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
	"gitee.com/tddh/mutong/services/httpclient"
)

type logoutOptions struct {
	IO        *iostreams.IOStreams
	ServerURL string
	Revoke    bool
}

func newCmdLogout(f *Factory) *cobra.Command {
	opts := &logoutOptions{IO: f.IO}

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Logout and remove cached tokens",
		Long: `Removes cached OAuth tokens from the local machine.

By default, only local token data is cleared. Use --revoke to also
notify the server to invalidate the token remotely.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.ServerURL == "" {
				opts.ServerURL = "http://localhost:8888"
			}
			return runLogout(opts)
		},
	}

	cmd.Flags().BoolVar(&opts.Revoke, "revoke", false, "Also revoke the token server-side")
	return cmd
}

func runLogout(opts *logoutOptions) error {
	tokensFile := tokensFilePath()

	if _, err := os.Stat(tokensFile); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(opts.IO.Out, "Not logged in. No tokens found.")
			return nil
		}
		return fmt.Errorf("checking token file: %w", err)
	}

	var targetTokens map[string]tokenEntry
	if opts.Revoke {
		data, err := os.ReadFile(tokensFile)
		if err == nil {
			_ = json.Unmarshal(data, &targetTokens)
		}
	}

	if opts.Revoke && targetTokens != nil {
		var failed []string
		for server, token := range targetTokens {
			if err := revokeToken(server, token.RefreshToken); err != nil {
				failed = append(failed, fmt.Sprintf("%s: %v", server, err))
			}
		}
		if len(failed) > 0 {
			fmt.Fprintf(opts.IO.ErrOut, "Warning: server-side revocation failed for:\n")
			for _, f := range failed {
				fmt.Fprintf(opts.IO.ErrOut, "  - %s\n", f)
			}
		}
	}

	if err := os.Remove(tokensFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing token file: %w", err)
	}

	fmt.Fprintln(opts.IO.Out, "✓ Logged out. Local tokens cleared.")
	if opts.Revoke {
		fmt.Fprintln(opts.IO.Out, "  Server-side revocation completed.")
	}
	fmt.Fprintln(opts.IO.Out)
	fmt.Fprintln(opts.IO.Out, "Run 'mutongctl auth login' to sign in again.")

	return nil
}

func revokeToken(server, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}

	form := url.Values{}
	form.Set("token", refreshToken)
	form.Set("token_type_hint", "refresh_token")

	client := httpclient.New(10 * time.Second)
	resp, err := client.PostForm(server+"/oauth/v2/revoke", form)
	if err != nil {
		return fmt.Errorf("revoke request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("revoke returned HTTP %d", resp.StatusCode)
	}
	return nil
}
