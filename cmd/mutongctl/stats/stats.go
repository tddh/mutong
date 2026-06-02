package stats

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

func NewCmdStats(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Manage stats",
	}
	cmd.AddCommand(NewCmdGetOverview(f, nil))
	cmd.AddCommand(NewCmdSyncStats(f, nil))
	return cmd
}
