package metrics

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

func NewCmdMetrics(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Query resource metrics and timeseries",
	}
	cmd.AddCommand(NewCmdResource(f, nil))
	cmd.AddCommand(NewCmdTimeseries(f, nil))
	return cmd
}
