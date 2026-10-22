package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type statusOptions struct {
	IO *iostreams.IOStreams
}

func newCmdStatus(f *Factory) *cobra.Command {
	opts := &statusOptions{IO: f.IO}

	return &cobra.Command{
		Use:   "status",
		Short: "Show current authentication status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(opts)
		},
	}
}

func runStatus(opts *statusOptions) error {
	tokensFile := tokensFilePath()

	data, err := os.ReadFile(tokensFile)
	if err != nil {
		if os.IsNotExist(err) {
			_, _ = fmt.Fprintln(opts.IO.Out, "Not logged in.")
			_, _ = fmt.Fprintln(opts.IO.Out)
			_, _ = fmt.Fprintln(opts.IO.Out, "Run 'mutongctl auth login' to authenticate.")
			return nil
		}
		return fmt.Errorf("reading token file: %w", err)
	}

	var tokens map[string]tokenEntry
	if err := json.Unmarshal(data, &tokens); err != nil {
		return fmt.Errorf("parsing token file: %w", err)
	}

	if len(tokens) == 0 {
		_, _ = fmt.Fprintln(opts.IO.Out, "Not logged in. Token file is empty.")
		return nil
	}

	_, _ = fmt.Fprintln(opts.IO.Out, "Authenticated sessions:")
	_, _ = fmt.Fprintln(opts.IO.Out)

	now := time.Now()
	for server, token := range tokens {
		expired := now.After(token.Expiry)
		expiryStr := token.Expiry.Local().Format("2006-01-02 15:04:05")

		status := "✓ valid"
		if expired {
			status = "✗ expired"
		}

		_, _ = fmt.Fprintf(opts.IO.Out, "  Server:    %s\n", server)
		_, _ = fmt.Fprintf(opts.IO.Out, "  Type:      %s\n", token.TokenType)
		_, _ = fmt.Fprintf(opts.IO.Out, "  Token:     %s\n", maskSecret(token.AccessToken))
		_, _ = fmt.Fprintf(opts.IO.Out, "  Expires:   %s\n", expiryStr)
		if token.RefreshToken != "" {
			_, _ = fmt.Fprintf(opts.IO.Out, "  Refresh:   ✓ available (auto-refresh enabled)\n")
		} else {
			_, _ = fmt.Fprintf(opts.IO.Out, "  Refresh:   ✗ not available\n")
		}
		_, _ = fmt.Fprintf(opts.IO.Out, "  Status:    %s\n", status)
		_, _ = fmt.Fprintf(opts.IO.Out, "  File:      %s\n", tokensFile)
		_, _ = fmt.Fprintln(opts.IO.Out)
	}

	return nil
}
