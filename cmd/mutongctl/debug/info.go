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

			fmt.Fprintf(f.IO.Out, "=== mutongctl debug info ===\n")
			fmt.Fprintf(f.IO.Out, "Version:    %s\n", version)
			fmt.Fprintf(f.IO.Out, "Go version: %s\n", runtime.Version())
			fmt.Fprintf(f.IO.Out, "OS/Arch:    %s/%s\n", runtime.GOOS, runtime.GOARCH)

			if f.Config != nil {
				fmt.Fprintf(f.IO.Out, "\n--- Config ---\n")
				fmt.Fprintf(f.IO.Out, "Server:    %s\n", f.Config.Server)
				fmt.Fprintf(f.IO.Out, "Namespace: %s\n", f.Config.Namespace)
				if tok := os.Getenv("MUTONG_TOKEN"); tok != "" {
					masked := tok
					if len(masked) > 8 {
						masked = masked[:4] + "..." + masked[len(masked)-4:]
					}
					fmt.Fprintf(f.IO.Out, "Token:     %s (env:MUTONG_TOKEN)\n", masked)
				} else {
					fmt.Fprintf(f.IO.Out, "Token:     (use 'mutongctl auth login')\n")
				}
			}

			if f.Client != nil {
				fmt.Fprintf(f.IO.Out, "\n--- Client ---\n")
				fmt.Fprintf(f.IO.Out, "Server URL: %s\n", f.Client.BaseURL())
			}

			if info != nil {
				fmt.Fprintf(f.IO.Out, "\n--- Build ---\n")
				fmt.Fprintf(f.IO.Out, "Module: %s\n", info.Main.Path)
				fmt.Fprintf(f.IO.Out, "Go:     %s\n", info.GoVersion)
				for _, s := range info.Settings {
					switch s.Key {
					case "vcs.revision":
						fmt.Fprintf(f.IO.Out, "Commit: %s\n", s.Value)
					case "vcs.time":
						fmt.Fprintf(f.IO.Out, "Time:   %s\n", s.Value)
					case "vcs.modified":
						fmt.Fprintf(f.IO.Out, "Dirty:  %s\n", s.Value)
					}
				}
			}

			return nil
		},
	}
}
