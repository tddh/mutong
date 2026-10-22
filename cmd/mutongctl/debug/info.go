package debug

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/config"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Config *config.Config
}

func NewCmdDebug(f *Factory, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "debug-info",
		Short: "Print debug information",
		RunE: func(cmd *cobra.Command, args []string) error {
			info, _ := debug.ReadBuildInfo()

			_, _ = fmt.Fprintf(f.IO.Out, "=== mutongctl debug info ===\n")
			_, _ = fmt.Fprintf(f.IO.Out, "Version:    %s\n", version)
			_, _ = fmt.Fprintf(f.IO.Out, "Go version: %s\n", runtime.Version())
			_, _ = fmt.Fprintf(f.IO.Out, "OS/Arch:    %s/%s\n", runtime.GOOS, runtime.GOARCH)

			if f.Config != nil {
				_, _ = fmt.Fprintf(f.IO.Out, "\n--- Config ---\n")
				_, _ = fmt.Fprintf(f.IO.Out, "Server:    %s\n", f.Config.Server)
				_, _ = fmt.Fprintf(f.IO.Out, "Namespace: %s\n", f.Config.Namespace)
				if tok := os.Getenv("MUTONG_TOKEN"); tok != "" {
					masked := tok
					if len(masked) > 8 {
						masked = masked[:4] + "..." + masked[len(masked)-4:]
					}
					_, _ = fmt.Fprintf(f.IO.Out, "Token:     %s (env:MUTONG_TOKEN)\n", masked)
				} else {
					_, _ = fmt.Fprintf(f.IO.Out, "Token:     (use 'mutongctl auth login')\n")
				}
			}

			if f.Client != nil {
				_, _ = fmt.Fprintf(f.IO.Out, "\n--- Client ---\n")
				_, _ = fmt.Fprintf(f.IO.Out, "Server URL: %s\n", f.Client.BaseURL())
			}

			if info != nil {
				_, _ = fmt.Fprintf(f.IO.Out, "\n--- Build ---\n")
				_, _ = fmt.Fprintf(f.IO.Out, "Module: %s\n", info.Main.Path)
				_, _ = fmt.Fprintf(f.IO.Out, "Go:     %s\n", info.GoVersion)
				for _, s := range info.Settings {
					switch s.Key {
					case "vcs.revision":
						_, _ = fmt.Fprintf(f.IO.Out, "Commit: %s\n", s.Value)
					case "vcs.time":
						_, _ = fmt.Fprintf(f.IO.Out, "Time:   %s\n", s.Value)
					case "vcs.modified":
						_, _ = fmt.Fprintf(f.IO.Out, "Dirty:  %s\n", s.Value)
					}
				}
			}

			return nil
		},
	}
}
