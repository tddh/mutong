package cluster

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type GetClusterStatsOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Name   string
	Format string
}

func NewCmdGetClusterStats(f *Factory, runF func(*GetClusterStatsOptions) error) *cobra.Command {
	opts := &GetClusterStatsOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "stats <name>",
		Short: "Get cluster stats",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.Name = args[0]
			if runF != nil {
				return runF(opts)
			}
			return getClusterStatsRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func getClusterStatsRun(opts *GetClusterStatsOptions) error {
	clusterAPI := api.NewClusterAPI(opts.Client)
	stats, err := clusterAPI.GetClusterStats(opts.Name)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, stats, opts.IO.ColorEnabled())
}
