package logs

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

// Factory provides shared dependencies to all logs subcommands.
type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

// NewCmdLogs creates the logs parent command.
func NewCmdLogs(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Query logs from Mutong",
	}
	cmd.AddCommand(NewCmdLogsPod(f, nil))
	cmd.AddCommand(NewCmdLogsSearch(f, nil))
	cmd.AddCommand(NewCmdLogsErrors(f, nil))
	return cmd
}
